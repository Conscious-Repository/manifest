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
	"time"

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

// AionPanel returns one portal item's plan record + delegation state.
func (s *Server) AionPanel(itemID string) map[string]any {
	id := "aion:" + itemID
	rec := s.readPlanRecord(id)
	out := map[string]any{
		"plan":     rec.Plan,
		"assignee": rec.Assignee,
	}
	if d, ok := s.delegationIndex()[id]; ok {
		out["delegation"] = d
	}
	return out
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
