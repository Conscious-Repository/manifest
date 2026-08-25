package consume

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const secretCookie = "substack.sid=s%3ASUPERSECRETSESSIONVALUE.abcd1234"

// ⚠ THE TEST THIS REPO HAS NEVER HAD. The subscription list lives in
// extrinsic/feeds.md — inside the owner's VAULT, a git repo that auto-commits
// and pushes. A credential landing there is unrecallable.
func TestCookieNeverReachesTheVault(t *testing.T) {
	v := newVault(t)
	feed := serve(t, substackish, nil)
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = feed.Client()

	if err := s.sites.Set(SiteKey(feed.URL), secretCookie); err != nil {
		t.Fatal(err)
	}
	sub, err := s.Subscribe(context.Background(), feed.URL, "Paid Thing", "essays", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise every path that rewrites the vault line.
	if err := s.UpdateSub(Subscription{ID: sub.ID, Title: "Renamed", List: "ai", Mirror: MirrorExcerpt}); err != nil {
		t.Fatal(err)
	}
	if err := s.PollNow(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}

	feeds := v.read(t, feedsPath)
	for _, needle := range []string{secretCookie, "SUPERSECRETSESSIONVALUE", "substack.sid"} {
		if strings.Contains(feeds, needle) {
			t.Fatalf("A CREDENTIAL REACHED THE VAULT (%q):\n%s", needle, feeds)
		}
	}
	// And it really is on disk in the secrets tier, 0600.
	if got := s.cookieFor(feed.URL); got != secretCookie {
		t.Fatalf("cookie not retrievable: %q", got)
	}
	fi, err := os.Stat(filepath.Join(s.sites.dir, safeName(SiteKey(feed.URL))+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("credential file mode = %o, want 600", fi.Mode().Perm())
	}
}

// ⚠ A tokenized feed URL would be written to the vault as [url:: …]. Substack's
// podcast private feeds are exactly that shape.
func TestTokenizedFeedURLIsRefused(t *testing.T) {
	v := newVault(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(substackish))
	}))
	defer srv.Close()
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = srv.Client()

	_, err := s.Subscribe(context.Background(),
		srv.URL+"/feed/private/AKIAIOSFODNN7EXAMPLE1234567890abcdefEXAMPLEKEY", "", "", "")
	if err == nil {
		t.Fatal("a tokenized private-feed URL was accepted — it would land in the vault")
	}
	if !strings.Contains(err.Error(), "sign in") {
		t.Errorf("the refusal should point at the alternative: %v", err)
	}
	if len(s.Subscriptions()) != 0 {
		t.Error("a refused subscribe still wrote a subscription")
	}
}

// ⚠ consume's failure paths flow into the process log, a 0644 cache, and the
// API response. None of those are the secrets tier.
func TestURLSecretsAreRedactedFromErrors(t *testing.T) {
	v := newVault(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := New(t.TempDir(), v.io(), Config{})
	s.hc = srv.Client()
	d := ParseFeeds("")
	d.Add(Subscription{Title: "Tokened", Kind: KindRSS, URL: srv.URL + "/feed?auth=SUPERSECRETTOKEN9999"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	sub := s.Subscriptions()[0]
	err := s.PollNow(context.Background(), sub.ID)
	if err == nil {
		t.Fatal("expected the poll to fail")
	}
	if strings.Contains(err.Error(), "SUPERSECRETTOKEN9999") {
		t.Errorf("the token leaked into the error (which is logged): %v", err)
	}
	// …and into the cached status the API ships to the client.
	_, lastErr := s.store.Status(sub.ID)
	if strings.Contains(lastErr, "SUPERSECRETTOKEN9999") {
		t.Errorf("the token leaked into the cached lastErr: %q", lastErr)
	}
	for _, st := range s.Statuses() {
		if strings.Contains(st.LastErr, "SUPERSECRETTOKEN9999") {
			t.Errorf("the token leaked into the API response: %q", st.LastErr)
		}
	}
	if !strings.Contains(lastErr, "500") {
		t.Errorf("redaction ate the actual reason: %q", lastErr)
	}
}

// ⚠ Go copies headers across redirects. Without CheckRedirect, one bounce onto
// another host hands somebody else the owner's whole session.
func TestCookieIsDroppedOnACrossHostRedirect(t *testing.T) {
	var gotCookie string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(substackish))
	}))
	defer other.Close()

	// ⚠ Ports do not scope cookies, so two httptest servers on 127.0.0.1 are
	// the SAME site and the cookie should travel between them. To exercise a
	// real host change, address the second server as "localhost" — same
	// listener, different name, which is exactly the boundary a browser draws.
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)

	var sameHostCookie string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stay" {
			sameHostCookie = r.Header.Get("Cookie")
			_, _ = w.Write([]byte(substackish))
			return
		}
		http.Redirect(w, r, otherURL+"/landed", http.StatusFound)
	}))
	defer origin.Close()

	c := httpClient()
	c.Transport = origin.Client().Transport

	req, _ := http.NewRequest(http.MethodGet, origin.URL+"/go", nil)
	req.Header.Set("Cookie", secretCookie)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if strings.Contains(gotCookie, "SUPERSECRETSESSIONVALUE") {
		t.Fatalf("THE SESSION COOKIE WAS SENT TO ANOTHER HOST: %q", gotCookie)
	}

	// A same-site redirect must KEEP it, or signing in would break on any
	// ordinary http→https or trailing-slash bounce.
	req2, _ := http.NewRequest(http.MethodGet, origin.URL+"/stay", nil)
	req2.Header.Set("Cookie", secretCookie)
	resp2, err := c.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if !strings.Contains(sameHostCookie, "SUPERSECRETSESSIONVALUE") {
		t.Errorf("the cookie was dropped on a same-host request: %q", sameHostCookie)
	}
}

// The whole point: signed in, a paid feed returns full content and the
// truncation machinery simply stops firing.
func TestSignedInFetchGetsTheFullPost(t *testing.T) {
	teaser := `<rss><channel><title>Paid</title><item><title>A Post</title>
	  <link>https://e.com/p/a</link><guid>a</guid>
	  <content:encoded>The opening, and then it stops. Read more</content:encoded></item></channel></rss>`
	full := `<rss><channel><title>Paid</title><item><title>A Post</title>
	  <link>https://e.com/p/a</link><guid>a</guid>
	  <content:encoded>` + strings.Repeat("The whole essay, paid for and delivered. ", 80) +
		`</content:encoded></item></channel></rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Cookie"), "SUPERSECRETSESSIONVALUE") {
			_, _ = w.Write([]byte(full))
			return
		}
		_, _ = w.Write([]byte(teaser))
	}))
	defer srv.Close()

	// Anonymous: a teaser, detected as truncated.
	anon := &rssFetcher{hc: srv.Client()}
	items, _, err := anon.Fetch(context.Background(), Subscription{ID: "p", URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].teaser {
		t.Fatal("setup: the anonymous fetch should look truncated")
	}

	// Signed in: the whole thing, and nothing flags it.
	in := &rssFetcher{hc: srv.Client(), cookie: secretCookie}
	items, _, err = in.Fetch(context.Background(), Subscription{ID: "p", URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].teaser {
		t.Error("a signed-in fetch is still flagged as truncated")
	}
	if items[0].Chars < 2000 {
		t.Errorf("did not get the full post: %d chars", items[0].Chars)
	}
	if !strings.Contains(items[0].Body, "paid for and delivered") {
		t.Errorf("body: %q", items[0].Body)
	}
}

// One cookie covers a whole registrable domain — that is what makes pasting it
// once unlock every publication there.
func TestSiteKeyFollowsCookieScope(t *testing.T) {
	same := []string{
		"https://buildingoptimism.substack.com/feed",
		"https://another.substack.com/feed",
		"https://substack.com/feed",
	}
	for _, u := range same {
		if got := SiteKey(u); got != "substack.com" {
			t.Errorf("SiteKey(%q) = %q, want substack.com", u, got)
		}
	}
	// A publication on its own domain gets its own credential, exactly as a
	// browser would treat it.
	if got := SiteKey("https://www.noahpinion.blog/feed"); got != "noahpinion.blog" {
		t.Errorf("custom domain: %q", got)
	}
	if SiteKey("https://example.com/feed") == "substack.com" {
		t.Error("unrelated host matched substack")
	}
	if SiteKey("not a url") != "" {
		t.Error("garbage should yield no key")
	}
}

func TestSiteCredsRoundTrip(t *testing.T) {
	c := NewSiteCreds(t.TempDir())
	if c.Cookie("https://a.substack.com/feed") != "" {
		t.Fatal("empty store returned a cookie")
	}
	if err := c.Set("substack.com", secretCookie); err != nil {
		t.Fatal(err)
	}
	// Reachable from any host on the domain.
	if c.Cookie("https://b.substack.com/feed") != secretCookie {
		t.Error("domain scope not applied")
	}
	sites := c.Sites()
	if len(sites) != 1 {
		t.Fatalf("sites: %+v", sites)
	}
	if strings.Contains(sites[0].Masked, "SUPERSECRET") {
		t.Errorf("the mask leaks the value: %q", sites[0].Masked)
	}
	if !strings.HasSuffix(sites[0].Masked, secretCookie[len(secretCookie)-4:]) {
		t.Errorf("mask should show the last four: %q", sites[0].Masked)
	}
	// Clearing really clears.
	c.Clear("substack.com")
	if c.Cookie("https://b.substack.com/feed") != "" {
		t.Error("cleared credential still returned")
	}
	if len(c.Sites()) != 0 {
		t.Error("cleared credential still listed")
	}
}

func TestSiteCredsEnvOverride(t *testing.T) {
	c := NewSiteCreds(t.TempDir())
	_ = c.Set("substack.com", "from-file")
	t.Setenv(envKey("substack.com"), "from-env")
	if got := c.Cookie("https://a.substack.com/x"); got != "from-env" {
		t.Errorf("env override not applied: %q", got)
	}
}

// ⚠ Expiry is judged ONLY from a successful poll. A feed that is merely down
// must never tell the owner to re-authenticate.
func TestSessionExpiryJudgement(t *testing.T) {
	c := NewSiteCreds(t.TempDir())
	_ = c.Set("substack.com", secretCookie)
	now := time.Now().UTC()

	// Still previews while signed in → the session is dead.
	c.MarkResult("substack.com", true, now)
	if !c.Expired("https://a.substack.com/feed") {
		t.Error("a signed-in poll returning only previews should mark the session expired")
	}
	// A good poll clears it.
	c.MarkResult("substack.com", false, now)
	if c.Expired("https://a.substack.com/feed") {
		t.Error("a successful poll should clear the expiry")
	}
	// A site with no credential is never "expired".
	c2 := NewSiteCreds(t.TempDir())
	c2.MarkResult("substack.com", true, now)
	if c2.Expired("https://a.substack.com/feed") {
		t.Error("a site with no credential cannot have an expired session")
	}
}

// The service-level half: a network failure says nothing about the session.
func TestFailedPollDoesNotExpireTheSession(t *testing.T) {
	v := newVault(t)
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer fail.Close()

	s := New(t.TempDir(), v.io(), Config{})
	s.hc = fail.Client()
	_ = s.sites.Set(SiteKey(fail.URL), secretCookie)
	d := ParseFeeds("")
	d.Add(Subscription{Title: "Paid", Kind: KindRSS, URL: fail.URL})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	_ = s.PollNow(context.Background(), s.Subscriptions()[0].ID)

	if s.sites.Expired(fail.URL) {
		t.Error("a 500 was read as an expired session — the owner would be told to re-authenticate for nothing")
	}
}

func TestStatusesReportSignInWithoutTheSecret(t *testing.T) {
	v := newVault(t)
	feed := serve(t, substackish, nil)
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = feed.Client()
	_ = s.sites.Set(SiteKey(feed.URL), secretCookie)
	sub, err := s.Subscribe(context.Background(), feed.URL, "Paid", "", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Statuses()
	if len(st) != 1 {
		t.Fatalf("statuses: %+v", st)
	}
	if !st[0].SignedIn {
		t.Error("a subscription with a stored cookie should report signedIn")
	}
	if st[0].Site != SiteKey(feed.URL) {
		t.Errorf("site: %q", st[0].Site)
	}
	blob := fmt.Sprintf("%+v", st)
	if strings.Contains(blob, "SUPERSECRETSESSIONVALUE") {
		t.Fatalf("the status row carries the credential: %s", blob)
	}
	_ = sub
}

// ⚠ Mirroring a PAID post into the public feed republishes what the publisher
// sells. A signed-in source is excerpt-only whatever the subscription says.
func TestPaidSourcesAreNeverFullyMirrored(t *testing.T) {
	v := newVault(t)
	feed := serve(t, substackish, nil)
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = feed.Client()

	sub, err := s.Subscribe(context.Background(), feed.URL, "Paid", "", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.mirrorFor(sub); got != MirrorFull {
		t.Fatalf("a free source with mirror:full should mirror: %q", got)
	}

	// Sign in — now it is a paid source.
	if err := s.sites.Set(SiteKey(feed.URL), secretCookie); err != nil {
		t.Fatal(err)
	}
	d, _ := s.doc()
	sub, _ = d.Find(sub.ID)
	if got := s.mirrorFor(sub); got != MirrorExcerpt {
		t.Errorf("a PAID source was set to mirror in full: %q", got)
	}

	// And the curated note records that.
	_ = s.PollNow(context.Background(), sub.ID)
	cards := s.Cards(Query{View: "all"})
	if len(cards) == 0 {
		t.Fatal("no items")
	}
	entry, err := s.Curate(cards[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	note := v.read(t, entry.Path)
	if !strings.Contains(note, "mirror: excerpt") {
		t.Errorf("the curated note does not record excerpt-only:\n%s", note)
	}
}
