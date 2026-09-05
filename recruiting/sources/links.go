package sources

import (
	"net/url"
	"strings"
)

// Link classification (source-card enrichment Phase 1, owner call O3).
//
// A draft's Links are an opaque union of whatever each source cited: an
// author page, a repo, a blog, a registry entry. A card cannot say "here is
// their LinkedIn" from that, so this sorts each link into ONE of four named
// classes by HOST — a deterministic table, no heuristics on the person —
// with a `site` fallback for a page that is plainly theirs-or-not-theirs but
// that this code will not CLAIM is a personal homepage.
//
// ⚠ NEVER GUESS. A URL is taken exactly as the source gave it; no handle is
// derived from a name, no profile is fabricated from a domain. The only
// question answered here is "what kind of page is this URL", and when the
// honest answer is "some page" the answer is `site`.

// LinkKind is the closed classification vocabulary.
type LinkKind string

const (
	LinkLinkedIn LinkKind = "linkedin"
	LinkGitHub   LinkKind = "github"
	LinkORCID    LinkKind = "orcid"
	// LinkHomepage is a bare personal domain — assigned only when the host is
	// on no known platform, under no institutional TLD, and the path is
	// shallow. Confidence, not coverage.
	LinkHomepage LinkKind = "homepage"
	// LinkSite is every other http(s) page: an institutional people-page, a
	// deep path on a personal domain, a hosted blog. Real, cited, but not
	// something this code will call a homepage.
	LinkSite LinkKind = "site"
)

// linkPlatformHosts are hosts (exact, or any subdomain of) whose pages are
// a PLATFORM's, never a personal domain: indexes, registries, publishers,
// hosted-blog and social platforms. A link here is `site`, and it also never
// fills the draft's Site slot — the card already cites it as evidence and a
// contact strip gains nothing by repeating an index URL.
var linkPlatformHosts = []string{
	// scholarly indexes, registries, publishers
	"openalex.org", "orcid.org", "scholar.google.com", "semanticscholar.org",
	"researchgate.net", "academia.edu", "ncbi.nlm.nih.gov", "nih.gov", "doi.org",
	"arxiv.org", "biorxiv.org", "medrxiv.org", "ssrn.com", "dblp.org", "scopus.com",
	"webofscience.com", "publons.com", "ieee.org", "ieeexplore.ieee.org", "acm.org",
	"springer.com", "link.springer.com", "sciencedirect.com", "wiley.com", "nature.com",
	"science.org", "pnas.org", "cell.com", "elsevier.com", "frontiersin.org", "mdpi.com",
	"plos.org", "jstor.org", "loop.frontiersin.org", "clinicaltrials.gov", "patents.google.com",
	// code hosting and pages
	"github.com", "github.io", "gist.github.com", "gitlab.com", "gitlab.io", "bitbucket.org",
	"huggingface.co", "kaggle.com", "pages.dev", "netlify.app", "vercel.app", "herokuapp.com",
	"readthedocs.io", "pypi.org", "npmjs.com", "crates.io",
	// hosted blogs, link pages, social
	"linkedin.com", "twitter.com", "x.com", "bsky.app", "mastodon.social", "facebook.com",
	"instagram.com", "youtube.com", "youtu.be", "medium.com", "substack.com", "wordpress.com",
	"blogspot.com", "tumblr.com", "notion.site", "notion.so", "sites.google.com", "google.com",
	"linktr.ee", "about.me", "carrd.co", "wixsite.com", "weebly.com", "squarespace.com",
	"godaddysites.com", "strikingly.com", "behance.net", "dribbble.com", "stackoverflow.com",
	"reddit.com", "slideshare.net", "speakerdeck.com", "calendly.com", "zoom.us", "meetup.com",
	"eventbrite.com", "crunchbase.com", "angel.co", "wellfound.com", "glassdoor.com",
	"indeed.com", "ashbyhq.com", "greenhouse.io", "lever.co", "workable.com",
	"wikipedia.org", "wikidata.org", "amazon.com", "goodreads.com",
}

// linkInstitutionalSuffixes are TLD patterns under which a page belongs to
// an institution, not a person, whatever its path says (`~name` is still the
// university's server). Such a link is `site`.
var linkInstitutionalSuffixes = []string{
	".edu", ".gov", ".mil", ".int", ".ac.uk", ".ac.jp", ".ac.kr", ".ac.in", ".ac.il", ".ac.nz",
	".ac.za", ".ac.at", ".ac.be", ".ac.cn", ".edu.au", ".edu.cn", ".edu.sg", ".edu.hk",
	".edu.tw", ".edu.br", ".edu.mx", ".edu.co", ".edu.ar",
}

// linkInstitutionalLabels are host-label prefixes that name a university
// outside the .edu/.ac.* namespaces ("uni-heidelberg.de", "univ-paris.fr").
var linkInstitutionalLabels = []string{"uni-", "univ-", "university"}

// linkPathDepthHomepage is the deepest path a homepage may have. A bare
// domain or "/about" is a homepage; "/people/lab/jane" is a page on a site.
const linkPathDepthHomepage = 1

// ClassifyLink sorts one link into its kind. It returns the URL trimmed but
// otherwise EXACTLY as given (scheme, case, trailing slash all kept — the
// source's citation is the source's). Kind is "" for anything that is not
// an http(s) URL: a mailto:, a tel:, a bare word, an unparsable string. Such
// a link is not a page and is never promoted to a profile field (D15).
func ClassifyLink(raw string) (LinkKind, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", raw
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return "", raw
	}
	segs := pathSegments(u.Path)
	switch {
	case hostIs(host, "linkedin.com"):
		// a PROFILE is /in/<handle> (or the older /pub/<handle>/…); a company,
		// school, job or post page on the same host is a page about something
		// else and is `site` — never printed as the person's LinkedIn
		if len(segs) >= 2 && (segs[0] == "in" || segs[0] == "pub") {
			return LinkLinkedIn, raw
		}
		return LinkSite, raw
	case host == "github.com":
		// a profile is github.com/<login>; github.com/<owner>/<repo> names a
		// repo (an org's, as often as not) and is `site`
		if len(segs) == 1 {
			return LinkGitHub, raw
		}
		return LinkSite, raw
	case host == "orcid.org":
		return LinkORCID, raw
	}
	if linkIsPlatform(host) || linkIsInstitutional(host) {
		return LinkSite, raw
	}
	// a homepage is a SHORT host (name.tld, or one label more) with a shallow
	// path; anything else is a page on somebody's site
	if strings.Count(host, ".") > 2 || !strings.Contains(host, ".") {
		return LinkSite, raw
	}
	if strings.Contains(host, "~") || strings.Contains(u.Path, "~") {
		return LinkSite, raw
	}
	if len(segs) > linkPathDepthHomepage {
		return LinkSite, raw
	}
	return LinkHomepage, raw
}

// pathSegments is the non-empty, lower-cased segments of a URL path.
func pathSegments(p string) []string {
	var out []string
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if seg != "" {
			out = append(out, strings.ToLower(seg))
		}
	}
	return out
}

// hostIs reports whether host is domain or a subdomain of it.
func hostIs(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func linkIsPlatform(host string) bool {
	for _, p := range linkPlatformHosts {
		if hostIs(host, p) {
			return true
		}
	}
	return false
}

func linkIsInstitutional(host string) bool {
	for _, s := range linkInstitutionalSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	for _, label := range strings.Split(host, ".") {
		for _, p := range linkInstitutionalLabels {
			if strings.HasPrefix(label, p) {
				return true
			}
		}
	}
	return false
}

// ClassifyLinks fills a draft's structured link fields from its Links, in
// Links order, and ONLY where the field is still blank: a source that set
// `Orcid` outright keeps it. `Site` takes the first fallback page that is
// not a platform's — an institutional people-page, a deep personal path —
// so an index URL the draft already cites never masquerades as a contact.
// Links itself is untouched: it stays the raw union for back-compat.
func ClassifyLinks(d CandidateDraft) CandidateDraft {
	for _, l := range d.Links {
		kind, u := ClassifyLink(l)
		switch kind {
		case LinkLinkedIn:
			if d.LinkedIn == "" {
				d.LinkedIn = u
			}
		case LinkGitHub:
			if d.Github == "" {
				d.Github = u
			}
		case LinkORCID:
			if d.Orcid == "" {
				d.Orcid = u
			}
		case LinkHomepage:
			if d.Homepage == "" {
				d.Homepage = u
			}
		case LinkSite:
			if d.Site == "" && !linkIsPlatform(linkHost(u)) {
				d.Site = u
			}
		}
	}
	return d
}

// linkHost is the folded host of an http(s) URL, "" otherwise.
func linkHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
