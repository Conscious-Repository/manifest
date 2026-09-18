package domainextract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"manifest/approvals"
)

// contextRebaseHold is the exact reason the pre-provider path leaves behind when
// the recorded domain context no longer matches the live vault. The gate that
// produces it (Service.fresh) is correct and is never weakened.
const contextRebaseHold = "source or domain context changed before publication"

// RebaseReconciliation is immutable owner authorization for one NEW attempt
// rebuilt against the CURRENT domain context. It never edits, deletes or resets
// the original job, its reconciliation, its child attempts, or any receipt.
type RebaseReconciliation struct {
	Version           int       `json:"version"`
	JobID             string    `json:"jobId"`
	JobHash           string    `json:"jobSha256"`
	Source            string    `json:"source"`
	SourceHash        string    `json:"sourceSha256"`
	PriorAttempts     []string  `json:"priorAttemptIds"`
	PriorContextHash  string    `json:"priorContextSha256"`
	NewContextHash    string    `json:"newContextSha256"`
	OwnershipRevision uint64    `json:"ownershipRevision"`
	Authorization     string    `json:"authorization"`
	Replay            bool      `json:"replay"`
	At                time.Time `json:"at"`
}

// AttemptID is derived, never stored: the attempt identity is a pure function of
// the original job, the source hash and the authorized context hash.
func (r RebaseReconciliation) AttemptID() string {
	return rebaseID(r.JobID, r.SourceHash, r.NewContextHash)
}

func rebaseID(id, sourceHash, newContextHash string) string {
	return approvals.EvidenceHash("rebased-context-retry-v1\n" + id + "\n" + sourceHash + "\n" + newContextHash)
}

func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for k := i; k > 0 && v[k] < v[k-1]; k-- {
			v[k], v[k-1] = v[k-1], v[k]
		}
	}
}

func rebaseReceiptPath(dir, id string) string {
	return filepath.Join(dir, "rebase-reconciliations", id+".json")
}

// contextHash binds the recorded domain context bytes for one input.
func contextHash(i Input) string {
	names := make([]string, 0, len(i.Context))
	for n := range i.Context {
		names = append(names, n)
	}
	sortStrings(names)
	h := approvals.EvidenceHash("")
	for _, n := range names {
		h = approvals.EvidenceHash(h + "\n" + n + "\n" + approvals.EvidenceHash(i.Context[n]))
	}
	return h
}

// rebaseEligiblePrior reports whether one prior attempt is an honest,
// effect-free hold that a rebase may supersede.
func rebaseEligiblePrior(j Job) bool {
	if j.Version != 1 || j.Replay || j.State != "uncertain" || j.Model != "" || j.Execution != nil || j.SpentUSD != 0 || len(j.Candidates) != 0 || j.Published != 0 {
		return false
	}
	r := j.Reason
	switch {
	case strings.HasPrefix(r, "bounded execution not verified; owner review required"):
		return true
	case strings.HasPrefix(r, contextRebaseHold):
		return true
	case r == "interrupted execution; owner review required":
		return true
	case r == "interrupted or uncertain execution; owner review required":
		return true
	default:
		return false
	}
}

// rebasedInput re-reads the domain context and source from the live vault, so a
// new attempt carries genuinely current context. It never mutates the original.
func (s *Service) rebasedInput(j Job) (Input, string, error) {
	docs := make([]Document, 0, len(j.Input.Documents))
	for _, d := range j.Input.Documents {
		docs = append(docs, Document{Name: d.Name})
	}
	fresh, err := ReadInput(s.vault, j.Input.Ritual, docs)
	if err != nil {
		return Input{}, "", err
	}
	return fresh, contextHash(fresh), nil
}

// RetryRebased is an explicit owner operation. It authorizes exactly one new
// attempt per (original job, source hash, new context hash). It requires the
// live context to actually differ from what the prior attempts recorded, so it
// can never be used to replay an attempt whose context is unchanged.
func (s *Service) RetryRebased(id, source, sourceHash, authorization string, prior []string) (Job, error) {
	var empty Job
	if !evidenceHash.MatchString(id) || !evidenceHash.MatchString(sourceHash) || strings.TrimSpace(authorization) == "" || len(authorization) > 512 || s.ap == nil || s.runner == nil || len(prior) == 0 || len(prior) > 4 {
		return empty, fmt.Errorf("exact job, source hash, owner authorization and prior attempts required")
	}
	for _, p := range prior {
		if !evidenceHash.MatchString(p) {
			return empty, fmt.Errorf("prior attempt identity required")
		}
	}
	path := filepath.Join(s.dir, id+".json")
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return empty, err
	}
	defer f.Close()
	if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return empty, fmt.Errorf("extraction request busy")
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)

	b, err := os.ReadFile(path)
	if err != nil {
		return empty, err
	}
	var j Job
	if strict(b, &j) != nil || j.ID != id || j.Input.ID() != id || j.Input.Validate() != nil || len(j.Input.Documents) != 1 || j.Input.Documents[0].Name != source || j.Input.Ritual != "aion" || !s.cfg.Aion || j.Version != 1 || j.Replay || j.State != "uncertain" || j.ParentID != "" {
		return empty, fmt.Errorf("job is not a held aion extraction eligible for a rebased attempt")
	}
	if approvals.EvidenceHash(j.Input.Documents[0].Text) != sourceHash {
		return empty, fmt.Errorf("recorded source no longer matches the supplied hash")
	}
	if j.Model != "" || j.Execution != nil || j.SpentUSD != 0 || len(j.Candidates) != 0 || j.Published != 0 {
		return empty, fmt.Errorf("job carries provider or publication effects; rebase refused")
	}
	if !rebaseEligiblePrior(j) {
		return empty, fmt.Errorf("job is not an effect-free hold")
	}
	// Every named prior attempt must exist and be an honest effect-free hold.
	for _, p := range prior {
		raw, err := os.ReadFile(filepath.Join(s.dir, p+".json"))
		if err != nil {
			return empty, fmt.Errorf("prior attempt %s is not present", p[:12])
		}
		var a Job
		if strict(raw, &a) != nil || a.ID != p || a.Input.ID() != id || !rebaseEligiblePrior(a) {
			return empty, fmt.Errorf("prior attempt %s is not an eligible hold", p[:12])
		}
	}
	if err = s.runner.ValidateExtractionDuty(j.Input.Ritual); err != nil {
		return empty, err
	}
	fence, release, err := s.acquire(j.Input.Ritual)
	if err != nil {
		return empty, err
	}
	held := true
	defer func() {
		if held {
			release()
		}
	}()
	if fence.Revision != j.OwnershipRevision {
		return empty, fmt.Errorf("ownership revision changed")
	}
	if err = s.ap.CheckNoExtractionSource(source); err != nil {
		return empty, err
	}
	fresh, newHash, err := s.rebasedInput(j)
	if err != nil {
		return empty, err
	}
	if fresh.Documents[0].Text != j.Input.Documents[0].Text {
		return empty, fmt.Errorf("source changed; rebase is not the correct recovery")
	}
	priorHash := contextHash(j.Input)
	if priorHash == newHash {
		return empty, fmt.Errorf("domain context is unchanged; a rebased attempt is not required")
	}
	attempt := rebaseID(id, sourceHash, newHash)
	receiptPath := rebaseReceiptPath(s.dir, id)
	for _, p := range []string{receiptPath, filepath.Join(s.dir, attempt+".json")} {
		if _, err = os.Stat(p); !os.IsNotExist(err) {
			return empty, fmt.Errorf("rebased reconciliation or attempt already exists; inspect durable receipts")
		}
	}
	if err = os.MkdirAll(filepath.Dir(receiptPath), 0700); err != nil {
		return empty, err
	}
	receipt := RebaseReconciliation{Version: 1, JobID: id, JobHash: approvals.EvidenceHash(string(b)), Source: source, SourceHash: sourceHash, PriorAttempts: prior, PriorContextHash: priorHash, NewContextHash: newHash, OwnershipRevision: fence.Revision, Authorization: authorization, Replay: false, At: time.Now().UTC()}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return empty, err
	}
	if err = atomic(receiptPath, raw); err != nil {
		return empty, err
	}
	next := Job{Version: 1, OwnershipRevision: fence.Revision, ID: attempt, ParentID: id, Input: fresh, State: "queued", Started: time.Now().UTC()}
	if err = s.save(next); err != nil {
		return empty, err
	}
	release()
	held = false
	s.run(filepath.Join(s.dir, attempt+".json"))
	raw, err = os.ReadFile(filepath.Join(s.dir, attempt+".json"))
	if err != nil {
		return empty, err
	}
	err = json.Unmarshal(raw, &next)
	return next, err
}

// validRebaseIdentity proves a rebased attempt is bound to its receipt, its
// original job, and the exact context bytes it was authorized against.
func (s *Service) validRebaseIdentity(j Job) bool {
	if j.ParentID == "" {
		return false
	}
	raw, err := os.ReadFile(rebaseReceiptPath(s.dir, j.ParentID))
	if err != nil {
		return false
	}
	var r RebaseReconciliation
	if strict(raw, &r) != nil || r.Version != 1 || r.Replay || r.JobID != j.ParentID || r.AttemptID() != j.ID || r.OwnershipRevision != j.OwnershipRevision || strings.TrimSpace(r.Authorization) == "" || len(j.Input.Documents) != 1 || r.Source != j.Input.Documents[0].Name || r.SourceHash != approvals.EvidenceHash(j.Input.Documents[0].Text) {
		return false
	}
	original, err := os.ReadFile(filepath.Join(s.dir, j.ParentID+".json"))
	if err != nil || r.JobHash != approvals.EvidenceHash(string(original)) {
		return false
	}
	var parent Job
	if json.Unmarshal(original, &parent) != nil || parent.ID != j.ParentID || parent.Input.ID() != j.ParentID {
		return false
	}
	// The attempt's context must be the authorized NEW context.
	if contextHash(j.Input) != r.NewContextHash {
		return false
	}
	// And it must still match the live vault, or the attempt has gone stale again.
	if err := s.fresh(j.Input); err != nil {
		return false
	}
	return j.ID == rebaseID(j.ParentID, r.SourceHash, r.NewContextHash)
}
