package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// Web is the Phase 3b.6 targeted web adapter: a BOUNDED traversal from one
// owner-supplied seed URL (D16), not a crawler. Breadth-first, serialized,
// at most webMaxDepth hops from the seed and at most webMaxPages pages per
// run, with every fetched page and every emitted fact carrying the exact
// URL, the retrieval time and the URL it was discovered from. The frontier
// is a local variable: nothing about the traversal survives Search returning.
//
// What it will not do, by construction rather than by policy text:
//   - fetch anything but http(s);
//   - touch LinkedIn or the social-graph hosts in webBlockedHosts, at any
//     depth, including via redirect;
//   - fetch a login / session / auth URL, by path segment or query key;
//   - fetch a path robots.txt disallows for `User-agent: *`, when robots.txt
//     is fetchable and parses;
//   - follow a cross-domain link it did not discover on a traversed page;
//   - put an email or phone on a draft (D15) — a line carrying an address is
//     not even quoted.
//
// Candidate extraction is deliberately conservative: a draft is emitted only
// for a person-shaped name line followed (within the same card) by a line
// with a title or organisation cue. A page that names nobody that plainly
// yields zero drafts; the adapter never invents an identity. Loose page
// text is TrustLow.
type Web struct {
	// Client is held BY VALUE (rule 1: no pointer or interface field on an
	// adapter). A zero Client is the default transport; a Timeout of zero
	// gets webTimeout applied per request.
	Client http.Client
	// Delay is the pause between consecutive requests. Zero means
	// webDefaultDelay; a negative value disables the pause (tests).
	Delay time.Duration
}

// compile-time proof that the Web source satisfies the adapter contract.
var _ Adapter = Web{}

const (
	// WebUserAgent is the polite identifier every request carries. The
	// robots.txt reader also honours a group addressed to it by name.
	WebUserAgent = "manifest-aion-recruiting/phase3b (bounded; +https://github.com/benjaminbanderson/manifest)"

	// Scope field keys.
	webFieldSeed  = "seed_url"
	webFieldPages = "max_pages"
	webFieldDepth = "depth"

	// webDefaultPages / webMaxPages bound the page budget (D16: default 25,
	// configurable up to 50). webMaxDepth is the hop ceiling; the default
	// is the ceiling.
	webDefaultPages = 25
	webMaxPages     = 50
	webMaxDepth     = 3
	// webDefaultDelay spaces requests when the adapter is given none.
	webDefaultDelay = 250 * time.Millisecond
	// webTimeout applies per request when the injected client has none.
	webTimeout = 20 * time.Second
	// webMaxBody bounds how much of a page is read; webMaxRobots how much
	// of a robots.txt.
	webMaxBody   = 2 << 20
	webMaxRobots = 256 << 10
	// webMaxRedirects caps a redirect chain; each hop is re-checked.
	webMaxRedirects = 5
	// webSnippetChars caps a quoted card.
	webSnippetChars = 320
	// webCardLines is how many lines after a name line may belong to its
	// card.
	webCardLines = 3
	// webMaxNameRunes bounds a person-shaped line.
	webMaxNameRunes = 48
)

// webBlockedHosts are never fetched at any depth: LinkedIn and the social
// graphs (D16). A host matches when equal or a subdomain.
var webBlockedHosts = []string{
	"linkedin.com", "facebook.com", "fb.com", "instagram.com", "x.com",
	"twitter.com", "threads.net", "tiktok.com",
}

// webAuthSegments are path segments (extension stripped) that mark a
// login / session / auth page.
var webAuthSegments = map[string]bool{
	"login": true, "log-in": true, "signin": true, "sign-in": true, "signup": true,
	"sign-up": true, "register": true, "auth": true, "oauth": true, "oauth2": true,
	"session": true, "sessions": true, "logout": true, "log-out": true, "signout": true,
	"sign-out": true, "password": true, "reset-password": true, "sso": true,
	"account": true, "accounts": true, "wp-login": true, "wp-admin": true, "admin": true,
}

// webAuthQueryKeys are query keys that carry a session or credential.
var webAuthQueryKeys = map[string]bool{
	"session": true, "sessionid": true, "session_id": true, "sid": true, "token": true,
	"access_token": true, "auth": true, "jsessionid": true, "phpsessid": true,
	"password": true, "cookie": true, "login": true, "api_key": true, "apikey": true,
}

// webSkipExt are link targets that cannot be an HTML page worth a fetch.
var webSkipExt = map[string]bool{
	".pdf": true, ".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".svg": true,
	".webp": true, ".zip": true, ".gz": true, ".tar": true, ".css": true, ".js": true,
	".json": true, ".xml": true, ".rss": true, ".ico": true, ".mp4": true, ".mp3": true,
	".doc": true, ".docx": true, ".ppt": true, ".pptx": true, ".xls": true, ".xlsx": true,
	".bib": true, ".txt": true, ".csv": true,
}

// webLabSignals mark a page as people/lab-shaped even when no query term
// matches it.
var webLabSignals = []string{
	"people", "team", "lab ", "laboratory", "faculty", "members", "staff",
	"postdoc", "phd", "researcher", "research group", "our group",
}

// webTitleCues are the words that make a line read as a title or role.
var webTitleCues = map[string]bool{
	"phd": true, "postdoc": true, "postdoctoral": true, "professor": true, "student": true,
	"engineer": true, "scientist": true, "researcher": true, "research": true, "director": true,
	"fellow": true, "candidate": true, "lead": true, "investigator": true, "principal": true,
	"founder": true, "co-founder": true, "cofounder": true, "associate": true, "assistant": true,
	"physicist": true, "technician": true, "developer": true, "faculty": true, "staff": true,
	"manager": true, "head": true, "chair": true, "md": true, "msc": true, "mba": true,
	"intern": true, "ceo": true, "cto": true, "pi": true, "lecturer": true, "instructor": true,
	"specialist": true, "analyst": true, "architect": true, "programmer": true,
}

// webOrgCues are the words that make a line read as an organisation.
var webOrgCues = map[string]bool{
	"university": true, "institute": true, "laboratory": true, "lab": true, "hospital": true,
	"college": true, "school": true, "inc": true, "inc.": true, "ltd": true, "llc": true,
	"corp": true, "company": true, "center": true, "centre": true, "department": true,
	"clinic": true, "foundation": true, "group": true, "gmbh": true, "labs": true,
}

// webNameStop are capitalised words that disqualify a line from being a
// person's name: section headings, org words, nav labels.
var webNameStop = map[string]bool{
	"lab": true, "laboratory": true, "labs": true, "university": true, "institute": true,
	"department": true, "center": true, "centre": true, "group": true, "team": true,
	"people": true, "home": true, "about": true, "contact": true, "news": true, "research": true,
	"publications": true, "projects": true, "members": true, "faculty": true, "staff": true,
	"students": true, "alumni": true, "join": true, "login": true, "sign": true, "search": true,
	"menu": true, "school": true, "college": true, "hospital": true, "company": true, "inc": true,
	"llc": true, "program": true, "page": true, "site": true, "the": true, "our": true, "all": true,
	"new": true, "read": true, "more": true, "us": true, "in": true, "of": true, "and": true,
	"for": true, "with": true, "current": true, "former": true, "principal": true,
	"investigator": true, "postdoctoral": true, "graduate": true, "undergraduate": true,
	"visiting": true, "senior": true, "junior": true, "lead": true, "open": true, "positions": true,
	"privacy": true, "policy": true, "terms": true, "skip": true, "main": true, "content": true,
	"facebook": true, "twitter": true, "instagram": true, "linkedin": true, "github": true,
	"youtube": true, "google": true, "email": true, "phone": true, "tel": true, "web": true,
	"website": true, "cv": true, "resume": true, "bio": true, "profile": true,
}

// webNameParticles may appear lower-cased in the middle of a name.
var webNameParticles = map[string]bool{
	"van": true, "von": true, "de": true, "da": true, "del": true, "della": true, "di": true,
	"la": true, "le": true, "du": true, "der": true, "den": true, "bin": true, "al": true,
}

func (Web) ID() string { return "web" }

func (Web) Kind() Kind { return KindWeb }

func (Web) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "relevance terms", Placeholder: "e.g. mri pulse sequence postdoc", Required: true},
		{Key: webFieldSeed, Label: "seed URL", Placeholder: "https://lab.example.edu/people", Required: true},
		{Key: webFieldPages, Label: "max pages", Placeholder: strconv.Itoa(webDefaultPages) + " (cap " + strconv.Itoa(webMaxPages) + ")"},
		{Key: webFieldDepth, Label: "depth", Placeholder: strconv.Itoa(webMaxDepth) + " (max " + strconv.Itoa(webMaxDepth) + ")"},
	}
}

// webFrontierItem is one URL waiting its turn, with where it came from.
// The slice it lives in is local to Search.
type webFrontierItem struct {
	url   *url.URL
	depth int
	from  string
}

// Search performs the bounded traversal and returns the drafts it could
// cite. The seed is fetched first; its links, then their links, are visited
// breadth-first until the depth ceiling or the page budget stops it. Every
// URL passes the same filters whether it is the seed, same-domain or a
// cross-domain linkout. Requests are strictly sequential.
func (w Web) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	terms := webTerms(s.Query)
	if len(terms) == 0 {
		return nil, errors.New("web: a run needs relevance terms in the query")
	}
	rawSeed := strings.TrimSpace(s.Fields[webFieldSeed])
	if rawSeed == "" {
		return nil, errors.New("web: a run needs a seed_url")
	}
	seed, err := url.Parse(rawSeed)
	if err != nil || seed.Host == "" || (seed.Scheme != "http" && seed.Scheme != "https") {
		return nil, fmt.Errorf("web: seed_url %q is not an http(s) URL", rawSeed)
	}
	if why := webRefuse(seed); why != "" {
		return nil, fmt.Errorf("web: seed_url refused: %s", why)
	}
	maxPages, err := webBound(s.Fields[webFieldPages], webDefaultPages, 1, webMaxPages, webFieldPages)
	if err != nil {
		return nil, err
	}
	maxDepth, err := webBound(s.Fields[webFieldDepth], webMaxDepth, 0, webMaxDepth, webFieldDepth)
	if err != nil {
		return nil, err
	}
	maxDrafts := s.Max
	if maxDrafts <= 0 {
		maxDrafts = webDefaultPages
	}

	seed.Fragment = ""
	frontier := []webFrontierItem{{url: seed, depth: 0, from: "seed"}}
	visited := map[string]bool{seed.String(): true}
	robots := map[string]*webRobots{}
	var out []CandidateDraft
	seen := map[string]bool{} // page URL + name, so a card quoted twice is one draft
	fetched := 0

	for len(frontier) > 0 && fetched < maxPages && len(out) < maxDrafts {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("web: %v", err)
		}
		item := frontier[0]
		frontier = frontier[1:]

		if !w.allowedByRobots(ctx, robots, item.url) {
			continue
		}
		if fetched > 0 {
			w.pause(ctx)
		}
		fetched++
		page, err := w.fetch(ctx, item.url)
		if err != nil {
			// a 500, a non-HTML body, a refused redirect: the page is
			// skipped, the budget is spent, the run goes on
			continue
		}
		if page.url.String() != item.url.String() {
			// a redirect landed somewhere else — that is the page we hold
			key := page.url.String()
			if visited[key] {
				continue
			}
			visited[key] = true
		}

		relevant := page.relevant(terms)
		if relevant {
			for _, d := range w.drafts(page, item, s.Role) {
				k := page.url.String() + "\x00" + strings.ToLower(d.Name)
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, d)
				if len(out) >= maxDrafts {
					break
				}
			}
		}

		if item.depth >= maxDepth {
			continue
		}
		for _, l := range page.links {
			key := l.url.String()
			if visited[key] {
				continue
			}
			if webRefuse(l.url) != "" || webSkipExt[strings.ToLower(path.Ext(l.url.Path))] {
				continue
			}
			// a cross-domain linkout is followed only when the page that
			// carried it was relevant, or the link itself names a term
			if l.url.Host != page.url.Host && !relevant && !webMentions(l.text+" "+l.url.Path, terms) {
				continue
			}
			visited[key] = true
			frontier = append(frontier, webFrontierItem{url: l.url, depth: item.depth + 1, from: page.url.String()})
		}
	}
	return out, nil
}

// Enrich is a no-op: the traversal already read everything it is allowed
// to, and nothing more is read in this phase.
func (Web) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns what the draft already carries — nothing, in 3b.6.
// Loose page text does not support a relationship claim; same_lab edges
// from an explicit roster are Phase 4.
func (Web) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// ---- scope ----

// webTerms lower-cases and tokenises the query into relevance terms,
// dropping quotes and single characters.
func webTerms(q string) []string {
	var out []string
	for _, f := range strings.Fields(strings.ToLower(q)) {
		f = strings.Trim(f, `"'“”‘’(),;:`)
		if len([]rune(f)) >= 2 {
			out = append(out, f)
		}
	}
	return out
}

// webBound parses an optional integer field and clamps it to [lo, hi];
// empty means def. A non-integer is refused so a typo is not silently a
// default.
func webBound(raw string, def, lo, hi int, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("web: %s %q is not a number", name, raw)
	}
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}
	return n, nil
}

// ---- URL policy ----

// webRefuse reports why a URL may not be fetched, or "" when it may. Every
// URL — seed, same-domain, cross-domain, redirect target — goes through it.
func webRefuse(u *url.URL) string {
	if u == nil {
		return "no URL"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "not http(s)"
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "no host"
	}
	if u.User != nil {
		return "credentials in URL"
	}
	for _, b := range webBlockedHosts {
		if host == b || strings.HasSuffix(host, "."+b) {
			return "social-graph host " + b + " is never fetched"
		}
	}
	for _, seg := range strings.Split(strings.ToLower(u.Path), "/") {
		if seg == "" {
			continue
		}
		if ext := path.Ext(seg); ext != "" && len(ext) <= 5 {
			seg = strings.TrimSuffix(seg, ext)
		}
		if webAuthSegments[seg] {
			return "login/session path /" + seg
		}
	}
	for key := range u.Query() {
		if webAuthQueryKeys[strings.ToLower(key)] {
			return "session/auth query key " + key
		}
	}
	return ""
}

// ---- robots ----

// webRobots is the parsed `User-agent: *` (or our own) group of one host's
// robots.txt: the Disallow patterns only. Allow lines are ignored, which
// errs toward not fetching.
type webRobots struct {
	disallow []string
}

// allowedByRobots fetches robots.txt for the URL's host once per run and
// answers from the cache after. A robots.txt that is missing, errors or
// does not parse allows everything — the page filters still apply.
func (w Web) allowedByRobots(ctx context.Context, cache map[string]*webRobots, u *url.URL) bool {
	host := u.Scheme + "://" + u.Host
	r, ok := cache[host]
	if !ok {
		r = w.fetchRobots(ctx, host)
		cache[host] = r
	}
	if r == nil {
		return true
	}
	return r.allows(u.Path)
}

func (w Web) fetchRobots(ctx context.Context, host string) *webRobots {
	body, _, _, err := w.get(ctx, host+"/robots.txt", webMaxRobots)
	if err != nil {
		return nil
	}
	return parseRobots(string(body))
}

// parseRobots reads the groups addressed to `*` or to this adapter and
// collects their Disallow patterns.
func parseRobots(text string) *webRobots {
	r := &webRobots{}
	applies := false
	sawAgent := false
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			// consecutive user-agent lines share one group; a rule line
			// closes the header
			if !sawAgent {
				applies = false
			}
			sawAgent = true
			agent := strings.ToLower(val)
			if agent == "*" || strings.HasPrefix(strings.ToLower(WebUserAgent), agent) {
				applies = true
			}
		case "disallow":
			sawAgent = false
			if applies && val != "" {
				r.disallow = append(r.disallow, val)
			}
		default:
			sawAgent = false
		}
	}
	return r
}

// allows reports whether no Disallow pattern matches the path. Patterns
// are prefixes with `*` wildcards and an optional `$` end anchor, per the
// robots.txt convention.
func (r *webRobots) allows(p string) bool {
	if p == "" {
		p = "/"
	}
	for _, pat := range r.disallow {
		if robotsMatch(pat, p) {
			return false
		}
	}
	return true
}

func robotsMatch(pat, p string) bool {
	anchored := strings.HasSuffix(pat, "$")
	pat = strings.TrimSuffix(pat, "$")
	parts := strings.Split(pat, "*")
	if !strings.HasPrefix(p, parts[0]) {
		return false
	}
	rest := p[len(parts[0]):]
	if len(parts) == 1 {
		return !anchored || rest == ""
	}
	if anchored {
		last := parts[len(parts)-1]
		if !strings.HasSuffix(rest, last) {
			return false
		}
		rest = rest[:len(rest)-len(last)]
		parts = parts[:len(parts)-1]
	}
	for _, part := range parts[1:] {
		i := strings.Index(rest, part)
		if i < 0 {
			return false
		}
		rest = rest[i+len(part):]
	}
	return true
}

// ---- fetch ----

// webPage is one fetched, parsed HTML page.
type webPage struct {
	url       *url.URL
	retrieved time.Time
	title     string
	lines     []webLine
	links     []webLink
}

// webLine is one block of page text with the http(s) hrefs it carried and
// whether it was a heading.
type webLine struct {
	text  string
	links []string
	// level is the heading level (1 for h1) when the line is a heading,
	// else 0. Only an h1 makes a page "about" the person it names.
	level int
}

type webLink struct {
	url  *url.URL
	text string
}

// fetch GETs one URL, refuses to follow a redirect anywhere the filters
// reject, and parses the body as HTML. A non-200, a non-HTML content type
// or an unparseable body is an error the caller skips.
func (w Web) fetch(ctx context.Context, u *url.URL) (*webPage, error) {
	body, final, ctype, err := w.get(ctx, u.String(), webMaxBody)
	if err != nil {
		return nil, err
	}
	if mt, _, _ := mime.ParseMediaType(ctype); mt != "text/html" && mt != "application/xhtml+xml" {
		return nil, fmt.Errorf("web: %s is %q, not HTML", final, ctype)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("web: %s did not parse: %v", final, err)
	}
	finalURL, err := url.Parse(final)
	if err != nil {
		return nil, err
	}
	finalURL.Fragment = ""
	page := &webPage{url: finalURL, retrieved: time.Now().UTC()}
	page.extract(doc)
	return page, nil
}

// get performs one GET with the polite User-Agent, a redirect policy that
// re-checks every hop, and a bounded read. It returns the body, the URL
// the response actually came from, and its content type.
func (w Web) get(ctx context.Context, target string, maxBody int64) ([]byte, string, string, error) {
	if w.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, webTimeout)
		defer cancel()
	}
	client := w.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= webMaxRedirects {
			return errors.New("too many redirects")
		}
		if why := webRefuse(req.URL); why != "" {
			return errors.New("redirect refused: " + why)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("web: %v", err)
	}
	req.Header.Set("User-Agent", WebUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("web: GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	final := target
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", "", fmt.Errorf("web: reading GET %s: %v", target, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("web: GET %s returned HTTP %d", final, resp.StatusCode)
	}
	return body, final, resp.Header.Get("Content-Type"), nil
}

// pause sleeps the configured delay, or returns early on cancellation.
func (w Web) pause(ctx context.Context) {
	d := w.Delay
	if d == 0 {
		d = webDefaultDelay
	}
	if d < 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// ---- HTML → lines ----

// webBlockTags flush the current line when opened or closed.
var webBlockTags = map[string]bool{
	"p": true, "div": true, "li": true, "ul": true, "ol": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "tr": true, "td": true, "th": true, "dt": true, "dd": true,
	"section": true, "article": true, "header": true, "footer": true, "aside": true, "main": true,
	"table": true, "blockquote": true, "pre": true, "figure": true, "figcaption": true,
	"address": true, "details": true, "summary": true, "form": true, "fieldset": true, "nav": true,
	"hr": true, "br": true, "body": true, "html": true, "title": true, "label": true,
}

var webHeadingTags = map[string]bool{"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true}

// webSkipTags contribute no text.
var webSkipTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true,
	"iframe": true, "object": true, "canvas": true, "select": true, "textarea": true,
}

// extract linearises the document into lines and collects every resolvable
// http(s) link in document order.
func (p *webPage) extract(doc *html.Node) {
	var cur strings.Builder
	var curLinks []string
	curLevel := 0
	flush := func() {
		text := strings.Join(strings.Fields(cur.String()), " ")
		if text != "" {
			p.lines = append(p.lines, webLine{text: text, links: curLinks, level: curLevel})
		}
		cur.Reset()
		curLinks = nil
		curLevel = 0
	}
	var walk func(n *html.Node, level int)
	walk = func(n *html.Node, level int) {
		switch n.Type {
		case html.TextNode:
			cur.WriteString(" ")
			cur.WriteString(n.Data)
			return
		case html.ElementNode:
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, level)
			}
			return
		}
		tag := n.Data
		if webSkipTags[tag] {
			return
		}
		if tag == "title" && p.title == "" {
			p.title = strings.Join(strings.Fields(webText(n)), " ")
			return
		}
		if tag == "a" {
			if href := p.resolve(webAttr(n, "href")); href != nil {
				curLinks = append(curLinks, href.String())
				p.links = append(p.links, webLink{url: href, text: strings.Join(strings.Fields(webText(n)), " ")})
			}
		}
		if webBlockTags[tag] {
			flush()
		}
		if webHeadingTags[tag] {
			level = int(tag[1] - '0')
		}
		if level > 0 && (curLevel == 0 || level < curLevel) {
			curLevel = level
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, level)
		}
		if webBlockTags[tag] {
			flush()
		}
	}
	walk(doc, 0)
	flush()
}

// resolve turns an href into an absolute http(s) URL against the page, or
// nil when it is not one (mailto:, tel:, javascript:, a bare fragment).
func (p *webPage) resolve(href string) *url.URL {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return nil
	}
	ref, err := url.Parse(href)
	if err != nil {
		return nil
	}
	abs := p.url.ResolveReference(ref)
	abs.Fragment = ""
	if abs.Scheme != "http" && abs.Scheme != "https" || abs.Host == "" {
		return nil
	}
	return abs
}

func webAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func webText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(" ")
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// ---- relevance ----

// relevant reports whether the page carries a query term or reads as a
// people/lab page. Simple and deterministic: lower-cased substring.
func (p *webPage) relevant(terms []string) bool {
	var b strings.Builder
	b.WriteString(strings.ToLower(p.title))
	for _, l := range p.lines {
		b.WriteString(" ")
		b.WriteString(strings.ToLower(l.text))
	}
	text := b.String() + " "
	if webMentions(text, terms) {
		return true
	}
	for _, s := range webLabSignals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

func webMentions(text string, terms []string) bool {
	text = strings.ToLower(text)
	for _, t := range terms {
		if strings.Contains(text, t) {
			return true
		}
	}
	return false
}

// ---- candidate extraction ----

// drafts scans the page's lines for person cards: a person-shaped name line
// followed, within webCardLines and before the next name line, by a line
// with a title or organisation cue. Anything less is not a candidate.
func (w Web) drafts(page *webPage, item webFrontierItem, role string) []CandidateDraft {
	var out []CandidateDraft
	for i, line := range page.lines {
		name, inline := webNameLine(line.text)
		if name == "" {
			continue
		}
		card := []string{line.text}
		cues := inline
		links := append([]string(nil), line.links...)
		for j := i + 1; j < len(page.lines) && j <= i+webCardLines; j++ {
			next := page.lines[j]
			if n, _ := webNameLine(next.text); n != "" {
				break
			}
			card = append(card, next.text)
			cues = append(cues, next.text)
			links = append(links, next.links...)
		}
		title, org := webCues(cues)
		if title == "" && org == "" {
			continue
		}
		out = append(out, w.draft(page, item, role, name, title, org, card, links, line.level == 1))
	}
	return out
}

// webNameLine returns the person-shaped name a line starts with, and the
// remaining inline segments ("Dana Reyes — Postdoctoral Fellow" splits on
// the dash). "" when the line is not a name.
func webNameLine(text string) (string, []string) {
	segs := webSplitInline(text)
	if len(segs) == 0 {
		return "", nil
	}
	name := strings.TrimSpace(segs[0])
	if !webPersonName(name) {
		return "", nil
	}
	return name, segs[1:]
}

var webInlineSeps = []string{" — ", " – ", " | ", " - ", ": ", ", ", " · ", " • "}

func webSplitInline(text string) []string {
	segs := []string{text}
	for _, sep := range webInlineSeps {
		var next []string
		for _, s := range segs {
			next = append(next, strings.Split(s, sep)...)
		}
		segs = next
	}
	var out []string
	for _, s := range segs {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// webPersonName is the conservative name shape: two to four words, each
// capitalised (or an initial, or a lower-case particle in the middle),
// letters only, none a stop word, at most webMaxNameRunes long.
func webPersonName(s string) bool {
	if s == "" || len([]rune(s)) > webMaxNameRunes {
		return false
	}
	words := strings.Fields(s)
	if len(words) < 2 || len(words) > 4 {
		return false
	}
	caps := 0
	for i, wd := range words {
		clean := strings.Trim(wd, ".,")
		if clean == "" {
			return false
		}
		lower := strings.ToLower(clean)
		// a title, org or nav word is never part of a name
		if webNameStop[lower] || webTitleCues[lower] || webOrgCues[lower] {
			return false
		}
		if i > 0 && i < len(words)-1 && webNameParticles[lower] {
			continue
		}
		runes := []rune(clean)
		if !unicode.IsUpper(runes[0]) {
			return false
		}
		// a lone capital is an initial only when written as one ("J.")
		if len(runes) == 1 && !strings.HasSuffix(wd, ".") {
			return false
		}
		for _, r := range runes[1:] {
			if !unicode.IsLetter(r) && r != '\'' && r != '-' && r != '’' && r != '.' {
				return false
			}
		}
		// an all-caps word longer than an initial is a heading, not a name
		if len(runes) > 2 && strings.ToUpper(clean) == clean {
			return false
		}
		caps++
	}
	return caps >= 2
}

// webCues picks the title and organisation lines out of a card, if any.
// The first line with a title cue is the title; the first other line with
// an org cue is the org. A line carrying an address is never used (D15).
func webCues(lines []string) (title, org string) {
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || containsAddress(l) || webPhoneish(l) || len([]rune(l)) > 120 {
			continue
		}
		words := strings.Fields(strings.ToLower(l))
		hasTitle, hasOrg := false, false
		for _, wd := range words {
			wd = strings.Trim(wd, ".,;:()")
			if webTitleCues[wd] {
				hasTitle = true
			}
			if webOrgCues[wd] {
				hasOrg = true
			}
		}
		if hasTitle && title == "" {
			title = l
			continue
		}
		if hasOrg && org == "" && l != title {
			org = l
		}
	}
	return title, org
}

// webPhoneish reports whether a line looks like it carries a phone number:
// seven or more digits with only phone punctuation between them.
func webPhoneish(s string) bool {
	digits, run := 0, 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			run++
			if run > digits {
				digits = run
			}
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '+' || r == '.':
		default:
			run = 0
		}
	}
	return digits >= 7
}

// draft builds one cited draft. The evidence row is the page, dated, with
// the card quoted verbatim; the Note carries the traversal provenance
// (which page, discovered from which URL, at what depth), since the
// Evidence DTO has no field for it.
func (w Web) draft(page *webPage, item webFrontierItem, role, name, title, org string, card, links []string, own bool) CandidateDraft {
	pageURL := page.url.String()
	d := CandidateDraft{
		SourceID:   w.ID(),
		ExternalID: pageURL + "#" + webSlug(name),
		Name:       name,
		Title:      title,
		Org:        org,
		Role:       strings.TrimSpace(role),
		Note:       "found on " + pageURL + " · discovered from " + item.from + " · depth " + strconv.Itoa(item.depth),
	}
	seenLink := map[string]bool{}
	if own {
		// the page's h1 names the person: the page is theirs
		d.Links = append(d.Links, pageURL)
		seenLink[pageURL] = true
	}
	for _, l := range links {
		lu, err := url.Parse(l)
		if err != nil || webRefuse(lu) != "" || seenLink[l] {
			continue
		}
		seenLink[l] = true
		d.Links = append(d.Links, l)
	}
	var quoted []string
	for _, c := range card {
		if containsAddress(c) || webPhoneish(c) {
			continue
		}
		quoted = append(quoted, c)
	}
	snippet := strings.Join(quoted, " · ")
	if r := []rune(snippet); len(r) > webSnippetChars {
		snippet = string(r[:webSnippetChars]) + "…"
	}
	d.Evidence = append(d.Evidence, Evidence{
		SourceID: w.ID(), URLOrFile: pageURL, RetrievedAt: page.retrieved,
		Snippet: snippet, Kind: EvidencePage, Trust: TrustLow,
	})
	return d
}

func webSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
