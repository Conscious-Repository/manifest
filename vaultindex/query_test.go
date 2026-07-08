package vaultindex

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fixture writes a small vault and returns an open, rebuilt in-memory index.
func fixture(t *testing.T) (*Index, string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// people (inline + block styles), with an alias
	write("alice.md", "---\ncategories: [people]\nalias: [Al]\n---\n- GP at [[acme]]\n")
	write("bob.md", "---\ncategories:\n  - people\n---\nbody\n")
	// syncs (dated), linking people + a bare entity with no note behind it
	write("2026-05-01 alice sync.md", "---\ncategories:\n  - sync\n---\n[[alice]] [[shoumik dabir]]\nnotes\n")
	write("2026-06-01 team sync.md", "---\ncategories: [sync]\n---\n[[bob]] discussed roadmap\n")
	// a daily linking alice + shoumik (soft interaction, dated by filename)
	write("intrinsic/2026-07-01.md", "<!-- manifest:start -->\nmeeting [[alice]] and [[shoumik dabir]] today\n")
	// an AI-authored brief about alice — indexed, but NOT an interaction
	write("Agents/brief-alice.md", "---\ncategories: [research]\n---\nBrief on [[alice]] and [[shoumik dabir]].\n")
	// an undated note linking shoumik — a backlink that contributes NO date
	write("random idea.md", "thoughts on [[shoumik dabir]]\n")

	ix, err := Open(Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	return ix, root
}

func names(refs []NoteRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Name
	}
	return out
}

func TestCategoryReproducesContains(t *testing.T) {
	ix, _ := fixture(t)
	people, _ := ix.Category("people", SortNameAsc)
	if got := names(people); !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Fatalf("people (name asc) = %v", got)
	}
	syncs, _ := ix.Category("sync", SortMtimeDesc)
	if len(syncs) != 2 {
		t.Fatalf("sync count = %d, want 2", len(syncs))
	}
	// block-style category must be found identically to inline (audit §0)
	if len(mustCat(t, ix, "people")) != 2 {
		t.Fatal("block-style categories not indexed like inline")
	}
}

func TestShoumikEntityCaseSynthetic(t *testing.T) {
	ix, _ := fixture(t)
	e, ok := ix.Entity("shoumik dabir")
	if !ok || e.HasNote {
		t.Fatalf("entity = %+v, ok=%v (want exists, no note behind it)", e, ok)
	}
	// backlinks include the AI brief; interactions exclude it
	bl, _ := ix.Backlinks("shoumik dabir")
	inter, _ := ix.Interactions("shoumik dabir")
	if len(bl) != len(inter)+1 {
		t.Fatalf("backlinks %d, interactions %d (AI brief should be the only difference)", len(bl), len(inter))
	}
	for _, b := range inter {
		if b.AIAuthored {
			t.Fatal("interactions must exclude AI-authored notes")
		}
	}
	// last-met is the newest DATED, non-AI source; the undated note contributes nothing
	date, src, ok := ix.LastMet("shoumik dabir")
	if !ok || date != "2026-07-01" {
		t.Fatalf("last-met = %q from %q (want 2026-07-01)", date, src)
	}
}

func TestLastMetIgnoresAIEvenWhenNewer(t *testing.T) {
	ix, root := fixture(t)
	// an AI note dated in the far future must NOT become "last met"
	if err := os.WriteFile(filepath.Join(root, "Agents", "2099-01-01 auto.md"),
		[]byte("[[shoumik dabir]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ix.ReindexPaths([]string{"Agents/2099-01-01 auto.md"}); err != nil {
		t.Fatal(err)
	}
	date, _, _ := ix.LastMet("shoumik dabir")
	if date == "2099-01-01" {
		t.Fatal("AI-authored note must never set last-met")
	}
}

func TestResolveByNameAliasAndTarget(t *testing.T) {
	ix, _ := fixture(t)
	if e, ok := ix.Resolve("Alice"); !ok || e.NotePath != "alice.md" || !e.IsPerson {
		t.Fatalf("resolve by name: %+v ok=%v", e, ok)
	}
	if e, ok := ix.Resolve("al"); !ok || e.NotePath != "alice.md" {
		t.Fatalf("resolve by alias 'Al': %+v ok=%v", e, ok)
	}
	if e, ok := ix.Resolve("Shoumik Dabir"); !ok || e.HasNote {
		t.Fatalf("resolve bare target: %+v ok=%v", e, ok)
	}
}

func TestMentionsFTS(t *testing.T) {
	ix, _ := fixture(t)
	hits, err := ix.Mentions("roadmap", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Name != "2026-06-01 team sync" {
		t.Fatalf("FTS mentions('roadmap') = %v", names(hits))
	}
	// AI-authored content is excluded from FTS results
	ai, _ := ix.Mentions("Brief", 10)
	for _, h := range ai {
		if h.Path == "Agents/brief-alice.md" {
			t.Fatal("FTS must exclude AI-authored notes")
		}
	}
}

func TestVocabularyDriftAndClean(t *testing.T) {
	ix, root := fixture(t)
	// clean vault: no category has two spellings
	if c, _ := ix.Vocabulary(); len(c) != 0 {
		t.Fatalf("clean vault should have no drift, got %+v", c)
	}
	// introduce a plural drift: project vs projects
	os.WriteFile(filepath.Join(root, "p1.md"), []byte("---\ncategories: [project]\n---\n"), 0o644)
	os.WriteFile(filepath.Join(root, "p2.md"), []byte("---\ncategories: [projects]\n---\n"), 0o644)
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	clusters, _ := ix.Vocabulary()
	if len(clusters) != 1 || len(clusters[0].Variants) != 2 {
		t.Fatalf("expected one project/projects cluster, got %+v", clusters)
	}
}

func TestIncrementalReindexAndDelete(t *testing.T) {
	ix, root := fixture(t)
	// edit alice: drop the people category, add essays
	os.WriteFile(filepath.Join(root, "alice.md"), []byte("---\ncategories: [essays]\n---\nnew body about widgets\n"), 0o644)
	if err := ix.ReindexPaths([]string{"alice.md"}); err != nil {
		t.Fatal(err)
	}
	if got := names(mustCat(t, ix, "people")); !reflect.DeepEqual(got, []string{"bob"}) {
		t.Fatalf("after reindex, people = %v (want [bob])", got)
	}
	// FTS reflects the new body and drops the old (proves DELETE FROM notes_fts WHERE path works)
	if hits, _ := ix.Mentions("widgets", 10); len(hits) != 1 {
		t.Fatalf("FTS should find the reindexed body, got %v", names(hits))
	}
	// delete the file → gone from every projection
	os.Remove(filepath.Join(root, "bob.md"))
	if err := ix.ReindexPaths([]string{"bob.md"}); err != nil {
		t.Fatal(err)
	}
	if len(mustCat(t, ix, "people")) != 0 {
		t.Fatal("deleted note still present in category index")
	}
}

func TestLosslessRebuild(t *testing.T) {
	ix, _ := fixture(t)
	c1 := counts(t, ix)
	if _, err := ix.Rebuild(); err != nil { // rebuild from scratch again
		t.Fatal(err)
	}
	if c2 := counts(t, ix); !reflect.DeepEqual(c1, c2) {
		t.Fatalf("rebuild not lossless: %v != %v", c1, c2)
	}
}

func mustCat(t *testing.T, ix *Index, v string) []NoteRef {
	t.Helper()
	refs, err := ix.Category(v, SortNameAsc)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

func counts(t *testing.T, ix *Index) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tbl := range []string{"notes", "note_categories", "links", "entities", "note_aliases"} {
		var c int
		if err := ix.DB().QueryRow("SELECT count(*) FROM " + tbl).Scan(&c); err != nil {
			t.Fatal(err)
		}
		out[tbl] = c
	}
	return out
}

// ---- zone model (system-root-plan §1/§3) ----

// zoneFixture is fixture plus a SYSTEM ZONE: a CRM record linking a person, a
// date-named engine file, and an agents brief — all under system/.
func zoneFixture(t *testing.T) (*Index, string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alice.md", "---\ncategories: [people]\n---\nfriend\n")
	write("2026-05-01 alice sync.md", "---\ncategories: [sync]\n---\n[[alice]]\n- [ ] send deck\n")
	// CRM record (system zone, HUMAN-edited): links alice + a person with no note.
	// Dated FRONTMATTER + an unchecked task to prove none of it leaks into contacts.
	write("system/crm/fundraising/acme ventures.md",
		"---\nstage: intro\ndate: 2026-07-07\npeople: [\"[[alice]]\", \"[[carol newperson]]\"]\n---\n[[alice]] intro'd via [[carol newperson]]\n- [ ] follow up\n")
	// engine regions under system/: excluded from FTS entirely
	write("system/excalibur/spirits/x/memories/window/2026-07-07.md", "engine memory naming [[alice]]\n")
	write("system/agents/brief.md", "brief on [[alice]] zebrafish\n")
	// knowledge-zone note for FTS contrast
	write("zebra notes.md", "zebrafish research\n")

	ix, err := Open(Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	return ix, root
}

// A [[Person]] link inside a system-zone record resolves (both zones indexed)
// and appears in raw Backlinks (the CRM-strip join), but creates NO contact,
// timeline entry, triage item, open loop, or interaction date.
func TestSystemZoneLinksCreateNoContactSignals(t *testing.T) {
	ix, _ := zoneFixture(t)

	// resolves across the zone line
	if e, ok := ix.Resolve("acme ventures"); !ok || e.NotePath != "system/crm/fundraising/acme ventures.md" {
		t.Fatalf("system-zone note must resolve as an entity: %+v ok=%v", e, ok)
	}

	// carol is linked ONLY from the CRM record → must NOT be a note-less target (no triage)
	targets, err := ix.NoteLessTargets()
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range targets {
		if tg.Key == "carol newperson" {
			t.Fatalf("a person linked only from a system-zone record must not reach triage: %+v", tg)
		}
	}

	// alice's timeline: only the knowledge-zone sync — never the CRM record
	tl, err := ix.Timeline("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(tl) != 1 || tl[0].Path != "2026-05-01 alice sync.md" {
		t.Fatalf("timeline must be knowledge-only, got %+v", tl)
	}
	// last-met unchanged by the CRM record's 2026-07-07 date
	if d, _, ok := ix.LastMet("alice"); !ok || d != "2026-05-01" {
		t.Fatalf("last-met = %q, want 2026-05-01 (system-zone dates never count)", d)
	}
	// interaction dates exclude the CRM date too
	dates, _ := ix.InteractionDatesByKey()
	if got := dates["alice"]; len(got) != 1 || got[0] != "2026-05-01" {
		t.Fatalf("interaction dates = %v, want [2026-05-01]", got)
	}
	// the CRM record's unchecked task is not an open loop for alice
	loops, _ := ix.OpenLoops([]string{"alice"})
	for _, l := range loops {
		if l.Path == "system/crm/fundraising/acme ventures.md" {
			t.Fatalf("CRM task must not surface as an open loop: %+v", l)
		}
	}

	// ...but the link IS visible on raw Backlinks (the CRM strip joins through it)
	bls, _ := ix.Backlinks("alice")
	found := false
	for _, b := range bls {
		if b.Path == "system/crm/fundraising/acme ventures.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("the CRM record must appear among raw backlinks (both zones indexed)")
	}
}

// FTS: AI regions (system/agents, system/excalibur) are excluded entirely; other
// system-zone notes are indexed but Mentions (a contacts surface) stays knowledge-only.
func TestZoneFTSAndMentions(t *testing.T) {
	ix, _ := zoneFixture(t)
	var n int
	if err := ix.DB().QueryRow(
		`SELECT COUNT(*) FROM notes_fts JOIN notes n ON n.id=notes_fts.rowid WHERE n.ai_authored=1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("AI-region notes must not be in FTS, found %d", n)
	}
	// CRM record IS in FTS (searchable) …
	if err := ix.DB().QueryRow(
		`SELECT COUNT(*) FROM notes_fts JOIN notes n ON n.id=notes_fts.rowid WHERE n.path LIKE 'system/crm/%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CRM records must stay searchable in FTS, found %d", n)
	}
	// … but Mentions (contact surface) returns knowledge-zone notes only
	refs, err := ix.Mentions("zebrafish", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Path != "zebra notes.md" {
		t.Fatalf("mentions must be knowledge-only, got %+v", refs)
	}
}

// The zone column reflects the path split; a date-named file under
// system/excalibur/ keeps its parsed date but its zone keeps it out of every
// contact query (the vault scanner independently keeps it out of dailies).
func TestZoneColumnAndNoteZone(t *testing.T) {
	ix, _ := zoneFixture(t)
	if z := ix.NoteZone("system/crm/fundraising/acme ventures.md"); z != "system" {
		t.Fatalf("CRM record zone = %q, want system", z)
	}
	if z := ix.NoteZone("alice.md"); z != "knowledge" {
		t.Fatalf("alice zone = %q, want knowledge", z)
	}
	var date, zone string
	if err := ix.DB().QueryRow(
		`SELECT date, zone FROM notes WHERE path='system/excalibur/spirits/x/memories/window/2026-07-07.md'`).Scan(&date, &zone); err != nil {
		t.Fatal(err)
	}
	if date != "2026-07-07" || zone != "system" {
		t.Fatalf("engine file date/zone = %q/%q, want 2026-07-07/system", date, zone)
	}
}
