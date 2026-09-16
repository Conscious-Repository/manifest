// Package personalemail implements only personal mailbox continuity checks.
// It has no approval, vault, roster, candidate, or cursor writer.
package personalemail

import (
	"context"
	"fmt"
	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/gmailauth"
	"manifest/gmailsync"
)

type AnchorReader interface {
	ThreadAnchor(context.Context, string, string, int64) error
}
type OpenReader func(context.Context, string) (AnchorReader, error)
type Options struct {
	Enabled bool
	Account string
}
type Observation struct {
	ThreadID string `json:"threadId"`
	Status   string `json:"status"`
	Replay   bool   `json:"replay"`
}
type Report struct {
	Identity     connectorhandoff.EmailIdentityReport `json:"identity"`
	Enabled      bool                                 `json:"enabled"`
	Mode         string                               `json:"mode"`
	Observations []Observation                        `json:"observations"`
}

// GmailReader reuses personal account selection and refreshes tokens in memory.
// No credential file is updated. Gmail API operations are GET only; OAuth may
// POST to refresh an expired token when explicitly enabling live verification.
func GmailReader(ctx context.Context, account string) (AnchorReader, error) {
	src, err := gmailauth.New().ReadSource(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("personal Gmail read connection unavailable")
	}
	return gmailsync.NewMailboxClient(src, account), nil
}

// Check permits clean-thread reads even when other threads are quarantined.
// Disabled mode never opens credentials or a network client. No mode imports an
// active cursor, claims parity, or authorizes historical replay.
func Check(ctx context.Context, o Options, raw []byte, inv approvals.ConnectorInventory, open OpenReader) (Report, error) {
	identity, err := connectorhandoff.ReconcileEmailContinuity(raw, o.Account, inv)
	r := Report{Identity: identity, Enabled: o.Enabled, Mode: "continuity-only", Observations: []Observation{}}
	if err != nil {
		return r, err
	}
	if !o.Enabled {
		return r, nil
	}
	var reader AnchorReader
	for _, t := range identity.Threads {
		status := "quarantined"
		if len(t.StopReasons) == 0 {
			status = "anchor-unavailable"
			if t.LastMsgID != "" && t.LastInternalMS > 0 {
				if reader == nil {
					if open == nil {
						return r, fmt.Errorf("read transport required")
					}
					reader, err = open(ctx, o.Account)
					if err != nil || reader == nil {
						return r, fmt.Errorf("personal Gmail read connection unavailable")
					}
				}
				status = "read-failed"
				if reader.ThreadAnchor(ctx, t.ThreadID, t.LastMsgID, t.LastInternalMS) == nil {
					status = "anchor-verified"
				}
			}
		}
		r.Observations = append(r.Observations, Observation{ThreadID: t.ThreadID, Status: status})
	}
	return r, nil
}
