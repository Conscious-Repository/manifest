package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manifest/consume"
)

// The CONSUME routes — the private side of the lane. The PUBLIC side is a
// different listener entirely (consume.PublicHandler, wired in main.go) and
// shares nothing with this file but the service.
//
// Every handler nil-checks s.consume: the Use* wiring happens after New, so a
// build with no vault or no consume config answers empty rather than 500ing.

// UseConsume wires the CONSUME lane. xTokenPath is where the X bearer token
// lives (secrets tier, 0600) and publicURL is the curation feed's public
// address — display only, since this server never serves it.
func (s *Server) UseConsume(c *consume.Service, xTokenPath, publicURL string) {
	s.consume = c
	s.consumeXTokenPath = xTokenPath
	s.consumePublicURL = publicURL
}

// handleConsumeList — the lane and its CONSUME view.
// ?view=unread|all  ?list=<group>
func (s *Server) handleConsumeList(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		writeJSON(w, map[string]any{"items": []any{}, "lists": []any{}, "unread": 0})
		return
	}
	q := r.URL.Query()
	list := q.Get("list")
	view := q.Get("view")
	items := s.consume.Cards(consume.Query{
		View: view, List: list, Sub: q.Get("sub"), Q: q.Get("q"), Type: q.Get("type"),
	})
	// Paging: the reader asks for a screenful and fetches more on scroll. A
	// caller that names no limit gets everything, as before.
	total := len(items)
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 || offset > total {
		offset = total
	}
	items = items[offset:]
	more := false
	if limit, _ := strconv.Atoi(q.Get("limit")); limit > 0 && limit < len(items) {
		items, more = items[:limit], true
	}
	if (view == "later" || view == "later-done") && s.consume.MissingLater() {
		s.resolveMissingLater()
	}
	writeJSON(w, map[string]any{
		"items": items,
		"more":  more,
		"count": total,
		"lists": s.consume.Lists(),
		// Scoped to the active group so it agrees with the scoped
		// "mark all read" sitting next to it.
		"unread": s.consume.Unread(list),
		"total":  s.consume.Unread(""),
		"nav":    s.consume.Nav(),
	})
}

// resolveMissingLater re-reads queue entries whose cached copy is gone, in the
// background and one run at a time. The list still shows them from their
// vault line meanwhile.
func (s *Server) resolveMissingLater() {
	if !s.consumeLaterResolving.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.consumeLaterResolving.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s.consume.ResolveMissingLater(ctx, 6)
	}()
}

// handleConsumeItem — the reader's fetch: one item with its sanitized body.
//
// The body is server-sanitized at poll time (consume/sanitize.go) and this is
// the only place it is handed to the client, which renders it through the
// single innerHTML sink in the FEED.
func (s *Server) handleConsumeItem(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	it, sub, ok := s.consume.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Opening the reader IS reading it — no separate call, no way to leave a
	// read item looking unread because a second request failed. A PEEK is the
	// exception: the reader prefetches the next few bodies so moving on is
	// instant, and fetching ahead must not mark anything read.
	if r.URL.Query().Get("peek") != "1" {
		s.consume.MarkRead(id)
	}
	out := map[string]any{
		"id": it.ID, "title": it.Title, "url": it.URL, "author": it.Author,
		"source": it.Source, "list": sub.List, "body": it.Body,
		"published": it.PublishedAt, "chars": it.Chars, "preview": it.Preview,
		"curated": s.consumeCuratedFor(it.URL),
		"note":    s.consumeNoteFor(it.URL),
		"subId":   sub.ID,
		"read":    it.ReadAt != "" || r.URL.Query().Get("peek") != "1",
	}
	if it.Embed != "" {
		out["embed"] = it.Embed
		out["image"] = it.Image
	}
	if e, ok := s.consume.LaterEntryFor(it.ID); ok {
		out["later"], out["laterId"], out["laterDone"] = true, e.ID, e.Done != ""
	} else if e, ok := s.consume.LaterEntryForURL(it.URL); ok {
		out["later"], out["laterId"], out["laterDone"] = true, e.ID, e.Done != ""
	}
	// An episode: the reader shows a player above the show notes. Sent only
	// when the item actually carries audio, so a text article's payload is
	// byte-identical to what it was.
	if it.Podcast() {
		out["audio"] = it.Audio
		out["audioType"] = it.AudioType
		out["duration"] = it.Duration
		out["episode"] = it.Episode
		out["season"] = it.Season
		out["image"] = it.Image
	}
	writeJSON(w, out)
}

// consumeCuratedFor / consumeNoteFor let the reader show the current curation
// state without a second round trip.
func (s *Server) consumeCuratedFor(url string) bool {
	_, ok := s.consumeEntryFor(url)
	return ok
}

func (s *Server) consumeNoteFor(url string) string {
	if e, ok := s.consumeEntryFor(url); ok {
		return e.Note
	}
	return ""
}

func (s *Server) consumeEntryFor(url string) (consume.CuratedEntry, bool) {
	if s.consume == nil {
		return consume.CuratedEntry{}, false
	}
	return s.consume.CuratedFor(url)
}

func (s *Server) handleConsumeRead(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.MarkRead(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConsumeDismiss(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.Dismiss(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeUndismiss is the undo behind the dismiss toast.
func (s *Server) handleConsumeUndismiss(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.Undismiss(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeUnread bumps an archived item back into the queue.
func (s *Server) handleConsumeUnread(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.MarkUnread(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeReadAll clears the unread backlog, optionally for one group.
func (s *Server) handleConsumeReadAll(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	// A narrower view — one source, one media type, a search — marks exactly
	// what it shows. The plain stream-or-everything case keeps its old path.
	if q.Get("sub") != "" || q.Get("type") != "" || q.Get("q") != "" {
		writeJSON(w, map[string]any{"ok": true, "marked": s.consume.MarkViewRead(consume.Query{
			List: q.Get("list"), Sub: q.Get("sub"), Type: q.Get("type"), Q: q.Get("q"),
		})})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "marked": s.consume.MarkAllRead(q.Get("list"))})
}

// ---- Watch Later ----

// handleConsumeLaterAdd queues a pasted link ({"url"}) or a feed item
// ({"item"}).
func (s *Server) handleConsumeLaterAdd(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		URL  string `json:"url"`
		Item string `json:"item"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var (
		e   consume.LaterEntry
		err error
	)
	switch {
	case strings.TrimSpace(body.Item) != "":
		e, err = s.consume.SaveLaterItem(strings.TrimSpace(body.Item))
	case strings.TrimSpace(body.URL) != "":
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		e, err = s.consume.SaveLaterURL(ctx, strings.TrimSpace(body.URL))
	default:
		err = errBadRequest("paste a link, or pick an item")
	}
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "entry": e})
}

func (s *Server) handleConsumeLaterDone(w http.ResponseWriter, r *http.Request) {
	s.consumeLaterSet(w, r, true)
}

func (s *Server) handleConsumeLaterUndone(w http.ResponseWriter, r *http.Request) {
	s.consumeLaterSet(w, r, false)
}

func (s *Server) consumeLaterSet(w http.ResponseWriter, r *http.Request, done bool) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.SetLaterDone(r.PathValue("id"), done); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConsumeLaterRemove(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.RemoveLater(r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// shareToLater is the phone share sheet's path for a bare link: the queue line
// is written now and the page is read in the background, so the share answers
// at once. It reports false when the share is not a single link.
func (s *Server) shareToLater(title, text, rawURL string, files int) bool {
	if s.consume == nil || files > 0 {
		return false
	}
	link := strings.TrimSpace(rawURL)
	if link == "" {
		// Android shares put the link in text, sometimes after a title line.
		fields := strings.Fields(text)
		if len(fields) == 0 {
			return false
		}
		last := fields[len(fields)-1]
		if !strings.HasPrefix(last, "http://") && !strings.HasPrefix(last, "https://") {
			return false
		}
		// Anything besides a title and the link is a note, which belongs in
		// the capture tray rather than the reading queue.
		rest := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), last))
		if rest != "" && rest != strings.TrimSpace(title) && len(rest) > 200 {
			return false
		}
		link = last
	}
	e, fresh, err := s.consume.QueueLaterURL(link)
	if err != nil {
		return false
	}
	if fresh {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, _ = s.consume.ResolveLater(ctx, e)
		}()
	}
	return true
}

// handleConsumeStreamRename renames a stream (a heading in feeds.md).
func (s *Server) handleConsumeStreamRename(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.consume.RenameStream(body.From, body.To); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumePollAll refreshes every subscription now, ignoring intervals.
func (s *Server) handleConsumePollAll(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "polled": s.consume.PollAll(r.Context())})
}

// handleConsumeCurate — THE button. Writes an extrinsic/ note under the
// consume-curate capability; the public feed reads those notes and nothing
// else.
func (s *Server) handleConsumeCurate(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // a note is optional
	entry, err := s.consume.Curate(r.Context(), r.PathValue("id"), body.Note)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": entry.Path, "note": entry.Note})
}

// handleConsumeCurateURL — the FEED header's ＋ curate: a pasted address, an
// optional note, published now.
//
// It is a CONSUME route rather than a FEED one because the write is a CONSUME
// domain write; the FEED is only where the button lives. The answer is the
// shape handleFeedCurate answers, so the client tells the owner which of the
// kinds he just published with one convention instead of two.
func (s *Server) handleConsumeCurateURL(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.Error(w, "curation unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		URL  string `json:"url"`
		Note string `json:"note"`
	}
	if err := decode(r, &body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		http.Error(w, "paste a link to curate", http.StatusBadRequest)
		return
	}
	entry, err := s.consume.CurateURL(r.Context(), body.URL, body.Note)
	if err != nil {
		// A rejected link is the OWNER's mistake to fix — an unsupported
		// scheme, a private address, a port that is not the web's — so it
		// answers 400 with the guard's own sentence rather than a 500.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"ok":     true,
		"path":   entry.Path,
		"note":   entry.Note,
		"mirror": entry.Mirror,
		"full":   strings.EqualFold(entry.Mirror, consume.MirrorFull),
		"public": s.consumePublicURL,
		// What it turned out to BE — paper, episode, platform or article. The
		// toast says which, the way the FEED card's does.
		"kind":  consume.LinkKind(entry),
		"title": entry.Title,
		"audio": entry.Audio != "",
		"embed": entry.Embed,
	})
}

func (s *Server) handleConsumeUncurate(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.Uncurate(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeCuratedNote — "edit note" in the curated panel.
//
// Deliberately NOT /api/consume/item/{id}/curate. That route resolves a live
// store item, and the curated panel lists NOTES: a note curated from a pasted
// link or an external bridge carries an `item:` id no store holds, so editing
// its note through the item route answered `no item "ext-…"`. The identity
// here is the note's path, which is what the projection names.
func (s *Server) handleConsumeCuratedNote(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Path string `json:"path"`
		Item string `json:"item"`
		Note string `json:"note"`
	}
	if err := decode(r, &body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	entry, err := s.consume.UpdateCuratedNote(body.Item, body.Path, body.Note)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": entry.Path, "note": entry.Note})
}

// handleConsumeCurated — the private mirror of exactly what the public feed
// serves, so the owner can audit it without leaving the app.
func (s *Server) handleConsumeCurated(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		writeJSON(w, map[string]any{"entries": []any{}})
		return
	}
	writeJSON(w, map[string]any{"entries": s.consume.Curated(), "public": s.consumePublicURL})
}

// ---- subscriptions ----

func (s *Server) handleConsumeSubs(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		writeJSON(w, map[string]any{"subscriptions": []any{}})
		return
	}
	// nav rides along: the sidebar is drawn from this one answer.
	writeJSON(w, map[string]any{"subscriptions": s.consume.Statuses(), "xReady": s.consumeXReady(), "nav": s.consume.Nav()})
}

func (s *Server) handleConsumeSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Input  string `json:"input"`
		Title  string `json:"title"`
		List   string `json:"list"`
		Mirror string `json:"mirror"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sub, err := s.consume.Subscribe(r.Context(), body.Input, body.Title, body.List, body.Mirror)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "subscription": sub, "archived": s.consume.Seeded(sub.ID)})
}

func (s *Server) handleConsumeSubUpdate(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Title    *string `json:"title"`
		List     *string `json:"list"`
		Mirror   string  `json:"mirror"`
		MinChars int     `json:"minChars"`
		Fulltext string  `json:"fulltext"`
		// The yes/no switches travel on their own: a toggle sends only the
		// switch it changed, and nothing else on the line moves.
		Pays   *bool `json:"pays"`
		Shorts *bool `json:"shorts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if body.Title != nil || body.List != nil || body.Mirror != "" || body.Fulltext != "" {
		cur := consume.Subscription{}
		for _, sub := range s.consume.Subscriptions() {
			if sub.ID == id {
				cur = sub
			}
		}
		title, list := cur.Title, cur.List
		if body.Title != nil {
			title = *body.Title
		}
		if body.List != nil {
			list = *body.List
		}
		if err := s.consume.UpdateSub(consume.Subscription{
			ID: id, Title: title, List: list,
			Mirror: body.Mirror, MinChars: body.MinChars, Fulltext: body.Fulltext,
		}); err != nil {
			httpError(w, err)
			return
		}
	}
	if body.Pays != nil || body.Shorts != nil {
		if err := s.consume.SetSubSwitches(id, body.Pays, body.Shorts); err != nil {
			httpError(w, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConsumeUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.Unsubscribe(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConsumePoll(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.PollNow(r.Context(), r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
