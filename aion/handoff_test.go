package aion

import (
	"os"
	"strings"
	"testing"
)

// The REAL draft handoff document is the parser fixture (plans/ symlinks
// into the vault; skip when absent — CI). The owner's final payload arrives
// later; this pins the format.
func loadHandoff(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../plans/aion-historic-backlog-handoff.md")
	if err != nil {
		t.Skip("draft handoff not present")
	}
	return string(b)
}

func TestParseHandoffRealDraft(t *testing.T) {
	payloads, err := ParseHandoff(loadHandoff(t))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, p := range payloads {
		counts[p.Kind]++
	}
	// the draft's own audit facts: 30 open + 65 done tasks, 32 decided +
	// 10 open decisions, 29 heuristics = 166
	if counts[KindTask] != 95 || counts[KindDecision] != 42 || counts[KindHeuristic] != 29 {
		t.Fatalf("counts: %v (want 95 tasks, 42 decisions, 29 heuristics)", counts)
	}

	var openTasks, doneTasks, decided, openDecisions int
	for _, p := range payloads {
		switch {
		case p.Kind == KindTask && p.Status == StatusOpen:
			openTasks++
			if p.Review != "confirm-open" {
				t.Errorf("open task without review flag: %q", p.Title)
			}
		case p.Kind == KindTask && p.Status == StatusDone:
			doneTasks++
			if p.DoneOn == "" {
				t.Errorf("done task without done_on: %q", p.Title)
			}
		case p.Kind == KindDecision && p.Status == StatusDecided:
			decided++
			if p.Decided == "" || p.Outcome == "" {
				t.Errorf("decided decision missing decided/outcome: %q", p.Title)
			}
		case p.Kind == KindDecision:
			openDecisions++
			if p.Owner == "" {
				t.Errorf("open decision without decides→owner: %q", p.Title)
			}
		}
		if p.Kind != KindHeuristic && len(p.Sources) == 0 {
			t.Errorf("record without source: %q", p.Title)
		}
		if err := p.Validate(nil); err != nil {
			t.Errorf("invalid payload %q: %v", p.Title, err)
		}
	}
	if openTasks != 30 || doneTasks != 65 || decided != 32 || openDecisions != 10 {
		t.Fatalf("split: %d open, %d done, %d decided, %d open decisions", openTasks, doneTasks, decided, openDecisions)
	}

	// evidence/confidence captured on the proposal…
	withQuote := 0
	for _, p := range payloads {
		if p.Quote != "" && p.Confidence > 0 {
			withQuote++
		}
	}
	if withQuote < 100 {
		t.Fatalf("only %d payloads carry evidence", withQuote)
	}
	// …and NEVER in the rendered record line
	for _, p := range payloads {
		if p.Kind == KindHeuristic {
			continue
		}
		line := RenderItemLine(p)
		// the review-side markers must never render (titles may legitimately
		// contain the words "evidence"/"confidence" — check the field shapes)
		for _, marker := range []string{"- evidence:", "confidence:", "[confidence::", "[review::", "[evidence::"} {
			if strings.Contains(line, marker) {
				t.Fatalf("%s leaked into record line: %s", marker, line)
			}
		}
		if p.Quote != "" && len(p.Quote) > 12 && strings.Contains(line, p.Quote) {
			t.Fatalf("quote leaked into record line: %s", line)
		}
	}

	// heuristics: mode new, multi-source reinforcement history preserved
	multi := 0
	for _, p := range payloads {
		if p.Kind == KindHeuristic {
			if p.Heuristic.Mode != HeuristicModeNew {
				t.Fatalf("heuristic mode: %+v", p)
			}
			if len(p.Sources) > 1 {
				multi++
			}
		}
	}
	if multi == 0 {
		t.Fatal("no merged heuristic kept its reinforcement history")
	}

	// the whole payload files cleanly as proposal bodies that round-trip
	for _, p := range payloads[:10] {
		body := ProposalBody(p)
		re, ok := ParsePayload(body)
		if !ok || re.Title != p.Title || re.Kind != p.Kind {
			t.Fatalf("body round-trip failed for %q", p.Title)
		}
	}
}

func TestParseHandoffEvidenceLine(t *testing.T) {
	q, c := parseEvidenceLine(` "the crux of deep tech week"; confidence: 0.89`)
	if q != "the crux of deep tech week" || c != 0.89 {
		t.Fatalf("%q %v", q, c)
	}
	q, c = parseEvidenceLine(` explicit unchecked validation plan; confidence: 0.99`)
	if q != "explicit unchecked validation plan" || c != 0.99 {
		t.Fatalf("%q %v", q, c)
	}
	q, c = parseEvidenceLine(` "focus everything"; confidence: 0.93 (speaker was JR; owner confirmation advised)`)
	if q != "focus everything" || c != 0.93 {
		t.Fatalf("%q %v", q, c)
	}
}
