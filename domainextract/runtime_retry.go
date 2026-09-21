package domainextract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"manifest/approvals"
)

// RuntimeReconciliation binds the single post-fix child to immutable history.
// The receipt is keyed by original job, so changing the runtime cannot retry again.
type RuntimeReconciliation struct {
	Reconciliation  `json:"reconciliation"`
	ParentAttemptID string `json:"parentAttemptId"`
	ParentHash      string `json:"parentSha256"`
	RuntimeFix      string `json:"runtimeFix"`
}

func runtimeRetryID(parent, runtime, sourceHash string) string {
	return approvals.EvidenceHash("post-runtime-fix-v1\n" + parent + "\n" + runtime + "\n" + sourceHash)
}

func runtimeRetryEligible(j Job) bool {
	switch j.Reason {
	case "bounded execution not verified; owner review required",
		"bounded execution not verified; owner review required: bounded Hermes execution failed",
		"bounded execution not verified; owner review required: Hermes agent initialization denied by filesystem boundary",
		"bounded execution not verified; owner review required: bounded Hermes timeout or cancellation": // a run the 120 s cap cut short (2026-09-21)
	default:
		return false
	}
	return j.Version == 1 && j.State == "uncertain" && !j.Replay && j.Published == 0 && j.Model == "" && j.Execution == nil && j.SpentUSD == 0 && len(j.Candidates) == 0
}

// RetryPostRuntimeFix is an explicit owner operation; it never resets history or
// approves proposals. Execution and publication use the ordinary worker gates.
func (s *Service) RetryPostRuntimeFix(id, source, sourceHash, parentID, authorization, runtime string) (Job, error) {
	var empty Job
	if !evidenceHash.MatchString(id) || !evidenceHash.MatchString(parentID) || !evidenceHash.MatchString(sourceHash) || strings.TrimSpace(authorization) == "" || len(authorization) > 512 || strings.TrimSpace(runtime) == "" || len(runtime) > 512 || strings.ContainsAny(runtime, "\r\n") || s.ap == nil || s.runner == nil || !s.cfg.Aion {
		return empty, fmt.Errorf("exact original job, source/hash, parent attempt, owner authorization and runtime fix required")
	}
	f, err := os.OpenFile(filepath.Join(s.dir, id+".json.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return empty, err
	}
	defer f.Close()
	if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return empty, fmt.Errorf("extraction request busy")
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	original, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return empty, err
	}
	parentRaw, err := os.ReadFile(filepath.Join(s.dir, parentID+".json"))
	if err != nil {
		return empty, err
	}
	var parent Job
	if strict(parentRaw, &parent) != nil || parent.ID != parentID || parent.ParentID != id || parentID != retryID(id) || !s.validIdentity(parent) || !runtimeRetryEligible(parent) || parent.Input.Validate() != nil || parent.Input.Ritual != "aion" || len(parent.Input.Documents) != 1 || parent.Input.Documents[0].Name != source || approvals.EvidenceHash(parent.Input.Documents[0].Text) != sourceHash {
		return empty, fmt.Errorf("parent is not an eligible immutable pre-provider attempt")
	}
	if err = s.runner.ValidateExtractionDuty("aion"); err != nil {
		return empty, err
	}
	fence, release, err := s.acquire("aion")
	if err != nil {
		return empty, err
	}
	held := true
	defer func() {
		if held {
			release()
		}
	}()
	if fence.Revision != parent.OwnershipRevision {
		return empty, fmt.Errorf("ownership revision changed")
	}
	if err = s.fresh(parent.Input); err != nil {
		return empty, err
	}
	if err = s.ap.CheckNoExtractionSource(source); err != nil {
		return empty, err
	}
	attempt := runtimeRetryID(parentID, runtime, sourceHash)
	receiptPath := filepath.Join(s.dir, "runtime-reconciliations", id+".json")
	for _, p := range []string{receiptPath, filepath.Join(s.dir, attempt+".json")} {
		if _, err = os.Stat(p); !os.IsNotExist(err) {
			return empty, fmt.Errorf("post-runtime-fix reconciliation or attempt already exists; inspect durable receipts")
		}
	}
	r := RuntimeReconciliation{Reconciliation: Reconciliation{Version: 1, JobID: id, JobHash: approvals.EvidenceHash(string(original)), Source: source, SourceHash: sourceHash, OwnershipRevision: fence.Revision, Authorization: authorization, AttemptID: attempt, At: time.Now().UTC()}, ParentAttemptID: parentID, ParentHash: approvals.EvidenceHash(string(parentRaw)), RuntimeFix: runtime}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return empty, err
	}
	if err = atomic(receiptPath, raw); err != nil {
		return empty, err
	}
	next := Job{Version: 1, OwnershipRevision: fence.Revision, ID: attempt, ParentID: parentID, Input: parent.Input, State: "queued", Started: time.Now().UTC()}
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

func (s *Service) validRuntimeIdentity(j Job) bool {
	id := j.Input.ID()
	if j.ParentID != retryID(id) || len(j.Input.Documents) != 1 {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "runtime-reconciliations", id+".json"))
	var r RuntimeReconciliation
	if err != nil || strict(raw, &r) != nil || r.Version != 1 || r.Replay || r.JobID != id || r.ParentAttemptID != j.ParentID || r.AttemptID != j.ID || strings.TrimSpace(r.Authorization) == "" || strings.TrimSpace(r.RuntimeFix) == "" || r.OwnershipRevision != j.OwnershipRevision || r.Source != j.Input.Documents[0].Name || r.SourceHash != approvals.EvidenceHash(j.Input.Documents[0].Text) || j.ID != runtimeRetryID(j.ParentID, r.RuntimeFix, r.SourceHash) {
		return false
	}
	original, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil || r.JobHash != approvals.EvidenceHash(string(original)) {
		return false
	}
	parentRaw, err := os.ReadFile(filepath.Join(s.dir, j.ParentID+".json"))
	if err != nil || r.ParentHash != approvals.EvidenceHash(string(parentRaw)) {
		return false
	}
	var parent Job
	if strict(parentRaw, &parent) != nil {
		return false
	}
	want, _ := json.Marshal(parent.Input)
	got, _ := json.Marshal(j.Input)
	return bytes.Equal(want, got) && parent.ID == j.ParentID && parent.ParentID == id && parent.Input.ID() == id && parent.OwnershipRevision == j.OwnershipRevision && runtimeRetryEligible(parent) && s.validIdentity(parent)
}
