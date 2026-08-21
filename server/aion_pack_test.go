package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
)

// Leak canaries: content the contract itself already excludes (the finances
// body, retired heuristics). The pack renders the contract and nothing else,
// so neither string may ever surface in a pack byte.
const (
	aPackCanaryFinances = "CANARY-private-finances-body"
	aPackCanaryRetired  = "CANARY-retired-heuristic"
)

// aPackFixture renders a real contract through aion.RenderContract — the
// pack's input is exactly what AionLive serves — pinned to a revision + the
// timestamp every pack file must carry.
func aPackFixture(t *testing.T, rev string) *aionPackSnapshot {
	t.Helper()
	serves := "aion/human-prototype-mri"
	owner := "RT"
	in := aion.ExportInput{
		People: aion.ParsePeople("- [initials:: BA] [name:: Benjamin Anderson] [role:: CEO]\n" +
			"- [initials:: JR] [name:: Jack Ruhl]\n"),
		VTO: aion.ParseVTO(`## 01 core values
- Morale is the most valuable resource we have

## 02 core focus
- [purpose:: control biology with fields]
- [niche:: field-based longevity]

## 03 10-year target
- A medbed in every home

## 04 marketing strategy
- [target:: longevity clinics]
- unique one

## 05 3-year picture
- [date:: 2029-08-01]
- 100 installed units

## 06 1-year plan
- [date:: 2027-08-01] [goal:: aion/human-prototype-mri]
- first human image

## 07 quarter
- [start:: 2026-07-01] [end:: 2026-09-30]
`),
		Backlog: aion.ParseBacklog(`## Tasks
- [ ] Secure the venue [kind:: task] [rock:: aion/human-prototype-mri-rock] [status:: open] [owner:: JR] [source:: [[sync]]] [captured:: 2026-07-31]
- [x] Hire Morgan [kind:: task] [status:: done] [done_on:: 2026-07-06] [owner:: BA] [captured:: 2026-07-06]

## Decisions
- Outsource pig work [kind:: decision] [status:: decided] [decided:: 2026-07-27] [outcome:: use a CRO] [owner:: BA] [captured:: 2026-07-27]
`),
		Heuristics: aion.ParseHeuristics(`- Take the longer path [first:: 2025-11-19]
    - [[aion biosciences]] [date:: 2026-07-02]

## retired
- ` + aPackCanaryRetired + ` [first:: 2025-01-01]
    - [[old note]] [date:: 2025-01-01]
`),
		Finances: aion.ParseFinances("---\ncapital: 1500000\nmonthly_burn: 95000\nas_of: 2026-08-01\n" +
			"currency: USD\nsource: manual\nnote: seed round\n---\n\n" + aPackCanaryFinances + "\n"),
		HiringMD:     []byte("# AION — hiring\n- [role:: lab engineer] [stage:: sourcing]\n"),
		ReferencesMD: []byte("# AION — references\n- primer [url:: https://example.com]\n"),
		Goals: []aion.ExportGoal{
			{ID: "aion/human-prototype-mri", Title: "Human prototype MRI", Horizon: "1yr",
				Status: "open", Children: []string{"aion/human-prototype-mri-rock"}},
			{ID: "aion/human-prototype-mri-rock", Title: "Human-scale spec + team hired", Horizon: "rock",
				Status: "open", Serves: &serves, Owner: &owner, Quarter: "2026-Q3", Children: []string{}},
		},
		PublishedAt: "2026-08-21T00:00:00Z",
	}
	rendered, err := aion.RenderContract(in)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for contractPath, b := range rendered {
		files[contractURLPath(contractPath)] = b
	}
	return &aionPackSnapshot{
		Revision: rev,
		At:       time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		Files:    files,
	}
}

// The generator writes one file per record type, every one stamped with the
// snapshot's revision — freshness must be visible on any file a reader opens.
func TestAPackWritesEveryRecordType(t *testing.T) {
	dir := t.TempDir()
	snap := aPackFixture(t, "aaaa111122223333")
	wrote, err := syncAionPack(dir, snap)
	if err != nil || !wrote {
		t.Fatalf("syncAionPack = %v, %v — want a first write", wrote, err)
	}
	for _, name := range aionPackFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		if !strings.Contains(string(b), "revision: "+snap.Revision) {
			t.Fatalf("%s carries no revision stamp", name)
		}
		// hidden records are outside the contract — the pack shows what the
		// portal shows, nothing more
		for _, canary := range []string{aPackCanaryFinances, aPackCanaryRetired} {
			if strings.Contains(string(b), canary) {
				t.Fatalf("%s leaked %q", name, canary)
			}
		}
	}
	backlog, _ := os.ReadFile(filepath.Join(dir, "backlog.md"))
	if !strings.Contains(string(backlog), "Secure the venue") ||
		!strings.Contains(string(backlog), "Outsource pig work") {
		t.Fatal("a backlog record is missing from backlog.md")
	}
	goals, _ := os.ReadFile(filepath.Join(dir, "goals.md"))
	if !strings.Contains(string(goals), "Human prototype MRI") ||
		!strings.Contains(string(goals), "aion/human-prototype-mri-rock") {
		t.Fatal("a goal is missing from goals.md")
	}
	people, _ := os.ReadFile(filepath.Join(dir, "people.md"))
	if !strings.Contains(string(people), "Benjamin Anderson") {
		t.Fatal("a person is missing from people.md")
	}
	heur, _ := os.ReadFile(filepath.Join(dir, "heuristics.md"))
	if !strings.Contains(string(heur), "Take the longer path") {
		t.Fatal("a live heuristic is missing from heuristics.md")
	}
	fin, _ := os.ReadFile(filepath.Join(dir, "finances.md"))
	if !strings.Contains(string(fin), "runway: 15.8 months") {
		t.Fatal("the runway figure is missing from finances.md")
	}
	if got := aionPackRevision(dir); got != snap.Revision {
		t.Fatalf("stamped revision = %q, want %q", got, snap.Revision)
	}
}

// Same snapshot → byte-identical pack. Determinism is what makes the
// revision stamp meaningful: a diff means the DOMAIN moved, never the code's
// iteration order.
func TestAPackIsDeterministic(t *testing.T) {
	a := aionPackRender(aPackFixture(t, "aaaa111122223333"))
	b := aionPackRender(aPackFixture(t, "aaaa111122223333"))
	for _, name := range aionPackFiles {
		if a[name] != b[name] {
			t.Fatalf("%s differs across two renders of the same snapshot", name)
		}
	}
}

// An unchanged revision must not rewrite — the generator runs on the portal
// refresh path, so a no-op check is what keeps it cheap.
func TestAPackSkipsWhenRevisionUnchanged(t *testing.T) {
	dir := t.TempDir()
	snap := aPackFixture(t, "aaaa111122223333")
	if _, err := syncAionPack(dir, snap); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "backlog.md")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o664); err != nil {
		t.Fatal(err)
	}
	wrote, err := syncAionPack(dir, snap)
	if err != nil || wrote {
		t.Fatalf("syncAionPack = %v, %v — an unchanged revision must be a no-op", wrote, err)
	}
	if b, _ := os.ReadFile(sentinel); string(b) != "SENTINEL" {
		t.Fatal("the no-op path rewrote a file")
	}
}

// The drift guard: a pack stamped with an old revision is DETECTABLY stale
// against the live snapshot, and one sync repairs it. This is the invariant
// that keeps the standing channel from ever going silently stale. Hermetic:
// temp-dir fixtures only, never the real /shared mount.
func TestAPackDriftGuard(t *testing.T) {
	dir := t.TempDir()
	if _, err := syncAionPack(dir, aPackFixture(t, "aaaa111122223333")); err != nil {
		t.Fatal(err)
	}
	live := aPackFixture(t, "bbbb444455556666") // the source moved
	if aionPackRevision(dir) == live.Revision {
		t.Fatal("a stale pack read as current — the stamp is not guarding anything")
	}
	wrote, err := syncAionPack(dir, live)
	if err != nil || !wrote {
		t.Fatalf("syncAionPack = %v, %v — a diverged pack must rewrite", wrote, err)
	}
	if got := aionPackRevision(dir); got != live.Revision {
		t.Fatalf("after sync, stamped revision = %q, want %q", got, live.Revision)
	}
}
