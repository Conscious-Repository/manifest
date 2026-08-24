package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/teamportal"
)

// AionLive owns the one live AION projection shared by the private cockpit
// and team portal. The vault is the owner-authored base; team state is layered
// at read time.
//
// Since the §12 amendment of 2026-08-24 that layer is a STAGING AREA, not a
// parallel truth: aion_sync_back.go reconciles it into system/aion/backlog.md
// and the override is cleared once the record carries it. What you see here is
// the settled answer either way — before the sync, from the overlay; after it,
// from the line itself.
type AionLive struct {
	s *Server

	mu          sync.Mutex
	team        *teamportal.Store
	admin       teamportal.Identity
	lastCheck   time.Time
	currentRev  string
	servingRev  string
	files       map[string][]byte
	warnings    []string
	stale       bool
	lastError   string
	lastGoodAt  time.Time
	sourceAt    time.Time
	cacheLoaded bool
	opMu        sync.Mutex

	// packDir is kairos's standing context-pack destination (aion_pack.go);
	// "" = pack disabled. packErrRev keeps a failed export to one log line
	// per revision.
	packDir    string
	packErrRev string
}

type aionLiveCache struct {
	Revision string            `json:"revision"`
	At       time.Time         `json:"at"`
	Files    map[string][]byte `json:"files"`
}

// AionLiveStatus is returned by both listeners' lightweight revision route.
type AionLiveStatus struct {
	BaseRevision      string   `json:"baseRevision"`
	TeamRevision      string   `json:"teamRevision"`
	EffectiveRevision string   `json:"effectiveRevision"`
	ServingRevision   string   `json:"servingRevision"`
	Stale             bool     `json:"stale"`
	Degraded          bool     `json:"degraded"`
	Error             string   `json:"error,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	GeneratedAt       string   `json:"generatedAt,omitempty"`
	SourceUpdatedAt   string   `json:"sourceUpdatedAt,omitempty"`
	LastGoodAt        string   `json:"lastGoodAt,omitempty"`
}

// AionEffectiveItem is the canonical composed item shape used by the portal.
type AionEffectiveItem struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Title    string  `json:"title"`
	Owner    *string `json:"owner"`
	Source   string  `json:"source"`
	Captured string  `json:"captured"`
	Rock     *string `json:"rock"`
	Due      *string `json:"due"`
	Status   *string `json:"status"`
	DoneOn   *string `json:"done_on"`
	NeededBy *string `json:"needed_by"`
	Decided  *string `json:"decided"`
	Outcome  *string `json:"outcome"`
	Team     bool    `json:"team,omitempty"`
	AddedBy  string  `json:"added_by,omitempty"`

	SourceType   string            `json:"source_type"` // vault | team
	OverrideBy   string            `json:"override_by,omitempty"`
	OverrideAt   *time.Time        `json:"override_at,omitempty"`
	Override     map[string]string `json:"override,omitempty"`
	LastActor    string            `json:"last_actor,omitempty"`
	LastAt       *time.Time        `json:"last_at,omitempty"`
	CommentCount int               `json:"comment_count,omitempty"`
}

type aionTeamStateView struct {
	teamportal.Ext
	EffectiveItems []AionEffectiveItem `json:"effective_items"`
	Sync           AionLiveStatus      `json:"sync"`
}

func newAionLive(s *Server) *AionLive {
	l := &AionLive{s: s, files: map[string][]byte{}}
	_ = l.refresh(true)
	return l
}

func (l *AionLive) UseTeam(st *teamportal.Store, admin teamportal.Identity) {
	l.mu.Lock()
	l.team, l.admin = st, admin
	l.mu.Unlock()
}

func (l *AionLive) teamStore() (*teamportal.Store, teamportal.Identity) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.team, l.admin
}

func (l *AionLive) cachePath() string {
	return filepath.Join(l.s.aionDataDir, "aion", "live-contract.json")
}

func (l *AionLive) sourceRevision() string {
	h := sha256.New()
	if l.s.aion != nil {
		for _, name := range aion.Files {
			h.Write([]byte(name))
			h.Write([]byte(l.s.aion.RawFile(name)))
		}
	}
	if b, err := json.Marshal(l.s.aionExportGoals()); err == nil {
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (l *AionLive) loadFallbackLocked() {
	if l.cacheLoaded {
		return
	}
	l.cacheLoaded = true
	var c aionLiveCache
	if b, err := os.ReadFile(l.cachePath()); err == nil && json.Unmarshal(b, &c) == nil && len(c.Files) > 0 {
		l.files, l.servingRev, l.lastGoodAt = c.Files, c.Revision, c.At
		return
	}
	// First-release bootstrap: the last compiled portal contract remains a
	// safe fallback until the first valid live snapshot is promoted.
	sub, err := fs.Sub(webFiles, "web/portal")
	if err != nil {
		return
	}
	files := map[string][]byte{}
	for _, contractPath := range aion.ContractPaths() {
		urlPath := contractURLPath(contractPath)
		if b, err := fs.ReadFile(sub, strings.TrimPrefix(urlPath, "/")); err == nil {
			files[urlPath] = b
		}
	}
	if len(files) > 0 {
		l.files = files
		l.servingRev = "embedded"
	}
}

func (l *AionLive) saveLocked() {
	c := aionLiveCache{Revision: l.servingRev, At: l.lastGoodAt, Files: l.files}
	b, err := json.Marshal(c)
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

func contractURLPath(contractPath string) string {
	const prefix = "server/web/portal"
	return strings.TrimPrefix(filepath.ToSlash(contractPath), prefix)
}

func (l *AionLive) refresh(force bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !force && time.Since(l.lastCheck) < time.Second {
		return nil
	}
	l.lastCheck = time.Now()
	l.loadFallbackLocked()
	rev := l.sourceRevision()
	if rev == l.currentRev && len(l.files) > 0 {
		return nil
	}
	l.currentRev = rev
	now := time.Now().UTC()
	l.sourceAt = now
	rendered, err := aion.RenderContract(l.s.aionExportInput(now.Format(time.RFC3339)))
	if err == nil {
		errs, warnings := aion.AcceptContract(rendered, aionInScopeQuarters(now))
		l.warnings = append([]string{}, warnings...)
		if len(errs) > 0 {
			err = fmt.Errorf("%s", strings.Join(errs, "; "))
		}
	}
	if err != nil {
		l.stale, l.lastError = true, err.Error()
		return err
	}
	files := map[string][]byte{}
	for contractPath, b := range rendered {
		files[contractURLPath(contractPath)] = append([]byte(nil), b...)
	}
	l.files, l.servingRev = files, rev
	l.stale, l.lastError, l.lastGoodAt = false, "", now
	l.saveLocked()
	// the standing context pack rides the same recompose: a new revision
	// here is exactly "the source moved", the only time the pack rewrites
	l.syncPackLocked()
	return nil
}

func (l *AionLive) statusLocked() AionLiveStatus {
	teamRev := ""
	if l.team != nil {
		teamRev = l.team.Revision()
	}
	h := sha256.Sum256([]byte(l.currentRev + "\x00" + teamRev))
	st := AionLiveStatus{
		BaseRevision: l.currentRev, TeamRevision: teamRev,
		EffectiveRevision: hex.EncodeToString(h[:])[:16], ServingRevision: l.servingRev,
		Stale: l.stale, Degraded: l.stale, Error: l.lastError, Warnings: append([]string{}, l.warnings...),
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

func (l *AionLive) Status() AionLiveStatus {
	_ = l.refresh(false)
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.statusLocked()
}

// LiveSyncState satisfies signals.AionLiveState.
func (l *AionLive) LiveSyncState() (bool, string, string) {
	st := l.Status()
	return st.Stale, st.ServingRevision, st.Error
}

func (l *AionLive) File(urlPath string) ([]byte, string, error) {
	_ = l.refresh(false)
	l.mu.Lock()
	defer l.mu.Unlock()
	if urlPath == "/data/meta.json" {
		st := l.statusLocked()
		meta := map[string]any{
			"contract": "2", "source": "manifest-live", "revision": st.ServingRevision,
			"base_revision": st.BaseRevision, "team_revision": st.TeamRevision,
			"generated_at": st.GeneratedAt, "source_updated_at": st.SourceUpdatedAt,
			"published_at": st.LastGoodAt, "stale": st.Stale,
			"warnings": st.Warnings, "error": st.Error,
		}
		b, _ := json.MarshalIndent(meta, "", "  ")
		return append(b, '\n'), st.EffectiveRevision, nil
	}
	b, ok := l.files[urlPath]
	if !ok {
		return nil, "", os.ErrNotExist
	}
	return append([]byte(nil), b...), l.servingRev, nil
}

func strPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (l *AionLive) effectiveItems() []AionEffectiveItem {
	if l.s.aion == nil {
		return []AionEffectiveItem{}
	}
	_ = l.refresh(false)
	ext := teamportal.Ext{Comments: map[string][]teamportal.Comment{}, Overrides: map[string]teamportal.Override{}}
	var activity []teamportal.Entry
	if team, _ := l.teamStore(); team != nil {
		ext = team.Ext()
		activity = team.Activity(time.Time{})
	}
	archived := map[string]bool{}
	for _, a := range ext.Archives {
		archived[a.ID] = true
	}
	items := []AionEffectiveItem{}
	l.mu.Lock()
	backlogBytes := append([]byte(nil), l.files["/data/backlog.json"]...)
	l.mu.Unlock()
	var base struct {
		Items []AionEffectiveItem `json:"items"`
	}
	_ = json.Unmarshal(backlogBytes, &base)
	for _, it := range base.Items {
		if archived[it.ID] {
			continue
		}
		it.SourceType = "vault"
		it.CommentCount = len(ext.Comments[it.ID])
		items = append(items, it)
	}
	for _, it := range ext.Items {
		if archived[it.ID] {
			continue
		}
		items = append(items, AionEffectiveItem{
			ID: it.ID, Kind: it.Kind, Title: it.Title, Owner: strPtr(it.Owner), Captured: it.Captured,
			Rock: strPtr(it.Rock), Due: strPtr(it.Due), Status: strPtr(it.Status), DoneOn: strPtr(it.DoneOn),
			NeededBy: strPtr(it.NeededBy), Decided: strPtr(it.Decided), Outcome: strPtr(it.Outcome),
			Team: true, AddedBy: it.AddedBy, SourceType: "team", CommentCount: len(ext.Comments[it.ID]),
		})
	}
	for i := range items {
		ov, ok := ext.Overrides[items[i].ID]
		if !ok {
			continue
		}
		for key, val := range ov.Fields {
			switch key {
			case "status":
				items[i].Status = strPtr(val)
			case "done_on":
				items[i].DoneOn = strPtr(val)
			case "due":
				items[i].Due = strPtr(val)
			case "needed_by":
				items[i].NeededBy = strPtr(val)
			case "outcome":
				items[i].Outcome = strPtr(val)
			// widened 2026-08-24 with teamportal.PatchFields. The two must
			// agree: a field a member can PATCH but this switch ignores would
			// save, vanish from every read, and reappear only after the
			// reconciler wrote it to the vault — an edit that looks lost.
			case "title":
				items[i].Title = val
			case "owner":
				items[i].Owner = strPtr(val)
			case "decided":
				items[i].Decided = strPtr(val)
			}
		}
		items[i].OverrideBy, items[i].OverrideAt = ov.By, &ov.At
		items[i].Override = map[string]string{}
		for key, value := range ov.Fields {
			items[i].Override[key] = value
		}
	}
	latest := map[string]teamportal.Entry{}
	for _, entry := range activity {
		itemID, _ := entry.Payload["item"].(string)
		if itemID != "" {
			latest[itemID] = entry
		}
	}
	for i := range items {
		if entry, ok := latest[items[i].ID]; ok {
			at := entry.TS
			items[i].LastActor, items[i].LastAt = entry.Actor, &at
		}
	}
	// A journaled owner write is the top layer until its overlay cleanup is
	// complete. This makes partial two-file recovery converge visibly at once.
	if j, ok := l.readJournal(); ok {
		if j.Kind == "archive" {
			kept := items[:0]
			for _, item := range items {
				if item.ID != j.Item {
					kept = append(kept, item)
				}
			}
			items = kept
		} else if j.Kind == "update" || j.Kind == "decide" {
			for i := range items {
				if items[i].ID == j.Item {
					applyEffectiveFields(&items[i], j.Set)
				}
			}
		}
	}
	return items
}

func applyEffectiveFields(it *AionEffectiveItem, fields map[string]string) {
	for key, val := range fields {
		switch key {
		case "title":
			it.Title = val
		case "owner":
			it.Owner = strPtr(val)
		case "rock":
			it.Rock = strPtr(val)
		case "due":
			it.Due = strPtr(val)
		case "needed_by":
			it.NeededBy = strPtr(val)
		case "outcome":
			it.Outcome = strPtr(val)
		case "status":
			it.Status = strPtr(val)
		}
	}
}

func (l *AionLive) EffectiveItems() []AionEffectiveItem {
	return l.effectiveItems()
}

// ManifestItems adapts the neutral live item shape to the cockpit's historic
// camelCase backlog JSON while adding collaboration provenance additively.
func (l *AionLive) ManifestItems() []map[string]any {
	out := make([]map[string]any, 0)
	for _, it := range l.EffectiveItems() {
		value := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}
		sources := []string{}
		if it.Source != "" {
			sources = append(sources, it.Source)
		}
		m := map[string]any{
			"id": it.ID, "text": it.Title, "kind": it.Kind, "owner": value(it.Owner),
			"sources": sources, "captured": it.Captured, "rock": value(it.Rock),
			"due": value(it.Due), "status": value(it.Status), "doneOn": value(it.DoneOn),
			"neededBy": value(it.NeededBy), "decided": value(it.Decided), "outcome": value(it.Outcome),
			"checked": value(it.Status) == aion.StatusDone, "children": []any{},
			"team": it.Team, "addedBy": it.AddedBy, "sourceType": it.SourceType,
			"overrideBy": it.OverrideBy, "override": it.Override, "lastActor": it.LastActor,
			"commentCount": it.CommentCount,
		}
		if it.OverrideAt != nil {
			m["overrideAt"] = it.OverrideAt
		}
		if it.LastAt != nil {
			m["lastAt"] = it.LastAt
		}
		out = append(out, m)
	}
	return out
}

// TeamStateJSON satisfies PortalLive: the same body TeamState returns, boxed
// so a second portal can serve its own effective-item shape from the shared
// /api/team/state route. TeamState keeps its concrete type for the cockpit.
func (l *AionLive) TeamStateJSON() any { return l.TeamState() }

func (l *AionLive) TeamState() aionTeamStateView {
	ext := teamportal.Ext{Comments: map[string][]teamportal.Comment{}, Overrides: map[string]teamportal.Override{}}
	if team, _ := l.teamStore(); team != nil {
		ext = team.Ext()
	}
	return aionTeamStateView{Ext: ext, EffectiveItems: l.EffectiveItems(), Sync: l.Status()}
}

func (l *AionLive) OwnerOf(itemID string) (string, bool) {
	for _, it := range l.effectiveItems() {
		if it.ID == itemID {
			if it.Owner == nil {
				return "", true
			}
			return *it.Owner, true
		}
	}
	return "", false
}

func (l *AionLive) People() []portalPerson {
	if l.s.aion == nil {
		return nil
	}
	var out []portalPerson
	for _, p := range l.s.aion.LoadPeople().People() {
		out = append(out, portalPerson{Initials: p.Initials, Name: p.Name, Email: p.Email})
	}
	return out
}

func (l *AionLive) OrphanTeamIDs() []string {
	team, _ := l.teamStore()
	if team == nil {
		return nil
	}
	known := map[string]bool{}
	for _, it := range l.effectiveItems() {
		known[it.ID] = true
	}
	ext := team.Ext()
	for _, archived := range ext.Archives {
		known[archived.ID] = true
	}
	seen := map[string]bool{}
	for id := range ext.Comments {
		if !known[id] {
			seen[id] = true
		}
	}
	for id := range ext.Overrides {
		if !known[id] {
			seen[id] = true
		}
	}
	var out []string
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
