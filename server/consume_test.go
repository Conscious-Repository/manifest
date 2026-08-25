package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/consume"
)

// consumeHarness wires a Server with a live CONSUME lane over a temp vault and
// a fake feed, exactly the way main.go does.
type consumeHarness struct {
	srv    *Server
	svc    *consume.Service
	vault  string
	feed   *httptest.Server
	handle http.Handler
}

const testFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
  <title>Test Letter</title>
  <item>
    <title>An Essay</title>
    <link>https://test.example/p/essay</link>
    <guid>essay-1</guid>
    <pubDate>Fri, 21 Aug 2026 14:02:00 GMT</pubDate>
    <content:encoded><![CDATA[<p>The body of the essay.</p><script>alert(1)</script>]]></content:encoded>
  </item>
</channel>
</rss>`

func newConsumeHarness(t *testing.T) *consumeHarness {
	t.Helper()
	vault := t.TempDir()
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testFeed))
	}))
	t.Cleanup(feed.Close)

	svc := consume.New(t.TempDir(), consume.VaultIO{
		Read: func(rel string) ([]byte, error) { return os.ReadFile(filepath.Join(vault, rel)) },
		Write: func(rel string, data []byte) error {
			p := filepath.Join(vault, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			return os.WriteFile(p, data, 0o644)
		},
		List: func(dir string) ([]string, error) {
			entries, err := os.ReadDir(filepath.Join(vault, dir))
			if err != nil {
				return nil, err
			}
			var out []string
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					out = append(out, dir+"/"+e.Name())
				}
			}
			return out, nil
		},
	}, consume.Config{})

	s := New(nil, nil, nil)
	s.UseConsume(svc, filepath.Join(t.TempDir(), "x-creds"), "https://reading.example")
	return &consumeHarness{srv: s, svc: svc, vault: vault, feed: feed, handle: s.Handler()}
}

func (h *consumeHarness) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.handle.ServeHTTP(w, r)
	return w
}

func (h *consumeHarness) subscribe(t *testing.T) consume.Subscription {
	t.Helper()
	sub, err := h.svc.Subscribe(context.Background(), h.feed.URL, "Test Letter", "essays", consume.MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

// The registry contract: consume appears in /api/feed under its own field and
// contributes ZERO to the badge.
func TestConsumeIsRegisteredAndDoesNotBadge(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)

	w := h.do(t, http.MethodGet, "/api/feed", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/api/feed: %d", w.Code)
	}
	var resp struct {
		ConsumeItems []map[string]any `json:"consumeItems"`
		Badge        int              `json:"badge"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.ConsumeItems) != 1 {
		t.Fatalf("consumeItems missing from the feed contract: %s", w.Body.String())
	}
	if resp.ConsumeItems[0]["kind"] != "consume" {
		t.Errorf("card kind: %v", resp.ConsumeItems[0])
	}
	// ⚠ Reading is not attention debt. An unread essay must never make the nav
	// pill nag.
	if resp.Badge != 0 {
		t.Errorf("consume contributed %d to the FEED badge; it must contribute 0", resp.Badge)
	}
	if code := h.do(t, http.MethodGet, "/api/feed/badge", "").Code; code != http.StatusOK {
		t.Errorf("badge route: %d", code)
	}
}

// A consume item must never be reachable through the findings verb routes —
// the per-kind split is what makes "Keep on an essay" structurally impossible.
func TestConsumeItemsCannotUseFindingsVerbs(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)
	id := h.svc.Cards("all", "")[0].ID

	for _, path := range []string{
		"/api/feed/" + id + "/status",
		"/api/feed/" + id + "/save-to-vault",
		"/api/feed/" + id + "/to-task",
	} {
		w := h.do(t, http.MethodPost, path, `{"status":"kept"}`)
		if w.Code == http.StatusOK {
			t.Errorf("%s accepted a consume item; the kinds must stay separate", path)
		}
	}
}

func TestConsumeReaderRoundTrip(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)

	list := h.do(t, http.MethodGet, "/api/consume?view=unread", "")
	var lr struct {
		Items  []map[string]any `json:"items"`
		Lists  []string         `json:"lists"`
		Unread int              `json:"unread"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Items) != 1 || lr.Unread != 1 {
		t.Fatalf("lane: %s", list.Body.String())
	}
	if len(lr.Lists) != 1 || lr.Lists[0] != "essays" {
		t.Errorf("lists: %v", lr.Lists)
	}
	id, _ := lr.Items[0]["id"].(string)

	// Opening the reader returns the sanitized body AND marks it read.
	w := h.do(t, http.MethodGet, "/api/consume/item/"+id, "")
	if w.Code != http.StatusOK {
		t.Fatalf("reader: %d %s", w.Code, w.Body.String())
	}
	var item struct {
		Body    string `json:"body"`
		Curated bool   `json:"curated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(item.Body, "The body of the essay") {
		t.Errorf("body missing: %q", item.Body)
	}
	if strings.Contains(strings.ToLower(item.Body), "<script") {
		t.Errorf("UNSANITIZED body reached the only innerHTML sink in the FEED: %q", item.Body)
	}
	if item.Curated {
		t.Error("not curated yet")
	}
	if h.svc.Unread("") != 0 {
		t.Error("opening the reader should mark the item read")
	}

	// Curate it — a note lands in the vault.
	cw := h.do(t, http.MethodPost, "/api/consume/item/"+id+"/curate", `{"note":"worth your time"}`)
	if cw.Code != http.StatusOK {
		t.Fatalf("curate: %d %s", cw.Code, cw.Body.String())
	}
	var cr struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(cw.Body.Bytes(), &cr)
	if !strings.HasPrefix(cr.Path, "extrinsic/") {
		t.Fatalf("curated note is not in extrinsic/: %q", cr.Path)
	}
	if _, err := os.Stat(filepath.Join(h.vault, cr.Path)); err != nil {
		t.Fatalf("note not on disk: %v", err)
	}

	// The curated list reports it, and the reader now says so.
	var listed struct {
		Entries []map[string]any `json:"entries"`
		Public  string           `json:"public"`
	}
	_ = json.Unmarshal(h.do(t, http.MethodGet, "/api/consume/curated", "").Body.Bytes(), &listed)
	if len(listed.Entries) != 1 {
		t.Fatalf("curated list: %+v", listed)
	}
	if listed.Public != "https://reading.example" {
		t.Errorf("public url not surfaced: %q", listed.Public)
	}

	// Un-curate.
	if code := h.do(t, http.MethodPost, "/api/consume/item/"+id+"/uncurate", "").Code; code != http.StatusOK {
		t.Errorf("uncurate: %d", code)
	}
	_ = json.Unmarshal(h.do(t, http.MethodGet, "/api/consume/curated", "").Body.Bytes(), &listed)
	if len(listed.Entries) != 0 {
		t.Errorf("still curated after uncurate: %+v", listed.Entries)
	}
}

func TestConsumeSubscriptionRoutes(t *testing.T) {
	h := newConsumeHarness(t)

	body := `{"input":"` + h.feed.URL + `","list":"essays","mirror":"full"}`
	w := h.do(t, http.MethodPost, "/api/consume/subscriptions", body)
	if w.Code != http.StatusOK {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body.String())
	}
	var sr struct {
		Subscription consume.Subscription `json:"subscription"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &sr)
	id := sr.Subscription.ID
	if id == "" {
		t.Fatalf("no subscription returned: %s", w.Body.String())
	}
	// The subscription is in the VAULT.
	feeds, err := os.ReadFile(filepath.Join(h.vault, "extrinsic", "feeds.md"))
	if err != nil {
		t.Fatalf("extrinsic/feeds.md not written: %v", err)
	}
	if !strings.Contains(string(feeds), "Test Letter") {
		t.Errorf("subscription line missing:\n%s", feeds)
	}

	var subs struct {
		Subscriptions []map[string]any `json:"subscriptions"`
		XReady        bool             `json:"xReady"`
	}
	_ = json.Unmarshal(h.do(t, http.MethodGet, "/api/consume/subscriptions", "").Body.Bytes(), &subs)
	if len(subs.Subscriptions) != 1 {
		t.Fatalf("subscriptions: %+v", subs)
	}
	if subs.XReady {
		t.Error("X should report sealed with no token")
	}

	if code := h.do(t, http.MethodPost, "/api/consume/subscriptions/"+id+"/update",
		`{"title":"Renamed","list":"ai","mirror":"excerpt"}`).Code; code != http.StatusOK {
		t.Errorf("update: %d", code)
	}
	if code := h.do(t, http.MethodPost, "/api/consume/subscriptions/"+id+"/poll", "").Code; code != http.StatusOK {
		t.Errorf("poll: %d", code)
	}
	if code := h.do(t, http.MethodPost, "/api/consume/subscriptions/"+id+"/remove", "").Code; code != http.StatusOK {
		t.Errorf("remove: %d", code)
	}
	if len(h.svc.Subscriptions()) != 0 {
		t.Error("subscription survived removal")
	}
}

// With no lane wired at all, every route answers rather than panicking.
func TestConsumeRoutesAreSafeWhenUnwired(t *testing.T) {
	s := New(nil, nil, nil)
	h := s.Handler()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/consume"},
		{http.MethodGet, "/api/consume/curated"},
		{http.MethodGet, "/api/consume/subscriptions"},
		{http.MethodGet, "/api/consume/item/consume:rss:a:b"},
		{http.MethodPost, "/api/consume/item/consume:rss:a:b/curate"},
		{http.MethodPost, "/api/consume/subscriptions/x/poll"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}")))
		if w.Code >= 500 {
			t.Errorf("%s %s = %d with no lane wired", tc.method, tc.path, w.Code)
		}
	}
}

// The X row is sealed without a token and says where to get one.
func TestConsumeXPortalRow(t *testing.T) {
	h := newConsumeHarness(t)
	w := h.do(t, http.MethodGet, "/api/portals", "")
	var resp struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	for _, r := range resp.Rows {
		if r["id"] == "x" {
			row = r
		}
	}
	if row == nil {
		t.Fatalf("no X row in the portals panel: %s", w.Body.String())
	}
	if row["state"] != "sealed" {
		t.Errorf("state without a token: %v", row["state"])
	}
	if !strings.Contains(row["note"].(string), "CONSUME") {
		t.Errorf("sealed note should point at the lane: %v", row["note"])
	}

	// Setting a token opens it, and the token never comes back in a response.
	kw := h.do(t, http.MethodPost, "/api/portals/x/key", `{"fields":{"token":"AAAA-secret-token-1234"}}`)
	if kw.Code != http.StatusOK {
		t.Fatalf("set key: %d %s", kw.Code, kw.Body.String())
	}
	if strings.Contains(kw.Body.String(), "AAAA-secret-token-1234") {
		t.Errorf("the token was echoed back: %s", kw.Body.String())
	}
	var after map[string]any
	_ = json.Unmarshal(kw.Body.Bytes(), &after)
	if after["state"] != "open" {
		t.Errorf("state after setting a token: %v", after["state"])
	}
	if after["masked"] != "····1234" {
		t.Errorf("masked: %v", after["masked"])
	}
	if code := h.do(t, http.MethodPost, "/api/portals/x/disconnect", "").Code; code != http.StatusOK {
		t.Errorf("disconnect: %d", code)
	}
}

func TestConsumeCardCarriesReadingTime(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)
	cards := h.svc.Cards("all", "")
	if len(cards) != 1 {
		t.Fatalf("cards: %d", len(cards))
	}
	if cards[0].Minutes < 1 {
		t.Errorf("reading time should never be zero: %+v", cards[0])
	}
	if cards[0].Published == "" {
		t.Error("published date missing")
	}
	if _, err := time.Parse(time.RFC3339, cards[0].Published); err != nil {
		t.Errorf("published is not RFC3339: %q", cards[0].Published)
	}
}

// The new lifecycle routes: undo, mark-all-read, refresh-all.
func TestConsumeBacklogRoutes(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)
	id := h.svc.Cards("all", "")[0].ID

	// Dismiss → gone from every view, not just unread.
	if code := h.do(t, http.MethodPost, "/api/consume/item/"+id+"/dismiss", "").Code; code != http.StatusOK {
		t.Fatalf("dismiss: %d", code)
	}
	if n := len(h.svc.Cards("all", "")); n != 0 {
		t.Errorf("dismissed item still in the all view: %d", n)
	}
	// ⚠ And it stays gone across a re-poll, which is the whole point of the
	// tombstone — feeds keep listing a post long after you are done with it.
	if err := h.svc.PollNow(context.Background(), "test-letter"); err != nil {
		t.Logf("re-poll: %v", err)
	}
	if n := len(h.svc.Cards("all", "")); n != 0 {
		t.Errorf("a re-poll resurrected a dismissed item: %d", n)
	}

	// Undo brings it back, unread.
	if code := h.do(t, http.MethodPost, "/api/consume/item/"+id+"/undismiss", "").Code; code != http.StatusOK {
		t.Fatalf("undismiss: %d", code)
	}
	if h.svc.Unread("") != 1 {
		t.Errorf("undo did not restore it unread: %d", h.svc.Unread(""))
	}

	// Mark all read.
	w := h.do(t, http.MethodPost, "/api/consume/read-all", "")
	if w.Code != http.StatusOK {
		t.Fatalf("read-all: %d", w.Code)
	}
	if h.svc.Unread("") != 0 {
		t.Errorf("mark-all-read left %d unread", h.svc.Unread(""))
	}

	// Refresh all.
	if code := h.do(t, http.MethodPost, "/api/consume/poll-all", "").Code; code != http.StatusOK {
		t.Errorf("poll-all: %d", code)
	}
}
