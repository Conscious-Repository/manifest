package approvals

import "testing"

// The apply-path allow-lists are a BYTE-CONTRACT with the excalibur engine's
// audit predicates (audit.ApplyPathAllowed and the aion twins). The two live in
// separate modules, so the guarantee is these shared vector tables: they are
// duplicated verbatim in the engine's audit/bytecontract_test.go, and both
// assert the SAME expected verdicts. If you change a predicate on one side, the
// table changes, and the sibling test must be updated to match.
//
// Keep these tables identical to engine/internal/audit/bytecontract_test.go.

type pathVec struct {
	path  string
	apply bool // ApplyPathAllowed (tuning surfaces)
}

var contractVectors = []pathVec{
	// tuning surfaces (ApplyPathAllowed): cornerstone / rituals / chargebook
	{"chargebook.md", true},
	{"spirits/domain-scout/cornerstone.md", true},
	{"spirits/domain-scout/rituals/tune.md", true},
	{"spirits/domain-scout/memories/long-term.md", false},
	{"", false},
}

// aionContractVectors pin the aion apply-path predicates the same way —
// duplicated verbatim in engine/internal/audit/bytecontract_test.go.
var aionContractVectors = []struct {
	path      string
	backlog   bool // AionBacklogPathAllowed
	heuristic bool // AionHeuristicPathAllowed
}{
	{"system/aion/backlog.md", true, false},
	{"system/aion/heuristics.md", false, true},
	{"system/aion/Backlog.md", false, false},
	{"backlog.md", false, false},
	{"system/aion/../aion/backlog.md", false, false},
	{"/system/aion/backlog.md", false, false},
	{"system/aion/backlog.md ", false, false},
	{"system/aion/people.md", false, false},
	{"", false, false},
}

func TestByteContract_AionPredicates(t *testing.T) {
	for _, v := range aionContractVectors {
		if got := AionBacklogPathAllowed(v.path); got != v.backlog {
			t.Errorf("AionBacklogPathAllowed(%q) = %v, want %v", v.path, got, v.backlog)
		}
		if got := AionHeuristicPathAllowed(v.path); got != v.heuristic {
			t.Errorf("AionHeuristicPathAllowed(%q) = %v, want %v", v.path, got, v.heuristic)
		}
	}
}

func TestByteContract_ManifestPredicates(t *testing.T) {
	for _, v := range contractVectors {
		if got := ApplyPathAllowed(v.path); got != v.apply {
			t.Errorf("ApplyPathAllowed(%q) = %v, want %v", v.path, got, v.apply)
		}
	}
}
