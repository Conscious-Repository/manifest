package sources

import (
	"context"
	"strings"
	"testing"
)

// The works fixture is the LIVE shape, pulled from api.openalex.org and
// trimmed to four authorships — including the one the registry gives no id
// and no ORCID, and the affiliation line that ends in an email address.

func TestOpenAlexWorkByDOIDraftsEveryAuthor(t *testing.T) {
	s := newOpenAlexServer(t, 200, openAlexFixture(t, "openalex-work.json"))
	got, err := s.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Max: 25,
		Fields: map[string]string{"work": "10.1038/s41586-020-2649-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("a draft per author on the paper: %d", len(got))
	}
	reqs := s.requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0].URL.Path, "/works/doi:10.1038/s41586-020-2649-2") {
		t.Fatalf("one bounded GET at the works endpoint: %+v", reqs[0].URL)
	}
	first := got[0]
	if first.Name != "Charles R. Harris" || first.Role != "role/mri-engineer" {
		t.Fatalf("first author: %+v", first)
	}
	if !strings.Contains(first.Note, "first author") {
		t.Fatalf("position belongs on the draft: %q", first.Note)
	}
	if len(first.Evidence) == 0 || first.Evidence[0].Kind != EvidencePublication {
		t.Fatalf("every author cites the paper: %+v", first.Evidence)
	}
	if !strings.Contains(first.Evidence[0].Snippet, "Array programming with NumPy") ||
		!strings.Contains(first.Evidence[0].Snippet, "Nature") ||
		!strings.Contains(first.Evidence[0].Snippet, "10.1038/s41586-020-2649-2") {
		t.Fatalf("the citation is the snippet: %q", first.Evidence[0].Snippet)
	}
	if first.Evidence[0].URLOrFile != "https://doi.org/10.1038/s41586-020-2649-2" {
		t.Fatalf("a paper cites at its DOI: %q", first.Evidence[0].URLOrFile)
	}

	// the second author has an ORCID and two institutions
	second := got[1]
	if second.Org != "Berkeley College" {
		t.Fatalf("structured institution only: %q", second.Org)
	}
	if !containsStr(second.Links, "https://orcid.org/0000-0002-5263-5070") {
		t.Fatalf("the ORCID rides as a link: %v", second.Links)
	}
}

// D15, on the path most likely to break it: the live fixture's affiliation
// line ends "…Berkeley, CA, USA. millman@berkeley.edu". Nothing anywhere on
// a draft may carry it.
func TestOpenAlexWorkNeverEmitsContactDetails(t *testing.T) {
	s := newOpenAlexServer(t, 200, openAlexFixture(t, "openalex-work.json"))
	got, err := s.adapter().Search(context.Background(), Scope{Max: 25,
		Fields: map[string]string{"work": "10.1038/s41586-020-2649-2"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		if len(d.Contact) != 0 {
			t.Fatalf("contact map: %+v", d.Contact)
		}
		hay := d.Name + " " + d.Org + " " + d.Title + " " + d.Note + " " + strings.Join(d.Links, " ")
		for _, e := range d.Evidence {
			hay += " " + e.Snippet + " " + e.URLOrFile
		}
		for _, edge := range d.Edges {
			hay += " " + edge.Basis
		}
		if strings.Contains(hay, "@berkeley.edu") || strings.Contains(hay, "millman@") {
			t.Fatalf("an email reached a draft: %q", hay)
		}
	}
}

// Coauthorship is the canon for `coauthor`: the registry's own author list.
func TestOpenAlexWorkEmitsCoauthorEdges(t *testing.T) {
	s := newOpenAlexServer(t, 200, openAlexFixture(t, "openalex-work.json"))
	got, _ := s.adapter().Search(context.Background(), Scope{Max: 25,
		Fields: map[string]string{"work": "10.1038/s41586-020-2649-2"}})

	// Keys are asymmetric, on purpose. A draft's OWN endpoint is filled at
	// accept time with the record id, so even the author the registry gives
	// neither an id nor an ORCID carries their coauthor edges — accepting
	// them IS what makes them nameable. What a keyless author cannot be is
	// the FAR endpoint of someone else's edge: nothing could point at them
	// again tomorrow.
	var withORCID, withoutKey CandidateDraft
	for _, d := range got {
		switch d.Name {
		case "K. Jarrod Millman":
			withORCID = d
		case "Charles R. Harris":
			withoutKey = d
		}
	}
	if len(withoutKey.Edges) == 0 {
		t.Fatal("accepting a keyless author still records who they wrote it with")
	}
	for _, e := range withoutKey.Edges {
		if !strings.HasPrefix(e.From, ExtNodePrefix) {
			t.Fatalf("the far endpoint must be a durable key: %q", e.From)
		}
	}
	for _, d := range got {
		for _, e := range d.Edges {
			if strings.Contains(e.From, "Charles R. Harris") || e.From == "" {
				t.Fatalf("a keyless author is never a far endpoint: %+v", e)
			}
		}
	}
	if len(withORCID.Edges) == 0 {
		t.Fatal("an author with an ORCID should carry coauthor edges")
	}
	for _, e := range withORCID.Edges {
		if e.Type != EdgeCoauthor {
			t.Fatalf("kind: %q", e.Type)
		}
		if !strings.HasPrefix(e.From, ExtNodePrefix) {
			t.Fatalf("an endpoint is a durable key, never a display name: %q", e.From)
		}
		if e.To != "" {
			t.Fatalf("the near endpoint is filled at accept time, not here: %q", e.To)
		}
		if e.Basis == "" || e.Inferred || e.Confidence <= 0 || e.Confidence >= OwnerConfidence {
			t.Fatalf("a stated coauthorship is basised, not inferred, and weaker than the owner's word: %+v", e)
		}
		if !strings.Contains(e.Basis, "Array programming with NumPy") {
			t.Fatalf("the basis names the paper: %q", e.Basis)
		}
	}
}

// The measured caution: a 20-author paper still yields drafts, and no edges.
func TestOpenAlexWorkCrowdedPaperClaimsNoRelationship(t *testing.T) {
	s := newOpenAlexServer(t, 200, openAlexFixture(t, "openalex-work-crowd.json"))
	got, err := s.adapter().Search(context.Background(), Scope{Max: 50,
		Fields: map[string]string{"work": "10.1234/consortium.2026"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("every author still lands as a draft: %d", len(got))
	}
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Fatalf("%s: a consortium byline is not a relationship: %+v", d.Name, d.Edges)
		}
	}
	if !strings.Contains(got[0].Evidence[0].Snippet, "no edges claimed") {
		t.Fatalf("the evidence must say why: %q", got[0].Evidence[0].Snippet)
	}
}

// Max bounds the works path the same way it bounds the search path.
func TestOpenAlexWorkRespectsMax(t *testing.T) {
	s := newOpenAlexServer(t, 200, openAlexFixture(t, "openalex-work-crowd.json"))
	got, _ := s.adapter().Search(context.Background(), Scope{Max: 3,
		Fields: map[string]string{"work": "W9000001"}})
	if len(got) != 3 {
		t.Fatalf("max caps the authors returned: %d", len(got))
	}
}

func TestOpenAlexWorkPath(t *testing.T) {
	cases := map[string]string{
		"10.1038/s41586-020-2649-2":                 "/works/doi:10.1038/s41586-020-2649-2",
		"https://doi.org/10.1038/s41586-020-2649-2": "/works/doi:10.1038/s41586-020-2649-2",
		"W3035965352":                                "/works/W3035965352",
		"https://openalex.org/W3035965352":           "/works/W3035965352",
		"https://api.openalex.org/works/W3035965352": "/works/W3035965352",
		"doi:10.1234/x.y (the one from the call)":    "/works/doi:10.1234/x.y",
	}
	for in, want := range cases {
		got, err := openAlexWorkPath(in)
		if err != nil || got != want {
			t.Errorf("%q → %q, %v (want %q)", in, got, err, want)
		}
	}
	if _, err := openAlexWorkPath("just some words"); err == nil {
		t.Error("prose is not a paper reference")
	}
}

func containsStr(hay []string, want string) bool {
	for _, s := range hay {
		if s == want {
			return true
		}
	}
	return false
}
