package server

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/spirits"
)

// A run whose `request:` frontmatter was truncated past its trailing
// `[todo:: …]` token must still resolve to its todo: the token is recovered
// from the report body (which carries the request in full), else from the brief
// the run wrote. Old reports predate the engine-side truncation fix and can't
// be rewritten, so the projection has to be robust to them.
func TestDelegationIndexRecoversTruncatedToken(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// report A: token truncated out of `request:`, still present in the body
	mk("artifacts/runs/2026-08-12-courier-run-a.md", `---
run: run-a
spirit: courier
ritual: delegate
request: Investigate the UAE fast-track device approval pathway and how that data feeds a US submis…
started: 2026-08-12T16:07:58Z
finished: 2026-08-12T16:20:00Z
outcome: completed
---
## Summoner's request

TASK (from your todo board): Investigate the UAE fast-track pathway
For this todo: [todo:: aion:667571bb]
`)
	mk("artifacts/library/2026-08-12-uae.md", `---
type: artifact
title: UAE Fast-Track Device Approval — Research
run: run-a
date: 2026-08-12T16:19:00Z
---
findings
`)
	// report B: token truncated out of `request:` AND missing from the body,
	// but the brief this run wrote carries it
	mk("artifacts/runs/2026-08-12-courier-run-b.md", `---
run: run-b
spirit: courier
ritual: delegate
request: a long work order whose token fell off the end…
started: 2026-08-12T09:00:00Z
finished: 2026-08-12T09:30:00Z
outcome: completed
---
no request section here
`)
	mk("artifacts/library/2026-08-12-visas.md", `---
type: artifact
title: Visa routes brief
run: run-b
date: 2026-08-12T09:29:00Z
---
answering [todo:: p-77]
`)

	s := &Server{}
	s.UseHarnesses([]Harness{{Name: "hermes", Spirits: spirits.NewStore(root)}})

	idx := s.delegationIndex()
	a, ok := idx["aion:667571bb"]
	if !ok {
		t.Fatalf("truncated request not recovered from report body: %+v", idx)
	}
	if a.State != "done" || a.RunID != "2026-08-12-courier-run-a" {
		t.Fatalf("run A projection wrong: %+v", a)
	}
	if a.ArtifactRef != "artifacts/library/2026-08-12-uae.md" {
		t.Fatalf("run A artifact not linked: %+v", a)
	}
	b, ok := idx["p-77"]
	if !ok {
		t.Fatalf("truncated request not recovered from the brief: %+v", idx)
	}
	if b.State != "done" || b.ArtifactRef != "artifacts/library/2026-08-12-visas.md" {
		t.Fatalf("run B projection wrong: %+v", b)
	}
}

// A request that still carries its token keeps the existing behaviour, and two
// done runs for one todo resolve newest-first.
func TestDelegationIndexPrefersNewestDoneRun(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []struct{ id, started string }{{"old", "2026-08-10T10:00:00Z"}, {"new", "2026-08-12T10:00:00Z"}} {
		mk("artifacts/runs/2026-08-12-courier-"+r.id+".md", `---
run: `+r.id+`
spirit: courier
ritual: delegate
request: work the thing [todo:: p-9]
started: `+r.started+`
finished: `+r.started+`
outcome: completed
---
body
`)
	}
	s := &Server{}
	s.UseHarnesses([]Harness{{Name: "hermes", Spirits: spirits.NewStore(root)}})

	d := s.delegationIndex()["p-9"]
	if d.State != "done" || d.RunID != "2026-08-12-courier-new" {
		t.Fatalf("want the newest done run, got %+v", d)
	}
}
