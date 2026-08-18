package realestate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The content-addressed file store (overhaul §3.3): one blob per document
// under system/realestate/files/<sha256>.<ext>, in the vault system zone so
// manifest-sync carries it to both machines. Records reference blobs by
// `sha256:<hex>` URIs — a two-property contract points at ONE blob; nothing
// ever copies a blob per-property. files.json is the operational index
// (hash → original name, mime, size, uploaded date) — one of the two allowed
// stored-derived exceptions (§10). Writes go through the injected
// capability-bound writer (`re-files`) — the store never touches
// os.WriteFile itself.

// FileMeta is one files.json index row.
type FileMeta struct {
	Name     string `json:"name"` // original filename at upload
	Ext      string `json:"ext"`  // blob extension incl. dot (".pdf")
	Mime     string `json:"mime,omitempty"`
	Size     int64  `json:"size"`
	Uploaded string `json:"uploaded"` // YYYY-MM-DD
}

// CASRef is the reference returned to callers ("sha256:<hex>").
type CASRef struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mime    string `json:"mime,omitempty"`
	Existed bool   `json:"existed"` // dedupe hit — the blob was already here
}

// FileStore reads/writes the CAS directory.
type FileStore struct {
	vaultRoot string
	dirRel    string // vault-relative CAS dir (system/realestate/files)
	write     func(rel string, data []byte) error
	mu        sync.Mutex
}

// NewFileStore wires the store; write is the capability-bound vault writer.
func NewFileStore(vaultRoot, reRoot string, write func(rel string, data []byte) error) *FileStore {
	return &FileStore{
		vaultRoot: vaultRoot,
		dirRel:    path.Join(filepath.ToSlash(reRoot), "files"),
		write:     write,
	}
}

var casHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// MaxCASSize caps one upload (matches the thread-store blob cap).
const MaxCASSize = 25 << 20

// Save stores bytes content-addressed. Identical bytes dedupe to the same
// blob (that is the feature); the index remembers the FIRST original name.
func (fs *FileStore) Save(data []byte, origName, mime string, now time.Time) (CASRef, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ext := strings.ToLower(filepath.Ext(origName))
	if len(ext) > 8 || strings.ContainsAny(ext, "/\\") {
		ext = ""
	}
	ref := CASRef{Ref: "sha256:" + hash, Name: origName, Size: int64(len(data)), Mime: mime}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	idx := fs.index()
	if meta, ok := idx[hash]; ok {
		ref.Existed = true
		ref.Name = meta.Name // the first upload's name is canonical
		return ref, nil
	}
	if err := fs.write(path.Join(fs.dirRel, hash+ext), data); err != nil {
		return CASRef{}, err
	}
	idx[hash] = FileMeta{Name: origName, Ext: ext, Mime: mime,
		Size: int64(len(data)), Uploaded: now.Format("2006-01-02")}
	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return CASRef{}, err
	}
	if err := fs.write(path.Join(fs.dirRel, "files.json"), append(out, '\n')); err != nil {
		return CASRef{}, err
	}
	return ref, nil
}

// Lookup resolves a `sha256:<hex>` ref to the blob's absolute path + meta.
func (fs *FileStore) Lookup(ref string) (string, FileMeta, bool) {
	hash := strings.TrimPrefix(strings.TrimSpace(ref), "sha256:")
	if !casHashRe.MatchString(hash) {
		return "", FileMeta{}, false
	}
	meta, ok := fs.index()[hash]
	if !ok {
		return "", FileMeta{}, false
	}
	abs := filepath.Join(fs.vaultRoot, filepath.FromSlash(fs.dirRel), hash+meta.Ext)
	if _, err := os.Stat(abs); err != nil {
		return "", FileMeta{}, false
	}
	return abs, meta, true
}

// Index returns the current hash → meta map (read-only copy).
func (fs *FileStore) Index() map[string]FileMeta { return fs.index() }

func (fs *FileStore) index() map[string]FileMeta {
	idx := map[string]FileMeta{}
	raw, err := os.ReadFile(filepath.Join(fs.vaultRoot, filepath.FromSlash(fs.dirRel), "files.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &idx)
	}
	return idx
}
