package manifestmcp

import (
	"context"
	"errors"
	"manifest/gmailsend"
	"os"
	"testing"
)

func reconciliationFixture(t *testing.T) (*Adapter, string, gmailsend.SentProof) {
	t.Helper()
	a, _, _ := fixture(t)
	p, err := a.PrepareEmail(EmailInput{Domain: "ooda", To: []string{"contractor@example.com"}, Subject: "Quote", Body: "Exact approved body", IdempotencyKey: "recover-fixture", MonitorReplies: true})
	if err != nil {
		t.Fatal(err)
	}
	id := p["operationId"].(string)
	approve(t, a, id)
	var proof gmailsend.SentProof
	a.mailSend = func(_ context.Context, m gmailsend.Message) (gmailsend.Ref, error) {
		raw, err := gmailsend.Build(m)
		if err != nil {
			t.Fatal(err)
		}
		proof = gmailsend.SentProof{Mailbox: m.From, Ref: gmailsend.Ref{ID: "sent-original", ThreadID: "original-thread"}, Raw: raw}
		return gmailsend.Ref{}, errors.New("lost acknowledgment")
	}
	execute(t, a, id, "partial")
	a.mailSend = func(context.Context, gmailsend.Message) (gmailsend.Ref, error) {
		t.Error("recovery resent mail")
		return gmailsend.Ref{}, nil
	}
	return a, id, proof
}
func TestEmailReconcileCanonicalRecovery(t *testing.T) {
	a, id, proof := reconciliationFixture(t)
	reads := 0
	lookup := func(ctx context.Context, sender, messageID string) (gmailsend.SentProof, error) {
		reads++
		if sender != proof.Mailbox || messageID == "" {
			t.Fatal(sender, messageID)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded read")
		}
		return proof, nil
	}
	out, err := a.ReconcileEmail(context.Background(), id, lookup)
	if err != nil {
		t.Fatal(err)
	}
	o := out["record"].(*OperationRecord)
	if o.Status != "succeeded" || o.EmailWatch == nil || !o.EmailWatch.Enabled || o.Result["reconciliation"] == nil {
		t.Fatal(o)
	}
	if _, err := a.ConfirmedEmail(id); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(a.Vault, a.Data, a.System)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReconcileEmail(context.Background(), id, lookup); err != nil {
		t.Fatal(err)
	}
	execute(t, a, id, "succeeded")
	if reads != 1 {
		t.Fatal(reads)
	}
}
func TestEmailReconcileRefusesInvalidApprovalAndEvidence(t *testing.T) {
	for _, kind := range []string{"pending", "actor", "payload", "version", "mapping", "missing", "content", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			a, id, proof := reconciliationFixture(t)
			o, _ := a.loadOperation(id)
			switch kind {
			case "pending":
				o.Status = "pending_approval"
			case "actor":
				o.ApprovalActor = "agent"
			case "payload":
				o.Payload = []byte(`{}`)
			case "version":
				o.ToolVersion = "unsupported"
			case "mapping":
				a.Mail.Ooda = nil
			}
			if err := a.saveOperation(o); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads := 0
			_, err := a.ReconcileEmail(ctx, id, func(context.Context, string, string) (gmailsend.SentProof, error) {
				reads++
				switch kind {
				case "missing":
					return proof, errors.New("PRIVATE")
				case "content":
					proof.Raw = []byte("wrong")
				case "cancel":
					cancel()
				}
				return proof, nil
			})
			if err == nil {
				t.Fatal("accepted", kind)
			}
			if kind != "missing" && kind != "content" && kind != "cancel" && reads != 0 {
				t.Fatal("read without valid approval")
			}
			if _, err := a.ConfirmedEmail(id); err == nil {
				t.Fatal("unconfirmed outcome")
			}
		})
	}
}
func TestEmailReconcileRepairsOperationWriteFailure(t *testing.T) {
	a, id, proof := reconciliationFixture(t)
	path, _ := a.opPath(id)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.ReconcileEmail(context.Background(), id, func(context.Context, string, string) (gmailsend.SentProof, error) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		return proof, nil
	})
	if err == nil {
		t.Fatal("expected operation write failure")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = a.ReconcileEmail(context.Background(), id, func(context.Context, string, string) (gmailsend.SentProof, error) {
		t.Error("re-read confirmed evidence")
		return proof, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmedEmail(id); err != nil {
		t.Fatal(err)
	}
}
