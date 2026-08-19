package books

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const docsJSON = `{"docs":[
 {"key":"/works/OL1W","title":"The Power Broker","author_name":["Robert A. Caro"],"first_publish_year":1974,"number_of_pages_median":1246},
 {"key":"/works/OL2W","title":"  The   Power Broker  ","author_name":["Robert A. Caro","Someone Else"],"first_publish_year":0,"number_of_pages_median":0},
 {"key":"/works/OL3W","title":"","author_name":["Nobody"]}
]}`

func testService(t *testing.T, h http.HandlerFunc) (*Service, *httptest.Server, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	s := New(t.TempDir())
	s.base = srv.URL
	s.interval = 0 // the limiter is exercised separately; keep the test fast
	return s, srv, &hits
}

func TestSearchNormalizesAndDropsUnusable(t *testing.T) {
	s, _, _ := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "power broker" {
			t.Errorf("query not passed through: %q", got)
		}
		w.Write([]byte(docsJSON))
	})
	got, err := s.Search(context.Background(), "  power   broker ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("a title-less record is not a candidate; got %d", len(got))
	}
	if got[0].Title != "The Power Broker" || got[0].Authors[0] != "Robert A. Caro" {
		t.Fatalf("unexpected first candidate: %+v", got[0])
	}
	if got[0].Year != "1974" || got[0].Pages != 1246 {
		t.Fatalf("year/pages not carried: %+v", got[0])
	}
	if got[1].Year != "" || got[1].Pages != 0 {
		t.Fatalf("unknown year/pages must stay empty, not zero-values on show: %+v", got[1])
	}
	if got[1].Title != "The Power Broker" {
		t.Fatalf("whitespace not collapsed: %q", got[1].Title)
	}
	if got[0].Attribution != Attribution {
		t.Fatal("every result carries its attribution")
	}
}

// A repeated query is answered from the cache — the shelf types fast, and the
// catalogue should not see the same question twice.
func TestSearchCachesByQuery(t *testing.T) {
	s, _, hits := testService(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(docsJSON)) })
	if _, err := s.Search(context.Background(), "Power Broker"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(context.Background(), "power broker"); err != nil { // case-insensitive
		t.Fatal(err)
	}
	if *hits != 1 {
		t.Fatalf("expected one upstream call, got %d", *hits)
	}
	// and it survives a restart: the cache is on disk
	s2 := New(func() string { return s.cachePath[:len(s.cachePath)-len("/books.json")] }())
	s2.base = s.base
	s2.interval = 0
	if _, err := s2.Search(context.Background(), "power broker"); err != nil {
		t.Fatal(err)
	}
	if *hits != 1 {
		t.Fatalf("a warm cache should survive a restart, got %d calls", *hits)
	}
}

// Failure ≠ empty: a provider error must reach the caller as an error, never
// as "no such book".
func TestSearchErrorIsNotEmpty(t *testing.T) {
	s, _, _ := testService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	})
	if _, err := s.Search(context.Background(), "anything"); err == nil {
		t.Fatal("a 502 must be an error")
	}
	if _, err := s.Search(context.Background(), "   "); err == nil {
		t.Fatal("an empty query is refused before any request")
	}
}

func TestWaitTurnSpacesRequests(t *testing.T) {
	s := New(t.TempDir())
	s.interval = 40 * time.Millisecond
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := s.waitTurn(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("three turns should take two intervals, took %v", elapsed)
	}
}

// Ranking: the book they meant, first. Open Library's keyword order routinely
// puts a same-titled obscurity or a "Summary of…" above the canonical record.
func TestRankPutsTheCanonicalBookFirst(t *testing.T) {
	out := []Result{
		{Title: "The power broker", Authors: []string{"Joseph I. Lieberman"}, Year: "1966"},
		{Title: "Summary of The Power Broker", Authors: []string{"Irb Media"}},
		{Title: "The power broker: Robert Moses and the fall of New York", Authors: []string{"Robert A. Caro"}, Year: "1974"},
		{Title: "Unrelated", Authors: nil},
	}
	editions := map[string]int{
		key(out[0]): 2,
		key(out[1]): 1,
		key(out[2]): 61,
		key(out[3]): 99, // popularity must not rescue an irrelevant record
	}
	rank(out, "the power broker", editions)
	if out[0].Authors[0] != "Robert A. Caro" {
		t.Fatalf("the canonical record should lead, got %q by %v", out[0].Title, out[0].Authors)
	}
	if out[len(out)-1].Title != "Unrelated" {
		t.Fatalf("an unmatched record sorts last, got %q", out[len(out)-1].Title)
	}
}

// Naming the author in the query is a real way people search.
func TestRankUsesTheAuthorInTheQuery(t *testing.T) {
	out := []Result{
		{Title: "Comment on a New Yorker profile", Authors: []string{"Robert Moses"}},
		{Title: "The power broker: Robert Moses and the fall of New York", Authors: []string{"Robert A. Caro"}},
	}
	rank(out, "power broker caro", map[string]int{})
	if out[0].Authors[0] != "Robert A. Caro" {
		t.Fatalf("the author named in the query should decide, got %v", out[0].Authors)
	}
}

func TestNormFoldsPunctuation(t *testing.T) {
	if norm("  The Power Broker: Robert Moses! ") != "the power broker robert moses" {
		t.Fatalf("unexpected fold: %q", norm("  The Power Broker: Robert Moses! "))
	}
}
