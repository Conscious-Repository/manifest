package spirits

import "testing"

// The RUNS log's "deliverable" link opens what a run wrote through the
// read-only file route: library briefs and feed cards are readable, nothing
// else under artifacts/ is, and no read path becomes writable.
func TestAllowedReadPath_Deliverables(t *testing.T) {
	ok := []string{
		"artifacts/library/2026-09-03-brief.md",
		"artifacts/feed/briefing-2026-09-03-da4fad61.md",
		"spirits/concierge/rituals/briefing.md", // edit paths stay readable
	}
	for _, p := range ok {
		if _, allowed := allowedReadPath(p); !allowed {
			t.Errorf("%s: want readable", p)
		}
	}
	no := []string{
		"artifacts/runs/2026-09-03-concierge-x.md", // reports have their own route
		"artifacts/approvals/pending/x.md",
		"artifacts/feed/nested/x.md",
		"artifacts/feed/x.json",
		"../artifacts/feed/x.md",
		"/etc/passwd",
	}
	for _, p := range no {
		if _, allowed := allowedReadPath(p); allowed {
			t.Errorf("%s: want refused", p)
		}
	}
	for _, p := range []string{"artifacts/feed/x.md", "artifacts/library/x.md"} {
		if _, allowed := allowedEditPath(p); allowed {
			t.Errorf("%s: deliverables must never be writable", p)
		}
	}
}
