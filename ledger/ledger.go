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

// Entry is one ledger line.
type Entry struct {
	TS      time.Time      `json:"ts"`                // UTC
	Source  string         `json:"source"`            // "thread" | "chat" | "run" | "plan"
	Kind    string         `json:"kind"`              // see the package vocabulary
	Actor   string         `json:"actor"`             // "owner" | "agent:hermes" | "system" | spirit name
	Task    string         `json:"todo,omitempty"`    // composite todo id
	Session string         `json:"session,omitempty"` // chat session id
	Run     string         `json:"run,omitempty"`     // run id
	Harness string         `json:"harness,omitempty"`
	Text    string         `json:"text,omitempty"` // first ~280 chars — the ledger reads as prose
	Ref     string         `json:"ref,omitempty"`  // harness-relative artifact ref or vault-relative path
	Meta    map[string]any `json:"meta,omitempty"`
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

// Days lists the dates with a ledger file, sorted ascending.
func (s *Store) Days() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
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
