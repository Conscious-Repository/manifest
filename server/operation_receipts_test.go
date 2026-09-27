package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"manifest/agentchat"
	"manifest/approvals"
	"manifest/gmailsend"
	"manifest/manifestmcp"
)

// Owner decision D1 (2026-09-27): a settled approval's receipt is reachable
// from chat, task and Feed, and all three show the same immutable receipt.
// One approved send and one rejection are prepared in a chat linked to a
// task; a third, approved in an unrelated chat, must reach Feed but not the
// task.
func TestSettledApprovalReceiptIsOneIdentityAcrossChatTaskFeed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GMAIL_SEND_TOKEN", "")
	creds := filepath.Join(root, "creds.json")
	os.WriteFile(creds, []byte(`{"installed":{"client_id":"fixture","client_secret":"fixture","auth_uri":"https://example.invalid/auth","token_uri":"https://example.invalid/token","redirect_uris":["http://localhost"]}}`), 0600)
	t.Setenv("GMAIL_OAUTH_CLIENT", creds)

	s := loopFixture(t)
	st := agentchat.New(filepath.Join(root, "chats"))
	s.UseAgentChat(st)
	adapter, err := manifestmcp.New(filepath.Join(root, "vault"), filepath.Join(root, "data"), "system")
	if err != nil {
		t.Fatal(err)
	}
	reg := gmailsend.NewRegistry(filepath.Join(root, "data"))
	s.UseApprovals(approvals.NewStore(filepath.Join(root, "harness")))
	s.UseMailSenders(reg)
	s.UseManifestOperations(adapter)
	sends := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg-fixture","threadId":"thread-fixture"}`))
	}))
	defer provider.Close()
	reg.Ooda.UseEndpoint(provider.URL, provider.Client())
	reg.Ooda.SaveToken("ben@ooda.group", &oauth2.Token{AccessToken: "fixture", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, []string{gmailsend.SendScope})

	// The task's conversation, linked by the explicit promote origin.
	linked, err := st.Create("alfred", "", "contractor quotes", "")
	if err != nil {
		t.Fatal(err)
	}
	st.AppendTurn("alfred", linked, "user", "get quotes from the gutter contractors", 0)
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"turn":1}`))
	req.SetPathValue("agent", "alfred")
	req.SetPathValue("id", linked)
	w := httptest.NewRecorder()
	s.handleAgentChatPromote(w, req)
	var promoted struct {
		Created string `json:"created"`
	}
	json.Unmarshal(w.Body.Bytes(), &promoted)
	task := promoted.Created
	if w.Code != 200 || task == "" {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	other, err := st.Create("alfred", "", "unrelated", "")
	if err != nil {
		t.Fatal(err)
	}

	prepare := func(conversation, key string) string {
		t.Helper()
		body := `{"domain":"ooda","to":["fixture@example.com"],"subject":"Quote ` + key + `","body":"Please quote.","conversation":"` + conversation + `","idempotencyKey":"` + key + `"}`
		w := httptest.NewRecorder()
		s.handleEmailPrepare(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		var out struct {
			ID string `json:"operationId"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != 200 || out.ID == "" {
			t.Fatalf("prepare %s: %d %s", key, w.Code, w.Body.String())
		}
		return out.ID
	}
	decide := func(id string, handler func(http.ResponseWriter, *http.Request)) {
		t.Helper()
		req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
		req.SetPathValue("id", manifestmcp.ProposalID(id))
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != 200 {
			t.Fatalf("decision %s: %d %s", id, w.Code, w.Body.String())
		}
	}
	sent := prepare(linked, "receipt-sent")
	declined := prepare(linked, "receipt-declined")
	elsewhere := prepare(other, "receipt-elsewhere")

	// Pending is not settled: no surface shows a receipt yet.
	if got := s.operationReceipts(); len(got) != 0 {
		t.Fatalf("pending operations projected as receipts: %+v", got)
	}
	decide(sent, s.handleSpiritsApprovalConfirm)
	decide(declined, s.handleSpiritsApprovalReject)
	decide(elsewhere, s.handleSpiritsApprovalConfirm)
	if sends != 2 {
		t.Fatalf("sends = %d, want the two approved", sends)
	}

	// Chat: the operation card carries the receipt.
	chat := map[string]string{}
	for _, item := range s.chatOperations(linked) {
		rc, ok := item["receipt"].(operationReceipt)
		if !ok {
			t.Fatalf("chat operation without a receipt: %+v", item["record"])
		}
		chat[rc.OperationID] = mustJSON(t, rc)
	}
	// Task: the panel payload, through the HTTP handler.
	pw := httptest.NewRecorder()
	s.handleTaskPanel(pw, httptest.NewRequest("GET", "/api/tasks/panel?id="+task, nil))
	var panel struct {
		Receipts []json.RawMessage `json:"receipts"`
	}
	if err := json.Unmarshal(pw.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	taskView := receiptsByID(t, panel.Receipts)
	// Feed: the settled lane's endpoint.
	fw := httptest.NewRecorder()
	s.handleOperationReceipts(fw, httptest.NewRequest("GET", "/api/manifest/operations/receipts", nil))
	var feed struct {
		Receipts []json.RawMessage `json:"receipts"`
		Total    int               `json:"total"`
	}
	if err := json.Unmarshal(fw.Body.Bytes(), &feed); err != nil || fw.Code != 200 {
		t.Fatalf("feed receipts: %d %s", fw.Code, fw.Body.String())
	}
	feedView := receiptsByID(t, feed.Receipts)

	for _, id := range []string{sent, declined} {
		if chat[id] == "" || chat[id] != taskView[id] || chat[id] != feedView[id] {
			t.Fatalf("receipt %s differs across surfaces:\nchat %s\ntask %s\nfeed %s", id, chat[id], taskView[id], feedView[id])
		}
	}
	if _, onTask := taskView[elsewhere]; onTask || feedView[elsewhere] == "" || len(taskView) != 2 || feed.Total != 3 {
		t.Fatalf("linkage: task %v, feed %v (total %d)", receiptIDs(taskView), receiptIDs(feedView), feed.Total)
	}

	var got operationReceipt
	json.Unmarshal([]byte(feedView[declined]), &got)
	if got.Status != "rejected" || got.DecidedBy != "owner:local" || got.DecidedAt.IsZero() || got.ProposalID != manifestmcp.ProposalID(declined) {
		t.Fatalf("rejected receipt: %+v", got)
	}
	json.Unmarshal([]byte(feedView[sent]), &got)
	if got.Status != "succeeded" || got.DecidedBy != "owner:local" || got.Result["threadId"] != "thread-fixture" || got.Conversation != linked {
		t.Fatalf("sent receipt: %+v", got)
	}

	// Immutable: later activity on the record (a re-executed approval is
	// refused as already settled, observation runs again) changes nothing.
	adapter.Execute(context.Background(), sent)
	s.syncManifestOperations()
	again := receiptsByID(t, func() []json.RawMessage {
		w := httptest.NewRecorder()
		s.handleOperationReceipts(w, httptest.NewRequest("GET", "/", nil))
		var out struct {
			Receipts []json.RawMessage `json:"receipts"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Receipts
	}())
	for id, v := range feedView {
		if again[id] != v {
			t.Fatalf("receipt %s changed after settlement:\nwas %s\nnow %s", id, v, again[id])
		}
	}
	if sends != 2 {
		t.Fatal("reading receipts replayed a send")
	}
}

func receiptsByID(t *testing.T, raw []json.RawMessage) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range raw {
		var rc operationReceipt
		if err := json.Unmarshal(r, &rc); err != nil {
			t.Fatal(err)
		}
		out[rc.OperationID] = mustJSON(t, rc)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func receiptIDs(m map[string]string) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The same receipts rendered: chat card, task section and Feed's settled lane
// share one data-operation-id, offer no decision, and name an unreadable lane
// (testdata/operation-receipts.cjs).
func TestOperationReceiptsBrowserUI(t *testing.T) {
	runFixture(t, "operation-receipts.cjs", true, false)
}
