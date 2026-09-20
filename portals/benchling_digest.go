package portals

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ---- Benchling → one card per day, one line per object (2026-09-20) ----
//
// Benchling stamps every save as a new modifiedAt, so a notebook entry edited
// eighteen times in an afternoon used to be eighteen cards. A day now
// collapses to one card; inside it each object is one line that says how
// many saves it took, who made them, what kind of object it is and whether it
// is new; lines group by the project folder the object lives in (read from
// its web URL) and the busiest project leads. A dismissed day resurfaces only
// with the changes that landed after the dismissal.

// benchProjectRe pulls the project folder slug out of a Benchling web URL:
// https://<tenant>.benchling.com/<tenant>/f/lib_<id>-<slug>/…
var benchProjectRe = regexp.MustCompile(`/f/lib_[A-Za-z0-9]+-([^/]+)/`)

// benchProject names the project a URL points into, humanised ("w-008
// magnetoacoustics"); "" when the URL carries no folder.
func benchProject(url string) string {
	m := benchProjectRe.FindStringSubmatch(url)
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(m[1], "-", " ")
}

// benchObjectKey strips the modifiedAt suffix off an event id so every save
// of one object folds together: "benchling:entry:etr_x:1789855886" → "benchling:entry:etr_x".
func benchObjectKey(id string) string {
	parts := strings.Split(id, ":")
	if len(parts) >= 3 {
		return strings.Join(parts[:3], ":")
	}
	return id
}

type benchLineAgg struct {
	line   DigestLine
	actors map[string]bool
	order  []string
	latest time.Time
	isNew  bool
	proj   string
}

// buildBenchlingDigest is the pure function of a day's Benchling events after
// `floor` (the last dismissal of that day's card, or zero): the card id, the
// per-project groups, the one-line summary for the card's detail slot and the
// latest change time. No groups → nothing to show.
func buildBenchlingDigest(events []Event, day string, loc *time.Location, floor time.Time) (id string, groups []DigestGroup, summary string, at time.Time) {
	id = "benchling-digest:" + day
	aggs := map[string]*benchLineAgg{}
	var keys []string
	for _, e := range events {
		if e.Portal != "benchling" || e.At.In(loc).Format("2006-01-02") != day || !e.At.After(floor) {
			continue
		}
		if e.At.After(at) {
			at = e.At
		}
		k := benchObjectKey(e.ID)
		a, ok := aggs[k]
		if !ok {
			a = &benchLineAgg{line: DigestLine{Text: e.Title, Detail: e.Detail, ForMe: e.ForMe}, actors: map[string]bool{}, proj: benchProject(e.URL)}
			aggs[k] = a
			keys = append(keys, k)
		}
		a.line.Count++
		if e.Actor != "" && !a.actors[e.Actor] {
			a.actors[e.Actor] = true
			a.order = append(a.order, e.Actor)
		}
		if e.Change == "new" {
			a.isNew = true
		}
		if !e.At.Before(a.latest) {
			a.latest = e.At
			a.line.URL = e.URL
			if e.Title != "" {
				a.line.Text = e.Title
			}
		}
		a.line.ForMe = a.line.ForMe || e.ForMe
	}
	if len(keys) == 0 {
		return id, nil, "", at
	}
	byProj := map[string][]DigestLine{}
	var projs []string
	people := map[string]bool{}
	var peopleOrder []string
	changes := 0
	for _, k := range keys {
		a := aggs[k]
		a.line.Who = strings.Join(a.order, ", ")
		a.line.Change = "edited"
		if a.isNew {
			a.line.Change = "new"
		}
		a.line.At = a.latest.UTC().Format(time.RFC3339)
		changes += a.line.Count
		for _, who := range a.order {
			if !people[who] {
				people[who] = true
				peopleOrder = append(peopleOrder, who)
			}
		}
		p := a.proj
		if p == "" {
			p = "—"
		}
		if _, ok := byProj[p]; !ok {
			projs = append(projs, p)
		}
		byProj[p] = append(byProj[p], a.line)
	}
	// busiest project first, then by name; within a project the latest change first
	sort.SliceStable(projs, func(i, j int) bool {
		ni, nj := len(byProj[projs[i]]), len(byProj[projs[j]])
		if ni != nj {
			return ni > nj
		}
		return projs[i] < projs[j]
	})
	for _, p := range projs {
		lines := byProj[p]
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].At > lines[j].At })
		groups = append(groups, DigestGroup{List: p, Lines: lines})
	}
	noun := "items"
	if len(keys) == 1 {
		noun = "item"
	}
	summary = fmt.Sprintf("%d %s · %d %s", changes, plural(changes, "change"), len(keys), noun)
	if len(peopleOrder) > 0 {
		summary += " · " + strings.Join(peopleOrder, ", ")
	}
	return id, groups, summary, at
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
