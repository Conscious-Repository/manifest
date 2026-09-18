package domainextract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"manifest/mdfm"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"manifest/approvals"
)

const scratchHold = "bounded execution not verified; owner review required: Hermes scratch unavailable"

// Reconciliation is immutable owner authorization for one new attempt. The
// original uncertain job and its report are never modified. Replay stays false.
type Reconciliation struct {
	Version           int       `json:"version"`
	JobID             string    `json:"jobId"`
	ReportHash        string    `json:"reportSha256"`
	JobHash           string    `json:"jobSha256"`
	Source            string    `json:"source"`
	SourceHash        string    `json:"sourceSha256"`
	OwnershipRevision uint64    `json:"ownershipRevision"`
	Authorization     string    `json:"authorization"`
	AttemptID         string    `json:"attemptId"`
	Replay            bool      `json:"replay"`
	At                time.Time `json:"at"`
}

func retryID(id string) string {
	return approvals.EvidenceHash("pre-provider-scratch-reconciliation-v1\n" + id)
}

// RetryPreProvider is an explicit owner operation, never called by Submit or
// background recovery. One deterministic attempt prevents repeated CLI calls,
// crashes, and simultaneous operators from authorizing additional execution.
func (s *Service) RetryPreProvider(id, source, authorization string) (Job, error) {
	var empty Job
	if !evidenceHash.MatchString(id) || authorization == "" || len(authorization) > 512 || s.ap == nil || s.runner == nil {
		return empty, fmt.Errorf("exact job, source and owner authorization required")
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
	if strict(b, &j) != nil || j.ID != id || j.Input.ID() != id || j.Input.Validate() != nil || len(j.Input.Documents) != 1 || j.Input.Documents[0].Name != source || j.Input.Ritual != "aion" || !s.cfg.Aion || j.Version != 1 || j.Replay || j.State != "uncertain" || j.Reason != scratchHold || j.Model != "" || j.Execution != nil || j.SpentUSD != 0 || len(j.Candidates) != 0 || j.Published != 0 || j.ParentID != "" {
		return empty, fmt.Errorf("job is not an effect-free pre-provider scratch hold")
	}
	if err = s.runner.ValidateExtractionDuty(j.Input.Ritual); err != nil {
		return empty, err
	}
	fence, release, err := s.acquire(j.Input.Ritual)
	if err != nil {
		return empty, err
	}
	// Release before run acquires the same fence; the worker checks revision again.
	held := true
	defer func() {
		if held {
			release()
		}
	}()
	if fence.Revision != j.OwnershipRevision {
		return empty, fmt.Errorf("ownership revision changed")
	}
	if err = s.fresh(j.Input); err != nil {
		return empty, err
	}
	reportPath := filepath.Join(s.harness, "artifacts", "runs", j.Started.Format("2006-01-02")+"-extractor-manifest-"+j.ID[:20]+".md")
	report, reportErr := os.ReadFile(reportPath)
	fm, body := mdfm.Split(string(report))
	if reportErr != nil || fm["outcome"] != "error" || fm["model"] != "" || fm["items_written"] != "0" || fm["charge_spent_usd"] != "0" || fm["request"] != source || fm["executor"] != "manifest" || fm["ritual"] != "aion" || !strings.Contains(body, scratchHold) {
		return empty, fmt.Errorf("original failure report absent or inconsistent")
	}
	if err = s.ap.CheckNoExtractionSource(source); err != nil {
		return empty, err
	}
	attempt := retryID(id)
	receiptPath := filepath.Join(s.dir, "reconciliations", id+".json")
	// Existing authorization is a permanent hold on additional attempts, including
	// a crash between receipt and job creation. Never silently resume/replay.
	for _, p := range []string{receiptPath, filepath.Join(s.dir, attempt+".json")} {
		if _, err = os.Stat(p); !os.IsNotExist(err) {
			return empty, fmt.Errorf("reconciliation or attempt already exists; inspect durable receipts")
		}
	}
	receipt := Reconciliation{Version: 1, ReportHash: approvals.EvidenceHash(string(report)), JobID: id, JobHash: approvals.EvidenceHash(string(b)), Source: source, SourceHash: approvals.EvidenceHash(j.Input.Documents[0].Text), OwnershipRevision: fence.Revision, Authorization: authorization, AttemptID: attempt, At: time.Now().UTC()}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return empty, err
	}
	if err = atomic(receiptPath, raw); err != nil {
		return empty, err
	}
	next := Job{Version: 1, OwnershipRevision: fence.Revision, ID: attempt, ParentID: id, Input: j.Input, State: "queued", Started: time.Now().UTC()}
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

func (s *Service) validIdentity(j Job) bool {
	if j.ParentID == "" {
		return j.ID == j.Input.ID()
	}
	if j.ParentID != j.Input.ID() || j.ID != retryID(j.ParentID) {
		return s.validRuntimeIdentity(j)
	}
	b, err := os.ReadFile(filepath.Join(s.dir, "reconciliations", j.ParentID+".json"))
	var r Reconciliation
	if err != nil || strict(b, &r) != nil || r.Version != 1 || r.Replay || r.JobID != j.ParentID || r.AttemptID != j.ID || r.OwnershipRevision != j.OwnershipRevision || r.Authorization == "" || len(j.Input.Documents) != 1 || r.Source != j.Input.Documents[0].Name || r.SourceHash != approvals.EvidenceHash(j.Input.Documents[0].Text) {
		return false
	}
	original, err := os.ReadFile(filepath.Join(s.dir, j.ParentID+".json"))
	if err != nil || r.JobHash != approvals.EvidenceHash(string(original)) {
		return false
	}
	var parent Job
	if json.Unmarshal(original, &parent) != nil {
		return false
	}
	want, _ := json.Marshal(parent.Input)
	got, _ := json.Marshal(j.Input)
	return bytes.Equal(want, got)
}
