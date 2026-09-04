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
}

// doiRe matches a bare DOI (the registry's own shape).
var doiRe = regexp.MustCompile(`(?i)\b(10\.\d{4,9}/[^\s"'<>]+)`)

// orcidRe matches an ORCID iD with or without its URL.
var orcidRe = regexp.MustCompile(`\b(\d{4}-\d{4}-\d{4}-\d{3}[\dX])\b`)

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
		r.Why = "nothing pasted"
		return r
	}

	// a bare ORCID iD, with or without the URL around it
	if m := orcidRe.FindStringSubmatch(raw); m != nil && !strings.Contains(raw, " ") {
		r.Kind, r.Class, r.Dest = "orcid", SeedPerson, DestCandidate
		r.ORCID = m[1]
		r.URL = "https://orcid.org/" + m[1]
		r.Name, r.Provisional = "", true
		r.Adapters = []string{"orcid", "openalex"}
		r.Why = "an ORCID iD — ORCID and OpenAlex both answer to it"
		return r
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
	r.Why = "a DOI — OpenAlex returns the paper and every author on it"
	return r
}

func resolveURL(r Resolution, u *url.URL) Resolution {
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	segs := pathSegments(u.Path)
	r.URL = u.String()

	switch {
	case hostSuffix(host, "orcid.org"):
		if m := orcidRe.FindStringSubmatch(u.Path); m != nil {
			r.ORCID = m[1]
		}
		r.Kind, r.Class, r.Dest = "orcid", SeedPerson, DestCandidate
		r.Name, r.Provisional = "", true
		r.Adapters = []string{"orcid", "openalex"}
		r.Why = "an ORCID profile — ORCID and OpenAlex both answer to it"

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
			r.Kind, r.Class, r.Dest = "github-user", SeedPerson, DestCandidate
			r.Handle = segs[0]
			r.Name, r.Provisional = segs[0], true
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
		r.Kind, r.Class, r.Dest = "social", SeedPerson, DestCandidate
		if len(segs) > 1 {
			r.Handle = segs[len(segs)-1]
			r.Name, r.Provisional = strings.ReplaceAll(segs[len(segs)-1], "-", " "), true
		}
		r.LinkOnly = true
		r.Why = "a LinkedIn profile — kept as a link; no adapter reads LinkedIn (D12)"

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

func resolveName(r Resolution, raw string) Resolution {
	words := strings.Fields(raw)
	r.Kind = "name"
	r.Name = raw
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
