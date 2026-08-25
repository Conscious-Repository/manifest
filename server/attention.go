package server

import (
	"net/url"
	"time"

	"manifest/attention"
	"manifest/consume"
	"manifest/feed"
)

// The §5 attention sources, as thin adapters over the surfaces the server
// already holds. Each adapter nil-checks its backing service (Use* wiring
// happens after New), so a disabled surface contributes nothing — never an
// error, never a missing field. handleFeedList iterates these; the response
// field each kind renders into is fixed by kindField (the client contract).

// attentionRegistry builds the ordered registry: findings, signals, notices,
// receipts — the fourth kind now BACKED by the errand store (errands-aside §5
// as amended: the registry's own lane, permanent lifecycle, badge counts
// queued/running/failed only).
func (s *Server) attentionRegistry() *attention.Registry {
	r := &attention.Registry{}
	r.Register(findingsSource{s})
	r.Register(signalsSource{s})
	r.Register(noticesSource{s})
	r.Register(consumeSource{s})
	r.Register(receiptsSource{s})
	return r
}

// kindField maps an attention kind to its response field (the client keys its
// card renderers off these exact names).
var kindField = map[string]string{
	"finding": "items",
	"signal":  "signals",
	"notice":  "portalItems",
	"consume": "consumeItems",
	"receipt": "receipts",
}

// findingsSource: engine-authored feed items (artifacts/feed tree). Lifecycle
// kept-discarded; the badge counts only the inbox slice.
type findingsSource struct{ s *Server }

func (f findingsSource) Kind() string                   { return "finding" }
func (f findingsSource) Lifecycle() attention.Lifecycle { return attention.LifecycleKeptDiscarded }
func (f findingsSource) Active(now time.Time, q url.Values) []attention.Card {
	views := []attention.Card{}
	if f.s.spirits == nil {
		return views
	}
	flt := feed.Filter{Status: q.Get("status"), Type: q.Get("type"), Domain: q.Get("domain")}
	for _, h := range f.s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		lib := harnessLibrary(h) // read at most once per harness, only if a card needs it
		for _, it := range h.Spirits.Feed.List(flt, now) {
			vaultRel, harnessRef := f.s.artifactRefsIn(h, it, lib)
			views = append(views, feedItemView{Item: it, ArtifactPath: vaultRel, ArtifactRef: harnessRef, Harness: f.s.harnessTag(h.Name)})
		}
	}
	return views
}
func (f findingsSource) Count(now time.Time) int {
	if f.s.spirits == nil {
		return 0
	}
	n := 0
	for _, h := range f.s.eachHarness() {
		if h.Spirits != nil {
			n += len(h.Spirits.Feed.List(feed.Filter{Status: "inbox"}, now))
		}
	}
	return n
}

// signalsSource: app-computed conditions. Lifecycle dismiss-snooze-autoclear.
type signalsSource struct{ s *Server }

func (g signalsSource) Kind() string { return "signal" }
func (g signalsSource) Lifecycle() attention.Lifecycle {
	return attention.LifecycleDismissSnoozeAutoclear
}
func (g signalsSource) Active(now time.Time, _ url.Values) []attention.Card {
	out := []attention.Card{}
	for _, sig := range g.s.activeSignals(now) {
		out = append(out, sig)
	}
	return out
}
func (g signalsSource) Count(now time.Time) int {
	if g.s.signals == nil {
		return 0
	}
	return g.s.signals.Count(now)
}

// noticesSource: externally-sourced portal cards (clickup, benchling).
// Lifecycle dismiss-expire; the external system is the source of truth.
type noticesSource struct{ s *Server }

func (n noticesSource) Kind() string                   { return "notice" }
func (n noticesSource) Lifecycle() attention.Lifecycle { return attention.LifecycleDismissExpire }
func (n noticesSource) Active(_ time.Time, _ url.Values) []attention.Card {
	out := []attention.Card{}
	for _, c := range n.s.portalCards() {
		out = append(out, c)
	}
	return out
}
func (n noticesSource) Count(time.Time) int { return n.s.portalInboxCount() }

// consumeSource: subscribed reading — RSS/Atom feeds and X accounts (§5
// amendment 2026-08-24, the fifth kind). Lifecycle read-curate-dismiss.
//
// ⚠ Count is deliberately ZERO. Reading is not attention debt: the FEED badge
// counts things that want something FROM the owner, and an unread essay does
// not. The lane shows its own unread total instead, so a growing backlog is
// visible without ever making the nav pill nag.
type consumeSource struct{ s *Server }

func (c consumeSource) Kind() string { return "consume" }
func (c consumeSource) Lifecycle() attention.Lifecycle {
	return attention.LifecycleReadCurateDismiss
}
func (c consumeSource) Active(_ time.Time, q url.Values) []attention.Card {
	out := []attention.Card{}
	if c.s.consume == nil {
		return out
	}
	for _, card := range c.s.consume.Cards(consume.Query{View: q.Get("view"), List: q.Get("list")}) {
		out = append(out, card)
	}
	return out
}
func (c consumeSource) Count(time.Time) int { return 0 }
