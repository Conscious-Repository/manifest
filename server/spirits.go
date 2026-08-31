package server

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/goals"
	"manifest/record"
	"manifest/secrets"
	"manifest/spirits"
	"manifest/teamportal"
)

// maskSecrets is the display-side defense for aion proposal bodies: any
// span matching a secret class renders as "•••" (the authoritative gates
// refuse at edit/apply — this is belt over braces for the card itself).
func maskSecrets(text string) string { return secrets.Mask(text) }

// SPIRITS — the excalibur harness console. The dashboard reads the sibling
// tree and records user decisions (keep/discard/snooze); execution belongs to
// the engine, which the dashboard only ever reaches by dropping a run-now
// request in the spool (excalibur-path-plan.md §7.5).

func (s *Server) handleSpiritsStatus(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"enabled": false})
		return
	}
	alive, at := s.spirits.EngineAlive()
	resp := map[string]any{
		"enabled":     true,
		"engineAlive": alive, // legacy top-level = the primary harness
		"spirits":     s.spirits.Spirits(),
		"feedInbox":   s.feedInboxCount(time.Now()), // same compute as /api/feed — counts never drift
		"harnesses":   s.harnessHeartbeats(),        // federation: per-harness liveness
	}
	if !at.IsZero() {
		resp["heartbeat"] = at.Format(time.RFC3339)
	}
	writeJSON(w, resp)
}

// (Feed handlers moved to server/feed.go — FEED is a first-class surface now;
// SPIRITS keeps only the engine console: runs, rituals, approvals, status.)

func (s *Server) handleSpiritsRuns(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}, "queued": []any{}})
		return
	}
	// data = every run report (running ones included, outcome:running); queued =
	// spool files not yet picked up — merged across the harness federation,
	// tagged by source. The client derives queued/running/done from these
	// files alone — no browser-held run state (plan §1).
	writeJSON(w, map[string]any{"data": s.mergedRuns(), "queued": s.mergedQueued(),
		"primary": s.primaryHarnessName()})
}

func (s *Server) handleSpiritsRun(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	h, sum, body, ok := s.findRun(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"summary": sum, "body": body, "harness": h.Name})
}

// handleSpiritsRunPrompt serves the preserved exact prompts — the §6.5 "show
// assembled prompt" affordance.
func (s *Server) handleSpiritsRunPrompt(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	h, sum, _, ok := s.findRun(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	turns, err := h.Spirits.RunPrompts(sum.Spirit, sum.Run)
	if err != nil {
		http.Error(w, "prompts not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"data": turns})
}

// Approvals — the ONE inbox (excalibur/artifacts/approvals, plan §2.5).
// Spirits file proposals via the write_approval cast; Confirm/Reject here only
// RECORD the human decision (a folder move) — nothing sends or executes.

// approvalRow is a pending Proposal enriched for rendering: whether its
// apply-path is inside the type's allow-list (Confirm disabled otherwise) and
// the target's CURRENT content for the current-vs-proposed diff.
type approvalRow struct {
	approvals.Proposal
	Allowed bool   `json:"allowed"`
	Current string `json:"current"`
	Harness string `json:"harness,omitempty"` // federation source tag
	// AionPayload is the parsed structured payload of an aion proposal (nil
	// otherwise) — the editable card renders a form over it. Secret spans in
	// the body are masked display-side before it reaches the client.
	AionPayload *aion.ProposalPayload `json:"aionPayload,omitempty"`
	// AionLine is the exact record line Confirm would write (the diff target).
	AionLine string `json:"aionLine,omitempty"`
	// ReContractPayload is the parsed intake payload of a re-contract proposal
	// (nil otherwise) — the card renders the adjust-amounts editor over it.
	ReContractPayload *approvals.ReContractPayload `json:"reContractPayload,omitempty"`
	// GoalsPayload is the parsed placement of a goals-item proposal (nil
	// otherwise). Proposed carries the exact post-confirm bytes when the
	// placement currently applies cleanly — the card's diff IS the apply.
	GoalsPayload *goals.PlacementPayload `json:"goalsPayload,omitempty"`
	// GoalsErr says why the placement would refuse right now (stale anchor,
	// duplicate, missing area) — the card shows it instead of a green diff.
	// It doubles as the generic "why Confirm is off" line for types with no
	// diff of their own, which is how a portal-proposal explains staleness.
	GoalsErr string `json:"goalsErr,omitempty"`
	// PortalProposal is the parsed payload of a team-portal proposal card
	// (nil otherwise): who proposed what to whom, and which proposal Confirm
	// decides. There is no diff — the effect is a team-store write.
	PortalProposal *approvals.PortalProposalPayload `json:"portalProposal,omitempty"`
	// PortalSettled is the live status ("approved" | "rejected") when the
	// portal already decided this proposal — the target acted first. The card
	// then renders the outcome with a lone Dismiss (owner ask 2026-08-31: a
	// settled card must not keep offering Reject as the only way out).
	PortalSettled string `json:"portalSettled,omitempty"`
}

// approvalRows returns the enriched pending approvals, skipping any types in
// exclude. Shared by the SPIRITS endpoint and the FEED (the approvals inbox).
func (s *Server) approvalRows(exclude map[string]bool) []approvalRow {
	rows := []approvalRow{}
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		rows = append(rows, s.harnessApprovalRows(h, exclude)...)
	}
	return rows
}

// harnessApprovalRows enriches ONE harness's pending proposals; the apply
// allow-lists + current-content reads run against that harness's store.
func (s *Server) harnessApprovalRows(h Harness, exclude map[string]bool) []approvalRow {
	rows := []approvalRow{}
	store := h.Approvals
	for _, p := range store.List("pending") {
		if exclude[p.Type] {
			continue
		}
		rr := approvalRow{Proposal: p, Harness: s.harnessTag(h.Name)}
		if p.ApplyPath != "" {
			switch p.Type {
			case approvals.TypeCreateVaultNote:
				// A new vault-root note: allowed by its own path rule, no current
				// content (the diff renders as an all-added new file).
				rr.Allowed = approvals.CreateVaultNotePathAllowed(p.ApplyPath)
			case approvals.TypeAppendVaultNote:
				// An email-thread append that auto-apply refused (renamed note,
				// thread-id mismatch): surfaces as a human card with the current
				// note so the owner can see where the sections would land.
				rr.Allowed = approvals.AppendVaultNotePathAllowed(p.ApplyPath) && p.GmailThreadID != ""
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
			case approvals.TypeAionBacklog, approvals.TypeAionHeuristic, approvals.TypeReBacklog,
				approvals.TypeAionResolve, approvals.TypeReResolve:
				// A domain extraction candidate (aion or real-estate): editable
				// payload + the exact line Confirm would append (or, for a
				// resolve, the item line as it would read once flipped).
				// Secret-masked.
				rr.Allowed = ((p.Type == approvals.TypeAionBacklog || p.Type == approvals.TypeAionResolve) && approvals.AionBacklogPathAllowed(p.ApplyPath)) ||
					(p.Type == approvals.TypeAionHeuristic && approvals.AionHeuristicPathAllowed(p.ApplyPath)) ||
					((p.Type == approvals.TypeReBacklog || p.Type == approvals.TypeReResolve) && approvals.ReBacklogPathAllowed(p.ApplyPath))
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
				if payload, ok := approvals.AionPayload(p); ok {
					rr.AionPayload = &payload
					rr.AionLine = aion.RenderItemLine(payload)
				} else {
					rr.Allowed = false
				}
				rr.Body = maskSecrets(rr.Body)
			case approvals.TypeGoalsItem:
				// a goals placement (§12 2026-08-19): the diff shown is the exact
				// write Confirm makes — computed fresh against the live file, so
				// a stale anchor or a duplicate reads as a refusal BEFORE the click
				rr.Allowed = approvals.GoalsPathAllowed(p.ApplyPath)
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
				if payload, ok := approvals.GoalsPayload(p); ok {
					rr.GoalsPayload = &payload
					if rr.Current != "" {
						if next, err := goals.ApplyPlacement(rr.Current, payload, time.Now()); err == nil {
							rr.Proposed = next
						} else {
							rr.GoalsErr = err.Error()
						}
					}
				} else {
					rr.Allowed = false
				}
				rr.Body = maskSecrets(rr.Body)
			case approvals.TypeReContract:
				// an intake proposal (overhaul §5): the card renders the payload
				// with editable amounts — the owner always adjusts; the spirit
				// proposes, never decides
				rr.Allowed = approvals.ReContractPathAllowed(p.ApplyPath)
				if payload, ok := approvals.ParseReContractPayload(p.Body); ok {
					rr.ReContractPayload = &payload
				} else {
					rr.Allowed = false
				}
				rr.Body = maskSecrets(rr.Body)
			default:
				rr.Allowed = approvals.ApplyPathAllowed(p.ApplyPath)
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
			}
		} else if p.Type == approvals.TypePortalProposal {
			// No ApplyPath: the effect is a team-store Decide, not a file write.
			// Allowed asks whether Confirm can actually do anything, so it turns
			// on the two things the dispatch needs — a readable payload and a
			// configured portal — rather than on a path rule.
			if payload, ok := approvals.ParsePortalProposal(p); ok {
				rr.PortalProposal = &payload
				rr.Allowed = s.teamStoreNamed(payload.Portal) != nil
				if !rr.Allowed {
					rr.GoalsErr = "portal " + payload.Portal + " is not configured on this host"
				} else if st := s.teamStoreNamed(payload.Portal); st != nil {
					// a proposal the target already settled in the portal: say so
					// on the card instead of letting Confirm look available
					for _, live := range st.Ext().Proposals {
						if live.ID == payload.PropID && live.Status != "pending" {
							rr.Allowed = false
							rr.PortalSettled = live.Status
							rr.GoalsErr = "already " + live.Status + " in the portal — this card is stale"
						}
					}
				}
			}
		}
		rows = append(rows, rr)
	}
	return rows
}

func (s *Server) handleSpiritsApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeJSON(w, map[string]any{"pending": []any{}, "counts": map[string]int{}})
		return
	}
	counts := map[string]int{}
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		for k, v := range h.Approvals.Counts() {
			counts[k] += v
		}
	}
	writeJSON(w, map[string]any{"pending": s.approvalRows(nil), "counts": counts})
}

func (s *Server) handleSpiritsApprovalConfirm(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	// A create-vault-note may carry edited attendees, a retitled filename,
	// and edited frontmatter categories (the `aion` category is the
	// automation key — it makes the written note trigger extraction).
	// Edit* flags distinguish "no edit" from "cleared to none".
	var b struct {
		Attendees      []string `json:"attendees"`
		EditAttendees  bool     `json:"editAttendees"`
		Title          string   `json:"title"` // create-vault-note: owner-edited filename title
		Categories     []string `json:"categories"`
		EditCategories bool     `json:"editCategories"`
	}
	_ = decode(r, &b) // body is optional (plain confirm)
	id := r.PathValue("id")
	store := s.approvalsFor(id) // federation: route to the harness holding the id
	// run-errand payload must be read BEFORE confirm (confirm moves the file);
	// a failed confirm never enqueues, so approval maps 1:1 to one execution.
	pending, loadErr := store.LoadPending(id)
	var err error
	if b.EditAttendees || b.EditCategories || strings.TrimSpace(b.Title) != "" {
		err = store.ConfirmCreateNote(id, approvals.ConfirmEdits{
			Attendees: b.Attendees, EditAttendees: b.EditAttendees,
			Title:      b.Title,
			Categories: b.Categories, EditCategories: b.EditCategories,
		})
	} else {
		err = store.Confirm(id)
	}
	if err != nil {
		httpError(w, err)
		return
	}
	// Extraction nudge: a freshly-written transcript with the aion category
	// should spool the extractor NOW, not on the watcher's debounce. The
	// sink re-checks category + content hash, so non-aion notes and the
	// watcher's duplicate event are no-ops. The written filename is the
	// (possibly retitled) apply path, lowercased by the apply.
	if loadErr == nil && pending.Type == approvals.TypeCreateVaultNote && s.aionSink != nil {
		if approved, err := store.LoadApproved(id); err == nil && approved.ApplyPath != "" {
			s.aionSink.Notify([]string{"log/" + strings.ToLower(approved.ApplyPath)})
		}
	}
	// re-contract applies write index-read records (contract, contractor,
	// property trees) — reindex them so the surfaces see the confirm at once
	if loadErr == nil && pending.Type == approvals.TypeReContract && s.index != nil {
		paths := []string{pending.ApplyPath}
		if payload, ok := approvals.ParseReContractPayload(pending.Body); ok {
			seen := map[string]bool{}
			for _, a := range payload.Allocations {
				if !seen[a.Property] {
					seen[a.Property] = true
					paths = append(paths, s.realestateRootOr()+"/properties/"+a.Property+".md")
				}
			}
			if payload.ContractorCreate != "" {
				paths = append(paths, s.realestateRootOr()+"/contractors/"+record.Slug(payload.ContractorCreate, 60)+".md")
			}
		}
		_ = s.index.ReindexPaths(paths)
	}
	// A manually-confirmed append (the auto-apply-refused fallback card) grew
	// the thread note — same nudge; the final path reflects a range rename.
	if loadErr == nil && pending.Type == approvals.TypeAppendVaultNote && s.aionSink != nil {
		s.aionSink.Notify([]string{approvals.AppendFinalPath(pending)})
	}
	// A portal proposal's effect lives in the TEAM STORE, not the vault, so
	// like run-errand it carries no ApplyPath and the dispatch happens here
	// after the folder move. Decide mints the item; the materialization lane
	// then carries it into the backlog on its own.
	if loadErr == nil && pending.Type == approvals.TypePortalProposal {
		if err := s.decidePortalProposal(pending, true); err != nil {
			httpError(w, errBadRequest("approved, but the proposal could not be applied: "+err.Error()))
			return
		}
	}
	if loadErr == nil && pending.Type == approvals.TypeRunErrand {
		if s.errandExec == nil {
			httpError(w, errBadRequest("approved, but errands are not available to enqueue"))
			return
		}
		if _, err := s.errandExec.Enqueue(pending.ErrandText, pending.ErrandAccount, "proposal", id, pending.ErrandGoal); err != nil {
			httpError(w, errBadRequest("approved, but enqueue failed: "+err.Error()))
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSpiritsApprovalGoals rewrites a pending goals proposal's editable
// placement (mode/level/area/parent/target/title/fields) — the pre-Confirm
// edit lane. The id never changes.
func (s *Server) handleSpiritsApprovalGoals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	var payload goals.PlacementPayload
	if err := decode(r, &payload); err != nil {
		httpError(w, err)
		return
	}
	if err := s.approvalsFor(r.PathValue("id")).SetGoalsPayload(r.PathValue("id"), payload); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSpiritsApprovalAion rewrites a pending aion proposal's editable
// payload (kind/title/owner/rock/due/outcome, heuristic new⇄reinforce flip)
// — the pre-Confirm edit lane. The id never changes.
func (s *Server) handleSpiritsApprovalAion(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	var payload aion.ProposalPayload
	if err := decode(r, &payload); err != nil {
		httpError(w, err)
		return
	}
	if err := s.approvalsFor(r.PathValue("id")).SetAionPayload(r.PathValue("id"), payload); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleSpiritsApprovalReject(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	_ = decode(r, &b) // reason is optional
	id := r.PathValue("id")
	// read BEFORE the move — Reject relocates the file and LoadPending would
	// then miss it, the same ordering the confirm path uses
	pending, loadErr := s.approvalsFor(id).LoadPending(id)
	if err := s.approvalsFor(id).Reject(id, b.Reason); err != nil {
		httpError(w, err)
		return
	}
	// a rejected portal proposal must also close in the TEAM STORE, or the
	// target still sees it pending in the portal and can approve what the
	// owner just refused
	if loadErr == nil && pending.Type == approvals.TypePortalProposal {
		if err := s.decidePortalProposal(pending, false); err != nil {
			log.Printf("portal proposal reject: card closed but the store did not: %v", err)
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSpiritsApprovalDismiss clears a SETTLED portal-proposal card: the
// target decided it in the portal first, so there is nothing left to decide
// here — the card only needs to leave the feed.
//
// ⚠ Deliberately narrow. It re-verifies against the LIVE team store and
// refuses anything still pending, so dismiss can never become a silent
// discard lane for real decisions — Confirm and Reject stay the only verbs
// that decide. The pending file is archived under the outcome that actually
// happened; nothing is dispatched.
func (s *Server) handleSpiritsApprovalDismiss(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	pending, err := s.approvalsFor(id).LoadPending(id)
	if err != nil {
		httpError(w, err)
		return
	}
	if pending.Type != approvals.TypePortalProposal {
		httpError(w, errBadRequest("only a settled portal-proposal card can be dismissed — use Confirm or Reject"))
		return
	}
	payload, ok := approvals.ParsePortalProposal(pending)
	if !ok {
		httpError(w, errBadRequest("this card has no readable portal payload"))
		return
	}
	st := s.teamStoreNamed(payload.Portal)
	if st == nil {
		httpError(w, errBadRequest("portal "+payload.Portal+" is not configured on this host"))
		return
	}
	settled := ""
	for _, live := range st.Ext().Proposals {
		if live.ID == payload.PropID && live.Status != "pending" {
			settled = live.Status
		}
	}
	if settled == "" {
		httpError(w, errBadRequest("this proposal is still pending in the portal — decide it with Confirm or Reject"))
		return
	}
	if err := s.approvalsFor(id).Settle(id, settled); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// decidePortalProposal is the FEED card's effect: one call into the team store
// that owns the proposal.
//
// A proposal has always been decidable by the target as well as the owner
// (portal.go handleDecide), and that did not change — so by the time a card is
// clicked the proposal may already be settled. Store.Decide refuses a
// non-pending proposal, and that refusal is SUCCESS here: the question the card
// asked has been answered, just not by this click. Treating it as an error
// would leave a card the owner cannot clear by any means.
func (s *Server) decidePortalProposal(p approvals.Proposal, approve bool) error {
	payload, ok := approvals.ParsePortalProposal(p)
	if !ok {
		return errors.New("this card has no readable portal payload")
	}
	store := s.teamStoreNamed(payload.Portal)
	if store == nil {
		return errors.New("portal " + payload.Portal + " is not configured on this host")
	}
	isOoda := strings.TrimSpace(payload.Portal) == "ooda-portal"
	adminEmail := s.aionAdminEmail()
	if isOoda {
		adminEmail = s.oodaAdminEmail()
	}
	admin := teamportal.Identity{Email: adminEmail, Name: "Benjamin"}
	if _, err := store.Decide(admin, payload.PropID, approve, time.Now()); err != nil {
		if strings.Contains(err.Error(), "already") {
			return nil // settled in the portal first — the card was stale, not wrong
		}
		return err
	}
	if approve {
		// the minted item reaches its portal's backlog now
		if isOoda {
			s.syncOodaPortalToVault(time.Now())
		} else {
			s.syncPortalToVault(time.Now())
		}
	}
	return nil
}

// teamStoreNamed resolves a portal name to its team store. The name is the
// bridge's card-id prefix, so it is already the stable identifier for "which
// portal" everywhere else in the FEED.
func (s *Server) teamStoreNamed(name string) *teamportal.Store {
	switch strings.TrimSpace(name) {
	case "", "aion-portal":
		if s.aionLive == nil {
			return nil
		}
		st, _ := s.aionLive.teamStore()
		return st
	case "ooda-portal":
		return s.oodaTeam
	}
	return nil
}

// RITUALS board — every ritual across spirits with computed next-fire, last
// outcome, ceiling, and validity (plans/spirits-console-upgrade.md §1).
func (s *Server) handleSpiritsRituals(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, map[string]any{"data": s.spirits.Rituals(time.Now())})
}

// handleSpiritsFileGet / Put — the raw markdown editor over the allow-listed
// harness config files (§2). Paths off the allow-list 404; PUT lints and blocks
// hard breakage (422) while letting warnings through.
// Reads span the FEDERATION (fix 2026-08-12): an artifact brief lives in the
// harness that wrote it, so a hermes card asking for its own library file must
// not 404 against excalibur. `harness` narrows the search when the client knows
// the tag; otherwise every tree is tried, primary first. Writes stay
// primary-only (PUT below) — the federation contract is read-side voluntary.
func (s *Server) handleSpiritsFileGet(w http.ResponseWriter, r *http.Request) {
	hs := s.eachHarness()
	if len(hs) == 0 {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	path := r.URL.Query().Get("path")
	want := r.URL.Query().Get("harness")
	offList := false
	for _, h := range hs {
		if h.Spirits == nil {
			continue
		}
		if want != "" && h.Name != want && s.harnessTag(h.Name) != want {
			continue
		}
		content, allowed, err := h.Spirits.ReadFile(path)
		if !allowed {
			offList = true
			continue // the allow-list is identical across trees
		}
		if err != nil {
			continue // not in THIS tree — try the next one
		}
		writeJSON(w, map[string]any{"path": path, "harness": s.harnessTag(h.Name), "content": content})
		return
	}
	if offList {
		http.Error(w, "path not editable", http.StatusNotFound)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handleSpiritsFilePut(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Content string `json:"content"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	res, allowed, err := s.spirits.WriteFile(r.URL.Query().Get("path"), b.Content)
	if !allowed {
		http.Error(w, "path not editable", http.StatusNotFound)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	if !res.OK {
		w.WriteHeader(http.StatusUnprocessableEntity) // lint blocked the save
	}
	writeJSON(w, res)
}

// handleSpiritsNewRitual / NewSpirit — quick create (§3).
func (s *Server) handleSpiritsNewRitual(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Spirit string `json:"spirit"`
		Name   string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	path, err := s.spirits.ScaffoldRitual(b.Spirit, b.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"path": path})
}

func (s *Server) handleSpiritsNewSpirit(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.ScaffoldSpirit(b.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"path": "spirits/" + b.Name + "/cornerstone.md"})
}

func (s *Server) handleSpiritsRunNow(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Spirit  string `json:"spirit"`
		Ritual  string `json:"ritual"`
		Request string `json:"request"`
		Skill   string `json:"skill"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.SpoolRunNow(b.Spirit, b.Ritual, b.Request, b.Skill); err != nil {
		if errors.Is(err, spirits.ErrAlreadyActive) {
			w.WriteHeader(http.StatusConflict) // the ritual is already queued/running
			writeJSON(w, map[string]any{"active": true, "error": "already queued or running"})
			return
		}
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"spooled": true})
}

// handleSpiritsCastables lists what the command bar can cast: the summoner's
// vault skills (each cast through sage) and the on-demand rituals.
func (s *Server) handleSpiritsCastables(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, map[string]any{"data": s.spirits.Castables(time.Now())})
}

// handleSpiritsCatalog — the spirit page's capability vocabularies: conduits
// (grimoire/portals) + spellbooks (grimoire/spellbooks). Read-only, primary
// harness (editing is primary-only anyway).
func (s *Server) handleSpiritsCatalog(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"portals": []string{}, "spellbooks": []string{}})
		return
	}
	writeJSON(w, map[string]any{"portals": s.spirits.Conduits(), "spellbooks": s.spirits.Spellbooks()})
}

// handleSpiritsMemories — names and counts only, never contents (the
// memories belong to the spirits).
func (s *Server) handleSpiritsMemories(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, map[string]any{"data": s.spirits.Memories(r.URL.Query().Get("spirit"))})
}

// handleSpiritsDeleteRitual / DeleteSpirit — deliberate destruction, primary
// harness only (the same boundary as writes); the harness repo's git history
// is the undo.
func (s *Server) handleSpiritsDeleteRitual(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Spirit string `json:"spirit"`
		Name   string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.DeleteRitual(b.Spirit, b.Name); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleSpiritsDeleteSpirit(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.DeleteSpirit(b.Name); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
