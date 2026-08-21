// Package chatthreads is the AION portal's native chat-with-kairos store
// (chat-kairos handoff). Threads are SHARED portal objects — the whole team
// reads and posts to all of them, every message attributed — not per-person
// sessions. The runtime is poll-based (cmd/kairos-runner): a message that asks
// kairos spools a run order, the runner writes a report + brief, and a sweep
// ingests the answer as a kairos message. There is no token stream.
//
// Write discipline is threads/teamportal's verbatim: atomic tmp+rename of
// chat.json (state lands first) then an append-only activity.log JSONL line,
// under one mutex, lenient reads.
package chatthreads

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Identity is a message author — a person (email) or the agent ("agent:kairos").
type Identity struct {
	ID   string // email, or "agent:kairos"
	Name string
}

// Proposal is one change a delegate turn suggests; a person applies it. Type is
// "set-field" (an item field patch) or "replace-section" (a plan/description
// section swap). State transitions pending → applied|discarded.
type Proposal struct {
	Verb    string `json:"verb"`             // human label: "set due", "replace ## plan"
	Type    string `json:"type"`             // set-field | replace-section
	ItemID  string `json:"item"`             // aion item id the change targets
	Target  string `json:"target,omitempty"` // display target (item id / plan file path)
	Section string `json:"section,omitempty"`
	Field   string `json:"field,omitempty"`
	Value   string `json:"value,omitempty"`
	Body    string `json:"body,omitempty"` // full proposed section body
	Was     string `json:"was,omitempty"`  // prior state, for the diff line
	State   string `json:"state"`          // pending | applied | discarded
	By      string `json:"by,omitempty"`   // who decided
	At      time.Time `json:"at,omitempty"`
}

// Message is one thread entry — a person ask or a kairos turn.
type Message struct {
	ID       string     `json:"id"`
	Thread   string     `json:"thread"`
	Kind     string     `json:"kind"` // ask | kairos | system
	Author   string     `json:"author"`
	AuthName string     `json:"author_name"`
	Text     string     `json:"text"`
	Context  []string   `json:"context,omitempty"` // structural ids that rode the order
	At       time.Time  `json:"at"`
	Ritual   string     `json:"ritual,omitempty"`  // ask | delegate (kairos turns)
	Outcome  string     `json:"outcome,omitempty"` // completed | failed
	Elapsed  string     `json:"elapsed,omitempty"`
	Run      string     `json:"run,omitempty"`
	Report   string     `json:"report,omitempty"` // harness-relative report path
	Brief    string     `json:"brief,omitempty"`  // harness-relative brief path
	Props    []Proposal `json:"proposals,omitempty"`
	// Files are the attachments that rode this message. Refs only — the bytes
	// live in the artifact pool, and chat.json is rewritten whole on every
	// message, so nothing large may live here.
	Files []FileRef `json:"files,omitempty"`
}

// FileRef is one attachment as this thread sees it: the pool key plus the name
// THIS sender used for it.
type FileRef struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
	Mime string `json:"mime,omitempty"`
}

// Thread is a named, rock-scoped, archivable conversation.
type Thread struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Rock     string    `json:"rock,omitempty"` // goal/rock id, "" = whole vault
	Archived bool      `json:"archived,omitempty"`
	Created  time.Time `json:"created"`
	By       string    `json:"by,omitempty"`
}

// Pending is an in-flight run — the queue-attribution record, cleared on ingest.
type Pending struct {
	OrderID string    `json:"order"`
	Thread  string    `json:"thread"`
	By      string    `json:"by"`
	ByEmail string    `json:"by_email"`
	Ritual  string    `json:"ritual"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

const maxChatLen = 6000

type state struct {
	Threads  []Thread             `json:"threads"`
	Messages map[string][]Message `json:"messages"`
	Pending  []Pending            `json:"pending"`
}

// Entry is one activity.log line (teamportal's shape).
type Entry struct {
	TS      time.Time      `json:"ts"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Payload map[string]any `json:"payload"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("chatthreads: empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) statePath() string { return filepath.Join(s.dir, "chat.json") }
func (s *Store) logPath() string   { return filepath.Join(s.dir, "activity.log") }

func (s *Store) read() state {
	st := state{Messages: map[string][]Message{}}
	if b, err := os.ReadFile(s.statePath()); err == nil {
		_ = json.Unmarshal(b, &st)
		if st.Messages == nil {
			st.Messages = map[string][]Message{}
		}
	}
	return st
}

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

// --- threads ---------------------------------------------------------------

func threadIndex(st state, id string) int {
	for i := range st.Threads {
		if st.Threads[i].ID == id {
			return i
		}
	}
	return -1
}

// CreateThread adds a thread (idempotent on id — returns the existing one).
func (s *Store) CreateThread(id, title, rock string, by Identity, now time.Time) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	if i := threadIndex(st, id); i >= 0 {
		return st.Threads[i], nil
	}
	t := Thread{ID: id, Title: title, Rock: rock, Created: now.UTC(), By: by.ID}
	st.Threads = append(st.Threads, t)
	err := s.write(st, Entry{TS: now.UTC(), Actor: by.ID, Action: "thread-create",
		Payload: map[string]any{"thread": id, "title": title, "rock": rock}})
	return t, err
}

// PatchThread merges thread fields (title/rock/archived) — the rename/rescope/
// archive/reopen path.
func (s *Store) PatchThread(id string, fields map[string]any, actor Identity, now time.Time) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	i := threadIndex(st, id)
	if i < 0 {
		return Thread{}, errors.New("thread not found")
	}
	if v, ok := fields["title"].(string); ok {
		st.Threads[i].Title = v
	}
	if v, ok := fields["rock"]; ok {
		st.Threads[i].Rock, _ = v.(string)
	}
	if v, ok := fields["archived"].(bool); ok {
		st.Threads[i].Archived = v
	}
	err := s.write(st, Entry{TS: now.UTC(), Actor: actor.ID, Action: "thread-patch",
		Payload: map[string]any{"thread": id, "fields": fields}})
	return st.Threads[i], err
}

// Threads returns every thread, oldest first.
func (s *Store) Threads() []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Thread(nil), s.read().Threads...)
}

// Messages returns a thread's messages, oldest first.
func (s *Store) Messages(threadID string) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.read().Messages[threadID]...)
}

// AddMessage appends a message to a thread.
func (s *Store) AddMessage(m Message, now time.Time) (Message, error) {
	if strings.TrimSpace(m.Text) == "" && len(m.Props) == 0 {
		return Message{}, errors.New("empty message")
	}
	if len(m.Text) > maxChatLen {
		m.Text = m.Text[:maxChatLen]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	if m.ID == "" {
		m.ID = fmt.Sprintf("m-%d", now.UnixNano())
	}
	if m.At.IsZero() {
		m.At = now.UTC()
	}
	st.Messages[m.Thread] = append(st.Messages[m.Thread], m)
	err := s.write(st, Entry{TS: now.UTC(), Actor: m.Author, Action: "message",
		Payload: map[string]any{"thread": m.Thread, "kind": m.Kind, "run": m.Run}})
	return m, err
}

// HasRun reports whether a kairos run is already ingested (idempotent sweep).
func (s *Store) HasRun(threadID, run string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.read().Messages[threadID] {
		if m.Run != "" && m.Run == run {
			return true
		}
	}
	return false
}

// PruneEmpty discards untitled/empty threads (the false-start rule) except
// keepID. Returns the dropped ids.
func (s *Store) PruneEmpty(keepID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	var kept []Thread
	var dropped []string
	changed := false
	for _, t := range st.Threads {
		empty := len(st.Messages[t.ID]) == 0 && (t.Title == "" || t.Title == "untitled")
		if empty && t.ID != keepID {
			dropped = append(dropped, t.ID)
			delete(st.Messages, t.ID)
			changed = true
			continue
		}
		kept = append(kept, t)
	}
	if !changed {
		return nil
	}
	st.Threads = kept
	_ = s.write(st, Entry{TS: time.Now().UTC(), Actor: "system", Action: "thread-prune",
		Payload: map[string]any{"dropped": dropped}})
	return dropped
}

// --- pending runs (queue attribution) --------------------------------------

// SetPending records an in-flight run.
func (s *Store) SetPending(p Pending, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	st.Pending = append(st.Pending, p)
	return s.write(st, Entry{TS: now.UTC(), Actor: p.ByEmail, Action: "spooled",
		Payload: map[string]any{"thread": p.Thread, "order": p.OrderID, "ritual": p.Ritual}})
}

// ClearPending removes a pending entry by order id.
func (s *Store) ClearPending(orderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	var kept []Pending
	changed := false
	for _, p := range st.Pending {
		if p.OrderID == orderID {
			changed = true
			continue
		}
		kept = append(kept, p)
	}
	if !changed {
		return
	}
	st.Pending = kept
	_ = s.write(st, Entry{TS: time.Now().UTC(), Actor: "system", Action: "cleared",
		Payload: map[string]any{"order": orderID}})
}

// Pending returns the in-flight runs, oldest first.
func (s *Store) Pending() []Pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := append([]Pending(nil), s.read().Pending...)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].At.Before(ps[j].At) })
	return ps
}

// --- proposals -------------------------------------------------------------

// Proposal reads one proposal (read-only) so the caller can gate + apply the
// change before recording the verdict.
func (s *Store) Proposal(threadID, msgID string, idx int) (Proposal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.read().Messages[threadID] {
		if m.ID == msgID && idx >= 0 && idx < len(m.Props) {
			return m.Props[idx], true
		}
	}
	return Proposal{}, false
}

// DecideProposal transitions one proposal on a message to applied|discarded.
// Returns the proposal (for the caller to apply the change) — the caller writes
// through the real item/plan paths, then this records the verdict.
func (s *Store) DecideProposal(threadID, msgID string, idx int, apply bool, by Identity, now time.Time) (Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	msgs := st.Messages[threadID]
	for i := range msgs {
		if msgs[i].ID != msgID {
			continue
		}
		if idx < 0 || idx >= len(msgs[i].Props) {
			return Proposal{}, errors.New("proposal index out of range")
		}
		p := &msgs[i].Props[idx]
		if p.State != "pending" {
			return *p, errors.New("proposal already decided")
		}
		if apply {
			p.State = "applied"
		} else {
			p.State = "discarded"
		}
		p.By = by.ID
		p.At = now.UTC()
		out := *p
		err := s.write(st, Entry{TS: now.UTC(), Actor: by.ID, Action: "proposal-" + p.State,
			Payload: map[string]any{"thread": threadID, "msg": msgID, "type": p.Type, "item": p.ItemID}})
		return out, err
	}
	return Proposal{}, errors.New("message not found")
}
