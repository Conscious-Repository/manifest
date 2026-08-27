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
	// Normalized like any other value — an env-injected bare value has the
	// same DevTools-copy problem.
	if got := c.Cookie("https://a.substack.com/x"); got != "substack.sid=from-env" {
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

// Curation is deliberate amplification: a signed-in (paid) source mirrors in
// full like any other, because the owner weighed the republishing question at
// the moment he clicked and the subscription's own setting decides. This
// REVERSES the earlier paid-source rule; the attribution header keeping credit
// and traffic pointed home is what he judged sufficient.
func TestCuratedPaidSourceMirrorsInFull(t *testing.T) {
	v := newVault(t)
	feed := serve(t, substackish, nil)
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = feed.Client()

	sub, err := s.Subscribe(context.Background(), feed.URL, "Paid", "", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	// Sign in — a paid source, and it still mirrors in full.
	if err := s.sites.Set(SiteKey(feed.URL), secretCookie); err != nil {
		t.Fatal(err)
	}
	d, _ := s.doc()
	sub, _ = d.Find(sub.ID)
	if got := s.mirrorFor(Item{}, sub); got != MirrorFull {
		t.Errorf("a curated paid source should mirror in full: %q", got)
	}
	// The owner's per-subscription excerpt setting still holds — that choice
	// was his, not the cookie's.
	excerptSub := sub
	excerptSub.Mirror = MirrorExcerpt
	if got := s.mirrorFor(Item{}, excerptSub); got != MirrorExcerpt {
		t.Errorf("mirror:excerpt subscription should stay excerpt: %q", got)
	}
	// A still-partial body has nothing full to publish, whatever the sub says.
	if got := s.mirrorFor(Item{Preview: PreviewPaid}, sub); got != MirrorExcerpt {
		t.Errorf("a preview item must not claim mirror:full: %q", got)
	}

	// And the curated note records full, so the public feed carries the body.
	_ = s.PollNow(context.Background(), sub.ID)
	cards := s.Cards(Query{View: "all"})
	if len(cards) == 0 {
		t.Fatal("no items")
	}
	entry, err := s.Curate(context.Background(), cards[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	note := v.read(t, entry.Path)
	if !strings.Contains(note, "mirror: full") {
		t.Errorf("the curated note does not record mirror:full:\n%s", note)
	}
	out := FeedXML(s.Entries(), PublicConfig{Title: "reading"})
	if !strings.Contains(out, "any</em> position sound reasonable") {
		t.Errorf("the public feed does not carry the paid post inline:\n%s", out)
	}
}

// ⚠ THE FIRST-USE FAILURE. DevTools shows a cookie as NAME and VALUE in
// separate columns, so "copy substack.sid" gets you the value alone. Sent as a
// Cookie header that is malformed, the server ignores it, and the only symptom
// is that nothing changes — which is what happened live.
func TestBarePastedCookieValueIsNormalized(t *testing.T) {
	bare := "s%3AAbCdEf1234567890.QwErTyUiOp" // what the Value column gives you
	cases := map[string]string{
		bare:                           "substack.sid=" + bare,
		"substack.sid=" + bare:         "substack.sid=" + bare, // already a pair
		"Cookie: substack.sid=" + bare: "substack.sid=" + bare, // pasted with the header name
		"a=1; b=2":                     "a=1; b=2",             // several pairs, untouched
		"  " + bare + "  ":             "substack.sid=" + bare, // whitespace
		"":                             "",
	}
	for in, want := range cases {
		if got := normalizeCookie(in); got != want {
			t.Errorf("normalizeCookie(%q)\n = %q\nwant %q", in, got, want)
		}
	}

	// And it applies through the store, including to a value stored before
	// normalization existed.
	c := NewSiteCreds(t.TempDir())
	if err := c.Set("substack.com", bare); err != nil {
		t.Fatal(err)
	}
	if got := c.Cookie("https://a.substack.com/feed"); got != "substack.sid="+bare {
		t.Errorf("stored cookie not normalized: %q", got)
	}
}

// A pasted cookie either works or silently does nothing. VerifySignIn answers
// that in one request pair rather than leaving it to be inferred.
func TestVerifySignIn(t *testing.T) {
	full := strings.Repeat("The whole article, unlocked for a subscriber. ", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if strings.Contains(r.Header.Get("Cookie"), "GOODSESSION") {
			_, _ = w.Write([]byte(`<html><body><article><p>` + full + `</p></article></body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><article><p>` +
			strings.Repeat("A teaser paragraph and nothing else here. ", 5) +
			`</p><p>This post is for paid subscribers.</p></article></body></html>`))
	}))
	defer srv.Close()

	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = srv.Client()
	host := SiteKey(srv.URL)

	d := ParseFeeds("")
	d.Add(Subscription{Title: "Paid", Kind: KindRSS, URL: srv.URL + "/feed"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	sub := s.Subscriptions()[0]
	s.store.Commit(sub.ID, time.Now().UTC(), true,
		[]Item{{ID: "consume:rss:" + sub.ID + ":x", URL: srv.URL + "/p/a", Preview: PreviewPaid, Chars: 40}}, nil, "")

	// No credential yet.
	res, err := s.VerifySignIn(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Reason, "no sign-in") {
		t.Errorf("with no cookie: %+v", res)
	}

	// A cookie that does nothing must be reported as doing nothing.
	_ = s.sites.Set(host, "substack.sid=WRONGVALUE")
	res, _ = s.VerifySignIn(context.Background(), host)
	if res.OK {
		t.Errorf("a useless cookie reported as working: %+v", res)
	}
	if !s.sites.Expired(srv.URL + "/feed") {
		t.Error("a failed check should mark the sign-in as not working")
	}

	// A working one.
	_ = s.sites.Set(host, "substack.sid=GOODSESSION")
	res, _ = s.VerifySignIn(context.Background(), host)
	if !res.OK {
		t.Fatalf("a working cookie reported as broken: %+v", res)
	}
	if res.SignedIn <= res.Anon {
		t.Errorf("counts do not support the verdict: %+v", res)
	}
	if s.sites.Expired(srv.URL + "/feed") {
		t.Error("a successful check should clear the not-working flag")
	}
	// ⚠ The result must never carry the credential.
	if strings.Contains(fmt.Sprintf("%+v", res), "GOODSESSION") {
		t.Errorf("the verify result leaked the cookie: %+v", res)
	}
}
