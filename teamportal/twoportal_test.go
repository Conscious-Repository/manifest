package teamportal_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/server"
	"manifest/teamportal"
)

// oodaShapedPortal stands up a SECOND portal instance the way the OODA portal
// will: its own policy (domain + allow-list), its own store, its own cookie
// namespace — over the same shared write layer.
func oodaShapedPortal(t *testing.T, dataDir string) (*httptest.Server, *teamportal.Auth, *teamportal.Store, string) {
	t.Helper()
	teamDir := filepath.Join(t.TempDir(), "ooda-team")
	store, err := teamportal.New(teamDir)
	if err != nil {
		t.Fatal(err)
	}
	// ONE file is both the allow-list and the email→initials map. That is an
	// invariant, not a convenience: ownerToken falls back to the address's
	// local part, so an admitted-but-unmapped partner would file work under a
	// garbage owner ("me@olgasobkiv.com" → "me" instead of "OS").
	// The REAL mapping (corrected 2026-08-21): brian@ooda.group is Brian
	// FROMAL (BF); Brian ANDERSON (BPA) is bpabbassa@att.net. BA carries two
	// addresses — the same-person-two-addresses arm anchors on him.
	if err := os.WriteFile(filepath.Join(teamDir, "emails.json"),
		[]byte(`{"ben@ooda.group":"BA","me@benjaminbanderson.com":"BA","brian@ooda.group":"BF","bpabbassa@att.net":"BPA","me@olgasobkiv.com":"OS"}`),
		0o600); err != nil {
		t.Fatal(err)
	}
	pol := teamportal.Policy{
		Domain: "ooda.group", CookiePrefix: "ooda_portal",
		ClientFile: "ooda-portal-oauth.json", KeyFile: "ooda-portal-session.key",
		TokenFile: "ooda-portal-tokens.json", TokenPrefix: "oodatok_",
		AllowExtra: func(email string) bool {
			_, ok := store.EmailOverrides()[email]
			return ok
		},
	}
	store.WithPolicy(pol)
	auth := teamportal.NewAuthPolicy(dataDir, pol)
	h, err := server.PortalHandler(server.PortalOptions{
		Auth: auth, Store: store, AdminEmail: "ben@ooda.group",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, auth, store, teamDir
}

func post(t *testing.T, srv *httptest.Server, c *http.Cookie, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

// addTeamItem creates an item owned by the caller and returns its id (the
// shared write layer's POST /api/team/items).
func addTeamItem(t *testing.T, srv *httptest.Server, c *http.Cookie, title string) string {
	t.Helper()
	res := post(t, srv, c, "POST", "/api/team/items", `{"kind":"task","title":"`+title+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add item: %d", res.StatusCode)
	}
	var it struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&it); err != nil || it.ID == "" {
		t.Fatalf("add item decode: %v", err)
	}
	return it.ID
}

// TWO portals over the ONE shared write layer, in one process, over one
// dataDir: sessions do not cross, and a write through one leaves the other's
// store untouched. This is the property that makes sharing portal.go safe.
func TestSecondPortalInstanceIsIsolated(t *testing.T) {
	dataDir := t.TempDir()

	aionTeam := filepath.Join(t.TempDir(), "aion-team")
	aionStore, err := teamportal.New(aionTeam)
	if err != nil {
		t.Fatal(err)
	}
	aionAuth := teamportal.NewAuthPolicy(dataDir, teamportal.Policy{})
	aionH, err := server.PortalHandler(server.PortalOptions{
		Auth: aionAuth, Store: aionStore, AdminEmail: "benjamin@aion.bio",
	})
	if err != nil {
		t.Fatal(err)
	}
	aionSrv := httptest.NewServer(aionH)
	t.Cleanup(aionSrv.Close)

	oodaSrv, oodaAuth, oodaStore, oodaTeam := oodaShapedPortal(t, dataDir)

	aionCookie, err := aionAuth.SessionCookie("hannah@aion.bio", "Hannah", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	oodaCookie, err := oodaAuth.SessionCookie("ben@ooda.group", "Ben", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// cross-portal sessions are rejected in BOTH directions
	if res := post(t, oodaSrv, aionCookie, "POST", "/api/team/comment",
		`{"item":"prop/x","body":"hi"}`); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("OODA accepted an AION session: %d", res.StatusCode)
	}
	if res := post(t, aionSrv, oodaCookie, "POST", "/api/team/comment",
		`{"item":"aion-bl/x","body":"hi"}`); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("AION accepted an OODA session: %d", res.StatusCode)
	}

	// a real write through OODA lands ONLY in the OODA store
	item := addTeamItem(t, oodaSrv, oodaCookie, "walk 748 N Euclid")
	aionLogBefore, _ := os.ReadFile(filepath.Join(aionTeam, "activity.log"))
	if res := post(t, oodaSrv, oodaCookie, "POST", "/api/team/comment",
		`{"item":"`+item+`","text":"roof looks done"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("OODA comment: %d", res.StatusCode)
	}
	oodaLog, err := os.ReadFile(filepath.Join(oodaTeam, "activity.log"))
	if err != nil || !strings.Contains(string(oodaLog), "roof looks done") {
		t.Fatalf("OODA activity.log: %v %s", err, oodaLog)
	}
	aionLogAfter, _ := os.ReadFile(filepath.Join(aionTeam, "activity.log"))
	if string(aionLogBefore) != string(aionLogAfter) {
		t.Fatal("an OODA write mutated the AION store")
	}
	if len(oodaStore.Ext().Comments) == 0 {
		t.Fatal("OODA store has no comment")
	}
	if len(aionStore.Ext().Comments) != 0 {
		t.Fatal("AION store gained a comment")
	}
}

// A partner with no address under the portal's domain is a first-class member:
// they sign in, comment, and can be PROPOSED TO (the gate the audit found in
// Store.Propose). And their initials come from the roster, never the local part.
func TestAllowListedPartnerIsAFullMember(t *testing.T) {
	srv, auth, store, _ := oodaShapedPortal(t, t.TempDir())
	olga, err := auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	item := addTeamItem(t, srv, olga, "masonry at 751")
	if res := post(t, srv, olga, "POST", "/api/team/comment",
		`{"item":"`+item+`","text":"masonry done"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("allow-listed partner cannot comment: %d", res.StatusCode)
	}
	// /api/me resolves her to OS from emails.json — not "me" from the address
	req, _ := http.NewRequest("GET", srv.URL+"/api/me", nil)
	req.AddCookie(olga)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var me map[string]any
	_ = json.NewDecoder(res.Body).Decode(&me)
	if ini, _ := me["initials"].(string); ini != "OS" {
		t.Fatalf("initials = %q, want OS (the local part 'me' means emails.json was bypassed)", ini)
	}

	ben, err := auth.SessionCookie("ben@ooda.group", "Ben", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res := post(t, srv, ben, "POST", "/api/team/proposals",
		`{"target":"me@olgasobkiv.com","kind":"task","title":"send the window quote"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("proposing to an allow-listed partner: %d", res.StatusCode)
	}
	found := false
	for _, p := range store.Ext().Proposals {
		if p.TargetOwner == "OS" {
			found = true
		}
	}
	if !found {
		t.Fatalf("proposal did not resolve to OS: %+v", store.Ext().Proposals)
	}
	// a stranger is still refused
	if res := post(t, srv, ben, "POST", "/api/team/proposals",
		`{"target":"stranger@yahoo.com","kind":"task","title":"nope"}`); res.StatusCode == http.StatusOK {
		t.Fatal("a stranger was accepted as a proposal target")
	}
}

// One person, two addresses, same initials: they may decide their own
// proposal from EITHER address (the audit's handleDecide finding).
func TestDecideAcceptsTheTargetsOtherAddress(t *testing.T) {
	srv, auth, store, _ := oodaShapedPortal(t, t.TempDir())
	olga, err := auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Olga proposes FOR Ben (target = his admin address)…
	if res := post(t, srv, olga, "POST", "/api/team/proposals",
		`{"target":"ben@ooda.group","kind":"task","title":"walk 4848"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("propose: %d", res.StatusCode)
	}
	var propID string
	for _, p := range store.Ext().Proposals {
		propID = p.ID
	}
	if propID == "" {
		t.Fatal("no proposal")
	}
	// …and Ben decides it signed in with his OTHER, NON-admin address: both
	// map to BA, so the ownerToken arm (not the admin arm) is what passes.
	benAlt, err := auth.SessionCookie("me@benjaminbanderson.com", "Benjamin", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res := post(t, srv, benAlt, "POST", "/api/team/proposals/decide",
		`{"id":"`+propID+`","approve":true}`); res.StatusCode != http.StatusOK {
		t.Fatalf("target's second address got %d — it must be able to decide its own proposal", res.StatusCode)
	}
	// an unrelated member (Brian Fromal) still cannot decide someone else's
	if res := post(t, srv, olga, "POST", "/api/team/proposals",
		`{"target":"ben@ooda.group","kind":"task","title":"second"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("propose 2: %d", res.StatusCode)
	}
	var second string
	for _, p := range store.Ext().Proposals {
		if p.Status == "pending" {
			second = p.ID
		}
	}
	fromal, err := auth.SessionCookie("brian@ooda.group", "Brian Fromal", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res := post(t, srv, fromal, "POST", "/api/team/proposals/decide",
		`{"id":"`+second+`","approve":true}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a third party decided someone else's proposal: %d", res.StatusCode)
	}
}
