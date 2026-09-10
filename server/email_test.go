package server

import (
	"context"
	"encoding/json"
	"golang.org/x/oauth2"
	"manifest/approvals"
	"manifest/gmailsend"
	"manifest/manifestmcp"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmailApprovalSharesChatFeedAndMappedSender(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GMAIL_SEND_TOKEN", "")
	creds := filepath.Join(root, "creds.json")
	os.WriteFile(creds, []byte(`{"installed":{"client_id":"fixture","client_secret":"fixture","auth_uri":"https://example.invalid/auth","token_uri":"https://example.invalid/token","redirect_uris":["http://localhost"]}}`), 0600)
	t.Setenv("GMAIL_OAUTH_CLIENT", creds)
	adapter, err := manifestmcp.New(filepath.Join(root, "vault"), filepath.Join(root, "data"), "system")
	if err != nil {
		t.Fatal(err)
	}
	reg := gmailsend.NewRegistry(filepath.Join(root, "data"))
	s := New(nil, nil, nil)
	s.UseApprovals(approvals.NewStore(filepath.Join(root, "harness")))
	s.UseMailSenders(reg)
	s.UseManifestOperations(adapter)
	sends := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		var msg map[string]string
		json.NewDecoder(r.Body).Decode(&msg)
		raw, _ := gmailsend.DecodeRaw(msg["raw"])
		if !strings.Contains(string(raw), "From: ben@ooda.group\r\n") {
			t.Error("wrong sender")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg-fixture","threadId":"thread-fixture"}`))
	}))
	defer provider.Close()
	reg.Ooda.UseEndpoint(provider.URL, provider.Client())
	reg.Ooda.SaveToken("ben@ooda.group", &oauth2.Token{AccessToken: "fixture", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, []string{gmailsend.SendScope})
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"domain":"ooda","to":["fixture@example.com"],"subject":"Plans","body":"Please quote.","conversation":"chat-fixture","idempotencyKey":"server-mail-fixture"}`))
	w := httptest.NewRecorder()
	s.handleEmailPrepare(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		ID string `json:"operationId"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if sends != 0 || len(s.chatOperations("chat-fixture")) != 1 || len(s.feedProposals()) != 1 {
		t.Fatal("preparation sent or lost approval projection")
	}
	req = httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	req.SetPathValue("id", manifestmcp.ProposalID(result.ID))
	w = httptest.NewRecorder()
	s.handleSpiritsApprovalConfirm(w, req)
	if w.Code != 200 || sends != 1 {
		state, _ := adapter.Operation(result.ID)
		t.Fatalf("decision %d %s sends=%d receipt=%+v", w.Code, w.Body.String(), sends, state["record"].(*manifestmcp.OperationRecord).Error)
	}
	if len(s.feedProposals()) != 0 {
		t.Fatal("settled proposal remains in feed")
	}
	done := s.chatOperations("chat-fixture")[0]["record"].(*manifestmcp.OperationRecord)
	if done.Status != "succeeded" || done.Result["threadId"] != "thread-fixture" {
		t.Fatalf("bad chat receipt: %+v", done)
	}
	adapter.Execute(context.Background(), result.ID)
	if sends != 1 {
		t.Fatal("replayed send")
	}
	if s.mailConnectionRow("ooda").Masked != "ben@ooda.group" || s.gmailSend.Sender() != "ben@aion.bio" {
		t.Fatal("settings/recruiting mappings differ")
	}
}
