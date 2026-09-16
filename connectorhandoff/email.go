package connectorhandoff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"manifest/approvals"
)

type EmailThread struct {
	Disposition    string   `json:"disposition,omitempty"`
	Replay         bool     `json:"replay"`
	ProposalIDs    []string `json:"proposal_ids,omitempty"`
	Status         string   `json:"status"`
	ProposalID     string   `json:"proposal_id,omitempty"`
	LastMsgID      string   `json:"last_msg_id,omitempty"`
	LastInternalMS int64    `json:"last_internal_ms,omitempty"`
	Filename       string   `json:"filename,omitempty"`
}
type EmailCheckpoint struct {
	Version      int                     `json:"version"`
	Account      string                  `json:"account"`
	LegacyHash   string                  `json:"legacyHash"`
	ApprovalHash string                  `json:"approvalHash"`
	Watermark    time.Time               `json:"watermark"`
	Threads      map[string]*EmailThread `json:"threads"`
	Uncertain    []string                `json:"uncertain"`
}

// PrepareEmail preserves the entire legacy ledger. account is an operator-supplied
// binding, not inferred from a slug/token. This never enables a mailbox or turns
// missing decisions into retryable work. An unresolved append holds the handoff.
func PrepareEmail(raw []byte, account string, inv approvals.ConnectorInventory) (EmailCheckpoint, error) {
	return PrepareEmailWithReconciliation(raw, account, inv, nil)
}

// PrepareEmailWithReconciliation retains rejected lineage and suppresses replay.
// owner must come from the validated durable inventory reader.
func PrepareEmailWithReconciliation(raw []byte, account string, inv approvals.ConnectorInventory, owner *approvals.OwnerReconciliation) (EmailCheckpoint, error) {
	var out EmailCheckpoint
	if owner != nil {
		if err := approvals.ValidateOwnerReconciliation(*owner, inv); err != nil {
			return out, err
		}
	}
	if account == "" || !ValidHash(inv.Hash) {
		return out, fmt.Errorf("account binding and canonical inventory required")
	}
	if err := uniqueJSON(raw); err != nil {
		return out, err
	}
	var legacy struct {
		Watermark string `json:"watermark"`
		Threads   map[string]*struct {
			Status         string `json:"status"`
			ProposalID     string `json:"proposal_id,omitempty"`
			LastMsgID      string `json:"last_msg_id,omitempty"`
			LastInternalMS int64  `json:"last_internal_ms,omitempty"`
			Filename       string `json:"filename,omitempty"`
		} `json:"threads"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&legacy) != nil || legacy.Threads == nil {
		return out, fmt.Errorf("invalid legacy email state [REDACTED]")
	}
	at, err := time.Parse(time.RFC3339, legacy.Watermark)
	if err != nil {
		return out, fmt.Errorf("invalid email watermark")
	}
	hash := sha256.Sum256(raw)
	out = EmailCheckpoint{Version: 1, Account: account, LegacyHash: hex.EncodeToString(hash[:]), ApprovalHash: inv.Hash, Watermark: at, Threads: map[string]*EmailThread{}, Uncertain: []string{}}
	byID := map[string]approvals.ConnectorApproval{}
	for _, p := range inv.Items {
		if p.Source != "gmail-thread" {
			continue
		}
		byID[p.ID] = p
		if p.Source == "gmail-thread" && p.Type == approvals.TypeAppendVaultNote && p.Status != "approved" {
			out.Uncertain = append(out.Uncertain, p.ID)
		}
	}
	for id, old := range legacy.Threads {
		if old == nil {
			return EmailCheckpoint{}, fmt.Errorf("invalid email thread")
		}
		th := &EmailThread{Status: old.Status, ProposalID: old.ProposalID, LastMsgID: old.LastMsgID, LastInternalMS: old.LastInternalMS, Filename: old.Filename}
		out.Threads[id] = th
		if id == "" || th == nil || th.LastInternalMS < 0 {
			return EmailCheckpoint{}, fmt.Errorf("invalid email thread [REDACTED]")
		}
		switch th.Status {
		case "proposed", "synced", "muted":
		default:
			return EmailCheckpoint{}, fmt.Errorf("unknown email lifecycle [REDACTED]")
		}
		p, ok := byID[th.ProposalID]
		if th.ProposalID != "" && (!ok || p.Source != "gmail-thread" || p.SourceID != id || p.Type != approvals.TypeCreateVaultNote) {
			return EmailCheckpoint{}, fmt.Errorf("email approval identity mismatch [REDACTED]")
		}
		if th.Status == "proposed" && !ok {
			return EmailCheckpoint{}, fmt.Errorf("proposed email lacks canonical approval [REDACTED]")
		}
		if owner != nil && id == owner.SourceID {
			if owner.Source != "email" || owner.Disposition != approvals.ReconciledRejected || owner.Replay || !ok || p.Status != "rejected" || th.Status == "synced" {
				return EmailCheckpoint{}, fmt.Errorf("rejected owner lineage conflicts with email state")
			}
			th.Status = "muted"
			th.Disposition = owner.Disposition
			th.Replay = false
			for _, artifact := range owner.Artifacts {
				th.ProposalIDs = append(th.ProposalIDs, artifact.ID)
			}
			continue
		}
		// Preserve proposed/synced/muted verbatim. A crash between the vault effect
		// and state save must be reviewed, never silently promoted or replayed.
		if th.Status == "synced" || th.Status == "proposed" && p.Status != "pending" || th.Status == "muted" && (!ok || p.Status != "rejected") {
			out.Uncertain = append(out.Uncertain, id)
		}
	}
	if owner != nil && out.Threads[owner.SourceID] == nil {
		return EmailCheckpoint{}, fmt.Errorf("owner rejected thread missing from legacy ledger")
	}
	sort.Strings(out.Uncertain)
	return out, nil
}

// encoding/json normally accepts duplicate keys with last-wins semantics, which
// would silently lose a thread during import. Reject them at every object depth.
func uniqueJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var value func() error
	value = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		if delim != '{' && delim != '[' {
			return fmt.Errorf("invalid JSON")
		}
		keys := map[string]bool{}
		for d.More() {
			if delim == '{' {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || keys[key] {
					return fmt.Errorf("duplicate JSON identity")
				}
				keys[key] = true
			}
			if err := value(); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return fmt.Errorf("invalid or duplicate email state [REDACTED]")
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing email state [REDACTED]")
	}
	return nil
}
