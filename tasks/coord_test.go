package tasks

import (
	"strings"
	"testing"
)

// coordination state (P1 Phase 1): [depends::] + [priority::] round-trip as
// fields, `blocked` derives from the dependencies' own state, and a file
// without either field is byte-untouched.

const coordSample = `# To Do

## Aion
- [ ] pick the MRI vendor [todo:: aion/pick-vendor] [added:: 2026-07-01] [priority:: high]
- [ ] sign the vendor contract [added:: 2026-07-02] [depends:: aion/pick-vendor]
- [ ] order the magnet [added:: 2026-07-03] [priority:: med] [depends:: aion/sign-the-vendor-contract, aion/pick-vendor]
- [ ] plan the lab move [added:: 2026-07-04] [depends:: re:abc123, aion/decide-lab-site]
- [x] site survey [added:: 2026-06-01] [done:: 2026-07-20] [priority:: low]
- [ ] after the survey [added:: 2026-07-05] [depends:: aion/site-survey]

### issues
- [ ] decide lab site [issue:: aion/decide-lab-site]
`

func TestCoordFixpoint(t *testing.T) {
	out := Serialize(Parse(coordSample))
	if out != coordSample {
		t.Fatalf("canonical coord sample changed:\n%s", out)
	}
	if Serialize(Parse(out)) != out {
		t.Fatal("coord sample not a fixpoint")
	}
	// the pre-existing samples carry neither field and must stay byte-identical
	for _, s := range []string{sample, sampleV2} {
		if Serialize(Parse(s)) != s {
			t.Fatal("a legacy sample changed once coordination fields exist")
		}
	}
}

func TestCoordParse(t *testing.T) {
	d := Parse(coordSample)
	dom := d.Domain("Aion")
	vendor, contract, magnet, move, survey, after := dom.Tasks[0], dom.Tasks[1], dom.Tasks[2], dom.Tasks[3], dom.Tasks[4], dom.Tasks[5]
	if vendor.Priority != PriorityHigh || vendor.PriorityN() != 3 || len(vendor.Depends) != 0 {
		t.Fatalf("vendor: %+v", vendor)
	}
	if got := strings.Join(magnet.Depends, "|"); got != "aion/sign-the-vendor-contract|aion/pick-vendor" || magnet.Priority != PriorityMed {
		t.Fatalf("magnet depends=%q priority=%q", got, magnet.Priority)
	}
	if survey.Priority != PriorityLow || survey.PriorityN() != 1 {
		t.Fatalf("survey: %+v", survey)
	}
	// depends never lands in the unknown-field passthrough
	for _, tk := range []*Task{contract, magnet, move, after} {
		if tk.FieldValue("depends") != "" || tk.FieldValue("priority") != "" {
			t.Fatalf("coordination key leaked into Fields: %+v", tk.Fields)
		}
	}
}

func TestDerivedBlocked(t *testing.T) {
	d := Parse(coordSample)
	dom := d.Domain("Aion")
	vendor, contract, magnet, move, after := dom.Tasks[0], dom.Tasks[1], dom.Tasks[2], dom.Tasks[3], dom.Tasks[5]
	res := d.Resolver()
	if vendor.Blocked(res) || d.StateOf(vendor) != "open" {
		t.Fatal("a task with no depends is never blocked")
	}
	if !contract.Blocked(res) || d.StateOf(contract) != "blocked" {
		t.Fatalf("contract should be blocked by the open vendor pick: %s", d.StateOf(contract))
	}
	if b, _ := magnet.BlockedBy(res); strings.Join(b, "|") != "aion/sign-the-vendor-contract|aion/pick-vendor" {
		t.Fatalf("magnet blockedBy = %v", b)
	}
	// a done dependency never blocks
	if after.Blocked(res) || d.StateOf(after) != "open" {
		t.Fatal("a done dependency must not block")
	}
	// an unknown id is tolerated + surfaced, an open ISSUE blocks (shared id space)
	b, un := move.BlockedBy(res)
	if strings.Join(b, "|") != "aion/decide-lab-site" || strings.Join(un, "|") != "re:abc123" {
		t.Fatalf("move blockedBy=%v unresolved=%v", b, un)
	}
	// "what depends on this" — open dependents only
	if got := strings.Join(d.Dependents("aion/pick-vendor"), "|"); got != "aion/sign-the-vendor-contract|aion/order-the-magnet" {
		t.Fatalf("dependents of vendor pick = %q", got)
	}
	if d.Dependents("aion/site-survey") == nil || d.Dependents("nobody") != nil {
		t.Fatal("dependents index")
	}
	// completing the blocker unblocks — derived, nothing stored
	vendor.Checked = true
	if contract.Blocked(d.Resolver()) {
		t.Fatal("blocked did not derive from the blocker's new state")
	}
	if !magnet.Blocked(d.Resolver()) { // still waits on the contract
		t.Fatal("magnet should still be blocked by the open contract")
	}
	// a blocked task that is done reads done, not blocked
	contract.Checked = true
	magnet.Checked = true
	if d.StateOf(magnet) != "done" {
		t.Fatalf("done wins: %s", d.StateOf(magnet))
	}
	// the view projection carries all of it
	vendor.Checked, contract.Checked, magnet.Checked = false, false, false
	v := d.View(now).Domains[0]
	if v.Tasks[1].State != "blocked" || v.Tasks[1].BlockedBy[0] != "aion/pick-vendor" ||
		v.Tasks[0].Priority != "high" || len(v.Tasks[0].Dependents) != 2 ||
		v.Tasks[3].Unresolved[0] != "re:abc123" || v.Tasks[5].State != "open" {
		t.Fatalf("view coord: %+v", v.Tasks)
	}
}

func TestPriorityClosedSet(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"", "", true}, {"high", "high", true}, {"HIGH ", "high", true}, {"med", "med", true},
		{"medium", "med", true}, {"low", "low", true},
		{"urgent", "", false}, {"1", "", false}, {"hi", "", false},
	} {
		got, ok := NormalizePriority(c.in)
		if got != c.want || ok != c.ok {
			t.Fatalf("NormalizePriority(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
	// an out-of-set hand edit is neither dropped nor guessed: it stays verbatim
	// as an unknown field, reads as no priority, and the line is a fixpoint
	raw := "# To Do\n\n## Inbox\n- [ ] odd one [added:: 2026-07-01] [priority:: urgent]\n"
	d := Parse(raw)
	tk := d.Domains[0].Tasks[0]
	if tk.Priority != "" || tk.FieldValue("priority") != "urgent" {
		t.Fatalf("out-of-set priority: %+v", tk)
	}
	if out := Serialize(d); out != raw {
		t.Fatalf("out-of-set priority not byte-stable:\n%s", out)
	}
	// "medium" normalizes ONCE (the documented hand-edit contract), then holds
	d2 := Parse("# To Do\n\n## Inbox\n- [ ] x [priority:: Medium]\n")
	out := Serialize(d2)
	if !strings.Contains(out, "[priority:: med]") || Serialize(Parse(out)) != out {
		t.Fatalf("normalize-once: %s", out)
	}
}

func TestSetDepends(t *testing.T) {
	d := Parse(sample)
	_, tk := d.Find("aion/yoshiro-side-letter")
	tk.SetDepends([]string{" real-estate/gutters-761 ", "", "aion/yoshiro-side-letter", "x/y", "real-estate/gutters-761"})
	if got := strings.Join(tk.Depends, "|"); got != "real-estate/gutters-761|x/y" {
		t.Fatalf("SetDepends normalize = %q (self + dupes + blanks must drop)", got)
	}
	tk.AddDepends("x/y")
	tk.AddDepends("z")
	tk.RemoveDepends("real-estate/gutters-761")
	if got := strings.Join(tk.Depends, "|"); got != "x/y|z" {
		t.Fatalf("add/remove = %q", got)
	}
	out := Serialize(d)
	if !strings.Contains(out, "[added:: 2026-07-01] [depends:: x/y, z]") {
		t.Fatalf("depends emitted at the tail:\n%s", out)
	}
	if Serialize(Parse(out)) != out {
		t.Fatal("depends not a fixpoint")
	}
	tk.RemoveDepends("x/y")
	tk.RemoveDepends("z")
	if strings.Contains(Serialize(d), "[depends::") {
		t.Fatal("an empty list must emit nothing")
	}
	// rank and priority stay separate fields — setting one never touches the other
	tk.Rank, tk.Priority = "4", PriorityHigh
	if !strings.Contains(Serialize(d), "[rank:: 4] [priority:: high]") {
		t.Fatalf("rank/priority: %s", Serialize(d))
	}
}
