package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/realestate"
)

// packFixture is the dashboard fixture pinned to a revision + timestamp, the
// two facts every pack file must carry.
func packFixture(rev string) *oodaSnapshot {
	snap := oodaFixture()
	snap.Revision = rev
	snap.At = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	snap.Backlog = []*aion.BacklogItem{
		{ID: "aion-bl/bank", Text: "call the bank", Owner: "BA", Kind: "task", Due: "2020-01-01"},
	}
	snap.Contracts = []realestate.Contract{{
		Slug: "roof-2026", Name: "Roof package", Contractor: "m-w-services",
		Status: "accepted", Total: 40000, Drawn: 10000,
		Allocations: []realestate.ContractAllocation{{Property: "late-st", NodeID: "s-Roof", Amount: 40000}},
	}}
	return snap
}

// The generator writes one file per record type, every one stamped with the
// snapshot's revision — freshness must be visible on any file a reader opens.
func TestOodaPackWritesEveryRecordType(t *testing.T) {
	dir := t.TempDir()
	snap := packFixture("aaaa111122223333")
	wrote, err := syncOodaPack(dir, snap)
	if err != nil || !wrote {
		t.Fatalf("syncOodaPack = %v, %v — want a first write", wrote, err)
	}
	for _, name := range oodaPackFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		if !strings.Contains(string(b), "revision: "+snap.Revision) {
			t.Fatalf("%s carries no revision stamp", name)
		}
	}
	props, _ := os.ReadFile(filepath.Join(dir, "properties.md"))
	if !strings.Contains(string(props), "over-st") {
		t.Fatal("a portfolio property is missing from properties.md")
	}
	// hidden + research-tail records are outside the shared portfolio — the
	// pack shows what the portal shows, nothing more
	if strings.Contains(string(props), "hidden-st") || strings.Contains(string(props), "tracked-st") {
		t.Fatal("a hidden or tracked record leaked into the pack")
	}
	backlog, _ := os.ReadFile(filepath.Join(dir, "backlog.md"))
	if !strings.Contains(string(backlog), "call the bank") {
		t.Fatal("an open backlog item is missing from backlog.md")
	}
	contracts, _ := os.ReadFile(filepath.Join(dir, "contracts.md"))
	if !strings.Contains(string(contracts), "Roof package") {
		t.Fatal("a contract is missing from contracts.md")
	}
	if got := oodaPackRevision(dir); got != snap.Revision {
		t.Fatalf("stamped revision = %q, want %q", got, snap.Revision)
	}
}

// Same snapshot → byte-identical pack. Determinism is what makes the
// revision stamp meaningful: a diff means the DOMAIN moved, never the code's
// iteration order.
func TestOodaPackIsDeterministic(t *testing.T) {
	a := oodaPackRender(packFixture("aaaa111122223333"))
	b := oodaPackRender(packFixture("aaaa111122223333"))
	for _, name := range oodaPackFiles {
		if a[name] != b[name] {
			t.Fatalf("%s differs across two renders of the same snapshot", name)
		}
	}
}

// An unchanged revision must not rewrite — the generator runs on the portal
// refresh path, so a no-op check is what keeps it cheap.
func TestOodaPackSkipsWhenRevisionUnchanged(t *testing.T) {
	dir := t.TempDir()
	snap := packFixture("aaaa111122223333")
	if _, err := syncOodaPack(dir, snap); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "properties.md")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o664); err != nil {
		t.Fatal(err)
	}
	wrote, err := syncOodaPack(dir, snap)
	if err != nil || wrote {
		t.Fatalf("syncOodaPack = %v, %v — an unchanged revision must be a no-op", wrote, err)
	}
	if b, _ := os.ReadFile(sentinel); string(b) != "SENTINEL" {
		t.Fatal("the no-op path rewrote a file")
	}
}

// The drift guard: a pack stamped with an old revision is DETECTABLY stale
// against the live snapshot, and one sync repairs it. This is the invariant
// that keeps the standing channel from ever going silently stale again.
func TestOodaPackDriftGuard(t *testing.T) {
	dir := t.TempDir()
	if _, err := syncOodaPack(dir, packFixture("aaaa111122223333")); err != nil {
		t.Fatal(err)
	}
	live := packFixture("bbbb444455556666") // the source moved
	if oodaPackRevision(dir) == live.Revision {
		t.Fatal("a stale pack read as current — the stamp is not guarding anything")
	}
	wrote, err := syncOodaPack(dir, live)
	if err != nil || !wrote {
		t.Fatalf("syncOodaPack = %v, %v — a diverged pack must rewrite", wrote, err)
	}
	if got := oodaPackRevision(dir); got != live.Revision {
		t.Fatalf("after sync, stamped revision = %q, want %q", got, live.Revision)
	}
}
