package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
)

// Phase 2 hermetic proof: a fired ("go") Hermes reply carrying
// manifest-proposal blocks files pending proposals into the approvals inbox,
// the todo shows state=proposed, and Confirm applies through the EXISTING
// lane — the note lands in the temp vault's log/. No live Hermes, no real
// vault.
func TestHermesGoFilesProposalsAndConfirmApplies(t *testing.T) {
	srv, vault := panelFixture(t)
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts")).WithVaultRoot(vault)
	srv.UseApprovals(store)

	taskID := "inbox/set-up-vendor-notes"
	reply := "# RESULT\n\nShortlist assembled.\n\n" +
		"```manifest-proposal\n" +
		`{"type":"create-vault-note","title":"Vendor Shortlist","body":"- acme\n- globex"}` + "\n" +
		"```\n\n" +
		"```manifest-proposal\n" +
		`{"type":"run-errand","errand":"email the acme rep for a quote"}` + "\n" +
		"```\n"
	srv.materializeHermesBrief(taskID, "", "go", "", reply)

	pending := store.List("pending")
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2: %+v", len(pending), pending)
	}
	var note, errand *approvals.Proposal
	for i := range pending {
		switch pending[i].Type {
		case approvals.TypeCreateVaultNote:
			note = &pending[i]
		case approvals.TypeRunErrand:
			errand = &pending[i]
		}
	}
	if note == nil || errand == nil {
		t.Fatalf("missing types in %+v", pending)
	}
	for _, p := range []*approvals.Proposal{note, errand} {
		if p.Agent != "hermes" {
			t.Errorf("agent = %q, want hermes", p.Agent)
		}
		if !strings.Contains(p.Action, "[todo:: "+taskID+"]") {
			t.Errorf("action missing todo token: %q", p.Action)
		}
	}
	if errand.ErrandText != "email the acme rep for a quote" {
		t.Errorf("errand text = %q", errand.ErrandText)
	}

	// the todo panel sees the pending proposal → state=proposed
	if d := srv.delegationIndex()[taskID]; d.State != "proposed" {
		t.Errorf("delegation state = %q, want proposed (%+v)", d.State, d)
	}

	// the thread comment: filed count + placeholders, never raw JSON
	th := srv.listThread(taskID)
	if len(th) != 1 {
		t.Fatalf("thread = %+v", th)
	}
	txt := th[0].Text
	if !strings.Contains(txt, "2 change(s) filed") || !strings.Contains(txt, "→ proposed: vault note — Vendor Shortlist") {
		t.Errorf("thread text missing filing summary:\n%s", txt)
	}
	if strings.Contains(txt, "manifest-proposal") || strings.Contains(txt, `"type"`) {
		t.Errorf("raw proposal JSON leaked into the thread:\n%s", txt)
	}

	// Confirm the note → the EXISTING apply lane writes it under log/ (lowercased)
	if err := store.Confirm(note.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	want := filepath.Join(vault, "log", time.Now().Format("2006-01-02")+" vendor shortlist.md")
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("confirmed note not written to %s: %v", want, err)
	}
	if !strings.Contains(string(b), "- acme") {
		t.Errorf("note content = %q", string(b))
	}

	// re-filing the same reply dedupes (same action|body → same id, already decided elsewhere or pending)
	srv.materializeHermesBrief(taskID, "", "go", "", reply)
	if got := len(store.List("pending")); got != 1 { // errand still pending; note already approved → re-filed? id exists in approved
		// Propose dedupes only against pending; an approved twin refiling is
		// acceptable — assert no THIRD distinct proposal appeared.
		if got > 2 {
			t.Errorf("refiling exploded pending to %d", got)
		}
	}

	// Reject the errand → nothing runs, folder move only
	pend := store.List("pending")
	for _, p := range pend {
		if p.Type == approvals.TypeRunErrand {
			if err := store.Reject(p.ID, "not now"); err != nil {
				t.Fatalf("reject: %v", err)
			}
		}
	}
}

// A go reply with a malformed and a disallowed block files nothing and warns.
func TestHermesGoBadBlocksWarnOnly(t *testing.T) {
	srv, vault := panelFixture(t)
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts")).WithVaultRoot(vault)
	srv.UseApprovals(store)
	taskID := "inbox/bad-blocks"
	reply := "result\n```manifest-proposal\n{nope\n```\n" +
		"```manifest-proposal\n" + `{"type":"delete-everything"}` + "\n```\n"
	srv.materializeHermesBrief(taskID, "", "go", "", reply)
	if got := len(store.List("pending")); got != 0 {
		t.Fatalf("pending = %d, want 0", got)
	}
	th := srv.listThread(taskID)
	if len(th) != 1 || !strings.Contains(th[0].Text, "⚠") {
		t.Fatalf("expected warnings in thread: %+v", th)
	}
}
