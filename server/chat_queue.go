package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
	"time"

	"manifest/agentchat"
	"manifest/chatstate"
)

// The existing durable delivery outbox is also the editable follow-up queue.
// The event hub drains it promptly; AgentLoopTicker drains it without a browser.
// Both use the same CAS claim as Steer/Edit/Remove, then the ordinary input
// handler (access, attachment, runtime-identity and receipt checks included).
func terminalReadyForFollowup(ob terminalObservation) bool {
	return ob.Connectivity == "connected" && ob.Process == "running" && (ob.AgentState == "idle" || ob.AgentState == "done")
}

type chatQueuedFollowup struct {
	StateKey        string        `json:"stateKey"`
	Scope           string        `json:"scope"`
	Agent           string        `json:"agent"`
	URL             string        `json:"url"`
	At              string        `json:"at"`
	Staged          bool          `json:"staged"`
	WaitingForAgent bool          `json:"waitingForAgent"`
	StagedError     string        `json:"stagedError"`
	Payload         terminalInput `json:"payload"`
}
type chatQueueValue struct {
	Items map[string]json.RawMessage `json:"items"`
}

func (s *Server) chatQueuedFollowupSweep() {
	if s.chatState == nil || s.terminal == nil || !s.chatQueueMu.TryLock() {
		return
	}
	defer s.chatQueueMu.Unlock()
	snapshots, err := s.chatState.List("deliveries")
	if err != nil {
		return
	}
	type entry struct {
		snapshot chatstate.Snapshot
		item     chatQueuedFollowup
		raw      json.RawMessage
		target   string
	}
	var entries []entry
	for _, snapshot := range snapshots {
		var value chatQueueValue
		if json.Unmarshal(snapshot.Value, &value) != nil {
			continue
		}
		for id, raw := range value.Items {
			var item chatQueuedFollowup
			if json.Unmarshal(raw, &item) != nil || item.StateKey != snapshot.Key || item.Payload.RequestID != id || !agentchat.ValidRequestID(id) {
				continue
			}
			target := strings.TrimSuffix(strings.TrimPrefix(item.URL, "/api/terminal/session/"), "/input")
			if !termIDRe.MatchString(target) || item.URL != "/api/terminal/session/"+target+"/input" {
				continue
			}
			entries = append(entries, entry{snapshot, item, raw, target})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].item.At == entries[j].item.At {
			return entries[i].item.Payload.RequestID < entries[j].item.Payload.RequestID
		}
		return entries[i].item.At < entries[j].item.At
	})
	seen := map[string]bool{}
	for _, entry := range entries {
		item := entry.item
		if seen[entry.target] {
			continue
		}
		seen[entry.target] = true // one message per recipient, including uncertain sends
		if !item.Staged {
			// A sent receipt survives a crash between delivery and queue cleanup.
			// Reconcile it; absence or uncertainty never authorizes another send.
			receipt, err := s.terminal.readInputReceipt(entry.target, item.Payload.RequestID)
			if err == nil && receipt.State == "sent" && receipt.Fingerprint == item.Payload.fingerprint() {
				s.replaceQueuedFollowup(entry.snapshot.Key, item.Payload.RequestID, entry.raw, nil)
			}
			continue
		}
		if !item.Staged || (item.StagedError != "" && !item.WaitingForAgent) || strings.TrimSpace(item.Payload.Text) == "" || item.Payload.Key != "" || item.Payload.Supervise || len(item.Payload.QuestionAnswers) != 0 {
			continue
		}
		se, ok := s.terminal.find(entry.target)
		if !ok || se.backend() != "herdr" || se.Device != "" || se.Kind != item.Agent || (se.Kind != "claude" && se.Kind != "codex") || s.chatOwnerDeleted("terminal:"+se.Kind+"/"+se.ID) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		ob, err := s.observeTerm(ctx, se)
		if err != nil || !terminalReadyForFollowup(ob) {
			cancel()
			continue
		}
		// Preserve all outbox metadata and context, including fields this dispatcher
		// does not interpret. A crash after claiming is uncertain, never replayed.
		var claimed map[string]any
		if json.Unmarshal(entry.raw, &claimed) != nil {
			cancel()
			continue
		}
		payload := claimed["payload"].(map[string]any)
		payload["afterRun"], payload["steer"] = true, false
		claimed["staged"] = false
		claimedRaw, _ := json.Marshal(claimed)
		if !s.replaceQueuedFollowup(entry.snapshot.Key, item.Payload.RequestID, entry.raw, claimedRaw) {
			cancel()
			continue
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", item.URL, bytes.NewReader(body)).WithContext(ctx)
		req.SetPathValue("id", se.ID)
		rec := httptest.NewRecorder()
		s.handleTermInput(rec, req)
		cancel()
		var result struct {
			Delivery terminalInputReceipt `json:"delivery"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &result) == nil && result.Delivery.State == "sent" {
			s.replaceQueuedFollowup(entry.snapshot.Key, item.Payload.RequestID, claimedRaw, nil)
		} else if strings.Contains(rec.Body.String(), "nothing sent") {
			claimed["staged"] = true
			payload["afterRun"] = false
			if !strings.Contains(rec.Body.String(), "run is not ready") && !strings.Contains(rec.Body.String(), "agent is working") {
				claimed["stagedError"] = strings.TrimSpace(rec.Body.String())
			}
			restored, _ := json.Marshal(claimed)
			s.replaceQueuedFollowup(entry.snapshot.Key, item.Payload.RequestID, claimedRaw, restored)
		}
		// Unknown responses retain the claimed recovery entry for status inspection.
	}
}

func (s *Server) replaceQueuedFollowup(key, id string, before, after json.RawMessage) bool {
	for n := 0; n < 4; n++ {
		snapshot, err := s.chatState.Read(key, "deliveries")
		if err != nil {
			return false
		}
		var value chatQueueValue
		if json.Unmarshal(snapshot.Value, &value) != nil {
			return false
		}
		// Compare decoded JSON because the store canonicalizes object key order.
		var current, expected any
		if json.Unmarshal(value.Items[id], &current) != nil || json.Unmarshal(before, &expected) != nil {
			return false
		}
		a, _ := json.Marshal(current)
		b, _ := json.Marshal(expected)
		if !bytes.Equal(a, b) {
			return false
		}
		if after == nil {
			delete(value.Items, id)
		} else {
			value.Items[id] = after
		}
		raw, _ := json.Marshal(value)
		_, err = s.chatState.Write(key, "deliveries", snapshot.Revision, raw)
		if err == chatstate.ErrConflict {
			continue
		}
		return err == nil
	}
	return false
}
