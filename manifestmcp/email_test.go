package manifestmcp

import (
	"context"
	"encoding/json"
	"errors"
	"manifest/artifacts"
	"manifest/gmailsend"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmailApprovalAndDeliveryRecovery(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "lost-ack"}[uncertain], func(t *testing.T) {
			a, _, _ := fixture(t)
			q := EmailInput{Domain: "ooda", To: []string{"contractor@example.com"}, Subject: "Plans", Body: "Please quote this work.", Conversation: "chat-1", IdempotencyKey: "email-fixture-1"}
			prepared, err := a.PrepareEmail(q)
			if err != nil {
				t.Fatal(err)
			}
			id := prepared["operationId"].(string)
			calls := 0
			a.mailSend = func(_ context.Context, m gmailsend.Message) (gmailsend.Ref, error) {
				calls++
				if m.From != "ben@ooda.group" || m.Body != q.Body {
					t.Fatal("wrong approved envelope")
				}
				if uncertain {
					return gmailsend.Ref{}, errors.New("timeout")
				}
				return gmailsend.Ref{ID: "message-1", ThreadID: "thread-1"}, nil
			}
			if _, err = a.Execute(context.Background(), id); err == nil || calls != 0 {
				t.Fatal("sent without approval")
			}
			// Unrelated recruiting state must not invalidate an immutable email.
			os.WriteFile(filepath.Join(a.Vault, a.Records.Root(), "fixture-unrelated.md"), []byte("changed"), 0600)
			rows, err := a.Observe()
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].Status != "pending_approval" {
				t.Fatal("unrelated data invalidated email")
			}
			approve(t, a, id)
			status := "succeeded"
			if uncertain {
				status = "partial"
			}
			o := execute(t, a, id, status)
			if calls != 1 {
				t.Fatal("wrong send count")
			}
			if !uncertain && o.Result["threadId"] != "thread-1" {
				t.Fatal("provider thread not retained")
			}
			// Simulate process loss between provider receipt and operation completion.
			o.Status = "executing"
			a.saveOperation(o)
			restarted, err := New(a.Vault, a.Data, a.System)
			if err != nil {
				t.Fatal(err)
			}
			restarted.mailSend = a.mailSend
			execute(t, restarted, id, status)
			if calls != 1 {
				t.Fatal("replayed email after restart")
			}
			q.Body = "changed"
			if _, err = a.PrepareEmail(q); err == nil {
				t.Fatal("same key accepted edited email")
			}
		})
	}
}
func TestEmailTamperAndUnavailableSender(t *testing.T) {
	a, _, _ := fixture(t)
	q := EmailInput{Domain: "aion", To: []string{"x@example.com"}, Subject: "Test", Body: "body", IdempotencyKey: "email-fixture-tamper"}
	p, e := a.PrepareEmail(q)
	if e != nil {
		t.Fatal(e)
	}
	id := p["operationId"].(string)
	approve(t, a, id)
	o, e := a.loadOperation(id)
	if e != nil {
		t.Fatal(e)
	}
	var args Object
	json.Unmarshal(o.Arguments, &args)
	args["domain"] = "ooda"
	o.Arguments, _ = json.Marshal(args)
	a.saveOperation(o)
	if _, e = a.Execute(context.Background(), id); e == nil {
		t.Fatal("accepted changed preview")
	}
	q.IdempotencyKey = "email-unconnected"
	p, e = a.PrepareEmail(q)
	if e != nil {
		t.Fatal(e)
	}
	id = p["operationId"].(string)
	approve(t, a, id)
	o = execute(t, a, id, "failed")
	if !strings.Contains(o.Error, "Nothing sent") {
		t.Fatal(o.Error)
	}
	q.Domain = "personal"
	q.IdempotencyKey = "personal-missing"
	if _, e = a.PrepareEmail(q); e == nil {
		t.Fatal("personal fell back")
	}
}

func TestEmailAttachmentsAreFrozenBeforeApproval(t *testing.T) {
	a, _, _ := fixture(t)
	pool, err := artifacts.New(filepath.Join(a.Data, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pool.Save(strings.NewReader("approved attachment bytes"), "plans.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.Add("ooda", artifacts.Entry{Ref: ref}); err != nil {
		t.Fatal(err)
	}
	q := EmailInput{Domain: "aion", To: []string{"x@example.com"}, Subject: "Plans", Body: "Review", Attachments: []EmailAttachment{{Hash: ref.Hash, Name: ref.Name}}, IdempotencyKey: "attachment-fixture"}
	if _, err = a.PrepareEmail(q); err == nil {
		t.Fatal("cross-domain attachment accepted")
	}
	q.Domain = "ooda"
	p, err := a.PrepareEmail(q)
	if err != nil {
		t.Fatal(err)
	}
	id := p["operationId"].(string)
	os.WriteFile(pool.BlobPath(ref.Hash), []byte("changed after approval"), 0600)
	a.mailSend = func(_ context.Context, m gmailsend.Message) (gmailsend.Ref, error) {
		if len(m.Attachments) != 1 || string(m.Attachments[0].Data) != "approved attachment bytes" {
			t.Fatal("did not send frozen bytes")
		}
		return gmailsend.Ref{ID: "msg"}, nil
	}
	approve(t, a, id)
	execute(t, a, id, "succeeded")
}
