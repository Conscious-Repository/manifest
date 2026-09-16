package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
)

func TestApprovalActionErrorBoundary(t *testing.T) {
	for _, action := range []string{"confirm", "reject", "dismiss", "aion", "goals"} {
		t.Run(action, func(t *testing.T) {
			for _, scenario := range []string{"missing", "read failure", "malformed JSON"} {
				t.Run(scenario, func(t *testing.T) {
					dir := filepath.Join(t.TempDir(), "private", "artifacts")
					store := approvals.NewStore(dir)
					s := &Server{approvals: store}
					id, body, wantStatus := "does-not-exist", "{}", http.StatusNotFound
					if scenario == "read failure" {
						// Reading a directory fails even when tests run as root.
						if err := os.Mkdir(filepath.Join(dir, "approvals", "pending", id+".md"), 0o700); err != nil {
							t.Fatal(err)
						}
						wantStatus = http.StatusBadRequest
					}
					if scenario == "malformed JSON" {
						if action != "aion" && action != "goals" {
							t.Skip("handler does not require a JSON body")
						}
						body, wantStatus = "{", http.StatusBadRequest
					}
					before := store.Counts()
					handlers := map[string]http.HandlerFunc{
						"confirm": s.handleSpiritsApprovalConfirm,
						"reject":  s.handleSpiritsApprovalReject,
						"dismiss": s.handleSpiritsApprovalDismiss,
						"aion":    s.handleSpiritsApprovalAion,
						"goals":   s.handleSpiritsApprovalGoals,
					}
					req := httptest.NewRequest(http.MethodPost, "/api/spirits/approvals/"+id+"/"+action, strings.NewReader(body))
					req.SetPathValue("id", id)
					res := httptest.NewRecorder()
					handlers[action](res, req)
					if res.Code != wantStatus {
						t.Fatalf("status = %d, want %d; body: %s", res.Code, wantStatus, res.Body.String())
					}
					if scenario == "missing" && res.Body.String() != "approval not found\n" {
						t.Errorf("expected path-free not-found response, got %q", res.Body.String())
					}
					if after := store.Counts(); !reflect.DeepEqual(before, after) {
						t.Errorf("counts changed: before %v, after %v", before, after)
					}
				})
			}
		})
	}
}

func TestApprovalConfirmMissingApplyTargetRemainsBadRequest(t *testing.T) {
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	p, err := store.Propose(approvals.Proposal{
		Action: "Update cornerstone", ApplyPath: "spirits/test/cornerstone.md",
		Body: "````proposed\nUpdated content\n````",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{approvals: store}
	req := httptest.NewRequest(http.MethodPost, "/api/spirits/approvals/"+p.ID+"/confirm", strings.NewReader("{}"))
	req.SetPathValue("id", p.ID)
	res := httptest.NewRecorder()
	s.handleSpiritsApprovalConfirm(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "cannot read current") {
		t.Fatalf("missing apply target misclassified: %d %s", res.Code, res.Body.String())
	}
	if _, err := store.LoadPending(p.ID); err != nil {
		t.Fatalf("failed confirm must leave approval pending: %v", err)
	}
}

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
