package teamportal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	store *Store
	dir   string // <dataDir>/portal-cache/aion-portal
	admin string // the owner's email — his own writes never nag his own feed

	mu sync.Mutex
}

const noticeRetention = 14 * 24 * time.Hour

func NewBridge(store *Store, dataDir, adminEmail string) *Bridge {
	return &Bridge{
		store: store,
		dir:   filepath.Join(dataDir, "portal-cache", "aion-portal"),
		admin: adminEmail,
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
	return len(cardID) > 12 && cardID[:12] == "aion-portal:"
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
		id := fmt.Sprintf("aion-portal:%d:%s", e.TS.UnixNano(), e.Action)
		if _, ok := dismissed[id]; ok {
			continue
		}
		title, detail := describe(e)
		cards = append(cards, portals.Card{
			ID: id, Type: "portal-item", Portal: "aion-portal",
			Title: title, Detail: detail, Change: e.Action, Actor: e.Actor,
			URL:  "https://portal.aion.bio/#todo",
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
	case "agent-assign":
		return "portal: " + str("owner") + " assigned on " + str("item"), "by " + e.Actor
	case "agent-fire":
		return "portal: plan FIRED on " + str("item"), "by " + e.Actor + " — the agent is executing"
	}
	return "portal activity: " + e.Action, ""
}
