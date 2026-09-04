package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"manifest/teamportal"
)

// initialsFor resolves a Google email to the roster person via the explicit
// email field (people.md), so ben@aion.bio → BA even though the local-part
// matches neither "benjamin" nor "ba".
func TestInitialsForEmailField(t *testing.T) {
	api := &portalAPI{people: []portalPerson{
		{Initials: "BA", Name: "Benjamin Anderson", Email: "ben@aion.bio"},
		{Initials: "YA", Name: "Yousuke Akama", Email: "yashiro@aion.bio"},
		{Initials: "JR", Name: "Jack Ruhl"}, // no email — heuristic fallback
	}}
	cases := []struct{ email, want string }{
		{"ben@aion.bio", "BA"},     // exact email field
		{"BEN@AION.BIO", "BA"},     // case-insensitive
		{"yashiro@aion.bio", "YA"}, // name ≠ local-part, resolved by email
		{"jack@aion.bio", "JR"},    // fallback: first-name heuristic
		{"nobody@aion.bio", ""},    // unmapped
	}
	for _, c := range cases {
		if got := api.initialsFor(c.email); got != c.want {
			t.Errorf("initialsFor(%q) = %q, want %q", c.email, got, c.want)
		}
	}
	if got := api.personName("ben@aion.bio"); got != "Benjamin Anderson" {
		t.Errorf("personName = %q", got)
	}
}

// The standalone portal listener (AION portal move, phase 1): GET / is the
// portal's own index.html, and its assets resolve at the root of that port.
func TestPortalHandlerServesTheEmbeddedPortal(t *testing.T) {
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatalf("PortalHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, tc := range []struct{ path, want string }{
		{"/", "AION &middot; portal"},
		{"/index.html", "AION &middot; portal"},
		{"/src/data-load.js", "loadPortalData"},
		{"/data/meta.json", ""},
		{"/content/hiring.md", ""},
		{"/assets/colors_and_type.css", ":root"},
		{"/assets/favicon.png", ""},
	} {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body := readAllString(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", tc.path, resp.StatusCode)
			continue
		}
		if tc.want != "" && !strings.Contains(body, tc.want) {
			t.Errorf("GET %s body missing %q", tc.path, tc.want)
		}
	}

	// The portal index points at its assets by relative path (no /investor
	// prefix from the aionbio root) so it renders standalone on :7778. The v2
	// redesign self-hosts fonts in portal.css (colors_and_type.css dropped).
	idx := getBody(t, srv.URL+"/")
	if !strings.Contains(idx, `href="src/portal.css`) {
		t.Errorf("index.html: missing portal.css reference")
	}
	if !strings.Contains(idx, `href="./assets/favicon.png"`) {
		t.Errorf("index.html: missing relative favicon reference")
	}
	if strings.Contains(idx, "/investor/assets") {
		t.Errorf("index.html: still points at aionbio /investor/assets absolute path")
	}

	// The portal mux is mutually exclusive with the dashboard mux: no API
	// surface, and none of the dashboard's own assets, live on this port.
	for _, path := range []string{"/api/day", "/js/app.js"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 on the portal listener", path, resp.StatusCode)
		}
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	return readAllString(t, resp)
}

func readAllString(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

// fakeLive implements PortalLive and NOTHING else. If portal.go ever grows a
// sixth call on opt.Live, this file stops compiling — which is the point: the
// read seam between two portals should be widened deliberately, not by
// accident (ooda-portal plan, Stage A step 5).
type fakeLive struct{ owner string }

func (f *fakeLive) File(string) ([]byte, string, error) { return nil, "", errNoFile }
func (f *fakeLive) Status() PortalLiveStatus            { return PortalLiveStatus{BaseRevision: "fake"} }
func (f *fakeLive) TeamStateJSON() any                  { return map[string]any{"fake": true} }
func (f *fakeLive) People() []portalPerson              { return nil }
func (f *fakeLive) OwnerOf(string) (string, bool)       { return f.owner, f.owner != "" }

var errNoFile = errors.New("no such file")

var _ PortalLive = (*fakeLive)(nil)

// A portal driven by a NON-Aion projection serves the shared routes fine, and
// a typed-nil *AionLive must never reach a handler (it would be a non-nil
// interface and panic on the first data request).
func TestPortalLiveSeamAcceptsAnyProjection(t *testing.T) {
	store, err := teamportal.New(filepath.Join(t.TempDir(), "team"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := PortalHandler(PortalOptions{
		Auth: teamportal.NewAuth(t.TempDir()), Store: store,
		AdminEmail: "ben@ooda.group", Live: &fakeLive{owner: "BPA"},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/team/state", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 && rec.Code != 200 {
		t.Fatalf("team/state through a foreign projection: %d", rec.Code)
	}

	// typed-nil guard: main.go must never assign a nil *AionLive, and a
	// handler built with one must not panic if it somehow does.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("typed-nil *AionLive panicked: %v", r)
		}
	}()
	var nilLive *AionLive
	h2, err := PortalHandler(PortalOptions{Live: nilLive})
	if err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, httptest.NewRequest("GET", "/api/live/revision", nil))
}
