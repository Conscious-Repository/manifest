// Package errands is the action layer's record + executor (errands-aside
// plan): an errand is a natural-language web task executed by the Aside
// browser through its CLI. Manifest supplies intent, authorization, execution
// records, and receipts; Aside supplies the hands (its own logins, its own
// password manager, its own ask-gates). Operational state, NEVER the vault:
// one JSON record per errand under <dataDir>/errands/ with the full
// stdout/stderr transcript beside it — the records ARE the receipts the FEED
// renders (attention kind "receipt", permanent lifecycle).
//
// Probed reality this code is built against (§0, 2026-07-30, aside
// 1.26.717.1619): exec streams ONLY under a pty (pipes: silence); success
// exit 0, flag/account errors exit 1; ANSI human transcript, no json mode;
// NO session id is emitted (so no continue affordance — retry re-enqueues);
// session modes are per-task in the app with Guard the default and no CLI
// flag (guard-always holds structurally).
package errands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
)

// idSeq disambiguates same-second enqueues (see Enqueue).
var idSeq atomic.Uint64

// Status is an errand's lifecycle state. Terminal states never change.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Record is one errand — the receipt. Stored as <dataDir>/errands/<id>.json.
type Record struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	Account    string `json:"account"`
	Source     string `json:"source"` // "user" | "proposal"
	ApprovalID string `json:"approvalId,omitempty"`
	GoalID     string `json:"goalId,omitempty"`
	ReadOnly   bool   `json:"readOnly,omitempty"` // reserved: no CLI mode flag exists (§0)
	Created    string `json:"created"`
	Started    string `json:"started,omitempty"`
	Finished   string `json:"finished,omitempty"`
	Status     Status `json:"status"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	Outcome    string `json:"outcome,omitempty"` // one line, ANSI-stripped (card summary)
	Transcript string `json:"transcript,omitempty"`
	// Acknowledged is READ-state, not a verdict: a cleared receipt leaves the
	// inbox and the badge but the record + transcript stay forever (the §5
	// permanent-audit-trail rule) — visible under the feed's ALL view.
	Acknowledged bool `json:"acknowledged,omitempty"`
}

// Store owns <dataDir>/errands/: one JSON record + one transcript per errand
// (the feed.Store per-item-file shape with portals' JSON encoding).
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "errands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id string) string           { return filepath.Join(s.dir, id+".json") }
func (s *Store) TranscriptPath(id string) string { return filepath.Join(s.dir, id+".transcript.txt") }

// Save writes one record (whole-file, small).
func (s *Store) Save(r *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(r.ID), b, 0o644)
}

// Get loads one record by id.
func (s *Store) Get(id string) (*Record, error) {
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Ack marks a TERMINAL errand acknowledged (cleared from the inbox; the
// record persists). Queued/running errands can't be cleared — cancel them.
func (s *Store) Ack(id string) error {
	r, err := s.Get(id)
	if err != nil {
		return err
	}
	switch r.Status {
	case StatusDone, StatusFailed, StatusCancelled:
		r.Acknowledged = true
		return s.Save(r)
	}
	return errors.New("only a finished errand clears — cancel it first")
}

// List returns every errand, newest-created first.
func (s *Store) List() []*Record {
	ents, _ := os.ReadDir(s.dir)
	var out []*Record
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if r, err := s.Get(strings.TrimSuffix(e.Name(), ".json")); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out
}

// ansiRe strips terminal escapes for the one-line outcome (transcripts keep
// the raw bytes — they are the audit trail).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\r`)

// OutcomeLine derives the card summary: the last non-empty ANSI-stripped line.
func OutcomeLine(transcript []byte) string {
	lines := strings.Split(ansiRe.ReplaceAllString(string(transcript), ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// Executor runs errands SERIALLY (one at a time — errands-aside §3/§8)
// through the aside CLI under a pty, streaming to the transcript, killing at
// the timeout, SIGTERM on cancel.
type Executor struct {
	store   *Store
	timeout time.Duration
	bin     string // "aside", overridable for tests

	mu      sync.Mutex
	queue   []string // errand ids, FIFO
	running string   // id currently executing ("" when idle)
	cancels map[string]func()
	ptys    map[string]*os.File // running errand → pty master (mid-run answers)
	wake    chan struct{}
	// OnChange, when set, fires after every state transition (badge refresh).
	OnChange func()
}

// NewExecutor builds the serial executor. timeoutMinutes <= 0 → 15 (§6).
func NewExecutor(store *Store, timeoutMinutes int) *Executor {
	if timeoutMinutes <= 0 {
		timeoutMinutes = 15
	}
	return &Executor{
		store: store, timeout: time.Duration(timeoutMinutes) * time.Minute,
		bin: "aside", cancels: map[string]func(){}, ptys: map[string]*os.File{},
		wake: make(chan struct{}, 1),
	}
}

// SetBinary overrides the CLI path (tests use a stub script).
func (e *Executor) SetBinary(bin string) { e.bin = bin }

// Start recovers orphans (a record still queued/running belongs to a dead
// process — a restart killed its child; mark it failed, transcript kept),
// then launches the run loop.
func (e *Executor) Start() {
	for _, r := range e.store.List() {
		if r.Status == StatusQueued || r.Status == StatusRunning {
			r.Status = StatusFailed
			if r.Outcome == "" {
				r.Outcome = "interrupted by a dashboard restart — transcript kept"
			}
			r.Finished = time.Now().UTC().Format(time.RFC3339)
			_ = e.store.Save(r)
		}
	}
	go e.loop()
}

// SendInput writes a line into a RUNNING errand's pty — the mid-run answer
// path for Aside's ask-gates (the agent's question streams into the
// transcript; the reply goes back the same wire; pty echo records it there).
func (e *Executor) SendInput(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("nothing to send")
	}
	e.mu.Lock()
	f, ok := e.ptys[id]
	e.mu.Unlock()
	if !ok {
		return errors.New("errand is not running")
	}
	_, err := f.Write([]byte(text + "\r"))
	return err
}

// Enqueue validates and queues one errand (the ONLY way work starts —
// explicit user submit or an approved run-errand proposal, §4).
func (e *Executor) Enqueue(text, account, source, approvalID, goalID string) (*Record, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("an errand needs task text")
	}
	if strings.TrimSpace(account) == "" {
		return nil, errors.New("an errand needs an --account (pick one)")
	}
	now := time.Now().UTC()
	// id = second-stamp + process-monotonic sequence: two Enqueues in the same
	// millisecond must never collide (a collision silently merges two records).
	r := &Record{
		ID:   now.Format("20060102-150405") + "-" + fmt.Sprintf("%04d", idSeq.Add(1)%10000),
		Text: text, Account: account, Source: source,
		ApprovalID: approvalID, GoalID: goalID,
		Created: now.Format(time.RFC3339), Status: StatusQueued,
	}
	if err := e.store.Save(r); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.queue = append(e.queue, r.ID)
	e.mu.Unlock()
	e.poke()
	return r, nil
}

// Cancel SIGTERMs a running errand or drops a queued one.
func (e *Executor) Cancel(id string) error {
	e.mu.Lock()
	if cancel, ok := e.cancels[id]; ok {
		e.mu.Unlock()
		cancel()
		return nil
	}
	for i, qid := range e.queue {
		if qid == id {
			e.queue = append(e.queue[:i], e.queue[i+1:]...)
			e.mu.Unlock()
			if r, err := e.store.Get(id); err == nil && r.Status == StatusQueued {
				r.Status = StatusCancelled
				r.Finished = time.Now().UTC().Format(time.RFC3339)
				_ = e.store.Save(r)
				e.changed()
			}
			return nil
		}
	}
	e.mu.Unlock()
	return errors.New("errand is not queued or running")
}

// QueuePosition returns 1-based place in line (0 = not queued).
func (e *Executor) QueuePosition(id string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, qid := range e.queue {
		if qid == id {
			return i + 1
		}
	}
	return 0
}

func (e *Executor) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Executor) changed() {
	if e.OnChange != nil {
		e.OnChange()
	}
}

func (e *Executor) loop() {
	for range e.wake {
		for {
			e.mu.Lock()
			if len(e.queue) == 0 {
				e.mu.Unlock()
				break
			}
			id := e.queue[0]
			e.queue = e.queue[1:]
			e.running = id
			e.mu.Unlock()
			e.runOne(id)
			e.mu.Lock()
			e.running = ""
			e.mu.Unlock()
		}
	}
}

// runOne executes a single errand under a pty (§0: pipes get silence).
func (e *Executor) runOne(id string) {
	r, err := e.store.Get(id)
	if err != nil || r.Status != StatusQueued {
		return
	}
	r.Status = StatusRunning
	r.Started = time.Now().UTC().Format(time.RFC3339)
	r.Transcript = filepath.Base(e.store.TranscriptPath(id))
	_ = e.store.Save(r)
	e.changed()

	tf, err := os.OpenFile(e.store.TranscriptPath(id), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		e.finish(r, nil, "failed opening transcript: "+err.Error())
		return
	}
	defer tf.Close()

	// guard mode is the app's per-task default; the CLI has no mode flag (§0)
	cmd := exec.Command(e.bin, "--account", r.Account, r.Text)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		e.finish(r, nil, "failed starting aside: "+err.Error())
		return
	}
	cancelled := false
	timedOut := false
	e.mu.Lock()
	e.cancels[id] = func() { cancelled = true; _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Process.Kill() }
	e.ptys[id] = ptmx
	e.mu.Unlock()
	timer := time.AfterFunc(e.timeout, func() { timedOut = true; _ = cmd.Process.Kill() })

	buf := make([]byte, 4096)
	var tail []byte // last chunk window for the outcome line
	for {
		n, rerr := ptmx.Read(buf)
		if n > 0 {
			_, _ = tf.Write(buf[:n])
			tail = append(tail, buf[:n]...)
			if len(tail) > 8192 {
				tail = tail[len(tail)-8192:]
			}
		}
		if rerr != nil {
			break // EIO on child exit is the pty's EOF
		}
	}
	werr := cmd.Wait()
	timer.Stop()
	e.mu.Lock()
	delete(e.cancels, id)
	delete(e.ptys, id)
	e.mu.Unlock()
	_ = ptmx.Close()

	code := 0
	if werr != nil {
		if ee := (&exec.ExitError{}); errors.As(werr, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	r.ExitCode = &code
	r.Outcome = OutcomeLine(tail)
	switch {
	case cancelled:
		r.Status = StatusCancelled
	case timedOut:
		r.Status = StatusFailed
		r.Outcome = "timed out after " + e.timeout.String() + " — transcript kept"
	case code == 0:
		r.Status = StatusDone
	default:
		r.Status = StatusFailed
	}
	r.Finished = time.Now().UTC().Format(time.RFC3339)
	_ = e.store.Save(r)
	e.changed()
}

// finish records a pre-exec failure.
func (e *Executor) finish(r *Record, code *int, outcome string) {
	r.Status = StatusFailed
	r.ExitCode = code
	r.Outcome = outcome
	r.Finished = time.Now().UTC().Format(time.RFC3339)
	_ = e.store.Save(r)
	e.changed()
}

// CLIPresent reports whether the aside binary is on PATH.
func CLIPresent() bool { _, err := exec.LookPath("aside"); return err == nil }

// Accounts shells `aside account list` (works without a pty, §0) and parses
// the probed shape: `* u0  email  signed in  profiles: …` (+ provider line).
type Account struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Current bool   `json:"current"`
}

var accountRe = regexp.MustCompile(`^(\*?)\s*(u?\d+)\s+(\S+)`)

func Accounts() ([]Account, error) {
	out, err := exec.Command("aside", "account", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("aside account list: %v — %s", err, strings.TrimSpace(string(out)))
	}
	return parseAccounts(string(out)), nil
}

func parseAccounts(s string) []Account {
	var accs []Account
	for _, ln := range strings.Split(s, "\n") {
		if m := accountRe.FindStringSubmatch(strings.TrimRight(ln, "\r")); m != nil {
			accs = append(accs, Account{ID: m[2], Label: m[3], Current: m[1] == "*"})
		}
	}
	return accs
}
