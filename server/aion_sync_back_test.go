package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/teamportal"
)

// syncFixture is liveFixture plus the materialization writer. The reconciler
// refuses to run without its own store handle, which is the nil-safety that
// keeps every other test in this package on the pre-2026-08-24 behaviour.
func syncFixture(t *testing.T) (*Server, *AionLive, *teamportal.Store, string, string) {
	t.Helper()
	srv, live, team, baseID := liveFixture(t)
	root := srv.aion.Root()
	backlog := filepath.Join(srv.aion.Path("backlog.md"))
	srv.UsePortalSync(aion.NewStore(
		strings.TrimSuffix(srv.aion.Path("backlog.md"), "/"+root+"/backlog.md"),
		root,
		func(path string, body []byte) error { return os.WriteFile(path, body, 0o644) }))
	return srv, live, team, baseID, backlog
}

// THE duplicate trap. effectiveItems() concatenates the vault base and
// ext.Items with no id-based dedup, so a materialized item that is still in
// the store shows up TWICE on every surface — and nothing in the codebase
// would notice, because every safety net is a keyed lookup that finds the
// first copy and stops.
func TestSyncMaterializesWithoutDoubling(t *testing.T) {
	srv, live, team, _, backlog := syncFixture(t)
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}

	created, err := team.AddItem(member, "MM", "task", "Wire the new sensor", "", "2026-09-10", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := team.AddComment(member, created.ID, "starting today", now); err != nil {
		t.Fatal(err)
	}
	before := len(live.EffectiveItems())

	srv.syncPortalToVault(now)

	// exactly one copy, and it is the VAULT one
	items := live.EffectiveItems()
	if len(items) != before {
		t.Fatalf("item count moved %d → %d; materialization must not add a row", before, len(items))
	}
	var found *AionEffectiveItem
	for i := range items {
		if items[i].Title == "Wire the new sensor" {
			if found != nil {
				t.Fatal("the item appears TWICE — the store row was not dropped")
			}
			found = &items[i]
		}
	}
	if found == nil {
		t.Fatal("the item vanished — it left the store without reaching the vault")
	}
	if found.SourceType != "vault" {
		t.Errorf("sourceType = %q, want vault", found.SourceType)
	}
	if !strings.HasPrefix(found.ID, "aion-bl/") {
		t.Errorf("id = %q, want the vault namespace", found.ID)
	}
	// the comment followed the id, or the discussion is orphaned
	if found.CommentCount != 1 {
		t.Errorf("commentCount = %d, want 1 — the comment did not follow the rekey", found.CommentCount)
	}

	// and it is really in the file, with its fields
	body, err := os.ReadFile(backlog)
	if err != nil {
		t.Fatal(err)
	}
	line := findLine(t, string(body), "Wire the new sensor")
	for _, want := range []string{"[kind:: task]", "[owner:: MM]", "[due:: 2026-09-10]"} {
		if !strings.Contains(line, want) {
			t.Errorf("backlog line missing %s:\n%s", want, line)
		}
	}

	// running again changes nothing — the reconciler is called on every write
	srv.syncPortalToVault(now)
	if got := len(live.EffectiveItems()); got != before {
		t.Fatalf("a second pass changed the count to %d — not idempotent", got)
	}
}

// A field edit on a VAULT item has to reach the line, not sit forever as an
// overlay that only manifest can see. This is the half the owner actually
// notices: the portal says done, Obsidian says open.
func TestSyncWritesFieldEditsIntoTheLine(t *testing.T) {
	srv, live, team, baseID, backlog := syncFixture(t)
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}

	original, err := os.ReadFile(backlog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := team.Patch(member, baseID, map[string]string{
		"status": "done", "done_on": "2026-08-24"}, now); err != nil {
		t.Fatal(err)
	}
	srv.syncPortalToVault(now)

	body, err := os.ReadFile(backlog)
	if err != nil {
		t.Fatal(err)
	}
	line := findLine(t, string(body), "Base task")
	if !strings.Contains(line, "[status:: done]") || !strings.Contains(line, "2026-08-24") {
		t.Fatalf("the edit did not reach the line:\n%s", line)
	}
	if !strings.HasPrefix(line, "- [x]") {
		t.Errorf("checkbox and status disagree — the line still reads unchecked:\n%s", line)
	}
	// the override is spent: leaving it would let stale fields veto the
	// owner's next edit in Obsidian
	if _, ok := team.Ext().Overrides[baseID]; ok {
		t.Error("the override survived the sync")
	}
	// FIXPOINT: every other line is byte-identical
	assertOnlyLineChanged(t, string(original), string(body), "Base task")

	// and the composed read still shows it exactly once, still done
	items := live.EffectiveItems()
	n := 0
	for _, it := range items {
		if it.Title == "Base task" {
			n++
			if it.Status == nil || *it.Status != "done" {
				t.Errorf("status = %v after sync, want done", it.Status)
			}
		}
	}
	if n != 1 {
		t.Fatalf("Base task appears %d times", n)
	}
}

// Closing a decision was the live bug: the portal wrote status=done, the
// archive selects on status=decided, and the decision vanished from both.
func TestSyncClosesADecisionWithItsOutcome(t *testing.T) {
	srv, live, team, _, backlog := syncFixture(t)
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}

	d, err := team.AddItem(member, "MM", "decision", "Pick the imaging modality", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	srv.syncPortalToVault(now) // it becomes a vault line first
	vaultID := ""
	for _, it := range live.EffectiveItems() {
		if it.Title == "Pick the imaging modality" {
			vaultID = it.ID
		}
	}
	if vaultID == "" {
		t.Fatal("the decision never materialized")
	}
	if _, ok := team.Ext().IDMigrations[d.ID]; !ok {
		t.Error("no alias for the old team id — existing links would dangle")
	}

	if _, err := team.Patch(member, vaultID, map[string]string{
		"status": "decided", "decided": "2026-08-24",
		"outcome": "ultrasound, with MRI as the fallback"}, now); err != nil {
		t.Fatalf("Patch refused a decided close: %v", err)
	}
	srv.syncPortalToVault(now)

	body, _ := os.ReadFile(backlog)
	line := findLine(t, string(body), "Pick the imaging modality")
	for _, want := range []string{"[kind:: decision]", "[status:: decided]",
		"[decided:: 2026-08-24]", "[outcome:: ultrasound, with MRI as the fallback]"} {
		if !strings.Contains(line, want) {
			t.Errorf("decision line missing %s:\n%s", want, line)
		}
	}
	// a decision is a plain bullet by canon, never a checkbox
	if strings.HasPrefix(line, "- [") {
		t.Errorf("the decision rendered as a checkbox:\n%s", line)
	}
}

// The reconciler must be inert without its writer — every other test in this
// package predates materialization and asserts the overlay-only shape.
func TestSyncIsInertWithoutItsCapability(t *testing.T) {
	srv, live, team, baseID, backlog := syncFixture(t)
	srv.aionPortal = nil // as if the capability were never granted
	now := time.Now()
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}

	before, _ := os.ReadFile(backlog)
	if _, err := team.Patch(member, baseID, map[string]string{"status": "done"}, now); err != nil {
		t.Fatal(err)
	}
	srv.syncPortalToVault(now)

	after, _ := os.ReadFile(backlog)
	if string(before) != string(after) {
		t.Fatal("the backlog changed with no aion-portal handle — the rollback switch does not work")
	}
	if _, ok := team.Ext().Overrides[baseID]; !ok {
		t.Fatal("the override was cleared without the write landing — the edit is lost")
	}
	// and the overlay still answers correctly, so the portal is unharmed
	for _, it := range live.EffectiveItems() {
		if it.ID == baseID && (it.Status == nil || *it.Status != "done") {
			t.Errorf("overlay stopped applying: %v", it.Status)
		}
	}
}

// THE guard. The projection runs AcceptContract on every recompose and, on any
// error, refuses the whole corpus and keeps serving the last good snapshot. So
// one bad line does not degrade one item — it takes AION offline for everyone
// until a human edits the vault. A team member typing a rock name that matches
// no goal is enough to trigger it, which makes this the reconciler's single
// most dangerous power.
func TestSyncRefusesToPoisonTheContract(t *testing.T) {
	srv, live, team, _, backlog := syncFixture(t)
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}

	before, err := os.ReadFile(backlog)
	if err != nil {
		t.Fatal(err)
	}
	created, err := team.AddItem(member, "MM", "task", "Task on a rock that does not exist",
		"aion/no-such-rock", "", now)
	if err != nil {
		t.Fatal(err)
	}
	srv.syncPortalToVault(now)

	after, _ := os.ReadFile(backlog)
	if string(before) != string(after) {
		t.Fatalf("the poisoned line was written:\n%s", string(after))
	}
	if st := live.Status(); st.Stale {
		t.Fatalf("the projection went stale — the sync took AION down: %s", st.Error)
	}
	// the work is not lost: it stays a team item, visible in the portal and
	// the cockpit, and the next pass retries once the rock exists
	found := false
	for _, it := range team.Ext().Items {
		if it.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the item was dropped from the store without reaching the vault — that is data loss")
	}
	seen := 0
	for _, it := range live.EffectiveItems() {
		if it.Title == "Task on a rock that does not exist" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the item appears %d times in the composed read, want 1 (still a team row)", seen)
	}
}

func findLine(t *testing.T, body, needle string) string {
	t.Helper()
	for _, ln := range strings.Split(body, "\n") {
		if strings.Contains(ln, needle) {
			return ln
		}
	}
	t.Fatalf("no line containing %q in:\n%s", needle, body)
	return ""
}

// assertOnlyLineChanged is the fixpoint guard: a sync that rewrites unrelated
// lines would silently reformat the owner's file under him.
func assertOnlyLineChanged(t *testing.T, before, after, needle string) {
	t.Helper()
	b, a := strings.Split(before, "\n"), strings.Split(after, "\n")
	if len(b) != len(a) {
		t.Fatalf("line count changed %d → %d; a field edit must not add or drop lines", len(b), len(a))
	}
	for i := range b {
		if b[i] == a[i] || strings.Contains(b[i], needle) {
			continue
		}
		t.Errorf("line %d changed but should not have:\n  before: %s\n  after:  %s", i+1, b[i], a[i])
	}
}

// The proposal → FEED card lane. The owner asked to approve proposals "like
// any other card"; before this they reached the FEED only as a dismissible
// notice, and deciding one meant leaving the FEED entirely.
func TestPortalProposalBecomesAnApprovableCard(t *testing.T) {
	srv, _, team, _, _ := syncFixture(t)
	srv.UseApprovals(approvals.NewStore(t.TempDir()))
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	proposer := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}

	prop, err := team.Propose(proposer, "yashiro@aion.bio", "YA", "task",
		"Draft the data-room index", "", "", now)
	if err != nil {
		t.Fatal(err)
	}

	file := srv.FileProposalCard("aion-portal")
	if err := file(prop); err != nil {
		t.Fatalf("FileProposalCard: %v", err)
	}
	// filing twice is what a retry looks like; it must not double the inbox
	if err := file(prop); err != nil {
		t.Fatalf("second file: %v", err)
	}

	rows := srv.approvalRows(nil)
	var card *approvalRow
	for i := range rows {
		if rows[i].Type == approvals.TypePortalProposal {
			if card != nil {
				t.Fatal("the proposal filed TWICE — the inbox would show a duplicate")
			}
			card = &rows[i]
		}
	}
	if card == nil {
		t.Fatal("no approvable card was filed")
	}
	if !card.Allowed {
		t.Errorf("Confirm is disabled on a live proposal: %s", card.GoalsErr)
	}
	if card.PortalProposal == nil || card.PortalProposal.PropID != prop.ID {
		t.Fatalf("payload does not name the proposal: %+v", card.PortalProposal)
	}
	if card.ApplyPath != "" {
		t.Errorf("applyPath = %q, want empty — the effect is a store write, not a file", card.ApplyPath)
	}
	if !strings.Contains(card.Body, "Draft the data-room index") {
		t.Error("the card body does not say what is being proposed")
	}

	// approving mints the item and materializes it
	if err := srv.decidePortalProposal(card.Proposal, true); err != nil {
		t.Fatalf("decide: %v", err)
	}
	settled := team.Ext().Proposals
	if len(settled) != 1 || settled[0].Status != "approved" {
		t.Fatalf("proposal not approved in the store: %+v", settled)
	}
	found := false
	for _, it := range srv.aionLive.EffectiveItems() {
		if it.Title == "Draft the data-room index" {
			found = true
			if it.Owner == nil || *it.Owner != "YA" {
				t.Errorf("owner = %v, want YA — it was proposed FOR Yashiro", it.Owner)
			}
		}
	}
	if !found {
		t.Error("the approved item never appeared")
	}
}

// The target can decide in the portal too, so a card can be stale by the time
// it is clicked. That must resolve, not error — an un-clearable card is worse
// than a redundant one.
func TestStalePortalProposalCardResolvesQuietly(t *testing.T) {
	srv, _, team, _, _ := syncFixture(t)
	srv.UseApprovals(approvals.NewStore(t.TempDir()))
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	proposer := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}
	target := teamportal.Identity{Email: "yashiro@aion.bio", Name: "Yashiro"}

	prop, err := team.Propose(proposer, "yashiro@aion.bio", "YA", "task", "Book the vendor call", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.FileProposalCard("aion-portal")(prop); err != nil {
		t.Fatal(err)
	}
	// the target settles it in the portal first
	if _, err := team.Decide(target, prop.ID, true, now); err != nil {
		t.Fatal(err)
	}

	// the card now says so instead of offering a Confirm that cannot work
	for _, row := range srv.approvalRows(nil) {
		if row.Type != approvals.TypePortalProposal {
			continue
		}
		if row.Allowed {
			t.Error("a settled proposal still offers Confirm")
		}
		if !strings.Contains(row.GoalsErr, "already") {
			t.Errorf("the card does not explain why: %q", row.GoalsErr)
		}
		// and clicking it anyway is a no-op, not an error the owner cannot clear
		if err := srv.decidePortalProposal(row.Proposal, true); err != nil {
			t.Errorf("confirming a stale card errored: %v", err)
		}
	}
}

// The materialization lane is a hole in "portals never write the vault", so
// its edge has to be exactly where the amendment says: ONE file. This is the
// AION-side twin of TestNoPortalWriteTouchesTheVault, which still holds
// unchanged for OODA because that portal has no sync hook at all.
func TestSyncTouchesOnlyTheBacklogFile(t *testing.T) {
	srv, _, team, baseID, backlog := syncFixture(t)
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}
	vaultRoot := filepath.Dir(filepath.Dir(filepath.Dir(backlog))) // …/system/aion/backlog.md

	before := fingerprintTree(t, vaultRoot)

	// every member-reachable write, in one pass
	if _, err := team.AddItem(member, "MM", "task", "A brand new task", "", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := team.Patch(member, baseID, map[string]string{"status": "in_progress"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := team.AddComment(member, baseID, "a comment that must NOT reach the vault", now); err != nil {
		t.Fatal(err)
	}
	srv.syncPortalToVault(now)

	after := fingerprintTree(t, vaultRoot)
	rel := "system/aion/backlog.md"
	for path, sum := range after {
		if path == rel {
			continue
		}
		if before[path] != sum {
			t.Errorf("the sync wrote %s — the lane is pinned to %s alone", path, rel)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("the sync deleted %s", path)
		}
	}
	if before[rel] == after[rel] {
		t.Fatal("backlog.md did not change — the test proved nothing")
	}
	// and the comment stayed put
	body, _ := os.ReadFile(backlog)
	if strings.Contains(string(body), "must NOT reach the vault") {
		t.Error("a comment was written into the backlog — comments are store-only")
	}
}

func fingerprintTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A member with an item open when the reconciler promotes it is still holding
// `team/<slug>` while the record has become `aion-bl/<slug>`. Their next edit
// must land, not 404 — the alias exists precisely so the id can move without
// anybody noticing.
func TestEditsSurviveThePromotionRekey(t *testing.T) {
	srv, live, team, _, backlog := syncFixture(t)
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}

	created, err := team.AddItem(member, "MM", "task", "Order the connectors", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	srv.syncPortalToVault(now)

	if got := team.ResolveID(created.ID); !strings.HasPrefix(got, "aion-bl/") {
		t.Fatalf("ResolveID(%s) = %s, want the vault id", created.ID, got)
	}
	// the stale id still writes, through the alias
	live.EffectiveItems() // settle the projection
	if _, err := team.Patch(member, team.ResolveID(created.ID),
		map[string]string{"status": "done", "done_on": "2026-08-24"}, now); err != nil {
		t.Fatalf("patch through the alias: %v", err)
	}
	srv.syncPortalToVault(now)

	body, _ := os.ReadFile(backlog)
	line := findLine(t, string(body), "Order the connectors")
	if !strings.Contains(line, "[status:: done]") {
		t.Fatalf("the edit did not land:\n%s", line)
	}
	// an id with no alias resolves to itself — never to something else
	if got := team.ResolveID("aion-bl/never-migrated"); got != "aion-bl/never-migrated" {
		t.Errorf("ResolveID invented a mapping: %s", got)
	}
}

// A card whose proposal the target already settled offers ONE verb: Dismiss
// (owner ask 2026-08-31 — Reject-as-pseudo-dismiss made him afraid to click).
// Dismiss archives under the outcome that actually happened and dispatches
// nothing; on a still-pending proposal it refuses, so it can never become a
// silent discard lane for real decisions.
func TestSettledPortalProposalCardDismisses(t *testing.T) {
	srv, _, team, _, _ := syncFixture(t)
	srv.UseApprovals(approvals.NewStore(t.TempDir()))
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	proposer := teamportal.Identity{Email: "hannah@aion.bio", Name: "Hannah"}

	prop, err := team.Propose(proposer, "ellie@aion.bio", "EZ", "task",
		"Run GCaMP6f on the ultrasound set-up", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.FileProposalCard("aion-portal")(prop); err != nil {
		t.Fatal(err)
	}

	// The target approves in the PORTAL before the owner touches the card.
	if _, err := team.Decide(teamportal.Identity{Email: "ellie@aion.bio", Name: "Ellie"}, prop.ID, true, now); err != nil {
		t.Fatal(err)
	}

	rows := srv.approvalRows(nil)
	var card *approvalRow
	for i := range rows {
		if rows[i].Type == approvals.TypePortalProposal {
			card = &rows[i]
		}
	}
	if card == nil {
		t.Fatal("no card")
	}
	if card.PortalSettled != "approved" {
		t.Fatalf("portalSettled = %q, want approved — the card cannot render the outcome without it", card.PortalSettled)
	}
	if card.Allowed {
		t.Error("Confirm still reads available on a settled card")
	}

	// Dismiss clears it — archived under what actually happened.
	h := srv.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/spirits/approvals/"+card.ID+"/dismiss", strings.NewReader("{}")))
	if w.Code != 200 {
		t.Fatalf("dismiss: %d %s", w.Code, w.Body.String())
	}
	for _, r := range srv.approvalRows(nil) {
		if r.ID == card.ID {
			t.Fatal("the card is still in the inbox after dismiss")
		}
	}
	if _, err := srv.approvals.LoadApproved(card.ID); err != nil {
		t.Errorf("dismissed card is not filed under approved/: %v", err)
	}

	// ⚠ The refusal half: a still-pending proposal cannot be dismissed.
	prop2, err := team.Propose(proposer, "ellie@aion.bio", "EZ", "task",
		"Second, still-pending proposal", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.FileProposalCard("aion-portal")(prop2); err != nil {
		t.Fatal(err)
	}
	var card2 *approvalRow
	for _, r := range srv.approvalRows(nil) {
		if r.Type == approvals.TypePortalProposal {
			rr := r
			card2 = &rr
		}
	}
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest("POST", "/api/spirits/approvals/"+card2.ID+"/dismiss", strings.NewReader("{}")))
	if w2.Code == 200 {
		t.Fatal("a still-pending proposal was dismissed — dismiss became a silent discard lane")
	}
	found := false
	for _, r := range srv.approvalRows(nil) {
		if r.ID == card2.ID {
			found = true
		}
	}
	if !found {
		t.Error("the refused dismiss still removed the card")
	}
}
