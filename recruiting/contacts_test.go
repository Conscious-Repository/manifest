package recruiting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/recruiting/sources"
)

// contactsSite is an in-process lab site: a people page the crawl accepts
// (name lines followed by role lines) that also publishes addresses the
// crawl must not quote and the contact pass must bind by card; and a
// challenge body served under two URLs on two hosts.
type contactsSite struct{ hits map[string]int }

const contactsPeople = `<html><head><title>Example Lab · People</title></head><body>
<nav><a href="/">Home</a> <a href="mailto:lab@example.test">Contact</a></nav>
<main><h1>Lab Members</h1>
<div class="card"><h2>Ellie Berkland</h2><p>Undergraduate Research Assistant</p><p><a href="mailto:ellie@example.test">ellie@example.test</a></p></div>
<div class="card"><h2>Cory Berkland</h2><p>Professor</p><p>Email: cory@example.test</p></div>
<div class="card"><h2>Dana Reyes</h2><p>Postdoctoral Fellow</p></div>
<div class="card"><h2>Lu Xu</h2><p>Graduate Student</p><h2>Mack Yang</h2><p>Graduate Student</p><p><a href="mailto:shared@example.test">shared@example.test</a></p></div>
</main></body></html>`

const contactsChallenge = `<html><body><p>Just a moment — checking your browser before accessing the site.</p></body></html>`

func (s *contactsSite) RoundTrip(req *http.Request) (*http.Response, error) {
	s.hits[req.URL.String()]++
	body, status := "", http.StatusNotFound
	switch req.URL.Host + req.URL.Path {
	case "lab.example/robots.txt", "waf-a.example/robots.txt", "waf-b.example/robots.txt":
		body, status = "User-agent: *\nAllow: /\n", http.StatusOK
	case "lab.example/people/":
		body, status = contactsPeople, http.StatusOK
	case "waf-a.example/people-page/", "waf-b.example/allpeople/":
		body, status = contactsChallenge, http.StatusOK
	case "lab.example/":
		body, status = `<html><body><main><p>Welcome.</p><a href="/people/">People</a></main></body></html>`, http.StatusOK
	}
	if status != http.StatusOK {
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func contactsRunStore(t *testing.T) (*RunStore, *Store, string, *contactsSite) {
	t.Helper()
	site := &contactsSite{hits: map[string]int{}}
	rs, store, vault := testRunStore(t, nil)
	rs.Register(sources.Web{Client: http.Client{Transport: site}, Delay: -1})
	return rs, store, vault, site
}

func contactRows(d Draft) []string {
	var out []string
	for _, e := range d.Draft.Evidence {
		if e.Kind == sources.EvidenceContactPublished {
			out = append(out, e.Snippet)
		}
	}
	return out
}

// The pass files a published address on the draft it was printed beside —
// as evidence, verbatim — and touches nothing else: no status, no count,
// no record, no tombstone. A second identical pass changes no byte.
func TestContactPassFilesEvidenceAndNothingElse(t *testing.T) {
	rs, _, vault, _ := contactsRunStore(t)
	run := mustRun(t, rs, RunRequest{Source: "web", Query: "example", Fields: map[string]string{"seed_url": "https://lab.example/people/"}})
	if len(run.Drafts) != 5 {
		t.Fatalf("crawl queued %d drafts, want 5: %+v", len(run.Drafts), run.Drafts)
	}
	for _, d := range run.Drafts {
		for _, e := range d.Draft.Evidence {
			if strings.Contains(e.Snippet, "@") {
				t.Fatalf("the crawl quoted an address (D15): %+v", e)
			}
		}
	}
	before := snapshot(t, vault)
	statuses, _ := json.Marshal(run.Drafts)

	pass, err := rs.NewContactPass(true)
	if err != nil {
		t.Fatal(err)
	}
	now := testNow.Add(time.Hour)
	got, res, err := pass.Run(context.Background(), run.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	assertIdentical(t, "vault after the contact pass", before, snapshot(t, vault))
	if res.Seen != 5 || res.Resolved != 2 || res.Unset != 3 || res.Added != 2 || res.Unavailable {
		t.Errorf("counts: %+v", res)
	}
	if len(res.Pages) != 1 || res.Pages[0].Status != ContactsPageRead || res.Pages[0].Bound != 2 || res.Pages[0].URL != "https://lab.example/people/" {
		t.Errorf("pages: %+v", res.Pages)
	}
	want := map[string]string{"Ellie Berkland": "Ellie Berkland · ellie@example.test", "Cory Berkland": "Cory Berkland · cory@example.test"}
	for _, d := range got.Drafts {
		rows := contactRows(d)
		if w := want[d.Draft.Name]; w != "" {
			if len(rows) != 1 || rows[0] != w {
				t.Errorf("%s rows %v want [%s]", d.Draft.Name, rows, w)
			}
			for _, e := range d.Draft.Evidence {
				if e.Kind == sources.EvidenceContactPublished && (e.URLOrFile != "https://lab.example/people/" || e.SourceID != "web" || e.Trust != sources.TrustMedium || !e.RetrievedAt.Equal(now.UTC())) {
					t.Errorf("%s row: %+v", d.Draft.Name, e)
				}
			}
		} else if len(rows) != 0 {
			t.Errorf("%s got %v — a shared card or a card with no address must stay blank", d.Draft.Name, rows)
		}
		if d.Status != DraftNew || !d.DecidedAt.IsZero() || d.CandidateID != "" || len(d.Draft.Contact) != 0 {
			t.Errorf("%s changed beyond evidence: %+v", d.Draft.Name, d)
		}
		for _, l := range d.Draft.Links {
			if strings.HasPrefix(strings.ToLower(l), "mailto:") {
				t.Errorf("%s: mailto link on the draft: %s", d.Draft.Name, l)
			}
		}
	}
	cw, _ := json.Marshal(run.Counts)
	cg, _ := json.Marshal(got.Counts)
	if string(cw) != string(cg) || got.Contacts == nil || got.Contacts.Added != 2 {
		t.Errorf("run state: counts %+v → %+v, contacts %+v", run.Counts, got.Counts, got.Contacts)
	}
	// statuses and decisions byte-identical, evidence aside
	var was, is []Draft
	_ = json.Unmarshal(statuses, &was)
	is = got.Drafts
	for i := range was {
		was[i].Draft.Evidence, is[i].Draft.Evidence, was[i].Paths, is[i].Paths = nil, nil, nil, nil
		a, _ := json.Marshal(was[i])
		b, _ := json.Marshal(is[i])
		if string(a) != string(b) {
			t.Errorf("draft %s changed beyond evidence:\n%s\n%s", was[i].ID, a, b)
		}
	}

	// determinism: the same pass again adds nothing and rewrites no byte
	first, _ := os.ReadFile(filepath.Join(rs.Root(), run.ID, "drafts.json"))
	again, res2, err := pass.Run(context.Background(), run.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(rs.Root(), run.ID, "drafts.json"))
	if string(first) != string(second) || res2.Added != 0 || res2.Resolved != 2 || len(contactRows(again.Drafts[0])) != 1 {
		t.Errorf("second pass was not a no-op: %+v\n%s", res2, firstDiff(string(first), string(second)))
	}
	// persisted: a fresh load carries the rows and the pass record
	loaded, err := rs.Get(run.ID)
	if err != nil || loaded.Contacts == nil || len(contactRows(loaded.Drafts[0])) != 1 {
		t.Fatalf("not persisted: %+v %v", loaded.Contacts, err)
	}
}

// A challenge body served under two URLs is recorded unavailable and
// nothing is taken from it — by the roster signal on the first, by the
// identical-body signal on the second.
func TestContactPassRecordsBoilerplateUnavailable(t *testing.T) {
	rs, _, vault, site := contactsRunStore(t)
	a := &fakeAdapter{id: "web", drafts: []sources.CandidateDraft{
		webFoundDraft("Kyle Apley", "https://waf-a.example/people-page/"),
		webFoundDraft("Vikas Kaushik", "https://waf-a.example/people-page/"),
	}}
	rs.Register(a)
	runA := mustRun(t, rs, RunRequest{Source: "web", Query: "example", Fields: map[string]string{"seed_url": "https://waf-a.example/people-page/"}})
	a.drafts = []sources.CandidateDraft{webFoundDraft("Yimei Yue", "https://waf-b.example/allpeople/")}
	runB := mustRun(t, rs, RunRequest{Source: "web", Query: "example", Fields: map[string]string{"seed_url": "https://waf-b.example/allpeople/"}})
	// the crawl was faked; the pass reads through the real adapter
	rs.Register(sources.Web{Client: http.Client{Transport: site}, Delay: -1})
	before := snapshot(t, vault)

	pass, err := rs.NewContactPass(true)
	if err != nil {
		t.Fatal(err)
	}
	gotA, resA, err := pass.Run(context.Background(), runA.ID, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !resA.Unavailable || resA.Resolved != 0 || resA.Unset != 2 || len(resA.Pages) != 1 || resA.Pages[0].Status != ContactsPageUnavailable || !strings.Contains(resA.Pages[0].Why, "none of the queued names") {
		t.Errorf("run A: %+v", resA)
	}
	gotB, resB, err := pass.Run(context.Background(), runB.ID, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !resB.Unavailable || len(resB.Pages) != 1 || resB.Pages[0].Status != ContactsPageUnavailable || !strings.Contains(resB.Pages[0].Why, "identical body to https://waf-a.example/people-page/") {
		t.Errorf("run B: %+v", resB)
	}
	for _, run := range []Run{gotA, gotB} {
		if run.Contacts == nil || !run.Contacts.Unavailable {
			t.Errorf("run %s did not record the pass as unavailable: %+v", run.ID, run.Contacts)
		}
		for _, d := range run.Drafts {
			if len(contactRows(d)) != 0 || d.Status != DraftNew {
				t.Errorf("draft touched from a boilerplate page: %+v", d)
			}
		}
	}
	assertIdentical(t, "vault after unavailable passes", before, snapshot(t, vault))
}

// The boundaries of the pass: not a non-web run, not a page off the seed's
// host, not an unregistered adapter; a fetch error is a page error, not a
// failed pass.
func TestContactPassBounds(t *testing.T) {
	rs, _, _, site := contactsRunStore(t)
	other := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "x1")}}
	rs.Register(other)
	fake := mustRun(t, rs, RunRequest{Source: "fake", Query: "dana"})
	pass, err := rs.NewContactPass(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pass.Run(context.Background(), fake.ID, testNow); err == nil || !strings.Contains(err.Error(), "only a web run") {
		t.Errorf("non-web run accepted: %v", err)
	}
	web := &fakeAdapter{id: "web", drafts: []sources.CandidateDraft{
		webFoundDraft("Ellie Berkland", "https://lab.example/people/"),
		webFoundDraft("Piyoosh Sharma", "https://github.com/psharm37"),
		webFoundDraft("Dana Reyes", "https://lab.example/missing/"),
	}}
	rs.Register(web)
	run := mustRun(t, rs, RunRequest{Source: "web", Query: "example", Fields: map[string]string{"seed_url": "https://lab.example/people/"}})
	rs.Register(sources.Web{Client: http.Client{Transport: site}, Delay: -1})
	pass, _ = rs.NewContactPass(true)
	got, res, err := pass.Run(context.Background(), run.ID, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if site.hits["https://github.com/psharm37"] != 0 {
		t.Errorf("a page off the seed host was fetched")
	}
	if len(res.Pages) != 2 || res.Pages[0].Status != ContactsPageRead || res.Pages[1].Status != ContactsPageError || res.Unavailable {
		t.Errorf("pages: %+v", res.Pages)
	}
	if rows := contactRows(got.Drafts[0]); len(rows) != 1 || rows[0] != "Ellie Berkland · ellie@example.test" {
		t.Errorf("ellie: %v", rows)
	}
	if res.Seen != 3 || res.Resolved != 1 || res.Unset != 2 {
		t.Errorf("counts: %+v", res)
	}
	if ids := rs.WebRunIDs(); len(ids) != 1 || ids[0] != run.ID {
		t.Errorf("web run ids: %v", ids)
	}
	rs.adapters = map[string]sources.Adapter{}
	if _, err := rs.NewContactPass(true); err == nil {
		t.Error("a pass without the web adapter was built")
	}
}

// A dry pass reports what it would file and writes nothing.
func TestContactPassDryRunWritesNothing(t *testing.T) {
	rs, _, _, _ := contactsRunStore(t)
	run := mustRun(t, rs, RunRequest{Source: "web", Query: "example", Fields: map[string]string{"seed_url": "https://lab.example/people/"}})
	cache := snapshot(t, rs.Root())
	pass, _ := rs.NewContactPass(false)
	got, res, err := pass.Run(context.Background(), run.ID, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 || len(contactRows(got.Drafts[0])) != 1 {
		t.Errorf("dry pass reported nothing: %+v", res)
	}
	assertIdentical(t, "run cache after a dry pass", cache, snapshot(t, rs.Root()))
	loaded, _ := rs.Get(run.ID)
	if loaded.Contacts != nil || len(contactRows(loaded.Drafts[0])) != 0 {
		t.Errorf("dry pass persisted: %+v", loaded)
	}
}

// The per-draft lookup files the same rows for the one draft pressed, and
// reports them, without touching its neighbours.
func TestLookupFilesPublishedContactForOneDraft(t *testing.T) {
	rs, _, vault, _ := contactsRunStore(t)
	run := mustRun(t, rs, RunRequest{Source: "web", Query: "example", Fields: map[string]string{"seed_url": "https://lab.example/people/"}})
	before := snapshot(t, vault)
	var ellie, cory string
	for _, d := range run.Drafts {
		switch d.Draft.Name {
		case "Ellie Berkland":
			ellie = d.ID
		case "Cory Berkland":
			cory = d.ID
		}
	}
	got, res, err := rs.Lookup(context.Background(), run.ID, ellie, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Contacts != 1 || len(res.ContactPages) != 1 || res.ContactPages[0].Status != ContactsPageRead {
		t.Errorf("lookup result: %+v", res)
	}
	for _, d := range got.Drafts {
		rows := contactRows(d)
		switch d.ID {
		case ellie:
			if len(rows) != 1 || rows[0] != "Ellie Berkland · ellie@example.test" {
				t.Errorf("ellie: %v", rows)
			}
		default:
			if len(rows) != 0 {
				t.Errorf("%s (%s) gained rows from a neighbour's lookup: %v", d.ID, d.Draft.Name, rows)
			}
		}
		if d.Status != DraftNew {
			t.Errorf("%s status %s", d.ID, d.Status)
		}
	}
	_ = cory
	assertIdentical(t, "vault after lookup", before, snapshot(t, vault))
}

// The read step's classification, independent of any network: a source
// error is a page error, an unnamed page is unavailable, the same bytes
// under a second URL are unavailable, and a URL is asked once per pass.
func TestContactPassReadClassifies(t *testing.T) {
	src := &fakeContactSource{pages: map[string]sources.ContactPage{
		"https://a.example/people/": {URL: "https://a.example/people/", BodyHash: "h1", Named: []string{"Dana Reyes"}},
		"https://b.example/people/": {URL: "https://b.example/people/", BodyHash: "h1", Named: []string{"Dana Reyes"}},
		"https://c.example/people/": {URL: "https://c.example/people/", BodyHash: "h2"},
	}, errs: map[string]error{"https://d.example/people/": errors.New("HTTP 403")}}
	p := &ContactPass{source: src, pages: map[string]contactRead{}, bodies: map[string]string{}}
	roster := []string{"Dana Reyes"}
	if got := p.read(context.Background(), "https://a.example/people/", roster); got.Status != ContactsPageRead {
		t.Errorf("a: %+v", got)
	}
	if got := p.read(context.Background(), "https://b.example/people/", roster); got.Status != ContactsPageUnavailable || !strings.Contains(got.Why, "identical body to https://a.example/people/") {
		t.Errorf("b: %+v", got)
	}
	if got := p.read(context.Background(), "https://c.example/people/", roster); got.Status != ContactsPageUnavailable || !strings.Contains(got.Why, "none of the queued names") {
		t.Errorf("c: %+v", got)
	}
	if got := p.read(context.Background(), "https://d.example/people/", roster); got.Status != ContactsPageError || got.Why != "HTTP 403" {
		t.Errorf("d: %+v", got)
	}
	p.read(context.Background(), "https://a.example/people/", roster)
	if src.calls["https://a.example/people/"] != 1 {
		t.Errorf("a asked %d times", src.calls["https://a.example/people/"])
	}
}

type fakeContactSource struct {
	pages map[string]sources.ContactPage
	errs  map[string]error
	calls map[string]int
}

func (f *fakeContactSource) LookupContacts(_ context.Context, u string, _ []string) (sources.ContactPage, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[u]++
	if err := f.errs[u]; err != nil {
		return sources.ContactPage{}, err
	}
	return f.pages[u], nil
}

// webFoundDraft is a draft the way the web crawl emits one: found on a
// page, cited by that page, no contact detail.
func webFoundDraft(name, page string) sources.CandidateDraft {
	return sources.CandidateDraft{
		SourceID: "web", ExternalID: page + "#" + strings.ToLower(strings.ReplaceAll(name, " ", "-")),
		Name: name, Title: "Graduate Student",
		Evidence: []sources.Evidence{{SourceID: "web", URLOrFile: page, RetrievedAt: testNow, Snippet: name + " · Graduate Student", Kind: sources.EvidencePage, Trust: sources.TrustLow}},
	}
}
