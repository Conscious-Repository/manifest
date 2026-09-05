// Package artifacts is the content-addressed store behind chat file
// attachments on both team portals.
//
// One blob pool, many owners. Bytes live at
// blobs/<sha[:2]>/<sha><ext> under DataDir — OUTSIDE the vault, deliberately:
// the owner's Obsidian vault holds his words, not everyone's uploads. Identical
// bytes are stored exactly once no matter how many people send the same file.
//
// The metadata a human cares about — the filename, who sent it, when, in which
// thread — lives on the REFERENCE, never on the blob. That is the one design
// point the older realestate/cas.go gets wrong: it keys name+mime by hash, so
// the second person to upload the same PDF silently inherits the first
// person's filename. Here two people can send byte-identical files under their
// own names and the pool still holds one copy.
//
// Each portal owns an INDEX (index/<domain>.json). The index is both the
// artifact database a per-portal UI reads and the access-control list: a hash
// absent from a domain's index does not exist for that domain. That is what
// makes one shared pool safe across two businesses with different people in
// them — AION bytes never resolve for an OODA partner, even given the hash.
package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxBlobSize matches the caps already in threads.MaxBlobSize and
// realestate.MaxCASSize — one number for "a file a person attaches".
const MaxBlobSize = 25 << 20

// validDomain keeps a domain name usable as a filename with no traversal:
// [a-z0-9][a-z0-9-]{0,30}, spelled out (the package carries no regex).
func validDomain(s string) bool {
	if s == "" || len(s) > 31 || s[0] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// Ref is one attachment as a MESSAGE sees it: the pooled bytes plus this
// sender's own name for them.
type Ref struct {
	Hash string `json:"hash"` // sha256 hex — the pool key
	Name string `json:"name"` // THIS sender's filename, not the first sender's
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"` // sniffed at upload, never the client's claim
}

// Entry is one row of a domain's artifact index — a Ref plus its provenance.
type Entry struct {
	Ref
	By      string    `json:"by,omitempty"`      // uploader display name
	ByEmail string    `json:"byEmail,omitempty"` //
	At      time.Time `json:"at"`
	Thread  string    `json:"thread,omitempty"`
	Message string    `json:"message,omitempty"` // set once the message exists
}

// Store is the pool plus every domain index beside it.
type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	for _, d := range []string{"blobs", "extracts", "index"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir}, nil
}

// Save streams r into the pool. Returns the ref with the caller's name and the
// sniffed mime. Identical bytes rename into place once; every later save of
// the same bytes is a no-op that still returns a full ref.
func (s *Store) Save(r io.Reader, name, mime string) (Ref, error) {
	tmp, err := os.CreateTemp(s.dir, "blob-*")
	if err != nil {
		return Ref{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, MaxBlobSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Ref{}, err
	}
	if n > MaxBlobSize {
		return Ref{}, fmt.Errorf("file too large (%d MB max)", MaxBlobSize>>20)
	}
	if n == 0 {
		return Ref{}, errors.New("empty file")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dir := filepath.Join(s.dir, "blobs", sum[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Ref{}, err
	}
	dst := filepath.Join(dir, sum+SafeExt(name))
	if _, err := os.Stat(dst); err != nil { // dedup: the bytes are already here
		if err := os.Rename(tmp.Name(), dst); err != nil {
			return Ref{}, err
		}
	}
	return Ref{Hash: sum, Name: filepath.Base(name), Size: n, Mime: mime}, nil
}

// SafeExt is the stored extension for a filename: lowercased, bounded, and
// never a path fragment. "" when the name carries nothing usable.
func SafeExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if len(ext) > 12 || strings.ContainsAny(ext, `/\`) {
		return ""
	}
	return ext
}

// BlobPath resolves a hash to its file ("" when absent or malformed). It does
// NOT check domain ownership — callers serving bytes must ask Owns first.
func (s *Store) BlobPath(hash string) string {
	if !ValidHash(hash) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(s.dir, "blobs", hash[:2], hash+"*"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// --- extracts: the text of a blob, computed once per blob -------------------

// ExtractPath is where a blob's extracted text lives. Content-addressed like
// the blob, so re-uploading the same document never re-extracts it.
func (s *Store) ExtractPath(hash string) string {
	if !ValidHash(hash) {
		return ""
	}
	return filepath.Join(s.dir, "extracts", hash+".md")
}

// Extract returns a blob's cached text and whether it was there.
func (s *Store) Extract(hash string) (string, bool) {
	p := s.ExtractPath(hash)
	if p == "" {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// PutExtract caches a blob's text (atomic, so a crash never leaves a half file
// that later reads as the document's contents).
func (s *Store) PutExtract(hash, text string) error {
	p := s.ExtractPath(hash)
	if p == "" {
		return errors.New("bad hash")
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// --- per-domain index: the artifact database AND the access list ------------

func (s *Store) indexPath(domain string) string {
	return filepath.Join(s.dir, "index", domain+".json")
}

// Add records that `domain` owns this ref. Idempotent per (hash, thread,
// sender): re-attaching the same file to the same thread does not grow the
// index, but the same file in a DIFFERENT thread is a separate artifact.
func (s *Store) Add(domain string, e Entry) error {
	if !validDomain(domain) {
		return fmt.Errorf("bad domain %q", domain)
	}
	if !ValidHash(e.Hash) {
		return fmt.Errorf("bad hash")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.load(domain)
	for _, x := range list {
		if x.Hash == e.Hash && x.Thread == e.Thread && x.ByEmail == e.ByEmail {
			return nil
		}
	}
	list = append(list, e)
	return s.save(domain, list)
}

// Owns reports whether a domain may read this hash — the access check that
// makes one pool safe for two businesses.
func (s *Store) Owns(domain, hash string) bool {
	if !validDomain(domain) || !ValidHash(hash) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.load(domain) {
		if e.Hash == hash {
			return true
		}
	}
	return false
}

// Lookup returns a domain's entry for a hash (the newest one when a file was
// attached more than once).
func (s *Store) Lookup(domain, hash string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Entry
	found := false
	for _, e := range s.load(domain) {
		if e.Hash == hash && (!found || e.At.After(out.At)) {
			out, found = e, true
		}
	}
	return out, found
}

// List returns a domain's artifacts, newest first — the per-portal UI's read.
func (s *Store) List(domain string) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.load(domain)
	sort.SliceStable(list, func(i, j int) bool { return list[i].At.After(list[j].At) })
	return list
}

// load reads a domain index; a missing or corrupt file reads as empty rather
// than failing the request (the blobs are still there either way).
func (s *Store) load(domain string) []Entry {
	b, err := os.ReadFile(s.indexPath(domain))
	if err != nil {
		return nil
	}
	var list []Entry
	if json.Unmarshal(b, &list) != nil {
		return nil
	}
	return list
}

func (s *Store) save(domain string, list []Entry) error {
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	p := s.indexPath(domain)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
