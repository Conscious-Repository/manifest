package personalemail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/gmailauth"
)

var extraStateName = regexp.MustCompile(`^state-[a-z0-9]+(?:-[a-z0-9]+)*\.json$`)

type Plan struct {
	Version        int    `json:"version"`
	Revision       uint64 `json:"revision"`
	ConfigHash     string `json:"configHash"`
	BindingHash    string `json:"bindingHash"`
	LegacyHash     string `json:"legacyHash"`
	ApprovalHash   string `json:"approvalHash"`
	QuarantineHash string `json:"quarantineHash"`
}

func (p Plan) Hash() string { b, _ := json.Marshal(p); return approvals.EvidenceHash(string(b)) }
func (s *Service) prepare(revision uint64) (Plan, State, error) {
	var p Plan
	var st State
	if s.Config.Enabled {
		return p, st, fmt.Errorf("cutover requires disabled worker config")
	}
	binding, err := s.validate()
	if err != nil {
		return p, st, err
	}
	extras, err := filepath.Glob(filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state-*.json"))
	if err != nil {
		return p, st, err
	}
	expected := map[string]bool{}
	for _, c := range s.Config.ExtraAccounts {
		expected[filepath.Join(s.Config.LegacyRoot, "vessel/state/email", gmailauth.ExtraStateFilename(c.Account))] = true
	}
	var orphans []connectorhandoff.EmailOrphanReceipt
	for _, path := range extras {
		if expected[path] {
			delete(expected, path)
			continue
		}
		// The slug is only a filename convention, never an account attribution.
		if !extraStateName.MatchString(filepath.Base(path)) || filepath.Base(path) == gmailauth.ExtraStateFilename(s.Config.Account) {
			return p, st, fmt.Errorf("ambiguous extra legacy state identity")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return p, st, fmt.Errorf("invalid orphan state file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return p, st, err
		}
		orphan, err := connectorhandoff.QuarantineEmailOrphan(path, raw)
		if err != nil {
			return p, st, err
		}
		orphans = append(orphans, orphan)
	}
	if len(expected) != 0 {
		return p, st, fmt.Errorf("extra legacy state coverage mismatch")
	}
	raw, err := os.ReadFile(filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state.json"))
	if err != nil {
		return p, st, err
	}
	inv, err := approvals.ReadEmailContinuityInventory(filepath.Join(s.Config.LegacyRoot, "artifacts"))
	if err != nil {
		return p, st, err
	}
	receipt, err := connectorhandoff.ReconcileEmailContinuity(raw, s.Config.Account, inv)
	if err != nil {
		return p, st, err
	}
	b, _ := json.Marshal(receipt)
	p = Plan{1, revision, s.Config.hash(), approvals.EvidenceHash(binding + "\x00" + s.Config.Account), receipt.LegacyHash, receipt.ApprovalHash, approvals.EvidenceHash(string(b))}
	st = State{Activation: p, Version: 1, PlanHash: p.Hash(), Revision: revision + 1, ConfigHash: p.ConfigHash, BindingHash: p.BindingHash, Watermark: receipt.Watermark, Receipt: receipt, Threads: map[string]Thread{}}
	primary, err := importMailbox(raw, s.Config.Account, inv)
	if err != nil {
		return p, st, err
	}
	st.Threads = primary.Threads
	st.Orphans = orphans
	st.ExtraAccounts = map[string]MailboxState{}
	for _, c := range s.Config.ExtraAccounts {
		path := filepath.Join(s.Config.LegacyRoot, "vessel/state/email", gmailauth.ExtraStateFilename(c.Account))
		b, err := os.ReadFile(path)
		if err != nil {
			return p, st, err
		}
		m, err := importMailbox(b, c.Account, inv)
		if err != nil {
			return p, st, err
		}
		// Preserve the primary handoff's permanent quarantine across accounts too;
		// discovering the extra state is not permission to repair/replay those rows.
		for i, r := range m.Receipt.Threads {
			if prior, ok := st.Threads[r.ThreadID]; ok && prior.Status == approvals.ReconciledUncertain {
				t := m.Threads[r.ThreadID]
				t.Status = approvals.ReconciledUncertain
				m.Threads[r.ThreadID] = t
				m.Receipt.Threads[i].Disposition = approvals.ReconciledUncertain
				m.Receipt.Threads[i].StopReasons = append(m.Receipt.Threads[i].StopReasons, "primary-handoff-quarantine")
				m.Receipt.IdentityComplete = false
			}
		}
		st.ExtraAccounts[c.Account] = m
		again, err := os.ReadFile(path)
		if err != nil || approvals.EvidenceHash(string(again)) != m.Receipt.LegacyHash {
			return p, st, fmt.Errorf("extra legacy state changed")
		}
	}
	p.QuarantineHash = quarantineHash(st)
	st.Activation = p
	st.PlanHash = p.Hash()
	// Repeat hashes to catch changes during the observation. Apply additionally
	// holds both shared dispatch and canonical approval decision fences.
	again, err := os.ReadFile(filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state.json"))
	if err != nil || approvals.EvidenceHash(string(again)) != p.LegacyHash {
		return p, st, fmt.Errorf("legacy state changed during prepare")
	}
	againInv, err := approvals.ReadEmailContinuityInventory(filepath.Join(s.Config.LegacyRoot, "artifacts"))
	if err != nil || againInv.Hash != p.ApprovalHash {
		return p, st, fmt.Errorf("approval evidence changed during prepare")
	}
	for _, orphan := range st.Orphans {
		raw, err := os.ReadFile(orphan.Path)
		if err != nil || approvals.EvidenceHash(string(raw)) != orphan.LegacyHash {
			return p, st, fmt.Errorf("orphan legacy state changed")
		}
	}
	againExtras, err := filepath.Glob(filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state-*.json"))
	if err != nil || !slices.Equal(extras, againExtras) {
		return p, st, fmt.Errorf("extra legacy inventory changed")
	}
	againBinding, err := s.validate()
	if err != nil || againBinding != binding {
		return p, st, fmt.Errorf("mailbox bindings changed during prepare")
	}
	return p, st, nil
}

// Prepare is read-only: Excalibur dispatch-owner alone publishes ownership.
func (s *Service) Prepare() (Plan, error) {
	f, err := connectorhandoff.FenceSnapshot(s.Config.LegacyRoot, "email")
	if err != nil {
		return Plan{}, err
	}
	if f.Owner != "excalibur" {
		return Plan{}, fmt.Errorf("prepare requires legacy ownership")
	}
	p, _, err := s.prepare(f.Revision)
	return p, err
}

// Apply is a CAS against the explicit ownership revision and plan hash. State,
// permanent quarantine receipt and activation are one atomic, exclusive file.
func (s *Service) Apply(expected string, revision uint64) (Plan, error) {
	f, release, err := connectorhandoff.AcquireFence(s.Config.LegacyRoot, "email")
	if err != nil {
		return Plan{}, err
	}
	defer release()
	if f.Owner != "manifest" || f.PreviousOwner != "excalibur" || f.Revision != revision+1 || f.Evidence != expected {
		return Plan{}, fmt.Errorf("matching dispatch-owner transfer required")
	}
	releaseDecisions, err := approvals.AcquireDecisionFence(filepath.Join(s.Config.LegacyRoot, "artifacts"))
	if err != nil {
		return Plan{}, err
	}
	defer releaseDecisions()
	p, st, err := s.prepare(revision)
	if err != nil {
		return p, err
	}
	if p.Hash() != expected {
		return p, fmt.Errorf("cutover evidence changed")
	}
	return p, write(s.path(), st, true)
}

func quarantineHash(st State) string {
	receipts := map[string]connectorhandoff.EmailIdentityReport{"primary": st.Receipt}
	for account, m := range st.ExtraAccounts {
		receipts[account] = m.Receipt
	}
	b, _ := json.Marshal(receipts)
	// Preserve activation hashes for existing receipts without orphans.
	if len(st.Orphans) > 0 {
		b, _ = json.Marshal(struct {
			Mailboxes map[string]connectorhandoff.EmailIdentityReport `json:"mailboxes"`
			Orphans   []connectorhandoff.EmailOrphanReceipt           `json:"orphans"`
		}{receipts, st.Orphans})
	}
	return approvals.EvidenceHash(string(b))
}
func importMailbox(raw []byte, account string, inv approvals.ConnectorInventory) (MailboxState, error) {
	receipt, err := connectorhandoff.ReconcileEmailContinuity(raw, account, inv)
	if err != nil {
		return MailboxState{}, err
	}
	m := MailboxState{receipt.Watermark, receipt, map[string]Thread{}}
	var legacy struct {
		Threads map[string]connectorhandoff.EmailThread `json:"threads"`
	}
	if err = json.Unmarshal(raw, &legacy); err != nil {
		return m, err
	}
	for _, r := range receipt.Threads {
		t := Thread{Status: approvals.ReconciledUncertain, ProposalID: r.LegacyProposalID, LastID: r.LastMsgID, LastMS: r.LastInternalMS}
		old := legacy.Threads[r.ThreadID]
		if len(r.StopReasons) == 0 && len(r.Claims) == 1 {
			switch r.Claims[0].Status {
			case "rejected":
				t.Status = "muted"
			case "pending":
				if old.Status == "proposed" {
					t.Status = "proposed"
				}
			case "approved":
				if old.Status == "synced" && t.LastID != "" && t.LastMS > 0 {
					t.Status = "synced"
				}
			}
		}
		m.Threads[r.ThreadID] = t
	}
	return m, nil
}
