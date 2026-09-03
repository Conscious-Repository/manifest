package consume

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func liveSvc(t *testing.T) (*Service, *fakeVault, *httptest.Server) {
	t.Helper()
	v := newVault(t)
	feed := serve(t, substackish, nil)
	s := New(t.TempDir(), v.io(), Config{})
	s.hc = feed.Client()
	return s, v, feed
}

func TestSubscribeThenReadTheLane(t *testing.T) {
	s, v, feed := liveSvc(t)

	sub, err := s.Subscribe(context.Background(), feed.URL, "", "essays", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Title != "Melissa's Newsletter" {
		t.Errorf("feed title not adopted as the default name: %q", sub.Title)
	}
	if sub.List != "essays" {
		t.Errorf("list: %q", sub.List)
	}
	// The subscription is in the VAULT, not in dataDir.
	if !strings.Contains(v.read(t, feedsPath), "Melissa") {
		t.Errorf("subscription not written to extrinsic/feeds.md:\n%s", v.read(t, feedsPath))
	}

	// ⚠ THE BACKFILL RULE. Subscribing archives what the feed already
	// published — it must not drop a feed's whole back catalogue into the
	// queue. Zero unread here is correct, not a bug.
	if n := s.Unread(""); n != 0 {
		t.Fatalf("subscribing flooded the queue with %d unread", n)
	}
	if n := s.Seeded(sub.ID); n != 2 {
		t.Fatalf("want 2 archived on subscribe, got %d", n)
	}
	if n := len(s.Cards(Query{View: "all"})); n != 2 {
		t.Fatalf("archived items should still be browsable, got %d", n)
	}

	// Bump them into the queue so the rest of the lifecycle can be exercised.
	for _, c := range s.Cards(Query{View: "all"}) {
		if !s.MarkUnread(c.ID) {
			t.Fatalf("bump %s failed", c.ID)
		}
	}
	if err := s.PollNow(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	cards := s.Cards(Query{View: "unread", List: ""})
	if len(cards) != 2 {
		t.Fatalf("want 2 cards after bumping, got %d", len(cards))
	}
	c := cards[0]
	if c.Kind != "consume" || c.Type != KindRSS {
		t.Errorf("card kind/type: %+v", c)
	}
	if c.Source != "Melissa's Newsletter" || c.List != "essays" {
		t.Errorf("card provenance: %+v", c)
	}
	if c.Minutes < 1 {
		t.Errorf("reading time should always be at least a minute: %+v", c)
	}
	if c.Curated {
		t.Error("a freshly polled card should not be curated")
	}
	if s.Unread("") != 2 {
		t.Errorf("unread count: %d", s.Unread(""))
	}

	// Read one; it leaves the unread view but stays in "all".
	if !s.MarkRead(cards[0].ID) {
		t.Fatal("mark read failed")
	}
	if n := len(s.Cards(Query{View: "unread", List: ""})); n != 1 {
		t.Errorf("read item still in the unread lane: %d", n)
	}
	if n := len(s.Cards(Query{View: "all", List: ""})); n != 2 {
		t.Errorf("read item missing from the all view: %d", n)
	}
	if s.Unread("") != 1 {
		t.Errorf("unread count after reading: %d", s.Unread(""))
	}

	// Dismiss the other. ⚠ Dismissed means gone from EVERY view, not just
	// unread — the bug the owner hit on 2026-08-25 was that it came back in ALL.
	if !s.Dismiss(cards[1].ID) {
		t.Fatal("dismiss failed")
	}
	if n := len(s.Cards(Query{View: "unread", List: ""})); n != 0 {
		t.Errorf("dismissed item still showing in unread: %d", n)
	}
	if n := len(s.Cards(Query{View: "all", List: ""})); n != 1 {
		t.Errorf("dismissed item came back in the all view: %d", n)
	}

	// Undo restores it, unread.
	if !s.Undismiss(cards[1].ID) {
		t.Fatal("undismiss failed")
	}
	if n := len(s.Cards(Query{View: "unread", List: ""})); n != 1 {
		t.Errorf("undo did not restore the item: %d", n)
	}
	s.Dismiss(cards[1].ID)

	// List filter.
	if n := len(s.Cards(Query{View: "all", List: "essays"})); n != 1 {
		t.Errorf("list filter dropped items: %d", n)
	}
	if n := len(s.Cards(Query{View: "all", List: "nonexistent"})); n != 0 {
		t.Errorf("list filter matched the wrong group: %d", n)
	}
	if lists := s.Lists(); len(lists) != 1 || lists[0] != "essays" {
		t.Errorf("lists: %v", lists)
	}
}

func TestSubscribeRejectsDuplicates(t *testing.T) {
	s, _, feed := liveSvc(t)
	if _, err := s.Subscribe(context.Background(), feed.URL, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(context.Background(), feed.URL+"/", "", "", ""); err == nil {
		t.Error("subscribing twice to the same feed should be refused")
	}
	if _, err := s.Subscribe(context.Background(), "  ", "", "", ""); err == nil {
		t.Error("empty input should be refused")
	}
}

// An @handle routes through the self-hosted RSSHub and becomes an ORDINARY
// RSS subscription — no X API, no token, the existing poller end to end
// (decision 2026-08-27; the native KindX path needs a paid bearer token).
func TestSubscribeAnXHandleRoutesThroughRSSHub(t *testing.T) {
	v := newVault(t)
	hub := serve(t, substackish, nil) // plays RSSHub: any path yields a feed
	s := New(t.TempDir(), v.io(), Config{RSSHubBase: hub.URL + "/"})
	s.hc = hub.Client()

	sub, err := s.Subscribe(context.Background(), "@melissa", "", "people", "")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Kind != KindRSS || sub.Handle != "" {
		t.Fatalf("an @handle should become a plain RSS subscription: %+v", sub)
	}
	if sub.URL != hub.URL+"/twitter/user/melissa" {
		t.Fatalf("not routed through RSSHub: %q", sub.URL)
	}
	if sub.Title != "@melissa" {
		t.Errorf("default title should stay the handle, not the bridge feed's: %q", sub.Title)
	}
	// It polled on subscribe like any feed — with no X token anywhere.
	if n := s.Seeded(sub.ID); n != 2 {
		t.Errorf("want 2 items seeded through the bridge, got %d", n)
	}
	// Same handle again, with or without the @, is the same source.
	if _, err := s.Subscribe(context.Background(), "melissa", "", "", ""); err == nil {
		t.Error("re-subscribing the same handle should be refused")
	}
}

// A bridge post is whole by construction — completing it against an x.com
// link can never beat the feed body, and the loop used to conclude "partial"
// about every post, which flagged the whole free account as PAID. The rule is
// enforced at read time too, so stored labels from before the rule stay dead.
func TestRSSHubPostsAreNeverPreviewsOrPaid(t *testing.T) {
	v := newVault(t)
	hub := serve(t, substackish, nil)
	s := New(t.TempDir(), v.io(), Config{RSSHubBase: hub.URL})
	s.hc = hub.Client()

	sub, err := s.Subscribe(context.Background(), "@melissa", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Plant the mislabel an earlier build wrote to the cache.
	items := s.store.Items(sub.ID)
	for i := range items {
		items[i].Preview = PreviewPartial
	}
	s.store.commit(sub.ID, s.now(), true, items, nil, "", PollMeta{})

	for _, c := range s.Cards(Query{View: "all"}) {
		if c.Preview != "" {
			t.Errorf("an X post can never be a preview: %+v", c)
		}
		if c.Type != KindX {
			t.Errorf("a bridge item should read as an X post, got type %q", c.Type)
		}
		if it, _, ok := s.Get(c.ID); !ok || it.Preview != "" {
			t.Errorf("the reader still says preview: %+v", it)
		}
	}
	for _, st := range s.Statuses() {
		if st.ID == sub.ID && st.Paid {
			t.Error("a free X account presented as a paid publication")
		}
	}
}

// A hand-edited [kind:: x] line still takes the native API path, which stays
// token-gated: without one the poll must fail actionably, not silently.
func TestHandEditedXKindStillWantsAToken(t *testing.T) {
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{})
	d := ParseFeeds("")
	d.Add(Subscription{Title: "@melissa", Kind: KindX, Handle: "melissa", List: "people"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	sub := s.Subscriptions()[0]
	err := s.PollNow(context.Background(), sub.ID)
	if err == nil || !strings.Contains(err.Error(), "PORTALS") {
		t.Errorf("sealed X portal should say where to fix it: %v", err)
	}
	if _, lastErr := s.store.Status(sub.ID); lastErr == "" {
		t.Error("the failure should be recorded as the subscription's degraded reason")
	}
}

func TestUnsubscribeForgetsTheCacheButNotTheVault(t *testing.T) {
	s, v, feed := liveSvc(t)
	sub, _ := s.Subscribe(context.Background(), feed.URL, "", "", MirrorFull)
	_ = s.PollNow(context.Background(), sub.ID)
	if len(s.Cards(Query{View: "all", List: ""})) == 0 {
		t.Fatal("setup: no items")
	}
	// Curate one first — that note must survive unsubscribing.
	cards := s.Cards(Query{View: "all", List: ""})
	entry, err := s.Curate(context.Background(), cards[0].ID, "keeping this")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Unsubscribe(sub.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Subscriptions()) != 0 {
		t.Error("subscription line survived")
	}
	if len(s.store.Items(sub.ID)) != 0 {
		t.Error("cache survived")
	}
	if v.read(t, entry.Path) == "" {
		t.Error("unsubscribing deleted a curated note — that note is the owner's")
	}
	if err := s.Unsubscribe("nope"); err == nil {
		t.Error("unsubscribing an unknown id should error")
	}
}

func TestUpdateSubEditsOnlyOwnerFields(t *testing.T) {
	s, _, feed := liveSvc(t)
	sub, _ := s.Subscribe(context.Background(), feed.URL, "", "essays", MirrorFull)

	err := s.UpdateSub(Subscription{
		ID: sub.ID, Title: "Renamed", List: "ai", Mirror: MirrorExcerpt, MinChars: 500,
		URL: "https://attacker.example/feed", // must be ignored
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.doc()
	if err != nil {
		t.Fatal(err)
	}
	cur, found := d.Find(sub.ID)
	if !found {
		t.Fatal("subscription vanished")
	}
	if cur.Title != "Renamed" || cur.List != "ai" || cur.Mirrors() {
		t.Errorf("owner edits did not apply: %+v", cur)
	}
	if cur.URL != feed.URL {
		t.Errorf("the source URL was repointed by an update: %q", cur.URL)
	}
}

// The rollback story: without a write capability the lane must degrade
// gracefully, not panic or half-write.
func TestServiceIsInertWithoutAWriteCapability(t *testing.T) {
	v := newVault(t)
	io := v.io()
	io.Write = nil
	// The fake hub keeps the @handle path off the network: discovery succeeds,
	// so the failure below is the write capability and nothing else.
	hub := serve(t, substackish, nil)
	s := New(t.TempDir(), io, Config{RSSHubBase: hub.URL})
	s.hc = hub.Client()

	if _, err := s.Subscribe(context.Background(), "@someone", "", "", ""); err == nil {
		t.Error("subscribing without a write capability should fail loudly")
	}
	if _, err := s.Curate(context.Background(), "consume:rss:a:b", ""); err == nil {
		t.Error("curating without a write capability should fail loudly")
	}
	// Reads still work and return nothing rather than panicking.
	if len(s.Cards(Query{View: "all", List: ""})) != 0 || s.Unread("") != 0 || len(s.Curated()) != 0 {
		t.Error("read paths should be empty, not broken")
	}
}

// A failing feed must show as degraded, not as an empty subscription.
func TestPollFailureSurfacesInTheManagePanel(t *testing.T) {
	v := newVault(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()

	s := New(t.TempDir(), v.io(), Config{})
	s.hc = bad.Client()
	d := ParseFeeds("")
	d.Add(Subscription{Title: "Broken", Kind: KindRSS, URL: bad.URL})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	sub := s.Subscriptions()[0]
	_ = s.PollNow(context.Background(), sub.ID)

	st := s.Statuses()
	if len(st) != 1 {
		t.Fatalf("want 1 status row, got %d", len(st))
	}
	if st[0].LastErr == "" {
		t.Error("a broken feed must say so, not just go quiet")
	}
	if st[0].LastOK != "" {
		t.Error("a never-succeeded feed should have no lastOK")
	}
}

func TestDueRespectsIntervals(t *testing.T) {
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{RSSInterval: time.Hour, XInterval: 6 * time.Hour})
	now := time.Now().UTC()
	sub := Subscription{ID: "s", Kind: KindRSS, URL: "https://e.com/f"}

	if !s.due(sub, now) {
		t.Error("a never-polled subscription is due")
	}
	s.store.Commit("s", now, true, nil, nil, "")
	if s.due(sub, now.Add(30*time.Minute)) {
		t.Error("polled 30m ago with a 1h interval should not be due")
	}
	if !s.due(sub, now.Add(2*time.Hour)) {
		t.Error("polled 2h ago with a 1h interval should be due")
	}
	// X is on its own, slower clock — it costs money per poll.
	xs := Subscription{ID: "s", Kind: KindX, Handle: "h"}
	if s.due(xs, now.Add(2*time.Hour)) {
		t.Error("X should not poll hourly")
	}
	if !s.due(xs, now.Add(7*time.Hour)) {
		t.Error("X should poll after its own interval")
	}
}

func TestSubOfParsesItemIDs(t *testing.T) {
	id := itemID(KindRSS, "melissa", "guid")
	got, ok := subOf(id)
	if !ok || got != "melissa" {
		t.Errorf("subOf(%q) = %q, %v", id, got, ok)
	}
	for _, bad := range []string{"", "nope", "consume:rss", "other:rss:a:b", "consume:rss:a:b:c"} {
		if _, ok := subOf(bad); ok {
			t.Errorf("subOf accepted %q", bad)
		}
	}
}

// "unfiled" is the scaffold's heading for ungrouped feeds, not a group the
// owner named. It must never become a filter chip.
func TestUnfiledIsNotAGroup(t *testing.T) {
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{})
	d := ParseFeeds("")
	d.Add(Subscription{Title: "No Group", Kind: KindRSS, URL: "https://e.com/f"})
	d.Add(Subscription{Title: "Real Group", Kind: KindRSS, URL: "https://e.com/g", List: "essays"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	lists := s.Lists()
	for _, l := range lists {
		if strings.EqualFold(l, ungrouped) {
			t.Errorf("the scaffold heading leaked into the filter chips: %v", lists)
		}
	}
	if len(lists) != 1 || lists[0] != "essays" {
		t.Errorf("lists: %v", lists)
	}
}

// A Nostr naddr in a feed URL is a public ADDRESS, not a credential — the
// vault-protecting secret guard must not refuse it (live refusal 2026-09-03:
// drss.io/rss/naddr1…). A URL carrying an actual token assignment stays
// refused.
func TestSubscribeAllowsNostrAddressRefusesRealTokens(t *testing.T) {
	s, v, feed := liveSvc(t)

	naddr := "/rss/naddr1qqxkgunnwvkhqmmyvdshxarnqy28wumn8ghj7un9d3shjtnwdaehgu3wd9hsygxasx5t4jatpdwrqp73vuhmsvqnsw6wjkpagvvrtxzs2u3rav5c55psgqqqw56qcr2h9e"
	sub, err := s.Subscribe(context.Background(), feed.URL+naddr, "", "", "")
	if err != nil {
		t.Fatalf("naddr subscribe refused: %v", err)
	}
	if !strings.Contains(sub.URL, "naddr1") {
		t.Fatalf("subscription lost the address: %q", sub.URL)
	}
	if !strings.Contains(v.read(t, feedsPath), "naddr1") {
		t.Fatal("subscription not written to the vault list")
	}

	// the guard still refuses a URL with a credential assignment in it
	if _, err := s.Subscribe(context.Background(),
		feed.URL+"/feed?api_key=sk9F2jQ7xWm4bTz8LpV3hYr6", "", "", ""); err == nil {
		t.Fatal("a tokened URL must still be refused")
	}
}
