package server

import (
	"manifest/agentchat"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeInterruptCancelsRunnerAndQueue(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	s, st, _ := agentChatFixture(t, "#!/bin/sh\nprintf started > '"+marker+"'\nexec sleep 60\n")
	id, _ := st.Create("alfred", "", "", "")
	st.Accept("alfred", id, "active-request", "first")
	st.Accept("alfred", id, "queued-request", "second")
	s.startAgentChatDelivery("alfred", id)
	deadline := time.Now().Add(3 * time.Second)
	for {
		d, _ := st.Receipt("alfred", id, "active-request")
		_, started := os.Stat(marker)
		if d.ToolScope != nil && started == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dispatch never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancelRequest := httptest.NewRequest("POST", "/", strings.NewReader(`{"requestId":"queued-request"}`))
	cancelRequest.SetPathValue("agent", "alfred")
	cancelRequest.SetPathValue("id", id)
	cancelled := httptest.NewRecorder()
	s.handleAgentChatCancelQueued(cancelled, cancelRequest)
	if cancelled.Code != 200 {
		t.Fatal(cancelled.Code, cancelled.Body.String())
	}
	active, _ := st.Receipt("alfred", id, "active-request")
	if active.State != agentchat.DeliveryRunning || active.StopRequested {
		t.Fatal("queued cancellation touched active turn", active)
	}
	interrupt := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"requestId":"active-request"}`))
		r.SetPathValue("agent", "alfred")
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleAgentChatInterrupt(w, r)
		return w
	}
	if w := interrupt(); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	sess := waitIdle(t, st, "alfred", id)
	if sess.Deliveries[0].State != agentchat.DeliveryInterrupted || sess.Deliveries[1].State != agentchat.DeliveryCancelled || sess.Deliveries[1].Text != "second" {
		t.Fatal(sess.Deliveries)
	}
	if w := interrupt(); w.Code != 200 {
		t.Fatal("lost-response retry", w.Code)
	}
	after, _, _, _ := st.Get("alfred", id)
	if after.Turns != sess.Turns {
		t.Fatal("retry appended turns")
	}
}
