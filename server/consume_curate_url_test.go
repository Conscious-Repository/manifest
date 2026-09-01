package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/consume"
)

// THE ENDPOINT behind the FEED header's ＋ curate. It answers the shape
// handleFeedCurate answers, plus what the pasted-link toast needs to say which
// kind landed.

const pastedArticlePage = `<!doctype html><html><head>
<meta property="og:title" content="The Dictatorship of the Articulate" />
<meta property="og:site_name" content="Melissa's Newsletter" />
<meta property="og:description" content="A claim about who gets heard." />
</head><body><article>
<p>The first claim is that fluency is mistaken for correctness in nearly every room where a decision is actually made, and the mistake compounds.</p>
<p>The second claim is that the correction is procedural rather than cultural: write it down before you say it, and the articulate lose their edge.</p>
<p>What follows from both is a way of running a meeting that is duller and considerably more accurate than the one it replaces every week.</p>
</article></body></html>`

func newCurateURLHarness(t *testing.T) (*consumeHarness, *httptest.Server) {
	t.Helper()
	h := newConsumeHarnessWith(t, consume.Config{AllowPrivateCurateFetch: true})
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pastedArticlePage))
	}))
	t.Cleanup(page.Close)
	return h, page
}

func TestCurateURLEndpointPublishesAndAnswersTheFeedShape(t *testing.T) {
	h, page := newCurateURLHarness(t)

	w := h.do(t, "POST", "/api/consume/curate-url",
		`{"url":"`+page.URL+`/p/dictatorship","note":"the middle third is the argument"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var d struct {
		OK     bool   `json:"ok"`
		Path   string `json:"path"`
		Note   string `json:"note"`
		Mirror string `json:"mirror"`
		Full   bool   `json:"full"`
		Public string `json:"public"`
		Kind   string `json:"kind"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if !d.OK || !d.Full || d.Mirror != consume.MirrorFull {
		t.Errorf("response: %+v", d)
	}
	if !strings.HasPrefix(d.Path, "extrinsic/") {
		t.Errorf("path: %q", d.Path)
	}
	if d.Kind != "article" {
		t.Errorf("kind: %q", d.Kind)
	}
	if d.Title != "The Dictatorship of the Articulate" {
		t.Errorf("title: %q", d.Title)
	}
	if d.Note != "the middle third is the argument" {
		t.Errorf("note: %q", d.Note)
	}
	if d.Public != "https://reading.example" {
		t.Errorf("public: %q", d.Public)
	}
	// It is really in the vault, under extrinsic/, and nowhere else.
	if _, err := os.Stat(filepath.Join(h.vault, d.Path)); err != nil {
		t.Fatalf("no note on disk: %v", err)
	}
	// …and the private curated mirror now shows it.
	cw := h.do(t, "GET", "/api/consume/curated", "")
	if !strings.Contains(cw.Body.String(), "Dictatorship") {
		t.Errorf("the curated panel does not see it:\n%s", cw.Body.String())
	}
}

func TestCurateURLEndpointRejectsAnEmptyURL(t *testing.T) {
	h, _ := newCurateURLHarness(t)
	for _, body := range []string{`{}`, `{"url":""}`, `{"url":"   ","note":"x"}`} {
		w := h.do(t, "POST", "/api/consume/curate-url", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s → status %d, want 400", body, w.Code)
		}
	}
}

// A rejected link is the owner's to fix, so the guard's own sentence comes
// back with a 400 rather than a 500 and a log line he will never read.
func TestCurateURLEndpointAnswersTheGuardsSentence(t *testing.T) {
	h := newConsumeHarnessWith(t, consume.Config{}) // the guard as production runs it
	for _, raw := range []string{
		"http://127.0.0.1:1200/twitter/user/melissa",
		"file:///etc/passwd",
		"http://example.com:9200/_search",
	} {
		w := h.do(t, "POST", "/api/consume/curate-url", `{"url":"`+raw+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s → status %d, want 400", raw, w.Code)
		}
		if strings.TrimSpace(w.Body.String()) == "" {
			t.Errorf("%s → 400 with no explanation", raw)
		}
	}
}

// The neighbours' contract: a build with no lane answers rather than 500s.
func TestCurateURLEndpointWithoutTheLane(t *testing.T) {
	s := New(nil, nil, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/consume/curate-url",
		strings.NewReader(`{"url":"https://example.com/x"}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", w.Code)
	}
}

// Pasting the same link twice refreshes one note — asserted through the HTTP
// surface, because that is where a double-click actually happens.
func TestCurateURLEndpointIsIdempotent(t *testing.T) {
	h, page := newCurateURLHarness(t)
	first := h.do(t, "POST", "/api/consume/curate-url", `{"url":"`+page.URL+`/p/x","note":"one"}`)
	second := h.do(t, "POST", "/api/consume/curate-url", `{"url":"`+page.URL+`/p/x?utm_source=x","note":"two"}`)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("%d / %d", first.Code, second.Code)
	}
	var a, b struct {
		Path string `json:"path"`
		Note string `json:"note"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if a.Path != b.Path {
		t.Errorf("two notes for one link: %s / %s", a.Path, b.Path)
	}
	if b.Note != "two" {
		t.Errorf("the note was not refreshed: %q", b.Note)
	}
	entries, err := os.ReadDir(filepath.Join(h.vault, "extrinsic"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("extrinsic/ holds %d notes, want 1", len(entries))
	}
}
