package transcriptsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"manifest/approvals"
	"manifest/connectorhandoff"
)

// CutoverPlan binds the owner's explicit account to an immutable checkpoint and
// one exact legacy ownership revision. Hashes contain no credentials.
type CutoverPlan struct {
	Version            int    `json:"version"`
	Source             string `json:"source"`
	LegacyRoot         string `json:"legacyRoot"`
	Account            string `json:"account"`
	StagedHash         string `json:"stagedHash"`
	CheckpointHash     string `json:"checkpointHash"`
	ApprovalHash       string `json:"approvalHash"`
	ReconciliationHash string `json:"reconciliationHash"`
	Revision           uint64 `json:"revision"`
}

func (p CutoverPlan) Hash() string {
	b, _ := json.Marshal(p)
	return approvals.EvidenceHash(string(b))
}

type stagedTranscript struct {
	State        State  `json:"state"`
	ApprovalHash string `json:"approvalHash"`
}

func (s *Service) prepareCutover(source, root, hash string, revision uint64) (CutoverPlan, State, error) {
	var p CutoverPlan
	if (source != "granola" && source != "pocket") || !connectorhandoff.ValidHash(hash) || !filepath.IsAbs(root) {
		return p, State{}, fmt.Errorf("transcript source, absolute legacy root and staged hash required")
	}
	c, err := s.config(source)
	if err != nil || c.Enabled || c.Account == "" {
		return p, State{}, fmt.Errorf("explicit account and disabled source required for cutover")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(s.dir), "connector-handoff", "checkpoints", source, hash+".json"))
	if err != nil || approvals.EvidenceHash(string(b)) != hash {
		return p, State{}, fmt.Errorf("staged checkpoint missing or hash mismatch")
	}
	var cp stagedTranscript
	if json.Unmarshal(b, &cp) != nil || cp.State.Account != c.Account {
		return p, State{}, fmt.Errorf("staged account binding mismatch")
	}
	st, approvalHash, err := s.ReconcileCheckpoint(source, root)
	if err != nil {
		return p, State{}, err
	}
	if approvalHash != cp.ApprovalHash || !reflect.DeepEqual(st, cp.State) {
		return p, State{}, fmt.Errorf("current checkpoint or approval evidence differs from staged checkpoint")
	}
	for _, item := range st.Items {
		if item.Replay {
			return p, State{}, fmt.Errorf("checkpoint permits replay")
		}
	}
	_, owner, err := approvals.ReadReconciledConnectorInventory(filepath.Join(root, "artifacts"), source, filepath.Dir(s.dir))
	if err != nil {
		return p, State{}, err
	}
	reconciliation := approvals.EvidenceHash("no owner exception")
	if owner != nil {
		reconciliation = approvals.EvidenceHash(string(approvals.OwnerReconciliationBytes(*owner)))
	}
	p = CutoverPlan{1, source, root, c.Account, hash, st.ImportedFrom, approvalHash, reconciliation, revision}
	return p, st, nil
}

// PrepareCutover is read-only. The returned hash is supplied to Excalibur's
// OS-account-controlled dispatch-owner CLI before ApplyCutover is called.
func (s *Service) PrepareCutover(source, root, hash string) (CutoverPlan, error) {
	r, err := connectorhandoff.FenceSnapshot(root, source)
	if err != nil {
		return CutoverPlan{}, err
	}
	if r.Owner != "excalibur" {
		return CutoverPlan{}, fmt.Errorf("prepare requires Excalibur ownership")
	}
	p, _, err := s.prepareCutover(source, root, hash, r.Revision)
	return p, err
}

// ApplyCutover cannot transfer ownership itself. A matching explicit CLI transfer
// must already have drained and fenced legacy work. Failure leaves legacy fenced.
func (s *Service) ApplyCutover(source, root, hash, expected string, revision uint64) (CutoverPlan, error) {
	r, release, err := connectorhandoff.AcquireFence(root, source)
	if err != nil {
		return CutoverPlan{}, err
	}
	defer release()
	if r.Owner != "manifest" || r.Revision != revision+1 || r.PreviousOwner != "excalibur" || r.Evidence != expected {
		return CutoverPlan{}, fmt.Errorf("matching explicit dispatch-owner transfer required")
	}
	p, st, err := s.prepareCutover(source, root, hash, revision)
	if err != nil {
		return p, err
	}
	if !connectorhandoff.ValidHash(expected) || p.Hash() != expected {
		return p, fmt.Errorf("cutover prepare hash mismatch")
	}
	record := connectorhandoff.Record{Version: 1, Source: source, Phase: connectorhandoff.Enabled, Owner: "manifest", RollbackOwner: "excalibur", CheckpointHash: p.CheckpointHash, ApprovalHash: p.ApprovalHash, Evidence: map[string]string{
		"dispatch-exclusion": p.Hash(), "account-binding": approvals.EvidenceHash(p.Account), "source-reconciliation": p.ReconciliationHash,
	}}
	if err = record.Validate(); err != nil {
		return p, err
	}
	path := filepath.Join(filepath.Dir(s.dir), "connector-handoff", source+".json")
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		return p, fmt.Errorf("existing or unavailable handoff record; inspect before recovery")
	}
	// Publish the cursor first: without the final record no guarded poll can run.
	b, _ := json.MarshalIndent(st, "", "  ")
	if err = publishNew(s.statePath(source), b); err != nil {
		return p, err
	}
	b, _ = json.MarshalIndent(record, "", "  ")
	if err = publishNew(path, b); err != nil {
		return p, fmt.Errorf("cursor published but activation incomplete; inspect before recovery: %w", err)
	}
	return p, nil
}
func publishNew(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cutover-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (s *Service) enterSuccessor(source string) (func(), error) {
	r, err := connectorhandoff.Read(s.handoffDataDir, source)
	if err != nil || (r.Phase != connectorhandoff.Enabled && r.Phase != connectorhandoff.Verified) {
		return nil, fmt.Errorf("transcript handoff evidence required; activation flag is insufficient")
	}
	if s.cfg.LegacyRoot == "" {
		return nil, fmt.Errorf("legacy dispatch fence root required")
	}
	fence, release, err := connectorhandoff.AcquireFence(s.cfg.LegacyRoot, source)
	if err != nil {
		return nil, err
	}
	c, _ := s.config(source)
	st, err := s.read(source)
	if err != nil || fence.Owner != "manifest" || fence.Evidence != r.Evidence["dispatch-exclusion"] || r.Evidence["account-binding"] != approvals.EvidenceHash(c.Account) || st.ImportedFrom != r.CheckpointHash {
		release()
		return nil, fmt.Errorf("successor ownership, account or checkpoint mismatch")
	}
	return release, nil
}

// VerifyContinuity checks durable outcomes and the upstream overlap with GETs
// only. It never writes state, proposals or the vault, and never fetches bodies.
func (s *Service) VerifyContinuity(ctx context.Context, source string) (map[string]int, error) {
	if s.handoffDataDir == "" {
		return nil, fmt.Errorf("guarded service required")
	}
	release, err := s.enterSuccessor(source)
	if err != nil {
		return nil, err
	}
	defer release()
	st, err := s.read(source)
	if err != nil {
		return nil, err
	}
	return s.verifyState(ctx, source, st)
}

// PreviewContinuity performs the same read-only source check before ownership
// transfer, so a credential or API incompatibility cannot strand the duty.
func (s *Service) PreviewContinuity(ctx context.Context, source, root, hash string) (map[string]int, error) {
	p, err := s.PrepareCutover(source, root, hash)
	if err != nil {
		return nil, err
	}
	_, st, err := s.prepareCutover(source, root, hash, p.Revision)
	if err != nil {
		return nil, err
	}
	return s.verifyState(ctx, source, st)
}
func (s *Service) verifyState(ctx context.Context, source string, st State) (map[string]int, error) {
	if s.idx == nil || s.idx.db == nil {
		return nil, fmt.Errorf("source index required")
	}
	inv, owner, err := approvals.ReadReconciledConnectorInventory(filepath.Join(s.cfg.LegacyRoot, "artifacts"), source, s.handoffDataDir)
	if err != nil {
		return nil, err
	}
	for id, prior := range st.Items {
		if err = s.checkReconciledOutcome(source, id, &prior, inv, owner); err != nil {
			return nil, err
		}
	}
	key, err := s.key(source)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{"checkpointItems": len(st.Items), "listed": 0, "known": 0, "new": 0}
	count := func(id string) error {
		if !validID(id) {
			return fmt.Errorf("invalid upstream identity")
		}
		counts["listed"]++
		if _, ok := st.Items[id]; ok {
			counts["known"]++
		} else {
			counts["new"]++
		}
		return nil
	}
	since := st.Watermark.Add(-24 * time.Hour)
	if source == "granola" {
		items, e := s.granola(key).ListNotesSince(ctx, since)
		if e != nil {
			return nil, e
		}
		for _, it := range items {
			if e = count(it.ID); e != nil {
				return nil, e
			}
		}
	} else {
		items, e := s.pocket(key).ListRecordings(ctx, since.UTC().Format("2006-01-02"))
		if e != nil {
			return nil, e
		}
		for _, it := range items {
			if e = count(it.ID); e != nil {
				return nil, e
			}
		}
	}
	return counts, nil
}

// observe uses the original scheduler slots but persists only health timestamps.
// It never advances the watermark or creates proposals in continuity-only mode.
func (s *Service) observe(ctx context.Context, source string) {
	s.locks[source].Lock()
	defer s.locks[source].Unlock()
	release, err := s.enterSuccessor(source)
	if err != nil {
		return
	}
	defer release()
	st, err := s.read(source)
	if err != nil {
		return
	}
	counts, err := s.verifyState(ctx, source, st)
	st.LastAttempt = s.now().UTC()
	st.Error = ""
	if err != nil {
		st.Error = err.Error()
	} else {
		st.LastSuccess = st.LastAttempt
		st.Fetched = counts["listed"]
		st.Skipped = counts["known"]
		st.Waiting = counts["new"]
		st.Filed = 0
	}
	_ = s.save(source, st)
}
