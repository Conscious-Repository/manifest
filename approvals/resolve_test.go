package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
)

// seedResolveBacklog appends one open task + one open decision into the
// seeded aion backlog via the same transform the accept path uses.
func seedResolveBacklog(t *testing.T, vault string) {
	t.Helper()
	target := filepath.Join(vault, "system", "aion", "backlog.md")
	cur, _ := os.ReadFile(target)
	out := string(cur)
	var err error
	for _, p := range []aion.ProposalPayload{
		{Kind: "task", Title: "Send the venue contract", Owner: "BA", Status: "open", Captured: "2026-08-10"},
		{Kind: "decision", Title: "Pick the lab HVAC vendor", Status: "open", Captured: "2026-08-10"},
	} {
		out, err = aion.AppendBacklogItem(out, p)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolveProposal(payload aion.ProposalPayload) Proposal {
	body := "closed-loop: the owner's reply resolves this item\n\n> \"" + payload.Quote + "\"\n\n" +
		aion.RenderPayloadFence(payload)
	return Proposal{
		Type: TypeAionResolve, Action: "aion: resolve — " + payload.Title,
		Agent: "extractor", Ritual: "aion", Body: body, ApplyPath: AionBacklogPath,
	}
}

func TestResolveTaskMarksDone(t *testing.T) {
	s, vault, _ := aionTestStore(t)
	seedResolveBacklog(t, vault)
	target := filepath.Join(vault, "system", "aion", "backlog.md")
	before, _ := os.ReadFile(target)
	p, err := s.Propose(resolveProposal(aion.ProposalPayload{
		Kind: "task", Title: "Send the venue contract", Status: "done", DoneOn: "2026-08-12",
		Sources: []string{"2026-08-11 venue email thread"}, Quote: "sent it over this morning",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(target)
	if strings.Count(string(after), "\n") != strings.Count(string(before), "\n") {
		t.Fatalf("resolve must flip a line, not add one:\n%s", after)
	}
	if !strings.Contains(string(after), "- [x] Send the venue contract") ||
		!strings.Contains(string(after), "[done:: 2026-08-12]") ||
		!strings.Contains(string(after), "[source:: [[2026-08-11 venue email thread]]]") {
		t.Fatalf("task not resolved:\n%s", after)
	}
	if strings.Contains(string(after), "sent it over this morning") {
		t.Fatal("evidence quote leaked into the record")
	}
	// untouched decision line stays byte-stable
	if !strings.Contains(string(after), "Pick the lab HVAC vendor [id:: aion-bl/pick-the-lab-hvac-vendor] [kind:: decision]") {
		t.Fatalf("unrelated line disturbed:\n%s", after)
	}
}

func TestResolveDecisionDecides(t *testing.T) {
	s, vault, _ := aionTestStore(t)
	seedResolveBacklog(t, vault)
	p, err := s.Propose(resolveProposal(aion.ProposalPayload{
		Kind: "decision", Title: "Pick the lab HVAC vendor", Status: "decided",
		Decided: "2026-08-12", Outcome: "went with Airtech per the quote",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(vault, "system", "aion", "backlog.md"))
	if !strings.Contains(string(after), "[status:: decided]") ||
		!strings.Contains(string(after), "[decided:: 2026-08-12]") ||
		!strings.Contains(string(after), "[outcome:: went with Airtech per the quote]") {
		t.Fatalf("decision not decided:\n%s", after)
	}
}

func TestResolveRefusals(t *testing.T) {
	_, vault, _ := aionTestStore(t)
	seedResolveBacklog(t, vault)
	now := time.Now()
	target := filepath.Join(vault, "system", "aion", "backlog.md")
	cur, _ := os.ReadFile(target)

	for _, tc := range []struct {
		name string
		p    aion.ProposalPayload
		msg  string
	}{
		{"not found", aion.ProposalPayload{Kind: "task", Title: "No such item", Status: "done"}, "not found"},
		{"task wrong verb", aion.ProposalPayload{Kind: "task", Title: "Send the venue contract", Status: "decided"}, "resolves to status done"},
		{"decision no outcome", aion.ProposalPayload{Kind: "decision", Title: "Pick the lab HVAC vendor", Status: "decided"}, "outcome is required"},
		{"heuristic", aion.ProposalPayload{Kind: "heuristic", Title: "x", Status: "done", Heuristic: aion.HeuristicIntent{Mode: "new"}}, "reinforced, not resolved"},
	} {
		if _, err := aion.ResolveBacklogItem(string(cur), tc.p, now); err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s: got %v, want error containing %q", tc.name, err, tc.msg)
		}
	}

	// already resolved refuses on a second pass
	out, err := aion.ResolveBacklogItem(string(cur), aion.ProposalPayload{
		Kind: "task", Title: "Send the venue contract", Status: "done",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aion.ResolveBacklogItem(out, aion.ProposalPayload{
		Kind: "task", Title: "Send the venue contract", Status: "done",
	}, now); err == nil || !strings.Contains(err.Error(), "already done") {
		t.Errorf("re-done: got %v", err)
	}
	out2, err := aion.ResolveBacklogItem(out, aion.ProposalPayload{
		Kind: "decision", Title: "Pick the lab HVAC vendor", Status: "decided", Outcome: "airtech",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aion.ResolveBacklogItem(out2, aion.ProposalPayload{
		Kind: "decision", Title: "Pick the lab HVAC vendor", Status: "decided", Outcome: "again",
	}, now); err == nil || !strings.Contains(err.Error(), "already decided") {
		t.Errorf("re-decide: got %v", err)
	}
}

func TestResolveEditKeepsKind(t *testing.T) {
	s, vault, _ := aionTestStore(t)
	seedResolveBacklog(t, vault)
	p, err := s.Propose(resolveProposal(aion.ProposalPayload{
		Kind: "task", Title: "Send the venue contract", Status: "done",
	}))
	if err != nil {
		t.Fatal(err)
	}
	// a kind flip is refused; a title edit lands in the fence
	if err := s.SetAionPayload(p.ID, aion.ProposalPayload{
		Kind: "decision", Title: "Send the venue contract", Status: "decided", Outcome: "x",
	}); err == nil || !strings.Contains(err.Error(), "keeps its kind") {
		t.Fatalf("kind flip: got %v", err)
	}
	if err := s.SetAionPayload(p.ID, aion.ProposalPayload{
		Kind: "task", Title: "Send the venue contract (signed)", Status: "done",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadPending(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := AionPayload(got)
	if !ok || payload.Title != "Send the venue contract (signed)" {
		t.Fatalf("edit did not land: %+v ok=%v", payload, ok)
	}
	if got.Type != TypeAionResolve || got.ApplyPath != AionBacklogPath {
		t.Fatalf("type/apply-path drifted: %s %s", got.Type, got.ApplyPath)
	}
}
