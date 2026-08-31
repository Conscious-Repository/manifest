package server

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/attention"
	"manifest/consume"
	"manifest/feed"
	"manifest/signals"
	"manifest/spirits"
	"manifest/tasks"
)

// FEED CENTRAL — manifest's one inbox, promoted from a SPIRITS sub-tab to a
// first-class surface (plans/feed-central.md §1). Spirit items keep living in
// the excalibur artifacts/feed tree — the engine contract (file format, ids,
// statuses) is untouched; only the dashboard's address for them changed.
// App signals (§2) and virtual proposal cards (§4 pinned lane) join the same
// response in later phases.
//
// NOT gated on spirits: signals derive from contacts/goals and must flow even
// with excaliburPath unset — only the spirit-item slice needs s.spirits.

// feedInboxCount is THE badge compute. The list handler, the badge handler,
// and /api/spirits/status.feedInbox all call this one function so the counts
// can never drift (feed-central §1). Count = every registered attention
// kind's contribution (§5 registry: findings inbox + signals + notices +
// receipts) + the pending-proposals lane (the same set that renders as cards).
func (s *Server) feedInboxCount(now time.Time) int {
	return s.attentionRegistry().Badge(now) + len(s.approvalRows(nil))
}

// activeSignals returns the app-signal cards (empty when disabled).
func (s *Server) activeSignals(now time.Time) []signals.Signal {
	if s.signals == nil {
		return []signals.Signal{}
	}
	return s.signals.Active(now)
}

// feedItemView is a feed item enriched for the client: an `artifact` card whose
// content lives in the harness tree (artifacts/library/…) gets a resolved
// reference. Since the medium split the harness tree lives OUTSIDE the vault:
// ArtifactRef is the harness-relative path the client opens through the spirits
// read API (never the vault note view — two-media doctrine). ArtifactPath
// survives for the legacy in-vault layout only.
type feedItemView struct {
	feed.Item
	ArtifactPath string `json:"artifactPath,omitempty"` // vault-relative (legacy in-vault harness only)
	ArtifactRef  string `json:"artifactRef,omitempty"`  // harness-relative, read via /api/spirits/file
	Harness      string `json:"harness,omitempty"`      // federation source tag
}

// libraryRefRe pulls an `artifacts/library/<name>.md` reference out of a card's
// link or body (the engine puts it in either place).
var libraryRefRe = regexp.MustCompile(`artifacts/library/[^\s)"']+\.md`)

// handleFeedList serves the unified stream by iterating the §5 attention
// registry — one response field per registered kind (kindField), plus the
// proposals lane (an authorization queue, not an attention kind) and the
// badge. Adding a kind = registering a source; no hand-merged fields.
func (s *Server) handleFeedList(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	resp := map[string]any{
		"proposals":   s.feedProposals(),
		"badge":       s.feedInboxCount(now),
		"bankPending": s.bankPendingRows(),
	}
	for _, src := range s.attentionRegistry().Sources() {
		field := kindField[src.Kind()]
		cards := src.Active(now, r.URL.Query())
		// two sources may share a kind (errand + aion-publish receipts):
		// append into the field instead of overwriting — same field, same
		// array shape, client-invisible
		if existing, ok := resp[field].([]attention.Card); ok {
			resp[field] = append(existing, cards...)
		} else {
			resp[field] = cards
		}
	}
	writeJSON(w, resp)
}

// artifactRefsIn resolves an artifact card's library reference against ITS
// harness. Returns (vaultRel, harnessRef): vaultRel is non-empty only under
// the legacy in-vault layout (opens in the note view); harnessRef is the
// harness-relative path (opens through the spirits read API). Both empty when
// the item isn't an artifact or the file is missing.
//
// Three ways in, in order of directness (owner ask 2026-08-12 — a delegated
// artifact must ALWAYS be viewable): the card's own link, a library path
// mentioned in its body, and — for cards that name their brief nowhere — the
// delegation token they carry, matched against the harness library.
func (s *Server) artifactRefsIn(h Harness, it feed.Item, lib libraryFn) (string, string) {
	if it.Type != "artifact" || h.Spirits == nil {
		return "", ""
	}
	ref := ""
	switch {
	case strings.HasPrefix(it.Link, "artifacts/library/"):
		ref = it.Link
	default:
		if m := libraryRefRe.FindString(it.Body); m != "" {
			ref = m
		} else if m := todoTokenRe.FindStringSubmatch(it.Title + "\n" + it.Why + "\n" + it.Body); m != nil {
			ref = libraryRefForToken(m[1], lib)
		}
	}
	return s.artifactRefSplit(h, ref)
}

// feedProposals returns the FULL enriched approval rows for the feed's pinned
// lane (approvals-move-to-feed plan): every pending approval. The card in FEED
// is the control itself — diff + Confirm/Reject inline — and it resolves
// atomically on decision because pending/ is the only source of truth. Nothing
// is ever written to the engine's feed dir for these.
func (s *Server) feedProposals() []approvalRow {
	return s.approvalRows(nil)
}

// handleFeedBadge is the thin nav-pill count (same compute as the list).
func (s *Server) handleFeedBadge(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"count": s.feedInboxCount(time.Now())})
}

// handleFeedStatus records a verdict (keep/discard/snooze/restore) — the
// user's own decision written back to item frontmatter.
func (s *Server) handleFeedStatus(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Status string `json:"status"` // kept | discarded | snoozed | new
		Days   int    `json:"days"`   // for snooze
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id := r.PathValue("id")
	h, ok := s.feedHarnessFor(id) // federation: the verdict lands in the item's own tree
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	var (
		it  feed.Item
		err error
	)
	if b.Status == "snoozed" {
		days := b.Days
		if days <= 0 {
			days = 7
		}
		it, err = h.Spirits.Feed.Snooze(id, time.Now().Add(time.Duration(days)*24*time.Hour))
	} else {
		it, err = h.Spirits.Feed.SetStatus(id, b.Status)
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, it)
}

// ---- card actions (feed-central §3) ----

// handleFeedDig spools a run-now for the originating spirit with a request
// line carrying the item — findings arrive as new feed items, closing the
// loop in the feed itself. The target is the spirit's ON-DEMAND ritual
// (cadence-less + valid, exactly the castables rule); a spirit without one
// (ea-coordinator's digests) is un-diggable.
func (s *Server) handleFeedDig(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	fh, ok := s.feedHarnessFor(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	it, _ := fh.Spirits.Feed.Get(r.PathValue("id"))
	// dig spools into the PRIMARY engine; a non-primary item whose agent isn't
	// a primary spirit falls out below with an honest 422.
	ritual := s.onDemandRitual(it.Agent)
	if ritual == "" {
		http.Error(w, it.Agent+" has no on-demand ritual to dig with", http.StatusUnprocessableEntity)
		return
	}
	request := "go deeper on: " + it.Title
	if it.Link != "" {
		request += " " + it.Link
	}
	if err := s.spirits.SpoolRunNow(it.Agent, ritual, request, ""); err != nil {
		if errors.Is(err, spirits.ErrAlreadyActive) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"active": true, "spirit": it.Agent, "ritual": ritual})
			return
		}
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"spooled": true, "spirit": it.Agent, "ritual": ritual})
}

// onDemandRitual picks the spirit's cadence-less valid ritual (first
// alphabetically when several — single user, deterministic).
func (s *Server) onDemandRitual(spirit string) string {
	var names []string
	for _, rr := range s.spirits.Rituals(time.Now()) {
		if rr.Spirit != spirit || rr.Cadence != "" || !rr.Valid || !rr.Enabled {
			continue // paused on-demand rituals are not silently castable
		}
		if rr.Spirit == "sage" && rr.Ritual == "skill-cast" {
			continue // cast a skill instead (castables rule)
		}
		names = append(names, rr.Ritual)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// ---- signal actions (feed-central §2) ----
// Signals carry namespaced ids ("contact-cold:…" / "rock-stalled:…") and use
// these dedicated routes, so they can never fall into feed.Store.SetStatus,
// and Keep/Save-to-vault on a signal is structurally impossible.

func (s *Server) handleSignalDismiss(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		http.Error(w, "signals disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct{ ID, Hash string }
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if err := s.signals.Dismiss(b.ID, b.Hash); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleSignalSnooze(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		http.Error(w, "signals disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		ID   string
		Days int
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if b.Days <= 0 {
		b.Days = 7
	}
	if err := s.signals.Snooze(b.ID, time.Now().Add(time.Duration(b.Days)*24*time.Hour)); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleFeedSaveToVault promotes a feed item into a real extrinsic/ vault note
// (write-once) and records the note path back on the item. User-triggered.
// handleFeedToTask promotes a feed card into an Inbox todo (todos-surface
// §"Feed promote") — the card's title becomes the line, the source rides in
// parentheses, and the item is marked kept. The board's domain chips finish it.
func (s *Server) handleFeedToTask(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil || s.tasksStore == nil {
		http.Error(w, "todos unavailable", http.StatusServiceUnavailable)
		return
	}
	fh, ok := s.feedHarnessFor(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	it, _ := fh.Spirits.Feed.Get(r.PathValue("id"))
	doc, err := s.tasksStore.Load()
	if err != nil {
		httpError(w, err)
		return
	}
	text := strings.TrimSpace(it.Title)
	if it.Source != "" {
		text += " (" + it.Source + ")"
	}
	dom := doc.EnsureDomain(tasks.InboxName)
	dom.Tasks = append(dom.Tasks, &tasks.Task{Text: text, Added: time.Now().Format("2006-01-02")})
	if err := s.tasksStore.Save(doc); err != nil {
		httpError(w, err)
		return
	}
	updated, err := fh.Spirits.Feed.SetStatus(it.ID, "kept")
	if err != nil {
		writeJSON(w, it) // todo landed; status flip is best-effort
		return
	}
	writeJSON(w, updated)
}

func (s *Server) handleFeedSaveToVault(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault save unavailable", http.StatusServiceUnavailable)
		return
	}
	fh, ok := s.feedHarnessFor(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	it, _ := fh.Spirits.Feed.Get(r.PathValue("id"))
	rel, err := s.vault.SaveExtrinsic(it.Title, it.Type, it.Why, it.Link, it.Source, it.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	updated, err := fh.Spirits.Feed.SetVaultNote(it.ID, rel)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, updated)
}

// ---- curate: the bridge from a research card into the public feed ----
//
// A domain-scout paper and a subscribed essay are the same kind of thing once
// the owner decides subscribers should read it, so "curate" on a FEED card
// must land in the same place the CONSUME lane's button lands: one extrinsic/
// note, written under the consume-curate capability, projected by the public
// feed. The bridge lives in consume.CurateExternal — this handler only reads
// the card and hands it over, so there is no second write path to the vault
// and no second source for the feed.
//
// It is a NEW action beside Discard / → task / dig →, not a stage of any of
// them: curating says nothing about whether the finding still wants a verdict.
func (s *Server) handleFeedCurate(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	if s.consume == nil {
		http.Error(w, "curation unavailable", http.StatusServiceUnavailable)
		return
	}
	fh, ok := s.feedHarnessFor(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	it, _ := fh.Spirits.Feed.Get(r.PathValue("id"))
	var body struct {
		Note string `json:"note"`
	}
	_ = decode(r, &body) // a note is optional, and so is the request body
	entry, err := s.consume.CurateExternal(r.Context(), consume.ExternalRef{
		ID:          it.ID,
		Title:       it.Title,
		URL:         it.Link,
		Source:      firstNonEmpty(it.Source, it.Domain),
		Fallback:    it.Why,
		PublishedAt: feedItemDate(it),
	}, body.Note)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":     true,
		"path":   entry.Path,
		"note":   entry.Note,
		"mirror": entry.Mirror,
		// full says whether the fetch got the whole piece; the client tells the
		// owner which of the two he just published.
		"full":   strings.EqualFold(entry.Mirror, consume.MirrorFull),
		"public": s.consumePublicURL,
	})
}

// handleFeedUncurate clears the curated marker on the note this card produced.
// The note survives — un-curating is not a delete, here or in the lane.
func (s *Server) handleFeedUncurate(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.Error(w, "curation unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.consume.Uncurate(consume.ExternalItemID(r.PathValue("id"))); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// feedItemDate reads a card's RFC3339 date; a card without one publishes with
// its curation date alone.
func feedItemDate(it feed.Item) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(it.Date))
	if err != nil {
		return time.Time{}
	}
	return t
}
