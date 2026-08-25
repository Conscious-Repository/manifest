package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/realestate"
	"manifest/teamportal"
)

// OodaLive owns the one live OODA projection (ooda-portal plan, Stage B).
// The vault's system/realestate/ records are the owner-authored BASE; the team
// store at /private/ooda/team contributes comments, activity, and field
// overrides. The overlay wins for its bounded fields and is NEVER materialized
// into the vault (ARCHITECTURE §12).
//
// It mirrors AionLive's DISCIPLINE — revision cache, last-known-good snapshot,
// serve-stale-on-failure — not its content. Where AionLive hashes seven corpus
// files' bytes, the RE tree is ~300 records + sidecars, so the base revision is
// a count+mtime scan instead.
type OodaLive struct {
	s *Server

	mu          sync.Mutex
	team        *teamportal.Store
	admin       teamportal.Identity
	lastCheck   time.Time
	currentRev  string
	servingRev  string
	snap        *oodaSnapshot
	stale       bool
	lastError   string
	lastGoodAt  time.Time
	sourceAt    time.Time
	cacheLoaded bool

	// mapCache memoizes the heavy MAP payload against the projection revision
	// (it carries every parcel polygon, so recomposing it per request would
	// re-read ~175 geo sidecars).
	mapCache oodaMapCache

	// packDir is zeck's standing context-pack destination (ooda_pack.go);
	// "" = pack disabled. packErrRev keeps a failed export to one log line
	// per revision.
	packDir    string
	packErrRev string
}

// oodaSnapshot is one composed read of the whole domain. Everything the four
// surfaces need comes from here, so a request never re-walks the vault.
type oodaSnapshot struct {
	Revision    string                               `json:"revision"`
	At          time.Time                            `json:"at"`
	Properties  []realestate.Property                `json:"properties"`
	Entities    []realestate.Entity                  `json:"entities"`
	Deals       []realestate.Deal                    `json:"deals"`
	Contractors []realestate.Entity                  `json:"contractors"`
	Contracts   []realestate.Contract                `json:"contracts"`
	Backlog     []*aion.BacklogItem                  `json:"backlog"`
	People      []*aion.Person                       `json:"people"`
	Holdings    map[string]realestate.EntityHoldings `json:"holdings"`
}

func newOodaLive(s *Server) *OodaLive {
	l := &OodaLive{s: s}
	_ = l.refresh(true)
	return l
}

// UseTeam attaches the OODA team store (its own — never AION's).
func (l *OodaLive) UseTeam(st *teamportal.Store, admin teamportal.Identity) {
	l.mu.Lock()
	l.team, l.admin = st, admin
	l.mu.Unlock()
}

func (l *OodaLive) teamStore() (*teamportal.Store, teamportal.Identity) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.team, l.admin
}

func (l *OodaLive) cachePath() string {
	return filepath.Join(l.s.aionDataDir, "ooda", "live.json")
}

// sourceRevision fingerprints the RE record tree: name + size + mtime of every
// file under the realestate root. Cheap next to parsing 63 properties with
// their ledgers, and it changes on any owner edit in Obsidian.
func (l *OodaLive) sourceRevision() string {
	h := sha256.New()
	if l.s.index == nil {
		return ""
	}
	root := filepath.Join(l.s.index.VaultRoot(), filepath.FromSlash(l.s.realestateRootOr()))
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		fmt.Fprintf(h, "%s|%d|%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (l *OodaLive) loadFallbackLocked() {
	if l.cacheLoaded {
		return
	}
	l.cacheLoaded = true
	var snap oodaSnapshot
	if b, err := os.ReadFile(l.cachePath()); err == nil && json.Unmarshal(b, &snap) == nil && len(snap.Properties) > 0 {
		l.snap, l.servingRev, l.lastGoodAt = &snap, snap.Revision, snap.At
	}
}

func (l *OodaLive) saveLocked() {
	if l.snap == nil {
		return
	}
	b, err := json.Marshal(l.snap)
	if err != nil {
		return
	}
	path := l.cachePath()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// refresh recomposes when the source moved. On failure the PREVIOUS good
// snapshot keeps serving (stale=true) — partners see slightly old numbers
// rather than an error page.
func (l *OodaLive) refresh(force bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !force && time.Since(l.lastCheck) < time.Second {
		return nil
	}
	l.lastCheck = time.Now()
	l.loadFallbackLocked()
	if l.s.realestate == nil {
		return fmt.Errorf("realestate service not configured")
	}
	rev := l.sourceRevision()
	if rev == l.currentRev && l.snap != nil {
		return nil
	}
	l.currentRev = rev
	now := time.Now().UTC()
	l.sourceAt = now

	props, err := l.s.realestate.Properties()
	if err != nil {
		l.stale, l.lastError = true, err.Error()
		return err
	}
	deals, _ := l.s.realestate.Deals()
	snap := &oodaSnapshot{
		Revision: rev, At: now,
		Properties: props, Deals: deals,
		Entities:    l.s.realestate.Entities(),
		Contractors: l.s.realestate.Contractors(),
		Contracts:   l.s.realestate.Contracts(),
		Holdings:    realestate.Holdings(props),
	}
	if l.s.re != nil {
		snap.Backlog = l.s.re.LoadBacklog().Items()
	}
	if l.s.index != nil {
		raw, _ := os.ReadFile(filepath.Join(l.s.index.VaultRoot(), filepath.FromSlash(l.s.rePeopleRel())))
		snap.People = aion.ParsePeople(string(raw)).People()
	}
	l.snap, l.servingRev = snap, rev
	l.stale, l.lastError, l.lastGoodAt = false, "", now
	l.saveLocked()
	// the standing context pack rides the same recompose: a new revision
	// here is exactly "the source moved", the only time the pack rewrites
	l.syncPackLocked()
	return nil
}

// server exposes the owning Server to this package's OODA handlers, nil-safe
// so a projection built without one degrades rather than panicking.
func (l *OodaLive) server() *Server {
	if l == nil || l.s == nil || l.s.realestate == nil {
		return nil
	}
	return l.s
}

// Snapshot returns the current composed read (nil only before the first
// successful compose with no cache on disk).
func (l *OodaLive) Snapshot() *oodaSnapshot {
	_ = l.refresh(false)
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snap
}

func (l *OodaLive) statusLocked() PortalLiveStatus {
	teamRev := ""
	if l.team != nil {
		teamRev = l.team.Revision()
	}
	h := sha256.Sum256([]byte(l.currentRev + "\x00" + teamRev))
	st := PortalLiveStatus{
		BaseRevision: l.currentRev, TeamRevision: teamRev,
		EffectiveRevision: hex.EncodeToString(h[:])[:16], ServingRevision: l.servingRev,
		Stale: l.stale, Degraded: l.stale, Error: l.lastError,
	}
	if !l.lastGoodAt.IsZero() {
		st.LastGoodAt = l.lastGoodAt.Format(time.RFC3339)
		st.GeneratedAt = st.LastGoodAt
	}
	if !l.sourceAt.IsZero() {
		st.SourceUpdatedAt = l.sourceAt.Format(time.RFC3339)
	}
	return st
}

// ---- PortalLive (the five-method read seam) ----

// Status satisfies PortalLive.
func (l *OodaLive) Status() PortalLiveStatus {
	_ = l.refresh(false)
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.statusLocked()
}

// File satisfies PortalLive. The OODA app is served entirely by the /api/ooda
// routes, so there are no contract data files — /data/meta.json alone exists,
// mirroring AION's, so a shared client helper can read the revision.
func (l *OodaLive) File(urlPath string) ([]byte, string, error) {
	if urlPath != "/data/meta.json" {
		return nil, "", fmt.Errorf("no such file: %s", urlPath)
	}
	st := l.Status()
	b, err := json.Marshal(map[string]any{
		"contract": "1", "source": "manifest-live-ooda",
		"revision": st.EffectiveRevision, "generatedAt": st.GeneratedAt,
	})
	return b, "application/json", err
}

// TeamStateJSON satisfies PortalLive: the overlay dump the shared
// /api/team/state route serves.
func (l *OodaLive) TeamStateJSON() any {
	team, _ := l.teamStore()
	ext := teamportal.Ext{Comments: map[string][]teamportal.Comment{}, Overrides: map[string]teamportal.Override{}}
	if team != nil {
		ext = team.Ext()
	}
	return map[string]any{
		"comments":  ext.Comments,
		"overrides": ext.Overrides,
		"items":     ext.Items,
		"proposals": ext.Proposals,
		"sync":      l.Status(),
	}
}

// People satisfies PortalLive: the RE roster (people.md partners) plus every
// contractor record, so both are assignable and both render as work owners.
func (l *OodaLive) People() []portalPerson {
	snap := l.Snapshot()
	if snap == nil {
		return nil
	}
	out := make([]portalPerson, 0, len(snap.People)+len(snap.Contractors))
	seen := map[string]bool{}
	for _, p := range snap.People {
		if p == nil || p.Initials == "" {
			continue
		}
		seen[strings.ToUpper(p.Initials)] = true
		out = append(out, portalPerson{Initials: p.Initials, Name: p.Name, Email: p.Email})
	}
	for _, c := range snap.Contractors {
		ini := contractorInitials(c.Name)
		if ini == "" || seen[ini] {
			continue // a partner who is also a vendor stays ONE person
		}
		seen[ini] = true
		out = append(out, portalPerson{Initials: ini, Name: c.Name, Email: c.Email})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Initials < out[j].Initials })
	return out
}

// OwnerOf satisfies PortalLive — the assignee lock's oracle. Resolution order:
// RE backlog item → property rock-tree node → bare property anchor →
// portal-created team item. Owners come back NORMALIZED through the same
// alias map the WORK view groups by (oodaOwnerAliases): the vault names
// people by initials, slug, or full name interchangeably, and the lock
// compares against roster initials — a raw `olga-sobkiv` would 403 the very
// member the WORK view shows the button to.
func (l *OodaLive) OwnerOf(itemID string) (string, bool) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", false
	}
	// A staged owner override IS the item's holder until the base catches up
	// (or forever, for overlay-only rock items). Without this, someone
	// assigned from the portal could SEE their name on the item (buildOodaWork
	// reads the override) while every edit still 403'd against the base's
	// empty owner — the display and the gate must consult the same truth.
	if team, _ := l.teamStore(); team != nil {
		if ov, ok := team.Ext().Overrides[itemID]; ok {
			if v := strings.TrimSpace(ov.Fields["owner"]); v != "" {
				var alias map[string]string
				if snap := l.Snapshot(); snap != nil {
					alias = oodaOwnerAliases(snap.People)
				}
				return oodaOwner(v, alias), true
			}
		}
	}
	snap := l.Snapshot()
	var alias map[string]string
	if snap != nil {
		alias = oodaOwnerAliases(snap.People)
		for _, it := range snap.Backlog {
			if it != nil && it.ID == itemID {
				return oodaOwner(it.Owner, alias), true
			}
		}
		if prop, workID, ok := parseOodaNodeID(itemID); ok {
			for i := range snap.Properties {
				if !strings.EqualFold(snap.Properties[i].Slug, prop) {
					continue
				}
				owner := ""
				found := false
				realestate.WalkNodes(snap.Properties[i].Work, func(_ *realestate.WorkStage, n *realestate.WorkNode) {
					if found || n.ID != workID || n.Task == nil {
						return
					}
					found = true
					owner = n.Task.Owner
				})
				if found {
					return oodaOwner(owner, alias), true
				}
			}
		}
		// a bare `prop/<slug>` is the property's own thread anchor (the
		// detail page's comment thread). It exists — comments succeed — but
		// nobody "holds" it, so state writes stay 403 (owner "").
		if rest, found := strings.CutPrefix(itemID, "prop/"); found && !strings.Contains(rest, "#") {
			for i := range snap.Properties {
				if strings.EqualFold(snap.Properties[i].Slug, rest) {
					return "", true
				}
			}
		}
	}
	if team, _ := l.teamStore(); team != nil {
		for _, it := range team.Ext().Items {
			if it.ID == itemID {
				return oodaOwner(it.Owner, alias), true
			}
		}
	}
	return "", false
}

// parseOodaNodeID splits "prop/<slug>#<work-id>" — the rock-tree item id.
func parseOodaNodeID(id string) (slug, workID string, ok bool) {
	rest, found := strings.CutPrefix(id, "prop/")
	if !found {
		return "", "", false
	}
	slug, workID, found = strings.Cut(rest, "#")
	return slug, workID, found && slug != "" && workID != ""
}

// contractorInitials derives a stable owner token for a contractor record the
// same way the cockpit's assignee lists do: first letters, upper, max 3.
func contractorInitials(name string) string {
	var b strings.Builder
	for _, part := range strings.Fields(name) {
		if b.Len() >= 3 {
			break
		}
		r := []rune(part)
		if len(r) > 0 && (r[0] >= 'a' && r[0] <= 'z' || r[0] >= 'A' && r[0] <= 'Z') {
			b.WriteString(strings.ToUpper(string(r[0])))
		}
	}
	return b.String()
}

var _ PortalLive = (*OodaLive)(nil)
