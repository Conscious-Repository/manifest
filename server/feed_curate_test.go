package server

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/consume"
	"manifest/feed"
	"manifest/spirits"
)

// THE BRIDGE, end to end: a domain-scout card → the full article behind its
// link → an extrinsic/ note → content:encoded in the public feed. This is the
// one test that spans both lanes, because the whole point of the feature is
// that they land in the same place.

const scoutArticleHTML = `<!doctype html><html><body>
<nav><a href="/">home</a><a href="/about">about</a></nav>
<article>
<h1>Predictive Coding in Cortical Microcircuits</h1>
<p>The hierarchical predictive coding account holds that each cortical area sends predictions downward and errors upward, a loop that is cheap to run and expensive to falsify.</p>
<p>We recorded from layer 2/3 across four animals and found the error signal was carried by a sparse population rather than distributed across the sheet, which the standard formulation does not require.</p>
<p>The consequence for the wider theory is modest but real: the loop is not merely a mathematical convenience, it has an identifiable substrate that can be lesioned and measured.</p>
</article>
<footer><p>Copyright some publisher, all rights reserved, contact us about reprints.</p></footer>
</body></html>`

// scoutHarness is the consume harness plus a spirits tree holding one feed
// card, wired the way main.go wires them.
type scoutHarness struct {
	*consumeHarness
	article *httptest.Server
	hits    int
}

func newScoutHarness(t *testing.T, page func(w http.ResponseWriter, r *http.Request)) *scoutHarness {
	t.Helper()
	h := &scoutHarness{consumeHarness: newConsumeHarness(t)}
	h.article = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits++
		page(w, r)
	}))
	t.Cleanup(h.article.Close)
	h.srv.UseSpirits(spirits.NewStore(t.TempDir()))
	return h
}

func servePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(scoutArticleHTML))
}

// plantCard writes one research card into the harness feed tree.
func (h *scoutHarness) plantCard(t *testing.T, id, link string) feed.Item {
	t.Helper()
	it := feed.Item{
		ID: id, Type: "paper", Status: "new",
		Title:  "Predictive Coding in Cortical Microcircuits",
		Why:    "the first lesion evidence for the error-signal population",
		Link:   link,
		Source: "arXiv",
		Agent:  "domain-scout",
		Domain: "neuro",
		Date:   time.Now().UTC().Format(time.RFC3339),
	}
	if _, _, err := h.srv.spirits.Feed.Upsert(it); err != nil {
		t.Fatal(err)
	}
	return it
}

// publicItems renders what a subscriber would actually fetch.
func (h *scoutHarness) publicItems(t *testing.T) []struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	Desc    string `xml:"description"`
	Encoded string `xml:"encoded"`
	GUID    string `xml:"guid"`
} {
	t.Helper()
	w := httptest.NewRecorder()
	consume.PublicHandler(h.svc, consume.PublicConfig{
		Title: "attention", BaseURL: "https://attention.consciousrepository.com",
	}).ServeHTTP(w, httptest.NewRequest("GET", "/feed.xml", nil))
	var parsed struct {
		Channel struct {
			Items []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				Desc    string `xml:"description"`
				Encoded string `xml:"encoded"`
				GUID    string `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("public feed does not parse: %v\n%s", err, w.Body.String())
	}
	return parsed.Channel.Items
}

func TestFeedCurateBridgesCardIntoPublicFeed(t *testing.T) {
	h := newScoutHarness(t, servePage)
	h.plantCard(t, "paper-1", h.article.URL+"/p/predictive-coding")

	w := h.do(t, "POST", "/api/feed/paper-1/curate", `{"note":"the lesion result is the news here"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("curate = %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		OK     bool   `json:"ok"`
		Path   string `json:"path"`
		Note   string `json:"note"`
		Mirror string `json:"mirror"`
		Full   bool   `json:"full"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.Full || got.Mirror != consume.MirrorFull {
		t.Fatalf("a fetched article should curate in full: %+v", got)
	}
	if !strings.HasPrefix(got.Path, "extrinsic/") {
		t.Fatalf("note did not land under extrinsic/: %q", got.Path)
	}
	if got.Note != "the lesion result is the news here" {
		t.Errorf("note not carried: %q", got.Note)
	}
	if h.hits != 1 {
		t.Errorf("want exactly one article fetch, got %d", h.hits)
	}

	// The vault note is the archive: the WHOLE piece, in markdown, not the
	// card's one-line `why`.
	note, err := os.ReadFile(filepath.Join(h.vault, got.Path))
	if err != nil {
		t.Fatal(err)
	}
	body := string(note)
	if !strings.Contains(body, "sparse population rather than distributed") {
		t.Errorf("note body is not the fetched article:\n%s", body)
	}
	if !strings.Contains(body, "mirror: full") {
		t.Errorf("note is not marked full:\n%s", body)
	}
	if !strings.Contains(body, "categories: [articles]") {
		t.Errorf("note carries no articles category — the public feed will not select it:\n%s", body)
	}

	// …and a subscriber gets that same piece as content:encoded.
	items := h.publicItems(t)
	if len(items) != 1 {
		t.Fatalf("want 1 public item, got %d", len(items))
	}
	pub := items[0]
	if !strings.Contains(pub.Encoded, "sparse population rather than distributed") {
		t.Errorf("public feed does not carry the article:\n%s", pub.Encoded)
	}
	if !strings.Contains(pub.Encoded, "the lesion result is the news here") {
		t.Errorf("the owner's note is missing from the entry:\n%s", pub.Encoded)
	}
	if pub.Link != h.article.URL+"/p/predictive-coding" {
		t.Errorf("link is not the original: %q", pub.Link)
	}
	if pub.Desc != "the lesion result is the news here" {
		t.Errorf("description should be the note: %q", pub.Desc)
	}
	if pub.GUID != consume.ExternalItemID("paper-1") {
		t.Errorf("guid is not the bridged id: %q", pub.GUID)
	}

	// Uncurate unpublishes without deleting the archive.
	if w := h.do(t, "POST", "/api/feed/paper-1/uncurate", ""); w.Code != http.StatusOK {
		t.Fatalf("uncurate = %d: %s", w.Code, w.Body.String())
	}
	if items := h.publicItems(t); len(items) != 0 {
		t.Fatalf("uncurated entry still public: %d items", len(items))
	}
	if _, err := os.Stat(filepath.Join(h.vault, got.Path)); err != nil {
		t.Fatalf("uncurate deleted the note: %v", err)
	}
}

// A publisher that refuses the page must not cost the owner his choice: the
// card's own words are curated instead, honestly marked as an excerpt.
func TestFeedCurateFallsBackToWhy(t *testing.T) {
	h := newScoutHarness(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	})
	h.plantCard(t, "paper-2", h.article.URL+"/p/paywalled")

	w := h.do(t, "POST", "/api/feed/paper-2/curate", `{"note":""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("curate = %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Path   string `json:"path"`
		Mirror string `json:"mirror"`
		Full   bool   `json:"full"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Full || got.Mirror != consume.MirrorExcerpt {
		t.Fatalf("an unfetchable page must not claim a full mirror: %+v", got)
	}
	note, err := os.ReadFile(filepath.Join(h.vault, got.Path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(note), "the first lesion evidence") {
		t.Errorf("fallback body is not the card's why:\n%s", note)
	}
	items := h.publicItems(t)
	if len(items) != 1 {
		t.Fatalf("want 1 public item, got %d", len(items))
	}
	// mirror: excerpt means link + description, never a body pretending to be
	// the piece.
	if items[0].Encoded != "" {
		t.Errorf("excerpt entry carried a body: %q", items[0].Encoded)
	}
	if items[0].Desc == "" {
		t.Errorf("excerpt entry has nothing to show a reader")
	}
}

// Curating twice refreshes the note rather than writing a second one, and
// never clobbers what the owner wrote underneath.
func TestFeedCurateIsIdempotent(t *testing.T) {
	h := newScoutHarness(t, servePage)
	h.plantCard(t, "paper-3", h.article.URL+"/p/predictive-coding")

	first := h.do(t, "POST", "/api/feed/paper-3/curate", `{"note":"first"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("curate = %d: %s", first.Code, first.Body.String())
	}
	var a struct{ Path string }
	_ = json.Unmarshal(first.Body.Bytes(), &a)

	// the owner adds his own paragraph in Obsidian
	p := filepath.Join(h.vault, a.Path)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(raw, []byte("\n\nMy own reading of this.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	second := h.do(t, "POST", "/api/feed/paper-3/curate", `{"note":"second"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("re-curate = %d: %s", second.Code, second.Body.String())
	}
	var b struct{ Path, Note string }
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if b.Path != a.Path {
		t.Fatalf("re-curate wrote a second note: %q then %q", a.Path, b.Path)
	}
	if b.Note != "second" {
		t.Errorf("note was not refreshed: %q", b.Note)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "My own reading of this.") {
		t.Fatalf("re-curate clobbered the owner's writing:\n%s", after)
	}
	if n := len(h.publicItems(t)); n != 1 {
		t.Fatalf("want 1 public item after two curates, got %d", n)
	}
}

// A card with no fetchable link — an artifact reference, say — still curates,
// and the public entry does not point subscribers at a path that means nothing
// off this machine.
func TestFeedCurateDropsNonWebLink(t *testing.T) {
	h := newScoutHarness(t, servePage)
	h.plantCard(t, "paper-4", "artifacts/library/some-brief.md")

	w := h.do(t, "POST", "/api/feed/paper-4/curate", `{"note":"local brief"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("curate = %d: %s", w.Code, w.Body.String())
	}
	if h.hits != 0 {
		t.Errorf("a non-web link must not be fetched, got %d hits", h.hits)
	}
	items := h.publicItems(t)
	if len(items) != 1 {
		t.Fatalf("want 1 public item, got %d", len(items))
	}
	if items[0].Link != "" {
		t.Errorf("published a link subscribers cannot follow: %q", items[0].Link)
	}
}

// Without a spirits tree there is no card to read, and without the consume
// lane there is nowhere to write: both answer honestly instead of 500ing.
func TestFeedCurateUnavailable(t *testing.T) {
	h := newConsumeHarness(t)
	if w := h.do(t, "POST", "/api/feed/whatever/curate", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no spirits: want 503, got %d", w.Code)
	}
	s := New(nil, nil, nil)
	s.UseSpirits(spirits.NewStore(t.TempDir()))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/feed/whatever/curate", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no consume lane: want 503, got %d", w.Code)
	}
}
