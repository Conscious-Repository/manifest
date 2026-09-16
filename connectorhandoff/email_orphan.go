package connectorhandoff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"manifest/approvals"
)

const LegacyOrphanQuarantined = "legacy-orphan-quarantined"

// EmailOrphanReceipt preserves disconnected state without asserting an account,
// matching approval identities, or constructing an active mailbox ledger.
type EmailOrphanReceipt struct {
	Path         string            `json:"path"`
	LegacyHash   string            `json:"legacyHash"`
	ThreadHashes map[string]string `json:"threadHashes"`
	Disposition  string            `json:"disposition"`
	Replay       bool              `json:"replay"`
}

func QuarantineEmailOrphan(path string, raw []byte) (EmailOrphanReceipt, error) {
	var out EmailOrphanReceipt
	if err := uniqueJSON(raw); err != nil {
		return out, err
	}
	var old legacyEmail
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&old) != nil || old.Threads == nil {
		return out, fmt.Errorf("ambiguous orphan email state")
	}
	if _, err := time.Parse(time.RFC3339, old.Watermark); err != nil {
		return out, fmt.Errorf("invalid orphan watermark")
	}
	out = EmailOrphanReceipt{Path: path, LegacyHash: approvals.EvidenceHash(string(raw)), ThreadHashes: map[string]string{}, Disposition: LegacyOrphanQuarantined}
	for id, row := range old.Threads {
		if id == "" || row == nil || row.LastInternalMS < 0 {
			return EmailOrphanReceipt{}, fmt.Errorf("invalid orphan thread")
		}
		b, _ := json.Marshal(row)
		out.ThreadHashes[id] = approvals.EvidenceHash(string(b))
	}
	return out, nil
}
