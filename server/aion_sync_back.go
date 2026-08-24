package server

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/teamportal"
)

// PORTAL → VAULT materialization (ARCHITECTURE §12 amendment, 2026-08-24).
//
// Until this file existed the team store was a PARALLEL TRUTH. A member's new
// task lived only in items.ext.json; a member marking the owner's task done
// wrote an override that the cockpit applied at read time while backlog.md
// still said `open`. Two surfaces, two answers, and Obsidian — the actual
// source of record — never heard about either.
//
// The reconciler closes that. Three passes, each idempotent, each ending with
// the team store giving up the state it just handed over:
//
//	new items   ext.Items          → a backlog line, then PromoteItem
//	field edits ext.Overrides      → set on the existing line, then ClearOverride
//	archives    ext.Archives       → the line is removed from backlog.md
//
// What does NOT cross: comments and the activity trail. They have no line
// grammar in backlog.md and are the portal's own record (owner decision).
//
// Every write goes through ONE capability pinned to ONE file — `aion-portal`,
// actor `portal-member` — so `write-audit.log` can always answer "which of my
// writers touched the backlog, and was a human in the loop".

// aionSyncBack owns the reconciler's serialization. Portal writes arrive
// concurrently on the portal listener while the ticker also fires; two passes
// interleaving over a read-modify-write of the same file would lose one side.
type aionSyncBack struct {
	mu   sync.Mutex
	last time.Time
}

// syncPortalToVault reconciles the team store into system/aion/backlog.md.
// Safe to call on every portal write and on a ticker; it does nothing when
// there is nothing staged.
//
// Errors are logged, not returned: a portal member's PATCH must not 500
// because the vault happens to be busy. The next call retries, and until it
// succeeds the overlay keeps serving the right answer to every manifest
// surface — the write is deferred, never lost.
func (s *Server) syncPortalToVault(now time.Time) {
	if s.aion == nil || s.aionPortal == nil || s.aionLive == nil {
		return
	}
	team, _ := s.aionLive.teamStore()
	if team == nil {
		return
	}
	s.syncBack.mu.Lock()
	defer s.syncBack.mu.Unlock()

	ext := team.Ext()
	doc := s.aion.LoadBacklog()
	if doc == nil {
		return
	}
	actor := teamportal.Identity{Email: s.aionAdminEmail(), Name: "portal sync"}

	dirty := false
	promote := map[string]string{} // team id → the vault id it became
	synced := []string{}           // overrides now living in the file

	// ---- 1. new items become real lines ----------------------------------
	for _, it := range ext.Items {
		if archivedIn(ext, it.ID) {
			continue // it was created and removed before we ever got here
		}
		line := backlogItemFromTeam(it)
		// AppendItem mints aion-bl/<slug> when ID is empty and preserves a
		// set one — we deliberately let it mint, so the item lands in the
		// vault's own id namespace and owner edits route to the vault store
		// afterwards (server/aion_sync.go branches on the `team/` prefix).
		doc.AppendItem(line)
		promote[it.ID] = line.ID
		dirty = true
	}

	// ---- 2. staged field edits land on the line they describe -------------
	for id, ov := range ext.Overrides {
		if strings.HasPrefix(id, "team/") {
			continue // pass 1 carries its own fields across; nothing staged yet
		}
		item := doc.Find(id)
		if item == nil {
			continue // an override for a line we cannot see: leave it staged
		}
		if applyOverrideToItem(item, ov.Fields) {
			dirty = true
		}
		// cleared even when nothing moved — the fields already agree with the
		// record, so keeping the override would only let it veto a later edit
		synced = append(synced, id)
	}

	// ---- 3. archived items leave the file ---------------------------------
	for _, a := range ext.Archives {
		if strings.HasPrefix(a.ID, "team/") {
			continue // never reached the vault; the store row is the whole story
		}
		if doc.Remove(a.ID) {
			dirty = true
		}
	}

	if !dirty && len(synced) == 0 {
		return
	}
	if dirty {
		// PRE-FLIGHT. The projection runs AcceptContract on every recompose and,
		// on any error, refuses the whole corpus and serves the last good
		// snapshot instead (aion_live.go refresh). So a single bad line here
		// does not degrade one item — it FREEZES AION for everyone, on both
		// the cockpit and the portal, until someone edits the vault by hand.
		//
		// A member typing a rock that resolves to no goal is enough to do it.
		// So the reconciler checks its own work against the same gate the
		// projection will apply, and declines to write rather than poison the
		// corpus. The staged state stays in the store, the overlay keeps
		// serving, and the log names the line to fix.
		if err := s.contractWouldAccept(doc); err != nil {
			log.Printf("portal→vault sync: refusing to write, the corpus would not "+
				"validate and AION would go stale: %v", err)
			return
		}
		// ONE write for all three passes. The fixpoint contract does the rest:
		// untouched lines round-trip byte-identically and an edited line keeps
		// its original token order and spelling.
		if err := s.aionPortal.SaveBacklog(doc); err != nil {
			log.Printf("portal→vault sync: backlog write failed, retrying next tick: %v", err)
			return // nothing below runs — the store keeps everything, unharmed
		}
	}
	// Only now does the store let go. The vault record is the thing that must
	// exist first: crash between the two and the reconciler simply re-runs.
	for teamID, vaultID := range promote {
		if err := team.PromoteItem(actor, teamID, vaultID, now); err != nil {
			log.Printf("portal→vault sync: promote %s: %v", teamID, err)
		}
	}
	for _, id := range synced {
		if err := team.ClearOverride(actor, id, now); err != nil {
			log.Printf("portal→vault sync: clear override %s: %v", id, err)
		}
	}
	s.syncBack.last = now
	// force a recompose: the file changed under the projection, and until it
	// re-reads, EffectiveItems() still carries the pre-sync shape (and, for a
	// promoted item, would briefly show neither copy).
	_ = s.aionLive.refresh(true)
}

// SyncPortalToVault is the exported hook main binds to PortalOptions.AfterWrite.
func (s *Server) SyncPortalToVault(now time.Time) { s.syncPortalToVault(now) }

// AionArchiveSnapshot captures one effective item for the portal delete lane.
func (s *Server) AionArchiveSnapshot(itemID string) (teamportal.ArchivedItem, bool) {
	if s.aionLive == nil {
		return teamportal.ArchivedItem{}, false
	}
	for _, it := range s.aionLive.EffectiveItems() {
		if it.ID == itemID {
			return archiveSnapshot(it), true
		}
	}
	return teamportal.ArchivedItem{}, false
}

// StartPortalSync runs the reconciler on a slow ticker. AfterWrite covers the
// common path; this is the safety net for the two cases it cannot see — a
// write that landed while the vault was busy, and a line that was refused by
// the contract gate until the owner created the rock it names.
func (s *Server) StartPortalSync(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 5 * time.Minute
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.syncPortalToVault(now)
			}
		}
	}()
}

// FileProposalCard turns a portal proposal into an approvable FEED card.
//
// It files into the PRIMARY harness's approvals store, which is where every
// other card the owner sees already lives — so the proposal inherits the whole
// lane (badge, card, Confirm/Reject, inspector) rather than growing a parallel
// one. The id is derived from the proposal id so a re-file is a no-op and a
// rejected card never resurrects (approvals dedupe).
func (s *Server) FileProposalCard(portal string) func(teamportal.Proposal) error {
	return func(prop teamportal.Proposal) error {
		if s.approvals == nil {
			return errors.New("approvals are not configured")
		}
		payload := approvals.PortalProposalPayload{
			Portal: portal, PropID: prop.ID, Kind: prop.Kind,
			Title: prop.Title, Target: prop.Target, By: prop.ProposedName,
		}
		if payload.By == "" {
			payload.By = prop.ProposedBy
		}
		kind := prop.Kind
		if kind != "decision" {
			kind = "task"
		}
		_, err := s.approvals.Propose(approvals.Proposal{
			ID:     "portal-" + strings.TrimPrefix(prop.ID, "prop/"),
			Type:   approvals.TypePortalProposal,
			Action: "approve a " + kind + " proposed for " + prop.Target,
			Agent:  portal,
			Body:   approvals.RenderPortalProposalBody(payload),
		})
		return err
	}
}

// contractWouldAccept renders the CANDIDATE corpus — the doc as the reconciler
// is about to save it, every other file as it stands — and runs the projection's
// own acceptance gate over the result. Errors only; warnings are advisory and
// the live projection tolerates them.
//
// This is the reconciler's whole safety story. Without it the sync is a way for
// any signed-in team member to take AION offline by typing a rock name.
func (s *Server) contractWouldAccept(doc *aion.BacklogDoc) error {
	now := time.Now().UTC()
	in := s.aionExportInput(now.Format(time.RFC3339))
	in.Backlog = doc // the only substitution: everything else is the live corpus
	rendered, err := aion.RenderContract(in)
	if err != nil {
		return err
	}
	if errs, _ := aion.AcceptContract(rendered, aionInScopeQuarters(now)); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// backlogItemFromTeam projects a portal-created item onto a backlog line.
// ID is left empty on purpose: AppendItem mints the vault-namespace id.
func backlogItemFromTeam(it teamportal.TeamItem) *aion.BacklogItem {
	kind := strings.TrimSpace(it.Kind)
	if kind != "decision" {
		kind = "task"
	}
	line := &aion.BacklogItem{
		Text: it.Title, Kind: kind, Owner: it.Owner, Captured: it.Captured,
		Rock: it.Rock, Due: it.Due, Status: it.Status, DoneOn: it.DoneOn,
		NeededBy: it.NeededBy, Decided: it.Decided, Outcome: it.Outcome,
	}
	if line.Status == "" {
		line.Status = "open"
	}
	if kind == "decision" {
		// a decision is a PLAIN bullet by corpus canon (aion/payload.go) — the
		// checkbox form is the task shape, and rendering a decision as one
		// makes it parse back as a task on the next read
		line.Plain = true
		if line.Decided != "" {
			line.Status = "decided"
		}
	} else {
		line.Checked = line.Status == "done"
	}
	return line
}

// applyOverrideToItem writes the staged fields onto a parsed line, reporting
// whether anything actually changed. The key set mirrors teamportal.PatchFields
// exactly; an unrecognized key is ignored here the same way the read-time
// overlay ignores it, so a future field cannot silently reach the vault.
func applyOverrideToItem(item *aion.BacklogItem, fields map[string]string) bool {
	changed := false
	set := func(dst *string, v string) {
		if *dst != v {
			*dst, changed = v, true
		}
	}
	for key, val := range fields {
		switch key {
		case "status":
			set(&item.Status, val)
			// the checkbox and the token are two spellings of one fact
			if want := val == "done"; item.Checked != want {
				item.Checked, changed = want, true
			}
		case "done_on":
			set(&item.DoneOn, val)
		case "due":
			set(&item.Due, val)
		case "needed_by":
			set(&item.NeededBy, val)
		case "decided":
			set(&item.Decided, val)
		case "outcome":
			set(&item.Outcome, val)
		case "title":
			set(&item.Text, val)
		case "owner":
			set(&item.Owner, val)
		}
	}
	return changed
}

func archivedIn(ext teamportal.Ext, id string) bool {
	for _, a := range ext.Archives {
		if a.ID == id {
			return true
		}
	}
	return false
}

// aionAdminEmail names the sync in the activity trail. It is the portal owner
// because the standing consent is his; the individual edit was a member's, and
// that member is already named on the patch entry the trail carries alongside.
func (s *Server) aionAdminEmail() string {
	if s.aionLive != nil {
		if _, id := s.aionLive.teamStore(); id.Email != "" {
			return id.Email
		}
	}
	return "portal-sync"
}
