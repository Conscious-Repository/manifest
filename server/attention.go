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
// receipts — the fourth kind now BACKED by the errand store (errands-aside §5
// as amended: the registry's own lane, permanent lifecycle, badge counts
// queued/running/failed only).
func (s *Server) attentionRegistry() *attention.Registry {
	r := &attention.Registry{}
	r.Register(findingsSource{s})
	r.Register(signalsSource{s})
	r.Register(noticesSource{s})
	r.Register(receiptsSource{s})
	r.Register(aionPublishSource{s})
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

// aionPublishSource: outcomes of the AION publish effector — a SECOND
// receipt-kind source (the registry supports many per kind; handleFeedList
// appends per field). Success AND failure are receipts — both are "the
// outcome of an action the system took" (§5), and the trail is permanent;
// the badge counts only unacknowledged failures (a demand), never successes
// (history). Cards carry kind:"aion-publish" so the client renderer
// dispatches away from errand receipts.
type aionPublishSource struct{ s *Server }

type aionPublishCard struct {
	CardKind string `json:"cardKind"` // "aion-publish"
	aionPublishRecord
}

func (a aionPublishSource) Kind() string                   { return "receipt" }
func (a aionPublishSource) Lifecycle() attention.Lifecycle { return attention.LifecyclePermanent }
func (a aionPublishSource) Active(_ time.Time, q url.Values) []attention.Card {
	out := []attention.Card{}
	if a.s.aion == nil {
		return out
	}
	view := q.Get("status")
	if view == "kept" {
		return out
	}
	for _, r := range a.s.publishLog().list() {
		if view != "all" && r.Acknowledged {
			continue
		}
		out = append(out, aionPublishCard{CardKind: "aion-publish", aionPublishRecord: r})
	}
	return out
}
func (a aionPublishSource) Count(time.Time) int {
	if a.s.aion == nil {
		return 0
	}
	n := 0
	for _, r := range a.s.publishLog().list() {
		if r.Status == "failed" && !r.Acknowledged {
			n++
		}
	}
	return n
}
