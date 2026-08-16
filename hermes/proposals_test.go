package hermes

import (
	"strings"
	"testing"
)

func TestParseProposalsHappyPath(t *testing.T) {
	reply := "# RESULT\n\nDid the work.\n\n" +
		"```manifest-proposal\n" +
		`{"type":"create-vault-note","title":"Vendor shortlist","body":"- acme\n- globex"}` + "\n" +
		"```\n\nAlso queued the outreach:\n\n" +
		"```manifest-proposal\n" +
		`{"type":"run-errand","errand":"email the acme rep to ask for a quote"}` + "\n" +
		"```\n\nDone."
	clean, specs, warns := ParseProposals(reply)
	if len(warns) != 0 {
		t.Fatalf("warnings = %v", warns)
	}
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}
	if specs[0].Type != "create-vault-note" || specs[0].Title != "Vendor shortlist" {
		t.Errorf("spec[0] = %+v", specs[0])
	}
	if specs[1].Type != "run-errand" || !strings.Contains(specs[1].Errand, "acme rep") {
		t.Errorf("spec[1] = %+v", specs[1])
	}
	// blocks replaced by placeholders; no raw JSON leaks into the thread text
	if strings.Contains(clean, "manifest-proposal") || strings.Contains(clean, `"type"`) {
		t.Errorf("raw block leaked into clean text:\n%s", clean)
	}
	for _, want := range []string{"→ proposed: vault note — Vendor shortlist", "→ proposed: errand — email the acme rep", "# RESULT", "Done."} {
		if !strings.Contains(clean, want) {
			t.Errorf("clean text missing %q:\n%s", want, clean)
		}
	}
}

func TestParseProposalsBacklogKinds(t *testing.T) {
	reply := "```manifest-proposal\n" +
		`{"type":"aion-backlog","kind":"task","title":"Order the ICR reagents","owner":"MM","sources":["irving sync"]}` + "\n```\n" +
		"```manifest-proposal\n" +
		`{"type":"re-backlog","kind":"decision","title":"Pass on 752 Bayard"}` + "\n```\n" +
		"```manifest-proposal\n" +
		`{"type":"aion-backlog","kind":"heuristic","title":"nope"}` + "\n```"
	_, specs, warns := ParseProposals(reply)
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2 (heuristic kind must be dropped)", len(specs))
	}
	if specs[0].Owner != "MM" || len(specs[0].Sources) != 1 {
		t.Errorf("aion spec = %+v", specs[0])
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "task or decision") {
		t.Errorf("warns = %v", warns)
	}
}

func TestParseProposalsBadBlocks(t *testing.T) {
	reply := "text\n```manifest-proposal\nnot json at all\n```\n" +
		"```manifest-proposal\n" + `{"type":"format-disk"}` + "\n```\n" +
		"```manifest-proposal\n" + `{"type":"create-vault-note","title":"x"}` + "\n```\ntail"
	clean, specs, warns := ParseProposals(reply)
	if len(specs) != 0 {
		t.Fatalf("specs = %v, want none", specs)
	}
	if len(warns) != 3 {
		t.Fatalf("warns = %d (%v), want 3", len(warns), warns)
	}
	if !strings.Contains(warns[0], "not valid JSON") || !strings.Contains(warns[1], "unknown proposal type") ||
		!strings.Contains(warns[2], "title and body") {
		t.Errorf("warns = %v", warns)
	}
	if !strings.Contains(clean, "text") || !strings.Contains(clean, "tail") {
		t.Errorf("surrounding text lost:\n%s", clean)
	}
}

func TestParseProposalsNoBlocksAndOtherFences(t *testing.T) {
	reply := "plain result\n```go\nfmt.Println(1)\n```\nend"
	clean, specs, warns := ParseProposals(reply)
	if len(specs) != 0 || len(warns) != 0 {
		t.Fatalf("specs=%v warns=%v, want none", specs, warns)
	}
	if clean != reply {
		t.Errorf("non-proposal fences must pass through untouched:\n%s", clean)
	}
}

func TestParseProposalsUnclosedFence(t *testing.T) {
	reply := "head\n```manifest-proposal\n" + `{"type":"run-errand","errand":"call the bank"}`
	clean, specs, warns := ParseProposals(reply)
	if len(specs) != 1 || len(warns) != 0 {
		t.Fatalf("specs=%v warns=%v", specs, warns)
	}
	if !strings.Contains(clean, "head") || !strings.Contains(clean, "→ proposed: errand — call the bank") {
		t.Errorf("clean = %q", clean)
	}
}

func TestParseProposalsPathTraversalTitle(t *testing.T) {
	reply := "```manifest-proposal\n" +
		`{"type":"create-vault-note","title":"../../etc/cron","body":"x"}` + "\n```"
	_, specs, warns := ParseProposals(reply)
	if len(specs) != 0 || len(warns) != 1 {
		t.Fatalf("specs=%v warns=%v — traversal title must be rejected", specs, warns)
	}
}
