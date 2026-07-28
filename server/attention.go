package server

import (
	"net/url"
	"time"

	"manifest/attention"
	"manifest/feed"
)

// The §5 attention sources, as thin adapters over the surfaces the server
// already holds. Each adapter nil-checks its backing service (Use* wiring
// happens after New), so a disabled surface contributes nothing — never an
// error, never a missing field. handleFeedList iterates these; the response
// field each kind renders into is fixed by kindField (the client contract).

// attentionRegistry builds the ordered registry: findings, signals, notices,
// and the receipts slot — declared with a permanent lifecycle, EMPTY until an
// errand loop exists to write receipts (that's a feature pass, not this one).
func (s *Server) attentionRegistry() *attention.Registry {
	r := &attention.Registry{}
	r.Register(findingsSource{s})
	r.Register(signalsSource{s})
	r.Register(noticesSource{s})
	r.Register(attention.EmptySource{K: "receipt", L: attention.LifecyclePermanent})
	return r
}

// kindField maps an attention kind to its response field (the client keys its
// card renderers off these exact names).
var kindField = map[string]string{
	"finding": "items",
	"signal":  "signals",
	"notice":  "portalItems",
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
	for _, it := range f.s.spirits.Feed.List(flt, now) {
		views = append(views, feedItemView{Item: it, ArtifactPath: f.s.artifactPath(it)})
	}
	return views
}
func (f findingsSource) Count(now time.Time) int {
	if f.s.spirits == nil {
		return 0
	}
	return len(f.s.spirits.Feed.List(feed.Filter{Status: "inbox"}, now))
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
