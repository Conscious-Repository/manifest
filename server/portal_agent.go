package server

// The portal↔cockpit agent bridges (kairos plan Phases C/D). The portal
// listener is a separate mux in the SAME process — these exported closures
// are what main wires into PortalOptions so portal handlers can reach the
// delegation machinery without holding *Server. Portal item id X maps to the
// cockpit todo id "aion:"+X everywhere: the whole existing agent loop
// (mention → auto-assign → relay → persona reply → plan materialization →
// fire → result) then works on portal items unchanged, and agent comments on
// aion: ids already post team-visibly through the shared teamportal store.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"manifest/chatthreads"
	"manifest/mdfm"
	"manifest/teamportal"
	"manifest/threads"
)

// AionThreadHook is the portal's comment dialog hook: mention → auto-assign +
// relay; agent-assigned item → every comment relays. Same semantics as the
// dashboard composer.
func (s *Server) AionThreadHook(itemID string, mentions []string, text string) {
	s.threadDialogHook("aion:"+itemID, mentions, text)
}

// AionTeamAgents is the portal roster: TEAM-surface harnesses only (kairos —
// hermes stays personal; owner decision 2026-08-16).
func (s *Server) AionTeamAgents() []map[string]any { return s.teamAgentRoster() }

// AionPanel returns one portal item's plan record + delegation state
// (portal-v2: the whole PLAN block payload — description section, record
// provenance, and the agent-held flag).
func (s *Server) AionPanel(itemID string) map[string]any {
	id := "aion:" + itemID
	rec := s.readPlanRecord(id)
	out := map[string]any{
		"plan":     rec.Plan,
		"assignee": rec.Assignee,
		"exists":   rec.Exists,
		"state":    rec.State,
		"rel":      rec.Rel,
		"held":     strings.HasPrefix(rec.Assignee, "agent:"),
	}
	if rec.Exists && s.vault != nil {
		if raw, err := s.vault.ReadVaultFile(rec.Rel); err == nil {
			_, body := mdfm.Split(string(raw))
			out["description"] = sectionBody(body, "description")
		}
		// provenance the record itself carries: mtime only — the record tracks
		// no editor name (flagged in the v2 plan; a by/at line is a follow-up)
		if fi, err := os.Stat(filepath.Join(s.vault.VaultRoot(), filepath.FromSlash(rec.Rel))); err == nil {
			out["planAt"] = fi.ModTime().UTC().Format(time.RFC3339)
		}
		if p := strings.TrimSpace(rec.Plan); p != "" {
			out["planLines"] = len(strings.Split(p, "\n"))
		}
	}
	if d, ok := s.delegationIndex()[id]; ok {
		out["delegation"] = d
	}
	return out
}

// AionActivity returns one item's slice of the team activity trail — the
// stream's "changed" rows (patches, agent assigns/fires, proposal decisions).
func (s *Server) AionActivity(itemID string) []map[string]any {
	out := []map[string]any{}
	if s.threads == nil || s.threads.aion == nil {
		return out
	}
	for _, e := range s.threads.aion.Activity(time.Time{}) {
		item, _ := e.Payload["item"].(string)
		if item != itemID {
			continue
		}
		out = append(out, map[string]any{
			"ts": e.TS, "actor": e.Actor, "action": e.Action, "payload": e.Payload,
		})
	}
	return out
}

// AionPlanWrite writes ONE plan-record section from the portal — the same
// surgical section swap the dashboard uses (`todo-plans` capability), so an
// Obsidian hand-edit to the other section never collides. The assignee/admin
// gate is enforced by the portal handler (it owns the email→initials mapping).
func (s *Server) AionPlanWrite(itemID, section, text string) error {
	if section != "plan" && section != "description" {
		return errBadRequest("unknown section " + section)
	}
	return s.writePlanSection("todo-plans", "aion:"+itemID, section, strings.TrimRight(text, "\n"))
}

// AionFileBlob resolves a comment-attachment hash to its stored path ("" when
// absent) — the portal's archive/stream open attachments through this.
func (s *Server) AionFileBlob(hash string) string {
	if s.threads == nil || s.threads.aionFS == nil {
		return ""
	}
	return s.threads.aionFS.BlobPath(hash)
}

// AionAssign assigns an agent on a portal item, attributed to the member.
// The team activity trail records it (→ the owner's FEED via the bridge).
func (s *Server) AionAssign(itemID, owner, memberEmail, memberName string) error {
	_, err := s.assignTask(threads.Identity{ID: memberEmail, Name: memberName}, "aion:"+itemID, owner)
	if err == nil && s.threads != nil && s.threads.aion != nil {
		_ = s.threads.aion.LogAction(teamportal.Identity{Email: memberEmail, Name: memberName},
			"agent-assign", map[string]any{"item": itemID, "owner": owner}, time.Now())
	}
	return err
}

// AionFire executes an agent-held plan from the portal, attributed to the
// member. spirits.ErrAlreadyActive passes through for the 409; a successful
// fire lands in the team activity trail (→ the owner's FEED notice).
func (s *Server) AionFire(itemID, memberEmail, memberName string) error {
	err := s.fireTask(threads.Identity{ID: memberEmail, Name: memberName}, "aion:"+itemID)
	if err == nil && s.threads != nil && s.threads.aion != nil {
		_ = s.threads.aion.LogAction(teamportal.Identity{Email: memberEmail, Name: memberName},
			"agent-fire", map[string]any{"item": itemID}, time.Now())
	}
	return err
}

// --- native chat with kairos (chat-kairos handoff) --------------------------

// AionChatThreads returns the whole chat surface payload — threads, their
// messages, and the engine snapshot — running a read-driven sweep first so a
// just-finished kairos run appears without waiting for the 60s ticker.
func (s *Server) AionChatThreads() map[string]any {
	s.chatSweep()
	out := map[string]any{"threads": []any{}, "messages": map[string]any{}, "engine": s.chatEngine()}
	if s.chat == nil {
		return out
	}
	ths := s.chat.Threads()
	out["threads"] = ths
	msgs := map[string]any{}
	for _, t := range ths {
		msgs[t.ID] = s.chat.Messages(t.ID)
	}
	out["messages"] = msgs
	return out
}

// AionChatThread is the thread lifecycle: create | rename | rescope | archive |
// reopen (author attributed).
func (s *Server) AionChatThread(op, id, title, rock, memberEmail, memberName string) (map[string]any, error) {
	if s.chat == nil {
		return nil, errBadRequest("chat is not configured")
	}
	who := chatthreads.Identity{ID: memberEmail, Name: memberName}
	now := time.Now()
	switch op {
	case "create":
		if _, err := s.chat.CreateThread(id, strings.ToLower(strings.TrimSpace(title)), rock, who, now); err != nil {
			return nil, err
		}
	case "rename":
		if _, err := s.chat.PatchThread(id, map[string]any{"title": strings.ToLower(strings.TrimSpace(title))}, who, now); err != nil {
			return nil, err
		}
	case "rescope":
		if _, err := s.chat.PatchThread(id, map[string]any{"rock": rock}, who, now); err != nil {
			return nil, err
		}
	case "archive":
		if _, err := s.chat.PatchThread(id, map[string]any{"archived": true}, who, now); err != nil {
			return nil, err
		}
	case "reopen":
		if _, err := s.chat.PatchThread(id, map[string]any{"archived": false}, who, now); err != nil {
			return nil, err
		}
	case "prune":
		s.chat.PruneEmpty(id)
	default:
		return nil, errBadRequest("unknown thread op " + op)
	}
	return s.AionChatThreads(), nil
}

var chatIntentRe = regexp.MustCompile(`@kairos::([a-z0-9-]+)`)

// AionChatAsk spools an ask/delegate run for a chat message. Spool FIRST (the
// runner's IsActive guard is the one-run-at-a-time gate); on success record the
// member's message + the pending entry. ErrAlreadyActive passes through → 409.
func (s *Server) AionChatAsk(thread, text, ritual string, context []string, memberEmail, memberName string) error {
	if s.chat == nil {
		return errBadRequest("chat is not configured")
	}
	ritual = strings.TrimSpace(ritual)
	if ritual != "ask" && ritual != "delegate" {
		ritual = "ask"
	}
	if strings.TrimSpace(text) == "" {
		return errBadRequest("empty message")
	}
	intent := ""
	if m := chatIntentRe.FindStringSubmatch(text); m != nil {
		intent = m[1]
	}
	title := ""
	for _, t := range s.chat.Threads() {
		if t.ID == thread {
			title = t.Title
			break
		}
	}
	orderID, err := s.spoolChatOrder(thread, title, ritual, intent, text, context, memberName)
	if err != nil {
		return err // incl. spirits.ErrAlreadyActive → 409
	}
	now := time.Now()
	_, _ = s.chat.AddMessage(chatthreads.Message{
		Thread: thread, Kind: "ask", Author: memberEmail, AuthName: memberName,
		Text: text, Context: context, At: now,
	}, now)
	_ = s.chat.SetPending(chatthreads.Pending{
		OrderID: orderID, Thread: thread, By: memberName, ByEmail: memberEmail,
		Ritual: ritual, Text: text, At: now,
	}, now)
	return nil
}

// AionChatEngine is the sidebar snapshot (heartbeat, active claim, pending).
func (s *Server) AionChatEngine() map[string]any { return s.chatEngine() }

// AionChatProposal applies or discards a chat proposal. Gate: admin or the
// target item's assignee (the FIELDS rule). Apply routes through the SAME write
// paths as a human edit — teamportal Patch (set-field) / writePlanSection
// (replace-section) — so the ledger is identical except for the actor.
func (s *Server) AionChatProposal(thread, msg string, index int, apply bool, memberEmail, memberName, memberInitials string, admin bool) error {
	if s.chat == nil {
		return errBadRequest("chat is not configured")
	}
	p, ok := s.chat.Proposal(thread, msg, index)
	if !ok {
		return errBadRequest("proposal not found")
	}
	// gate against the target item's owner (initials), same as FIELDS
	owner := s.itemOwnerInitials(p.ItemID)
	if !admin && (owner == "" || !strings.EqualFold(owner, memberInitials)) {
		return errBadRequest("only the item's assignee (" + orDash(owner) + ") or an admin can apply this")
	}
	who := chatthreads.Identity{ID: memberEmail, Name: memberName}
	if apply {
		switch p.Type {
		case "set-field":
			if s.threads == nil || s.threads.aion == nil {
				return errBadRequest("team store not available")
			}
			if _, err := s.threads.aion.Patch(teamportal.Identity{Email: memberEmail, Name: memberName},
				p.ItemID, map[string]string{p.Field: p.Value}, time.Now()); err != nil {
				return err
			}
		case "replace-section":
			if err := s.writePlanSection("todo-plans", "aion:"+p.ItemID, p.Section, p.Body); err != nil {
				return err
			}
		default:
			return errBadRequest("unknown proposal type")
		}
	}
	if _, err := s.chat.DecideProposal(thread, msg, index, apply, who, time.Now()); err != nil {
		return err
	}
	if s.threads != nil && s.threads.aion != nil {
		verb := "chat-proposal-applied"
		if !apply {
			verb = "chat-proposal-discarded"
		}
		_ = s.threads.aion.LogAction(teamportal.Identity{Email: memberEmail, Name: memberName},
			verb, map[string]any{"item": p.ItemID, "type": p.Type}, time.Now())
	}
	return nil
}

// itemOwnerInitials resolves a portal item's owner (initials) — published
// backlog owner, else a team-added item's owner.
func (s *Server) itemOwnerInitials(itemID string) string {
	if s.aion != nil {
		if it := s.aion.LoadBacklog().Find(itemID); it != nil && it.Owner != "" {
			return it.Owner
		}
	}
	if s.threads != nil && s.threads.aion != nil {
		if o, ok := s.threads.aion.TeamOwner(itemID); ok {
			return o
		}
	}
	return ""
}
