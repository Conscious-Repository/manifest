package server

import (
	"manifest/approvals"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestTaskApprovalsShareFeedRecordsAndGuards(t *testing.T) {
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	s := &Server{approvals: store}
	p, err := store.Propose(approvals.Proposal{Type: "approval", Agent: "alfred", Action: "Review draft [todo:: personal/electricians]", Body: "Inspect recipients and content before approving."})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Propose(approvals.Proposal{Type: "approval", Agent: "alfred", Action: "Other [todo:: personal/electricians-other]", Body: "Unrelated task"})
	if err != nil {
		t.Fatal(err)
	}
	rows := s.taskProposals("personal/electricians")
	if len(rows) != 1 || rows[0].ID != p.ID || rows[0].Body != p.Body {
		t.Fatalf("wrong task membership or missing evidence: %+v", rows)
	}
	if rows[0].Action != "Review draft" {
		t.Fatalf("internal token displayed: %s", rows[0].Action)
	}
	var feed approvalRow
	for _, row := range s.approvalRows(nil) {
		if row.ID == p.ID {
			feed = row
		}
	}
	if feed.ID != rows[0].ID || feed.Type != rows[0].Type || feed.Allowed != rows[0].Allowed || feed.Current != rows[0].Current {
		t.Fatal("task proposal diverged from Feed guards")
	}
	if err := store.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.taskProposals("personal/electricians")) != 0 {
		t.Fatal("decided proposal is still actionable in task")
	}
	if len(s.approvalRows(nil)) != 1 {
		t.Fatal("decision didn't settle shared Feed record")
	}
}

func TestTaskApprovalDecisionLeavesTraceWithoutAgentDispatch(t *testing.T) {
	s, _ := panelFixture(t)
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	s.UseApprovals(store)
	p, err := store.Propose(approvals.Proposal{Type: "approval", Agent: "alfred", Action: "Draft request [todo:: inbox/electricians]", Body: "A review-only proposal"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/spirits/approvals/"+p.ID+"/confirm", nil)
	req.SetPathValue("id", p.ID)
	res := httptest.NewRecorder()
	s.handleSpiritsApprovalConfirm(res, req)
	if res.Code != 200 {
		t.Fatalf("confirm: %d %s", res.Code, res.Body.String())
	}
	trail := s.listThread("inbox/electricians")
	if len(trail) != 1 || trail[0].Action != "approval" || trail[0].Text != "Approved: Draft request" {
		t.Fatalf("wrong trace: %+v", trail)
	}
	if len(s.listThread("inbox/other")) != 0 {
		t.Fatal("receipt leaked to another task")
	}
}
