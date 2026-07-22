package server

import (
	"testing"
	"time"

	"manifest/approvals"
)

// The FEED is the approvals inbox (approvals-move-to-feed plan): its proposals
// are FULL enriched rows for every pending approval EXCEPT types with a native
// feed card (append-x-queue → the tweet-shaped draft card), and the badge
// counts exactly that same filtered set.
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
	mk("approval", "tune the cornerstone", "spirits/scribe/cornerstone.md", "new prose")
	mk(approvals.TypeUpdateVaultSkill, "revise the skill", "skills/x-content/SKILL.md", "new rulebook")
	mk(approvals.TypeCreateVaultNote, "Create vault note: 2026-07-22 sync.md", "2026-07-22 sync.md", "note body")
	mk(approvals.TypeAppendXQueue, "queue a post", "x posts.md", "- a bullet")

	rows := s.feedProposals()
	if len(rows) != 3 {
		t.Fatalf("feedProposals = %d rows, want 3 (append-x-queue excluded)", len(rows))
	}
	for _, r := range rows {
		if r.Type == approvals.TypeAppendXQueue {
			t.Fatal("append-x-queue must not surface as an approval card (draft card owns it)")
		}
		if !r.Allowed {
			t.Fatalf("row %q (%s) should be allowed", r.Action, r.Type)
		}
		if r.Proposed == "" {
			t.Fatalf("row %q missing proposed payload for the diff", r.Action)
		}
	}

	// Badge = items(0, spirits nil) + signals(0, nil) + the same 3 approvals.
	if n := s.feedInboxCount(time.Now()); n != 3 {
		t.Fatalf("feedInboxCount = %d, want 3", n)
	}

	// The SPIRITS endpoint keeps returning ALL rows (Studio tuning panel).
	if all := s.approvalRows(nil); len(all) != 4 {
		t.Fatalf("approvalRows(nil) = %d, want 4", len(all))
	}
}
