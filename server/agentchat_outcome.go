package server

import (
	"manifest/agentchat"
	"manifest/ledger"
)

// The delivery is authoritative. In particular, a successful runner return
// racing an owner stop is still interrupted, and a failed save is not an outcome.
func (s *Server) recordAgentChatOutcome(agent, id, requestID string, recipient agentchat.Recipient, reply string, usd float64) {
	receipt, ok := s.agentChat.store.Receipt(agent, id, requestID)
	if !ok {
		return
	}
	entry, ok := agentChatOutcomeEntry(agent, id, receipt, recipient, reply, usd)
	if ok {
		s.ledger(entry)
	}
}

func agentChatOutcomeEntry(agent, id string, d agentchat.Delivery, recipient agentchat.Recipient, reply string, usd float64) (ledger.Entry, bool) {
	e := ledger.Entry{Source: "run", Actor: "agent:" + recipient.Agent, Object: ledger.Object{Kind: ledger.ObjSession, ID: id}, Session: id, Harness: "hermes"}
	switch d.State {
	case agentchat.DeliveryCompleted:
		e.Source = "chat"
		e.Kind = "chat.assistant"
		e.Text = ledger.Snip(reply, 280)
	case agentchat.DeliveryFailed:
		e.Kind = "run.failed"
		e.Text = "chat turn failed — " + d.Error
	case agentchat.DeliveryInterrupted:
		e.Kind = "run.interrupted"
		e.Text = d.Error
	default:
		return ledger.Entry{}, false
	}
	e.Meta = map[string]any{"agent": recipient.Agent, "sourceAgent": agent, "profile": recipient.Profile, "requestId": d.ID, "deliveryState": d.State, "userTurn": d.UserTurn, "replyTurn": d.ReplyTurn, "selectedModel": recipient.Model, "requestedModel": recipient.RequestedModel, "spentUsd": usd}
	if d.Result != nil {
		e.Meta["sessionId"] = d.Result.SessionID
		e.Meta["model"] = d.Result.ReportedModel
	}
	return e, true
}
