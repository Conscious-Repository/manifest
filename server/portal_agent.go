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
	"strings"
	"time"

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
