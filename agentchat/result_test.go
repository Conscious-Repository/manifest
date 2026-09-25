package agentchat

import "testing"

func TestDeliveryResultSurvivesLaterTurnsAndRestart(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	ctx := &MessageContext{Recipient: &Recipient{Agent: "alfred", Model: "requested-model"}}
	first, err := s.Accept("alfred", id, "first-result", "first", ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Claim("alfred", id)
	result := InvocationResult{SessionID: "first-session", ReportedModel: "reported-first"}
	if err := s.FinishWithResult("alfred", id, "first-result", "alfred", "answer", DeliveryCompleted, "", 0, result); err != nil {
		t.Fatal(err)
	}
	s.Accept("alfred", id, "second-result", "second")
	s.Claim("alfred", id)
	s.FinishWithResult("alfred", id, "second-result", "alfred", "answer", DeliveryCompleted, "", 0, InvocationResult{SessionID: "second-session", ReportedModel: "reported-second"})
	fresh := New(s.Root())
	// A repeated completion cannot replace evidence, even after a newer turn.
	if err := fresh.FinishWithResult("alfred", id, "first-result", "alfred", "changed", DeliveryCompleted, "", 0, InvocationResult{SessionID: "forged", ReportedModel: "changed"}); err != nil {
		t.Fatal(err)
	}
	got, _ := fresh.Receipt("alfred", id, "first-result")
	if got.Result == nil || *got.Result != result || got.Fingerprint != first.Delivery.Fingerprint || got.Context.Recipient.Model != "requested-model" {
		t.Fatal(got)
	}
	sess, _, _, _ := fresh.Get("alfred", id)
	if sess.HermesSession != "second-session" || sess.Turns != 4 {
		t.Fatal(sess)
	}
	if retry, err := fresh.Accept("alfred", id, "first-result", "first", ctx); err != nil || retry.New {
		t.Fatal(retry, err)
	}
}

func TestDeliveryResultDoesNotInferMissingTelemetry(t *testing.T) {
	for _, outcome := range []string{DeliveryFailed, DeliveryInterrupted, DeliveryCompleted} {
		t.Run(outcome, func(t *testing.T) {
			s := New(t.TempDir())
			id, _ := s.Create("alfred", "", "", "requested-model")
			s.Accept("alfred", id, "empty-result", "first")
			s.Claim("alfred", id)
			state := outcome
			if state == DeliveryInterrupted {
				s.RequestStop("alfred", id, "empty-result")
				state = DeliveryFailed
			}
			if err := s.FinishWithResult("alfred", id, "empty-result", "system", "returned", state, "", 0, InvocationResult{}); err != nil {
				t.Fatal(err)
			}
			got, _ := New(s.Root()).Receipt("alfred", id, "empty-result")
			if got.State != outcome || got.Result == nil || *got.Result != (InvocationResult{}) {
				t.Fatal(got)
			}
		})
	}
}

func TestPreflightAndLegacyCompletionHaveNoResult(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	s.Accept("alfred", id, "legacy-result", "first")
	s.Claim("alfred", id)
	if err := s.Finish("alfred", id, "legacy-result", "system", "no invocation", DeliveryFailed, "", 0); err != nil {
		t.Fatal(err)
	}
	got, _ := New(s.Root()).Receipt("alfred", id, "legacy-result")
	if got.Result != nil {
		t.Fatal(got)
	}
}
