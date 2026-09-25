package server

import (
	"fmt"
	"manifest/agentchat"
	"manifest/artifacts"
	"net/http"
	"strings"
)

type chatOutput struct {
	Delivery  string `json:"delivery"`
	ReplyTurn int    `json:"replyTurn"`
	Hash      string `json:"hash"`
	body      string
	actor     string
}

// Only completed, receipt-backed native replies can become retained outputs.
// Listing candidates is read-only; the owner explicitly chooses what to retain.
func chatOutputs(sess agentchat.Session, body string) []chatOutput {
	out := []chatOutput{}
	if sess.Sharing != nil {
		return out
	}
	for _, turn := range agentchat.ParseTurns(body) {
		if turn.Who == "user" || turn.Who == "system" {
			continue
		}
		text := strings.TrimSpace(agentchat.SayBody(turn.Text))
		if text == "" || text == "(no reply)" {
			continue
		}
		for _, d := range sess.Deliveries {
			if d.State == agentchat.DeliveryCompleted && d.ReplyTurn == turn.N {
				out = append(out, chatOutput{d.ID, turn.N, artifacts.Hash([]byte(text)), text, turn.Who})
			}
		}
	}
	return out
}

func (s *Server) handleChatOutput(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	if s.artifactReg == nil {
		http.Error(w, "artifact registry unavailable", 503)
		return
	}
	agent, id := r.PathValue("agent"), r.PathValue("id")
	release, err := s.chatShareMutation(agent, id)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	defer release()
	var request struct{ Delivery, Hash string }
	if err := decode(r, &request); err != nil {
		httpError(w, err)
		return
	}
	sess, body, _, ok := s.agentChat.store.Get(agent, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	for _, output := range chatOutputs(sess, body) {
		if output.Delivery != request.Delivery {
			continue
		}
		if request.Hash != output.Hash {
			http.Error(w, "The reply changed. Reopen its current version.", 409)
			return
		}
		provenance := artifacts.Provenance{Source: "chat-output", Session: sessionConversation(sess).Key, Delivery: output.Delivery}
		actor := output.actor
		for _, d := range sess.Deliveries {
			if d.ID != output.Delivery || d.Context == nil {
				continue
			}
			provenance.Task = d.Context.Task
			for _, ref := range d.Context.Artifacts {
				provenance.Inputs = append(provenance.Inputs, ref.ID)
			}
		}
		identity := artifacts.Hash([]byte(agent + "/" + id + "/" + output.Delivery + "/" + output.Hash))
		result, err := s.artifactReg.Retain(artifacts.Put{Kind: artifacts.KindReport, Title: fmt.Sprintf("%s output · turn %d", actor, output.ReplyTurn), Harness: agent, Ref: "artifacts/chat-outputs/" + identity + ".md", Content: []byte(output.body), Actor: actor, Provenance: provenance})
		if err != nil {
			httpError(w, err)
			return
		}
		s.artifactEvent(result, actor)
		writeJSON(w, map[string]any{"id": result.Artifact.ID, "revision": result.Revision.Hash, "task": provenance.Task})
		return
	}
	http.NotFound(w, r)
}
