package recruiting

import (
	"testing"

	"manifest/recruiting/sources"
)

// RUNG 1 — the identifier namespace IS the type. No fetch, no judgment, and
// nothing downstream may talk it out of the answer.
func TestCascadeRungOneIdentifiers(t *testing.T) {
	cases := []struct {
		paste   string
		kind    string
		class   string
		certain bool
	}{
		{"0000-0002-5263-5070", "orcid", SeedPerson, true},
		{"10.1038/s41586-020-2649-2", "doi", SeedWork, true},
		{"arXiv:2301.12345", "arxiv", SeedWork, true},
		{"arxiv:2301.12345v2", "arxiv", SeedWork, true},
		{"https://arxiv.org/abs/1706.03762", "arxiv", SeedWork, true},
		{"PMID: 32939066", "pubmed", SeedWork, true},
		{"pmid:32939066", "pubmed", SeedWork, true},
		// ROR says organisation and does NOT say which kind — a rung-1 fact
		// that still leaves one question open, and says so
		{"https://ror.org/01yc7t268", "ror", SeedLab, false},
	}
	for _, c := range cases {
		r := ResolveIntake(c.paste)
		if r.Kind != c.kind || r.Class != c.class {
			t.Errorf("%q → kind %q class %q, want %q/%q (%s)", c.paste, r.Kind, r.Class, c.kind, c.class, r.Why)
		}
		if r.Rung != RungIdentifier {
			t.Errorf("%q was decided by %q, not the identifier rung", c.paste, r.Rung)
		}
		if r.Certain() != c.certain {
			t.Errorf("%q certain=%v, want %v (%s)", c.paste, r.Certain(), c.certain, r.Why)
		}
	}
	// an arXiv id resolves through the DOI registry, so it is looked up the
	// same way every other paper is
	if r := ResolveIntake("arXiv:1706.03762"); r.DOI != "10.48550/arXiv.1706.03762" {
		t.Fatalf("arXiv DOI: %q", r.DOI)
	}
	// eight bare digits are eight digits, not a PMID
	if r := ResolveIntake("32939066"); r.Kind == "pubmed" {
		t.Fatal("a bare number must not be read as a PubMed id")
	}
}

// RUNG 2 — the host+path table. The two that were WRONG before this pass:
// every linkedin.com URL resolved to a person, so a company page landed on
// the board as a candidate; and github.com/<owner> claimed to know.
func TestCascadeRungTwoHostTable(t *testing.T) {
	person := ResolveIntake("https://www.linkedin.com/in/dana-reyes-1234")
	if person.Class != SeedPerson || person.Dest != DestCandidate {
		t.Fatalf("/in/ is a person: %+v", person)
	}
	company := ResolveIntake("https://www.linkedin.com/company/acme-bio")
	if company.Class != SeedCompany || company.Dest != DestSeed {
		t.Fatalf("/company/ is not a person: %+v", company)
	}
	school := ResolveIntake("https://www.linkedin.com/school/washu")
	if school.Class != SeedLab {
		t.Fatalf("/school/ is a lab: %+v", school)
	}
	for _, r := range []Resolution{person, company, school} {
		if !r.LinkOnly || len(r.Adapters) != 0 {
			t.Fatalf("D12: nothing reads LinkedIn: %+v", r)
		}
	}
	// the ambiguity rung 4 exists to settle
	acct := ResolveIntake("https://github.com/numpy")
	if acct.Kind != "github-user" || acct.Certain() {
		t.Fatalf("a bare GitHub account cannot be certain from its URL: %+v", acct)
	}
	if len(acct.Suggest) == 0 {
		t.Fatal("an uncertain answer must offer the alternatives")
	}
	// a repo, though, is unambiguous
	if repo := ResolveIntake("github.com/numpy/numpy"); !repo.Certain() || repo.Class != SeedRepo {
		t.Fatalf("a repo URL says repo: %+v", repo)
	}
	scholar := ResolveIntake("https://scholar.google.com/citations?user=abc123")
	if scholar.Class != SeedPerson || scholar.Handle != "abc123" || !scholar.LinkOnly {
		t.Fatalf("scholar: %+v", scholar)
	}
}

// RUNG 3 — the page's own JSON-LD, and THE CASE THE PLAN NAMES: a page whose
// og:type is `website` (which is true of most of the web and means nothing)
// but whose JSON-LD says Organization.
func TestCascadeRungThreePageTypes(t *testing.T) {
	site := ResolveIntake("https://yablonskiylab.wustl.edu/")
	if site.Certain() {
		t.Fatalf("a bare site cannot be typed from its URL: %+v", site)
	}

	// a research organisation, said plainly
	lab := RefineWithPage(site, sources.PageTypes{
		JSONLD: []string{"WebPage", "CollegeOrUniversity"}, OGType: "website"})
	if lab.Class != SeedLab || lab.Rung != RungPage {
		t.Fatalf("CollegeOrUniversity is a lab, from the page: %+v", lab)
	}
	if lab.Asked != "CollegeOrUniversity" {
		t.Fatalf("the answer must say what it read: %q", lab.Asked)
	}

	// bare Organization NARROWS without answering — og:type website must not
	// be allowed to break the tie in either direction
	org := RefineWithPage(site, sources.PageTypes{JSONLD: []string{"Organization"}, OGType: "website"})
	if org.Class != "" {
		t.Fatalf("Organization does not say lab or company: %+v", org)
	}
	if len(org.Suggest) != 2 || org.Suggest[0] != SeedLab || org.Suggest[1] != SeedCompany {
		t.Fatalf("it must offer exactly the two it narrowed to: %+v", org.Suggest)
	}
	if org.Rung != RungPage || org.Asked != "Organization" {
		t.Fatalf("the narrowing is still the page's doing: %+v", org)
	}

	// full schema.org URLs and @graph nesting are ordinary on real pages
	corp := RefineWithPage(site, sources.PageTypes{JSONLD: []string{"https://schema.org/Corporation"}})
	if corp.Class != SeedCompany {
		t.Fatalf("a namespaced @type is the same type: %+v", corp)
	}

	// a page that says nothing changes nothing
	same := RefineWithPage(site, sources.PageTypes{})
	if same.Class != site.Class || same.Rung != site.Rung {
		t.Fatalf("an empty probe must be inert: %+v", same)
	}
}

// RUNG 5 — og:type is a TIEBREAKER. It speaks only where JSON-LD said
// nothing, and `website` never speaks at all.
func TestCascadeRungFiveOpenGraphIsOnlyATiebreaker(t *testing.T) {
	site := ResolveIntake("https://someone.example.org/")
	if r := RefineWithPage(site, sources.PageTypes{OGType: "profile"}); r.Class != SeedPerson || r.Rung != RungOpenGraph {
		t.Fatalf("og:profile is a person: %+v", r)
	}
	if r := RefineWithPage(site, sources.PageTypes{OGType: "website"}); r.Class != "" {
		t.Fatalf("og:website means the page has a URL: %+v", r)
	}
	// and it never outranks the page's own JSON-LD
	r := RefineWithPage(site, sources.PageTypes{JSONLD: []string{"Blog"}, OGType: "profile"})
	if r.Class != SeedMedia || r.Rung != RungPage {
		t.Fatalf("JSON-LD outranks og:type: %+v", r)
	}
	// an og-decided class is still not certain — it is the weakest rung that
	// answers at all, so the chips stay up
	if og := RefineWithPage(site, sources.PageTypes{OGType: "profile"}); og.Certain() {
		t.Fatal("og:type may decide a default, never a certainty")
	}
}

// RUNG 4 — GitHub's own account type, and nothing else's.
func TestCascadeRungFourAccountType(t *testing.T) {
	acct := ResolveIntake("https://github.com/numpy")
	org := RefineWithAccount(acct, "Organization")
	if org.Class != SeedCompany || org.Dest != DestSeed {
		t.Fatalf("an org account is not a candidate: %+v", org)
	}
	if org.Rung != RungAccount || org.Asked != "Organization" {
		t.Fatalf("the answer must name its source: %+v", org)
	}
	if len(org.Suggest) != 1 || org.Suggest[0] != SeedLab {
		t.Fatalf("company leads, lab is one chip away: %+v", org.Suggest)
	}
	user := RefineWithAccount(ResolveIntake("https://github.com/torvalds"), "User")
	if user.Class != SeedPerson || user.Dest != DestCandidate || len(user.Suggest) != 0 {
		t.Fatalf("a user account is a person, with nothing left to ask: %+v", user)
	}
	if !user.Certain() {
		t.Fatal("GitHub answering is an answer")
	}
	// an account type means nothing about anything else
	doi := ResolveIntake("10.1038/s41586-020-2649-2")
	if got := RefineWithAccount(doi, "Organization"); got.Class != SeedWork {
		t.Fatalf("a DOI is not a GitHub account: %+v", got)
	}
}

// THE RULE that keeps the cascade from becoming a coin toss: a fetched rung
// may only speak where the pure rungs left a question open.
func TestCascadeNeverOverridesACertainAnswer(t *testing.T) {
	certain := []string{
		"10.1038/s41586-020-2649-2",
		"0000-0002-5263-5070",
		"github.com/numpy/numpy",
		"https://openalex.org/W3005144120",
		"arXiv:1706.03762",
	}
	// the most confusing page imaginable
	liar := sources.PageTypes{JSONLD: []string{"Person", "Corporation"}, OGType: "profile"}
	for _, paste := range certain {
		before := ResolveIntake(paste)
		if !before.Certain() {
			t.Fatalf("%q should be certain before the page speaks: %+v", paste, before)
		}
		after := RefineWithPage(before, liar)
		if after.Class != before.Class || after.Rung != before.Rung {
			t.Errorf("%q was decided by an identifier; the page must not move it: %+v", paste, after)
		}
	}
}

// Every class a rung can produce has to be one of the six the records know,
// and its destination has to follow from it — a class and a dest that
// disagree is how a company lands on the board.
func TestCascadeClassesAndDestinationsAgree(t *testing.T) {
	valid := map[string]bool{SeedPerson: true, SeedCompany: true, SeedLab: true,
		SeedWork: true, SeedRepo: true, SeedMedia: true}
	for typ, class := range schemaClass {
		if !valid[class] {
			t.Errorf("schema %q maps to %q, which is not a seed class", typ, class)
		}
		r := RefineWithPage(ResolveIntake("https://example.org/x"), sources.PageTypes{JSONLD: []string{typ}})
		if r.Class != class {
			t.Errorf("schema %q resolved to %q", typ, r.Class)
		}
		if r.Dest != DestForClass(class) {
			t.Errorf("schema %q: class %q went to %q", typ, class, r.Dest)
		}
	}
	for typ, alts := range schemaAmbiguous {
		for _, a := range alts {
			if !valid[a] {
				t.Errorf("ambiguous %q offers %q, which is not a seed class", typ, a)
			}
		}
	}
	for typ, class := range ogClass {
		if !valid[class] {
			t.Errorf("og %q maps to %q, which is not a seed class", typ, class)
		}
	}
}
