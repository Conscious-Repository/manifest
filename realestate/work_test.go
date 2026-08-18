package realestate

import (
	"strings"
	"testing"
)

// The fixpoint is the contract that lets hand edits and app writes coexist:
// parse → emit must be byte-identical for app-shaped sections, including
// unknown [key:: value] fields and non-checkbox extra lines.
func TestWorkFixpoint(t *testing.T) {
	src := strings.Join([]string{
		"- [x] Demo [work:: demo]",
		"- [ ] Rough-in [work:: rough-in] [crew:: joes-guys]",
		"    - [x] Rough plumbing — 2 baths [work:: rough-in/rough-plumbing]",
		"    - [ ] Rough electrical",
		"    some hand-written context line",
		"- [ ] Finishes",
	}, "\n") + "\n"
	stages := ParseWork(strings.Split(strings.TrimRight(src, "\n"), "\n"))
	if out := EmitWork(stages); out != src {
		t.Fatalf("fixpoint broken:\n--- in ---\n%s--- out ---\n%s", src, out)
	}
	// derived semantics
	if len(stages) != 3 || stages[0].Current || !stages[1].Current {
		t.Fatalf("current must be the first UNCHECKED stage: %+v", stages)
	}
	if stages[1].Tasks[1].ID != "rough-in/rough-electrical" {
		t.Fatalf("derived todo id wrong: %q", stages[1].Tasks[1].ID)
	}
	if fieldValue(stages[1].Fields, "crew") != "joes-guys" {
		t.Fatal("unknown field lost")
	}
}

// A freshly seeded stage has no todos — Tasks must marshal as [] (never JSON
// null, which crashed every .forEach in the UI: blank WORK tab, "property not
// found" on pages with an empty work plan).
func TestWorkEmptyTasksNotNil(t *testing.T) {
	stages := ParseWork([]string{"- [ ] Demo", "- [ ] Rough-in"})
	for _, st := range stages {
		if st.Tasks == nil {
			t.Fatalf("stage %q Tasks is nil — marshals as null and breaks the client", st.Text)
		}
	}
}

func TestWorkReadyAndFreeze(t *testing.T) {
	stages := ParseWork([]string{
		"- [ ] Rough-in",
		"    - [x] plumbing",
		"    - [x] electrical",
	})
	if !stages[0].Ready || stages[0].Checked {
		t.Fatalf("all todos done → Ready (not Checked): %+v", stages[0])
	}
	// freeze writes the derived id as an explicit field exactly once
	if !FreezeWorkID(stages, "rough-in/plumbing") {
		t.Fatal("freeze should change an unfrozen todo")
	}
	out := EmitWork(stages)
	if !strings.Contains(out, "- [x] plumbing [work:: rough-in/plumbing]") {
		t.Fatalf("frozen id missing:\n%s", out)
	}
	re := ParseWork(strings.Split(strings.TrimRight(out, "\n"), "\n"))
	if FreezeWorkID(re, "rough-in/plumbing") {
		t.Fatal("second freeze must be a no-op")
	}
}

// The tether rollup, DRAW-AWARE (pass-5): an expense against a work item that
// carries an accepted bid is a DRAW — it moves paid, never committed (no
// double count; contracted-to-go hits 0 at full payment). committed per node =
// max(Σ accepted bids, Σ expenses); requested bids move nothing but chip.
func TestJoinWorkLedger(t *testing.T) {
	stages := ParseWork([]string{
		"- [ ] Rough-in [work:: rough-in]",
		"    - [ ] Rough plumbing [work:: rough-in/plumbing]",
	})
	ledger := []LedgerRow{
		{Type: "expense", Amount: 8400, Status: "paid", WorkID: "rough-in/plumbing"},
		{Type: "bid", Amount: 12150, Status: "accepted", Contractor: "ClearView", WorkID: "rough-in/plumbing"},
		{Type: "bid", Amount: 9999, Status: "requested", Contractor: "Other", WorkID: "rough-in/plumbing"},
		{Type: "expense", Amount: 500, Status: "paid", WorkID: "rough-in"}, // stage-level tether
		{Type: "expense", Amount: 77, Status: "paid"},                      // untethered — always legal
	}
	JoinWorkLedger(stages, ledger, nil)
	st := stages[0]
	if st.Paid != 8900 || st.Committed != 12150+500 {
		t.Fatalf("stage rollup: paid=%v committed=%v (want 8900 / 12650)", st.Paid, st.Committed)
	}
	td := st.Tasks[0]
	if td.Paid != 8400 || td.Committed != 12150 {
		t.Fatalf("todo rollup: paid=%v committed=%v (want the contract 12150)", td.Paid, td.Committed)
	}
	if len(td.Bids) != 2 || td.Bids[0].Who != "ClearView" {
		t.Fatalf("bid chips: %+v", td.Bids)
	}
	// overpayment: draws exceeding the contract raise committed to actual spend
	over := ParseWork([]string{"- [ ] S [work:: s]", "    - [ ] T [work:: s/t]"})
	JoinWorkLedger(over, []LedgerRow{
		{Type: "bid", Amount: 1000, Status: "accepted", WorkID: "s/t"},
		{Type: "expense", Amount: 1400, Status: "paid", WorkID: "s/t"},
	}, nil)
	if over[0].Tasks[0].Committed != 1400 {
		t.Fatalf("overpaid contract: committed=%v want 1400", over[0].Tasks[0].Committed)
	}
}

// Ledger note parsing: the [work:: id] token strips into WorkID for display and
// survives verbatim in RawNote for exact-match mutation.
func TestLedgerWorkToken(t *testing.T) {
	csv := "date,type,category,vendor,contractor,amount,status,note,doc\n" +
		"2026-07-20,bid,rough,,ClearView,12150,requested,windows quote [work:: rough-in/windows],\n"
	rows := parseLedger([]byte(csv))
	if len(rows) != 1 {
		t.Fatal("row lost")
	}
	r := rows[0]
	if r.WorkID != "rough-in/windows" || r.Note != "windows quote" {
		t.Fatalf("token not stripped: workId=%q note=%q", r.WorkID, r.Note)
	}
	if !strings.Contains(r.RawNote, "[work:: rough-in/windows]") {
		t.Fatalf("rawNote must keep the token: %q", r.RawNote)
	}
}
