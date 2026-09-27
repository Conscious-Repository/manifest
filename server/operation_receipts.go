package server

import (
	"net/http"
	"sort"
	"time"

	"manifest/agentchat"
	"manifest/manifestmcp"
)

// Settled approval receipts (owner decision D1, 2026-09-27): one receipt,
// reachable from chat, task and Feed. Every surface projects the same
// operation record through operationReceipt; nothing here writes, so the
// canonical proposal stays the one write path and there is no second store.

// operationReceipt is the settled outcome of one human-approval operation.
// It carries only settlement fields, so later record activity (reply
// tracking, re-observation) cannot change what a surface shows.
type operationReceipt struct {
	OperationID  string             `json:"operationId"`
	ProposalID   string             `json:"proposalId"`
	Tool         string             `json:"tool"`
	Action       string             `json:"action"`
	Status       string             `json:"status"`
	Conversation string             `json:"conversation,omitempty"`
	DecidedBy    string             `json:"decidedBy,omitempty"`
	DecidedAt    time.Time          `json:"decidedAt,omitzero"`
	SettledAt    time.Time          `json:"settledAt"`
	Result       manifestmcp.Object `json:"result,omitempty"`
	Error        string             `json:"error,omitempty"`
}

// settledStatuses end an approval: executed (succeeded, partial, failed),
// declined (rejected, cancelled) or invalidated before a decision (stale).
// approved and executing are in flight, not settled.
var settledStatuses = map[string]bool{"succeeded": true, "partial": true, "failed": true, "rejected": true, "cancelled": true, "stale": true}

func settledApproval(o *manifestmcp.OperationRecord) bool {
	return o != nil && o.Policy == "human_approval" && settledStatuses[o.Status]
}

func operationReceiptOf(o *manifestmcp.OperationRecord) operationReceipt {
	p := manifestmcp.Proposal(o)
	rc := operationReceipt{OperationID: o.ID, ProposalID: p.ID, Tool: o.Tool, Action: p.Action, Status: o.Status,
		Conversation: o.Conversation, Result: o.Result, Error: o.Error, SettledAt: o.CreatedAt}
	for _, t := range o.History {
		switch t.Status {
		case "approved", "rejected", "cancelled":
			rc.DecidedBy, rc.DecidedAt = t.Actor, t.At
		}
		if t.Status == o.Status {
			rc.SettledAt = t.At
		}
	}
	if rc.DecidedBy == "" {
		rc.DecidedBy = o.ApprovalActor
	}
	return rc
}

// chatOperationItem is one operation as a chat surface shows it: the record,
// its canonical proposal and, once settled, the receipt task and Feed show.
func chatOperationItem(o *manifestmcp.OperationRecord) map[string]any {
	item := map[string]any{"record": o, "proposal": manifestmcp.Proposal(o)}
	if settledApproval(o) {
		item["receipt"] = operationReceiptOf(o)
	}
	return item
}

// operationReceipts are every settled approval, newest settlement first.
func (s *Server) operationReceipts() []operationReceipt {
	out := []operationReceipt{}
	for _, o := range s.syncManifestOperations() {
		if settledApproval(o) {
			out = append(out, operationReceiptOf(o))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SettledAt.After(out[j].SettledAt) })
	return out
}

// operationSession finds the conversation an operation was prepared in. An
// operation names only a session id; an id found under two agents is
// ambiguous and links nowhere rather than to either.
func (s *Server) operationSession(id string) (agentchat.Session, bool) {
	if s.agentChat == nil || id == "" || !agentchat.ValidID(id) {
		return agentchat.Session{}, false
	}
	var found agentchat.Session
	n := 0
	for _, agent := range s.agentChat.store.Agents() {
		if sess, _, _, ok := s.agentChat.store.Get(agent, id); ok {
			found, n = sess, n+1
		}
	}
	return found, n == 1
}

// taskOperationReceipts are the settled approvals prepared in a conversation
// linked to the task, by the same rule that puts the task's proposals in that
// chat (chatTaskMatcher): an explicit thread origin wins over the session's
// task pointer.
func (s *Server) taskOperationReceipts(task string) []operationReceipt {
	out := []operationReceipt{}
	for _, rc := range s.operationReceipts() {
		if sess, ok := s.operationSession(rc.Conversation); ok && s.chatTaskMatcher(sess)(task) {
			out = append(out, rc)
		}
	}
	return out
}

// operationReceiptsLimit bounds the Feed lane; the total says how many exist.
const operationReceiptsLimit = 20

// handleOperationReceipts serves Feed's settled-approvals lane.
func (s *Server) handleOperationReceipts(w http.ResponseWriter, r *http.Request) {
	if s.manifestOperations == nil {
		http.Error(w, "operation receipts unavailable", http.StatusServiceUnavailable)
		return
	}
	all := s.operationReceipts()
	shown := all
	if len(shown) > operationReceiptsLimit {
		shown = shown[:operationReceiptsLimit]
	}
	writeJSON(w, map[string]any{"receipts": shown, "total": len(all)})
}
