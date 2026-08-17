package teamportal

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The domain gate (§: any @aion.bio account writes, nothing else — wildcard by
// design, no allow-list).
func TestAuthorizedDomainGate(t *testing.T) {
	for _, tc := range []struct {
		email string
		want  bool
	}{
		{"hannah@aion.bio", true},
		{"Benjamin@AION.BIO", true},      // case-insensitive
		{"  rj@aion.bio  ", true},        // whitespace-tolerant
		{"new-hire-2027@aion.bio", true}, // wildcard: unknown accounts still pass
		{"someone@gmail.com", false},
		{"hannah@notaion.bio", false}, // suffix must include the @
		{"hannah@aion.bio.evil.com", false},
		{"aion.bio", false}, // no @ at all
		{"", false},
	} {
		if got := Authorized(tc.email); got != tc.want {
			t.Errorf("Authorized(%q) = %v, want %v", tc.email, got, tc.want)
		}
	}
}

func TestIdentifyRequestAcceptsBearerAndPrefersCookie(t *testing.T) {
	tokens := NewTokens(t.TempDir())
	a := NewAuth(t.TempDir()).WithTokens(tokens)
	plain, _, err := tokens.Mint(Identity{Email: "hannah@aion.bio", Name: "Hannah"}, "test")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/team/state", nil)
	r.Header.Set("Authorization", "Bearer "+plain)
	if id, ok := a.IdentifyRequest(r); !ok || id.Email != "hannah@aion.bio" {
		t.Fatalf("IdentifyRequest bearer = %+v, %v", id, ok)
	}

	// A valid browser session is authoritative when both credentials appear.
	c, err := a.SessionCookie("rj@aion.bio", "RJ", false, time.Now())
	if err != nil {
		t.Fatalf("SessionCookie: %v", err)
	}
	r.AddCookie(c)
	if id, ok := a.IdentifyRequest(r); !ok || id.Email != "rj@aion.bio" {
		t.Fatalf("IdentifyRequest cookie precedence = %+v, %v", id, ok)
	}

	bad := httptest.NewRequest(http.MethodGet, "/api/team/state", nil)
	bad.Header.Set("Authorization", "Basic nope")
	if _, ok := a.IdentifyRequest(bad); ok {
		t.Fatal("IdentifyRequest accepted a non-bearer Authorization header")
	}
	bad.Header.Set("Authorization", "Bearer "+plain+" extra")
	if _, ok := a.IdentifyRequest(bad); ok {
		t.Fatal("IdentifyRequest accepted a malformed bearer header")
	}
}

// SessionCookie is defense in depth on the same gate: it refuses to mint a
// session for a foreign domain even if a caller slips past the callback check.
func TestSessionCookieEnforcesDomain(t *testing.T) {
	a := NewAuth(t.TempDir())
	if _, err := a.SessionCookie("evil@gmail.com", "Evil", false, time.Now()); err == nil {
		t.Fatal("SessionCookie minted a session for a non-@aion.bio account")
	}

	c, err := a.SessionCookie("hannah@aion.bio", "Hannah Zmuda", false, time.Now())
	if err != nil {
		t.Fatalf("SessionCookie for @aion.bio: %v", err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	id, ok := a.Identify(r)
	if !ok || id.Email != "hannah@aion.bio" || id.Name != "Hannah Zmuda" {
		t.Fatalf("Identify round-trip = %+v, %v; want hannah@aion.bio", id, ok)
	}
}

// Expired or tampered sessions are simply anonymous.
func TestIdentifyRejectsExpiredAndTamperedSessions(t *testing.T) {
	a := NewAuth(t.TempDir())

	expired, err := a.SessionCookie("hannah@aion.bio", "Hannah", false, time.Now().Add(-sessionTTL-time.Hour))
	if err != nil {
		t.Fatalf("SessionCookie: %v", err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(expired)
	if _, ok := a.Identify(r); ok {
		t.Error("Identify accepted an expired session")
	}

	good, err := a.SessionCookie("hannah@aion.bio", "Hannah", false, time.Now())
	if err != nil {
		t.Fatalf("SessionCookie: %v", err)
	}
	tampered := *good
	tampered.Value = "x" + tampered.Value[1:]
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&tampered)
	if _, ok := a.Identify(r); ok {
		t.Error("Identify accepted a tampered session")
	}

	// A session signed by a DIFFERENT key (another machine's auth) fails too.
	other := NewAuth(t.TempDir())
	foreign, err := other.SessionCookie("hannah@aion.bio", "Hannah", false, time.Now())
	if err != nil {
		t.Fatalf("SessionCookie: %v", err)
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(foreign)
	if _, ok := a.Identify(r); ok {
		t.Error("Identify accepted a session signed with a foreign key")
	}
}
