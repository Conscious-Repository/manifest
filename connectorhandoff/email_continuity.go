package connectorhandoff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"manifest/approvals"
)

// EmailIdentityReport is derived evidence only, never a cursor or apply queue.
// ImportedLegacy is an exact in-memory copy only when every identity reconciles;
// it is deliberately excluded from the redacted report serialization.
type EmailIdentityReport struct {
	Version          int             `json:"version"`
	AccountHash      string          `json:"accountHash"`
	LegacyHash       string          `json:"legacyHash"`
	ApprovalHash     string          `json:"approvalHash"`
	Watermark        time.Time       `json:"watermark"`
	Replay           bool            `json:"replay"`
	IdentityComplete bool            `json:"identityComplete"`
	Parity           bool            `json:"parity"`
	Threads          []EmailIdentity `json:"threads"`
	ImportedLegacy   []byte          `json:"-"`
}
type EmailIdentity struct {
	ThreadID         string       `json:"threadId"`
	LegacyProposalID string       `json:"legacyProposalId,omitempty"`
	LegacyThreadHash string       `json:"legacyThreadHash,omitempty"`
	Disposition      string       `json:"disposition"`
	Replay           bool         `json:"replay"`
	StopReasons      []string     `json:"stopReasons"`
	Claims           []EmailClaim `json:"claims"`
	LastMsgID        string       `json:"-"`
	LastInternalMS   int64        `json:"-"`
}
type EmailClaim struct {
	ProposalID   string `json:"proposalId"`
	ThreadID     string `json:"threadId"`
	Status       string `json:"status"`
	ArtifactHash string `json:"artifactHash"`
}

// ReconcileEmailContinuity never consumes owner exceptions. Both sides of a
// wrong association, duplicate claims, and approvals absent from state remain
// quarantined. Clean identities may be read, but no historical effect is replayed.
func ReconcileEmailContinuity(raw []byte, account string, inv approvals.ConnectorInventory) (EmailIdentityReport, error) {
	var r EmailIdentityReport
	if account == "" || !ValidHash(inv.Hash) {
		return r, fmt.Errorf("account and canonical inventory required")
	}
	if err := uniqueJSON(raw); err != nil {
		return r, err
	}
	var old legacyEmail
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&old) != nil || old.Threads == nil {
		return r, fmt.Errorf("invalid legacy email state [REDACTED]")
	}
	at, err := time.Parse(time.RFC3339, old.Watermark)
	if err != nil {
		return r, fmt.Errorf("invalid email watermark")
	}
	r = EmailIdentityReport{Version: 1, AccountHash: approvals.EvidenceHash(account), LegacyHash: approvals.EvidenceHash(string(raw)), ApprovalHash: inv.Hash, Watermark: at, IdentityComplete: true, Threads: []EmailIdentity{}}
	rows := map[string]*EmailIdentity{}
	row := func(id string) *EmailIdentity {
		if rows[id] == nil {
			rows[id] = &EmailIdentity{ThreadID: id, Disposition: "legacy-continuity-only", StopReasons: []string{}, Claims: []EmailClaim{}}
		}
		return rows[id]
	}
	stop := func(id, reason string) {
		t := row(id)
		for _, s := range t.StopReasons {
			if s == reason {
				return
			}
		}
		t.StopReasons = append(t.StopReasons, reason)
		t.Disposition = approvals.ReconciledUncertain
		r.IdentityComplete = false
	}
	refs := map[string][]string{}
	for id, t := range old.Threads {
		if id == "" || t == nil || t.LastInternalMS < 0 {
			return EmailIdentityReport{}, fmt.Errorf("invalid legacy thread")
		}
		b, _ := json.Marshal(t)
		out := row(id)
		out.LegacyThreadHash = approvals.EvidenceHash(string(b))
		out.LegacyProposalID = t.ProposalID
		out.LastMsgID = t.LastMsgID
		out.LastInternalMS = t.LastInternalMS
		if t.Status != "synced" && t.Status != "proposed" && t.Status != "muted" {
			stop(id, "unknown-lifecycle")
		}
		if t.ProposalID != "" {
			refs[t.ProposalID] = append(refs[t.ProposalID], id)
		} else {
			stop(id, "missing-proposal-identity")
		}
	}
	byID := map[string][]approvals.ConnectorApproval{}
	creates := map[string]int{}
	for _, p := range inv.Items {
		if p.Source != "gmail-thread" || p.ID == "" || p.SourceID == "" || !ValidHash(p.Hash) {
			return EmailIdentityReport{}, fmt.Errorf("invalid canonical email evidence")
		}
		byID[p.ID] = append(byID[p.ID], p)
		if p.Type == approvals.TypeCreateVaultNote {
			creates[p.SourceID]++
		}
		claim := EmailClaim{p.ID, p.SourceID, p.Status, p.Hash}
		row(p.SourceID).Claims = append(row(p.SourceID).Claims, claim)
		if old.Threads[p.SourceID] == nil {
			stop(p.SourceID, "approval-thread-absent-from-state")
		}
		for _, id := range refs[p.ID] {
			if id != p.SourceID {
				row(id).Claims = append(row(id).Claims, claim)
				stop(id, "proposal-thread-mismatch")
				stop(p.SourceID, "proposal-thread-mismatch")
			}
		}
		if p.Type != approvals.TypeCreateVaultNote {
			stop(p.SourceID, "append-lineage-needs-effect-evidence")
		}
	}
	for id, t := range old.Threads {
		ps := byID[t.ProposalID]
		if len(ps) == 0 {
			stop(id, "missing-canonical-approval")
		}
		if len(ps) > 1 || len(refs[t.ProposalID]) > 1 {
			stop(id, "duplicate-proposal-identity")
		}
		for _, p := range ps {
			if p.Type != approvals.TypeCreateVaultNote {
				stop(id, "wrong-proposal-type")
			}
		}
	}
	for _, ps := range byID {
		if len(ps) > 1 {
			for _, p := range ps {
				stop(p.SourceID, "duplicate-proposal-identity")
			}
		}
	}
	for id, n := range creates {
		if n > 1 {
			stop(id, "duplicate-thread-create-claims")
		}
	}
	for _, t := range rows {
		sort.Strings(t.StopReasons)
		sort.Slice(t.Claims, func(i, j int) bool {
			a, _ := json.Marshal(t.Claims[i])
			b, _ := json.Marshal(t.Claims[j])
			return string(a) < string(b)
		})
		r.Threads = append(r.Threads, *t)
	}
	sort.Slice(r.Threads, func(i, j int) bool { return r.Threads[i].ThreadID < r.Threads[j].ThreadID })
	if r.IdentityComplete {
		r.ImportedLegacy = bytes.Clone(raw)
	}
	return r, nil
}
