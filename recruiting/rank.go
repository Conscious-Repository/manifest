package recruiting

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/recruiting/sources"
)

// RANKING — sourcing-effectiveness plan Phase 4. A person a sweep named is
// ranked by what their matching works say, and the rank SHOWS its reasons:
// a human can explain the top result without opening anything opaque. No
// model, no stored score — this is derived on every read and never written
// to a run cache, a candidate, or the network.
//
// The default weights, in words (RankFormula is on every answer):
//
//	1 per matching work (a byline of more than ConsortiumAuthors counts ¼ —
//	  a name on a 400-author paper is not a signal of expertise)
//	+ ½ per work led (sole, first or last author)
//	+ up to 2 for recency (2 this year, falling to 0 at ten years old)
//	+ ¼ × ln(1 + citations), when the registry counted them
//	× ½ when the identity is ambiguous (an initials-only byline)
//
// The registry's own count of matching works (OpenAlex group_by) stands in
// for the works read when it is larger — the sweep read a sample, the
// registry counted them all.

const ConsortiumAuthors = 30

const RankFormula = "1 per matching work (¼ if over 30 authors) + ½ per work led + up to 2 for recency + ¼·ln(1+citations); ×½ if the identity is ambiguous"

// DraftRank is one person's derived rank.
type DraftRank struct {
	Score   float64  `json:"score"`
	Works   int      `json:"works"`
	Led     int      `json:"led"`
	Latest  int      `json:"latest,omitempty"`
	Reasons []string `json:"reasons"`
}

// RankDraft ranks one draft against the year the ranking is read in.
func RankDraft(d sources.CandidateDraft, year int) DraftRank {
	works, matching := sources.Authorships(d)
	return rankWorks(works, matching, draftAmbiguous(d), year)
}

func draftAmbiguous(d sources.CandidateDraft) bool {
	return strings.Contains(d.Note, "identity: ambiguous")
}

func rankWorks(works []sources.Authorship, matching int, ambiguous bool, year int) DraftRank {
	r := DraftRank{Works: len(works), Reasons: []string{}}
	if len(works) == 0 && matching == 0 {
		r.Reasons = append(r.Reasons, "no matching works read")
		return r
	}
	var score float64
	consortium, cited, citedKnown := 0, 0, false
	byPos := map[string]int{}
	for _, w := range works {
		if w.Authors > ConsortiumAuthors {
			consortium++
			score += 0.25
		} else {
			score++
		}
		if w.Position == "sole" || w.Position == "first" || w.Position == "last" {
			r.Led++
			byPos[w.Position]++
			score += 0.5
		}
		if w.Year > r.Latest {
			r.Latest = w.Year
		}
		if w.Cited > 0 {
			cited += w.Cited
			citedKnown = true
		}
	}
	if matching > len(works) {
		score += float64(matching - len(works))
		r.Works = matching
		r.Reasons = append(r.Reasons, strconv.Itoa(matching)+" matching works (registry count) · "+strconv.Itoa(len(works))+" read")
	} else {
		r.Reasons = append(r.Reasons, plural(len(works), "matching work", "matching works"))
	}
	if r.Latest > 0 {
		age := year - r.Latest
		if age < 0 {
			age = 0
		}
		score += math.Max(0, 2*(1-float64(age)/10))
		r.Reasons = append(r.Reasons, "latest "+strconv.Itoa(r.Latest))
	}
	if r.Led > 0 {
		var led []string
		for _, p := range []string{"last", "first", "sole"} {
			if n := byPos[p]; n > 0 {
				led = append(led, p+" author ×"+strconv.Itoa(n))
			}
		}
		r.Reasons = append(r.Reasons, strings.Join(led, " · "))
	}
	if citedKnown {
		score += 0.25 * math.Log1p(float64(cited))
		r.Reasons = append(r.Reasons, "cited "+strconv.Itoa(cited))
	}
	if consortium > 0 {
		r.Reasons = append(r.Reasons, plural(consortium, "consortium paper", "consortium papers")+" counted at ¼")
	}
	if ambiguous {
		score /= 2
		r.Reasons = append(r.Reasons, "identity ambiguous — ranked at half")
	}
	r.Score = math.Round(score*100) / 100
	return r
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// ---- the people across searches (sourcing-effectiveness Phase 5)

// MergedMember is one draft behind a merged person.
type MergedMember struct {
	Run    string `json:"run"`
	Draft  string `json:"draft"`
	Source string `json:"source"`
	Query  string `json:"query,omitempty"`
}

// MergedPerson is one human across every live sweep that named them — joined
// ONLY on a durable identifier (ORCID, OpenAlex id, GitHub, or the same
// source's own deterministic key). Two drafts that merely share a name stay
// two rows, and each says so (Namesake).
type MergedPerson struct {
	Key      string         `json:"key"`
	Name     string         `json:"name"`
	Org      string         `json:"org,omitempty"`
	Title    string         `json:"title,omitempty"`
	Members  []MergedMember `json:"members"`
	Sources  []string       `json:"sources"`
	Topics   []string       `json:"topics,omitempty"`
	Links    []string       `json:"links,omitempty"`
	Rank     DraftRank      `json:"rank"`
	Namesake bool           `json:"namesake,omitempty"`
}

// durableKeys are the identities two drafts may be merged on.
func durableKeys(d sources.CandidateDraft) []string {
	keys := extKeysOfDraft(d)
	if src, id := strings.TrimSpace(d.SourceID), strings.TrimSpace(d.ExternalID); src != "" && id != "" && !draftAmbiguous(d) {
		keys = append(keys, "src/"+src+"/"+id)
	}
	return keys
}

// MergedPeople ranks every undecided person across the live runs, one row
// per durable identity, strongest first.
func (r *RunStore) MergedPeople(now time.Time) []MergedPerson {
	r.mu.Lock()
	defer r.mu.Unlock()
	type item struct {
		m MergedMember
		d sources.CandidateDraft
	}
	var items []item
	for _, id := range r.ids() {
		run, err := r.load(id)
		if err != nil || (!run.ExpiresAt.IsZero() && now.After(run.ExpiresAt) && !run.Pinned) {
			continue
		}
		for _, d := range run.Drafts {
			if d.Status != DraftNew || strings.TrimSpace(d.Draft.Name) == "" {
				continue
			}
			items = append(items, item{MergedMember{Run: run.ID, Draft: d.ID, Source: d.Draft.SourceID, Query: run.Scope.Query}, d.Draft})
		}
	}

	// union-find over shared durable keys
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	owner := map[string]int{}
	for i, it := range items {
		for _, k := range durableKeys(it.d) {
			if j, ok := owner[k]; ok {
				parent[find(i)] = find(j)
			} else {
				owner[k] = i
			}
		}
	}
	groups := map[int][]int{}
	var roots []int
	for i := range items {
		root := find(i)
		if _, ok := groups[root]; !ok {
			roots = append(roots, root)
		}
		groups[root] = append(groups[root], i)
	}

	year := now.Year()
	var out []MergedPerson
	for _, root := range roots {
		idxs := groups[root]
		first := items[idxs[0]].d
		mp := MergedPerson{Name: strings.TrimSpace(first.Name), Org: first.Org, Title: first.Title}
		keys := durableKeys(first)
		if len(keys) > 0 {
			mp.Key = keys[0]
		} else {
			mp.Key = "draft/" + items[idxs[0]].m.Run + "/" + items[idxs[0]].m.Draft
		}
		var works []sources.Authorship
		seenWork := map[string]bool{}
		matching := 0
		ambiguous := true
		for _, i := range idxs {
			it := items[i]
			mp.Members = append(mp.Members, it.m)
			mp.Sources = appendNew(mp.Sources, it.d.SourceID)
			for _, t := range it.d.Topics {
				mp.Topics = appendNew(mp.Topics, t)
			}
			for _, l := range it.d.Links {
				mp.Links = appendNew(mp.Links, l)
			}
			if mp.Org == "" {
				mp.Org = it.d.Org
			}
			if mp.Title == "" {
				mp.Title = it.d.Title
			}
			ws, m := sources.Authorships(it.d)
			for _, w := range ws {
				if !seenWork[w.Ref] {
					seenWork[w.Ref] = true
					works = append(works, w)
				}
			}
			if m > matching {
				matching = m
			}
			if !draftAmbiguous(it.d) {
				ambiguous = false
			}
		}
		mp.Rank = rankWorks(works, matching, ambiguous, year)
		if len(mp.Sources) > 1 {
			mp.Rank.Reasons = append(mp.Rank.Reasons, "named by "+strings.Join(mp.Sources, " + "))
		}
		out = append(out, mp)
	}

	// a shared name without a shared identifier is said, never merged
	byName := map[string]int{}
	for _, p := range out {
		byName[sources.FoldName(p.Name)]++
	}
	for i := range out {
		if byName[sources.FoldName(out[i].Name)] > 1 {
			out[i].Namesake = true
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rank.Score != out[j].Rank.Score {
			return out[i].Rank.Score > out[j].Rank.Score
		}
		if a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); a != b {
			return a < b
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func appendNew(xs []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return xs
	}
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// KeepAlso marks another draft of an already-kept person as graphed onto
// their row — a merged person kept once is kept everywhere they were named.
func (r *RunStore) KeepAlso(runID, draftID, rowID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.load(runID)
	if err != nil {
		return err
	}
	i, err := run.find(draftID)
	if err != nil {
		return err
	}
	if run.Drafts[i].Status != DraftNew {
		return nil
	}
	run.decide(i, DraftGraphed, rowID, now)
	return r.writeRun(run, nil)
}
