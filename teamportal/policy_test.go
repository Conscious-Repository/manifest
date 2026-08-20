package teamportal

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// THE behaviour-preservation proof for the whole two-portal refactor: the zero
// Policy must decide every address exactly as the package gate always did.
func TestZeroPolicyMatchesAuthorized(t *testing.T) {
	cases := []string{
		"hannah@aion.bio", "HANNAH@AION.BIO", "  hannah@aion.bio  ",
		"anyone-we-never-heard-of@aion.bio", "hannah@notaion.bio",
		"hannah@aion.bio.evil.com", "aion.bio", "", "benjamin@gmail.com",
	}
	for _, e := range cases {
		if got, want := (Policy{}).Allows(e), Authorized(e); got != want {
			t.Fatalf("Policy{}.Allows(%q) = %v, Authorized = %v", e, got, want)
		}
	}
	if !Authorized("hannah@aion.bio") || Authorized("hannah@notaion.bio") {
		t.Fatal("the gate itself moved")
	}
}

// A portal whose partners have no address under its domain admits exactly the
// addresses its allow-list names — and the closure sees a normalized address,
// because the allow-list is keyed lowercase (it is also the initials map).
func TestPolicyAllowExtraAdmitsListedPartner(t *testing.T) {
	var seen []string
	allowed := map[string]bool{"me@olgasobkiv.com": true}
	p := Policy{Domain: "ooda.group", AllowExtra: func(e string) bool {
		seen = append(seen, e)
		return allowed[e]
	}}
	if !p.Allows("ben@ooda.group") {
		t.Fatal("domain arm must still admit an on-domain address")
	}
	if !p.Allows("  ME@OlgaSobkiv.com ") {
		t.Fatal("allow-listed partner rejected")
	}
	if p.Allows("stranger@yahoo.com") {
		t.Fatal("a stranger must not be admitted")
	}
	for _, e := range seen {
		if e != "me@olgasobkiv.com" && e != "stranger@yahoo.com" {
			t.Fatalf("AllowExtra saw %q — expected lowercased/trimmed input", e)
		}
	}
	// on-domain never reaches the closure (cheapest arm first)
	if len(seen) != 2 {
		t.Fatalf("AllowExtra called %d times, want 2 (never for on-domain)", len(seen))
	}
}

// Two portals in ONE dataDir: different cookie names AND different signing
// keys, so neither can read the other's session. Before the refactor both
// shared one key file and one cookie name.
func TestPolicyNamespacesCookiesAndKeys(t *testing.T) {
	dir := t.TempDir()
	aion := NewAuth(dir)
	ooda := NewAuthPolicy(dir, Policy{
		Domain: "ooda.group", CookiePrefix: "ooda_portal",
		ClientFile: "ooda-portal-oauth.json", KeyFile: "ooda-portal-session.key",
		AllowExtra: func(string) bool { return false },
	})

	aionCookie, err := aion.SessionCookie("hannah@aion.bio", "Hannah", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	oodaCookie, err := ooda.SessionCookie("ben@ooda.group", "Ben", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if aionCookie.Name != "aion_portal_session" {
		t.Fatalf("AION cookie name moved: %s", aionCookie.Name)
	}
	if oodaCookie.Name != "ooda_portal_session" {
		t.Fatalf("OODA cookie name = %s", oodaCookie.Name)
	}

	// distinct key files on disk
	for _, name := range []string{"aion-portal-session.key", "ooda-portal-session.key"} {
		if _, err := os.Stat(filepath.Join(dir, "portals", name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	// cross-portal replay: even carrying the other's cookie value, each auth
	// rejects it (different name AND different key)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "ooda_portal_session", Value: oodaCookie.Value})
	if _, ok := aion.Identify(r); ok {
		t.Fatal("AION accepted an OODA session")
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(&http.Cookie{Name: "aion_portal_session", Value: aionCookie.Value})
	if _, ok := ooda.Identify(r2); ok {
		t.Fatal("OODA accepted an AION session")
	}
	// and the same value under the OTHER portal's cookie name still fails —
	// the signing keys differ, so this is not merely a naming convention
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.AddCookie(&http.Cookie{Name: "ooda_portal_session", Value: aionCookie.Value})
	if _, ok := ooda.Identify(r3); ok {
		t.Fatal("OODA verified a signature made with AION's key")
	}
}

// Two token stores over one dataDir: separate files, separate prefixes, and
// neither resolves the other's plaintext.
func TestTokensNamespacedAndCrossPortalRejected(t *testing.T) {
	dir := t.TempDir()
	aion := NewTokens(dir)
	ooda := NewTokensPolicy(dir, Policy{
		Domain: "ooda.group", TokenFile: "ooda-portal-tokens.json", TokenPrefix: "oodatok_",
		AllowExtra: func(e string) bool { return e == "me@olgasobkiv.com" },
	})

	aionPlain, _, err := aion.Mint(Identity{Email: "hannah@aion.bio", Name: "Hannah"}, "t")
	if err != nil {
		t.Fatal(err)
	}
	oodaPlain, _, err := ooda.Mint(Identity{Email: "me@olgasobkiv.com", Name: "Olga"}, "t")
	if err != nil {
		t.Fatalf("allow-listed partner must be able to mint: %v", err)
	}
	for _, name := range []string{"aion-portal-tokens.json", "ooda-portal-tokens.json"} {
		fi, err := os.Stat(filepath.Join(dir, "portals", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v", name, fi.Mode().Perm())
		}
	}
	if _, ok := ooda.Resolve(aionPlain); ok {
		t.Fatal("OODA resolved an AION token")
	}
	if _, ok := aion.Resolve(oodaPlain); ok {
		t.Fatal("AION resolved an OODA token")
	}
	if _, ok := ooda.Resolve(oodaPlain); !ok {
		t.Fatal("OODA could not resolve its own token")
	}
	// AION still refuses an off-domain identity outright
	if _, _, err := aion.Mint(Identity{Email: "me@olgasobkiv.com"}, "t"); err == nil {
		t.Fatal("AION minted a token for an off-domain address")
	}
}

// The missed call site: Store.Propose gates the proposal TARGET. Without a
// policy on the store, an OODA partner with no ooda.group address could never
// be proposed to.
func TestProposeTargetGateHonorsPolicy(t *testing.T) {
	aionDir, oodaDir := t.TempDir(), t.TempDir()
	aionStore, err := New(aionDir)
	if err != nil {
		t.Fatal(err)
	}
	// AION: unchanged domain gate, and the error text is the original
	_, err = aionStore.Propose(Identity{Email: "hannah@aion.bio"}, "me@olgasobkiv.com", "OS",
		"task", "t", "", "", time.Now())
	if err == nil || err.Error() != "target must be an @aion.bio account" {
		t.Fatalf("AION propose gate: %v", err)
	}
	if _, err := aionStore.Propose(Identity{Email: "hannah@aion.bio"}, "sam@aion.bio", "SM",
		"task", "t", "", "", time.Now()); err != nil {
		t.Fatalf("AION on-domain target rejected: %v", err)
	}

	oodaStore, err := New(oodaDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oodaDir, "emails.json"),
		[]byte(`{"me@olgasobkiv.com":"OS","ben@ooda.group":"BA"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oodaStore.WithPolicy(Policy{Domain: "ooda.group", AllowExtra: func(e string) bool {
		_, ok := oodaStore.EmailOverrides()[e]
		return ok
	}})
	if _, err := oodaStore.Propose(Identity{Email: "ben@ooda.group"}, "me@olgasobkiv.com", "OS",
		"task", "windows quote", "", "", time.Now()); err != nil {
		t.Fatalf("allow-listed partner must be a legal target: %v", err)
	}
	if _, err := oodaStore.Propose(Identity{Email: "ben@ooda.group"}, "stranger@yahoo.com", "XX",
		"task", "t", "", "", time.Now()); err == nil {
		t.Fatal("a stranger must not be a legal target")
	}
	// the allow-list is read LIVE — adding a line admits without a restart
	if err := os.WriteFile(filepath.Join(oodaDir, "emails.json"),
		[]byte(`{"me@olgasobkiv.com":"OS","ben@ooda.group":"BA","new@partner.com":"NP"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := oodaStore.Propose(Identity{Email: "ben@ooda.group"}, "new@partner.com", "NP",
		"task", "t", "", "", time.Now()); err != nil {
		t.Fatalf("a freshly added allow-list entry must work with no restart: %v", err)
	}
}

// Two bridges must never claim or dismiss each other's FEED cards.
func TestBridgesNamespaceTheirCards(t *testing.T) {
	dir := t.TempDir()
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	aion := NewBridge(st, dir, "ben@aion.bio")
	ooda := NewBridgeNamed(st, dir, "ben@ooda.group", "ooda-portal", "https://portal.ooda.group/#work")
	if !aion.Owns("aion-portal:123:comment") || aion.Owns("ooda-portal:123:comment") {
		t.Fatal("aion bridge ownership")
	}
	if !ooda.Owns("ooda-portal:123:comment") || ooda.Owns("aion-portal:123:comment") {
		t.Fatal("ooda bridge ownership")
	}
}
