package teamportal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/portals"
)

// Bridge surfaces team writes as FEED notices (ARCHITECTURE §5: notices —
// externally-sourced portal items; dismiss; expire 14d; no new attention
// kind). It reads the store's activity trail and renders one deterministic
// card per entry, portals.Card-shaped so the existing notice renderer and
// badge work unchanged.
//
// Dismissals are cockpit state, not team state → they live under the
// per-machine dataDir (portal-cache/aion-portal/dismissed.json), mirroring
// the clickup/benchling cache discipline, never in the shared team dir.
type Bridge struct {
	suppressPropose bool
	store           *Store
	dir             string // <dataDir>/portal-cache/<name>
	admin           string // the owner's email — his own writes never nag his own feed
	name            string // card-id prefix + source tag; namespaces two portals' cards
	link            string // deep link the card opens

	mu sync.Mutex
}

const noticeRetention = 14 * 24 * time.Hour

func NewBridge(store *Store, dataDir, adminEmail string) *Bridge {
	return NewBridgeNamed(store, dataDir, adminEmail, "aion-portal", "https://portal.aion.bio/#task")
}

// NewBridgeNamed namespaces a second portal's notices (ooda-portal plan,
// Stage A): the card-id prefix, the Owns() claim, and the dismissal cache all
// key on name, so one portal can never dismiss the other's cards.
func NewBridgeNamed(store *Store, dataDir, adminEmail, name, deepLink string) *Bridge {
	return &Bridge{
		store: store,
		dir:   filepath.Join(dataDir, "portal-cache", name),
		admin: adminEmail,
		name:  name,
		link:  deepLink,
	}
}

func (b *Bridge) dismissedPath() string { return filepath.Join(b.dir, "dismissed.json") }

func (b *Bridge) readDismissed() map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(b.dismissedPath()); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// Owns reports whether a feed card id belongs to this bridge.
func (b *Bridge) Owns(cardID string) bool {
	return strings.HasPrefix(cardID, b.name+":")
}

// Dismiss records a card id (GC'd past retention on the next Cards read).
func (b *Bridge) Dismiss(cardID string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d := b.readDismissed()
	d[cardID] = now.UTC().Format(time.RFC3339)
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return
	}
	if raw, err := json.MarshalIndent(d, "", "  "); err == nil {
		_ = os.WriteFile(b.dismissedPath(), raw, 0o644)
	}
}

// Cards renders the live notices: one per team write inside the retention
// window, minus dismissals and the owner's own writes. Deterministic ids
// (timestamp + actor) keep dismissals stable across reloads.
// SuppressProposeNotices stops this bridge emitting a dismiss-only notice for
// an event that also files an approvable card. Set by the portal that files
// them; a portal without the card lane keeps the notice, which is still the
// only way it would reach the FEED at all.
func (b *Bridge) SuppressProposeNotices() *Bridge {
	b.suppressPropose = true
	return b
}

func (b *Bridge) Cards(now time.Time) []portals.Card {
	if b.store == nil {
		return nil
	}
	entries := b.store.Activity(now.Add(-noticeRetention))
	b.mu.Lock()
	dismissed := b.readDismissed()
	b.mu.Unlock()
	var cards []portals.Card
	for _, e := range entries {
		if b.admin != "" && e.Actor == b.admin {
			continue
		}
		// A proposal now files its own APPROVABLE card in the FEED
		// (approvals.TypePortalProposal, 2026-08-24). Emitting the notice too
		// would show the owner the same event twice — once he can act on, once
		// he can only dismiss — and dismissing the notice would look like
		// declining the proposal.
		if b.suppressPropose && e.Action == ActPropose {
			continue
		}
		// The materialization lane's own bookkeeping is not news: promoting an
		// item and clearing a spent override are the sync doing its job, and a
		// notice per edit would bury the entries that mean something.
		if e.Action == ActPromote || e.Action == ActSynced {
			continue
		}
		id := fmt.Sprintf("%s:%d:%s", b.name, e.TS.UnixNano(), e.Action)
		if _, ok := dismissed[id]; ok {
			continue
		}
		title, detail := describe(e)
		cards = append(cards, portals.Card{
			ID: id, Type: "portal-item", Portal: b.name,
			Title: title, Detail: detail, Change: e.Action, Actor: e.Actor,
			URL:  b.link,
			Date: e.TS.Format(time.RFC3339),
		})
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].Date > cards[j].Date })
	return cards
}

// describe renders one activity entry as a card title + detail — script-built
// from the payload, no LLM (portals doctrine: deterministic cards only).
func describe(e Entry) (title, detail string) {
	str := func(k string) string { v, _ := e.Payload[k].(string); return v }
	switch e.Action {
	case ActComment:
		return "portal comment on " + str("item"), str("text")
	case ActDeleteComment:
		return "portal comment removed on " + str("item"), str("text")
	case ActPatch:
		fields := ""
		if f, ok := e.Payload["fields"].(map[string]any); ok {
			keys := make([]string, 0, len(f))
			for k := range f {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if fields != "" {
					fields += " · "
				}
				fields += fmt.Sprintf("%s → %v", k, f[k])
			}
		}
		return "portal update on " + str("item"), fields
	case ActAdd:
		return "portal team item added", str("title") + " (@" + str("owner") + ")"
	case ActPropose:
		return "portal proposal for " + str("target"), str("title")
	case ActDecide:
		verdict := "rejected"
		if ok, _ := e.Payload["approved"].(bool); ok {
			verdict = "approved"
		}
		return "portal proposal " + verdict, str("title") + " (for " + str("target") + ")"
	case ActOwnerResolve:
		return "manifest resolved portal fields on " + str("item"), "by " + e.Actor
	case ActOwnerPatch:
		return "manifest updated " + str("item"), "by " + e.Actor
	case ActArchive:
		return "manifest archived " + str("item"), str("title")
	case "agent-assign":
		return "portal: " + str("owner") + " assigned on " + str("item"), "by " + e.Actor
	case "agent-fire":
		return "portal: plan FIRED on " + str("item"), "by " + e.Actor + " — the agent is executing"
	}
	return "portal activity: " + e.Action, ""
}
