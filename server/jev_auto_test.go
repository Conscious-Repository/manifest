package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/approvals"
	"manifest/jev"
	"manifest/typesafe"
)

// Automatic Jev wiring (jev_auto.go). Every test runs on a typesafe.Fake or
// with no judge at all, with TYPESAFE_API_KEY cleared: nothing reaches the
// network.

const aionGranolaNote = "---\ncategories:\n  - aion\ngranola-id: gr_1\n---\n[[jane doe]]\n\n## Transcript\n\nWe reviewed the assay roadmap.\n"

func tierFakeAnswers(choice string, conf float64, probs map[string]float64, signals map[string]float64) *typesafe.Fake {
	ans := tierAnswers(choice, conf, probs)
	for id, p := range signals {
		ans["signal_"+id] = typesafe.NoulAnswer(p)
	}
	return &typesafe.Fake{Answers: ans, Model: "jev-test"}
}

// autoVisibilityServer: the visibility fixture with a Fake judge (nil → no
// judge) and a temp checkout holding a seeded copy of the tier map.
func autoVisibilityServer(t *testing.T, f *typesafe.Fake) (*Server, string, string) {
	t.Helper()
	t.Setenv(typesafe.EnvKey, "")
	tm := aion.TierMap{"2026-09-20 rj sync.md": {Tier: aion.TierInternal, Reason: "team sync", Bytes: 1}}
	s, root := visibilityServer(t, tm)
	if f != nil {
		s.jevJudge = &jev.Judge{Eval: f}
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "aion"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed, err := aion.MarshalTierMap(tm)
	if err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(repo, "aion", "tier-map.json")
	if err := os.WriteFile(mapPath, seed, 0o644); err != nil {
		t.Fatal(err)
	}
	s.terminal = &termCfg{codingRepo: repo}
	return s, root, mapPath
}

func suggestionFor(t *testing.T, s *Server, id string) *aionVisibilitySuggestion {
	t.Helper()
	for _, r := range s.approvalRows(nil) {
		if r.ID == id {
			return r.VisibilitySuggestion
		}
	}
	t.Fatalf("no row %s", id)
	return nil
}

// A new aion transcript's card defaults to Jev's policy tier once the sweep
// has run — and the tier map, the authority, is untouched until confirm.
func TestJevAutoTierDefaultPreselectsWithoutWriting(t *testing.T) {
	f := tierFakeAnswers("open", 0.9, map[string]float64{"open": 0.9, "internal": 0.07, "held": 0.03}, nil)
	s, root, mapPath := autoVisibilityServer(t, f)
	plantCreateNote(t, root, "g1", "2026-09-21 Assay Roadmap.md", aionGranolaNote)
	plantCreateNote(t, root, "p1", "2026-09-20 RJ Sync.md", "---\ncategories: [aion]\npocket-id: pk_1\n---\n\n## Transcript\n\nhi\n")
	before, _ := os.ReadFile(mapPath)

	s.jevAutoSweep(context.Background())
	if len(f.Requests) != 1 {
		t.Fatalf("Jev asked %d times, want 1 (the map-known note is not sent)", len(f.Requests))
	}
	sent := f.Last().State.(map[string]any)
	if txt := sent["transcript"].(string); strings.Contains(txt, "granola-id") || !strings.Contains(txt, "assay roadmap") {
		t.Fatalf("sent text = %q (want the body, not the frontmatter)", txt)
	}
	if sent["note"] != "2026-09-21 assay roadmap.md" || sent["source"] != "granola" {
		t.Fatalf("state = %v", sent)
	}

	g := suggestionFor(t, s, "g1")
	if g.Suggested != aion.TierOpen || g.Basis != "jev" || g.Known || g.Jev == nil {
		t.Fatalf("suggestion = %+v", g)
	}
	if g.Jev.State != jevStateAdvised || g.Jev.Applied || !g.Jev.Advisory || g.Jev.SourceOfTruth != aion.TierMapPath ||
		g.Jev.Advice == nil || g.Jev.Advice.JevTier != aion.TierOpen || g.Jev.Advice.Confidence != 0.9 || g.Jev.Advice.Probabilities["open"] != 0.9 {
		t.Fatalf("jev view = %+v", g.Jev)
	}
	// the map answers for a tiered note; Jev is not consulted
	if p := suggestionFor(t, s, "p1"); p.Basis != "map" || p.Suggested != aion.TierInternal || p.Jev != nil {
		t.Fatalf("known note = %+v", p)
	}
	// a confident open is still NOT eligible: the map is unchanged
	if after, _ := os.ReadFile(mapPath); string(after) != string(before) {
		t.Fatal("advice changed the tier map before the owner confirmed")
	}
	tm, _ := aion.ParseTierMap(before)
	if tm.PortalEligible("2026-09-21 assay roadmap.md") || tm.KairosEligible("2026-09-21 assay roadmap.md") {
		t.Fatal("unmapped note became eligible")
	}

	// the existing owner path records the confirmed tier (temp copy only)
	approved := approvals.Proposal{ID: "g1", Type: approvals.TypeCreateVaultNote, ApplyPath: "2026-09-21 Assay Roadmap.md", Proposed: aionGranolaNote}
	if err := s.aionRecordVisibility(approved, "open"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(mapPath)
	tm, err := aion.ParseTierMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := tm["2026-09-21 assay roadmap.md"]
	if !ok || e.Tier != aion.TierOpen || !strings.Contains(e.Reason, "owner · approvals inbox (granola)") || !strings.Contains(e.Reason, "jev advised open") {
		t.Fatalf("recorded = %+v %v", e, ok)
	}
	if !tm.PortalEligible("2026-09-21 assay roadmap.md") {
		t.Fatal("confirmed open is not eligible")
	}
}

// Held-class signals and low confidence default held whatever Jev picked.
func TestJevAutoTierPolicyKeepsHeld(t *testing.T) {
	for name, f := range map[string]*typesafe.Fake{
		"signal":     tierFakeAnswers("open", 0.9, map[string]float64{"open": 0.9, "held": 0.05}, map[string]float64{"compensation": 0.8}),
		"confidence": tierFakeAnswers("internal", 0.3, map[string]float64{"internal": 0.4, "open": 0.35, "held": 0.25}, nil),
		"heldProb":   tierFakeAnswers("internal", 0.6, map[string]float64{"internal": 0.6, "held": 0.35, "open": 0.05}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			s, root, _ := autoVisibilityServer(t, f)
			plantCreateNote(t, root, "g1", "2026-09-21 Comp Chat.md", aionGranolaNote)
			s.jevAutoSweep(context.Background())
			g := suggestionFor(t, s, "g1")
			if g.Suggested != aion.TierHeld || g.Jev == nil || g.Jev.Advice == nil || len(g.Jev.Advice.Policy) == 0 {
				t.Fatalf("suggestion = %+v jev = %+v", g, g.Jev)
			}
			if g.Jev.Advice.JevTier == aion.TierHeld {
				t.Fatalf("fixture should have Jev pick a looser tier: %+v", g.Jev.Advice)
			}
		})
	}
}

// Missing key, a failing Jev and an oversize transcript all leave the card
// on held with a reason, and listing never blocks or crashes.
func TestJevAutoTierSafeFallbacks(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		s, root, _ := autoVisibilityServer(t, nil)
		s.UseJevAdvice(t.TempDir()) // wired as in production, but no key
		plantCreateNote(t, root, "g1", "2026-09-21 Assay Roadmap.md", aionGranolaNote)
		s.jevAutoSweep(context.Background())
		g := suggestionFor(t, s, "g1")
		if g.Suggested != aion.TierHeld || g.Basis != "default" || g.Jev == nil || g.Jev.State != jevStateDisabled || !strings.Contains(g.Jev.Reason, "TYPESAFE_API_KEY") {
			t.Fatalf("suggestion = %+v jev = %+v", g, g.Jev)
		}
	})
	t.Run("not wired", func(t *testing.T) {
		s, root, _ := autoVisibilityServer(t, nil)
		plantCreateNote(t, root, "g1", "2026-09-21 Assay Roadmap.md", aionGranolaNote)
		if g := suggestionFor(t, s, "g1"); g.Suggested != aion.TierHeld || g.Jev.State != jevStateDisabled {
			t.Fatalf("suggestion = %+v", g)
		}
	})
	t.Run("error", func(t *testing.T) {
		f := &typesafe.Fake{Err: errors.New("typesafe: 500 upstream")}
		s, root, _ := autoVisibilityServer(t, f)
		plantCreateNote(t, root, "g1", "2026-09-21 Assay Roadmap.md", aionGranolaNote)
		s.jevAutoSweep(context.Background())
		g := suggestionFor(t, s, "g1")
		if g.Suggested != aion.TierHeld || g.Jev.State != jevStateError || !strings.Contains(g.Jev.Reason, "500") {
			t.Fatalf("suggestion = %+v jev = %+v", g, g.Jev)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		f := tierFakeAnswers("open", 0.99, map[string]float64{"open": 0.99}, nil)
		s, root, _ := autoVisibilityServer(t, f)
		big := aionGranolaNote + strings.Repeat("salary details ", jev.MaxTierText/10)
		plantCreateNote(t, root, "g1", "2026-09-21 Long Meeting.md", big)
		s.jevAutoSweep(context.Background())
		if len(f.Requests) != 0 {
			t.Fatal("an oversize transcript was sent to Jev")
		}
		g := suggestionFor(t, s, "g1")
		if g.Suggested != aion.TierHeld || g.Jev.State != jevStateOversize || !strings.Contains(g.Jev.Reason, "never truncated") {
			t.Fatalf("suggestion = %+v jev = %+v", g, g.Jev)
		}
	})
	t.Run("not aion-tagged", func(t *testing.T) {
		f := tierFakeAnswers("open", 0.99, map[string]float64{"open": 0.99}, nil)
		s, root, _ := autoVisibilityServer(t, f)
		plantCreateNote(t, root, "e1", "2026-09-19 hi.md", "---\ncategories: [personal]\ngmail-thread-id: th_1\n---\n\n## 2026-09-19 — them\n\nhi\n")
		s.jevAutoSweep(context.Background())
		if len(f.Requests) != 0 {
			t.Fatal("a non-aion note was sent to Jev")
		}
		if g := suggestionFor(t, s, "e1"); g.Suggested != aion.TierHeld || g.Jev != nil {
			t.Fatalf("suggestion = %+v", g)
		}
	})
}

// Advice is cached by proposal + content hash in the store dir: a restart
// does not re-ask, a changed transcript does.
func TestJevAutoTierCachePersists(t *testing.T) {
	f := tierFakeAnswers("internal", 0.8, map[string]float64{"internal": 0.8, "open": 0.15, "held": 0.05}, nil)
	s, root, _ := autoVisibilityServer(t, f)
	dir := t.TempDir()
	s.UseJevAdvice(dir)
	plantCreateNote(t, root, "g1", "2026-09-21 Assay Roadmap.md", aionGranolaNote)
	s.jevAutoSweep(context.Background())

	s2, _, _ := autoVisibilityServer(t, f)
	s2.UseHarnesses(s.harnessList)
	s2.UseJevAdvice(dir)
	s2.jevAutoSweep(context.Background())
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d, want 1 (cache hit after restart)", len(f.Requests))
	}
	if g := suggestionFor(t, s2, "g1"); g.Suggested != aion.TierInternal || g.Basis != "jev" {
		t.Fatalf("restarted suggestion = %+v", g)
	}
	plantCreateNote(t, root, "g1", "2026-09-21 Assay Roadmap.md", aionGranolaNote+"\nMore.\n")
	s2.jevAutoSweep(context.Background())
	if len(f.Requests) != 2 {
		t.Fatalf("requests = %d, want 2 (changed content re-asked)", len(f.Requests))
	}
	// the stored entry carries no transcript text
	entries, _ := filepath.Glob(filepath.Join(dir, jevKindTier, "*.json"))
	for _, p := range entries {
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), "reviewed the assay") {
			t.Fatalf("cache %s stores transcript text", p)
		}
	}
}

// ------------------------------------------------------------ clarify gate

func clarifyFake(decision string, risk float64) *typesafe.Fake {
	return &typesafe.Fake{Answers: map[string]typesafe.Answer{
		"gate":               typesafe.ChoiceAnswer(decision, 0.8, map[string]float64{decision: 0.8}),
		"risk":               typesafe.ScoreAnswer(risk, 0.7, nil, nil),
		"source_retrievable": typesafe.NoulAnswer(0.3),
		"continuation":       typesafe.NoulAnswer(0.2),
	}}
}

func TestJevAutoClarifyGateWorkOrder(t *testing.T) {
	t.Setenv(typesafe.EnvKey, "")
	want := map[string]string{
		"act_now":              "leans Tier 1",
		"ask_user":             "leans Tier 2 with a leading # questions",
		"inspect_source_first": "look the missing detail up first",
		"stop_due_to_risk":     "leans Tier 2 PLAN-GATE — do not execute",
	}
	for decision, hint := range want {
		t.Run(decision, func(t *testing.T) {
			srv := personaFixture(t)
			f := clarifyFake(decision, 1)
			srv.jevJudge = &jev.Judge{Eval: f}
			h := srv.findHarness("hermes")
			if err := srv.spoolTaskWorkOrder(h, "inbox/research-zoning", "comment", "pull the zoning notes", ""); err != nil {
				t.Fatal(err)
			}
			prompt := h.Spirits.Queued()[0].Request
			if !strings.Contains(prompt, "ALFRED TASK TIERS") {
				t.Fatalf("fixture is not on the Alfred tier path: %s", prompt)
			}
			if !strings.Contains(prompt, "JEV ADVISORY (clarify gate — advisory only") || !strings.Contains(prompt, decision) || !strings.Contains(prompt, hint) {
				t.Fatalf("prompt lacks the %s advisory: %s", decision, prompt)
			}
			if req := f.Last().State.(map[string]any)["request"]; req != "pull the zoning notes" {
				t.Fatalf("gate request = %v", req)
			}
		})
	}
	// off / failing → the work order is exactly what it was before
	for name, judge := range map[string]*jev.Judge{"off": nil, "error": {Eval: &typesafe.Fake{Err: errors.New("typesafe: timeout")}}} {
		t.Run(name, func(t *testing.T) {
			base := personaFixture(t)
			hb := base.findHarness("hermes")
			_ = base.spoolTaskWorkOrder(hb, "inbox/research-zoning", "comment", "pull the zoning notes", "")
			srv := personaFixture(t)
			srv.jevJudge = judge
			h := srv.findHarness("hermes")
			_ = srv.spoolTaskWorkOrder(h, "inbox/research-zoning", "comment", "pull the zoning notes", "")
			got, wantPrompt := h.Spirits.Queued()[0].Request, hb.Spirits.Queued()[0].Request
			if strings.Contains(got, "JEV ADVISORY") || got != wantPrompt {
				t.Fatalf("prompt changed with Jev %s:\n%s\n---\n%s", name, got, wantPrompt)
			}
		})
	}
	// a high-risk act_now carries its caution
	if line := clarifyAdvisoryLine(&jev.ClarifyGate{Decision: "act_now", Risk: jev.ScoreJudgment{Score: 3.4}, Cautions: []string{"act_now with risk 3.40 ≥ 3"}}); !strings.Contains(line, "Cautions: act_now with risk") {
		t.Fatalf("line = %q", line)
	}
	if clarifyAdvisoryLine(&jev.ClarifyGate{Decision: "bogus"}) != "" {
		t.Fatal("unknown decision rendered a line")
	}
}

// ----------------------------------------------------------- approval risk

func TestJevAutoApprovalRiskAnnotates(t *testing.T) {
	t.Setenv(typesafe.EnvKey, "")
	f := &typesafe.Fake{Answers: map[string]typesafe.Answer{
		"category":     typesafe.ChoiceAnswer("push_to_remote", 0.85, map[string]float64{"push_to_remote": 0.85}),
		"risk":         typesafe.ScoreAnswer(3.1, 0.7, nil, nil),
		"irreversible": typesafe.NoulAnswer(0.2),
	}}
	p := approvals.Proposal{ID: "op1", Type: approvals.TypeManifestOperation, Action: "Push manifest to origin/main", Body: "git push origin HEAD:main"}
	s := &Server{}
	if s.jevApprovalRisk(p) != nil {
		t.Fatal("annotated with Jev off")
	}
	s.jevJudge = &jev.Judge{Eval: f}
	s.jevStore(s.computeApprovalRisk(context.Background(), p, approvalRiskText(p)))
	v := s.jevApprovalRisk(p)
	if v == nil || v.State != jevStateAdvised || v.Category != "push_to_remote" || !v.Advisory || v.Authoritative != "the existing approval gate" || v.Result.Irreversible != 0.2 {
		t.Fatalf("risk = %+v", v)
	}
	if s.jevApprovalRisk(approvals.Proposal{ID: "n1", Type: approvals.TypeCreateVaultNote}) != nil {
		t.Fatal("a non-operation card was annotated")
	}
}

// --------------------------------------------------------- coding sidecar

func TestJevAutoRunAdviceSidecar(t *testing.T) {
	t.Setenv(typesafe.EnvKey, "")
	result := codingResult{Status: "completed", Summary: "All tests pass; pushed.", ArtifactURL: "https://example.test/commit/abc"}
	dir := t.TempDir()
	(&Server{}).jevRunAdvise(context.Background(), dir, "ship it", result)
	if _, err := os.Stat(filepath.Join(dir, jevRunAdviceFile)); err == nil {
		t.Fatal("sidecar written with Jev off")
	}
	f := &typesafe.Fake{Answers: map[string]typesafe.Answer{
		"verdict":              typesafe.ChoiceAnswer("partial", 0.7, map[string]float64{"partial": 0.7}),
		"overclaims":           typesafe.NoulAnswer(0.6),
		"missing_verification": typesafe.NoulAnswer(0.7),
		"state":                typesafe.ChoiceAnswer("ready_to_verify", 0.6, map[string]float64{"ready_to_verify": 0.6}),
	}}
	s := &Server{jevJudge: &jev.Judge{Eval: f}}
	s.jevRunAdvise(context.Background(), dir, "ship it", result)
	raw, err := os.ReadFile(filepath.Join(dir, jevRunAdviceFile))
	if err != nil {
		t.Fatal(err)
	}
	var out jevRunAdvice
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Advisory || out.Authoritative != "result.json status" || out.Status != "completed" ||
		out.Evidence == nil || out.Evidence.Verdict != "partial" || out.Goal == nil || out.Goal.State != "ready_to_verify" {
		t.Fatalf("sidecar = %+v", out)
	}
	s.jevRunAdvise(context.Background(), dir, "ship it", result)
	if len(f.Requests) != 2 {
		t.Fatalf("requests = %d, want 2 (same result not re-asked)", len(f.Requests))
	}
}
