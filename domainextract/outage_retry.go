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

// LAB-OUTAGE RETRY (2026-10-06). The lab Sparks went unreachable around
// 2026-10-02 and every aion job since landed "uncertain" without reaching a
// model. The other owner operations do not fit such a job: pre-provider wants
// the scratch hold, post-runtime-fix a failed retry as its parent, rebased a
// changed context. This one authorizes exactly ONE new attempt per (original
// job, source hash) for a first attempt that never reached a model — no reply,
// no candidates, no spend, a receipt (if any) that never completed — and it
// only QUEUES that attempt. The running worker routes it like any job: to the
// lab when it answers, to the owner's stand-in only when the notes are
// cleared to leave the lab, otherwise it waits.

// outageReceipt names the receipt file, keyed by the original job.
func (s *Service) outageReceipt(id string) string {
	return filepath.Join(s.dir, "reconciliations", id+".outage.json")
}

func outageRetryID(id, sourceHash string) string {
	return approvals.EvidenceHash("lab-outage-retry-v1\n" + id + "\n" + sourceHash)
}

// outageRetryEligible: a first attempt that failed before any model answered.
func outageRetryEligible(j Job) bool {
	if j.Version != 1 || j.State != "uncertain" || j.Replay || j.ParentID != "" || j.Published != 0 || len(j.Candidates) != 0 || j.SpentUSD != 0 {
		return false
	}
	if !strings.HasPrefix(j.Reason, "bounded execution not verified; owner review required") {
		return false
	}
	return j.Execution == nil || (!j.Execution.Completed && j.Execution.Status == 0 && j.Execution.ResponseModel == "")
}

// RetryAfterOutage is an explicit owner operation: it queues one attempt and
// returns it; it never runs a model, resets history or approves anything.
func (s *Service) RetryAfterOutage(id, source, sourceHash, authorization string) (Job, error) {
	var empty Job
	if !evidenceHash.MatchString(id) || !evidenceHash.MatchString(sourceHash) || strings.TrimSpace(authorization) == "" || len(authorization) > 512 || s.ap == nil || !s.cfg.Aion {
		return empty, fmt.Errorf("exact original job, source hash and owner authorization required")
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
	raw, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return empty, err
	}
	var j Job
	if strict(raw, &j) != nil || j.ID != id || j.Input.ID() != id || j.Input.Validate() != nil || j.Input.Ritual != "aion" || len(j.Input.Documents) != 1 || j.Input.Documents[0].Name != source {
		return empty, fmt.Errorf("job is not a single-source aion extraction for that source")
	}
	if !outageRetryEligible(j) {
		return empty, fmt.Errorf("job is not a first attempt that failed before any model answered")
	}
	if approvals.EvidenceHash(j.Input.Documents[0].Text) != sourceHash {
		return empty, fmt.Errorf("recorded source no longer matches the supplied hash")
	}
	if err = s.fresh(j.Input); err != nil {
		return empty, fmt.Errorf("the note or its context changed since; submit it anew instead")
	}
	childID := outageRetryID(id, sourceHash)
	if _, err = os.Stat(s.outageReceipt(id)); err == nil {
		return empty, fmt.Errorf("an outage retry for this job already exists; inspect its receipt")
	}
	if _, err = os.Stat(filepath.Join(s.dir, childID+".json")); err == nil {
		return empty, fmt.Errorf("the retry attempt already exists")
	}
	fence, release, err := s.acquire(j.Input.Ritual)
	if err != nil {
		return empty, err
	}
	release()
	receipt := Reconciliation{Version: 1, JobID: id, JobHash: approvals.EvidenceHash(string(raw)), Source: source, SourceHash: sourceHash,
		OwnershipRevision: fence.Revision, Authorization: authorization, AttemptID: childID, At: time.Now().UTC()}
	b, err := json.Marshal(receipt)
	if err != nil {
		return empty, err
	}
	if err = atomic(s.outageReceipt(id), b); err != nil {
		return empty, err
	}
	child := Job{Version: 1, OwnershipRevision: fence.Revision, ID: childID, ParentID: id, Input: j.Input, State: "queued", Started: time.Now().UTC()}
	if err = s.save(child); err != nil {
		return empty, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return child, nil
}

// validOutageIdentity binds an outage retry to its receipt and an unchanged
// original.
func (s *Service) validOutageIdentity(j Job) bool {
	b, err := os.ReadFile(s.outageReceipt(j.ParentID))
	var r Reconciliation
	if err != nil || strict(b, &r) != nil || r.Version != 1 || r.Replay || r.JobID != j.ParentID || r.AttemptID != j.ID || r.OwnershipRevision != j.OwnershipRevision || r.Authorization == "" || len(j.Input.Documents) != 1 || r.Source != j.Input.Documents[0].Name || r.SourceHash != approvals.EvidenceHash(j.Input.Documents[0].Text) || j.ID != outageRetryID(j.ParentID, r.SourceHash) {
		return false
	}
	original, err := os.ReadFile(filepath.Join(s.dir, j.ParentID+".json"))
	if err != nil || r.JobHash != approvals.EvidenceHash(string(original)) {
		return false
	}
	var parent Job
	if json.Unmarshal(original, &parent) != nil || parent.ID != j.ParentID {
		return false
	}
	want, _ := json.Marshal(parent.Input)
	got, _ := json.Marshal(j.Input)
	return bytes.Equal(want, got)
}
