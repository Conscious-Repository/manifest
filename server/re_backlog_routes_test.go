package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
)

// Portal-minted backlog ids carry a slash ("aion-bl/<slug>"), so the RE routes
// must take the id as a trailing wildcard. While the id sat mid-pattern every
// edit of such a row missed the mux and came back "404 page not found" —
// attaching a rock, checking it off, deciding, deleting.
func TestReBacklogRoutesAcceptSlashIDs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system", "realestate"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := aion.REBacklogSeed + "- [ ] legacy row [kind:: task] [owner:: BA] [status:: open]\n"
	if err := os.WriteFile(filepath.Join(dir, "system", "realestate", "backlog.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	re := aion.NewStore(dir, "system/realestate", testWriteAbs)

	it := &aion.BacklogItem{
		Kind: aion.KindTask, Text: "anderson organization taxes filed",
		Owner: "BA", Status: aion.StatusOpen, Captured: "2026-08-17",
	}
	if err := re.AddItem(it); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(it.ID, "/") {
		t.Fatalf("expected a portal id with a slash, got %q", it.ID)
	}

	h := (&Server{re: re}).Handler()
	post := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}

	// the action that 404'd: attach a rock to the row
	const rock = "ooda-group/operations-health"
	if w := post("/api/re/backlog/update/"+it.ID, `{"rock":"`+rock+`"}`); w.Code != http.StatusOK {
		t.Fatalf("update %q: %d %s", it.ID, w.Code, strings.TrimSpace(w.Body.String()))
	}
	got := re.LoadBacklog().Find(it.ID)
	if got == nil {
		t.Fatalf("item %q vanished", it.ID)
	}
	if got.Rock != rock {
		t.Fatalf("rock = %q, want %q", got.Rock, rock)
	}

	// the pre-wildcard path shape keeps answering for slash-free ids, so a
	// browser holding the old JS is not broken by the move
	var legacyID string
	for _, i := range re.LoadBacklog().Items() {
		if i.Text == "legacy row" {
			legacyID = i.ID
		}
	}
	if legacyID == "" || strings.Contains(legacyID, "/") {
		t.Fatalf("want a slash-free legacy id, got %q", legacyID)
	}
	if w := post("/api/re/backlog/"+legacyID+"/update", `{"owner":"SM"}`); w.Code != http.StatusOK {
		t.Fatalf("legacy update %q: %d %s", legacyID, w.Code, strings.TrimSpace(w.Body.String()))
	}
	if o := re.LoadBacklog().Find(legacyID).Owner; o != "SM" {
		t.Fatalf("legacy owner = %q, want SM", o)
	}
}
