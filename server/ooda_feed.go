package server

// The OODA portal FEED's approval lane (ooda-portal email plan, phase 5):
// extractor proposals surfaced to the members they concern, decided from the
// portal through the SAME audited apply lanes the cockpit uses.
//
// Doctrine (§12), amended 2026-08-21 and pinned by
// TestPortalApprovalConfirmIsTheOnlyVaultCrossing: a portal write reaches the
// vault ONLY through approvalsFor(id).Confirm under the predicate below —
// the typed apply lanes' hard path allow-lists still bound WHAT can change,
// vaultwriter capabilities still audit every write, and every non-approval
// portal route still never touches the vault.
//
// The relevance predicate (owner decisions, locked):
//   admin                → sees and decides everything.
//   re-backlog           → visible+decidable when the payload owner resolves
//                          to the caller, or the source artifact came from
//                          the caller's mailbox.
//   re-contract          → visible when any allocation property belongs to
//                          an entity the caller owns into (or the source is
//                          theirs); DECIDABLE only when EVERY allocation
//                          property does — a partner commits money only on
//                          their own entities' books (the bid-lane doctrine,
//                          extended by the owner 2026-08-21).

import (
	"net/http"
	"strings"
	"time"

	"manifest/approvals"
	"manifest/realestate"
)

// oodaFeedTypes: only the RE extraction types surface on the portal — the
// cockpit's private lanes (create-vault-note, goals, errands…) never do.
func oodaFeedType(t string) bool { return t == "re-backlog" || t == "re-contract" }

// oodaApprovalView is one portal-facing approval row: the cockpit's enriched
// row plus the caller's rights.
type oodaApprovalView struct {
	approvalRow
	Decidable bool `json:"decidable"`
}

func (a *oodaAPI) registerFeedRoutes(mux *http.ServeMux) {
	s := a.live.s
	if s == nil || a.opt.Auth == nil {
		return
	}
	mux.HandleFunc("GET /api/ooda/feed", a.feed)
	mux.HandleFunc("POST /api/ooda/approval/{id}", a.approvalDecide)
	mux.HandleFunc("POST /api/ooda/approval/{id}/recontract", a.approvalRecontract)
}

// feed is the portal FEED payload: the caller's Gmail status, their pending
// email candidates, and the approvals the predicate shows them.
func (a *oodaAPI) feed(w http.ResponseWriter, r *http.Request) {
	id, ok := PortalIdentify(a.opt, w, r)
	if !ok {
		return
	}
	s := a.live.s
	admin := a.isOodaAdmin(id.Email)
	out := map[string]any{"admin": admin}

	if s.oodaGmail != nil {
		acc, connected := s.oodaGmail.Status(id.Email)
		out["gmail"] = map[string]any{
			"connected": connected && !acc.NeedsReauth, "needsReauth": acc.NeedsReauth,
		}
	}
	if s.oodaEmail != nil {
		var pending []any
		for _, c := range s.oodaEmail.List("pending") {
			if admin || strings.EqualFold(c.Account, id.Email) {
				pending = append(pending, c)
			}
		}
		out["emailPending"] = pending
	}

	var approvals []oodaApprovalView
	snap := a.live.Snapshot()
	for _, row := range s.approvalRows(nil) {
		if !oodaFeedType(row.Type) {
			continue
		}
		visible, decidable := s.oodaApprovalAccess(id.Email, admin, a.opt, row, snap)
		if !visible {
			continue
		}
		approvals = append(approvals, oodaApprovalView{approvalRow: row, Decidable: decidable})
	}
	out["approvals"] = approvals
	writeJSON(w, out)
}

// oodaApprovalAccess is THE authorization predicate — one function, all the
// rules. Returns (visible, decidable) for one enriched approval row.
func (s *Server) oodaApprovalAccess(email string, admin bool, opt PortalOptions, row approvalRow, snap *oodaSnapshot) (bool, bool) {
	if admin {
		return true, true
	}
	initials := ""
	if opt.Store != nil {
		initials = opt.Store.EmailOverrides()[strings.ToLower(email)]
	}
	mine := s.approvalFromMyMailbox(row, email)
	switch row.Type {
	case "re-backlog":
		if row.AionPayload != nil && initials != "" &&
			strings.EqualFold(strings.TrimSpace(row.AionPayload.Owner), initials) {
			return true, true
		}
		return mine, mine
	case "re-contract":
		if row.ReContractPayload == nil || snap == nil {
			return mine, false
		}
		anyMine, allMine := false, len(row.ReContractPayload.Allocations) > 0
		for _, al := range row.ReContractPayload.Allocations {
			if s.callerOwnsProperty(snap, al.Property, initials) {
				anyMine = true
			} else {
				allMine = false
			}
		}
		return anyMine || mine, allMine
	}
	return false, false
}

// approvalFromMyMailbox: the proposal's evidence line names its source
// artifact (sha256:<hash>); when that artifact's provenance is the caller's
// mailbox, the proposal is theirs to see.
func (s *Server) approvalFromMyMailbox(row approvalRow, email string) bool {
	if s.artifacts == nil {
		return false
	}
	hash := oodaArtifactHashIn(row.Body)
	if hash == "" {
		return false
	}
	entry, ok := s.artifacts.Lookup("ooda", hash)
	return ok && strings.EqualFold(entry.ByEmail, email)
}

// callerOwnsProperty: the property's books entity — or any entity up its
// ownership chain — names the caller (matched by people.md name via their
// initials). Depth-bounded; entity-owns-entity edges recurse.
func (s *Server) callerOwnsProperty(snap *oodaSnapshot, propSlug, initials string) bool {
	if initials == "" {
		return false
	}
	name := ""
	for _, p := range snap.People {
		if p != nil && strings.EqualFold(p.Initials, initials) {
			name = p.Name
			break
		}
	}
	if name == "" {
		return false
	}
	entityName := ""
	for _, p := range snap.Properties {
		if strings.EqualFold(p.Slug, propSlug) {
			entityName = p.Entity
			break
		}
	}
	if entityName == "" {
		return false
	}
	return entityChainNames(snap.Entities, entityName, name, 0)
}

// entityChainNames walks an entity's owners; a person ref matching `name`
// wins, an entity ref recurses (the Garden SPE shape: person → LLC → SPE).
func entityChainNames(entities []realestate.Entity, entityName, person string, depth int) bool {
	if depth > 4 {
		return false
	}
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	var ent *realestate.Entity
	for i := range entities {
		if norm(entities[i].Name) == norm(entityName) || norm(entities[i].Slug) == norm(entityName) {
			ent = &entities[i]
			break
		}
	}
	if ent == nil {
		return false
	}
	for _, o := range ent.Owners {
		ref := norm(o.Ref)
		if ref == "" {
			continue
		}
		if ref == norm(person) {
			return true
		}
		// an owner ref naming another entity recurses up the chain
		for i := range entities {
			if norm(entities[i].Name) == ref || norm(entities[i].Slug) == ref {
				if entityChainNames(entities, entities[i].Name, person, depth+1) {
					return true
				}
				break
			}
		}
	}
	return false
}

// approvalDecide confirms or rejects ONE approval from the portal — 403
// unless the predicate says decidable. Confirm crosses into the vault
// through the SAME typed apply lane the cockpit uses; the acting member is
// recorded in the team activity trail.
func (a *oodaAPI) approvalDecide(w http.ResponseWriter, r *http.Request) {
	id, ok := PortalIdentify(a.opt, w, r)
	if !ok {
		return
	}
	s := a.live.s
	var b struct {
		Action string `json:"action"` // confirm | reject
		Reason string `json:"reason"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	row, found := a.findApproval(r.PathValue("id"))
	if !found {
		http.Error(w, "no such proposal", http.StatusNotFound)
		return
	}
	admin := a.isOodaAdmin(id.Email)
	_, decidable := s.oodaApprovalAccess(id.Email, admin, a.opt, row, a.live.Snapshot())
	if !decidable {
		http.Error(w, "this proposal is not yours to decide", http.StatusForbidden)
		return
	}
	store := s.approvalsFor(row.ID)
	switch b.Action {
	case "confirm":
		if err := store.Confirm(row.ID); err != nil {
			httpError(w, err)
			return
		}
		s.reindexApprovalTargets(row)
	case "reject":
		if err := store.Reject(row.ID, strings.TrimSpace(b.Reason)); err != nil {
			httpError(w, err)
			return
		}
	default:
		httpError(w, errBadRequest("action must be confirm or reject"))
		return
	}
	// the portal side of the audit: who decided what, in the team trail
	if a.opt.Store != nil {
		_ = a.opt.Store.LogAction(id, "approval-"+b.Action, map[string]any{
			"proposal": row.ID, "type": row.Type, "action": row.Action,
		}, time.Now())
	}
	writeJSON(w, map[string]any{"ok": true, "id": row.ID, "status": b.Action})
}

// approvalRecontract is the portal's amounts-only edit — the same payload
// contract as the cockpit's /recontract endpoint, decidable-gated.
func (a *oodaAPI) approvalRecontract(w http.ResponseWriter, r *http.Request) {
	id, ok := PortalIdentify(a.opt, w, r)
	if !ok {
		return
	}
	s := a.live.s
	row, found := a.findApproval(r.PathValue("id"))
	if !found {
		http.Error(w, "no such proposal", http.StatusNotFound)
		return
	}
	admin := a.isOodaAdmin(id.Email)
	if _, decidable := s.oodaApprovalAccess(id.Email, admin, a.opt, row, a.live.Snapshot()); !decidable {
		http.Error(w, "this proposal is not yours to edit", http.StatusForbidden)
		return
	}
	var payload approvals.ReContractPayload
	if err := decode(r, &payload); err != nil {
		httpError(w, err)
		return
	}
	if err := s.approvalsFor(row.ID).SetReContractPayload(row.ID, payload); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// findApproval resolves one pending approval row by id across harnesses.
func (a *oodaAPI) findApproval(id string) (approvalRow, bool) {
	for _, row := range a.live.s.approvalRows(nil) {
		if row.ID == id {
			return row, true
		}
	}
	return approvalRow{}, false
}

// reindexApprovalTargets mirrors the cockpit's post-confirm reindex for the
// two portal-decidable types (spirits.go handleSpiritsApprovalConfirm).
func (s *Server) reindexApprovalTargets(row approvalRow) {
	if s.index == nil {
		return
	}
	paths := []string{row.ApplyPath}
	if row.Type == "re-contract" && row.ReContractPayload != nil {
		for _, al := range row.ReContractPayload.Allocations {
			paths = append(paths, s.realestateRootOr()+"/properties/"+al.Property+".md")
		}
		if c := strings.TrimSpace(row.ReContractPayload.Contractor); c != "" {
			paths = append(paths, s.realestateRootOr()+"/contractors/"+c+".md")
		}
	}
	_ = s.index.ReindexPaths(paths)
}
