package server

import (
	"testing"
	"time"

	"manifest/approvals"
)

// The FEED is the approvals inbox (approvals-move-to-feed plan): its proposals
// are FULL enriched rows for every pending approval, and the badge counts that
// same set. (No approval type currently has a native feed card of its own.)
func TestFeedApprovalsInbox(t *testing.T) {
	s := New(nil, nil, nil)
	s.UseApprovals(approvals.NewStore(t.TempDir()))

	mk := func(typ, action, applyPath, proposed string) {
		t.Helper()
		body := "evidence\n\n````proposed\n" + proposed + "\n````"
		if _, err := s.approvals.Propose(approvals.Proposal{
			Type: typ, Action: action, Agent: "tester", Ritual: "tune",
			Body: body, ApplyPath: applyPath,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("approval", "tune the cornerstone", "spirits/domain-scout/cornerstone.md", "new prose")
	mk(approvals.TypeCreateVaultNote, "Create vault note: 2026-07-22 sync.md", "2026-07-22 sync.md", "note body")

	rows := s.feedProposals()
	if len(rows) != 2 {
		t.Fatalf("feedProposals = %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if !r.Allowed {
			t.Fatalf("row %q (%s) should be allowed", r.Action, r.Type)
		}
		if r.Proposed == "" {
			t.Fatalf("row %q missing proposed payload for the diff", r.Action)
		}
	}

	// Badge = items(0, spirits nil) + signals(0, nil) + the same 2 approvals.
	if n := s.feedInboxCount(time.Now()); n != 2 {
		t.Fatalf("feedInboxCount = %d, want 2", n)
	}

	// The SPIRITS endpoint returns the same rows (no exclusion in play).
	if all := s.approvalRows(nil); len(all) != 2 {
		t.Fatalf("approvalRows(nil) = %d, want 2", len(all))
	}
}
