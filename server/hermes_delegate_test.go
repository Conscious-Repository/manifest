package server

import (
	"testing"

	"manifest/hermes"
)

// TestHermesTurnBudget pins the per-phase timeout choice (2026-09-05: a DO on
// a real implementation task died at the 8m quick-ask cap). The work-order
// phases — plan (the composer's DO tab / @agent::plan) and go (fire) — get the
// long execution budget; an ask (comment phase) and anything unrecognized get
// 0, i.e. the runner's configured default, so the cap for asks is unchanged.
func TestHermesTurnBudget(t *testing.T) {
	if hermesExecTurnSeconds <= int(hermes.DefaultTimeout.Seconds()) {
		t.Fatalf("execution budget %ds must exceed the runner default %s", hermesExecTurnSeconds, hermes.DefaultTimeout)
	}
	cases := []struct {
		phase string
		want  int
	}{
		{"go", hermesExecTurnSeconds},
		{"plan", hermesExecTurnSeconds},
		{"comment", 0}, // an ask — keeps the default
		{"", 0},
		{"dig", 0},
	}
	for _, c := range cases {
		if got := hermesTurnBudget(c.phase); got != c.want {
			t.Errorf("hermesTurnBudget(%q) = %d, want %d", c.phase, got, c.want)
		}
	}
	// the DO tab's dispatch resolves to the plan phase — that is the turn the
	// bug report hit, so it must be on the long budget
	if got := hermesTurnBudget(personaPhase("plan")); got != hermesExecTurnSeconds {
		t.Errorf("DO tab (intent plan → phase %q) budget = %d, want %d", personaPhase("plan"), got, hermesExecTurnSeconds)
	}
	if got := hermesTurnBudget(personaPhase("info")); got != 0 {
		t.Errorf("ask (intent info → phase %q) budget = %d, want the default (0)", personaPhase("info"), got)
	}
}
