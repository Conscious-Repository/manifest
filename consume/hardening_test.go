package consume

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// ⚠ THE DISMISS TEST. A feed keeps listing a post for months. Without a
// tombstone that outlives the item record, a dismissal is undone by the next
// poll — which is exactly what "gone forever" must not mean.
func TestDismissSurvivesRepollAndPrune(t *testing.T) {
	s := primed(t, "s")
	now := time.Now().UTC()
	it := item("a", now)
	s.Commit("s", now, true, []Item{it, item("b", now)}, nil, "")

	if !s.Mark("s", "a", false, true, now) {
		t.Fatal("dismiss failed")
	}

	// The feed re-delivers everything, as feeds do.
	s.Commit("s", now.Add(time.Hour), true, []Item{item("a", now), item("b", now)}, nil, "")
	for _, x := range s.Items("s") {
		if x.ID == "a" && x.DismissedAt == "" {
			t.Fatal("a re-poll resurrected a dismissed item")
		}
	}

	// A year later the item record itself is pruned — the tombstone must not be.
	later := now.Add(400 * 24 * time.Hour)
	s.Commit("s", later, true, []Item{item("a", now), item("c", later)}, nil, "")
	for _, x := range s.Items("s") {
		if x.ID == "a" {
			t.Fatalf("dismissed item came back after its record aged out: %+v", x)
		}
	}
	if len(s.Items("s")) == 0 {
		t.Fatal("prune took everything")
	}
}

func TestUndismissClearsTheTombstone(t *testing.T) {
	s := primed(t, "s")
	now := time.Now().UTC()
	s.Commit("s", now, true, []Item{item("a", now)}, nil, "")
	s.Mark("s", "a", false, true, now)

	if !s.Undismiss("s", "a", now) {
		t.Fatal("undismiss failed")
	}
	got := s.Items("s")
	if len(got) != 1 || !got[0].Unread() {
		t.Fatalf("undo did not restore the item unread: %+v", got)
	}
	// And a later poll must not re-suppress it.
	s.Commit("s", now.Add(time.Hour), true, []Item{item("a", now)}, nil, "")
	if len(s.Items("s")) != 1 {
		t.Fatal("a stale tombstone re-suppressed the restored item")
	}
}

// ⚠ THE DUPLICATE-FLOOD TEST. ~41% of feeds regenerate their guid on every
// fetch. Identity keys on the canonical link precisely so those feeds do not
// produce a fresh batch of "new" items every hour.
func TestGuidChurnDoesNotDuplicate(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		// Same two posts, brand-new guids each time.
		fmt.Fprintf(w, `<rss><channel><title>Churn</title>
		  <item><title>One</title><link>https://e.com/one</link><guid>rand-%d-a</guid></item>
		  <item><title>Two</title><link>https://e.com/two</link><guid>rand-%d-b</guid></item>
		</channel></rss>`, n, n)
	}))
	defer srv.Close()

	st := primed(t, "churn")
	f := &rssFetcher{hc: srv.Client()}
	sub := Subscription{ID: "churn", Kind: KindRSS, URL: srv.URL}
	for i := 0; i < 3; i++ {
		items, cur, err := f.Fetch(context.Background(), sub, st.Cursors(sub.ID))
		if err != nil {
			t.Fatal(err)
		}
		st.Commit(sub.ID, time.Now().UTC(), true, items, cur, "")
	}
	if got := len(st.Items(sub.ID)); got != 2 {
		t.Fatalf("guid churn produced %d items across 3 polls; want 2", got)
	}
}

// Read state must survive the id migration from the old guid-first scheme:
// Commit adopts the existing id when the normalized link already matches.
func TestLinkDedupePreservesReadState(t *testing.T) {
	s := primed(t, "s")
	now := time.Now().UTC()
	old := Item{ID: "consume:rss:s:oldid", SubID: "s", Title: "One",
		URL: "https://e.com/one?utm_source=feed", PublishedAt: now, FetchedAt: now}
	s.Commit("s", now, true, []Item{old}, nil, "")
	s.Mark("s", old.ID, true, false, now)

	// The same essay arrives under a different id and a cleaner URL.
	fresh := Item{ID: "consume:rss:s:newid", SubID: "s", Title: "One",
		URL: "https://www.e.com/one/", PublishedAt: now, FetchedAt: now}
	s.Commit("s", now.Add(time.Hour), true, []Item{fresh}, nil, "")

	got := s.Items("s")
	if len(got) != 1 {
		t.Fatalf("the same essay was stored twice: %+v", got)
	}
	if got[0].Unread() {
		t.Error("read state lost when the id migrated")
	}
}

func TestMarkAllRead(t *testing.T) {
	s := primed(t, "s")
	now := time.Now().UTC()
	s.Commit("s", now, true, []Item{item("a", now), item("b", now), item("c", now)}, nil, "")
	if n := s.MarkAllRead("s", now); n != 3 {
		t.Fatalf("marked %d, want 3", n)
	}
	for _, it := range s.Items("s") {
		if it.Unread() {
			t.Errorf("%s still unread", it.ID)
		}
	}
	if n := s.MarkAllRead("s", now); n != 0 {
		t.Errorf("second pass marked %d, want 0", n)
	}
}

// ---- scheduling ----

func schedSvc(t *testing.T) *Service {
	t.Helper()
	v := newVault(t)
	return New(t.TempDir(), v.io(), Config{RSSInterval: time.Hour, XInterval: 6 * time.Hour})
}

// A dead feed must not be hammered hourly forever.
func TestBackoffGrowsAndCaps(t *testing.T) {
	s := schedSvc(t)
	sub := Subscription{ID: "s", Kind: KindRSS, URL: "https://e.com/f"}

	if got := s.interval(sub, 0, 0); got != time.Hour {
		t.Errorf("healthy interval: %v", got)
	}
	if got := s.interval(sub, 0, 1); got != 2*time.Hour {
		t.Errorf("after 1 failure: %v", got)
	}
	if got := s.interval(sub, 0, 3); got != 8*time.Hour {
		t.Errorf("after 3 failures: %v", got)
	}
	if got := s.interval(sub, 0, 50); got != maxBackoff {
		t.Errorf("backoff did not cap: %v", got)
	}

	// End to end: failures push the next attempt out; a success resets it.
	now := time.Now().UTC()
	s.store.commit("s", now, false, nil, nil, "boom", PollMeta{})
	s.store.commit("s", now, false, nil, nil, "boom", PollMeta{})
	if s.due(sub, now.Add(2*time.Hour)) {
		t.Error("still due at 2h after two failures (want 4h backoff)")
	}
	if !s.due(sub, now.Add(5*time.Hour)) {
		t.Error("not due at 5h")
	}
	s.store.commit("s", now.Add(5*time.Hour), true, nil, nil, "", PollMeta{})
	if !s.due(sub, now.Add(6*time.Hour+time.Minute)) {
		t.Error("a successful poll should reset the backoff")
	}
}

// A publisher's Retry-After is obeyed to the second.
func TestRetryAfterIsHonoured(t *testing.T) {
	s := schedSvc(t)
	sub := Subscription{ID: "s", Kind: KindRSS, URL: "https://e.com/f"}
	now := time.Now().UTC()
	s.store.commit("s", now, false, nil, nil, "429", PollMeta{RetryAfter: now.Add(3 * time.Hour)})

	if s.due(sub, now.Add(2*time.Hour)) {
		t.Error("polled before the publisher's Retry-After")
	}
	if !s.due(sub, now.Add(4*time.Hour)) {
		t.Error("not due after Retry-After elapsed")
	}
}

func TestParseRetryAfterBothForms(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	if at, ok := parseRetryAfter("120", now); !ok || !at.Equal(now.Add(2*time.Minute)) {
		t.Errorf("delta-seconds: %v %v", at, ok)
	}
	if at, ok := parseRetryAfter("Wed, 21 Oct 2026 07:28:00 GMT", now); !ok || at.IsZero() {
		t.Errorf("http-date: %v %v", at, ok)
	}
	if _, ok := parseRetryAfter("soon", now); ok {
		t.Error("nonsense should not parse")
	}
}

// A feed asking to be polled every 6h gets that, not hourly.
func TestFeedTTLIsHonoured(t *testing.T) {
	body := `<rss><channel><title>Slow</title><ttl>360</ttl>
	  <item><title>One</title><link>https://e.com/1</link><guid>1</guid></item></channel></rss>`
	srv := serve(t, body, nil)
	f := &rssFetcher{hc: srv.Client()}
	if _, _, err := f.Fetch(context.Background(), Subscription{ID: "s", URL: srv.URL}, nil); err != nil {
		t.Fatal(err)
	}
	if f.LastTTL() != 360 {
		t.Fatalf("ttl not read: %d", f.LastTTL())
	}

	s := schedSvc(t)
	sub := Subscription{ID: "s", Kind: KindRSS, URL: srv.URL}
	if got := s.interval(sub, 360*time.Minute, 0); got != 6*time.Hour {
		t.Errorf("ttl not applied: %v", got)
	}
	// A ttl SHORTER than the configured interval must not make us poll faster.
	if got := s.interval(sub, 5*time.Minute, 0); got != time.Hour {
		t.Errorf("a short ttl should not override the configured floor: %v", got)
	}
}

func TestSyUpdatePeriod(t *testing.T) {
	body := `<rss><channel><title>T</title><updatePeriod>daily</updatePeriod><updateFrequency>2</updateFrequency>
	  <item><title>x</title><link>https://e.com/1</link><guid>1</guid></item></channel></rss>`
	srv := serve(t, body, nil)
	f := &rssFetcher{hc: srv.Client()}
	if _, _, err := f.Fetch(context.Background(), Subscription{ID: "s", URL: srv.URL}, nil); err != nil {
		t.Fatal(err)
	}
	if f.LastTTL() != 720 { // twice daily = every 12h
		t.Errorf("sy:updatePeriod not read: %d", f.LastTTL())
	}
}

// ---- tracking parameters ----

func TestCleanLinkStripsTrackingButKeepsMeaning(t *testing.T) {
	cases := map[string]string{
		"https://e.com/post?utm_source=feed&id=7": "https://e.com/post?id=7",
		"https://e.com/post?fbclid=abc":           "https://e.com/post",
		"https://e.com/post#section":              "https://e.com/post",
		"https://e.com/post?p=1&utm_campaign=x":   "https://e.com/post?p=1",
		"https://e.com/post":                      "https://e.com/post",
	}
	for in, want := range cases {
		if got := cleanLink(in); got != want {
			t.Errorf("cleanLink(%q) = %q, want %q", in, got, want)
		}
	}
	// Not a URL at all — leave it alone rather than mangling it.
	if got := cleanLink("not a url"); got != "not a url" {
		t.Errorf("non-url mangled: %q", got)
	}
}

// ---- full-text extraction ----

const articlePage = `<html><head><title>T</title></head><body>
<nav class="site-nav"><a href="/a">Home</a><a href="/b">About</a><a href="/c">Archive</a></nav>
<article class="post-content">
  <h1>The Real Title</h1>
  <p>` + loremA + `</p>
  <p>` + loremB + `</p>
  <p>` + loremC + `</p>
</article>
<aside class="related"><h3>Related</h3><p>Some other post you might like, with a link.</p></aside>
<div class="comments"><p>First! This comment is long enough to be scored otherwise.</p></div>
<footer><p>Copyright notice that is reasonably long so it could compete.</p></footer>
</body></html>`

const (
	loremA = "There is a particular kind of person who can make any position sound reasonable, and we have built a civilization that rewards them handsomely for it, which is the problem this essay is about."
	loremB = "The second paragraph develops the argument at some length, because a real article has paragraphs of real length rather than a single teasing sentence followed by a call to subscribe."
	loremC = "A third paragraph, so that the article container decisively outweighs the furniture around it in any sane scoring scheme that counts paragraph text."
)

func TestReadableExtractsTheArticle(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(articlePage))
	if err != nil {
		t.Fatal(err)
	}
	got := Readable(doc)
	if !strings.Contains(got, "particular kind of person") {
		t.Fatalf("article body not extracted:\n%s", got)
	}
	if !strings.Contains(got, "third paragraph") {
		t.Error("extraction truncated the article")
	}
	for _, furniture := range []string{"Related", "First!", "Copyright", "Archive"} {
		if strings.Contains(got, furniture) {
			t.Errorf("furniture %q survived extraction:\n%s", furniture, got)
		}
	}
}

// Extraction runs through the sanitizer like everything else — a page from the
// open web is the least trusted input in this package.
func TestReadableIsSanitized(t *testing.T) {
	page := `<html><body><article class="post-content">
	  <p>` + loremA + `</p><p>` + loremB + `</p>
	  <script>alert(1)</script><img src=x onerror=alert(1)>
	</article></body></html>`
	doc, _ := html.Parse(strings.NewReader(page))
	got := strings.ToLower(Readable(doc))
	if strings.Contains(got, "<script") || strings.Contains(got, "onerror") {
		t.Fatalf("extraction bypassed the sanitizer:\n%s", got)
	}
}

// ⚠ Extraction may only ever IMPROVE an item. A consent wall or paywall
// extracts to almost nothing, and replacing a decent teaser with "please
// enable JavaScript" would be worse than doing nothing.
func TestFullTextNeverDowngradesAnItem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wall", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div><p>Please enable JavaScript to continue reading this content.</p></div></body></html>`))
	})
	mux.HandleFunc("/good", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(articlePage))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := schedSvc(t)
	s.hc = srv.Client()
	sub := Subscription{ID: "s", Kind: KindRSS, Fulltext: FullTextOn}

	teaser := "A short teaser that the publisher deliberately truncated right here to make you click through to the site."
	items := []Item{
		{ID: "wall", URL: srv.URL + "/wall", Body: "<p>" + teaser + "</p>", Chars: len([]rune(teaser)), Excerpt: teaser, teaser: true},
		{ID: "good", URL: srv.URL + "/good", Body: "<p>" + teaser + "</p>", Chars: len([]rune(teaser)), Excerpt: teaser, teaser: true},
	}
	out := s.fillFullText(context.Background(), sub, items)

	if !strings.Contains(out[0].Body, teaser) {
		t.Errorf("a consent wall replaced a usable teaser:\n%s", out[0].Body)
	}
	if !strings.Contains(out[1].Body, "particular kind of person") {
		t.Errorf("a real article was not fetched:\n%s", out[1].Body)
	}
	if out[1].Chars <= len([]rune(teaser)) {
		t.Error("char count not updated after extraction")
	}
}

// auto fetches only when the feed WITHHELD the article. A short post that
// arrived as content:encoded is short on purpose.
func TestAutoOnlyFetchesTeasers(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(articlePage))
	}))
	defer srv.Close()

	s := schedSvc(t)
	s.hc = srv.Client()
	short := "A genuinely short post, published in full."

	// Not a teaser: came from content:encoded. auto must not fetch.
	s.fillFullText(context.Background(), Subscription{Fulltext: FullTextAuto},
		[]Item{{ID: "a", URL: srv.URL, Body: "<p>" + short + "</p>", Chars: len([]rune(short)), teaser: false}})
	if hits != 0 {
		t.Errorf("auto fetched a full-content short post (%d hits)", hits)
	}

	// off never fetches, even for a teaser.
	s.fillFullText(context.Background(), Subscription{Fulltext: FullTextOff},
		[]Item{{ID: "b", URL: srv.URL, Body: "<p>x</p>", Chars: 1, teaser: true}})
	if hits != 0 {
		t.Errorf("off still fetched (%d hits)", hits)
	}

	// A teaser does get fetched.
	s.fillFullText(context.Background(), Subscription{Fulltext: FullTextAuto},
		[]Item{{ID: "c", URL: srv.URL, Body: "<p>x</p>", Chars: 1, teaser: true}})
	if hits != 1 {
		t.Errorf("auto did not fetch a teaser (%d hits)", hits)
	}
}

func TestFulltextFieldRoundTrips(t *testing.T) {
	d := ParseFeeds("## essays\n- A [id:: a] [kind:: rss] [url:: https://e.com/f] [fulltext:: off]\n")
	sub, ok := d.Find("a")
	if !ok || sub.FullText() != FullTextOff {
		t.Fatalf("fulltext not parsed: %+v", sub)
	}
	// auto is the default and is not written out, keeping ordinary lines clean.
	sub.Fulltext = FullTextAuto
	d.Update(sub)
	if strings.Contains(d.String(), "fulltext") {
		t.Errorf("default written to the line:\n%s", d.String())
	}
	if ParseFeeds(d.String()).String() != d.String() {
		t.Error("not a fixpoint after the edit")
	}
}

// ---- the backfill rule ----

// ⚠ THE FLOOD TEST. Following a feed that serves fifty articles must not put
// fifty articles in the queue. Everything from the first poll is archived.
func TestSubscribeSeedsRatherThanFloods(t *testing.T) {
	var extra string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rss><channel><title>Busy</title>`)
		for i := 0; i < 20; i++ {
			fmt.Fprintf(w, `<item><title>Post %d</title><link>https://e.com/%d</link><guid>%d</guid>
			  <pubDate>Mon, 18 Aug 2026 09:00:00 GMT</pubDate></item>`, i, i, i)
		}
		fmt.Fprint(w, extra)
		fmt.Fprint(w, `</channel></rss>`)
	}))
	defer srv.Close()

	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = srv.Client()

	sub, err := s.Subscribe(context.Background(), srv.URL, "Busy", "", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	if n := s.Unread(""); n != 0 {
		t.Fatalf("subscribe put %d items in the queue; it must put none", n)
	}
	if n := s.Seeded(sub.ID); n != 20 {
		t.Fatalf("archived %d of 20", n)
	}
	// Archived, not read — the distinction the UI depends on to tell the truth.
	all := s.Cards(Query{View: "all"})
	if len(all) != 20 {
		t.Fatalf("archive not browsable: %d", len(all))
	}
	for _, c := range all {
		if !c.Seeded {
			t.Errorf("%s is not flagged seeded", c.ID)
		}
	}

	// A post published AFTER subscribing is the first real unread.
	extra = `<item><title>Brand New</title><link>https://e.com/new</link><guid>new</guid>
	  <pubDate>Tue, 25 Aug 2026 09:00:00 GMT</pubDate></item>`
	if err := s.PollNow(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	unread := s.Cards(Query{View: "unread"})
	if len(unread) != 1 || unread[0].Title != "Brand New" {
		t.Fatalf("want exactly the new post unread, got %+v", unread)
	}
}

// ⚠ THE ARCHIVE-RETENTION TEST. stamp() falls back to the PUBLISHED date when
// no lifecycle timestamp is set, so a seeded post from years ago would be
// pruned the instant it arrived — the archive would be empty for exactly the
// feeds worth browsing.
func TestSeededItemsSurviveRetention(t *testing.T) {
	s := testStore(t) // fresh: this first commit seeds
	now := time.Now().UTC()
	ancient := now.Add(-3 * 365 * 24 * time.Hour)
	s.Commit("s", now, true, []Item{item("ancient", ancient)}, nil, "")

	got := s.Items("s")
	if len(got) != 1 {
		t.Fatalf("a three-year-old seeded post was pruned on arrival: %+v", got)
	}
	if !got[0].Seeded() {
		t.Errorf("not seeded: %+v", got[0])
	}

	// It still ages out eventually — 90 days after WE saw it, not after it was
	// written.
	s.Commit("s", now.Add(100*24*time.Hour), true, nil, nil, "")
	if n := len(s.Items("s")); n != 0 {
		t.Errorf("seeded item never expires: %d", n)
	}
}

func TestSeededSurvivesRepoll(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	s.Commit("s", now, true, []Item{item("a", now)}, nil, "")
	s.Commit("s", now.Add(time.Hour), true, []Item{item("a", now)}, nil, "")

	got := s.Items("s")
	if len(got) != 1 || got[0].Unread() {
		t.Fatalf("a seeded item became unread on re-poll: %+v", got)
	}
}

// Bumping pulls an item out of the archive, and a re-poll must not put it back.
func TestBumpToUnread(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	s.Commit("s", now, true, []Item{item("a", now)}, nil, "")

	if !s.SetUnread("s", "a") {
		t.Fatal("bump failed")
	}
	got := s.Items("s")
	if !got[0].Unread() {
		t.Fatalf("bump did not take: %+v", got[0])
	}
	s.Commit("s", now.Add(time.Hour), true, []Item{item("a", now)}, nil, "")
	if !s.Items("s")[0].Unread() {
		t.Error("a re-poll re-archived a bumped item")
	}

	// A dismissed item is terminal — bumping must not resurrect it.
	s.Mark("s", "a", false, true, now)
	if s.SetUnread("s", "a") {
		t.Error("bump revived a dismissed item; that is undismiss's job")
	}
}

// ---- search and the per-feed archive ----

func searchSvc(t *testing.T) *Service {
	t.Helper()
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{})
	d := ParseFeeds("")
	d.Add(Subscription{Title: "Alpha", Kind: KindRSS, URL: "https://a.example/f", List: "essays"})
	d.Add(Subscription{Title: "Beta", Kind: KindRSS, URL: "https://b.example/f", List: "news"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	mk := func(sub, id, title, excerpt string) Item {
		return Item{ID: "consume:rss:" + sub + ":" + id, SubID: sub, Title: title,
			Excerpt: excerpt, PublishedAt: now, FetchedAt: now}
	}
	s.store.Commit("alpha", now, true, []Item{
		mk("alpha", "1", "Mitochondria and you", "a piece about cell biology"),
		mk("alpha", "2", "Something else entirely", "unrelated musings"),
	}, nil, "")
	s.store.Commit("beta", now, true, []Item{
		mk("beta", "1", "Market report", "MITOCHONDRIA get a mention here"),
	}, nil, "")
	return s
}

func TestSearchAcrossFeeds(t *testing.T) {
	s := searchSvc(t)
	got := s.Cards(Query{View: "all", Q: "mitochondria"})
	if len(got) != 2 {
		t.Fatalf("search matched %d, want 2 (title + excerpt, case-insensitive)", len(got))
	}
	if n := len(s.Cards(Query{View: "all", Q: "MITOCHONDRIA"})); n != 2 {
		t.Errorf("search is case-sensitive: %d", n)
	}
	if n := len(s.Cards(Query{View: "all", Q: "nothing here"})); n != 0 {
		t.Errorf("search matched something it shouldn't: %d", n)
	}
	// Dismissed stays out of search — it is terminal.
	s.Dismiss("consume:rss:alpha:1")
	if n := len(s.Cards(Query{View: "all", Q: "mitochondria"})); n != 1 {
		t.Errorf("a dismissed item is still searchable: %d", n)
	}
}

func TestPerSubscriptionArchive(t *testing.T) {
	s := searchSvc(t)
	if n := len(s.Cards(Query{View: "all", Sub: "alpha"})); n != 2 {
		t.Errorf("alpha's history: %d, want 2", n)
	}
	if n := len(s.Cards(Query{View: "all", Sub: "beta"})); n != 1 {
		t.Errorf("beta's history: %d, want 1", n)
	}
	if n := len(s.Cards(Query{View: "all", Sub: "nope"})); n != 0 {
		t.Errorf("unknown sub returned %d", n)
	}
	// Sub and query compose.
	if n := len(s.Cards(Query{View: "all", Sub: "alpha", Q: "mitochondria"})); n != 1 {
		t.Errorf("sub+query: %d, want 1", n)
	}
	// Every card knows its subscription now.
	for _, c := range s.Cards(Query{View: "all"}) {
		if c.SubID == "" {
			t.Errorf("card has no subId: %+v", c)
		}
	}
}

func TestStatusesCountArchived(t *testing.T) {
	s := searchSvc(t)
	for _, st := range s.Statuses() {
		if st.ID == "alpha" {
			if st.Unread != 0 || st.Archived != 2 || st.Total != 2 {
				t.Errorf("alpha counts: unread=%d archived=%d total=%d", st.Unread, st.Archived, st.Total)
			}
		}
	}
	s.MarkUnread("consume:rss:alpha:1")
	for _, st := range s.Statuses() {
		if st.ID == "alpha" && (st.Unread != 1 || st.Archived != 1) {
			t.Errorf("after bump: unread=%d archived=%d", st.Unread, st.Archived)
		}
	}
}
