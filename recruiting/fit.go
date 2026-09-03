package recruiting

import (
	"sort"
	"strings"
)

// The fit gate (D6 / plan §4.6), computed and NEVER stored:
//
//	gate(candidate, role) passes iff
//	    for every criterion c in role where c.class == "must":
//	        candidate.score(c) >= 3  AND  len(candidate.evidence(c)) >= 1
//	    and no criterion with class == "disqualifier" is marked present
//
// A numeric score with no resolvable [evidence::] id parses fine and fails
// the gate — it renders `unscored` and blocks outreach. That keeps a hand
// edit in Obsidian legal while keeping the bar honest.

// GateState is the computed gate, and the exact thing the UI chip renders.
type GateState struct {
	Passed     bool `json:"passed"`
	Overridden bool `json:"overridden"`
	// Musts / Satisfied are the counts behind the chip ("2/3 musts evidenced").
	Musts     int `json:"musts"`
	Satisfied int `json:"satisfied"`
	// Unevidenced names the must criteria still blocking, in role order — the
	// chip has to be able to say WHICH, not just that something is missing.
	Unevidenced []string `json:"unevidenced"`
	// Disqualifiers names the disqualifier criteria marked present.
	Disqualifiers []string `json:"disqualifiers"`
	Override      Override `json:"override"`
	Reason        string   `json:"reason"`
}

// EvaluateGate computes the D6 gate for one candidate against its role. A nil
// role means the candidate is not tethered to a role yet: nothing can be
// judged, so nothing passes.
func EvaluateGate(role *RoleDoc, c Candidate) GateState {
	g := GateState{Override: c.Override, Unevidenced: []string{}, Disqualifiers: []string{}}
	if role == nil {
		g.Reason = "no role — a candidate is judged against a role's criteria"
		return applyOverride(g)
	}

	// index the candidate's fit rows and evidence ids once
	fit := map[string]FitEntry{}
	for _, f := range c.Fit {
		fit[normCriterion(f.Criterion)] = f
	}
	haveEvidence := map[string]bool{}
	for _, ev := range c.Evidence {
		if id := strings.TrimSpace(ev.ID); id != "" {
			haveEvidence[id] = true
		}
	}

	for _, crit := range role.Criteria() {
		entry, judged := fit[normCriterion(crit.Criterion)]
		switch crit.Class {
		case ClassMust:
			g.Musts++
			if judged && mustSatisfied(entry, haveEvidence) {
				g.Satisfied++
				continue
			}
			g.Unevidenced = append(g.Unevidenced, crit.Criterion)
		case ClassDisqualifier:
			if judged && entry.Present {
				g.Disqualifiers = append(g.Disqualifiers, crit.Criterion)
			}
		}
	}
	sort.Strings(g.Disqualifiers)

	switch {
	case g.Musts == 0:
		// A role whose posting has not been translated into must/nice yet has
		// no bar to clear. Vacuous truth would let everything through the one
		// gate that exists, so an untranslated role blocks and says why.
		g.Reason = "role has no must criteria yet"
	case len(g.Disqualifiers) > 0:
		g.Reason = "disqualifier present: " + strings.Join(g.Disqualifiers, ", ")
	case len(g.Unevidenced) > 0:
		g.Reason = "unevidenced musts: " + strings.Join(g.Unevidenced, ", ")
	default:
		g.Passed = true
		g.Reason = "all musts evidenced"
	}
	return applyOverride(g)
}

// mustSatisfied is the per-criterion half of D6: score ≥ 3 AND at least one
// evidence id that RESOLVES to a row on the record. An id pointing at nothing
// is not a citation.
func mustSatisfied(e FitEntry, haveEvidence map[string]bool) bool {
	n, ok := ScoreValue(e.Score)
	if !ok || n < GateMinScore {
		return false
	}
	for _, id := range e.Evidence {
		if haveEvidence[strings.TrimSpace(id)] {
			return true
		}
	}
	return false
}

// applyOverride lifts a failing gate when Benjamin recorded an override. The
// override is never silent: it stays on the state, the chip says so, and the
// reason and date came off the record.
func applyOverride(g GateState) GateState {
	if !g.Override.Present() || g.Passed {
		return g
	}
	g.Passed = true
	g.Overridden = true
	g.Reason = "overridden by " + g.Override.By
	if g.Override.Reason != "" {
		g.Reason += " — " + g.Override.Reason
	}
	return g
}

// normCriterion is the criterion-identity rule: criteria are matched between
// a role and a candidate by their text, case- and whitespace-insensitively.
func normCriterion(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
