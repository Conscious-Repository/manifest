package recruiting

import (
	"strings"
	"testing"
)

// gateFixture builds a role with one of each criterion class and a candidate
// judged against it, so each test states only what it changes.
func gateFixture(t *testing.T, fit, evidence []string) (*RoleDoc, Candidate) {
	t.Helper()
	role := ParseRole(`---
id: role/mri-engineer
title: MRI Engineer
---

## criteria
- [criterion:: low-field MRI hardware] [class:: must] [weight:: 3]
- [criterion:: on-site Saint Louis] [class:: must] [weight:: 2]
- [criterion:: rapid prototyping] [class:: nice] [weight:: 1]
- [criterion:: requires full remote] [class:: disqualifier]
`)
	body := "---\nid: cand/test\nname: Test Person\nrole: role/mri-engineer\nstage: reviewing\n---\n\n## fit\n"
	for _, ln := range fit {
		body += ln + "\n"
	}
	body += "\n## evidence\n"
	for _, ln := range evidence {
		body += ln + "\n"
	}
	return role, ParseCandidate(body).View("test", role)
}

// The D6 math: every must at ≥ 3, each backed by at least one evidence id.
func TestGatePassesWhenEveryMustIsEvidenced(t *testing.T) {
	_, c := gateFixture(t,
		[]string{
			"- [criterion:: low-field MRI hardware] [score:: 4] [evidence:: ev1]",
			"- [criterion:: on-site Saint Louis] [score:: 3] [evidence:: ev2]",
		},
		[]string{
			"- [id:: ev1] [url:: https://example.test/a] [collected:: 2026-09-01] [kind:: publication]",
			"- [id:: ev2] [url:: https://example.test/b] [collected:: 2026-09-01] [kind:: page]",
		})
	if !c.Gate.Passed || c.Gate.Overridden {
		t.Fatalf("gate: %+v", c.Gate)
	}
	if c.Gate.Musts != 2 || c.Gate.Satisfied != 2 || len(c.Gate.Unevidenced) != 0 {
		t.Fatalf("gate counts: %+v", c.Gate)
	}
	// a nice-to-have is never part of the bar
	if strings.Contains(strings.Join(c.Gate.Unevidenced, " "), "rapid prototyping") {
		t.Errorf("a nice criterion entered the gate: %+v", c.Gate)
	}
}

// The case the plan calls out by name: a numeric score with no evidence id
// parses fine, renders unscored, and blocks.
func TestGateBlocksAScoreWithNoEvidenceID(t *testing.T) {
	_, c := gateFixture(t,
		[]string{
			"- [criterion:: low-field MRI hardware] [score:: 5]",
			"- [criterion:: on-site Saint Louis] [score:: 4] [evidence:: ev1]",
		},
		[]string{"- [id:: ev1] [url:: https://example.test/b] [kind:: page]"})
	if c.Gate.Passed {
		t.Fatalf("an unevidenced 5 passed the gate: %+v", c.Gate)
	}
	if len(c.Gate.Unevidenced) != 1 || c.Gate.Unevidenced[0] != "low-field MRI hardware" {
		t.Fatalf("the gate did not name the blocking must: %+v", c.Gate)
	}
}

// An evidence id that resolves to nothing is not a citation.
func TestGateBlocksADanglingEvidenceID(t *testing.T) {
	_, c := gateFixture(t,
		[]string{
			"- [criterion:: low-field MRI hardware] [score:: 4] [evidence:: ev9]",
			"- [criterion:: on-site Saint Louis] [score:: 4] [evidence:: ev1]",
		},
		[]string{"- [id:: ev1] [url:: https://example.test/b] [kind:: page]"})
	if c.Gate.Passed {
		t.Fatalf("a dangling evidence id passed the gate: %+v", c.Gate)
	}
}

func TestGateBlocksBelowTheScoreBar(t *testing.T) {
	for _, score := range []string{"0", "1", "2", "unknown"} {
		_, c := gateFixture(t,
			[]string{
				"- [criterion:: low-field MRI hardware] [score:: " + score + "] [evidence:: ev1]",
				"- [criterion:: on-site Saint Louis] [score:: 4] [evidence:: ev1]",
			},
			[]string{"- [id:: ev1] [url:: https://example.test/b] [kind:: page]"})
		if c.Gate.Passed {
			t.Errorf("score %q passed the gate: %+v", score, c.Gate)
		}
	}
}

// A disqualifier marked present short-circuits an otherwise passing gate.
func TestGateShortCircuitsOnADisqualifier(t *testing.T) {
	_, c := gateFixture(t,
		[]string{
			"- [criterion:: low-field MRI hardware] [score:: 4] [evidence:: ev1]",
			"- [criterion:: on-site Saint Louis] [score:: 4] [evidence:: ev1]",
			"- [criterion:: requires full remote] [present:: true]",
		},
		[]string{"- [id:: ev1] [url:: https://example.test/b] [kind:: page]"})
	if c.Gate.Passed {
		t.Fatalf("a present disqualifier passed: %+v", c.Gate)
	}
	if len(c.Gate.Disqualifiers) != 1 || !strings.Contains(c.Gate.Reason, "disqualifier") {
		t.Fatalf("the gate did not name the disqualifier: %+v", c.Gate)
	}
	// a disqualifier that is NOT marked present is not a disqualification
	_, ok := gateFixture(t,
		[]string{
			"- [criterion:: low-field MRI hardware] [score:: 4] [evidence:: ev1]",
			"- [criterion:: on-site Saint Louis] [score:: 4] [evidence:: ev1]",
			"- [criterion:: requires full remote] [present:: false]",
		},
		[]string{"- [id:: ev1] [url:: https://example.test/b] [kind:: page]"})
	if !ok.Gate.Passed {
		t.Fatalf("an absent disqualifier blocked: %+v", ok.Gate)
	}
}

// The override lifts the gate AND says so — an override is never silent.
func TestGateOverrideIsRecordedAndVisible(t *testing.T) {
	s, _ := testStore(t)
	c, err := s.AddCandidate(QuickAdd{Text: "Sasha Prynne", Role: "role/mri-engineer"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.Gate.Passed {
		t.Fatalf("a brand-new candidate passed the gate: %+v", c.Gate)
	}
	got, err := s.SetOverride(c.ID, "benjamin", "on-site confirmed verbally 2026-09-03", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Gate.Passed || !got.Gate.Overridden {
		t.Fatalf("override did not lift the gate: %+v", got.Gate)
	}
	if got.Override.By != "benjamin" || got.Override.At != "2026-09-02" ||
		!strings.Contains(got.Gate.Reason, "on-site confirmed verbally") {
		t.Fatalf("the override was not recorded with its reason and date: %+v", got.Override)
	}
	// it is on the RECORD, not only in the projection
	raw := s.raw("candidates/sasha-prynne.md")
	if !strings.Contains(raw, "[override:: benjamin]") ||
		!strings.Contains(raw, "[override_at:: 2026-09-02]") {
		t.Fatalf("the override is not on the record:\n%s", raw)
	}
	if _, err := s.SetOverride(c.ID, "benjamin", "", testNow); err == nil {
		t.Error("an override with no reason was accepted")
	}
	cleared, err := s.SetOverride(c.ID, "", "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Gate.Passed || cleared.Override.Present() {
		t.Fatalf("clearing the override left it in force: %+v", cleared.Gate)
	}
	if strings.Contains(s.raw("candidates/sasha-prynne.md"), "[override::") {
		t.Error("the cleared override row is still on the record")
	}
}

// A role whose posting has not been translated into must/nice has no bar to
// clear. Vacuous truth would let everything through the one gate that exists,
// so it blocks and says why.
func TestGateBlocksWhenTheRoleHasNoMusts(t *testing.T) {
	role := ParseRole("---\nid: role/empty\n---\n\n## criteria\n")
	c := ParseCandidate("---\nid: cand/x\nname: X\n---\n").View("x", role)
	if c.Gate.Passed || !strings.Contains(c.Gate.Reason, "no must criteria") {
		t.Fatalf("gate: %+v", c.Gate)
	}
}

// An untethered candidate is judged against nothing, so nothing passes.
func TestGateBlocksWithNoRole(t *testing.T) {
	c := ParseCandidate("---\nid: cand/x\nname: X\n---\n").View("x", nil)
	if c.Gate.Passed || !strings.Contains(c.Gate.Reason, "no role") {
		t.Fatalf("gate: %+v", c.Gate)
	}
}

// Criteria are matched between role and candidate by their text, so an
// Obsidian edit that changes only spacing or case still lines up.
func TestGateMatchesCriteriaLoosely(t *testing.T) {
	_, c := gateFixture(t,
		[]string{
			"- [criterion:: Low-Field   MRI hardware] [score:: 4] [evidence:: ev1]",
			"- [criterion:: on-site saint louis] [score:: 4] [evidence:: ev1]",
		},
		[]string{"- [id:: ev1] [url:: https://example.test/b] [kind:: page]"})
	if !c.Gate.Passed {
		t.Fatalf("criterion matching is too strict: %+v", c.Gate)
	}
}

func TestScoreValidation(t *testing.T) {
	for _, ok := range []string{"0", "3", "5", "unknown"} {
		if err := ValidateScore(ok); err != nil {
			t.Errorf("score %q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "6", "-1", "high", "3.5", "UNKNOWN"} {
		if err := ValidateScore(bad); err == nil {
			t.Errorf("score %q accepted", bad)
		}
	}
	s, _ := testStore(t)
	c, err := s.AddCandidate(QuickAdd{Text: "Lux Amari", Role: "role/mri-engineer"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScoreFit(c.ID, "low-field MRI hardware", "9", nil, false); err == nil {
		t.Error("an out-of-range score was written")
	}
	if _, err := s.ScoreFit(c.ID, "", "3", nil, false); err == nil {
		t.Error("a criterion-less fit row was written")
	}
	got, err := s.ScoreFit(c.ID, "low-field MRI hardware", "4", []string{"ev1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fit) != 1 || got.Fit[0].Score != "4" || got.Fit[0].Evidence[0] != "ev1" {
		t.Fatalf("fit row: %+v", got.Fit)
	}
	// scoring the same criterion again edits the row in place
	if got, err = s.ScoreFit(c.ID, "low-field MRI hardware", "2", nil, false); err != nil {
		t.Fatal(err)
	}
	if len(got.Fit) != 1 || got.Fit[0].Score != "2" || len(got.Fit[0].Evidence) != 0 {
		t.Fatalf("a re-score appended instead of editing: %+v", got.Fit)
	}
}
