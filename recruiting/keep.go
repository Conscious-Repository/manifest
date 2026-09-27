package recruiting

import (
	"strings"
	"time"

	"manifest/recruiting/sources"
)

// KEEP — the Network tab's front door for a swept person (owner decision
// 2026-09-27: "keep" = a system-side network record, never a vault note).
//
// A bridge person lives in their run's cache and expires with it (RunTTL);
// accepting them makes a CANDIDATE, which is the wrong promise for an advisor,
// an expert to consult or a connector. Keep is the third answer: one row in
// network/people.md carrying the source identity (so a re-sweep says "in your
// network", and edges that named them by ORCID/OpenAlex resolve onto the
// row), the owner's chosen kind, and NO consent — they are known, not someone
// the owner would ask to make an introduction, so they never start an intro
// path until he says so.
//
// D15 holds: the draft is sanitized first and no email or phone survives —
// a kept record's email is owner-typed, in the editor, only.

// KeepDraft writes one draft as a network row and files its edge claims onto
// it. Refuses a person already kept (by source identity or ORCID) instead of
// writing a second row for the same human.
func (s *Store) KeepDraft(d sources.CandidateDraft, kind string, now time.Time) (NetworkPerson, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d = SanitizeDraft(d)
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return NetworkPerson{}, errf("a kept person needs a name")
	}
	kind = NormalizePersonType(kind)
	if kind != "" && !ValidPersonType(kind) {
		return NetworkPerson{}, errf("kind must be one of %s", strings.Join(PersonTypes, ", "))
	}

	ref := SourceRef(d)
	orcid := ""
	p := NetworkPerson{
		Name: name, Type: kind, Org: strings.TrimSpace(d.Org), Title: strings.TrimSpace(d.Title),
		Source: strings.TrimSpace(d.SourceID), SourceRef: ref,
		Added: now.UTC().Format("2006-01-02"),
	}
	for _, l := range d.Links {
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case strings.Contains(l, "orcid.org/"):
			orcid = l
		case linkKey(l) == "github" && p.GitHub == "":
			p.GitHub = l
		case linkKey(l) == "linkedin" && p.LinkedIn == "":
			p.LinkedIn = l // a link the source published — never fetched (D12)
		}
	}
	p.ORCID = orcid

	doc := s.LoadNetworkPeople()
	for _, have := range doc.People() {
		if ref != "" && have.SourceRef == ref {
			return NetworkPerson{}, errf("already in your network: %s", have.Name)
		}
		if orcid != "" && have.ORCID == orcid {
			return NetworkPerson{}, errf("already in your network: %s", have.Name)
		}
	}
	added, err := doc.Add(p)
	if err != nil {
		return NetworkPerson{}, err
	}
	// the RECORD lands first (acceptDraft's rule): an edge naming a row that
	// failed to write would be an orphan
	if err := s.SaveNetworkPeople(doc); err != nil {
		return NetworkPerson{}, err
	}
	if err := s.saveDraftEdges(d, added.ID); err != nil {
		return added, err
	}
	return added, nil
}

// Keep turns one queued draft into a network row and marks it graphed — the
// status that already meant "became a network row". Only a `new` draft keeps;
// a duplicate is already somewhere, and a decided draft was decided.
func (r *RunStore) Keep(runID, draftID, kind string, now time.Time) (Run, NetworkPerson, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.load(runID)
	if err != nil {
		return Run{}, NetworkPerson{}, err
	}
	i, err := run.find(draftID)
	if err != nil {
		return Run{}, NetworkPerson{}, err
	}
	d := &run.Drafts[i]
	switch d.Status {
	case DraftNew:
	case DraftDuplicate:
		return Run{}, NetworkPerson{}, errf("%s is already on file as %s", d.Draft.Name, d.CandidateID)
	default:
		return Run{}, NetworkPerson{}, errf("draft %s is already %s", draftID, d.Status)
	}
	p, err := r.store.KeepDraft(d.Draft, kind, now)
	if err != nil {
		return Run{}, NetworkPerson{}, err
	}
	run.decide(i, DraftGraphed, p.ID, now)
	if err := r.writeRun(run, nil); err != nil {
		return Run{}, NetworkPerson{}, err
	}
	return r.project(run, nil), p, nil
}
