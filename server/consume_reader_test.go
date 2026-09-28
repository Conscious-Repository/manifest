package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/consume"
)

// The reader pass (2026-09-27): paging, peek, Watch Later, source switches and
// the share sheet, at the HTTP boundary the browser actually uses.

func readerJSON(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	return out
}

func TestConsumeListPagesAndCarriesNav(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)
	// A second source (the harness serves its feed on every path) so there
	// are two items to page through.
	if _, err := h.svc.Subscribe(context.Background(), h.feed.URL+"/second", "Second Letter", "", consume.MirrorFull); err != nil {
		t.Fatal(err)
	}
	total := len(h.svc.Cards(consume.Query{View: "all"}))
	if total < 2 {
		t.Fatalf("the test feed should carry at least two items, got %d", total)
	}
	w := h.do(t, http.MethodGet, "/api/consume?view=all&limit=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	got := readerJSON(t, w.Body.String())
	if items := got["items"].([]any); len(items) != 1 || got["more"] != true || int(got["count"].(float64)) != total {
		t.Errorf("first page: %d items, more=%v, count=%v", len(items), got["more"], got["count"])
	}
	if _, ok := got["nav"].(map[string]any); !ok {
		t.Error("the list carries the sidebar counts, so switching views costs one request")
	}
	w = h.do(t, http.MethodGet, "/api/consume?view=all&limit=50&offset=1", "")
	got = readerJSON(t, w.Body.String())
	if items := got["items"].([]any); len(items) != total-1 || got["more"] != false {
		t.Errorf("second page: %d items, more=%v", len(items), got["more"])
	}
	// No limit keeps the old contract: everything.
	w = h.do(t, http.MethodGet, "/api/consume?view=all", "")
	if items := readerJSON(t, w.Body.String())["items"].([]any); len(items) != total {
		t.Errorf("an unpaged list returns everything: %d of %d", len(items), total)
	}
}

func TestConsumePeekDoesNotMarkRead(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)
	id := h.bump(t)
	path := "/api/consume/item/" + url.PathEscape(id)
	if w := h.do(t, http.MethodGet, path+"?peek=1", ""); w.Code != http.StatusOK {
		t.Fatalf("peek: %d", w.Code)
	}
	if h.svc.Unread("") != 1 {
		t.Fatal("prefetching ahead must not mark anything read")
	}
	if w := h.do(t, http.MethodGet, path, ""); w.Code != http.StatusOK {
		t.Fatalf("open: %d", w.Code)
	}
	if h.svc.Unread("") != 0 {
		t.Error("opening an item still marks it read")
	}
}

func TestConsumeLaterEndpoints(t *testing.T) {
	h := newConsumeHarness(t)
	h.subscribe(t)
	id := h.svc.Cards(consume.Query{View: "all"})[0].ID
	w := h.do(t, http.MethodPost, "/api/consume/later", `{"item":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	entry := readerJSON(t, w.Body.String())["entry"].(map[string]any)
	laterID := entry["id"].(string)

	list := readerJSON(t, h.do(t, http.MethodGet, "/api/consume?view=later", "").Body.String())
	if items := list["items"].([]any); len(items) != 1 {
		t.Fatalf("queue: %d", len(items))
	}
	if nav := list["nav"].(map[string]any); nav["later"].(float64) != 1 {
		t.Errorf("nav counts the queue: %v", nav["later"])
	}
	// The item route answers with the queue state, so the reader can say so.
	item := readerJSON(t, h.do(t, http.MethodGet, "/api/consume/item/"+url.PathEscape(id)+"?peek=1", "").Body.String())
	if item["later"] != true || item["laterId"] != laterID {
		t.Errorf("item carries its queue state: %v %v", item["later"], item["laterId"])
	}
	if w := h.do(t, http.MethodPost, "/api/consume/later/"+laterID+"/done", ""); w.Code != http.StatusOK {
		t.Fatalf("done: %d", w.Code)
	}
	done := readerJSON(t, h.do(t, http.MethodGet, "/api/consume?view=later-done", "").Body.String())
	if items := done["items"].([]any); len(items) != 1 {
		t.Errorf("done list: %d", len(items))
	}
	if w := h.do(t, http.MethodPost, "/api/consume/later/"+laterID+"/remove", ""); w.Code != http.StatusOK {
		t.Fatalf("remove: %d", w.Code)
	}
	if w := h.do(t, http.MethodPost, "/api/consume/later/nope/done", ""); w.Code != http.StatusBadRequest {
		t.Errorf("an unknown entry is refused, not silently ok: %d", w.Code)
	}
	if w := h.do(t, http.MethodPost, "/api/consume/later", `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("an empty save is refused: %d", w.Code)
	}
}

func TestConsumeSwitchUpdateLeavesTheRestOfTheLine(t *testing.T) {
	h := newConsumeHarness(t)
	sub := h.subscribe(t)
	w := h.do(t, http.MethodPost, "/api/consume/subscriptions/"+sub.ID+"/update", `{"pays":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	b, _ := os.ReadFile(filepath.Join(h.vault, "extrinsic", "feeds.md"))
	doc := string(b)
	if !strings.Contains(doc, "[pays:: yes]") {
		t.Errorf("switch written:\n%s", doc)
	}
	for _, s := range h.svc.Subscriptions() {
		if s.ID == sub.ID && (s.List != "essays" || s.Title != "Test Letter") {
			t.Errorf("a switch toggle must not regroup or rename: %+v", s)
		}
	}
	w = h.do(t, http.MethodPost, "/api/consume/streams/rename", `{"from":"essays","to":"long reads"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	if subs := h.svc.Subscriptions(); subs[0].List != "long reads" {
		t.Errorf("stream renamed: %+v", subs[0])
	}
}

func TestShareSheetSendsABareLinkToLater(t *testing.T) {
	h := newConsumeHarnessWith(t, consume.Config{AllowPrivateCurateFetch: true})
	link := h.feed.URL + "/an-essay"
	if !h.srv.shareToLater("An Essay", "An Essay "+link, "", 0) {
		t.Fatal("a title and a link is a link share")
	}
	if n := len(h.svc.LaterEntries()); n != 1 {
		t.Fatalf("queued before the page is read: %d", n)
	}
	// The page is read in the background; wait for it so nothing writes
	// into the test's directories after it ends.
	deadline := time.Now().Add(10 * time.Second)
	resolved := func() bool {
		e := h.svc.LaterEntries()
		return !h.svc.MissingLater() && len(e) == 1 && e[0].Kind != ""
	}
	for !resolved() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if h.srv.shareToLater("", "", link, 1) {
		t.Error("a share with files stays in the capture tray")
	}
	if h.srv.shareToLater("", strings.Repeat("a long note about it ", 20)+link, "", 0) {
		t.Error("a written note with a link stays in the capture tray")
	}
	if h.srv.shareToLater("", "just words", "", 0) {
		t.Error("no link, no Later")
	}
}
