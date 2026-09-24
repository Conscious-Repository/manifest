package fundraising

import (
	"sort"
	"strings"
)

// Touch is one dated interaction with a linked person and where the date came
// from. The people layer computes the automatic ones; the record holds only
// the manual dates. Nothing here is stored.
type Touch struct {
	Date      string `json:"date"`
	Kind      string `json:"kind"` // met | email | transcript | note | upcoming | manual
	Person    string `json:"person,omitempty"`
	PersonKey string `json:"personKey,omitempty"`
	Title     string `json:"title,omitempty"`
	Ref       string `json:"ref,omitempty"`
}

// touchRank matches the people layer's order on an equal date.
var touchRank = map[string]int{"met": 0, "upcoming": 0, "email": 1, "transcript": 2, "note": 3, "manual": 4}

func (a Touch) later(b Touch) bool {
	if b.Date == "" {
		return a.Date != ""
	}
	if a.Date != b.Date {
		return a.Date > b.Date
	}
	return touchRank[a.Kind] < touchRank[b.Kind]
}

func (a Touch) sooner(b Touch) bool {
	if b.Date == "" {
		return a.Date != ""
	}
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	return touchRank[a.Kind] < touchRank[b.Kind]
}

// MergeLast applies "latest wins" between the automatic last touch and the
// manual date: whichever is later shows, and on a tie the automatic one does.
func MergeLast(auto *Touch, manual string) *Touch {
	var best Touch
	if auto != nil && auto.Date != "" {
		best = *auto
	}
	if manual != "" {
		if m := (Touch{Date: manual, Kind: "manual"}); m.later(best) {
			best = m
		}
	}
	if best.Date == "" {
		return nil
	}
	return &best
}

// MergeNext chooses the next touch between the next calendar event and the
// manual date: the soonest one still ahead of today, else — with nothing
// ahead — the most recent overdue one so an unmet next step stays visible.
func MergeNext(auto *Touch, manual, today string) *Touch {
	var cands []Touch
	if auto != nil && auto.Date != "" {
		cands = append(cands, *auto)
	}
	if manual != "" {
		cands = append(cands, Touch{Date: manual, Kind: "manual"})
	}
	var ahead, behind Touch
	for _, c := range cands {
		if c.Date >= today {
			if c.sooner(ahead) {
				ahead = c
			}
		} else if c.later(behind) {
			behind = c
		}
	}
	switch {
	case ahead.Date != "":
		return &ahead
	case behind.Date != "":
		return &behind
	}
	return nil
}

// PickLast returns the latest of several people's automatic last touches.
func PickLast(touches []Touch) *Touch {
	var best Touch
	for _, t := range touches {
		if t.later(best) {
			best = t
		}
	}
	if best.Date == "" {
		return nil
	}
	return &best
}

// PickNext returns the soonest of several people's next calendar touches.
func PickNext(touches []Touch) *Touch {
	var best Touch
	for _, t := range touches {
		if t.sooner(best) {
			best = t
		}
	}
	if best.Date == "" {
		return nil
	}
	return &best
}

// PendingPerson is a name on the pipeline that is not yet a person note: a
// legacy registry row, or a name a collaborator typed into the Sheet. Each
// waits for the owner to link it to an existing contact or create the note.
type PendingPerson struct {
	Key           string       `json:"key"`
	Display       string       `json:"display"`
	Origin        string       `json:"origin"` // registry | sheet
	Emails        []string     `json:"emails,omitempty"`
	Opportunities []PendingRef `json:"opportunities"`
}

// PendingRef names one opportunity a pending person appears on.
type PendingRef struct {
	ID   string `json:"id"`
	Firm string `json:"firm"`
}

func sortPending(xs []PendingPerson) {
	sort.Slice(xs, func(i, j int) bool {
		if xs[i].Origin != xs[j].Origin {
			return xs[i].Origin == "sheet" // a collaborator's name waits on the owner; show it first
		}
		return strings.ToLower(xs[i].Display) < strings.ToLower(xs[j].Display)
	})
}
