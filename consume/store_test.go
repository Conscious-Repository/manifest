package consume

import (
	"os"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func item(id string, published time.Time) Item {
	return Item{ID: id, SubID: "s", Title: id, PublishedAt: published, FetchedAt: published, Body: "<p>" + id + "</p>"}
}

// THE trap this store exists to avoid. A feed re-delivers every item on every
// poll. portals/ upserts events wholesale because a notice carries no
// lifecycle state — do that here and the next poll marks everything the owner
// already read as unread again, forever.
func TestRepollDoesNotResurrectReadItems(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	items := []Item{item("a", now), item("b", now.Add(-time.Hour))}
	s.Commit("s", now, true, items, nil, "")

	if !s.Mark("s", "a", true, false, now) {
		t.Fatal("mark failed")
	}
	if !s.Mark("s", "b", false, true, now) {
		t.Fatal("dismiss failed")
	}

	// The publisher edits a title; the feed re-delivers both items.
	edited := []Item{item("a", now), item("b", now.Add(-time.Hour))}
	edited[0].Title = "a (updated)"
	s.Commit("s", now.Add(time.Minute), true, edited, nil, "")

	got := s.Items("s")
	for _, it := range got {
		if it.Unread() {
			t.Errorf("item %s came back unread after a re-poll", it.ID)
		}
	}
	if got[0].Title != "a (updated)" {
		t.Errorf("content update did not land: %q", got[0].Title)
	}
}

// failure ≠ empty: a failed poll keeps everything and records why.
func TestFailedPollKeepsTheCache(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	s.Commit("s", now, true, []Item{item("a", now)}, map[string]string{"etag": `"v1"`}, "")

	s.Commit("s", now.Add(time.Hour), false, nil, nil, "500 Server Error")

	if n := len(s.Items("s")); n != 1 {
		t.Fatalf("failed poll emptied the cache: %d items", n)
	}
	if _, err := s.Status("s"); err != "500 Server Error" {
		t.Errorf("failure reason not recorded: %q", err)
	}
	if s.Cursors("s")["etag"] != `"v1"` {
		t.Error("failed poll destroyed the cursor")
	}

	// Recovery clears the degraded reason.
	s.Commit("s", now.Add(2*time.Hour), true, []Item{item("a", now)}, nil, "")
	if _, err := s.Status("s"); err != "" {
		t.Errorf("error not cleared after a good poll: %q", err)
	}
}

// Unread reading is a promise, not a notice: it does not expire. What the
// owner already handled does.
func TestPruneKeepsUnreadAndExpiresHandled(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	old := now.Add(-200 * 24 * time.Hour)

	s.Commit("s", old, true, []Item{item("ancient-unread", old), item("ancient-read", old)}, nil, "")
	s.Mark("s", "ancient-read", true, false, old)

	// A later poll triggers the prune.
	s.Commit("s", now, true, []Item{item("fresh", now)}, nil, "")

	ids := map[string]bool{}
	for _, it := range s.Items("s") {
		ids[it.ID] = true
	}
	if !ids["ancient-unread"] {
		t.Error("an unread item expired — subscribed reading must not silently vanish")
	}
	if ids["ancient-read"] {
		t.Error("a read item from 200 days ago should have aged out")
	}
	if !ids["fresh"] {
		t.Error("lost the new item")
	}
}

func TestUnreadBacklogIsCapped(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	many := make([]Item, 0, unreadCap+50)
	for i := 0; i < unreadCap+50; i++ {
		many = append(many, item(string(rune('a'+i%26))+"-"+time.Duration(i).String(), now.Add(-time.Duration(i)*time.Minute)))
	}
	s.Commit("s", now, true, many, nil, "")
	if n := len(s.Items("s")); n > unreadCap {
		t.Errorf("unread backlog not capped: %d > %d", n, unreadCap)
	}
	// The newest survive, not an arbitrary slice.
	if s.Items("s")[0].PublishedAt.Before(now.Add(-time.Minute)) {
		t.Error("cap dropped the newest items instead of the oldest")
	}
}

// Bodies live outside the cache so the lane render never parses them.
func TestBodiesAreSeparateFromTheCache(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	s.Commit("s", now, true, []Item{item("a", now)}, nil, "")

	raw, err := os.ReadFile(s.cacheFile("s"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" {
		t.Fatal("no cache written")
	}
	if got := string(raw); len(got) > 0 && contains(got, "<p>a</p>") {
		t.Error("body was inlined into the cache file")
	}
	if body := s.Body("a"); body != "<p>a</p>" {
		t.Errorf("body not retrievable: %q", body)
	}
	got, ok := s.Get("s", "a")
	if !ok || got.Body != "<p>a</p>" {
		t.Errorf("Get did not load the body: %+v", got)
	}
	// A missing snapshot degrades to empty, never to an error.
	if s.Body("nope") != "" {
		t.Error("missing body should be empty")
	}
}

func TestForgetRemovesEverythingItOwned(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC()
	s.Commit("s", now, true, []Item{item("a", now)}, nil, "")
	if s.Body("a") == "" {
		t.Fatal("setup: no body")
	}
	s.Forget("s")
	if len(s.Items("s")) != 0 {
		t.Error("cache survived Forget")
	}
	if s.Body("a") != "" {
		t.Error("body survived Forget")
	}
}

// A hand-edited vault line can carry any id; it must not be able to write
// outside the cache directory.
func TestSubIDsCannotEscapeTheCacheDir(t *testing.T) {
	s := testStore(t)
	for _, bad := range []string{"../../etc/passwd", "a/b", "..", ""} {
		got := s.cacheFile(bad)
		if contains(got, "..") || contains(got, "/etc/") {
			t.Errorf("id %q escaped: %s", bad, got)
		}
	}
}

func contains(h, n string) bool {
	return len(n) == 0 || len(h) >= len(n) && (func() bool {
		for i := 0; i+len(n) <= len(h); i++ {
			if h[i:i+len(n)] == n {
				return true
			}
		}
		return false
	})()
}
