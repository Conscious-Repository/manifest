// Package threads is the todo-panel comment store (todo-panel plan D3/D4):
// teamportal's write discipline verbatim — atomic tmp+rename state
// (threads.json) + an append-only activity.log JSONL, one mutex, lenient
// reads — plus content-addressed file blobs (files/<sha[:2]>/<sha><ext>, the
// deliberate CAS seed). A Store instance is just a directory: the private
// store lives in dataDir, the shared real-estate store in realEstate.teamDir,
// and aion todos bypass this package entirely (they comment through the
// existing teamportal store, instantly team-visible).
package threads

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Identity is a thread author — the owner or an agent (id "agent:hermes").
type Identity struct {
	ID   string // stable token: email, "owner", or "agent:<name>"
	Name string // display name
}

// FileRef is one content-addressed attachment on a comment.
type FileRef struct {
	Hash string `json:"hash"` // sha256 hex
	Name string `json:"name"` // original filename
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"`
}

// Comment is one thread entry. Action distinguishes plain comments from the
// structural entries the panel renders inline (assign / plan / fire / result)
// — those are deliberately replayable by a future notifier.
type Comment struct {
	ID         string         `json:"id"`
	TodoID     string         `json:"todo"`
	Action     string         `json:"action"` // comment | assign | plan | fire | result
	Author     string         `json:"author"`
	AuthorName string         `json:"author_name"`
	Text       string         `json:"text"`
	Mentions   []string       `json:"mentions,omitempty"` // roster tokens, structural
	Files      []FileRef      `json:"files,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"` // e.g. {"run": id} for idempotency
	At         time.Time      `json:"at"`
}

// Actions.
const (
	ActComment = "comment"
	ActAssign  = "assign"
	ActPlan    = "plan"
	ActFire    = "fire"
	ActResult  = "result"
)

const maxCommentLen = 8000

// MaxBlobSize caps one attachment (plan D4).
const MaxBlobSize = 25 << 20

type state struct {
	Comments map[string][]Comment `json:"comments"` // todo id → oldest first
}

// Entry is one activity.log line (teamportal's shape).
type Entry struct {
	TS      time.Time      `json:"ts"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Payload map[string]any `json:"payload"`
}

// Store is one thread directory.
type Store struct {
	dir string
	mu  sync.Mutex
}

// New opens (creating if needed) a thread store directory.
func New(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("threads: empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string       { return s.dir }
func (s *Store) statePath() string { return filepath.Join(s.dir, "threads.json") }
func (s *Store) logPath() string   { return filepath.Join(s.dir, "activity.log") }

func (s *Store) read() state {
	st := state{Comments: map[string][]Comment{}}
	if b, err := os.ReadFile(s.statePath()); err == nil {
		_ = json.Unmarshal(b, &st)
		if st.Comments == nil {
			st.Comments = map[string][]Comment{}
		}
	}
	return st
}

// write lands state atomically, THEN the activity line — a crash between the
// two loses only the trail line, never the state (teamportal's ordering).
func (s *Store) write(st state, e Entry) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.statePath()); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Add appends a thread entry. action defaults to comment; either text or
// files must be present for comments, structural actions may be text-only.
func (s *Store) Add(author Identity, todoID, action, text string, mentions []string, files []FileRef, meta map[string]any, now time.Time) (Comment, error) {
	text = strings.TrimSpace(text)
	if action == "" {
		action = ActComment
	}
	if action == ActComment && text == "" && len(files) == 0 {
		return Comment{}, errors.New("empty comment")
	}
	if len(text) > maxCommentLen {
		return Comment{}, fmt.Errorf("comment too long (%d max)", maxCommentLen)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	c := Comment{
		ID:     fmt.Sprintf("c-%d", now.UnixNano()),
		TodoID: todoID, Action: action,
		Author: author.ID, AuthorName: author.Name,
		Text: text, Mentions: mentions, Files: files, Meta: meta,
		At: now.UTC(),
	}
	st.Comments[todoID] = append(st.Comments[todoID], c)
	err := s.write(st, Entry{TS: now.UTC(), Actor: author.ID, Action: action,
		Payload: map[string]any{"todo": todoID, "text": text, "mentions": mentions, "files": len(files), "meta": meta}})
	return c, err
}

// Thread returns a todo's entries, oldest first.
func (s *Store) Thread(todoID string) []Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read().Comments[todoID]
}

// HasAction reports whether an entry with action and meta run id already
// exists — the materialization idempotency check (plan Phase 4).
func (s *Store) HasAction(todoID, action, runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.read().Comments[todoID] {
		if c.Action != action || c.Meta == nil {
			continue
		}
		if r, _ := c.Meta["run"].(string); r == runID {
			return true
		}
	}
	return false
}

// --- content-addressed blobs (plan D4 — the CAS seed) ----------------------

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SaveBlob streams r (capped) into files/<sha[:2]>/<sha><ext>. Identical
// bytes land once — the second save just returns the same ref.
func (s *Store) SaveBlob(r io.Reader, name, mime string) (FileRef, error) {
	tmp, err := os.CreateTemp(s.dir, "blob-*")
	if err != nil {
		return FileRef{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, MaxBlobSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return FileRef{}, err
	}
	if n > MaxBlobSize {
		return FileRef{}, fmt.Errorf("file too large (%dMB max)", MaxBlobSize>>20)
	}
	if n == 0 {
		return FileRef{}, errors.New("empty file")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	ext := strings.ToLower(filepath.Ext(name))
	if len(ext) > 12 || strings.ContainsAny(ext, "/\\") {
		ext = ""
	}
	dir := filepath.Join(s.dir, "files", sum[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return FileRef{}, err
	}
	dst := filepath.Join(dir, sum+ext)
	if _, err := os.Stat(dst); err != nil { // dedup: already stored → skip
		if err := os.Rename(tmp.Name(), dst); err != nil {
			return FileRef{}, err
		}
	}
	return FileRef{Hash: sum, Name: filepath.Base(name), Size: n, Mime: mime}, nil
}

// BlobPath resolves a hash to its stored file ("" when absent or malformed).
func (s *Store) BlobPath(hash string) string {
	if !hashRe.MatchString(hash) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(s.dir, "files", hash[:2], hash+"*"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}
