package gmailsync

// The pending email-candidate store: one directory under the OODA portal's
// teamDir (<teamDir>/email). Deliberately its OWN store rather than a lane of
// teamportal.Store.Ext — Ext rides the flat /api/team/state read every member
// gets, and pending candidates are visibility-restricted (source member +
// admin only, owner decision 2026-08-21), the portal's first deliberate break
// from the flat-reads doctrine.
//
// Write discipline is the house standard (threads/teamportal/chatthreads):
// state.json atomic tmp+rename FIRST, then one JSONL line in activity.log,
// under one mutex, with lenient reads.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Candidate statuses. The lifecycle is absent → pending → confirmed |
// dismissed. Dismissed mutes the thread forever (the engine's rejection
// rule). After confirm, later messages spawn a NEW candidate carrying only
// the fresh messages — the append lane, as a fresh candidate.
const (
	StatusPending   = "pending"
	StatusConfirmed = "confirmed"
	StatusDismissed = "dismissed"
)

// Candidate is one synced email thread awaiting (or past) the member's call.
type Candidate struct {
	ID           string    `json:"id"`
	Account      string    `json:"account"` // the member's email (lowercased)
	ThreadID     string    `json:"threadId"`
	Subject      string    `json:"subject"`
	Participants []string  `json:"participants,omitempty"` // resolved roster names
	FirstMsgAt   time.Time `json:"firstMsgAt"`
	LastMsgAt    time.Time `json:"lastMsgAt"`
	LastMsgID    string    `json:"lastMsgId"`
	Note         string    `json:"note"`     // rendered markdown (ThreadNote)
	Filename     string    `json:"filename"` // suggested artifact name (NoteFilename)
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	DecidedAt    time.Time `json:"decidedAt,omitempty"`
	DecidedBy    string    `json:"decidedBy,omitempty"`
	// ArtifactHash ties a confirmed candidate to its artifacts-store entry.
	ArtifactHash string `json:"artifactHash,omitempty"`
	// SpoolPending marks a confirmed candidate whose extractor spool was
	// refused (engine busy) — the sync loop retries it.
	SpoolPending bool `json:"spoolPending,omitempty"`
	// Seq numbers the candidates of one thread: 1 = the original, 2+ = growth
	// after a confirm (fresh messages only).
	Seq int `json:"seq,omitempty"`
}

// CandidateID is the store's id convention — the approvals-store shape
// (sha1, 12 hex) over the dedupe key. Seq keeps a post-confirm growth
// candidate distinct from its confirmed ancestor.
func CandidateID(account, threadID string, seq int) string {
	key := strings.ToLower(account + "|" + threadID)
	if seq > 1 {
		key += "|" + fmt.Sprint(seq)
	}
	h := sha1.Sum([]byte(key))
	return hex.EncodeToString(h[:])[:12]
}

type candidateState struct {
	Candidates map[string]*Candidate `json:"candidates"` // id → candidate
	// Watermarks: account → RFC3339 of the newest message seen (never rewinds).
	Watermarks map[string]string `json:"watermarks"`
}

// Candidates is the store.
type Candidates struct {
	dir string
	mu  sync.Mutex
}

// NewCandidates roots the store, creating the directory.
func NewCandidates(dir string) (*Candidates, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Candidates{dir: dir}, nil
}

func (c *Candidates) load() candidateState {
	st := candidateState{Candidates: map[string]*Candidate{}, Watermarks: map[string]string{}}
	b, err := os.ReadFile(filepath.Join(c.dir, "state.json"))
	if err == nil {
		_ = json.Unmarshal(b, &st) // lenient — a torn read starts fresh
		if st.Candidates == nil {
			st.Candidates = map[string]*Candidate{}
		}
		if st.Watermarks == nil {
			st.Watermarks = map[string]string{}
		}
	}
	return st
}

// write lands state first (atomic tmp+rename), then the activity line — a
// crash loses only the trail line, never the state.
func (c *Candidates) write(st candidateState, actor, action string, payload map[string]any) error {
	b, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(c.dir, "state.json.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(c.dir, "state.json")); err != nil {
		return err
	}
	line, _ := json.Marshal(map[string]any{
		"ts": time.Now().UTC().Format(time.RFC3339), "actor": actor, "action": action, "payload": payload,
	})
	f, err := os.OpenFile(filepath.Join(c.dir, "activity.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil // the state landed; a lost trail line is acceptable
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
	return nil
}

// Watermark returns the account's high-water mark (zero time when new).
func (c *Candidates) Watermark(account string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	if s, ok := st.Watermarks[strings.ToLower(account)]; ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// AdvanceWatermark records the newest message time seen for an account. It
// never rewinds.
func (c *Candidates) AdvanceWatermark(account string, t time.Time) {
	if t.IsZero() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	key := strings.ToLower(account)
	if cur, ok := st.Watermarks[key]; ok {
		if ct, err := time.Parse(time.RFC3339, cur); err == nil && !t.After(ct) {
			return
		}
	}
	st.Watermarks[key] = t.UTC().Format(time.RFC3339)
	_ = c.write(st, "sync", "watermark", map[string]any{"account": key, "at": st.Watermarks[key]})
}

// ThreadState reports the latest candidate for a thread (highest Seq) so the
// sync loop can decide: absent → propose; pending → re-render; dismissed →
// mute; confirmed → propose growth beyond LastMsgID as a new candidate.
func (c *Candidates) ThreadState(account, threadID string) (*Candidate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	var latest *Candidate
	for _, cand := range st.Candidates {
		if cand.Account == strings.ToLower(account) && cand.ThreadID == threadID {
			if latest == nil || cand.Seq > latest.Seq {
				latest = cand
			}
		}
	}
	if latest == nil {
		return nil, false
	}
	cp := *latest
	return &cp, true
}

// Upsert writes a pending candidate (new, or re-rendered in place while
// still pending). No-op if that id is already decided.
func (c *Candidates) Upsert(cand Candidate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	cand.Account = strings.ToLower(cand.Account)
	if cand.Seq == 0 {
		cand.Seq = 1
	}
	if cand.ID == "" {
		cand.ID = CandidateID(cand.Account, cand.ThreadID, cand.Seq)
	}
	if prev, ok := st.Candidates[cand.ID]; ok {
		if prev.Status != StatusPending {
			return nil // decided — never resurrect
		}
		cand.CreatedAt = prev.CreatedAt
	} else if cand.CreatedAt.IsZero() {
		cand.CreatedAt = time.Now().UTC()
	}
	cand.Status = StatusPending
	st.Candidates[cand.ID] = &cand
	return c.write(st, "sync", "upsert", map[string]any{"id": cand.ID, "account": cand.Account, "subject": cand.Subject})
}

// Decide marks a pending candidate confirmed or dismissed. Returns the
// decided candidate. Idempotence: deciding an already-decided candidate the
// same way succeeds silently; the other way errors.
func (c *Candidates) Decide(id, by string, confirm bool, artifactHash string, spoolPending bool) (*Candidate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	cand, ok := st.Candidates[id]
	if !ok {
		return nil, fmt.Errorf("no candidate %s", id)
	}
	want := StatusDismissed
	if confirm {
		want = StatusConfirmed
	}
	if cand.Status != StatusPending {
		if cand.Status == want {
			cp := *cand
			return &cp, nil
		}
		return nil, fmt.Errorf("candidate %s already %s", id, cand.Status)
	}
	cand.Status = want
	cand.DecidedAt = time.Now().UTC()
	cand.DecidedBy = strings.ToLower(by)
	cand.ArtifactHash = artifactHash
	cand.SpoolPending = spoolPending
	if err := c.write(st, cand.DecidedBy, "decide", map[string]any{"id": id, "status": cand.Status}); err != nil {
		return nil, err
	}
	cp := *cand
	return &cp, nil
}

// ClearSpoolPending records that a confirmed candidate's extractor spool
// finally went through.
func (c *Candidates) ClearSpoolPending(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	if cand, ok := st.Candidates[id]; ok && cand.SpoolPending {
		cand.SpoolPending = false
		_ = c.write(st, "sync", "spooled", map[string]any{"id": id})
	}
}

// Get returns one candidate by id.
func (c *Candidates) Get(id string) (*Candidate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	cand, ok := st.Candidates[id]
	if !ok {
		return nil, false
	}
	cp := *cand
	return &cp, true
}

// List returns candidates filtered by status ("" = all), newest first.
func (c *Candidates) List(status string) []Candidate {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.load()
	var out []Candidate
	for _, cand := range st.Candidates {
		if status != "" && cand.Status != status {
			continue
		}
		out = append(out, *cand)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastMsgAt.After(out[j].LastMsgAt) })
	return out
}

// SpoolRetries returns confirmed candidates still awaiting their extractor
// spool.
func (c *Candidates) SpoolRetries() []Candidate {
	var out []Candidate
	for _, cand := range c.List(StatusConfirmed) {
		if cand.SpoolPending {
			out = append(out, cand)
		}
	}
	return out
}
