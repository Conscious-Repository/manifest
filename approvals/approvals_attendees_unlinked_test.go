package approvals

import "testing"

// Confirm's people rewrite keeps the unlinked line, minus anyone just linked.
func TestReplaceAttendeeLineKeepsUnlinkedPeople(t *testing.T) {
	in := "---\ncategories: [sync]\n---\n[[rj tevonian]]\n" + UnlinkedPeoplePrefix + "Jon Chu · Raquel Colom\n\n## 2026-09-25 — Jon Chu\n\nhi\n"
	got := replaceAttendeeLine(in, []string{"rj tevonian", "Jon Chu"})
	want := "---\ncategories: [sync]\n---\n[[rj tevonian]] [[Jon Chu]]\n" + UnlinkedPeoplePrefix + "Raquel Colom\n\n## 2026-09-25 — Jon Chu\n\nhi\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if got := replaceAttendeeLine(in, []string{"rj tevonian", "Jon Chu", "Raquel Colom"}); got != "---\ncategories: [sync]\n---\n[[rj tevonian]] [[Jon Chu]] [[Raquel Colom]]\n\n## 2026-09-25 — Jon Chu\n\nhi\n" {
		t.Fatalf("all linked → the unlinked line goes:\n%q", got)
	}
}
