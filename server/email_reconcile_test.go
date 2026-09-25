package server

import (
	"context"
	"encoding/json"
	"manifest/gmailsend"
	"manifest/manifestmcp"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmailReconcileHTTPRepairsSavedDeliveryOnly(t *testing.T) {
	a, err := manifestmcp.New(t.TempDir(), t.TempDir(), "system")
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.PrepareEmail(manifestmcp.EmailInput{Domain: "ooda", To: []string{"contractor@example.com"}, Subject: "Quote", Body: "Frozen body", IdempotencyKey: "http-recovery"})
	if err != nil {
		t.Fatal(err)
	}
	id := p["operationId"].(string)
	s := &Server{manifestOperations: a}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/manifest/operations/"+id+"/email-reconcile", strings.NewReader(`{"sender":"attacker@example.com","raw":"forged"}`))
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleEmailReconcile(w, r)
		return w
	}
	if w := request(); w.Code != 409 {
		t.Fatal("pending approval admitted", w.Code)
	}
	decided, err := a.Decide(id, "approved", "owner:local")
	if err != nil {
		t.Fatal(err)
	}
	o := decided["record"].(*manifestmcp.OperationRecord)
	var args struct {
		ID   string `json:"deliveryId"`
		Hash string `json:"envelopeHash"`
	}
	if err := json.Unmarshal(o.Arguments, &args); err != nil {
		t.Fatal(err)
	}
	store := gmailsend.DeliveryStore{Dir: filepath.Join(a.Data, "email-deliveries")}
	_, err = store.SendApproved(context.Background(), args.ID, args.Hash, func(context.Context, gmailsend.Message) (gmailsend.Ref, error) {
		return gmailsend.Ref{ID: "already-sent", ThreadID: "original-thread"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Model a process crash after the delivery receipt but before operation save.
	o.Status = "executing"
	b, _ := json.Marshal(o)
	path := filepath.Join(a.Data, "operations", strings.TrimPrefix(id, "sha256:")+".json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		w := request()
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		out, err := a.ConfirmedEmail(id)
		if err != nil || out.Ref.ID != "already-sent" || out.Message.From != "ben@ooda.group" {
			t.Fatal(out, err)
		}
	}
}
