package server

import "testing"

func TestAgentChatRecordsDispatchedToolScope(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	s.hermes.readTools = "web,files"
	id, _ := st.Create("alfred", "", "", "")
	if _, err := st.Accept("alfred", id, "scope-request", "inspect only"); err != nil {
		t.Fatal(err)
	}
	s.startAgentChatDelivery("alfred", id)
	session := waitIdle(t, st, "alfred", id)
	if len(session.Deliveries) != 1 || session.Deliveries[0].ToolScope == nil || session.Deliveries[0].ToolScope.Toolsets != "web,files" || session.Deliveries[0].ToolScope.Source != "request" {
		t.Fatal(session.Deliveries)
	}
	s.hermes.readTools = "changed"
	got, _ := st.Receipt("alfred", id, "scope-request")
	if got.ToolScope.Toolsets != "web,files" {
		t.Fatal("recorded dispatch followed config")
	}
}
