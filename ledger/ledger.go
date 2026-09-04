// Package ledger is the daily shared thread (persona plan Phase 0): one JSONL
// file per owner-day mirroring every foreground turn and background
// checkpoint. Append-only, tier-3 derived — every line is a projection of
// state that lives elsewhere (thread stores, chat sessions, run reports), so
// the ledger can always be rebuilt and never needs the tmp+rename discipline:
// there is no state file, only the log.
//
// Kind vocabulary (closed for now): thread.comment, thread.assign,
// thread.plan, thread.fire, thread.result, thread.questions, chat.user,
// chat.assistant, run.completed, run.failed, plan.materialized,
// plan.replanned. Hidden thread markers are never ledgered.
//
// Object scoping (P0 Phase 1): every entry is ABOUT one entity — a task, a
// chat session, a run, later a decision or an artifact. Writers tag it as
// Entry.Object; the legacy Task/Session/Run fields stay as related refs and,
// on lines written before the tag existed, still resolve to an object through
// Entry.Objects — so the whole history stays queryable without rewriting a
// single line. Reads (Events, History) scan the day files in order: there is
// still no index, no derived table, no state file. Only the log.
package ledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Object kinds — the entities an event can be about. Open-ended by design
// (the aion triad adds decision/heuristic; P1 adds artifact); these are the
// ones the writers stamp today.
const (
	ObjTask     = "task"     // composite todo id (inbox/x, aion:123, re:...)
	ObjSession  = "session"  // chat session / agent-chat thread id
	ObjRun      = "run"      // harness run id
	ObjFeed     = "feed"     // feed card id (digs)
	ObjJob      = "job"      // a transient server job (recruiting scaffold)
	ObjDecision = "decision" // reserved: decision ledger (P3)
	ObjArtifact = "artifact" // reserved: artifact model (P1)
)

// Object is the entity an entry is about.
type Object struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// IsZero reports an unset object (both halves empty count as unset).
func (o Object) IsZero() bool { return o.Kind == "" && o.ID == "" }

// String renders kind:id — the wire form the read API accepts back.
func (o Object) String() string {
	if o.IsZero() {
		return ""
	}
	return o.Kind + ":" + o.ID
}

// Entry is one ledger line.
type Entry struct {
	TS      time.Time      `json:"ts"`                // UTC
	Source  string         `json:"source"`            // "thread" | "chat" | "run" | "plan"
	Kind    string         `json:"kind"`              // see the package vocabulary
	Actor   string         `json:"actor"`             // "owner" | "agent:hermes" | "system" | spirit name
	Object  Object         `json:"object,omitzero"`   // the entity this event is about (P0 Phase 1)
	Task    string         `json:"todo,omitempty"`    // composite todo id
	Session string         `json:"session,omitempty"` // chat session id
	Run     string         `json:"run,omitempty"`     // run id
	Harness string         `json:"harness,omitempty"`
	Text    string         `json:"text,omitempty"` // first ~280 chars — the ledger reads as prose
	Ref     string         `json:"ref,omitempty"`  // harness-relative artifact ref or vault-relative path
	Meta    map[string]any `json:"meta,omitempty"`
}

// Objects lists every entity this entry references: the explicit Object
// first, then the legacy fields as derived refs (task, session, run), each at
// most once. A line written before the tag existed still names its object.
func (e Entry) Objects() []Object {
	var out []Object
	add := func(o Object) {
		if o.IsZero() || o.ID == "" {
			return
		}
		for _, have := range out {
			if have == o {
				return
			}
		}
		out = append(out, o)
	}
	add(e.Object)
	add(Object{Kind: ObjTask, ID: e.Task})
	add(Object{Kind: ObjSession, ID: e.Session})
	add(Object{Kind: ObjRun, ID: e.Run})
	return out
}

// DerivedObject is the object an untagged entry is about, by precedence: the
// task it concerns, else its chat session, else its run. Append stamps it so
// every new line carries the tag even when the writer didn't set one.
func (e Entry) DerivedObject() Object {
	if !e.Object.IsZero() {
		return e.Object
	}
	for _, o := range e.Objects() {
		return o
	}
	return Object{}
}

// About reports whether the entry references the object; an empty kind
// matches any kind with that id.
func (e Entry) About(kind, id string) bool {
	if id == "" {
		return false
	}
	for _, o := range e.Objects() {
		if o.ID == id && (kind == "" || o.Kind == kind) {
			return true
		}
	}
	return false
}

// Store is one ledger directory: <dir>/<YYYY-MM-DD>.jsonl in loc's days.
type Store struct {
	dir string
	loc *time.Location
	mu  sync.Mutex
}

// New opens (creating if needed) a ledger directory.
func New(dir string, loc *time.Location) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("ledger: empty dir")
	}
	if loc == nil {
		loc = time.Local
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, loc: loc}, nil
}

// Dir is the ledger directory (the sweep cursor lives beside the day files).
func (s *Store) Dir() string { return s.dir }

// Loc is the owner's timezone — the calendar the day files are bucketed in.
func (s *Store) Loc() *time.Location { return s.loc }

// Today is the current date in the owner's timezone.
func (s *Store) Today() string { return time.Now().In(s.loc).Format("2006-01-02") }

func validDate(d string) bool {
	_, err := time.Parse("2006-01-02", d)
	return err == nil
}

// Append writes one line into the entry's local-day file. O_APPEND under one
// mutex — a single process writes, and a torn last line (crash mid-write) is
// simply skipped by the lenient reader.
func (s *Store) Append(e Entry) error {
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	e.TS = e.TS.UTC()
	if e.Object.IsZero() {
		e.Object = e.DerivedObject()
	}
	day := e.TS.In(s.loc).Format("2006-01-02")
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, day+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// heal a torn last line (crash mid-append): if the file doesn't end in a
	// newline, start on a fresh one so the fragment stays its own skipped line
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 && b[len(b)-1] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// Day reads one date's entries, in file order. Unparsable lines are skipped.
func (s *Store) Day(date string) ([]Entry, error) {
	if !validDate(date) {
		return nil, errors.New("ledger: bad date")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readDay(date)
}

// readDay is Day under the caller's lock.
func (s *Store) readDay(date string) ([]Entry, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, date+".jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	out := []Entry{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if json.Unmarshal([]byte(line), &e) == nil && e.Kind != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// Query narrows an Events read. Every set field must match; Since is
// inclusive and Until exclusive (a half-open window, so a cursor at the last
// entry seen re-reads exactly from it). Limit > 0 keeps the LAST n matches —
// the most recent — still in file order.
type Query struct {
	Kinds      []string  // any of these exact kinds
	Source     string    // "thread" | "chat" | "run" | "plan"
	Actor      string    // exact actor id
	Object     string    // object id — explicit tag or legacy task/session/run ref
	ObjectKind string    // narrows Object to one kind; "" matches any kind
	Since      time.Time // zero = open
	Until      time.Time // zero = open
	Limit      int
}

func (q Query) matches(e Entry) bool {
	if len(q.Kinds) > 0 {
		hit := false
		for _, k := range q.Kinds {
			if e.Kind == k {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if q.Source != "" && e.Source != q.Source {
		return false
	}
	if q.Actor != "" && e.Actor != q.Actor {
		return false
	}
	if q.Object != "" && !e.About(q.ObjectKind, q.Object) {
		return false
	}
	if q.ObjectKind != "" && q.Object == "" {
		hit := false
		for _, o := range e.Objects() {
			if o.Kind == q.ObjectKind {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if !q.Since.IsZero() && e.TS.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && !e.TS.Before(q.Until) {
		return false
	}
	return true
}

// Events reads every entry matching q across the day files, oldest day first
// and in file order within a day. It is a scan, not an index: the day files
// remain the only truth, and days outside the Since/Until window (in the
// store's local calendar) are never opened.
func (s *Store) Events(q Query) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from, to := "", ""
	if !q.Since.IsZero() {
		from = q.Since.In(s.loc).Format("2006-01-02")
	}
	if !q.Until.IsZero() {
		to = q.Until.In(s.loc).Format("2006-01-02")
	}
	out := []Entry{}
	for _, day := range s.days() {
		if (from != "" && day < from) || (to != "" && day > to) {
			continue
		}
		es, err := s.readDay(day)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			if q.matches(e) {
				out = append(out, e)
			}
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out, nil
}

// History is one object's reconstructed event history: the entries in order
// plus the shape a resume needs at a glance — when it began, when it last
// moved, who touched it, which kinds of event it saw.
type History struct {
	Object  Object    `json:"object"`
	Entries []Entry   `json:"entries"`
	First   time.Time `json:"first,omitzero"`
	Last    time.Time `json:"last,omitzero"`
	Actors  []string  `json:"actors"` // distinct, first-seen order
	Kinds   []string  `json:"kinds"`  // distinct, first-seen order
}

// History reconstructs one object's ordered event history — every entry that
// names it explicitly or through a legacy ref. Kind "" matches any kind.
func (s *Store) History(kind, id string) (History, error) {
	h := History{Object: Object{Kind: kind, ID: id}, Entries: []Entry{}, Actors: []string{}, Kinds: []string{}}
	if strings.TrimSpace(id) == "" {
		return h, errors.New("ledger: empty object id")
	}
	es, err := s.Events(Query{Object: id, ObjectKind: kind})
	if err != nil {
		return h, err
	}
	h.Entries = es
	seenActor, seenKind := map[string]bool{}, map[string]bool{}
	for i, e := range es {
		if i == 0 {
			h.First = e.TS
		}
		h.Last = e.TS
		if e.Actor != "" && !seenActor[e.Actor] {
			seenActor[e.Actor] = true
			h.Actors = append(h.Actors, e.Actor)
		}
		if !seenKind[e.Kind] {
			seenKind[e.Kind] = true
			h.Kinds = append(h.Kinds, e.Kind)
		}
	}
	return h, nil
}

// Days lists the dates with a ledger file, sorted ascending.
func (s *Store) Days() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.days()
}

// days is Days under the caller's lock.
func (s *Store) days() []string {
	entries, _ := os.ReadDir(s.dir)
	var out []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") && validDate(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Snip truncates s to ~n chars on a rune boundary — ledger Text discipline.
func Snip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
