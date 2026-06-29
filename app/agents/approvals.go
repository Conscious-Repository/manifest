package agents

import (
	"os"
	"sort"
	"strings"
	"time"
)

// Approval is a proposed irreversible action (send email, create a real calendar
// event, spend money) awaiting a human yes. Agents may freely read the vault and
// WRITE proposals here, but nothing irreversible executes while a proposal sits
// in approvals/pending — the folder IS the gate. (v1 has no executor; approving
// only records the decision. Real execution is a later milestone.)
type Approval struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Agent     string    `json:"agent"`
	CreatedAt time.Time `json:"createdAt"`
	Status    string    `json:"status"` // pending | approved | rejected (= folder)
	Body      string    `json:"body"`
}

// Propose writes a proposal into approvals/pending via tmp+rename (atomic).
func (q *Queue) Propose(a Approval) (Approval, error) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.ID == "" {
		a.ID = newID(a.Action, a.Body, a.CreatedAt)
	}
	a.Status = "pending"
	tmp := q.dir("tmp", "approval-"+a.ID+".md")
	final := q.dir("approvals", "pending", a.ID+".md")
	if err := os.WriteFile(tmp, []byte(marshalApproval(a)), 0o644); err != nil {
		return Approval{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return Approval{}, err
	}
	q.log.Append("propose", a.ID, "action", a.Action, "agent", a.Agent)
	return a, nil
}

// Confirm moves a pending proposal to approvals/approved.
func (q *Queue) Confirm(id string) error {
	if err := os.Rename(q.dir("approvals", "pending", id+".md"), q.dir("approvals", "approved", id+".md")); err != nil {
		return err
	}
	q.log.Append("approve", id)
	return nil
}

// Reject moves a pending proposal to approvals/rejected.
func (q *Queue) Reject(id, reason string) error {
	src := q.dir("approvals", "pending", id+".md")
	if content, err := os.ReadFile(src); err == nil {
		_ = os.WriteFile(src, append(content, []byte("\n> rejected: "+reason+"\n")...), 0o644)
	}
	if err := os.Rename(src, q.dir("approvals", "rejected", id+".md")); err != nil {
		return err
	}
	q.log.Append("reject", id, "reason", reason)
	return nil
}

// Approvals lists proposals in the given status folder ("pending"/"approved"/"rejected").
func (q *Queue) Approvals(status string) []Approval {
	entries, _ := os.ReadDir(q.dir("approvals", status))
	var out []Approval
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		content, _ := os.ReadFile(q.dir("approvals", status, e.Name()))
		a := parseApproval(strings.TrimSuffix(e.Name(), ".md"), string(content))
		a.Status = status
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func marshalApproval(a Approval) string {
	var b strings.Builder
	b.WriteString("---\ntype: approval\n")
	b.WriteString("id: " + a.ID + "\n")
	b.WriteString("action: " + a.Action + "\n")
	b.WriteString("agent: " + a.Agent + "\n")
	b.WriteString("created: " + a.CreatedAt.UTC().Format(time.RFC3339) + "\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(a.Body, "\n"))
	b.WriteString("\n")
	return b.String()
}

func parseApproval(id, content string) Approval {
	a := Approval{ID: id}
	fm, body := splitFrontmatter(content)
	a.Body = strings.TrimRight(body, "\n")
	if v := fm["id"]; v != "" {
		a.ID = v
	}
	a.Action = fm["action"]
	a.Agent = fm["agent"]
	a.CreatedAt, _ = time.Parse(time.RFC3339, fm["created"])
	return a
}
