package server

import (
	"net/http"
	"testing"
)

func TestNativeConversationCapabilities(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	id, err := st.Create("alfred", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, before, _, _ := st.Get("alfred", id)
	code, response := agentChatJSON(t, s, "GET", "/api/agents/chat/alfred/sessions/"+id, nil)
	if code != http.StatusOK {
		t.Fatal(code, response)
	}
	caps, ok := response["capabilities"].(map[string]any)
	if !ok || caps["adapter"] != "hermes-oneshot" || caps["queue"] != "durable" || caps["cancelQueued"] != true || caps["interrupt"] != "request-and-cancel-queued" {
		t.Fatal("missing supported native controls", caps)
	}
	if caps["liveSteering"] != false || caps["structuredQuestions"] != false || caps["skillInventory"] != "not-reported" {
		t.Fatal("invented adapter support or skill telemetry", caps)
	}
	// Reading capabilities must not manufacture run evidence.
	session, body, _, found := st.Get("alfred", id)
	if !found || len(session.Deliveries) != 0 || body != before {
		t.Fatal("capability read changed conversation", session, body, found)
	}
}
