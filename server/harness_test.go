package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/spirits"
)

// plantHarness builds a minimal conforming tree: one run report, one feed
// item, one pending proposal.
func plantHarness(t *testing.T, root, spirit, runID string) {
	t.Helper()
	mk := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("artifacts/runs/2026-08-11-"+spirit+"-"+runID+".md", `---
run: `+runID+`
spirit: `+spirit+`
ritual: test
started: 2026-08-11T10:00:00Z
finished: 2026-08-11T10:01:00Z
outcome: completed
charge_spent_usd: 0.01
charge_ceiling_usd: 1.00
---
report body
`)
	mk("artifacts/feed/item-"+spirit+".md", `---
type: finding
id: item-`+spirit+`
title: a finding from `+spirit+`
agent: `+spirit+`
date: 2026-08-11T10:00:00Z
status: new
---
`)
	mk("artifacts/approvals/pending/prop-"+spirit+".md", `---
id: prop-`+spirit+`
type: approval
action: test proposal from `+spirit+`
agent: `+spirit+`
created: 2026-08-11T10:00:00Z
---
evidence
`)
}

// TestHarnessFederation: two planted trees merge with the primary untagged
// and the secondary tagged; feed/approval actions route to the owning tree.
func TestHarnessFederation(t *testing.T) {
	dir := t.TempDir()
	exRoot := filepath.Join(dir, "excalibur")
	hmRoot := filepath.Join(dir, "hermes")
	plantHarness(t, exRoot, "scribe", "aaa111")
	plantHarness(t, hmRoot, "courier", "bbb222")

	s := &Server{}
	s.UseHarnesses([]Harness{
		{Name: "excalibur", Spirits: spirits.NewStore(exRoot), Approvals: approvals.NewStore(filepath.Join(exRoot, "artifacts"))},
		{Name: "hermes", Spirits: spirits.NewStore(hmRoot), Approvals: approvals.NewStore(filepath.Join(hmRoot, "artifacts"))},
	})

	// runs merge; primary untagged, secondary tagged
	runs := s.mergedRuns()
	if len(runs) != 2 {
		t.Fatalf("want 2 merged runs, got %d", len(runs))
	}
	tags := map[string]string{}
	for _, r := range runs {
		tags[r.Spirit] = r.Harness
	}
	if tags["scribe"] != "" || tags["courier"] != "hermes" {
		t.Fatalf("tags wrong: %+v", tags)
	}

	// run lookup crosses harnesses
	if h, sum, _, ok := s.findRun("2026-08-11-courier-bbb222"); !ok || h.Name != "hermes" || sum.Spirit != "courier" {
		t.Fatalf("findRun failed: ok=%v h=%q", ok, h.Name)
	}

	// feed items route by id
	if h, ok := s.feedHarnessFor("item-courier"); !ok || h.Name != "hermes" {
		t.Fatalf("feedHarnessFor routed wrong: ok=%v", ok)
	}
	if _, ok := s.feedHarnessFor("no-such-item"); ok {
		t.Fatal("feedHarnessFor must miss unknown ids")
	}

	// approvals merge tagged + route
	rows := s.approvalRows(nil)
	if len(rows) != 2 {
		t.Fatalf("want 2 approval rows, got %d", len(rows))
	}
	byAgent := map[string]string{}
	for _, r := range rows {
		byAgent[r.Agent] = r.Harness
	}
	if byAgent["scribe"] != "" || byAgent["courier"] != "hermes" {
		t.Fatalf("approval tags wrong: %+v", byAgent)
	}
	if st := s.approvalsFor("prop-courier"); st == nil {
		t.Fatal("approvalsFor found nothing")
	} else if _, err := st.LoadPending("prop-courier"); err != nil {
		t.Fatalf("routed store can't load the proposal: %v", err)
	}

	// heartbeats: one entry per harness, both dead (no heartbeat files)
	hbs := s.harnessHeartbeats()
	if len(hbs) != 2 || hbs[0].EngineAlive || hbs[1].EngineAlive {
		t.Fatalf("heartbeats wrong: %+v", hbs)
	}
}

// TestSingleHarnessLegacyShape: with one harness every merged row is untagged —
// the payload is shape-identical to the pre-federation single-store output.
func TestSingleHarnessLegacyShape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "excalibur")
	plantHarness(t, root, "scribe", "ccc333")
	s := &Server{}
	s.UseHarnesses([]Harness{{Name: "excalibur", Spirits: spirits.NewStore(root),
		Approvals: approvals.NewStore(filepath.Join(root, "artifacts"))}})
	for _, r := range s.mergedRuns() {
		if r.Harness != "" {
			t.Fatalf("single-harness run carries a tag: %q", r.Harness)
		}
	}
	for _, r := range s.approvalRows(nil) {
		if r.Harness != "" {
			t.Fatalf("single-harness approval carries a tag: %q", r.Harness)
		}
	}
	_ = time.Now()
}
