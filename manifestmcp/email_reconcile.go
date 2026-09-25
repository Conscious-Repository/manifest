package manifestmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"manifest/gmailsend"
	"time"
)

// ReconcileEmail is an owner-only recovery entry point, not an MCP tool or an
// executor. lookup must be a trusted provider reader; no submitted body can
// replace the stored approval, sender, envelope or provider evidence.
func (a *Adapter) ReconcileEmail(ctx context.Context, id string, lookup func(context.Context, string, string) (gmailsend.SentProof, error)) (Object, error) {
	unlock, err := a.lockOperations()
	if err != nil {
		return nil, err
	}
	defer unlock()
	o, err := a.loadOperation(id)
	if err != nil {
		return nil, err
	}
	if o.ID != id || o.Tool != "email.prepare" || o.SchemaVersion != 1 || o.ToolVersion != Version || o.Policy != "human_approval" || o.ApprovalActor != "owner:local" || o.ApprovedAt.IsZero() {
		return nil, fmt.Errorf("approved email required")
	}
	if o.Status != "partial" && o.Status != "executing" && o.Status != "succeeded" {
		return nil, fmt.Errorf("an existing email attempt is required")
	}
	var payload struct {
		Preview json.RawMessage `json:"preview"`
	}
	if json.Unmarshal(o.Payload, &payload) != nil || revision(o.Payload) != id || revision(payload.Preview) != revision(o.Arguments) {
		return nil, fmt.Errorf("approved email changed")
	}
	var p struct {
		Domain string `json:"domain"`
		ID     string `json:"deliveryId"`
		Hash   string `json:"envelopeHash"`
	}
	if json.Unmarshal(o.Arguments, &p) != nil {
		return nil, fmt.Errorf("email approval unavailable")
	}
	d, err := a.mailStore().Get(p.ID)
	if err != nil {
		return nil, err
	}
	if d.Hash != p.Hash {
		return nil, fmt.Errorf("approved email envelope changed")
	}
	client, err := a.Mail.Resolve(p.Domain, nil)
	if err != nil {
		return nil, err
	}
	if client.Sender() != d.Message.From {
		return nil, fmt.Errorf("sender mapping changed; delivery remains unresolved")
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	d, err = a.mailStore().ReconcileApproved(bounded, p.ID, p.Hash, lookup)
	if err != nil {
		return nil, err
	} // Preserve the uncertain operation and original error.
	o.Result = Object{"deliveryId": p.ID, "envelopeHash": p.Hash, "sender": client.Sender(), "deliveryStatus": d.Status, "messageId": d.Ref.ID, "threadId": d.Ref.ThreadID}
	if d.Evidence != nil {
		o.Result["reconciliation"] = d.Evidence
	}
	o.Status, o.Error = "succeeded", ""
	if o.EmailWatch == nil && emailWatchRequested(o.Arguments) {
		o.EmailWatch = &EmailWatch{Enabled: true, Replies: []EmailReply{}}
	}
	if err := a.saveOperation(o); err != nil {
		return nil, err
	}
	return receipt(o), nil
}
