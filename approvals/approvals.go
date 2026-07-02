// Package approvals is the human-in-the-loop gate for side-effectful agent work.
// ea-coordinator DRAFTS proposals (never sends); the dashboard materializes them here,
// under <dataDir>/agents/approvals/{pending,approved,rejected}/ (OUTSIDE the vault).
// Confirm/Reject only RECORD the human decision (a folder move) — the app itself never
// sends, pays, or acts. The status is the folder the file lives in.
package approvals

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/mdfm"
)

// Proposal is one drafted action awaiting the user's decision.
type Proposal struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	Agent   string `json:"agent"`
	Created string `json:"created"` // RFC3339
	Status  string `json:"status"`  // pending|approved|rejected (= folder)
	Body    string `json:"body"`
}

var statuses = []string{"pending", "approved", "rejected"}

// Store is the approvals directory.
type Store struct{ dir string }

// NewStore roots the store at <agentsDir>/approvals and creates its subfolders.
func NewStore(agentsDir string) *Store {
	dir := filepath.Join(agentsDir, "approvals")
	for _, st := range statuses {
		_ = os.MkdirAll(filepath.Join(dir, st), 0o700)
	}
	return &Store{dir: dir}
}

// List returns proposals in a status folder, oldest-first.
func (s *Store) List(status string) []Proposal {
	entries, _ := os.ReadDir(filepath.Join(s.dir, status))
	var out []Proposal
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if p, err := s.parse(filepath.Join(s.dir, status, e.Name())); err == nil {
			p.Status = status
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

// Counts returns the number of proposals per status (for the sub-tab badge).
func (s *Store) Counts() map[string]int {
	c := map[string]int{}
	for _, st := range statuses {
		c[st] = len(s.List(st))
	}
	return c
}

// Propose writes a new pending proposal (dedupe by id — same action+body won't double up).
func (s *Store) Propose(p Proposal) (Proposal, error) {
	if strings.TrimSpace(p.Action) == "" {
		return Proposal{}, errors.New("proposal action is required")
	}
	if p.Created == "" {
		p.Created = time.Now().UTC().Format(time.RFC3339)
	}
	if p.ID == "" {
		p.ID = proposalID(p)
	}
	p.Status = "pending"
	dest := filepath.Join(s.dir, "pending", p.ID+".md")
	if _, err := os.Stat(dest); err == nil {
		return p, nil // already pending — dedupe
	}
	if err := os.WriteFile(dest, []byte(serialize(p)), 0o644); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// Confirm records approval: pending → approved. It does NOT send/execute anything.
func (s *Store) Confirm(id string) error { return s.move(id, "approved", "") }

// Reject records rejection (with an optional reason appended): pending → rejected.
func (s *Store) Reject(id, reason string) error { return s.move(id, "rejected", reason) }

// Materialize parses ea-coordinator's proposed actions (the last fenced JSON array of
// {action, body}) into pending proposals. Returns the newly created ones.
func (s *Store) Materialize(raw, agent string, now time.Time) ([]Proposal, error) {
	arr, ok := mdfm.ExtractJSONArray(raw)
	if !ok {
		return nil, nil
	}
	var raws []struct {
		Action string `json:"action"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(arr), &raws); err != nil {
		return nil, err
	}
	var created []Proposal
	for _, r := range raws {
		if strings.TrimSpace(r.Action) == "" {
			continue
		}
		p := Proposal{Action: strings.TrimSpace(r.Action), Body: r.Body, Agent: agent,
			Created: now.UTC().Format(time.RFC3339)}
		p.ID = proposalID(p)
		if _, err := os.Stat(filepath.Join(s.dir, "pending", p.ID+".md")); err == nil {
			continue // already pending
		}
		// Skip if already decided (approved/rejected) so re-runs don't resurrect it.
		if s.decidedElsewhere(p.ID) {
			continue
		}
		saved, err := s.Propose(p)
		if err != nil {
			return created, err
		}
		created = append(created, saved)
	}
	return created, nil
}

// ---- internals ----

func (s *Store) decidedElsewhere(id string) bool {
	for _, st := range []string{"approved", "rejected"} {
		if _, err := os.Stat(filepath.Join(s.dir, st, id+".md")); err == nil {
			return true
		}
	}
	return false
}

func (s *Store) move(id, to, reason string) error {
	src := filepath.Join(s.dir, "pending", id+".md")
	p, err := s.parse(src)
	if err != nil {
		return err
	}
	if reason != "" {
		p.Body = strings.TrimRight(p.Body, "\n") + "\n\n> rejected: " + reason
	}
	p.Status = to
	dest := filepath.Join(s.dir, to, id+".md")
	if err := os.WriteFile(dest, []byte(serialize(p)), 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

func (s *Store) parse(path string) (Proposal, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Proposal{}, err
	}
	fm, body := mdfm.Split(string(b))
	return Proposal{
		ID:      fm["id"],
		Action:  fm["action"],
		Agent:   fm["agent"],
		Created: fm["created"],
		Body:    strings.TrimSpace(body),
	}, nil
}

func serialize(p Proposal) string {
	return (&mdfm.Writer{}).
		SetRaw("type", "approval").
		Set("id", p.ID).
		Set("action", p.Action).
		Set("agent", p.Agent).
		Set("created", p.Created).
		String(strings.TrimSpace(p.Body))
}

func proposalID(p Proposal) string {
	h := sha1.Sum([]byte(strings.ToLower(p.Action + "|" + p.Body)))
	return hex.EncodeToString(h[:])[:12]
}
