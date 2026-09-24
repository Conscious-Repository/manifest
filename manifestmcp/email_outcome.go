package manifestmcp

import (
	"encoding/json"
	"fmt"
	"manifest/gmailsend"
	"time"
)

type ConfirmedEmailOutcome struct {
	OperationID string
	Source      *EmailSourceRecord
	Message     gmailsend.Message
	Ref         gmailsend.Ref
	ConfirmedAt time.Time
}

// ConfirmedEmail reads the immutable approval and confirmed delivery envelope. It
// cannot execute, retry or reconcile an uncertain provider send.
func (a *Adapter) ConfirmedEmail(id string) (ConfirmedEmailOutcome, error) {
	var out ConfirmedEmailOutcome
	o, err := a.loadOperation(id)
	if err != nil {
		return out, err
	}
	if o.ID != id || o.Tool != "email.prepare" || o.Status != "succeeded" || o.Policy != "human_approval" || o.ApprovalActor != "owner:local" || o.ApprovedAt.IsZero() || revision(o.Payload) != id {
		return out, fmt.Errorf("confirmed owner-approved email required")
	}
	var payload struct {
		Preview json.RawMessage `json:"preview"`
	}
	if json.Unmarshal(o.Payload, &payload) != nil || revision(payload.Preview) != revision(o.Arguments) {
		return out, fmt.Errorf("email approval changed")
	}
	var p struct {
		ID     string             `json:"deliveryId"`
		Hash   string             `json:"envelopeHash"`
		Source *EmailSourceRecord `json:"sourceRecord"`
	}
	if err = json.Unmarshal(o.Arguments, &p); err != nil {
		return out, err
	}
	d, err := a.mailStore().Get(p.ID)
	if err != nil {
		return out, err
	}
	if d.Hash != p.Hash || d.Status != "sent" || o.Result["deliveryStatus"] != "sent" || o.Result["messageId"] != d.Ref.ID || o.Result["threadId"] != d.Ref.ThreadID {
		return out, fmt.Errorf("confirmed delivery receipt does not match approval")
	}
	for _, h := range o.History {
		if h.Status == "succeeded" {
			out.ConfirmedAt = h.At
			break
		}
	}
	if out.ConfirmedAt.IsZero() {
		return out, fmt.Errorf("delivery confirmation time unavailable")
	}
	out.OperationID = id
	out.Source = p.Source
	out.Message = d.Message
	out.Ref = d.Ref
	return out, nil
}
