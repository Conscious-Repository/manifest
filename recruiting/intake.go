package recruiting

import (
	"net/url"
	"regexp"
	"strings"
)

// INTAKE — one paste, resolved (intake plan §5 stage 1).
//
// The old intake demanded a `class: name` micro-syntax and answered a wrong
// guess with a toast; three separate boxes each wrote a different thin row.
// This is the one front door: paste a link or a name, and the server says what
// it thinks the thing IS, where committing it would land, and which adapters
// can speak about it. Nothing here touches the network — resolution is a pure
// function of the text, so it can answer inside the request and be tested
// exhaustively. Fetching is the run's job, and the run happens after you have
// seen and corrected this.
//
// The rule the resolver keeps: it never invents a fact it did not read in the
// paste. A name derived from a URL is marked provisional, because "smith-lab"
// is a slug, not a name, and the scaffold must not present it as one.

// Destinations — where committing a resolution lands it.
const (
	DestCandidate = "candidate" // candidates/<slug>.md — someone we might hire
	DestNetwork   = "network"   // network/people.md — someone the owner knows
	DestSeed      = "seed"      // seeds.md — a thing we sweep FROM
)

// THE CASCADE (§4 of the surface plan). Six rungs, most-specific first,
// stopping at the first confident answer. Rungs 1–2 are pure and live here;
// 3–5 need a fetch and are applied afterwards by intake_refine.go, which is
// also pure — it is handed what the page said, not the page. A resolution
// always names the rung that decided it, because "why does it think that" is
// the difference between a guess you can correct and a guess you must catch.
const (
	RungIdentifier = "identifier" // 1. the id namespace IS the type (a DOI is a paper)
	RungHost       = "host"       // 2. host + path table
	RungPage       = "page"       // 3. the page's own JSON-LD @type
	RungAccount    = "account"    // 4. the API's own account type (GitHub User/Organization)
	RungOpenGraph  = "og"         // 5. og:type, tiebreaker only
	RungWords      = "words"      // 6. the words in a bare name — the weakest, and it says so
	RungNothing    = "nothing"    // nothing pasted
)

// Resolution is what the resolver made of one paste.
type Resolution struct {
	Text        string   `json:"text"`                  // the paste, trimmed
	Kind        string   `json:"kind"`                  // how it was recognized: orcid|doi|openalex|pubmed|github-user|github-repo|feed|social|site|name
	Class       string   `json:"class"`                 // seed class, "" when genuinely ambiguous
	Suggest     []string `json:"suggest,omitempty"`     // the classes to offer when ambiguous (or as alternatives)
	Dest        string   `json:"dest"`                  // DestCandidate | DestNetwork | DestSeed
	Name        string   `json:"name"`                  // best-effort display name
	Provisional bool     `json:"provisional,omitempty"` // Name came from the URL, not from the paste — ask before storing
	URL         string   `json:"url,omitempty"`
	Org         string   `json:"org,omitempty"`
	Handle      string   `json:"handle,omitempty"` // github / x handle
	DOI         string   `json:"doi,omitempty"`
	ORCID       string   `json:"orcid,omitempty"`
	Adapters    []string `json:"adapters,omitempty"` // source adapters that can speak about this
	Why         string   `json:"why"`                // one line: how it decided
	LinkOnly    bool     `json:"linkOnly,omitempty"` // no adapter may fetch it — keep the URL as a link on a person
	Rung        string   `json:"rung,omitempty"`     // WHICH rung decided (see the cascade above)
	Asked       string   `json:"asked,omitempty"`    // what a fetched rung read, verbatim ("Organization")
}

// Certain reports whether the class was decided by an unambiguous rung. A
// certain resolution commits without asking; an uncertain one shows its
// ranked chips, because a wrong type silently written is the one mistake this
// surface cannot show you later.
func (r Resolution) Certain() bool {
	return r.Class != "" && len(r.Suggest) == 0 &&
		r.Rung != RungWords && r.Rung != RungOpenGraph
}

// doiRe matches a bare DOI (the registry's own shape).
var doiRe = regexp.MustCompile(`(?i)\b(10\.\d{4,9}/[^\s"'<>]+)`)

// orcidRe matches an ORCID iD with or without its URL.
var orcidRe = regexp.MustCompile(`\b(\d{4}-\d{4}-\d{4}-\d{3}[\dX])\b`)

// The other identifier namespaces of rung 1. Each is a registry whose id
// SHAPE already says what the thing is, so no fetch and no judgment: an
// arXiv id is a paper, a ROR id is a research organisation, and a PMID is a
// paper — but only when it is spelled `pmid:`, because eight bare digits are
// not an identifier, they are eight digits.
var (
	arxivRe = regexp.MustCompile(`(?i)\barxiv[:/]\s*(\d{4}\.\d{4,5}(v\d+)?|[a-z-]+(\.[A-Z]{2})?/\d{7}(v\d+)?)\b`)
	pmidRe  = regexp.MustCompile(`(?i)\bpmid:?\s*(\d{6,8})\b`)
	rorRe   = regexp.MustCompile(`(?i)\b(?:ror\.org/|ror:)\s*(0[0-9a-hjkmnp-tv-z]{8})\b`)
)

// hostSuffix reports whether host is h or a subdomain of it.
func hostSuffix(host, h string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	return host == h || strings.HasSuffix(host, "."+h)
}

// feedPathRe catches the ordinary feed spellings without fetching anything.
var feedPathRe = regexp.MustCompile(`(?i)(^|/)(feed|feeds|rss|atom|podcast)(/|$)|\.(rss|atom|xml)$`)

// orgCues / labCues are the words that make a bare string an organisation
// rather than a person. Deliberately small: a wrong guess is corrected by one
// click on a chip, and a long list would start guessing at people's names.
var (
	labCues = []string{"lab", "laboratory", "university", "univ", "institute", "school",
		"college", "department", "dept", "center", "centre", "hospital", "faculty"}
	companyCues = []string{"inc", "inc.", "llc", "ltd", "corp", "corporation", "co.",
		"gmbh", "biosciences", "bioscience", "therapeutics", "technologies", "labs",
		"systems", "sciences", "pharma", "pharmaceuticals"}
)

func hasCue(words []string, cues []string) bool {
	for _, w := range words {
		w = strings.ToLower(strings.Trim(w, ".,()"))
		for _, c := range cues {
			if w == strings.TrimSuffix(c, ".") || w == c {
				return true
			}
		}
	}
	return false
}

// ResolveIntake classifies one pasted line. It never fetches.
func ResolveIntake(text string) Resolution {
	raw := strings.TrimSpace(text)
	r := Resolution{Text: raw}
	if raw == "" {
		r.Rung, r.Why = RungNothing, "nothing pasted"
		return r
	}

	// ---- rung 1: the identifier namespace IS the type ----

	// a bare ORCID iD, with or without the URL around it
	if m := orcidRe.FindStringSubmatch(raw); m != nil && !strings.Contains(raw, " ") {
		return resolveORCID(r, m[1])
	}
	// arXiv and PMID name themselves, so they are read even out of prose
	if m := arxivRe.FindStringSubmatch(raw); m != nil {
		return resolveArxiv(r, m[1])
	}
	if m := pmidRe.FindStringSubmatch(raw); m != nil {
		return resolvePMID(r, m[1])
	}
	if m := rorRe.FindStringSubmatch(raw); m != nil {
		return resolveROR(r, strings.ToLower(m[1]))
	}
	// a bare DOI (only when the whole paste is one token — prose mentioning a
	// DOI is a note, not an intake)
	if !strings.Contains(raw, " ") {
		if m := doiRe.FindStringSubmatch(raw); m != nil && !strings.HasPrefix(strings.ToLower(raw), "http") {
			return resolveDOI(r, strings.TrimRight(m[1], ".,"))
		}
	}

	if u := parseURLish(raw); u != nil {
		return resolveURL(r, u)
	}
	return resolveName(r, raw)
}

// parseURLish accepts what people actually paste: a full URL, or a bare
// host/path with no scheme (x.com/someone).
func parseURLish(s string) *url.URL {
	if strings.ContainsAny(s, " \t") {
		return nil
	}
	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return u
	}
	if strings.Contains(s, "/") || strings.HasPrefix(strings.ToLower(s), "www.") {
		if u, err := url.Parse("https://" + s); err == nil && u.Host != "" && strings.Contains(u.Host, ".") {
			return u
		}
	}
	return nil
}

func resolveDOI(r Resolution, doi string) Resolution {
	r.Kind, r.Class, r.Dest = "doi", SeedWork, DestSeed
	r.DOI = doi
	r.URL = "https://doi.org/" + doi
	r.Name, r.Provisional = doi, true
	r.Adapters = []string{"openalex"}
	r.Rung = RungIdentifier
	r.Why = "a DOI — OpenAlex returns the paper and every author on it"
	return r
}

func resolveORCID(r Resolution, id string) Resolution {
	r.Kind, r.Class, r.Dest = "orcid", SeedPerson, DestCandidate
	r.ORCID = id
	r.URL = "https://orcid.org/" + id
	r.Name, r.Provisional = "", true
	r.Adapters = []string{"orcid", "openalex"}
	r.Rung = RungIdentifier
	r.Why = "an ORCID iD — ORCID and OpenAlex both answer to it"
	return r
}

// arXiv ids resolve through the DOI registry (10.48550/arXiv.<id>), so the
// paper is looked up the same way every other paper is.
func resolveArxiv(r Resolution, id string) Resolution {
	r.Kind, r.Class, r.Dest = "arxiv", SeedWork, DestSeed
	r.DOI = "10.48550/arXiv." + id
	r.URL = "https://arxiv.org/abs/" + id
	r.Name, r.Provisional = "arXiv:"+id, true
	r.Adapters = []string{"openalex"}
	r.Rung = RungIdentifier
	r.Why = "an arXiv id — a preprint, and OpenAlex knows it by its arXiv DOI"
	return r
}

func resolvePMID(r Resolution, id string) Resolution {
	r.Kind, r.Class, r.Dest = "pubmed", SeedWork, DestSeed
	r.URL = "https://pubmed.ncbi.nlm.nih.gov/" + id + "/"
	r.Name, r.Provisional = "PMID "+id, true
	r.Adapters = []string{"pubmed", "openalex"}
	r.Rung = RungIdentifier
	r.Why = "a PubMed id — a paper, and its authors come with it"
	return r
}

// ROR is the Research Organization Registry: the id says organisation, and
// not which KIND of organisation. Most of the registry is universities,
// institutes and hospitals, so lab leads and company is one chip away — the
// cascade proposes rather than pretending the id said more than it did.
func resolveROR(r Resolution, id string) Resolution {
	r.Kind, r.Class, r.Dest = "ror", SeedLab, DestSeed
	r.URL = "https://ror.org/" + id
	r.Name, r.Provisional = id, true
	r.Suggest = []string{SeedCompany}
	r.Adapters = []string{"web", "openalex"}
	r.Rung = RungIdentifier
	r.Why = "an ROR id — a registered research organisation; say company if it is one"
	return r
}

func resolveURL(r Resolution, u *url.URL) Resolution {
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	segs := pathSegments(u.Path)
	r.URL = u.String()
	r.Rung = RungHost

	switch {
	case hostSuffix(host, "orcid.org"):
		if m := orcidRe.FindStringSubmatch(u.Path); m != nil {
			return resolveORCID(r, m[1])
		}
		r.Kind, r.Class, r.Dest = "orcid", SeedPerson, DestCandidate
		r.Name, r.Provisional = "", true
		r.Adapters = []string{"orcid", "openalex"}
		r.Why = "an ORCID profile — ORCID and OpenAlex both answer to it"

	// preprint servers: the DOI is in the path, so the paper resolves the
	// same way every other paper does rather than as "some page"
	case hostSuffix(host, "arxiv.org"):
		if m := arxivRe.FindStringSubmatch(u.Path); m != nil {
			return resolveArxiv(r, m[1])
		}
		if len(segs) >= 2 && (segs[0] == "abs" || segs[0] == "pdf") {
			return resolveArxiv(r, strings.TrimSuffix(segs[1], ".pdf"))
		}
		r.Kind, r.Class, r.Dest = "site", "", DestSeed
		r.Name, r.Provisional = host, true
		r.Why = "arXiv itself — paste one paper"

	case hostSuffix(host, "biorxiv.org"), hostSuffix(host, "medrxiv.org"):
		if m := doiRe.FindStringSubmatch(u.Path); m != nil {
			return resolveDOI(r, strings.TrimSuffix(strings.TrimRight(m[1], ".,"), ".full"))
		}
		r.Kind, r.Class, r.Dest = "site", SeedWork, DestSeed
		r.Name, r.Provisional = lastSeg(segs, "a preprint"), true
		r.Adapters = []string{"openalex"}
		r.Why = "a preprint server"

	case hostSuffix(host, "ror.org"):
		if m := rorRe.FindStringSubmatch(u.String()); m != nil {
			return resolveROR(r, strings.ToLower(m[1]))
		}
		r.Kind, r.Class, r.Dest = "ror", SeedLab, DestSeed
		r.Suggest = []string{SeedCompany}
		r.Name, r.Provisional = lastSeg(segs, host), true
		r.Why = "an ROR record"

	case hostSuffix(host, "doi.org"):
		if m := doiRe.FindStringSubmatch(u.Path); m != nil {
			return resolveDOI(r, m[1])
		}
		r.Kind, r.Class, r.Dest = "doi", SeedWork, DestSeed
		r.Name, r.Provisional = "a paper", true
		r.Adapters = []string{"openalex"}
		r.Why = "a DOI link"

	case hostSuffix(host, "openalex.org"):
		id := ""
		if len(segs) > 0 {
			id = strings.ToUpper(segs[len(segs)-1])
		}
		if strings.HasPrefix(id, "W") {
			r.Kind, r.Class, r.Dest = "openalex", SeedWork, DestSeed
			r.Why = "an OpenAlex work — its authors come with it"
		} else {
			r.Kind, r.Class, r.Dest = "openalex", SeedPerson, DestCandidate
			r.Why = "an OpenAlex author"
		}
		r.Name, r.Provisional = id, true
		r.Adapters = []string{"openalex"}

	case hostSuffix(host, "pubmed.ncbi.nlm.nih.gov"), hostSuffix(host, "ncbi.nlm.nih.gov"):
		r.Kind, r.Class, r.Dest = "pubmed", SeedWork, DestSeed
		r.Name, r.Provisional = lastSeg(segs, "a paper"), true
		r.Adapters = []string{"pubmed", "openalex"}
		r.Why = "a PubMed record"

	case hostSuffix(host, "reporter.nih.gov"):
		r.Kind, r.Class, r.Dest = "grant", SeedWork, DestSeed
		r.Name, r.Provisional = lastSeg(segs, "a grant"), true
		r.Adapters = []string{"nihreporter"}
		r.Why = "an NIH RePORTER project — its PIs come with it"

	case hostSuffix(host, "github.com"):
		switch {
		case len(segs) >= 2:
			r.Kind, r.Class, r.Dest = "github-repo", SeedRepo, DestSeed
			r.Handle = segs[0]
			r.Name = segs[0] + "/" + segs[1]
			r.Adapters = []string{"github"}
			r.Why = "a GitHub repo — its contributors are readable"
		case len(segs) == 1:
			// ⚠ the genuinely ambiguous one: github.com/numpy is an
			// ORGANISATION and github.com/torvalds is a person, and the URL
			// cannot tell them apart. Person leads because most pasted
			// accounts are people; rung 4 asks GitHub, which knows.
			r.Kind, r.Class, r.Dest = "github-user", SeedPerson, DestCandidate
			r.Handle = segs[0]
			r.Name, r.Provisional = segs[0], true
			r.Suggest = []string{SeedCompany, SeedLab}
			r.Adapters = []string{"github"}
			r.Why = "a GitHub account — the profile carries name, org and site"
		default:
			r.Kind, r.Class, r.Dest = "site", "", DestSeed
			r.Suggest = []string{SeedCompany, SeedLab}
			r.Name, r.Provisional = host, true
			r.Why = "github.com itself — say what you meant"
		}

	case hostSuffix(host, "x.com"), hostSuffix(host, "twitter.com"):
		r.Kind, r.Class, r.Dest = "social", SeedPerson, DestCandidate
		if len(segs) > 0 {
			r.Handle = "@" + segs[0]
			r.Name, r.Provisional = segs[0], true
		}
		r.LinkOnly = true
		r.Why = "an X profile — kept as a link on the person; X's API is paid and nothing here crawls it"

	case hostSuffix(host, "linkedin.com"):
		// /in/<slug> is a person and /company|/school/<slug> is not — the
		// path segment says which, and reading every LinkedIn URL as a person
		// filed companies onto the board as candidates.
		r.Kind, r.LinkOnly = "social", true
		if len(segs) > 1 {
			r.Handle = segs[len(segs)-1]
			r.Name, r.Provisional = strings.ReplaceAll(segs[len(segs)-1], "-", " "), true
		}
		switch {
		case len(segs) > 1 && segs[0] == "company":
			r.Class, r.Dest = SeedCompany, DestSeed
			r.Suggest = []string{SeedLab}
			r.Why = "a LinkedIn company page — kept as a link; no adapter reads LinkedIn (D12)"
		case len(segs) > 1 && (segs[0] == "school" || segs[0] == "edu"):
			r.Class, r.Dest = SeedLab, DestSeed
			r.Suggest = []string{SeedCompany}
			r.Why = "a LinkedIn school page — kept as a link; no adapter reads LinkedIn (D12)"
		default:
			r.Class, r.Dest = SeedPerson, DestCandidate
			r.Why = "a LinkedIn profile — kept as a link; no adapter reads LinkedIn (D12)"
		}

	// the scholarly profile hosts that block crawlers: the CLASS is certain
	// from the path, and the lookup is done by name through the open indexes
	case hostSuffix(host, "scholar.google.com"):
		r.Kind, r.Class, r.Dest = "social", SeedPerson, DestCandidate
		r.Handle = u.Query().Get("user")
		r.Name, r.Provisional = "", true
		r.LinkOnly = true
		r.Adapters = []string{"openalex", "orcid"}
		r.Why = "a Google Scholar profile — Scholar has no API, so OpenAlex answers for it"

	case hostSuffix(host, "researchgate.net"), hostSuffix(host, "bsky.app"), hostSuffix(host, "mastodon.social"):
		r.Kind, r.Class, r.Dest = "social", SeedPerson, DestCandidate
		if len(segs) > 1 {
			r.Handle = segs[len(segs)-1]
			r.Name, r.Provisional = strings.ReplaceAll(segs[len(segs)-1], "-", " "), true
		}
		r.LinkOnly = true
		r.Why = "a profile on " + host + " — kept as a link on the person"

	case hostSuffix(host, "youtube.com"), hostSuffix(host, "substack.com"):
		r.Kind, r.Class, r.Dest = "feed", SeedMedia, DestSeed
		r.Name, r.Provisional = lastSeg(segs, host), true
		r.Adapters = []string{"feed"}
		r.Why = "a channel or publication — read as a feed, episodes and all"

	case isFeedish(host, u.Path):
		r.Kind, r.Class, r.Dest = "feed", SeedMedia, DestSeed
		r.Name, r.Provisional = host, true
		r.Adapters = []string{"feed"}
		r.Why = "a feed — the show or blog is read from it, episodes and all"

	default:
		r.Kind, r.Dest = "site", DestSeed
		r.Class = ""
		r.Suggest = []string{SeedLab, SeedCompany, SeedMedia}
		r.Name, r.Provisional = host, true
		r.Adapters = []string{"web"}
		r.Why = "a page — say whether it's a lab or a company and the crawler can sweep it"
	}
	return r
}

// resolveName is rung 6, and the weakest: nothing here is an identifier, so
// the answer comes from the words themselves and is offered, never asserted.
func resolveName(r Resolution, raw string) Resolution {
	words := strings.Fields(raw)
	r.Kind = "name"
	r.Name = raw
	r.Rung = RungWords
	switch {
	case hasCue(words, labCues):
		r.Class, r.Dest = SeedLab, DestSeed
		r.Suggest = []string{SeedCompany}
		r.Adapters = []string{"web", "nihreporter", "openalex"}
		r.Why = "reads as a lab or department — a website makes it sweepable"
	case hasCue(words, companyCues):
		r.Class, r.Dest = SeedCompany, DestSeed
		r.Suggest = []string{SeedLab}
		r.Adapters = []string{"web"}
		r.Why = "reads as a company — a website makes it sweepable"
	default:
		r.Class, r.Dest = SeedPerson, DestCandidate
		r.Suggest = []string{SeedCompany, SeedLab}
		r.Adapters = []string{"openalex", "orcid", "github", "pubmed", "nihreporter"}
		r.Why = "reads as a person — the scholarly and code sources all take a name"
	}
	return r
}

func pathSegments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func lastSeg(segs []string, fallback string) string {
	if len(segs) == 0 {
		return fallback
	}
	return segs[len(segs)-1]
}

// isFeedish recognizes the ordinary feed spellings plus the podcast hosts that
// never spell them (a show page on Apple or Spotify is the show, not a page we
// would crawl for people).
func isFeedish(host, path string) bool {
	for _, h := range []string{"feeds.megaphone.fm", "anchor.fm", "feeds.buzzsprout.com",
		"feeds.simplecast.com", "feeds.transistor.fm", "feed.podbean.com", "libsyn.com"} {
		if hostSuffix(host, h) {
			return true
		}
	}
	if hostSuffix(host, "podcasts.apple.com") || hostSuffix(host, "podcasters.spotify.com") {
		return true
	}
	if hostSuffix(host, "open.spotify.com") && strings.Contains(path, "/show/") {
		return true
	}
	if strings.HasPrefix(host, "feeds.") || strings.HasPrefix(host, "feed.") {
		return true
	}
	return feedPathRe.MatchString(path)
}
