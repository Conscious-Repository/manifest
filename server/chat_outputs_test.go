package server

import (
	"manifest/agentchat"
	"manifest/artifacts"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetainNativeOutputExactReceipt(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	ws, _ := workspaceFixture(t)
	s.UseArtifactRegistry(ws.artifactReg)
	id, err := st.Create("alfred", "", "Research", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &agentchat.MessageContext{Recipient: &agentchat.Recipient{Agent: "alfred"}}
	if _, err := st.Accept("alfred", id, "output-request-001", "Research this", ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Claim("alfred", id); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := st.Finish("alfred", id, "output-request-001", "alfred", "### Step 1 — say\n\nOriginal findings", agentchat.DeliveryCompleted, "", 0); err != nil {
		t.Fatal(err)
	}
	sess, body, _, _ := st.Get("alfred", id)
	outputs := chatOutputs(sess, body)
	if len(outputs) != 1 || outputs[0].body != "Original findings" {
		t.Fatal(outputs)
	}
	if len(s.artifactReg.List(artifacts.Filter{})) != 0 {
		t.Fatal("projection retained output without owner action")
	}
	endpoint := "/api/agents/chat/alfred/sessions/" + id + "/output"
	request := map[string]any{"delivery": outputs[0].Delivery, "hash": outputs[0].Hash}
	code, ref := agentChatJSON(t, s, "POST", endpoint, request)
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, ok := s.artifactReg.Get(ref["id"].(string))
	if !ok || a.Provenance.Session != sessionConversation(sess).Key || a.Provenance.Delivery != "output-request-001" || a.Provenance.Run != "" || len(a.Revisions) != 1 {
		t.Fatal(a)
	}
	links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]
	if len(links) != 1 || links[0].Route != sessionConversation(sess).Route {
		t.Fatal(links)
	}
	if got, _ := s.searchArtifacts([]artifacts.Artifact{a}, "output-request-001"); len(got) != 1 {
		t.Fatal("delivery not searchable")
	}
	newer, err := s.artifactReg.Put(artifacts.Put{ID: a.ID, Content: []byte("Owner revised findings")})
	if err != nil {
		t.Fatal(err)
	}
	code, retry := agentChatJSON(t, s, "POST", endpoint, request)
	if code != 200 || retry["revision"] != ref["revision"] {
		t.Fatal(code, retry)
	}
	current, _ := s.artifactReg.Get(a.ID)
	if current.Head != newer.Artifact.Head || len(current.Revisions) != 2 || current.Provenance.Delivery != a.Provenance.Delivery {
		t.Fatal("retry replaced edited head or lost provenance", current)
	}
	request["hash"] = artifacts.Hash([]byte("Changed"))
	if code, _ := agentChatJSON(t, s, "POST", endpoint, request); code != 409 {
		t.Fatal("stale selection accepted", code)
	}
	request["hash"] = outputs[0].Hash
	request["delivery"] = "unrelated-request"
	if code, _ := agentChatJSON(t, s, "POST", endpoint, request); code != 404 {
		t.Fatal(code)
	}
	_, after, _, _ := st.Get("alfred", id)
	if after != body {
		t.Fatal("capture rewrote source")
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", endpoint, strings.NewReader(`{}`)))
	if w.Code == 200 {
		t.Fatal("public capture exposed")
	}
}

func TestNativeOutputRequiresCompletedPrivateReply(t *testing.T) {
	body := "## Turn 2 — alfred · 2026-09-25T10:00:00Z\n\nReply"
	sess := agentchat.Session{Deliveries: []agentchat.Delivery{{ID: "request-one", State: agentchat.DeliveryCompleted, ReplyTurn: 2}}}
	if len(chatOutputs(sess, body)) != 1 {
		t.Fatal("completed reply missing")
	}
	sess.Sharing = &agentchat.ShareState{State: "shared"}
	if len(chatOutputs(sess, body)) != 0 {
		t.Fatal("shared source offered private capture")
	}
	sess.Sharing = nil
	if len(chatOutputs(sess, strings.Replace(body, "Reply", "(no reply)", 1))) != 0 {
		t.Fatal("empty outcome offered as output")
	}
	for _, state := range []string{agentchat.DeliveryRunning, agentchat.DeliveryFailed, agentchat.DeliveryInterrupted, agentchat.DeliveryCancelled} {
		sess := agentchat.Session{Deliveries: []agentchat.Delivery{{ID: "request-one", State: state, ReplyTurn: 2}}}
		if got := chatOutputs(sess, "## Turn 2 — alfred · 2026-09-25T10:00:00Z\n\nReply"); len(got) != 0 {
			t.Fatal(state, got)
		}
	}
}
