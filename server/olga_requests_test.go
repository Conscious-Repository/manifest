package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/vaultwriter"
)

// The owner asked (2026-10-10) for Olga's requests to arrive in Approvals
// "like other things for me to quickly address", instead of only in a vault
// file nothing showed him.
func TestOlgaRequestsArriveInApprovals(t *testing.T) {
	vault := t.TempDir()
	srv := &Server{}
	srv.UseVault(vaultwriter.New(vault))
	srv.UseApprovals(approvals.NewStore(t.TempDir()))
	path := filepath.Join(vault, "system", "olga", "requests.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	head := "# Requests for Benjamin\n\nWhat Olga asked Liber for that needs a change outside her own part of the app.\n\n"
	first := "- 2026-10-09 22:28 — Olga asked: \"Could would create a \\\"direct\\\" chat option?\"\n  What it would take: A server change.\n  Her request is ready to send.\n"
	second := "- 2026-10-10 08:00 — Olga asked: \"Make the rail purple\" (touches shared files)\n"
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cards := func() []approvalRow {
		var out []approvalRow
		for _, r := range srv.feedProposals() {
			if r.Type == approvals.TypeOlgaRequest {
				out = append(out, r)
			}
		}
		return out
	}

	write(head + first)
	got := cards()
	if len(got) != 1 {
		t.Fatalf("want 1 card, got %d", len(got))
	}
	c := got[0]
	if c.Action != `Olga asked: Could would create a "direct" chat option?` || c.Agent != "olga" || !c.Allowed || c.ApplyPath != "" {
		t.Fatalf("card = %+v", c)
	}
	if !strings.Contains(c.Body, "What it would take:\nA server change.\nHer request is ready to send.") {
		t.Fatalf("body lost the write-up:\n%s", c.Body)
	}
	if n := srv.feedInboxCount(time.Now()); n < 1 {
		t.Fatalf("badge = %d", n)
	}

	// Done records the decision; the card never comes back, even when the
	// file grows with a new request
	if err := srv.approvals.Confirm(c.ID); err != nil {
		t.Fatal(err)
	}
	write(head + first + second)
	got = cards()
	if len(got) != 1 || got[0].Action != "Olga asked: Make the rail purple" || !strings.Contains(got[0].Body, "touches shared files") {
		t.Fatalf("after Done + a new request: %+v", got)
	}
	if err := srv.approvals.Reject(got[0].ID, "not worth it"); err != nil {
		t.Fatal(err)
	}
	write(head + first + second + "\n")
	if got = cards(); len(got) != 0 {
		t.Fatalf("decided requests came back: %+v", got)
	}
}
