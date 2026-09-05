package sources

import "testing"

// ⚠ A LINK IS SORTED BY HOST, NEVER BY HOPE. The four named classes match
// exact hosts; a bare personal domain is a homepage only when nothing says
// otherwise; every institutional or platform page is `site`; and anything
// that is not an http(s) page is nothing at all (D15 keeps mailto: out).
func TestClassifyLinkSortsByHostAndNeverGuesses(t *testing.T) {
	cases := []struct {
		raw  string
		kind LinkKind
	}{
		{"https://www.linkedin.com/in/dana-reyes", LinkLinkedIn},
		{"https://linkedin.com/in/dana-reyes/", LinkLinkedIn},
		{"https://uk.linkedin.com/in/dana-reyes", LinkLinkedIn},
		{"https://github.com/dreyes", LinkGitHub},
		{"https://github.com/dreyes/", LinkGitHub},
		{"https://github.com/dreyes?tab=repositories", LinkGitHub},
		{"https://orcid.org/0000-0002-1825-0097", LinkORCID},

		// the same hosts, but not a person's profile: a company page, a job
		// post, an org's repo — real pages, `site`, never the contact strip
		{"https://www.linkedin.com/company/acme-labs", LinkSite},
		{"https://www.linkedin.com/school/mit", LinkSite},
		{"https://www.linkedin.com/jobs/view/123", LinkSite},
		{"https://www.linkedin.com/posts/dana-reyes_mri-activity-1", LinkSite},
		{"https://www.linkedin.com/", LinkSite},
		{"https://www.linkedin.com/in/", LinkSite},
		{"https://www.linkedin.com/pub/dana-reyes/1/2/3", LinkLinkedIn},
		{"https://github.com/acme/tool", LinkSite},
		{"https://github.com/acme/tool/issues/4", LinkSite},
		{"https://github.com/", LinkSite},
		{"https://ORCID.org/0000-0002-1825-0097", LinkORCID},

		// a bare personal domain, shallow path → homepage
		{"https://danareyes.com", LinkHomepage},
		{"https://www.danareyes.com/", LinkHomepage},
		{"http://dana-reyes.org/about", LinkHomepage},
		{"https://dana.reyes.io", LinkHomepage},

		// institutional pages are the institution's, however personal the path
		{"https://web.mit.edu/~dreyes/", LinkSite},
		{"https://people.csail.mit.edu/dreyes", LinkSite},
		{"https://www.cs.ox.ac.uk/people/dana.reyes", LinkSite},
		{"https://www.uni-heidelberg.de/dreyes", LinkSite},
		{"https://reporter.nih.gov/project-details/11237107", LinkSite},

		// platforms host pages, they are not homepages
		{"https://dreyes.github.io", LinkSite},
		{"https://openalex.org/A5023888391", LinkSite},
		{"https://scholar.google.com/citations?user=abc", LinkSite},
		{"https://medium.com/@dreyes", LinkSite},
		{"https://dreyes.substack.com", LinkSite},
		{"https://twitter.com/dreyes", LinkSite},
		{"https://sites.google.com/view/dreyes", LinkSite},

		// a deep path on a private domain is a page on a site, not the site
		{"https://danareyes.com/lab/people/dana", LinkSite},
		{"https://danareyes.com/~dana", LinkSite},
		{"https://a.b.c.danareyes.com", LinkSite},

		// not pages: nothing is promoted from these
		{"mailto:dana@example.com", ""},
		{"tel:+1-555-0100", ""},
		{"dana reyes", ""},
		{"", ""},
		{"ftp://danareyes.com/cv.pdf", ""},
	}
	for _, c := range cases {
		kind, u := ClassifyLink(c.raw)
		if kind != c.kind {
			t.Errorf("%q: kind=%q want %q", c.raw, kind, c.kind)
		}
		if c.raw != "" && u != c.raw {
			t.Errorf("%q: url was rewritten to %q — a citation is kept verbatim", c.raw, u)
		}
	}
}

// ClassifyLinks fills only blank fields, in Links order, and Site skips a
// platform's page: an index URL the draft already cites is not a contact.
func TestClassifyLinksFillsBlankFieldsOnlyAndSkipsPlatformsForSite(t *testing.T) {
	d := CandidateDraft{
		Orcid: "https://orcid.org/0000-0001-0000-0001", // set by the source: kept
		Links: []string{
			"https://openalex.org/A1",                   // platform → no Site
			"https://orcid.org/0000-0002-9999-9999",     // second orcid: first wins
			"https://github.com/dreyes",                 // github
			"https://github.com/other",                  // second github: first wins
			"https://www.linkedin.com/in/dana",          // linkedin
			"https://web.mit.edu/~dreyes/",              // site (institutional)
			"https://danareyes.com",                     // homepage
			"mailto:dana@example.com",                   // nothing
			"https://people.csail.mit.edu/dreyes/other", // second site: first wins
		},
	}
	got := ClassifyLinks(d)
	if got.Orcid != "https://orcid.org/0000-0001-0000-0001" {
		t.Errorf("a set field was overwritten: %q", got.Orcid)
	}
	if got.Github != "https://github.com/dreyes" || got.LinkedIn != "https://www.linkedin.com/in/dana" ||
		got.Homepage != "https://danareyes.com" || got.Site != "https://web.mit.edu/~dreyes/" {
		t.Errorf("classified: %+v", got)
	}
	if len(got.Links) != len(d.Links) {
		t.Errorf("Links is the raw union and must not change: %v", got.Links)
	}

	// a draft whose only pages are platform ones gets no Site at all
	only := ClassifyLinks(CandidateDraft{Links: []string{"https://openalex.org/A1", "https://pubmed.ncbi.nlm.nih.gov/123/"}})
	if only.Site != "" || only.Homepage != "" {
		t.Errorf("a platform page was promoted: %+v", only)
	}
}
