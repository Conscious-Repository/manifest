package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"manifest/aion"
)

// The RECONCILE surface (aion-linkage-scope, permanent): it surfaces every
// gap between the manifest backend and what the aion.bio portal can render,
// and lets the owner close them in bulk. The portal joins backlog items to
// the goal graph ONLY through each item's rock; a decided decision also
// needs a `decided` date to appear in the agency field. So a "gap" is an
// item the portal can't fully place: an unanchored task/decision (rock
// doesn't resolve) or a decided decision missing its date.

// reconcileItem is one gap row for the UI.
type reconcileItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Owner    string `json:"owner"`
	Rock     string `json:"rock"`     // current raw value ("" = none)
	Decided  string `json:"decided"`  // decisions only
	NeededBy string `json:"neededBy"` // open-decision deadline (portal diamond)
	Captured string `json:"captured"`
	Hint     string `json:"hint"`   // original pre-linkage rock (git-recovered), "" if none
	Reason   string `json:"reason"` // why it's a gap: unanchored | undated-decided | open-decision
}

// isISODate reports whether s is exactly YYYY-MM-DD (the portal's timeline
// contract for decided / needed_by / start / due).
var isoDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func isISODate(s string) bool { return isoDateRe.MatchString(strings.TrimSpace(s)) }

// aionReconcileHints loads the git-recovered {normalized title → original
// rock} map (derived/operational, dataDir tier). Absent file = no hints.
func (s *Server) aionReconcileHints() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(s.aionDataDir, "aion", "rock-hints.json"))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

// handleAionReconcile (GET) returns the gap rows + the resolvable goal-id
// set the client uses to decide "does this rock resolve", plus counts.
func (s *Server) handleAionReconcile(w http.ResponseWriter, r *http.Request) {
	if s.aion == nil {
		httpError(w, errBadRequest("aion not enabled"))
		return
	}
	// the resolvable set: every goal id + slug + de-prefixed slug + alias,
	// exactly the portal's matchRock contract (util.js buildGoalIndex)
	ids := map[string]bool{}
	known := map[string]bool{}
	area := s.aionGoalsArea()
	if area != nil {
		add := func(id string, aliases []string) {
			ids[id] = true
			known[reconcileSlug(id)] = true
			known[reconcileSlug(strings.TrimPrefix(id, "aion/"))] = true
			for _, a := range aliases {
				known[reconcileSlug(a)] = true
			}
		}
		for i := range area.Annuals {
			add(area.Annuals[i].ID, area.Annuals[i].Aliases)
		}
		for _, rk := range area.Rocks {
			add(rk.ID, rk.Aliases)
			for _, c := range rk.Children {
				add(c.ID, c.Aliases)
			}
		}
	}
	resolves := func(rock string) bool {
		if rock == "" {
			return false
		}
		return ids[rock] || known[reconcileSlug(rock)] || known[reconcileSlug("aion/"+rock)]
	}

	hints := s.aionReconcileHints()
	backlog := s.aion.LoadBacklog()
	var gaps []reconcileItem
	var unTasks, unDecs, undated, openDecs int
	for _, it := range backlog.Items() {
		open := it.Status == aion.StatusOpen || it.Status == aion.StatusInProgress
		reason := ""
		switch {
		case it.Kind == aion.KindDecision && it.Status == aion.StatusDecided && it.Decided == "":
			reason = "undated-decided"
			undated++
		case it.Kind == aion.KindDecision && it.Status == aion.StatusDecided && !resolves(it.Rock):
			reason = "unanchored"
			unDecs++
		case it.Kind == aion.KindTask && open && !resolves(it.Rock):
			reason = "unanchored"
			unTasks++
		case it.Kind == aion.KindDecision && open && (!resolves(it.Rock) || !isISODate(it.NeededBy)):
			// open decision the portal can't fully place: no resolving rock
			// and/or no ISO deadline (no timeline diamond). Coverage gap (§7).
			reason = "open-decision"
			openDecs++
		default:
			continue // resolved, or a done task — not a portal gap
		}
		gaps = append(gaps, reconcileItem{
			ID: it.ID, Kind: it.Kind, Title: it.Text, Status: it.Status,
			Owner: it.Owner, Rock: it.Rock, Decided: it.Decided, NeededBy: it.NeededBy, Captured: it.Captured,
			Hint: hints[aion.NormalizeTitle(it.Text)], Reason: reason,
		})
	}
	writeJSON(w, map[string]any{
		"gaps": gaps,
		"counts": map[string]int{
			"unanchoredTasks":     unTasks,
			"unanchoredDecisions": unDecs,
			"undatedDecided":      undated,
			"openDecisions":       openDecs,
			"total":               len(gaps),
		},
	})
}

// handleAionBacklogLink (POST) applies a batch of linkage edits (rock /
// decided / owner) — works on decided decisions too (metadata only).
func (s *Server) handleAionBacklogLink(w http.ResponseWriter, r *http.Request) {
	if s.aion == nil {
		httpError(w, errBadRequest("aion not enabled"))
		return
	}
	var b struct {
		Edits []aion.LinkEdit `json:"edits"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	n, err := s.aion.BatchLink(b.Edits)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "changed": n})
}

// reconcileSlug mirrors the portal's slugify (util.js) exactly — the client
// and server must agree on what "resolves".
func reconcileSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r == ' ' || r == '_':
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '/' || r == '-':
			b.WriteRune(r)
			prevDash = false
		}
	}
	return b.String()
}
