package homeplan

import (
	"fmt"
	"sort"
	"strings"
)

// Sequence is the assistant's what-if placement: open weekend work laid on
// the shared weekends in dependency order, around reservations and the
// saved placements, skipping weekends too cold for an item's materials.
// It is derived on every read and never written to the plan.
type Sequence struct {
	Placements  []SeqPlacement `json:"placements"`
	Unplaced    []SeqUnplaced  `json:"unplaced"`    // hours that found no weekend
	Unestimated []string       `json:"unestimated"` // open weekend work with no hours yet
	Assumptions []string       `json:"assumptions"`
	Finish      string         `json:"finish,omitempty"` // last Saturday used
	Fits        bool           `json:"fits"`             // every estimated hour placed by the deadline
	SpareHours  float64        `json:"spareHours"`       // shared hours still free after it
}

type SeqPlacement struct {
	Ref     string  `json:"ref"`
	Weekend string  `json:"weekend"`
	Hours   float64 `json:"hours"`
	Basis   string  `json:"basis"` // estimate | allowance-high
}

type SeqUnplaced struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

func sequence(p *Plan, d *Derived, itemOf map[string]Item) *Sequence {
	seq := &Sequence{Placements: []SeqPlacement{}, Unplaced: []SeqUnplaced{}, Unestimated: []string{}, Assumptions: []string{}}
	deadline := d.Capacity.HardDeadline
	// usable weekends and what is free on each
	var weeks []int
	free := map[int]float64{}
	for i, w := range d.Weeks {
		if w.Past || w.Status != "shared" || (deadline != "" && w.Saturday > deadline) {
			continue
		}
		weeks = append(weeks, i)
		free[i] = max(w.Free, 0)
	}
	pos := map[string]int{} // Saturday → index into d.Weeks
	for i, w := range d.Weeks {
		pos[w.Saturday] = i
	}
	view := map[string]ItemView{}
	for _, iv := range d.Items {
		view[iv.Ref] = iv
	}
	// candidates: open weekend work with hours we can place
	type cand struct {
		ref   string
		hours float64
		basis string
	}
	var cands []cand
	isCand := map[string]bool{}
	for _, iv := range d.Items {
		if iv.Done || iv.IncludedIn != "" || iv.Draws != "pool" {
			continue
		}
		h, basis := 0.0, "estimate"
		switch {
		case iv.Hours != nil:
			h = *iv.Hours
		case iv.High != nil:
			h, basis = *iv.High, "allowance-high"
		default:
			seq.Unestimated = append(seq.Unestimated, iv.Ref)
			continue
		}
		h -= iv.Allocated // already placed in the saved plan
		if h <= 0 {
			continue
		}
		cands = append(cands, cand{iv.Ref, h, basis})
		isCand[iv.Ref] = true
	}
	// topological order; ties by the timeline's own order
	rank := map[string]int{}
	for i, iv := range d.Items {
		rank[iv.Ref] = i
	}
	deps := func(ref string) []string { return view[ref].DependsOn }
	var order []cand
	done := map[string]bool{}
	for len(order) < len(cands) {
		var ready []cand
		for _, c := range cands {
			if done[c.ref] {
				continue
			}
			ok := true
			for _, dep := range deps(c.ref) {
				if isCand[dep] && !done[dep] {
					ok = false
				}
			}
			if ok {
				ready = append(ready, c)
			}
		}
		if len(ready) == 0 {
			break // a cycle cannot be saved; defensive
		}
		sort.Slice(ready, func(i, j int) bool { return rank[ready[i].ref] < rank[ready[j].ref] })
		order = append(order, ready[0])
		done[ready[0].ref] = true
	}
	lastAt := map[string]int{} // ref → index of its last weekend (sequence or saved)
	for _, iv := range d.Items {
		if iv.Last != "" {
			lastAt[iv.Ref] = pos[iv.Last]
		}
	}
	assumed := map[string]bool{}
	assume := func(s string) {
		if !assumed[s] {
			assumed[s] = true
			seq.Assumptions = append(seq.Assumptions, s)
		}
	}
	for _, c := range order {
		start := 0
		for _, dep := range deps(c.ref) {
			if id, ok := strings.CutPrefix(dep, "milestone:"); ok {
				m := p.Milestones[id]
				if m.Date == "" {
					assume("assumes “" + m.Title + "” is done in time (no date yet)")
					continue
				}
				for i, w := range d.Weeks {
					if w.Saturday >= m.Date || addDays(w.Saturday, 1) >= m.Date {
						start = max(start, i)
						break
					}
				}
				continue
			}
			if view[dep].Done {
				continue
			}
			if at, ok := lastAt[dep]; ok {
				start = max(start, at)
			} else if !isCand[dep] {
				how := map[string]string{"evening": "evening work", "outside": "outside crew", "none": "no household time"}[view[dep].Draws]
				if strings.HasPrefix(view[dep].Draws, "reservation:") {
					how = "inside its reservation"
				} else if how == "" {
					how = "no weekend hours"
				}
				assume("assumes “" + view[dep].Title + "” is ready in time (" + how + ")")
			}
		}
		it := itemOf[c.ref]
		left := c.hours
		for _, i := range weeks {
			if i < start || left <= 0 {
				continue
			}
			if tooCold(it.MinTempF, &d.Weeks[i]) || free[i] <= 0 {
				continue
			}
			h := min(free[i], left)
			free[i] -= h
			left -= h
			seq.Placements = append(seq.Placements, SeqPlacement{c.ref, d.Weeks[i].Saturday, h, c.basis})
			lastAt[c.ref] = i
			if d.Weeks[i].Saturday > seq.Finish {
				seq.Finish = d.Weeks[i].Saturday
			}
		}
		if left > 0 {
			why := fmt.Sprintf("%.0f h do not fit before the deadline", left)
			if it.MinTempF != nil {
				why += fmt.Sprintf(" (needs ≥ %.0f°F)", *it.MinTempF)
			}
			seq.Unplaced = append(seq.Unplaced, SeqUnplaced{c.ref, why})
		}
	}
	for _, i := range weeks {
		seq.SpareHours += free[i]
	}
	seq.Fits = len(seq.Unplaced) == 0
	sort.SliceStable(seq.Unplaced, func(i, j int) bool { return rank[seq.Unplaced[i].Ref] < rank[seq.Unplaced[j].Ref] })
	return seq
}

func addDays(s string, n int) string {
	t, ok := parseDate(s)
	if !ok {
		return s
	}
	return day(t.AddDate(0, 0, n))
}
