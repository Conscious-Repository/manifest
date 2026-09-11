package manifestmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"manifest/artifacts"
	"manifest/gmailsend"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type EmailAttachment struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}
type EmailInput struct {
	MonitorReplies bool              `json:"monitorReplies,omitempty"`
	Domain         string            `json:"domain"`
	To             []string          `json:"to"`
	Cc             []string          `json:"cc,omitempty"`
	Subject        string            `json:"subject"`
	Body           string            `json:"body"`
	InReplyTo      string            `json:"inReplyTo,omitempty"`
	References     string            `json:"references,omitempty"`
	Attachments    []EmailAttachment `json:"attachments,omitempty"`
	Conversation   string            `json:"conversation,omitempty"`
	Turn           string            `json:"turn,omitempty"`
	IdempotencyKey string            `json:"idempotencyKey"`
}

func (a *Adapter) mailStore() gmailsend.DeliveryStore {
	return gmailsend.DeliveryStore{Dir: filepath.Join(a.Data, "email-deliveries")}
}
func (a *Adapter) emailPrepare(q EmailInput) (Object, error) {
	if strings.TrimSpace(q.IdempotencyKey) == "" {
		return nil, fmt.Errorf("email preparation requires an idempotency key")
	}
	client, err := a.Mail.Resolve(q.Domain, append(append([]string{}, q.To...), q.Cc...))
	if err != nil {
		return nil, err
	}
	domain := "aion"
	if client.Sender() == "ben@ooda.group" {
		domain = "ooda"
	}
	// Fail closed if a misconfigured adapter substitutes another account.
	expected := "ben@aion.bio"
	if domain == "ooda" {
		expected = "ben@ooda.group"
	}
	if client.Sender() != expected {
		return nil, fmt.Errorf("sender does not match the domain mapping")
	}
	m := gmailsend.Message{From: client.Sender(), To: q.To, Cc: q.Cc, Subject: q.Subject, Body: q.Body, InReplyTo: q.InReplyTo, References: q.References}
	if len(q.Attachments) > 20 {
		return nil, fmt.Errorf("at most 20 attachments")
	}
	if len(q.Attachments) > 0 {
		pool, e := artifacts.New(filepath.Join(a.Data, "artifacts"))
		if e != nil {
			return nil, e
		}
		total := 0
		for _, ref := range q.Attachments {
			if !artifacts.ValidHash(ref.Hash) || !pool.Owns(domain, ref.Hash) {
				return nil, fmt.Errorf("attachment is not available in the correspondence domain")
			}
			f, e := os.Open(pool.BlobPath(ref.Hash))
			if e != nil {
				return nil, fmt.Errorf("attachment unavailable")
			}
			data, e := io.ReadAll(io.LimitReader(f, (20<<20)+1))
			f.Close()
			total += len(data)
			if e != nil || total > 20<<20 || artifacts.Hash(data) != ref.Hash {
				return nil, fmt.Errorf("attachment is missing, changed or exceeds 20 MiB")
			}
			m.Attachments = append(m.Attachments, gmailsend.Attachment{Name: ref.Name, Data: data})
		}
	}
	// The same instruction has one delivery identity across processes/retries.
	d, err := a.mailStore().Prepare(strings.TrimPrefix(revision(q), "sha256:"), m)
	if err != nil {
		return nil, err
	}
	preview := Object{"monitorReplies": q.MonitorReplies, "domain": domain, "deliveryId": d.ID, "envelopeHash": d.Hash, "email": Object{"from": d.Message.From, "to": d.Message.To, "cc": d.Message.Cc, "subject": d.Message.Subject, "body": d.Message.Body, "inReplyTo": d.Message.InReplyTo, "references": d.Message.References, "attachments": q.Attachments}}
	return prepared("email.prepare", "human_approval", preview), nil
}

// PrepareEmail is the owner HTTP integration; MCP uses the same preparation
// method through the standard persisted operation wrapper.
func (a *Adapter) PrepareEmail(q EmailInput) (Object, error) {
	if old, err := a.previousRequest("email.prepare", q); err != nil {
		return nil, err
	} else if old != nil {
		return receipt(old), nil
	}
	out, err := a.emailPrepare(q)
	if err != nil {
		return nil, err
	}
	return a.persist(out, q)
}
func (a *Adapter) executeEmail(ctx context.Context, o *OperationRecord) (Object, error) {
	if o.Status != "approved" && o.Status != "executing" {
		if o.Status == "pending_approval" {
			return nil, fmt.Errorf("owner approval required")
		}
		return receipt(o), nil
	}
	if o.SchemaVersion != 1 || o.ToolVersion != Version {
		return nil, fmt.Errorf("email operation version unsupported; nothing sent")
	}
	if o.Policy != "human_approval" || o.ApprovalActor != "owner:local" || o.ApprovedAt.IsZero() {
		return nil, fmt.Errorf("missing owner approval")
	}
	var payload struct {
		Preview json.RawMessage `json:"preview"`
	}
	if json.Unmarshal(o.Payload, &payload) != nil || revision(o.Payload) != o.ID {
		return nil, fmt.Errorf("approved email payload changed")
	}
	var p struct {
		Domain string `json:"domain"`
		ID     string `json:"deliveryId"`
		Hash   string `json:"envelopeHash"`
	}
	if err := json.Unmarshal(o.Arguments, &p); err != nil {
		return nil, err
	}
	if revision(payload.Preview) != revision(json.RawMessage(o.Arguments)) {
		return nil, fmt.Errorf("approved email preview changed")
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
		return nil, fmt.Errorf("sender mapping changed; prepare a new approval")
	}
	send := client.Send
	if a.mailSend != nil {
		send = a.mailSend
	} else if d.Status == "prepared" && !client.SendCapable() {
		o.Status = "failed"
		o.Error = "Sender " + client.Sender() + " is not connected for sending. Connect it in Settings, then prepare a new approval. Nothing sent."
		if err = a.saveOperation(o); err != nil {
			return nil, err
		}
		return receipt(o), nil
	}
	o.Status = "executing"
	if err = a.saveOperation(o); err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	d, err = a.mailStore().SendApproved(bounded, p.ID, p.Hash, send)
	o.Result = Object{"deliveryId": p.ID, "envelopeHash": p.Hash, "sender": client.Sender(), "deliveryStatus": d.Status, "messageId": d.Ref.ID, "threadId": d.Ref.ThreadID}
	o.Status = "succeeded"
	o.Error = ""
	if err != nil {
		o.Status = "failed"
		o.Error = err.Error()
		if d.Status == "uncertain" {
			o.Status = "partial"
		}
	}
	if o.Status == "succeeded" && o.EmailWatch == nil && emailWatchRequested(o.Arguments) {
		o.EmailWatch = &EmailWatch{Enabled: true, Replies: []EmailReply{}}
	}
	if e := a.saveOperation(o); e != nil {
		return nil, e
	}
	return receipt(o), nil
}
