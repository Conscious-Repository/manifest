package server

import (
	"manifest/agentchat"
	"strings"
	"testing"
)

func TestAgentChatRetainsReportedResult(t *testing.T) {
	for _, tc := range []struct{ name, script, state, model, session string }{
		{"completed", echoStub, agentchat.DeliveryCompleted, "stub-model", "20260904_140000_ab12cd"},
		{"reported failure", strings.Replace(echoStub, `"model":"stub-model"`, `"failed":true,"model":"stub-model"`, 1), agentchat.DeliveryFailed, "stub-model", "20260904_140000_ab12cd"},
		{"missing telemetry", "#!/bin/sh\necho reply\n", agentchat.DeliveryCompleted, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, led := agentChatFixture(t, tc.script)
			id, _ := st.Create("alfred", "", "", "requested-model")
			ctx := &agentchat.MessageContext{Recipient: &agentchat.Recipient{Agent: "alfred", Model: "requested-model"}}
			st.Accept("alfred", id, "result-request", "inspect", ctx)
			s.startAgentChatDelivery("alfred", id)
			sess := waitIdle(t, st, "alfred", id)
			got := sess.Deliveries[0]
			if got.State != tc.state || got.Result == nil || got.Result.ReportedModel != tc.model || got.Result.SessionID != tc.session || got.Context.Recipient.Model != "requested-model" {
				t.Fatal(got)
			}
			event := waitAgentChatOutcome(t, led, "result-request")
			wantKind := "chat.assistant"
			if tc.state == agentchat.DeliveryFailed {
				wantKind = "run.failed"
			}
			if event.Kind != wantKind || event.Meta["deliveryState"] != tc.state || event.Meta["model"] != tc.model || event.Meta["selectedModel"] != "requested-model" || event.Meta["sessionId"] != tc.session {
				t.Fatal(event)
			}
			recovered, _ := agentchat.New(st.Root()).Receipt("alfred", id, "result-request")
			if recovered.Result == nil || *recovered.Result != *got.Result {
				t.Fatal(recovered)
			}
		})
	}
}
