package teamportal

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWebClient plants a minimal web-client JSON where the policy's Auth will
// look for it.
func writeWebClient(t *testing.T, dataDir, file string) {
	t.Helper()
	dir := filepath.Join(dataDir, "portals")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	client := `{"web":{"client_id":"cid","client_secret":"sec",` +
		`"redirect_uris":["https://portal.example.com/oauth2/callback"]}}`
	if err := os.WriteFile(filepath.Join(dir, file), []byte(client), 0o600); err != nil {
		t.Fatal(err)
	}
}

// loginRedirect drives HandleLogin and returns the parsed Google auth URL.
func loginRedirect(t *testing.T, a *Auth, query string) *url.URL {
	t.Helper()
	r := httptest.NewRequest("GET", "/oauth2/login"+query, nil)
	w := httptest.NewRecorder()
	a.HandleLogin(w, r)
	if w.Code != 302 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// A policy carrying ExtraScopes (the OODA portal bundling gmail.readonly into
// sign-in) must request the scope, offline access, and the consent screen —
// Google only mints a refresh token on explicit consent.
func TestExtraScopesRideTheLoginRedirect(t *testing.T) {
	dataDir := t.TempDir()
	writeWebClient(t, dataDir, "ooda-portal-oauth.json")
	a := NewAuthPolicy(dataDir, Policy{
		Domain: "ooda.group", CookiePrefix: "ooda_portal",
		ClientFile: "ooda-portal-oauth.json", KeyFile: "ooda-portal-session.key",
		ExtraScopes: []string{"https://www.googleapis.com/auth/gmail.readonly"},
	})
	u := loginRedirect(t, a, "")
	q := u.Query()
	if !strings.Contains(q.Get("scope"), "gmail.readonly") {
		t.Fatalf("scope missing gmail.readonly: %q", q.Get("scope"))
	}
	if q.Get("access_type") != "offline" {
		t.Fatalf("access_type = %q, want offline", q.Get("access_type"))
	}
	if q.Get("prompt") != "consent" {
		t.Fatalf("prompt = %q, want consent", q.Get("prompt"))
	}
	// ?switch composes: the chooser AND the consent screen.
	q2 := loginRedirect(t, a, "?switch=1").Query()
	if p := q2.Get("prompt"); p != "select_account consent" {
		t.Fatalf("switch prompt = %q, want %q", p, "select_account consent")
	}
}

// The zero policy (AION) must be byte-identical to the pre-ExtraScopes flow:
// identity scopes only, no offline access, no forced consent.
func TestZeroPolicyLoginRedirectUnchanged(t *testing.T) {
	dataDir := t.TempDir()
	writeWebClient(t, dataDir, "aion-portal-oauth.json")
	a := NewAuth(dataDir)
	q := loginRedirect(t, a, "").Query()
	if got := q.Get("scope"); got != "openid email profile" {
		t.Fatalf("scope = %q, want identity-only", got)
	}
	if q.Get("access_type") != "" || q.Get("prompt") != "" {
		t.Fatalf("zero policy gained access_type=%q prompt=%q", q.Get("access_type"), q.Get("prompt"))
	}
	if p := loginRedirect(t, a, "?switch=1").Query().Get("prompt"); p != "select_account" {
		t.Fatalf("switch prompt = %q, want select_account", p)
	}
}
