package agentchat

import (
	"encoding/json"
	"testing"
)

// The optional construction context leaves every existing delivery
// fingerprint unchanged: nil contexts hash the text alone, and contexts
// without a construction step hash exactly the pre-construction shape.
func TestConstructionContextLeavesFingerprintsUnchanged(t *testing.T) {
	if deliveryFingerprint("hello", nil) != fingerprint("hello") {
		t.Fatal("a nil context hashes the text alone")
	}
	type legacyContext struct {
		ExplicitArtifacts bool                `json:"explicitArtifacts,omitempty"`
		Recipient         *Recipient          `json:"recipient,omitempty"`
		Conversation      string              `json:"conversation"`
		Task              string              `json:"task,omitempty"`
		Agent             string              `json:"agent"`
		Artifacts         []ArtifactReference `json:"artifacts,omitempty"`
	}
	ctx := &MessageContext{Conversation: "hermes:alfred/c1", Agent: "alfred", Task: "t1", Recipient: &Recipient{Agent: "alfred", Model: "m"},
		Artifacts: []ArtifactReference{{ID: "a1", Revision: "r1"}}}
	legacy := &legacyContext{Conversation: ctx.Conversation, Agent: ctx.Agent, Task: ctx.Task, Recipient: ctx.Recipient, Artifacts: ctx.Artifacts}
	b, _ := json.Marshal(struct {
		Text    string
		Context *legacyContext
	}{"hello", legacy})
	if deliveryFingerprint("hello", ctx) != fingerprint(string(b)) {
		t.Fatal("a context without a construction step keeps its pre-construction fingerprint")
	}
	with := *ctx
	with.Construction = &ConstructionContext{Subject: "home:home", ProblemID: "cp-x", Stage: "extract", PacketHash: "h", ToolScope: "none"}
	if deliveryFingerprint("hello", &with) == deliveryFingerprint("hello", ctx) {
		t.Fatal("a construction step is part of the accepted identity")
	}
}
