package recruiting

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"manifest/record"
	"manifest/recruiting/sources"
)

// LOOK THEM UP — the deterministic profile pass (owner ask 2026-09-04).
//
// A draft arrives naming one person from one source, and that source knows one
// thing about them: a grant names its PI, a paper names an author, a repo names
// its owner. That is enough to decide "worth a look" and not enough to decide
// anything else. This asks the OTHER public sources what they hold under that
// exact name and merges what they say into the draft, so a record is worth
// keeping before it is kept.
//
// ⚠ DETERMINISTIC MEANS EXACT, NOT CLEVER. A hit counts only when its name
// matches the draft's under one flat normalization (case, punctuation and
// spacing folded — nothing else). No fuzzy score, no ranking, no model
// judgement: people share names, and a wrong merge writes a stranger's grants
// onto someone's record where they will read as fact forever. A near miss is
// dropped, not ranked — the cost of a miss is one empty result, the cost of a
// wrong merge is a corrupted citation.
//
// ⚠ IT ADDS, IT NEVER OVERWRITES. Links and citations are unioned; a profile
// field is filled ONLY where the draft left it empty. The source that found
// the person first keeps the last word on what it said.
//
// Nothing here writes a record. A lookup enriches the QUEUE; the accept that
// follows is the same deliberate one-record gesture it always was.

// lookupSources are the adapters worth asking about a person by name, in the
// order their answers are merged. A source is skipped when it is the one that
// produced the draft (it has already said what it knows) and when it is not
// registered on this box.
var lookupSources = []string{"openalex", "orcid", "github", "pubmed"}

// lookupMax bounds each source's answer. A name lookup wants the few rows that
// carry that exact name, not a survey.
const lookupMax = 8

// lookupTopicsMax caps the knowledge chips a draft accumulates across sources.
// Each source already caps its own; the union stays small enough to read.
const lookupTopicsMax = 10

// LookupResult reports what one pass actually found — per source, so a silent
// zero is legible as "they are not in these indexes" rather than "it broke".
type LookupResult struct {
	Name string `json:"name"`
	// Asked and Matched are source ids: everything consulted, and everything
	// that answered with this exact name.
	Asked   []string `json:"asked"`
	Matched []string `json:"matched"`
	// Failed are sources that errored — reported, never fatal: one index being
	// down must not cost the owner the others.
	Failed []string `json:"failed,omitempty"`
	Links  int      `json:"links"`
	Cites  int      `json:"cites"`
	// Filled names the profile fields this pass supplied, e.g. ["org"].
	Filled []string `json:"filled,omitempty"`
}

// Lookup enriches ONE draft in place from the other public sources.
func (r *RunStore) Lookup(ctx context.Context, runID, draftID string, now time.Time) (Run, LookupResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	run, err := r.load(runID)
	if err != nil {
		return Run{}, LookupResult{}, err
	}
	i, err := run.find(draftID)
	if err != nil {
		return Run{}, LookupResult{}, err
	}
	d := &run.Drafts[i]
	name := strings.TrimSpace(d.Draft.Name)
	if name == "" {
		return Run{}, LookupResult{}, errf("draft %s has no name to look up", draftID)
	}
	want := nameKey(name)
	if want == "" {
		return Run{}, LookupResult{}, errf("draft %s has no name to look up", draftID)
	}

	res := LookupResult{Name: name, Asked: []string{}, Matched: []string{}}
	haveLink := map[string]bool{}
	for _, l := range d.Draft.Links {
		haveLink[strings.TrimSpace(l)] = true
	}
	haveCite := map[string]bool{}
	for _, e := range d.Draft.Evidence {
		haveCite[strings.TrimSpace(e.URLOrFile)] = true
	}
	haveTopic := map[string]bool{}
	for _, t := range d.Draft.Topics {
		haveTopic[topicKey(t)] = true
	}

	for _, id := range lookupSources {
		adapter, ok := r.adapters[id]
		if !ok || id == d.Draft.SourceID {
			continue
		}
		res.Asked = append(res.Asked, id)
		hits, err := adapter.Search(ctx, sources.Scope{
			Role: run.Scope.Role, Query: name, Max: lookupMax,
		})
		if err != nil {
			res.Failed = append(res.Failed, id)
			continue
		}
		matched := false
		for _, h := range hits {
			if nameKey(h.Name) != want {
				continue // a different person who shares a search result
			}
			matched = true
			for _, l := range h.Links {
				if l = strings.TrimSpace(l); l != "" && !haveLink[l] {
					haveLink[l] = true
					d.Draft.Links = append(d.Draft.Links, l)
					res.Links++
				}
			}
			for _, e := range h.Evidence {
				u := strings.TrimSpace(e.URLOrFile)
				if u == "" || haveCite[u] {
					continue
				}
				haveCite[u] = true
				d.Draft.Evidence = append(d.Draft.Evidence, e)
				res.Cites++
			}
			// a hit that named its links outright keeps those names; the rest
			// are sorted by host before they are offered to the draft
			h = sources.ClassifyLinks(h)
			// fill only what the draft left blank
			for _, f := range []struct {
				key  string
				dst  *string
				from string
			}{
				{"title", &d.Draft.Title, h.Title},
				{"org", &d.Draft.Org, h.Org},
				{"location", &d.Draft.Location, h.Location},
				{"homepage", &d.Draft.Homepage, h.Homepage},
				{"linkedin", &d.Draft.LinkedIn, h.LinkedIn},
				{"github", &d.Draft.Github, h.Github},
				{"orcid", &d.Draft.Orcid, h.Orcid},
				{"site", &d.Draft.Site, h.Site},
			} {
				if strings.TrimSpace(*f.dst) == "" && strings.TrimSpace(f.from) != "" {
					*f.dst = strings.TrimSpace(f.from)
					res.Filled = append(res.Filled, f.key)
				}
			}
			// topics union: the hit's own chips (author-canonical by the
			// adapter's contract), added as said, deduped by controlled
			// normalization — a near-miss spelling is a second chip, not a
			// merge, because guessing two terms are one is how a vocabulary
			// quietly becomes wrong
			for _, t := range h.Topics {
				t = strings.TrimSpace(t)
				key := topicKey(t)
				if key == "" || haveTopic[key] || len(d.Draft.Topics) >= lookupTopicsMax {
					continue
				}
				haveTopic[key] = true
				d.Draft.Topics = append(d.Draft.Topics, t)
				res.Filled = append(res.Filled, "topics")
			}
		}
		if matched {
			res.Matched = append(res.Matched, id)
		}
	}
	// whatever the union of links now holds that no source labelled, label by
	// host — the same fallback Execute applies to a fresh queue
	before := d.Draft
	d.Draft = sources.ClassifyLinks(d.Draft)
	for _, f := range []struct{ key, was, now string }{
		{"homepage", before.Homepage, d.Draft.Homepage},
		{"linkedin", before.LinkedIn, d.Draft.LinkedIn},
		{"github", before.Github, d.Draft.Github},
		{"orcid", before.Orcid, d.Draft.Orcid},
		{"site", before.Site, d.Draft.Site},
	} {
		if f.was == "" && f.now != "" {
			res.Filled = append(res.Filled, f.key)
		}
	}
	sort.Strings(res.Filled)
	res.Filled = dedupeStrings(res.Filled)

	d.LookedUpAt = now.UTC()
	if err := r.writeRun(run, nil); err != nil {
		return Run{}, LookupResult{}, err
	}
	return r.project(run, nil), res, nil
}

// topicKey is the controlled topic normalizer (O1): the same rule that names
// a vault note, so "Diffusion MRI Reconstruction" and "diffusion-mri
// reconstruction" are ONE chip and "diffusion MRI" is another. Used for
// equality only — the chip itself is stored as the provider said it.
func topicKey(t string) string { return record.SlugSpaces(t, 0) }

// nameKey folds a person's name to what two indexes can agree on: lowercase,
// letters and digits only, single-spaced. Deliberately blunt — it is only ever
// used for EQUALITY, where being blunt costs a miss and being clever costs a
// wrong person.
func nameKey(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
