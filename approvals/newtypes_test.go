package approvals

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/vaultwriter"
)

// craftPending builds the body (evidence + a ````proposed fence) and files a
// pending proposal, returning its id.
func craftPending(t *testing.T, s *Store, p Proposal, proposed string) string {
	t.Helper()
	p.Body = "evidence line\n\n````proposed\n" + proposed + "\n````"
	saved, err := s.Propose(p)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	return saved.ID
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	harness := t.TempDir() // the "excalibur root"
	agents := filepath.Join(harness, "artifacts")
	vault := t.TempDir() // the knowledge vault
	s := NewStore(agents).WithVaultRoot(vault).WithVaultWriter(vaultwriter.New(vault))
	return s, vault
}

// run-errand (errands-aside §4): the payload round-trips through frontmatter,
// and Confirm is a pure folder move — NO file is applied anywhere (the server
// dispatches the enqueue; the store only records the decision).
func TestRunErrandRoundTripAndConfirmWritesNothing(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	p, err := st.Propose(Proposal{
		Type: TypeRunErrand, Action: "run errand: cancel the X subscription",
		Agent: "ea-coordinator", Body: "why: standing cleanup",
		ErrandText:    "cancel the X subscription on the ooda account",
		ErrandAccount: "u0", ErrandGoal: "realestate/fund-i",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadPending(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ErrandText != "cancel the X subscription on the ooda account" ||
		got.ErrandAccount != "u0" || got.ErrandGoal != "realestate/fund-i" {
		t.Fatalf("round-trip lost errand fields: %+v", got)
	}
	if got.ApplyPath != "" {
		t.Fatal("run-errand must not carry an apply path")
	}
	before := treeFiles(t, dir)
	if err := st.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after := treeFiles(t, dir)
	// exactly one file moved pending/ → approved/; nothing else appeared
	if len(after) != len(before) {
		t.Fatalf("confirm changed file count: %v → %v", before, after)
	}
	if _, err := st.LoadPending(p.ID); err == nil {
		t.Fatal("confirm must remove the pending file (no double-enqueue)")
	}
}

func treeFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}
