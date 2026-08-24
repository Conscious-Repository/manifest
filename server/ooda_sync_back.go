package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/teamportal"
)

// OODA PORTAL → VAULT materialization — the AION reconciler (aion_sync_back.go,
// ARCHITECTURE §12 amendment 2026-08-24) ported to the real-estate domain.
//
// Same three passes, same surrender-after-write discipline, same bounded blast
// radius: portal-created team items become lines in system/realestate/backlog.md,
// staged field edits land on the line they describe, and archives remove the
// line — all through ONE capability (`ooda-portal`, actor `portal-member`)
// pinned to that ONE file.
//
// What does NOT cross, beyond comments and the activity trail (the portal's own
// record, exactly as on AION): rock-tree node overrides. A `prop/<slug>#<id>`
// item lives in a PROPERTY record, outside the capability's file, so its
// portal-set state stays an overlay — the pre-existing OODA behaviour,
// preserved on purpose. Only backlog-shaped state materializes.

// oodaSyncBack owns the reconciler's serialization, mirroring aionSyncBack:
// portal writes arrive concurrently while the ticker also fires, and two
// read-modify-write passes over backlog.md interleaving would lose one side.
type oodaSyncBack struct {
	mu   sync.Mutex
	last time.Time
}

// UseOodaPortalSync wires the materialization writer — an aion.Store over the
// realestate root whose write func carries the `ooda-portal` capability. Nil
// (never wired) keeps the OODA portal overlay-only, which is the rollback
// switch and what every pre-materialization test asserts.
func (s *Server) UseOodaPortalSync(st *aion.Store) { s.oodaPortal = st }

// syncOodaPortalToVault reconciles the OODA team store into
// system/realestate/backlog.md. Safe on every portal write and on a ticker;
// it does nothing when nothing is staged. Errors are logged, not returned —
// the overlay keeps serving the right answer until the next pass succeeds,
// so a write is deferred, never lost.
func (s *Server) syncOodaPortalToVault(now time.Time) {
	if s.re == nil || s.oodaPortal == nil || s.oodaLive == nil {
		return
	}
	team, _ := s.oodaLive.teamStore()
	if team == nil {
		return
	}
	s.oodaSyncBack.mu.Lock()
	defer s.oodaSyncBack.mu.Unlock()

	ext := team.Ext()
	doc := s.re.LoadBacklog()
	if doc == nil {
		return
	}
	actor := teamportal.Identity{Email: s.oodaAdminEmail(), Name: "portal sync"}

	dirty := false
	promote := map[string]string{} // team id → the vault id it became
	synced := []string{}           // overrides now living in the file
	touched := []string{}          // line ids this pass writes — the gate's scope

	// ---- 1. new items become real lines ----------------------------------
	for _, it := range ext.Items {
		if archivedIn(ext, it.ID) {
			continue // created and removed before we ever got here
		}
		// backlogItemFromTeam is shared with AION on purpose: the RE backlog
		// speaks the same grammar (re.go), and a team item's RE context —
		// property slug + rock — already rides its Rock field.
		line := backlogItemFromTeam(it)
		doc.AppendItem(line) // mints the vault-namespace id (aion-bl/<slug>)
		promote[it.ID] = line.ID
		touched = append(touched, line.ID)
		dirty = true
	}

	// ---- 2. staged field edits land on the line they describe -------------
	for id, ov := range ext.Overrides {
		if strings.HasPrefix(id, "team/") {
			continue // pass 1 carries its own fields across; nothing staged yet
		}
		if strings.HasPrefix(id, "prop/") {
			continue // rock-tree / property state is overlay-only (see header)
		}
		item := doc.Find(id)
		if item == nil {
			continue // an override for a line we cannot see: leave it staged
		}
		if applyOverrideToItem(item, ov.Fields) {
			touched = append(touched, id)
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
		// PRE-FLIGHT. AION's gate re-runs AcceptContract because an invalid
		// corpus takes the whole projection offline. OODA's projection has no
		// contract, but the same class of failure exists: a line that does not
		// round-trip the grammar, or a rock naming a container no surface can
		// resolve, silently corrupts the record every OODA surface reads. So
		// the reconciler checks its own work — scoped to the lines IT is
		// writing, so legacy lines can never wedge the lane — and declines to
		// write rather than poison the backlog. The staged state stays in the
		// store, the overlay keeps serving, and the log names the line to fix.
		if err := s.oodaContractWouldAccept(doc, touched); err != nil {
			log.Printf("ooda portal→vault sync: refusing to write, the backlog would not "+
				"read back cleanly: %v", err)
			return
		}
		// ONE write for all three passes; untouched lines round-trip
		// byte-identically (the fixpoint contract).
		if err := s.oodaPortal.SaveBacklog(doc); err != nil {
			log.Printf("ooda portal→vault sync: backlog write failed, retrying next tick: %v", err)
			return // nothing below runs — the store keeps everything, unharmed
		}
	}
	// Only now does the store let go. The vault record must exist first:
	// crash between the two and the reconciler simply re-runs.
	for teamID, vaultID := range promote {
		if err := team.PromoteItem(actor, teamID, vaultID, now); err != nil {
			log.Printf("ooda portal→vault sync: promote %s: %v", teamID, err)
		}
	}
	for _, id := range synced {
		if err := team.ClearOverride(actor, id, now); err != nil {
			log.Printf("ooda portal→vault sync: clear override %s: %v", id, err)
		}
	}
	s.oodaSyncBack.last = now
	// force a recompose: the file changed under the projection, and until it
	// re-reads, the WORK view still carries the pre-sync shape (and, for a
	// promoted item, would briefly show neither copy).
	_ = s.oodaLive.refresh(true)
}

// SyncOodaPortalToVault is the exported hook main binds to the OODA portal's
// PortalOptions.AfterWrite.
func (s *Server) SyncOodaPortalToVault(now time.Time) { s.syncOodaPortalToVault(now) }

// StartOodaPortalSync runs the reconciler on a slow ticker — the safety net
// for a write deferred by a busy vault or refused by the pre-flight gate
// until the container it names exists (mirrors StartPortalSync).
func (s *Server) StartOodaPortalSync(ctx context.Context, every time.Duration) {
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
				s.syncOodaPortalToVault(now)
			}
		}
	}()
}

// OodaArchiveSnapshot captures one effective item for the portal delete lane.
// Only backlog-shaped items are archivable: a `prop/…` id names a rock-tree
// node or property anchor living in a PROPERTY record, outside the
// `ooda-portal` capability — archiving one would hide a line the reconciler
// can never remove, a parallel truth by construction.
func (s *Server) OodaArchiveSnapshot(itemID string) (teamportal.ArchivedItem, bool) {
	if s.oodaLive == nil || strings.HasPrefix(itemID, "prop/") {
		return teamportal.ArchivedItem{}, false
	}
	team, _ := s.oodaLive.teamStore()
	var snap teamportal.ArchivedItem
	found := false
	if team != nil {
		if it, ok := team.TeamItem(itemID); ok {
			snap = teamportal.ArchivedItem{
				ID: it.ID, Kind: it.Kind, Title: it.Title, Owner: it.Owner,
				Captured: it.Captured, Rock: it.Rock, Due: it.Due, Status: it.Status,
				DoneOn: it.DoneOn, NeededBy: it.NeededBy, Decided: it.Decided,
				Outcome: it.Outcome, Team: true, AddedBy: it.AddedBy,
			}
			found = true
		}
	}
	if !found {
		if live := s.oodaLive.Snapshot(); live != nil {
			for _, it := range live.Backlog {
				if it != nil && it.ID == itemID {
					snap = teamportal.ArchivedItem{
						ID: it.ID, Kind: it.Kind, Title: it.Text, Owner: it.Owner,
						Captured: it.Captured, Rock: it.Rock, Due: it.Due, Status: it.Status,
						DoneOn: it.DoneOn, NeededBy: it.NeededBy, Decided: it.Decided,
						Outcome: it.Outcome,
					}
					found = true
					break
				}
			}
		}
	}
	if !found {
		return teamportal.ArchivedItem{}, false
	}
	// the overlay wins for its bounded fields — the snapshot preserves the
	// EFFECTIVE item, the one every surface was showing when it was deleted
	if team != nil {
		if ov, ok := team.Ext().Overrides[itemID]; ok {
			applyOverrideToArchive(&snap, ov.Fields)
		}
	}
	return snap, true
}

// applyOverrideToArchive folds staged fields onto a snapshot — the same key
// set applyOverrideToItem honors, on the archive shape.
func applyOverrideToArchive(snap *teamportal.ArchivedItem, fields map[string]string) {
	for key, val := range fields {
		switch key {
		case "status":
			snap.Status = val
		case "done_on":
			snap.DoneOn = val
		case "due":
			snap.Due = val
		case "needed_by":
			snap.NeededBy = val
		case "decided":
			snap.Decided = val
		case "outcome":
			snap.Outcome = val
		case "title":
			snap.Title = val
		case "owner":
			snap.Owner = val
		}
	}
}

// oodaContractWouldAccept is the reconciler's pre-flight, scoped to the lines
// this pass writes (AION validates its whole corpus because its projection
// does; here a whole-file gate could let one legacy line wedge the lane for
// every member forever). Two checks per touched line:
//
//	round-trip — serialize the candidate doc and re-parse it; the line must
//	come back with the same id, title, kind, status and rock. A title that
//	parses as field tokens (or carries a newline) fails here instead of
//	silently becoming a different record on the next read.
//
//	resolution — a [rock::] must name a container an OODA surface can
//	resolve: a property slug, an entity, or a deal. A rock naming nothing
//	files the work where no view can find it.
func (s *Server) oodaContractWouldAccept(doc *aion.BacklogDoc, touched []string) error {
	if len(touched) == 0 {
		return nil
	}
	snap := s.oodaLive.Snapshot()
	if snap == nil {
		return fmt.Errorf("the real-estate projection has not composed — declining to write blind")
	}
	containers := map[string]bool{}
	for i := range snap.Properties {
		containers[strings.ToLower(snap.Properties[i].Slug)] = true
	}
	for i := range snap.Entities {
		containers[strings.ToLower(snap.Entities[i].Slug)] = true
	}
	for i := range snap.Deals {
		containers[strings.ToLower(snap.Deals[i].Slug)] = true
	}
	reparsed := aion.ParseBacklog(aion.SerializeBacklog(doc))
	for _, id := range touched {
		want := doc.Find(id)
		if want == nil {
			continue // removed later in the same pass
		}
		got := reparsed.Find(id)
		if got == nil {
			return fmt.Errorf("line %q does not survive a round-trip — refusing to write it", id)
		}
		if got.Text != want.Text || got.Kind != want.Kind || got.Status != want.Status || got.Rock != want.Rock {
			return fmt.Errorf("line %q reads back changed (title/kind/status/rock) — refusing to write it", id)
		}
		switch strings.ToLower(strings.TrimSpace(want.Status)) {
		case "", aion.StatusOpen, aion.StatusInProgress, aion.StatusDone, aion.StatusDecided:
		default:
			return fmt.Errorf("line %q carries status %q, outside the vocabulary", id, want.Status)
		}
		if rock := strings.TrimSpace(want.Rock); rock != "" {
			head, _, _ := strings.Cut(strings.TrimPrefix(rock, "prop/"), "/")
			if !containers[strings.ToLower(head)] {
				return fmt.Errorf("line %q names rock %q, which resolves to no property, entity or deal", id, rock)
			}
		}
	}
	return nil
}

// oodaAdminEmail names the sync in the activity trail — the OODA portal owner,
// because the standing consent is his; the member's own edit is already named
// on the patch entry the trail carries alongside.
func (s *Server) oodaAdminEmail() string {
	if s.oodaLive != nil {
		if _, id := s.oodaLive.teamStore(); id.Email != "" {
			return id.Email
		}
	}
	return "portal-sync"
}
