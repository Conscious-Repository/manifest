// Package olgachat is Liber: the assistant in Olga's personal Manifest.
//
// A conversation is a JSON file. App conversations and chats about her own
// tasks live under system/olga/chat; a conversation about a shared Home task
// lives with the shared Home data (system/home/chat) so Benjamin's Manifest can
// show it. Readers ignore fields they don't know (schema "v": 1).
//
// What Olga's browser receives is Thread.Public(): the turns and cards. Model
// names, routing numbers and session ids stay in Thread.Server and the log,
// never in a response to her (plan §3.5).
package olgachat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

// Thread kinds.
const (
	KindApp  = "app"
	KindTask = "task"
)

// Turn statuses (a Liber turn).
const (
	StatusThinking = "thinking" // the voice is answering
	StatusWorking  = "working"  // the builder is making a change
	StatusDone     = ""
	StatusFailed   = "failed"
)

// Card kinds and states.
const (
	CardProposal = "proposal" // a task/plan change she can apply (task chat)
	CardConfirm  = "confirm"  // "Make this change" (app chat)
	CardChange   = "change"   // a built change: preview / use / not this / undo
	CardNote     = "note"     // needs Benjamin

	StatePending   = "pending"
	StateApplied   = "applied"
	StateDeclined  = "declined"
	StateConflict  = "conflict"
	StateFailed    = "failed"
	StateBuilding  = "building"
	StateReady     = "ready"
	StateDeploying = "deploying"
	StateLive      = "live"
	StateDiscarded = "discarded"
	StateUndone    = "undone"
)

type Thread struct {
	V         int       `json:"v"`
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	TaskID    string    `json:"taskId,omitempty"`
	TaskTitle string    `json:"taskTitle,omitempty"`
	Shared    bool      `json:"shared,omitempty"` // a Home task: Benjamin can read it
	Title     string    `json:"title,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Turns     []Turn    `json:"turns"`
	Server    *Server   `json:"server,omitempty"`
}

type Turn struct {
	ID     string    `json:"id"`
	Who    string    `json:"who"` // olga | liber
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	Status string    `json:"status,omitempty"`
	Cards  []Card    `json:"cards,omitempty"`
	// Queued: an Olga message written while a turn was running; it is
	// answered when that turn ends.
	Queued bool `json:"queued,omitempty"`
}

type Card struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	State    string    `json:"state"`
	Summary  string    `json:"summary"`
	Detail   string    `json:"detail,omitempty"`
	Proposal *Proposal `json:"proposal,omitempty"`
	ChangeID string    `json:"changeId,omitempty"`
	Message  string    `json:"message,omitempty"` // what happened, in plain words
	Updated  time.Time `json:"updated"`
}

// Proposal is a change to her tasks or the Home plan, applied only when she
// taps the card, through the same handlers the app uses.
type Proposal struct {
	Kind     string          `json:"kind"` // task.add | task.update | task.note | plan.patch
	ID       string          `json:"id,omitempty"`
	Text     string          `json:"text,omitempty"`
	Area     string          `json:"area,omitempty"`
	Priority *string         `json:"priority,omitempty"`
	Append   string          `json:"append,omitempty"`
	Patch    json.RawMessage `json:"patch,omitempty"`
	Revision string          `json:"revision,omitempty"` // plan revision the card was built from
}

// Server holds what only the server and Benjamin's view may see.
type Server struct {
	Changes map[string]*Change `json:"changes,omitempty"`
	// TurnModels: the model behind each Liber turn, for Benjamin's read-only view.
	TurnModels map[string]string `json:"turnModels,omitempty"`
}

// Change is one app change and its builder session.
type Change struct {
	ID        string    `json:"id"`
	Brief     string    `json:"brief"`
	Session   string    `json:"session,omitempty"`
	Worktree  string    `json:"worktree,omitempty"`
	Commit    string    `json:"commit,omitempty"`
	Files     []string  `json:"files,omitempty"`
	State     string    `json:"state"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	LiveUntil time.Time `json:"liveUntil,omitempty"` // undo window end
}

// Public is the thread as Olga's browser receives it.
func (t *Thread) Public() *Thread {
	c := *t
	c.Server = nil
	c.Turns = append([]Turn(nil), t.Turns...)
	return &c
}

// Busy reports whether a Liber turn is running.
func (t *Thread) Busy() bool {
	for _, tu := range t.Turns {
		if tu.Who == "liber" && (tu.Status == StatusThinking || tu.Status == StatusWorking) {
			return true
		}
	}
	return false
}

// Card finds a card by id.
func (t *Thread) Card(id string) (*Turn, *Card) {
	for i := range t.Turns {
		for j := range t.Turns[i].Cards {
			if t.Turns[i].Cards[j].ID == id {
				return &t.Turns[i], &t.Turns[i].Cards[j]
			}
		}
	}
	return nil, nil
}

// Turn finds a turn by id.
func (t *Thread) Turn(id string) *Turn {
	for i := range t.Turns {
		if t.Turns[i].ID == id {
			return &t.Turns[i]
		}
	}
	return nil
}

// NewID is a short random id.
func NewID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// ---- storage ----

// WriteFunc writes a vault file through the capability that owns its zone.
type WriteFunc func(abs string, data []byte) error

// Store reads and writes thread files.
type Store struct {
	Private string // abs: <vault>/system/olga/chat
	Shared  string // abs: <vault>/system/home/chat
	Write   WriteFunc
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// TaskKey turns a task id into a file-name-safe key.
func TaskKey(taskID string) string {
	k := unsafeName.ReplaceAllStringFunc(taskID, func(s string) string { return "_" + hex.EncodeToString([]byte(s)) })
	if len(k) > 160 {
		k = k[:160]
	}
	return k
}

var threadIDRe = regexp.MustCompile(`^[a-z0-9-]{3,80}$`)

// ValidID reports whether an id can name an app thread file.
func ValidID(id string) bool { return threadIDRe.MatchString(id) }

func (s *Store) appPath(id string) string { return filepath.Join(s.Private, id+".json") }

func (s *Store) taskPath(taskID string, shared bool) string {
	if shared {
		return filepath.Join(s.Shared, TaskKey(taskID)+".json")
	}
	return filepath.Join(s.Private, "task-"+TaskKey(taskID)+".json")
}

// PathFor is where a thread lives.
func (s *Store) PathFor(t *Thread) string {
	if t.Kind == KindTask {
		return s.taskPath(t.TaskID, t.Shared)
	}
	return s.appPath(t.ID)
}

func read(path string) (*Thread, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Thread
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if t.V > SchemaVersion {
		return nil, errors.New("conversation was written by a newer version")
	}
	return &t, nil
}

// App loads an app thread.
func (s *Store) App(id string) (*Thread, error) {
	if !ValidID(id) {
		return nil, os.ErrNotExist
	}
	return read(s.appPath(id))
}

// Task loads the thread about a task (nil, nil when none yet).
func (s *Store) Task(taskID string, shared bool) (*Thread, error) {
	t, err := read(s.taskPath(taskID, shared))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return t, err
}

// Save writes a thread (atomically, through the capability).
func (s *Store) Save(t *Thread) error {
	t.V = SchemaVersion
	t.Updated = time.Now().UTC()
	b, err := json.MarshalIndent(t, "", " ")
	if err != nil {
		return err
	}
	return s.Write(s.PathFor(t), append(b, '\n'))
}

// List is every thread, newest first (app threads and task threads).
func (s *Store) List() []*Thread {
	var out []*Thread
	for _, dir := range []string{s.Private, s.Shared} {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if t, err := read(filepath.Join(dir, e.Name())); err == nil && len(t.Turns) > 0 {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}
