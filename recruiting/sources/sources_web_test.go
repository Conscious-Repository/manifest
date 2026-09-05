package sources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// webNet is an in-process internet: a map from host to handler, served
// through a RoundTripper without a socket. Every request the adapter makes
// is recorded; a request to a host that is not mapped is recorded as a
// leak and fails. That is how the tests prove LinkedIn was never asked for
// — not "it returned nothing" but "the transport never saw it".
type webNet struct {
	mu    sync.Mutex
	hosts map[string]http.Handler
	reqs  []string
	leaks []string
}

func newWebNet() *webNet { return &webNet{hosts: map[string]http.Handler{}} }

func (n *webNet) site(host string, pages map[string]string) *webNet {
	mux := http.NewServeMux()
	for p, body := range pages {
		body := body
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(body))
		})
	}
	n.hosts[host] = mux
	return n
}

func (n *webNet) handler(host string, h http.Handler) *webNet {
	n.hosts[host] = h
	return n
}

func (n *webNet) RoundTrip(req *http.Request) (*http.Response, error) {
	n.mu.Lock()
	n.reqs = append(n.reqs, req.URL.String())
	h, ok := n.hosts[req.URL.Host]
	if !ok {
		n.leaks = append(n.leaks, req.URL.String())
	}
	n.mu.Unlock()
	if !ok {
		return nil, errors.New("webNet: no such host " + req.URL.Host)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

func (n *webNet) adapter() Web {
	return Web{Client: http.Client{Transport: n}, Delay: -1}
}

func (n *webNet) requests() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.reqs...)
}

// pages are the HTML GETs, robots.txt fetches excluded.
func (n *webNet) pages() []string {
	var out []string
	for _, r := range n.requests() {
		if !strings.HasSuffix(r, "/robots.txt") {
			out = append(out, r)
		}
	}
	return out
}

func (n *webNet) requested(u string) bool {
	for _, r := range n.requests() {
		if r == u {
			return true
		}
	}
	return false
}

func (n *webNet) leaked() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.leaks...)
}

// labNet is the fixture site graph the D16 tests share:
//
//	seed.example/                (depth 0) → /people, /login, linkedin, ?session
//	seed.example/people          (depth 1) → lab.example/imaging (cross-domain), facebook, x.com
//	lab.example/imaging          (depth 2) → code.example/dreyes (cross-domain), /private/roster (robots)
//	code.example/dreyes          (depth 3) → code.example/dreyes/deeper (depth 4 — never)
func labNet() *webNet {
	n := newWebNet()
	n.site("seed.example", map[string]string{
		"/": `<html><head><title>Example Imaging Group</title></head><body>
<nav><a href="/">Home</a> <a href="/people">People</a> <a href="/login">Login</a>
<a href="/dashboard?session=abc123">Dashboard</a> <a href="/wp-login.php">Admin</a>
<a href="https://www.linkedin.com/company/example-imaging">LinkedIn</a></nav>
<h1>Example Imaging Group</h1>
<p>We build MRI pulse sequences and reconstruction software.</p>
<a href="/brochure.pdf">Brochure</a>
</body></html>`,
		"/people": `<html><head><title>People · Example Imaging Group</title></head><body>
<h1>People</h1>
<ul>
<li><a href="/people/dana">Dana Reyes</a> — Postdoctoral Fellow, MRI reconstruction</li>
<li><a href="/people/sam">Sam Okafor</a><br>PhD Student<br>Boston University</li>
<li>Read More</li>
<li><a href="https://www.facebook.com/exampleimaging">Facebook</a> <a href="https://x.com/exampleimaging">X</a>
<a href="https://twitter.com/exampleimaging">Twitter</a> <a href="https://instagram.com/exampleimaging">Instagram</a></li>
</ul>
<p>Collaborators: <a href="https://lab.example/imaging">Imaging Lab at lab.example</a></p>
</body></html>`,
		"/people/dana": `<html><head><title>Dana Reyes</title></head><body>
<h1>Dana Reyes</h1><p>Postdoctoral Fellow</p><p>Example Imaging Group</p>
<p>Email: <a href="mailto:dana@example.test">dana@example.test</a> · Phone: +1 (617) 555-0142</p>
</body></html>`,
		"/people/sam": `<html><head><title>Sam Okafor</title></head><body>
<h1>Sam Okafor</h1><p>PhD Student, MRI physics</p>
</body></html>`,
		"/login":        `<html><body><form>login</form></body></html>`,
		"/dashboard":    `<html><body>secret</body></html>`,
		"/wp-login.php": `<html><body>admin</body></html>`,
	})
	n.site("lab.example", map[string]string{
		"/robots.txt": "User-agent: *\nDisallow: /private/\n",
		"/imaging": `<html><head><title>Imaging Lab</title></head><body>
<h1>Imaging Lab</h1>
<h2>Members</h2>
<div class="card"><h3>Priya Natarajan</h3><p>Research Scientist</p><p>Imaging Lab</p>
<a href="https://code.example/dreyes">GitHub</a></div>
<div class="card"><h3>Marcus van der Berg</h3><p>Lab Manager</p></div>
<a href="/private/roster">Private roster</a>
</body></html>`,
		"/private/roster": `<html><body><h3>Hidden Person</h3><p>Professor</p></body></html>`,
	})
	n.site("code.example", map[string]string{
		"/dreyes": `<html><head><title>dreyes</title></head><body>
<h1>Dana Reyes</h1><p>Research Engineer at Example Imaging Group</p>
<a href="/dreyes/deeper">more</a>
</body></html>`,
		"/dreyes/deeper": `<html><body><h1>Too Deep</h1><p>Professor</p></body></html>`,
	})
	return n
}

func webScope(seed string, extra map[string]string) Scope {
	f := map[string]string{"seed_url": seed}
	for k, v := range extra {
		f[k] = v
	}
	return Scope{Role: "role/mri-engineer", Query: "mri imaging", Max: 25, Fields: f}
}

func draftByName(drafts []CandidateDraft, name string) *CandidateDraft {
	for i := range drafts {
		if drafts[i].Name == name {
			return &drafts[i]
		}
	}
	return nil
}

// D16 — the traversal reaches seed → same-domain people page → cross-domain
// lab page → cross-domain person page, breadth-first and serialized, and
// stops at depth 3. Every draft cites the page it came from, dated, and
// says where that page was discovered.
func TestWebTraversesSeedToCrossDomainPerson(t *testing.T) {
	n := labNet()
	before := time.Now().Add(-time.Second)
	got, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if leaks := n.leaked(); len(leaks) != 0 {
		t.Fatalf("requests left the fixture: %v", leaks)
	}
	for _, want := range []string{
		"https://seed.example/", "https://seed.example/people", "https://seed.example/people/dana",
		"https://seed.example/people/sam", "https://lab.example/imaging", "https://code.example/dreyes",
	} {
		if !n.requested(want) {
			t.Errorf("%s was not fetched; requests: %v", want, n.requests())
		}
	}
	if n.requested("https://code.example/dreyes/deeper") {
		t.Errorf("depth-4 page was fetched: %v", n.requests())
	}
	// breadth-first: the seed's own links come before the lab page's
	pages := n.pages()
	idx := func(u string) int {
		for i, p := range pages {
			if p == u {
				return i
			}
		}
		return -1
	}
	if !(idx("https://seed.example/") < idx("https://seed.example/people") &&
		idx("https://seed.example/people") < idx("https://lab.example/imaging") &&
		idx("https://lab.example/imaging") < idx("https://code.example/dreyes")) {
		t.Errorf("not breadth-first: %v", pages)
	}

	names := map[string]bool{}
	for _, d := range got {
		names[d.Name] = true
	}
	for _, want := range []string{"Dana Reyes", "Sam Okafor", "Priya Natarajan", "Marcus van der Berg"} {
		if !names[want] {
			t.Errorf("no draft for %s; got %v", want, names)
		}
	}
	for _, bad := range []string{"Read More", "Imaging Lab", "Example Imaging Group", "People", "Hidden Person", "Too Deep"} {
		if names[bad] {
			t.Errorf("a non-person became a draft: %q", bad)
		}
	}

	// the people-page card, discovered from the seed
	d := draftByName(got, "Dana Reyes")
	if d == nil {
		t.Fatal("no Dana Reyes draft")
	}
	if d.SourceID != "web" || d.Role != "role/mri-engineer" {
		t.Errorf("draft identity: %+v", d)
	}
	if d.Title != "Postdoctoral Fellow" {
		t.Errorf("title=%q", d.Title)
	}
	if len(d.Evidence) != 1 {
		t.Fatalf("evidence=%+v", d.Evidence)
	}
	ev := d.Evidence[0]
	if ev.URLOrFile != "https://seed.example/people" || ev.Kind != EvidencePage || ev.Trust != TrustLow || ev.SourceID != "web" {
		t.Errorf("evidence: %+v", ev)
	}
	if ev.RetrievedAt.Before(before) || ev.RetrievedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("retrievedAt=%v", ev.RetrievedAt)
	}
	if !strings.Contains(ev.Snippet, "Dana Reyes — Postdoctoral Fellow, MRI reconstruction") {
		t.Errorf("snippet is not verbatim: %q", ev.Snippet)
	}
	if !strings.Contains(d.Note, "found on https://seed.example/people") ||
		!strings.Contains(d.Note, "discovered from https://seed.example/") || !strings.Contains(d.Note, "depth 1") {
		t.Errorf("provenance note: %q", d.Note)
	}
	if len(d.Links) != 1 || d.Links[0] != "https://seed.example/people/dana" {
		t.Errorf("links=%v", d.Links)
	}

	// the <br>-separated card
	if s := draftByName(got, "Sam Okafor"); s == nil || s.Title != "PhD Student" || s.Org != "Boston University" {
		t.Errorf("Sam card: %+v", s)
	}

	// the cross-domain lab card, discovered from the people page, with its
	// GitHub link kept
	p := draftByName(got, "Priya Natarajan")
	if p == nil {
		t.Fatal("no Priya draft")
	}
	if p.Title != "Research Scientist" || p.Org != "Imaging Lab" {
		t.Errorf("Priya cues: title=%q org=%q", p.Title, p.Org)
	}
	if p.Evidence[0].URLOrFile != "https://lab.example/imaging" ||
		!strings.Contains(p.Note, "discovered from https://seed.example/people") || !strings.Contains(p.Note, "depth 2") {
		t.Errorf("Priya provenance: ev=%s note=%q", p.Evidence[0].URLOrFile, p.Note)
	}
	if len(p.Links) != 1 || p.Links[0] != "https://code.example/dreyes" {
		t.Errorf("Priya links=%v", p.Links)
	}

	// the depth-3 person page: its own heading names the person, so the
	// page is theirs and is the link; it is a second draft for the same
	// name because it is a second citation
	var deep *CandidateDraft
	for i := range got {
		if got[i].Name == "Dana Reyes" && got[i].Evidence[0].URLOrFile == "https://code.example/dreyes" {
			deep = &got[i]
		}
	}
	if deep == nil {
		t.Fatalf("no draft from the depth-3 page: %+v", got)
	}
	if !strings.Contains(deep.Note, "depth 3") || !strings.Contains(deep.Note, "discovered from https://lab.example/imaging") {
		t.Errorf("depth-3 provenance: %q", deep.Note)
	}
	if len(deep.Links) == 0 || deep.Links[0] != "https://code.example/dreyes" {
		t.Errorf("depth-3 links=%v", deep.Links)
	}
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Errorf("%s: edges emitted in 3b.6: %+v", d.Name, d.Edges)
		}
		if d.ExternalID == "" || !strings.HasPrefix(d.ExternalID, d.Evidence[0].URLOrFile+"#") {
			t.Errorf("%s: externalId=%q", d.Name, d.ExternalID)
		}
	}
}

// D16 — depth is a ceiling of 3 and the scope may lower it. depth=1 fetches
// the seed and its links, nothing they link to; depth=0 is the seed alone;
// depth=9 is clamped to 3.
func TestWebRespectsDepth(t *testing.T) {
	n := labNet()
	if _, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", map[string]string{"depth": "1"})); err != nil {
		t.Fatal(err)
	}
	if !n.requested("https://seed.example/people") {
		t.Errorf("depth-1 page not fetched: %v", n.pages())
	}
	for _, deny := range []string{"https://seed.example/people/dana", "https://lab.example/imaging", "https://code.example/dreyes"} {
		if n.requested(deny) {
			t.Errorf("depth>1 page fetched with depth=1: %s", deny)
		}
	}

	n = labNet()
	if _, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", map[string]string{"depth": "0"})); err != nil {
		t.Fatal(err)
	}
	if pages := n.pages(); len(pages) != 1 || pages[0] != "https://seed.example/" {
		t.Errorf("depth=0 pages=%v", pages)
	}

	n = labNet()
	if _, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", map[string]string{"depth": "9"})); err != nil {
		t.Fatal(err)
	}
	if !n.requested("https://code.example/dreyes") || n.requested("https://code.example/dreyes/deeper") {
		t.Errorf("depth=9 not clamped to 3: %v", n.pages())
	}
}

// D16 — the page budget: default 25, configurable, hard cap 50, counted in
// HTML fetches (a robots.txt fetch is not a page).
func TestWebRespectsPageCap(t *testing.T) {
	// a seed that links to 80 relevant pages, each naming one person
	pages := map[string]string{}
	var links []string
	for i := 0; i < 80; i++ {
		p := "/p" + strings.Repeat("x", i%3) + "/" + strings.ToLower(string(rune('a'+i%26))) + strings.Repeat("y", i/26)
		links = append(links, `<a href="`+p+`">page</a>`)
		// a real-shaped name: "Person Name" now reads as a form template, which
		// is the point of TestWebPersonNameRefusesFormTemplates
		pages[p] = `<html><body><h1>Dana Reyes</h1><p>MRI Engineer</p></body></html>`
	}
	pages["/"] = `<html><head><title>mri index</title></head><body>` + strings.Join(links, " ") + `</body></html>`
	fresh := func() *webNet { return newWebNet().site("big.example", pages) }

	n := fresh()
	if _, err := n.adapter().Search(context.Background(), webScope("https://big.example/", map[string]string{"max_pages": "3"})); err != nil {
		t.Fatal(err)
	}
	if got := n.pages(); len(got) != 3 {
		t.Errorf("max_pages=3 fetched %d: %v", len(got), got)
	}

	n = fresh()
	if _, err := n.adapter().Search(context.Background(), Scope{Query: "mri", Max: 100, Fields: map[string]string{"seed_url": "https://big.example/"}}); err != nil {
		t.Fatal(err)
	}
	if got := n.pages(); len(got) != webDefaultPages {
		t.Errorf("default fetched %d want %d", len(got), webDefaultPages)
	}

	n = fresh()
	if _, err := n.adapter().Search(context.Background(), Scope{Query: "mri", Max: 100, Fields: map[string]string{"seed_url": "https://big.example/", "max_pages": "999"}}); err != nil {
		t.Fatal(err)
	}
	if got := n.pages(); len(got) != webMaxPages {
		t.Errorf("max_pages=999 fetched %d want cap %d", len(got), webMaxPages)
	}

	// the draft cap stops the traversal too: Max 2 means two drafts and no
	// page fetched past the one that filled them
	n = fresh()
	got, err := n.adapter().Search(context.Background(), Scope{Query: "mri", Max: 2, Fields: map[string]string{"seed_url": "https://big.example/", "max_pages": "50"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(n.pages()) != 3 {
		t.Errorf("Max=2: drafts=%d pages=%d", len(got), len(n.pages()))
	}

	if _, err := n.adapter().Search(context.Background(), webScope("https://big.example/", map[string]string{"max_pages": "lots"})); err == nil || !strings.Contains(err.Error(), "max_pages") {
		t.Errorf("non-numeric max_pages: err=%v", err)
	}
}

// D16 — LinkedIn and the social graphs are never fetched: not as a seed,
// not as a link at any depth, not as a redirect target.
func TestWebBlocksSocialHosts(t *testing.T) {
	n := labNet()
	if _, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", nil)); err != nil {
		t.Fatal(err)
	}
	for _, r := range n.requests() {
		for _, h := range []string{"linkedin.com", "facebook.com", "x.com", "twitter.com", "instagram.com"} {
			if strings.Contains(r, h) {
				t.Errorf("social host requested: %s", r)
			}
		}
	}
	if leaks := n.leaked(); len(leaks) != 0 {
		t.Errorf("leaked: %v", leaks)
	}

	for _, seed := range []string{
		"https://www.linkedin.com/in/someone", "https://linkedin.com/company/x", "https://facebook.com/x",
		"https://x.com/someone", "https://twitter.com/someone", "https://sub.instagram.com/p/1",
	} {
		n := labNet()
		_, err := n.adapter().Search(context.Background(), webScope(seed, nil))
		if err == nil || !strings.Contains(err.Error(), "seed_url refused") {
			t.Errorf("seed %s: err=%v", seed, err)
		}
		if len(n.requests()) != 0 {
			t.Errorf("seed %s: a request was made: %v", seed, n.requests())
		}
	}

	// a redirect into LinkedIn is refused mid-flight
	n = newWebNet().site("r.example", map[string]string{
		"/":   `<html><head><title>mri</title></head><body><a href="/out">out</a><a href="/ok">ok</a></body></html>`,
		"/ok": `<html><body><h1>Real Person</h1><p>MRI Scientist</p></body></html>`,
	})
	n.hosts["r.example"].(*http.ServeMux).HandleFunc("/out", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.linkedin.com/in/someone", http.StatusFound)
	})
	got, err := n.adapter().Search(context.Background(), webScope("https://r.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if leaks := n.leaked(); len(leaks) != 0 {
		t.Errorf("redirect reached LinkedIn: %v", leaks)
	}
	if len(got) != 1 || got[0].Name != "Real Person" {
		t.Errorf("drafts=%+v", got)
	}
}

// D16 — login / session / auth URLs are skipped by path segment and by
// query key, and a seed that is one is refused.
func TestWebSkipsLoginAndSessionURLs(t *testing.T) {
	n := labNet()
	if _, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", nil)); err != nil {
		t.Fatal(err)
	}
	for _, deny := range []string{"https://seed.example/login", "https://seed.example/dashboard?session=abc123", "https://seed.example/wp-login.php"} {
		if n.requested(deny) {
			t.Errorf("auth URL fetched: %s", deny)
		}
	}
	if n.requested("https://seed.example/brochure.pdf") {
		t.Error("a PDF spent a page")
	}
	for _, seed := range []string{
		"https://seed.example/login", "https://seed.example/signin?next=/", "https://seed.example/people?token=abc",
		"https://seed.example/auth/callback", "https://user:pw@seed.example/", "ftp://seed.example/", "seed.example/people", "",
	} {
		if _, err := n.adapter().Search(context.Background(), webScope(seed, nil)); err == nil {
			t.Errorf("seed %q accepted", seed)
		}
	}
	// the policy helper itself: authors is not auth
	for raw, want := range map[string]bool{
		"https://a.example/authors/dana": true, "https://a.example/auth": false, "https://a.example/Login.html": false,
		"https://a.example/people?page=2": true, "https://a.example/people?SID=1": false, "https://a.example/accounts/x": false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := webRefuse(u) == ""; got != want {
			t.Errorf("webRefuse(%s): allowed=%v want %v", raw, got, want)
		}
	}
}

// D16 — robots.txt is fetched once per host and a `User-agent: *` Disallow
// is honoured; a host without one is traversed normally.
func TestWebHonoursRobots(t *testing.T) {
	n := labNet()
	got, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if n.requested("https://lab.example/private/roster") {
		t.Error("robots-disallowed path was fetched")
	}
	if draftByName(got, "Hidden Person") != nil {
		t.Error("a draft came from a disallowed page")
	}
	robots := 0
	for _, r := range n.requests() {
		if r == "https://lab.example/robots.txt" {
			robots++
		}
	}
	if robots != 1 {
		t.Errorf("lab.example/robots.txt fetched %d times", robots)
	}
	// seed.example has no robots.txt (404) — asked once, then traversed
	if !n.requested("https://seed.example/robots.txt") || !n.requested("https://seed.example/people") {
		t.Errorf("missing robots.txt should allow: %v", n.requests())
	}

	// the parser: groups, wildcards, anchors, comments, our own UA
	r := parseRobots("# hi\nUser-agent: Googlebot\nDisallow: /g\n\nUser-agent: other\nUser-agent: *\nDisallow: /private/\nDisallow: /*.pdf$\nDisallow: /tmp*\nAllow: /private/ok\n\nUser-agent: manifest-aion-recruiting\nDisallow: /noscout\n")
	for p, want := range map[string]bool{
		"/": true, "/g": true, "/private/": false, "/private/x": false, "/x/y.pdf": false, "/x.pdfz": true,
		"/tmpfile": false, "/noscout/x": false, "/people": true,
	} {
		if r.allows(p) != want {
			t.Errorf("allows(%q)=%v want %v", p, !want, want)
		}
	}
}

// D15 — a published address or phone reaches no candidate field and is
// not even quoted; links never carry mailto:.
func TestWebNeverEmitsContactDetails(t *testing.T) {
	n := labNet()
	got, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no drafts")
	}
	for _, d := range got {
		if len(d.Contact) != 0 {
			t.Errorf("%s: contact set: %+v", d.Name, d.Contact)
		}
		texts := append([]string{d.Note, d.Title, d.Org, d.Location, d.ExternalID}, d.Links...)
		for _, ev := range d.Evidence {
			texts = append(texts, ev.Snippet, ev.URLOrFile)
			if ev.Kind == EvidenceContactPublished {
				t.Errorf("%s: contact evidence emitted: %+v", d.Name, ev)
			}
		}
		for _, s := range texts {
			if containsAddress(s) || strings.Contains(s, "mailto:") || strings.Contains(s, "example.test") ||
				strings.Contains(s, "555-0142") || strings.Contains(s, "617") {
				t.Errorf("%s: contact detail reached the draft: %q", d.Name, s)
			}
		}
	}
	// the person page with the address still yielded its draft, minus the
	// address line
	var own *CandidateDraft
	for i := range got {
		if got[i].Evidence[0].URLOrFile == "https://seed.example/people/dana" {
			own = &got[i]
		}
	}
	if own == nil || own.Name != "Dana Reyes" || own.Title != "Postdoctoral Fellow" || own.Org != "Example Imaging Group" {
		t.Errorf("person page draft: %+v", own)
	}
}

// The scope is refused without a query or a seed_url, and no request is
// made either way.
func TestWebRefusesMissingSeedOrQuery(t *testing.T) {
	n := labNet()
	for name, s := range map[string]Scope{
		"no fields":   {Query: "mri", Max: 25},
		"empty seed":  {Query: "mri", Max: 25, Fields: map[string]string{"seed_url": "  "}},
		"no query":    {Query: "", Max: 25, Fields: map[string]string{"seed_url": "https://seed.example/"}},
		"blank query": {Query: " \" ' ", Max: 25, Fields: map[string]string{"seed_url": "https://seed.example/"}},
	} {
		if _, err := n.adapter().Search(context.Background(), s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(n.requests()) != 0 {
		t.Errorf("requests made: %v", n.requests())
	}
	fields := Web{}.Scope()
	keys := map[string]ScopeField{}
	for _, f := range fields {
		keys[f.Key] = f
	}
	if !keys["seed_url"].Required || !keys["query"].Required || keys["max_pages"].Key == "" || keys["depth"].Key == "" {
		t.Errorf("scope fields: %+v", fields)
	}
	if (Web{}).ID() != "web" || (Web{}).Kind() != KindWeb {
		t.Error("identity")
	}
}

// A 500, a transport failure, a non-HTML body and malformed HTML each cost
// a page and nothing more; the run finishes with what the good pages said.
func TestWebSurvivesBadPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>mri</title></head><body>
<a href="/boom">boom</a> <a href="/json">json</a> <a href="/broken">broken</a>
<a href="https://gone.example/x">gone</a> <a href="/good">good</a></body></html>`))
	})
	mux.HandleFunc("/boom", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 500) })
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Fake Person","title":"Professor"}`))
	})
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<div><h1>Broken Page<p>Professor<ul><li><a href="/good`))
	})
	mux.HandleFunc("/good", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>Good Person</h1><p>MRI Scientist</p></body></html>`))
	})
	n := newWebNet().handler("bad.example", mux)
	got, err := n.adapter().Search(context.Background(), webScope("https://bad.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Good Person" {
		t.Errorf("drafts=%+v", got)
	}
	if !n.requested("https://bad.example/boom") || !n.requested("https://bad.example/good") {
		t.Errorf("requests: %v", n.requests())
	}
	// the unreachable cross-domain host is asked for robots.txt and then
	// the page — a transport error each time, not a crash
	if leaks := n.leaked(); len(leaks) != 2 || leaks[0] != "https://gone.example/robots.txt" || leaks[1] != "https://gone.example/x" {
		t.Errorf("leaks=%v", leaks)
	}

	// a seed that 500s is not an error, just an empty run
	n = newWebNet().handler("down.example", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 503)
	}))
	got, err = n.adapter().Search(context.Background(), webScope("https://down.example/", nil))
	if err != nil || len(got) != 0 {
		t.Errorf("seed 503: drafts=%d err=%v", len(got), err)
	}

	// a cancelled context stops the run cleanly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := labNet().adapter().Search(ctx, webScope("https://seed.example/", nil)); err == nil {
		t.Error("cancelled context: no error")
	}
}

// An irrelevant page (no query term, no people/lab signal) yields no
// drafts and its cross-domain links are not followed; same-domain links
// still are.
func TestWebRelevanceGatesDraftsAndLinkouts(t *testing.T) {
	n := newWebNet().site("dull.example", map[string]string{
		"/": `<html><head><title>Widgets</title></head><body><h1>Widgets</h1><p>Buy widgets.</p>
<h3>Some Person</h3><p>Sales Manager</p>
<a href="https://elsewhere.example/">partner</a> <a href="/about">about</a></body></html>`,
		"/about": `<html><head><title>About</title></head><body><h1>Alex Kim</h1><p>MRI physicist</p></body></html>`,
	}).site("elsewhere.example", map[string]string{"/": `<html><body>x</body></html>`})
	got, err := n.adapter().Search(context.Background(), webScope("https://dull.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if n.requested("https://elsewhere.example/") {
		t.Error("cross-domain link followed from an irrelevant page")
	}
	if draftByName(got, "Some Person") != nil {
		t.Error("draft from an irrelevant page")
	}
	if a := draftByName(got, "Alex Kim"); a == nil || a.Title != "MRI physicist" {
		t.Errorf("same-domain relevant page: %+v", got)
	}
}

// The name shape: what is and is not a person-shaped line.
func TestWebPersonName(t *testing.T) {
	for s, want := range map[string]bool{
		"Dana Reyes": true, "Marcus van der Berg": true, "J. R. Okafor": true, "Mary-Jane O'Neil": true,
		"Reyes": false, "Dana": false, "Imaging Lab": false, "Boston University": false, "Read More": false,
		"Home About People Contact": false, "dana reyes": false, "DANA REYES": false, "Dana Reyes 2024": false,
		"Principal Investigator": false, "Skip to main content": false, "Our Team": false, "": false,
		"A B C D E": false, "Postdoctoral Fellow": false, "PhD Student": false, "Facebook X Twitter Instagram": false,
		"X Y": false, "Sales Manager": false, "Alex Kim": true,
		// chrome, service and section labels are never names
		"Imaging Services": false, "Internal Resources": false, "Research Domains": false,
		"Loading Comments...": false, "More About Us": false, "Administrative Staff": false,
		"Clinical Services": false, "Method & Technology Development": false, "Neuroimaging Core": false,
		"Learning Resources": false, "Parking Information": false, "Housing Options": false,
		"Education Programs": false, "Core Facilities": false, "News Events": false,
		// generic-noun suffixes
		"Radiology Department": false, "Genomics Platform": false, "Computing Software": false,
		// the initials-plus-surname shape may close on a role word; a bare
		// role pair may not
		"Jane Q. Researcher": true, "Q. Researcher": false, "Jane Researcher": false,
		"Jane Q. Postdoctoral Fellow": false, "Ying Chen": true, "Sion Davies": true,
		// a long "-ing" first token reads as a gerund ("Imaging", "Housing");
		// the rare given name of that shape is the price, surnames are not
		"Fleming Okafor": false, "Mary Fleming": true, "Irving Lee": true,
	} {
		if webPersonName(s) != want {
			t.Errorf("webPersonName(%q)=%v want %v", s, !want, want)
		}
	}
}

// Enrich and GraphEdges change nothing and make no request.
func TestWebEnrichAndEdgesAreInert(t *testing.T) {
	n := labNet()
	d := CandidateDraft{Name: "Dana Reyes", Evidence: []Evidence{{URLOrFile: "https://seed.example/people"}}}
	got, err := n.adapter().Enrich(context.Background(), d)
	if err != nil || got.Name != d.Name || len(got.Evidence) != 1 {
		t.Errorf("enrich: %+v %v", got, err)
	}
	edges, err := n.adapter().GraphEdges(context.Background(), d)
	if err != nil || len(edges) != 0 {
		t.Errorf("edges: %+v %v", edges, err)
	}
	if len(n.requests()) != 0 {
		t.Errorf("requests: %v", n.requests())
	}
}

// Regression for the Martinos live smoke: a WordPress page whose menus,
// sidebar and section headings read as capitalised pairs ("Imaging
// Services", "Internal Resources", "Research Domains") yields no draft at
// all. The labels are refused three ways — by chrome region, by vocabulary
// and by the title line being a bare label — and a copy of the menu placed
// in a plain <ul> outside any chrome region is still refused.
func TestWebRefusesChromeAndServiceLabels(t *testing.T) {
	menu := `<ul class="primary-menu-ul nav-ul menu-desktop">
<li class="menu-item"><div class="wrap"><a href="/impact/">Impact</a></div></li>
<li class="menu-item"><div class="wrap"><a href="/partnerships/">Partnerships</a></div></li>
<li class="menu-item"><div class="wrap"><a href="/imaging-services/">Imaging Services</a></div></li>
<li class="menu-item"><div class="wrap"><a href="#"><span>More About Us</span></a></div>
<ul class="sub-menu">
<li class="menu-item"><div class="wrap"><a href="/center-leadership/">Leadership</a></div></li>
<li class="menu-item"><div class="wrap"><a href="/faculty/">Faculty</a></div></li>
<li class="menu-item"><div class="wrap"><a href="/administrative-staff/">Administrative Staff</a></div></li>
<li class="menu-item"><div class="wrap"><a href="/research-domains/">Research</a></div></li>
<li class="menu-item"><div class="wrap"><a href="/conferences/">Conferences</a></div></li>
</ul></li>
<li class="menu-item"><div class="wrap"><a href="/internal-resources/">Internal Resources</a></div></li>
</ul>`
	labels := `<ul>
<li><a href="/cores/">Cores</a></li><li><a href="/facilities/">Facilities</a></li>
<li><a href="/education/">Education</a></li><li><a href="/publications/">Publications</a></li>
<li><a href="/resources/">Resources</a></li><li><a href="/services/">Services</a></li>
<li><a href="/contact/">Contact</a></li><li><a href="/news/">News</a></li>
<li><a href="/about-us/">About Us</a></li><li><a href="/research-domains/">Research Domains</a></li>
<li><a href="/imaging-services/">Imaging Services</a></li><li><a href="/faculty/">Faculty</a></li>
<li><a href="/internal-resources/">Internal Resources</a></li><li><a href="/clinical-services/">Clinical Services</a></li>
</ul>`
	body := `<html><head><title>Research Domains – Martinos Center for Biomedical Imaging</title></head><body>
<header><div class="hfg-slot right"><div class="builder-item has-nav"><div class="nv-nav-wrap">
<div role="navigation" class="nav-menu-primary" aria-label="Primary Menu">` + menu + `</div></div></div></div></header>
<main>
<h1>Research Domains</h1>
<p>Research in the Martinos Center is organized around two complementary domain categories that together drive innovation in biomedical imaging and its real-world impact.</p>
<h5>Method &amp; Technology Development</h5>
<p>Method &amp; Technology Development focuses on creating the tools that make new discoveries possible. Investigators build magnetic resonance imaging hardware and pulse sequences.</p>
<h5>Applications</h5>
<p>Applications use those tools in the lab and the clinic.</p>
<h2>Explore the Center</h2>
` + labels + `
<div class="sidebar"><h4>Quick Links</h4>` + labels + `</div>
</main>
<footer><div class="footer-menu">` + menu + `</div><h2>Loading Comments...</h2><p>Principal Investigator</p></footer>
</body></html>`
	n := newWebNet().site("martinos.example", map[string]string{"/research/": body})
	got, err := n.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "magnetic resonance imaging lab researcher", Max: 3,
		Fields: map[string]string{"seed_url": "https://martinos.example/research/", "max_pages": "5", "depth": "0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("drafts from a page that names nobody: %+v", got)
	}
	for _, d := range got {
		for _, bad := range []string{"Imaging Services", "Internal Resources", "Research Domains", "Research", "Faculty", "Administrative Staff", "Loading Comments...", "More About Us", "Cores", "Facilities", "About Us"} {
			if d.Name == bad {
				t.Errorf("label became a draft: %q", bad)
			}
		}
	}
	// the menu links were still discovered: refusing chrome as a name
	// source does not shrink the traversal
	if !n.requested("https://martinos.example/research/") {
		t.Errorf("seed not fetched: %v", n.requests())
	}
	n = newWebNet().site("martinos.example", map[string]string{"/research/": body})
	if _, err := n.adapter().Search(context.Background(), webScope("https://martinos.example/research/", map[string]string{"depth": "1", "max_pages": "3"})); err != nil {
		t.Fatal(err)
	}
	if !n.requested("https://martinos.example/impact/") {
		t.Errorf("chrome links not traversed: %v", n.pages())
	}

	// the same labels, one per line, followed by a line with a role cue that
	// is itself a bare label ("Faculty", "Staff", "Research"): still nothing
	n = newWebNet().site("labels.example", map[string]string{"/": `<html><head><title>mri lab</title></head><body>
<div><p>Imaging Services</p><p>Faculty</p></div>
<div><p>Internal Resources</p><p>Research Domains</p></div>
<div><p>Clinical Services</p><p>Staff</p></div>
<div><p>Core Facilities</p><p>Principal Investigator</p></div>
<div><p>Education Programs</p><p>Research Fellow Resources</p></div>
</body></html>`})
	got, err = n.adapter().Search(context.Background(), webScope("https://labels.example/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("drafts from labels: %+v", got)
	}

	// the extractor marks chrome lines and whole-link lines
	p := &webPage{url: mustURL(t, "https://x.example/")}
	doc, err := html.Parse(strings.NewReader(`<html><body><nav><ul><li><a href="/a">Nav Item</a></li></ul></nav>
<div class="menu-item"><a href="/b">Menu Item</a></div>
<ul id="sidebar-x"><li>Side Item</li></ul>
<div><a href="/c">Link Item</a></div>
<div><a href="/d">Link</a> and prose</div>
<header><h1>Page Title</h1></header></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	p.extract(doc)
	want := map[string][2]bool{ // chrome, allLink
		"Nav Item": {true, true}, "Menu Item": {true, true}, "Side Item": {true, false},
		"Link Item": {false, true}, "Link and prose": {false, false}, "Page Title": {false, false},
	}
	for _, l := range p.lines {
		w, ok := want[l.text]
		if !ok {
			continue
		}
		delete(want, l.text)
		if l.chrome != w[0] || l.allLink != w[1] {
			t.Errorf("%q: chrome=%v allLink=%v want %v %v", l.text, l.chrome, l.allLink, w[0], w[1])
		}
	}
	if len(want) != 0 {
		t.Errorf("lines not extracted: %v", want)
	}
}

// Real people on clear cards are still extracted: a heading-plus-role card,
// a card wrapped whole in one anchor, an inline "Name — Role" line, and a
// card whose final row is CV / website links. A name with only an
// organisation under it, or only a bare "Faculty" label, is not enough.
func TestWebExtractsClearPersonCards(t *testing.T) {
	n := newWebNet().site("lab.example", map[string]string{"/people": `<html><head><title>People · MRI Lab</title></head><body>
<nav role="navigation"><a href="/">Home</a> <a href="/people">People</a> <a href="/imaging-services">Imaging Services</a></nav>
<h1>Our People</h1>
<div class="person-card">
  <h3>Jane Q. Researcher</h3>
  <p>Principal Investigator, MRI Lab</p>
  <p>Quantitative MRI and tissue imaging.</p>
</div>
<a class="card" href="/people/tomas"><div><h3>Tomás Ferreira-Lima</h3><p>Assistant Professor of Radiology</p><p>Massachusetts General Hospital</p></div></a>
<div class="person-card"><h3>Priya Natarajan</h3><p>Research Scientist</p><p>Imaging Lab</p><p><a href="/cv/priya.pdf">CV</a> <a href="https://priya.example/">Website</a></p></div>
<p><a href="/people/dana">Dana Reyes</a> — Postdoctoral Fellow, MRI reconstruction</p>
<div class="person-card"><h3>Alex Kim</h3><p>Boston University</p></div>
<div class="person-card"><h3>Sam Okafor</h3><p>Faculty</p></div>
<div class="person-card"><h3>Lee Park</h3><p><a href="/imaging-services">Imaging Services</a></p><p><a href="/professor-info">Professor</a></p></div>
<div class="person-card"><h3>Director of Imaging Services</h3><p>Professor</p></div>
<h3>Robin Achebe</h3><h4>Associate Professor</h4>
<footer><h3>Casey Morgan</h3><p>Web Developer</p></footer>
</body></html>`})
	got, err := n.adapter().Search(context.Background(), webScope("https://lab.example/people", map[string]string{"depth": "0"}))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]CandidateDraft{}
	for _, d := range got {
		names[d.Name] = d
	}
	if j, ok := names["Jane Q. Researcher"]; !ok {
		t.Errorf("person-card not extracted: %v", names)
	} else if j.Title != "Principal Investigator" || j.Org != "MRI Lab" {
		t.Errorf("Jane cues: title=%q org=%q", j.Title, j.Org)
	} else if !strings.Contains(j.Evidence[0].Snippet, "Jane Q. Researcher · Principal Investigator, MRI Lab · Quantitative MRI and tissue imaging.") {
		t.Errorf("Jane snippet: %q", j.Evidence[0].Snippet)
	}
	if tf, ok := names["Tomás Ferreira-Lima"]; !ok {
		t.Errorf("anchor-wrapped card not extracted: %v", names)
	} else if tf.Title != "Assistant Professor of Radiology" || tf.Org != "Massachusetts General Hospital" {
		t.Errorf("Tomás cues: title=%q org=%q", tf.Title, tf.Org)
	}
	if p, ok := names["Priya Natarajan"]; !ok {
		t.Errorf("card with link row not extracted: %v", names)
	} else if p.Title != "Research Scientist" || p.Org != "Imaging Lab" {
		t.Errorf("Priya cues: title=%q org=%q", p.Title, p.Org)
	} else if len(p.Links) != 2 || p.Links[0] != "https://lab.example/cv/priya.pdf" || p.Links[1] != "https://priya.example/" {
		t.Errorf("Priya links kept from the link row: %v", p.Links)
	}
	if d, ok := names["Dana Reyes"]; !ok || d.Title != "Postdoctoral Fellow" {
		t.Errorf("inline card: %+v", d)
	}
	if r, ok := names["Robin Achebe"]; !ok || r.Title != "Associate Professor" {
		t.Errorf("sub-heading role: %+v", r)
	}
	for _, none := range []string{"Alex Kim", "Sam Okafor", "Lee Park", "Director of Imaging Services", "Casey Morgan", "Imaging Services", "Our People", "Faculty"} {
		if _, ok := names[none]; ok {
			t.Errorf("%q became a draft: %+v", none, names[none])
		}
	}
	if len(got) != 5 {
		t.Errorf("drafts=%d: %v", len(got), names)
	}
	if webLabelish("Research Domains") != true || webLabelish("Faculty") != true || webLabelish("News & Events") != true ||
		webLabelish("Principal Investigator") != false || webLabelish("Director of Imaging Services") != false || webLabelish("Research Scientist") != false {
		t.Error("webLabelish")
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// The SSRF guard: localhost and loopback / private / link-local /
// unspecified IP literals are refused as a seed, never followed as a link
// (the transport never sees them), and — for a public name that resolves
// there — refused again at the socket by the default transport's dialer.
func TestWebRefusesLocalAndPrivateHosts(t *testing.T) {
	n := newWebNet()
	n.site("seed.example", map[string]string{
		"/": `<html><head><title>Example Imaging Group</title></head><body>
<h1>Example Imaging Group</h1><p>We build MRI imaging software.</p>
<a href="http://127.0.0.1:9/x">A</a> <a href="http://localhost/admin">B</a> <a href="http://10.0.0.5/">C</a>
<a href="http://[::1]:8080/">D</a> <a href="http://169.254.169.254/latest/meta-data/">E</a>
<a href="http://192.168.1.1/imaging">F</a> <a href="http://0.0.0.0/">G</a>
<a href="https://lab.example/imaging">Imaging Lab</a>
</body></html>`,
	})
	n.site("lab.example", map[string]string{"/imaging": `<html><body><h1>Imaging Lab</h1><p>mri</p></body></html>`})
	if _, err := n.adapter().Search(context.Background(), webScope("https://seed.example/", nil)); err != nil {
		t.Fatal(err)
	}
	for _, r := range n.requests() {
		if webPrivateHost(mustURL(t, r).Hostname()) {
			t.Errorf("a local/private URL reached the transport: %s", r)
		}
	}
	if !n.requested("https://lab.example/imaging") {
		t.Errorf("the public linkout was not followed: %v", n.requests())
	}

	// as a seed, each is refused before anything is fetched
	for _, seed := range []string{
		"http://127.0.0.1:9/x", "http://localhost:7777/", "http://a.localhost/", "http://10.1.2.3/", "http://172.16.0.1/",
		"http://192.168.1.1/", "http://169.254.169.254/", "http://[::1]/", "http://[fe80::1]/", "http://[fc00::1]/",
		"http://0.0.0.0/", "http://[::]/", "http://[::ffff:127.0.0.1]/",
	} {
		before := len(n.requests())
		_, err := n.adapter().Search(context.Background(), webScope(seed, nil))
		if err == nil || !strings.Contains(err.Error(), "local/private") {
			t.Errorf("seed %q: %v", seed, err)
		}
		if len(n.requests()) != before {
			t.Errorf("seed %q was fetched", seed)
		}
	}

	// the policy helper both ways: public literals and names stay allowed
	for raw, want := range map[string]bool{
		"https://93.184.216.34/": true, "https://example.org/": true, "https://[2606:2800:220:1:248:1893:25c8:1946]/": true,
		"https://172.15.0.1/": true, "https://172.32.0.1/": true, "https://localhost.example/": true,
		"http://127.0.0.1/": false, "http://LOCALHOST/": false, "http://[fe80::1%25eth0]/": false, "http://224.0.0.1/": false,
	} {
		if got := webRefuse(mustURL(t, raw)) == ""; got != want {
			t.Errorf("webRefuse(%s): allowed=%v want %v", raw, got, want)
		}
	}

	// the dialer's control sees the resolved address, so a public name that
	// resolves to a private address (DNS rebinding) is refused at the socket
	for addr, ok := range map[string]bool{
		"127.0.0.1:9": false, "[::1]:80": false, "10.0.0.1:80": false, "169.254.169.254:80": false, "0.0.0.0:80": false,
		"93.184.216.34:443": true, "[2606:2800:220:1:248:1893:25c8:1946]:443": true,
	} {
		if err := webDialControl("tcp", addr, nil); (err == nil) != ok {
			t.Errorf("webDialControl(%s): %v", addr, err)
		}
	}
	// and the zero-value adapter (no injected transport) carries it: a
	// loopback server is unreachable through Web{}.get even though get
	// itself runs no URL policy
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html></html>")) }))
	defer srv.Close()
	if _, _, _, err := (Web{Delay: -1}).get(context.Background(), srv.URL+"/", 1024); err == nil || !strings.Contains(err.Error(), "refused dial") {
		t.Errorf("loopback dial through the default transport: %v", err)
	}
}

// FORM TEMPLATES ARE NOT PEOPLE. A donation or application form ships its
// labels as page text, and every one of them is name-shaped: two capitalised
// words, letters only. They reached the queue as candidates to triage,
// carrying the same weight as a real professor on the same page (owner,
// 2026-09-05).
func TestWebPersonNameRefusesFormTemplates(t *testing.T) {
	for _, s := range []string{
		"Student's Name",
		"Student’s Name", // the curly apostrophe a CMS emits
		"Student's Anticipated Degree",
		"Your Name",
		"First Last",
		"Full Name",
		"Donor Name",
		"Middle Initial",
		"Example Name",
	} {
		if webPersonName(s) {
			t.Errorf("%q is a form label, not a person", s)
		}
	}
}

// The filter must stay narrow: real names that happen to brush a template
// word still pass.
func TestWebPersonNameKeepsRealPeople(t *testing.T) {
	for _, s := range []string{
		"Dana M. Reyes",
		"Kai Okonkwo",
		"Priya Natarajan",
		"Stéfan van der Walt",
		"J. Nathan Kutz",
		"Michael S. Avidan",
	} {
		if !webPersonName(s) {
			t.Errorf("%q is a person and must survive the template filter", s)
		}
	}
}
