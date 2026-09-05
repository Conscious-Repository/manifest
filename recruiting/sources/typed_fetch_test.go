package sources

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A probe reads what the page SAYS about itself and reports it. It maps
// nothing — the type table lives in recruiting/intake_refine.go — so these
// tests pin the reading: the shapes real CMSes emit, and the guards.
func TestProbeTypesReadsWhatThePageDeclares(t *testing.T) {
	const labPage = `<!doctype html><html><head>
<title>Yablonskiy Lab</title>
<meta property="og:type" content="website">
<meta property="og:site_name" content="WashU BME">
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"CollegeOrUniversity","name":"Washington University"}
</script>
</head><body><h1>Lab</h1></body></html>`

	net := newWebNet().site("lab.example.edu", map[string]string{"/": labPage})
	got, err := net.adapter().ProbeTypes(context.Background(), "https://lab.example.edu/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.JSONLD) != 1 || got.JSONLD[0] != "CollegeOrUniversity" {
		t.Fatalf("jsonld: %v", got.JSONLD)
	}
	if got.OGType != "website" {
		t.Fatalf("og:type: %q", got.OGType)
	}
	if got.Name != "Washington University" || got.SiteName != "WashU BME" {
		t.Fatalf("names: %+v", got)
	}
	if got.Empty() {
		t.Fatal("a page that declared a type is not empty")
	}
}

// The shapes that break naive readers: @graph, an array of blocks, a type
// written as a full schema.org URL, and a block of broken JSON beside a good
// one. Half the web ships at least one of these.
func TestProbeTypesSurvivesRealWorldJSONLD(t *testing.T) {
	const page = `<!doctype html><html><head>
<script type="application/ld+json">{ this is not json }</script>
<script type="application/ld+json">
{"@context":"https://schema.org","@graph":[
  {"@type":"WebPage","name":"Home"},
  {"@type":["Organization","https://schema.org/Corporation"],"name":"Acme Bio"}
]}
</script>
<script type="application/ld+json">[{"@type":"BlogPosting"}]</script>
</head><body></body></html>`

	net := newWebNet().site("acme.example.com", map[string]string{"/about": page})
	got, err := net.adapter().ProbeTypes(context.Background(), "https://acme.example.com/about")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"WebPage", "Organization", "Corporation", "BlogPosting"}
	if strings.Join(got.JSONLD, ",") != strings.Join(want, ",") {
		t.Fatalf("types: %v, want %v (document order, deduped, namespace stripped)", got.JSONLD, want)
	}
}

// A page that declares nothing is not an error — it is an empty answer, and
// the resolution the owner already has stands.
func TestProbeTypesOnASilentPage(t *testing.T) {
	net := newWebNet().site("plain.example.org", map[string]string{"/": "<html><body>hi</body></html>"})
	got, err := net.adapter().ProbeTypes(context.Background(), "https://plain.example.org/")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Empty() {
		t.Fatalf("a silent page says nothing: %+v", got)
	}
}

// ⚠ THE GUARDS. A probe is a fetch of a URL somebody pasted, so it runs
// behind every refusal the crawler runs behind: no private hosts, no
// LinkedIn, and robots.txt is honoured — a probe is not a loophole around
// the rules the run obeys.
func TestProbeTypesKeepsTheCrawlersGuards(t *testing.T) {
	net := newWebNet()
	for _, target := range []string{
		"http://127.0.0.1:9999/",
		"https://localhost/x",
		"https://www.linkedin.com/company/acme",
		"ftp://example.org/x",
	} {
		if _, err := net.adapter().ProbeTypes(context.Background(), target); err == nil {
			t.Fatalf("%s must be refused before any request", target)
		}
	}
	if len(net.requests()) != 0 {
		t.Fatalf("a refused probe made requests: %v", net.requests())
	}

	// robots.txt disallowing the path stops the probe too
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private\n"))
	})
	mux.HandleFunc("/private/lab", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<script type="application/ld+json">{"@type":"Person"}</script>`))
	})
	net2 := newWebNet().handler("closed.example.org", mux)
	if _, err := net2.adapter().ProbeTypes(context.Background(), "https://closed.example.org/private/lab"); err == nil {
		t.Fatal("robots.txt disallowed the path — the probe must not fetch it")
	}
	for _, r := range net2.requests() {
		if strings.Contains(r, "/private/lab") {
			t.Fatalf("the page was fetched anyway: %v", net2.requests())
		}
	}
}

// A probe is ONE GET of the page it was given. It has no frontier and
// follows no links — that is the run's job, and the run happens later, after
// the owner has seen the scaffold.
func TestProbeTypesFetchesOnePage(t *testing.T) {
	const page = `<html><head><script type="application/ld+json">{"@type":"Person"}</script></head>
<body><a href="/one">one</a><a href="/two">two</a><a href="https://elsewhere.example.com/">off</a></body></html>`
	net := newWebNet().site("one.example.org", map[string]string{
		"/": page, "/one": page, "/two": page})
	if _, err := net.adapter().ProbeTypes(context.Background(), "https://one.example.org/"); err != nil {
		t.Fatal(err)
	}
	if got := net.pages(); len(got) != 1 {
		t.Fatalf("a probe fetched %d pages: %v", len(got), got)
	}
}
