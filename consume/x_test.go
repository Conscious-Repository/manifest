package consume

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// xServer is a fake X API that records what was actually asked for — the
// billing-shaped assertions below are about the REQUESTS, not the responses.
type xServer struct {
	*httptest.Server
	userCalls int
	queries   []url.Values
	posts     []map[string]any
}

func newXServer(t *testing.T, posts []map[string]any) *xServer {
	t.Helper()
	x := &xServer{posts: posts}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/by/username/", func(w http.ResponseWriter, r *http.Request) {
		x.userCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{"id": "42", "name": "Melissa", "username": "melissa"},
		})
	})
	mux.HandleFunc("/users/42/tweets", func(w http.ResponseWriter, r *http.Request) {
		x.queries = append(x.queries, r.URL.Query())
		out := map[string]any{"data": x.posts}
		if len(x.posts) > 0 {
			out["meta"] = map[string]any{"newest_id": x.posts[0]["id"]}
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	x.Server = httptest.NewServer(mux)
	t.Cleanup(x.Close)
	return x
}

func longPost(id, text string) map[string]any {
	return map[string]any{"id": id, "text": text, "created_at": "2026-08-21T14:02:00.000Z"}
}

func xSub() Subscription {
	return Subscription{ID: "melissa", Kind: KindX, Handle: "melissa", Title: "Melissa"}
}

// ⚠ THE BILLING TEST. X bills per post returned, so a poll without since_id
// re-reads and re-pays for the entire backlog on every tick. This is the
// assertion that keeps the lane from quietly costing money.
func TestXAlwaysSendsSinceIDAfterTheFirstPoll(t *testing.T) {
	body := strings.Repeat("a long essay of real substance. ", 20)
	srv := newXServer(t, []map[string]any{longPost("900", body), longPost("899", body)})
	f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}

	_, cur, err := f.Fetch(context.Background(), xSub(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cur["sinceId"] != "900" {
		t.Fatalf("cursor not advanced: %v", cur)
	}
	if _, _, err = f.Fetch(context.Background(), xSub(), cur); err != nil {
		t.Fatal(err)
	}
	if len(srv.queries) != 2 {
		t.Fatalf("want 2 timeline calls, got %d", len(srv.queries))
	}
	if got := srv.queries[1].Get("since_id"); got != "900" {
		t.Errorf("second poll did not send since_id (every poll would re-bill the backlog): %q", got)
	}
	if srv.queries[0].Get("since_id") != "" {
		t.Errorf("first poll should have no since_id: %v", srv.queries[0])
	}
}

// The first poll must be a bounded sample. max_results=100 across several
// accounts is real money in a single tick.
func TestXFirstPollIsCapped(t *testing.T) {
	srv := newXServer(t, []map[string]any{longPost("1", strings.Repeat("x", 400))})
	f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}
	if _, _, err := f.Fetch(context.Background(), xSub(), nil); err != nil {
		t.Fatal(err)
	}
	if got := srv.queries[0].Get("max_results"); got != "20" {
		t.Errorf("first poll requested %q posts; the backfill must be bounded", got)
	}
	if got := srv.queries[0].Get("exclude"); !strings.Contains(got, "retweets") || !strings.Contains(got, "replies") {
		t.Errorf("retweets/replies not excluded — paying to read other people's posts: %q", got)
	}
}

// The handle→id lookup is billed at the higher user-read rate and its answer
// never changes.
func TestXResolvesTheHandleOnlyOnce(t *testing.T) {
	srv := newXServer(t, []map[string]any{longPost("1", strings.Repeat("x", 400))})
	f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}
	_, cur, _ := f.Fetch(context.Background(), xSub(), nil)
	_, cur, _ = f.Fetch(context.Background(), xSub(), cur)
	_, _, _ = f.Fetch(context.Background(), xSub(), cur)
	if srv.userCalls != 1 {
		t.Errorf("resolved the handle %d times; it is billed and immutable", srv.userCalls)
	}
}

// Even when everything is filtered out, the cursor must advance — otherwise
// the same short posts are re-fetched and re-billed forever.
func TestXCursorAdvancesEvenWhenAllPostsAreFiltered(t *testing.T) {
	srv := newXServer(t, []map[string]any{longPost("500", "too short"), longPost("499", "also short")})
	f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}
	items, cur, err := f.Fetch(context.Background(), xSub(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("short posts should be filtered, got %d", len(items))
	}
	if cur["sinceId"] != "500" {
		t.Errorf("cursor stalled on an all-filtered poll: %v", cur)
	}
}

func TestXLengthFilterIsAtItsBoundaryAndCountsRunes(t *testing.T) {
	// 350 is dropped, 351 kept — and é counts as one character, not two.
	at := strings.Repeat("é", defaultMinChars)
	over := strings.Repeat("é", defaultMinChars+1)
	srv := newXServer(t, []map[string]any{longPost("2", over), longPost("1", at)})
	f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}
	items, _, err := f.Fetch(context.Background(), xSub(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("boundary wrong: want 1 kept, got %d", len(items))
	}
	if items[0].Chars != defaultMinChars+1 {
		t.Errorf("rune count wrong: %d", items[0].Chars)
	}
}

// Long-form posts ride in note_tweet; reading only `text` truncates exactly
// the writing this lane exists to collect.
func TestXPrefersNoteTweetAndExpandsLinks(t *testing.T) {
	post := longPost("1", "truncated…")
	post["note_tweet"] = map[string]any{"text": strings.Repeat("the full essay text. ", 30) + "https://t.co/abc"}
	post["entities"] = map[string]any{"urls": []map[string]any{
		{"url": "https://t.co/abc", "expanded_url": "https://example.com/real-post"},
	}}
	srv := newXServer(t, []map[string]any{post})
	f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}
	items, _, err := f.Fetch(context.Background(), xSub(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	it := items[0]
	if !strings.Contains(it.Body, "full essay text") {
		t.Errorf("note_tweet not preferred over text: %q", it.Body)
	}
	if !strings.Contains(it.Body, "example.com/real-post") {
		t.Errorf("t.co link not expanded: %q", it.Body)
	}
	if it.URL != "https://x.com/melissa/status/1" {
		t.Errorf("permalink: %q", it.URL)
	}
}

// Post text is third-party and gets no exemption for arriving via an API.
//
// The correct outcome is ESCAPING, not stripping: a post is plain text, so
// someone who literally typed "<script>" was writing about markup and should
// see those characters rendered back. What must never happen is that the
// sequence reaches the reader as live markup — so the test asserts the escaped
// form is present AND the raw tag is not.
func TestXBodyEscapesMarkupRatherThanExecutingIt(t *testing.T) {
	post := longPost("1", strings.Repeat("word ", 100)+`<script>alert(1)</script><img src=x onerror=alert(1)>`)
	srv := newXServer(t, []map[string]any{post})
	f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}
	items, _, _ := f.Fetch(context.Background(), xSub(), nil)
	if len(items) != 1 {
		t.Fatal("expected the post")
	}
	body := items[0].Body
	low := strings.ToLower(body)
	if strings.Contains(low, "<script") || strings.Contains(low, "<img") {
		t.Errorf("markup reached the reader as live tags: %q", body)
	}
	if !strings.Contains(low, "&lt;script&gt;") {
		t.Errorf("the author's literal text was deleted instead of escaped: %q", body)
	}
	// The only tags in the output are the ones we generated.
	for _, tag := range extractTags(body) {
		if tag != "p" && tag != "br" && tag != "a" {
			t.Errorf("unexpected tag %q in X body: %q", tag, body)
		}
	}
}

// extractTags lists the element names actually present in a fragment.
func extractTags(fragment string) []string {
	var out []string
	for i := 0; i < len(fragment); i++ {
		if fragment[i] != '<' {
			continue
		}
		j := i + 1
		if j < len(fragment) && fragment[j] == '/' {
			j++
		}
		start := j
		for j < len(fragment) && (fragment[j] >= 'a' && fragment[j] <= 'z' || fragment[j] >= 'A' && fragment[j] <= 'Z' || fragment[j] >= '0' && fragment[j] <= '9') {
			j++
		}
		if j > start {
			out = append(out, strings.ToLower(fragment[start:j]))
		}
	}
	return out
}

// A bad token must say what to do about it, not just fail.
func TestXAuthErrorsAreActionable(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusUnauthorized:    "PORTALS",
		http.StatusForbidden:       "PORTALS",
		http.StatusPaymentRequired: "credits",
		http.StatusTooManyRequests: "rate limit",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		f := &xFetcher{hc: srv.Client(), token: "t", base: srv.URL}
		_, _, err := f.Fetch(context.Background(), xSub(), nil)
		if err == nil {
			t.Errorf("status %d produced no error", status)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("status %d: %q does not mention %q", status, err, want)
		}
		srv.Close()
	}
}

func TestNewestIDHandlesLongNumericIDs(t *testing.T) {
	// X ids overflow int64 as decimal strings, so length has to be compared
	// before lexical order.
	got := newestID([]xPost{{ID: "999"}, {ID: "1000000000000000000000"}, {ID: "1001"}})
	if got != "1000000000000000000000" {
		t.Errorf("newest id wrong: %q", got)
	}
}
