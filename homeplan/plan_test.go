package homeplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var homeTasks = map[string]TaskRef{
	"home/roof-on": {Text: "roof on"}, "home/flashing-into-house": {Text: "flashing into house"},
	"home/plan-windows": {Text: "plan windows"}, "home/metal-finish-and-coated": {Text: "Metal finish and coated"},
	"home/order-materials": {Text: "order materials"},
}

func known(id string) bool { _, ok := homeTasks[id]; return ok }

func fixture(t *testing.T) *Plan {
	t.Helper()
	raw, err := os.ReadFile("testdata/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(p, known, nil); err != nil {
		t.Fatal(err)
	}
	return p
}

// The calendar: eight shared weekends, 128 elapsed hours (never 256), the
// Nov 1 daylight-saving change costs or adds nothing, evenings never count
// as weekend capacity, and solo weekends stay outside the baseline.
func TestCalendarCapacity(t *testing.T) {
	d := Derive(fixture(t), homeTasks, "2026-10-07")
	var shared []string
	for _, w := range d.Weeks {
		if w.Status == "shared" {
			shared = append(shared, w.Saturday)
			if w.SharedHours != 16 {
				t.Errorf("%s: shared hours %v, want 16", w.Saturday, w.SharedHours)
			}
		}
	}
	want := "2026-10-17 2026-10-31 2026-11-07 2026-11-21 2026-11-28 2026-12-05 2026-12-12 2026-12-19"
	if got := strings.Join(shared, " "); got != want {
		t.Fatalf("shared weekends\n got %s\nwant %s", got, want)
	}
	c := d.Capacity
	if c.SharedWeekends != 8 || c.SharedHours != 128 || c.ReservedHours != 48 || c.PoolHours != 80 {
		t.Fatalf("capacity = %+v", c)
	}
	if c.SoloWeekendHrs != 32 { // Oct 10–11 and Nov 14–15, Olga alone: shown, not counted
		t.Fatalf("solo hours = %v", c.SoloWeekendHrs)
	}
	if c.ContingencyHrs != 16 {
		t.Fatalf("contingency = %v", c.ContingencyHrs)
	}
	byStatus := map[string]string{}
	for _, w := range d.Weeks {
		byStatus[w.Saturday] = w.Status
		if w.EveningHours != 6 {
			t.Errorf("%s: evening hours %v, want 6", w.Start, w.EveningHours)
		}
	}
	if byStatus["2026-10-24"] != "away" || byStatus["2026-10-10"] != "solo" || byStatus["2026-11-14"] != "solo" {
		t.Fatalf("travel weekends = %v", byStatus)
	}
	if d.Weeks[0].Start != "2026-10-05" || d.Weeks[len(d.Weeks)-1].Start != "2026-12-21" {
		t.Fatalf("weeks run %s … %s", d.Weeks[0].Start, d.Weeks[len(d.Weeks)-1].Start)
	}
	// the DST week: Saturday Oct 31 → Monday Nov 2 is exactly two days
	for i, w := range d.Weeks[1:] {
		a, _ := time.Parse("2006-01-02", d.Weeks[i].Start)
		b, _ := time.Parse("2006-01-02", w.Start)
		if b.Sub(a) != 7*24*time.Hour {
			t.Fatalf("weeks %s → %s are not 7 days apart", d.Weeks[i].Start, w.Start)
		}
	}
	// a weekend already behind us leaves the pool
	later := Derive(fixture(t), homeTasks, "2026-10-19")
	if later.Capacity.SharedHours != 112 || later.Capacity.PoolHours != 80 {
		t.Fatalf("after Oct 18: %+v", later.Capacity)
	}
}

// 28 known window hours; roof unknown stays unknown; the four roof
// allowances leave 28/16/4/−4 for other envelope work, negative kept.
func TestWindowHoursAndRoofScenarios(t *testing.T) {
	d := Derive(fixture(t), homeTasks, "2026-10-07")
	if d.Capacity.KnownDemand != 28 || d.Capacity.RemainingHours != 52 {
		t.Fatalf("baseline: demand %v remaining %v", d.Capacity.KnownDemand, d.Capacity.RemainingHours)
	}
	unknown := strings.Join(d.Capacity.UnknownItems, ",")
	if !strings.Contains(unknown, "home/roof-on") || !strings.Contains(unknown, "home/plan-windows#upper") || strings.Contains(unknown, "flashing") {
		t.Fatalf("unknown items = %s (flashing is inside the roof allowance)", unknown)
	}
	if d.Capacity.UnknownLow != 24 || d.Capacity.UnknownHigh != 56 {
		t.Fatalf("unknown range %v–%v", d.Capacity.UnknownLow, d.Capacity.UnknownHigh)
	}
	if len(d.Capacity.WaitUnknown) != 1 {
		t.Fatalf("supplier wait unknown = %v", d.Capacity.WaitUnknown)
	}
	for id, want := range map[string]float64{"roof-24": 28, "roof-36": 16, "roof-48": 4, "roof-56": -4} {
		s := d.Scenarios[id]
		if s.Error != "" || s.Capacity.RemainingHours != want {
			t.Errorf("%s: remaining %v (err %q), want %v", id, s.Capacity.RemainingHours, s.Error, want)
		}
	}
	if !hasConflict(d.Scenarios["roof-56"].Conflicts, "overdemand") {
		t.Fatal("−4 h must surface as a conflict, not be hidden")
	}
	if hasConflict(d.Conflicts, "overdemand") {
		t.Fatal("the saved baseline must not inherit a scenario's shortfall")
	}
	// person-hours are derived, never used as capacity
	for _, it := range d.Items {
		if it.Ref == "home/plan-windows#panes" && (it.PersonHrs == nil || *it.PersonHrs != 24) {
			t.Fatalf("panes person-hours = %v", it.PersonHrs)
		}
	}
}

func hasConflict(cs []Conflict, kind string) bool {
	for _, c := range cs {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

func TestAllocationConflicts(t *testing.T) {
	p := fixture(t)
	roof := p.Tasks["home/roof-on"]
	h := 36.0
	roof.Estimate.Hours = &h
	roof.Allocations = map[string]float64{"2026-10-24": 8, "2026-12-19": 8, "2026-10-31": 12}
	p.Tasks["home/roof-on"] = roof
	p.Milestones["frame"] = Milestone{Title: "Frame erected", Kind: "external", Date: "2026-11-02"}
	d := Derive(p, homeTasks, "2026-10-07")
	for _, kind := range []string{"unavailable", "overallocated", "order"} {
		if !hasConflict(d.Conflicts, kind) {
			t.Errorf("no %s conflict in %+v", kind, d.Conflicts)
		}
	}
	for _, w := range d.Weeks {
		if w.Saturday == "2026-12-19" && w.Free != -8 {
			t.Fatalf("contingency weekend free = %v, want -8 (buffer never silently consumed)", w.Free)
		}
	}
}

func TestValidateRefusals(t *testing.T) {
	cases := map[string]string{
		"cycle":       `{"tasks":{"home/roof-on":{"dependsOn":["home/plan-windows"]},"home/plan-windows":{"dependsOn":["home/roof-on"]}}}`,
		"unknown dep": `{"tasks":{"home/roof-on":{"dependsOn":["home/nope"]}}}`,
		"evening":     `{"tasks":{"home/plan-windows":{"subtasks":{"panes":{"draws":"evening"}}}}}`,
		"not sat":     `{"tasks":{"home/roof-on":{"allocations":{"2026-10-18":4}}}}`,
		"new task":    `{"tasks":{"home/invented":{"phase":"execution"}}}`,
		"typo":        `{"tasks":{"home/roof-on":{"estimat":{}}}}`,
		"negative":    `{"tasks":{"home/roof-on":{"estimate":{"hours":-1}}}}`,
		"link":        `{"tasks":{"home/roof-on":{"links":{"x":{"label":"x","href":"javascript:alert(1)"}}}}}`,
	}
	base, _ := os.ReadFile("testdata/plan.json")
	for name, patch := range cases {
		merged, err := MergePatch(base, []byte(patch))
		if err != nil {
			t.Fatal(err)
		}
		p, err := Decode(merged)
		if err == nil {
			err = Validate(p, known, []string{"home/invented"}[:boolInt(strings.Contains(patch, "invented"))])
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func newStore(t *testing.T) *Store {
	dir := t.TempDir()
	return &Store{Path: filepath.Join(dir, "home", "plan.json"), Write: func(p string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, b, 0o644)
	}}
}

// Seed once; re-running is a conflict, not a duplicate. A stale revision
// conflicts; every replaced revision is kept and can be restored.
func TestStoreRevisionsAndHistory(t *testing.T) {
	s := newStore(t)
	seed, _ := os.ReadFile("testdata/plan.json")
	now := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)
	_, rev, err := s.Apply("", seed, known, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Apply("", seed, known, now); !isConflict(err) {
		t.Fatalf("re-seed = %v, want conflict", err)
	}
	_, rev2, err := s.Apply(rev, []byte(`{"tasks":{"home/roof-on":{"estimate":{"hours":40}}}}`), known, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Apply(rev, []byte(`{"tasks":{"home/roof-on":{"estimate":{"hours":30}}}}`), known, now.Add(2*time.Second)); !isConflict(err) {
		t.Fatalf("stale write = %v, want conflict", err)
	}
	p, _, cur, _ := s.Read()
	if cur != rev2 || *p.Tasks["home/roof-on"].Estimate.Hours != 40 || p.Tasks["home/roof-on"].Estimate.Low == nil {
		t.Fatal("the merge must change one field and keep its siblings")
	}
	// null removes: back to unknown, not zero
	_, rev3, err := s.Apply(rev2, []byte(`{"tasks":{"home/roof-on":{"estimate":{"hours":null}}}}`), known, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	p, _, _, _ = s.Read()
	if p.Tasks["home/roof-on"].Estimate.Hours != nil {
		t.Fatal("null must leave the estimate unknown")
	}
	// a no-op write keeps the revision and adds no history
	if _, same, err := s.Apply(rev3, []byte(`{}`), known, now.Add(4*time.Second)); err != nil || same != rev3 {
		t.Fatalf("no-op = %v %v", same == rev3, err)
	}
	h := s.History()
	if len(h) != 2 {
		t.Fatalf("history = %v", h)
	}
	restored, err := s.Restore(rev3, h[len(h)-1], known, now.Add(5*time.Second))
	if err != nil || restored != rev {
		t.Fatalf("restore → %v (want the seed's revision) %v", restored, err)
	}
}

func isConflict(err error) bool {
	var c *ConflictError
	return errors.As(err, &c)
}

// Cross-process: separate OS processes race on one revision. Exactly one
// wins; the rest conflict; the file is always a whole, valid plan.
func TestCrossProcessWrites(t *testing.T) {
	if os.Getenv("HOMEPLAN_CHILD") != "" {
		s := &Store{Path: os.Getenv("HOMEPLAN_PATH"), Write: func(p string, b []byte) error {
			os.MkdirAll(filepath.Dir(p), 0o755)
			tmp := p + ".tmp" + os.Getenv("HOMEPLAN_CHILD")
			if err := os.WriteFile(tmp, b, 0o644); err != nil {
				return err
			}
			return os.Rename(tmp, p)
		}}
		h := os.Getenv("HOMEPLAN_CHILD")
		_, _, err := s.Apply(os.Getenv("HOMEPLAN_REV"), []byte(`{"tasks":{"home/roof-on":{"estimate":{"hours":`+h+`}}}}`), known, time.Now())
		switch {
		case err == nil:
			fmt.Print("WON")
		case isConflict(err):
			fmt.Print("CONFLICT")
		default:
			fmt.Print("ERR " + err.Error())
		}
		os.Exit(0)
	}
	s := newStore(t)
	seed, _ := os.ReadFile("testdata/plan.json")
	_, rev, err := s.Apply("", seed, known, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run", "^TestCrossProcessWrites$")
			cmd.Env = append(os.Environ(), "HOMEPLAN_CHILD="+fmt.Sprint(10+i), "HOMEPLAN_PATH="+s.Path, "HOMEPLAN_REV="+rev)
			out, _ := cmd.Output()
			results[i] = string(out)
		}(i)
	}
	wg.Wait()
	won := 0
	for _, r := range results {
		switch {
		case strings.HasPrefix(r, "WON"):
			won++
		case strings.HasPrefix(r, "CONFLICT"):
		default:
			t.Fatalf("child: %q", r)
		}
	}
	if won != 1 {
		t.Fatalf("%d processes won one revision: %v", won, results)
	}
	if _, _, _, err := s.Read(); err != nil {
		t.Fatalf("plan after the race: %v", err)
	}
	if n := len(s.History()); n != 1 {
		t.Fatalf("history after one winning write = %d", n)
	}
}

func TestScenarioPatchIsNotBaseline(t *testing.T) {
	p := fixture(t)
	b, _ := json.Marshal(p)
	d := Derive(p, homeTasks, "2026-10-07")
	b2, _ := json.Marshal(p)
	if string(b) != string(b2) {
		t.Fatal("deriving scenarios mutated the saved plan")
	}
	if d.Capacity.KnownDemand != 28 {
		t.Fatal("baseline demand moved")
	}
}

// The October 17 planting morning (recorded in the notes after the first
// handoff): 4 h out of that Saturday, 124 h together, the prep hold split
// 12 + 4 onto Oct 31, a 76 h pool and 24/12/0/−8 for the roof scenarios.
func TestEventTakesHoursOutOfADay(t *testing.T) {
	base, _ := os.ReadFile("testdata/plan.json")
	merged, err := MergePatch(base, []byte(`{
	  "events": {"planting": {"date": "2026-10-17", "hours": 4, "title": "Neighborhood planting"}},
	  "reservations": {"prep": {"hours": 12}, "prep-spill": {"weekend": "2026-10-31", "hours": 4, "purpose": "prep and coating (spill)", "kind": "work", "status": "provisional"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Decode(merged)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(p, known, nil); err != nil {
		t.Fatal(err)
	}
	d := Derive(p, homeTasks, "2026-10-07")
	if c := d.Capacity; c.SharedWeekends != 8 || c.SharedHours != 124 || c.ReservedHours != 48 || c.PoolHours != 76 || c.RemainingHours != 48 {
		t.Fatalf("capacity = %+v", c)
	}
	for id, want := range map[string]float64{"roof-24": 24, "roof-36": 12, "roof-48": 0, "roof-56": -8} {
		if got := d.Scenarios[id].Capacity.RemainingHours; got != want {
			t.Errorf("%s: %v, want %v", id, got, want)
		}
	}
	for _, w := range d.Weeks {
		if w.Saturday == "2026-10-17" && (w.Status != "shared" || w.SharedHours != 12 || w.Free != 0) {
			t.Fatalf("Oct 17 week = %+v", w)
		}
	}
}

// The assistant sequence: dependency order, around holds and saved work,
// never on a weekend too cold for an item, and never saved.
func TestAssistantSequence(t *testing.T) {
	base, _ := os.ReadFile("testdata/plan.json")
	merged, _ := MergePatch(base, []byte(`{
	  "climate": {"source": "test", "normals": {"2026-10-31": {"high": 63, "low": 43}, "2026-11-07": {"high": 59, "low": 41}, "2026-11-21": {"high": 53, "low": 35}, "2026-11-28": {"high": 50, "low": 33}, "2026-12-05": {"high": 48, "low": 31}, "2026-12-12": {"high": 45, "low": 29}, "2026-12-19": {"high": 44, "low": 28}}},
	  "milestones": {"frame": {"date": "2026-11-05"}},
	  "tasks": {
	    "home/roof-on": {"minTempF": 50, "estimate": {"hours": 36}},
	    "home/plan-windows": {"subtasks": {"frames": {"dependsOn": ["home/roof-on"]}, "panes": {"allocations": {"2026-10-31": 8}}}}
	  }}`))
	p, err := Decode(merged)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(p, known, nil); err != nil {
		t.Fatal(err)
	}
	d := Derive(p, homeTasks, "2026-10-07")
	s := d.Sequence
	got := map[string][]string{}
	for _, pl := range s.Placements {
		got[pl.Ref] = append(got[pl.Ref], fmt.Sprintf("%s:%v", pl.Weekend, pl.Hours))
	}
	// the roof waits for the frame (Nov 5) and needs ≥ 55°F normal highs: Nov 7 only (59°F)
	if strings.Join(got["home/roof-on"], " ") != "2026-11-07:16" {
		t.Fatalf("roof = %v", got["home/roof-on"])
	}
	// the rest of the panes go first (Oct 31 had 8 saved, 8 left of 16 free after the 8)
	if strings.Join(got["home/plan-windows#panes"], " ") != "2026-10-31:4" {
		t.Fatalf("panes = %v", got["home/plan-windows#panes"])
	}
	// frames wait on the roof, so start on its last weekend or later
	if len(got["home/plan-windows#frames"]) == 0 || got["home/plan-windows#frames"][0] < "2026-11-07" {
		t.Fatalf("frames = %v", got["home/plan-windows#frames"])
	}
	if s.Fits {
		t.Fatal("20 roof hours have no warm weekend left: it must not claim to fit")
	}
	var roofLeft bool
	for _, u := range s.Unplaced {
		roofLeft = roofLeft || (u.Ref == "home/roof-on" && strings.Contains(u.Reason, "20 h") && strings.Contains(u.Reason, "50°F"))
	}
	if !roofLeft {
		t.Fatalf("unplaced = %+v", s.Unplaced)
	}
	// a saved placement on a cold weekend is a conflict
	roof := p.Tasks["home/roof-on"]
	roof.Allocations = map[string]float64{"2026-12-19": 4}
	p.Tasks["home/roof-on"] = roof
	if !hasConflict(Derive(p, homeTasks, "2026-10-07").Conflicts, "cold") {
		t.Fatal("roof on Dec 19 (44°F) is not flagged cold")
	}
	if b, _ := Encode(p); strings.Contains(string(b), "placements") {
		t.Fatal("the sequence leaked into the saved document")
	}
}
