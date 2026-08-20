package teamportal

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the derived team state on metis (OPTION A, decided 2026-08-13):
// a directory (production: /shared/apps/aion-portal) holding
//
//	activity.log    — append-only JSONL, one line per write (ts, actor, action, payload)
//	items.ext.json  — the extended item store (comments, overrides, team items, proposals)
//	emails.json     — optional hand-edited email → initials overrides (read-only here)
//
// Everything is versionable (the owner may git-commit the directory for
// backup; nothing here requires a commit at runtime) and disposable in the
// architectural sense: it is portal team state, never vault truth.
type Store struct {
	dir    string
	policy Policy // zero value = the AION domain gate (ooda-portal plan, Stage A)
	mu     sync.Mutex
}

// WithPolicy scopes the store's own identity check — today only the proposal
// TARGET gate below. The zero value keeps AION's domain gate exactly, so every
// existing New(dir) caller is unaffected. Fluent, mirroring Auth.WithTokens.
func (s *Store) WithPolicy(p Policy) *Store { s.policy = p; return s }

// New opens (creating if needed) the team-state directory.
func New(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("teamportal: empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string        { return s.dir }
func (s *Store) extPath() string    { return filepath.Join(s.dir, "items.ext.json") }
func (s *Store) logPath() string    { return filepath.Join(s.dir, "activity.log") }
func (s *Store) emailsPath() string { return filepath.Join(s.dir, "emails.json") }

// Comment is one team comment on an item (any signed-in member may comment).
// Files (todo-panel plan D4) are content-addressed refs into the shared dir's
// files/ tree; Mentions (kairos plan Phase C) are structural roster tokens
// (`agent:kairos`, optionally intent-tagged `agent:kairos::brief`) — both
// additive JSON; older portal clients simply ignore the fields.
type Comment struct {
	ID         string    `json:"id"`
	Item       string    `json:"item"`
	Author     string    `json:"author"`      // email
	AuthorName string    `json:"author_name"` // display name from Google
	Text       string    `json:"text"`
	Mentions   []string  `json:"mentions,omitempty"`
	Files      []FileRef `json:"files,omitempty"`
	At         time.Time `json:"at"`
}

// FileRef is one content-addressed attachment (sha256 blob in files/).
type FileRef struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime,omitempty"`
}

// Override is the latest team-applied field state for one item — merged over
// the published backlog item at read time. History lives in activity.log.
type Override struct {
	Fields map[string]string `json:"fields"` // status | done_on | due | needed_by | outcome
	By     string            `json:"by"`     // email of the last writer
	At     time.Time         `json:"at"`
}

// TeamItem is a member-added item. Its id carries the visible `team/` tag and
// its JSON shape mirrors data/backlog.json items so the portal renders it
// alongside published ones.
type TeamItem struct {
	ID       string `json:"id"` // "team/<slug>"
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Owner    string `json:"owner"` // initials (or email local-part when unmapped)
	Captured string `json:"captured"`
	Rock     string `json:"rock,omitempty"`
	Due      string `json:"due,omitempty"`
	Status   string `json:"status"`
	DoneOn   string `json:"done_on,omitempty"`
	NeededBy string `json:"needed_by,omitempty"`
	Decided  string `json:"decided,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Team     bool   `json:"team"`               // the distinguishing mark
	AddedBy  string `json:"added_by,omitempty"` // email of the creator
}

// ArchivedItem preserves the last visible item snapshot when the private
// cockpit archives collaborative work. Threads remain keyed by ID, so history
// stays readable without keeping the item in active projections.
type ArchivedItem struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Title    string    `json:"title"`
	Owner    string    `json:"owner,omitempty"`
	Captured string    `json:"captured,omitempty"`
	Rock     string    `json:"rock,omitempty"`
	Due      string    `json:"due,omitempty"`
	Status   string    `json:"status,omitempty"`
	DoneOn   string    `json:"done_on,omitempty"`
	NeededBy string    `json:"needed_by,omitempty"`
	Decided  string    `json:"decided,omitempty"`
	Outcome  string    `json:"outcome,omitempty"`
	Team     bool      `json:"team,omitempty"`
	AddedBy  string    `json:"added_by,omitempty"`
	By       string    `json:"archived_by"`
	At       time.Time `json:"archived_at"`
}

// Proposal is a member's suggested item for ANOTHER person. It mirrors the
// approvals model (§4/§5 proposals lane): pending until an authorized decider
// — the portal owner (admin) or the target themself — approves or rejects.
type Proposal struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Title        string    `json:"title"`
	Rock         string    `json:"rock,omitempty"`
	Due          string    `json:"due,omitempty"`
	Target       string    `json:"target"`        // email of the person it's for
	TargetOwner  string    `json:"target_owner"`  // their initials (display + item owner on approve)
	ProposedBy   string    `json:"proposed_by"`   // email
	ProposedName string    `json:"proposed_name"` // display name
	At           time.Time `json:"at"`
	Status       string    `json:"status"` // pending | approved | rejected
	DecidedBy    string    `json:"decided_by,omitempty"`
	DecidedAt    time.Time `json:"decided_at,omitempty"`
	ItemID       string    `json:"item_id,omitempty"` // the team item minted on approval
}

// Ext is the whole extended store (items.ext.json).
type Ext struct {
	Comments     map[string][]Comment `json:"comments"`  // item id → comments (oldest first)
	Overrides    map[string]Override  `json:"overrides"` // item id → latest team field state
	Items        []TeamItem           `json:"items"`     // team/-tagged member adds
	Proposals    []Proposal           `json:"proposals"`
	Archives     []ArchivedItem       `json:"archives,omitempty"`
	IDMigrations map[string]string    `json:"id_migrations,omitempty"` // legacy id → stable id
}

// Entry is one activity.log line.
type Entry struct {
	TS      time.Time      `json:"ts"`
	Actor   string         `json:"actor"` // email
	Action  string         `json:"action"`
	Payload map[string]any `json:"payload"`
}

// Actions written to activity.log.
const (
	ActComment       = "comment"
	ActDeleteComment = "delete-comment"
	ActPatch         = "patch"
	ActAdd           = "add-item"
	ActPropose       = "propose"
	ActDecide        = "decide-proposal"
	ActOwnerResolve  = "owner-resolve"
	ActOwnerPatch    = "owner-patch"
	ActArchive       = "archive-item"
	ActMigrateID     = "migrate-item-id"
)

// Sentinel errors the delete path returns so the HTTP layer can map them to
// 404/403 — a bare errors.New would collapse both into a generic 400.
var (
	ErrCommentNotFound = errors.New("comment not found")
	ErrNotYours        = errors.New("only the author or the portal owner may delete this comment")
)

// PatchFields is the closed set a team PATCH may touch.
var PatchFields = map[string]bool{
	"status": true, "done_on": true, "due": true, "needed_by": true, "outcome": true,
}

// PatchStatuses is the closed status vocabulary (matches the portal's marks).
var PatchStatuses = map[string]bool{"open": true, "in_progress": true, "done": true}

func (s *Store) readExt() Ext {
	ext := Ext{Comments: map[string][]Comment{}, Overrides: map[string]Override{}}
	if b, err := os.ReadFile(s.extPath()); err == nil {
		_ = json.Unmarshal(b, &ext)
		if ext.Comments == nil {
			ext.Comments = map[string][]Comment{}
		}
		if ext.Overrides == nil {
			ext.Overrides = map[string]Override{}
		}
	}
	return ext
}

// writeExt writes items.ext.json atomically (tmp + rename), then appends the
// activity line. The log line lands AFTER the state it describes; a crash
// between the two loses only the trail line, never the state.
func (s *Store) writeExt(ext Ext, e Entry) error {
	b, err := json.MarshalIndent(ext, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.extPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.extPath()); err != nil {
		return err
	}
	return s.appendLog(e)
}

func (s *Store) appendLog(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Ext returns the current extended state (the portal's /api/team/state body).
func (s *Store) Ext() Ext {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readExt()
}

// MigrateItemIDs atomically rekeys collaboration from legacy derived ids to
// stable ids. The append-only activity log is not rewritten; IDMigrations
// preserves aliases so historical activity remains addressable by inspectors.
// Only ids that actually have team state or activity are persisted.
func (s *Store) MigrateItemIDs(mapping map[string]string, actor Identity, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	if ext.IDMigrations == nil {
		ext.IDMigrations = map[string]string{}
	}
	logBody, _ := os.ReadFile(s.logPath())
	legacyIDs := make([]string, 0, len(mapping))
	for legacy := range mapping {
		legacyIDs = append(legacyIDs, legacy)
	}
	sort.Strings(legacyIDs)

	changed := 0
	for _, legacy := range legacyIDs {
		stable := strings.TrimSpace(mapping[legacy])
		legacy = strings.TrimSpace(legacy)
		if legacy == "" || stable == "" || legacy == stable || ext.IDMigrations[legacy] == stable {
			continue
		}
		hasActivity := bytes.Contains(logBody, []byte(`"item":"`+legacy+`"`))
		comments, hasComments := ext.Comments[legacy]
		legacyOverride, hasOverride := ext.Overrides[legacy]
		hasArchive := false
		for _, archived := range ext.Archives {
			if archived.ID == legacy {
				hasArchive = true
				break
			}
		}
		if !hasActivity && !hasComments && !hasOverride && !hasArchive {
			continue
		}

		if hasComments {
			for i := range comments {
				comments[i].Item = stable
			}
			ext.Comments[stable] = append(ext.Comments[stable], comments...)
			sort.SliceStable(ext.Comments[stable], func(i, j int) bool {
				return ext.Comments[stable][i].At.Before(ext.Comments[stable][j].At)
			})
			delete(ext.Comments, legacy)
		}
		if hasOverride {
			if current, ok := ext.Overrides[stable]; ok {
				older, newer := legacyOverride, current
				if current.At.Before(legacyOverride.At) {
					older, newer = current, legacyOverride
				}
				merged := map[string]string{}
				for key, value := range older.Fields {
					merged[key] = value
				}
				for key, value := range newer.Fields {
					merged[key] = value
				}
				newer.Fields = merged
				ext.Overrides[stable] = newer
			} else {
				ext.Overrides[stable] = legacyOverride
			}
			delete(ext.Overrides, legacy)
		}
		for i := range ext.Archives {
			if ext.Archives[i].ID == legacy {
				ext.Archives[i].ID = stable
			}
		}
		for i := range ext.Proposals {
			if ext.Proposals[i].ItemID == legacy {
				ext.Proposals[i].ItemID = stable
			}
		}
		ext.IDMigrations[legacy] = stable
		changed++
	}
	if changed == 0 {
		return 0, nil
	}
	return changed, s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActMigrateID,
		Payload: map[string]any{"count": changed}})
}

// AliasesFor returns legacy ids whose activity belongs to stableID.
func (s *Store) AliasesFor(stableID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for legacy, stable := range s.readExt().IDMigrations {
		if stable == stableID {
			out = append(out, legacy)
		}
	}
	sort.Strings(out)
	return out
}

// Revision is a strong token over the current team projection and activity
// trail. The files are intentionally small; hashing avoids timestamp races on
// shared filesystems and makes conditional refresh exact.
func (s *Store) Revision() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := sha256.New()
	for _, path := range []string{s.extPath(), s.logPath()} {
		if b, err := os.ReadFile(path); err == nil {
			h.Write([]byte(path))
			h.Write(b)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// EmailOverrides reads the optional hand-edited email → initials map.
func (s *Store) EmailOverrides() map[string]string {
	out := map[string]string{}
	if b, err := os.ReadFile(s.emailsPath()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// TeamOwner returns the owner of a team/ item, if the id names one.
func (s *Store) TeamOwner(itemID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.readExt().Items {
		if it.ID == itemID {
			return it.Owner, true
		}
	}
	return "", false
}

// TeamItem returns a copy of an active team-added item.
func (s *Store) TeamItem(itemID string) (TeamItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.readExt().Items {
		if it.ID == itemID {
			return it, true
		}
	}
	return TeamItem{}, false
}

// HasCollaboration reports whether an item has state that must survive an
// owner delete as an archive rather than becoming an orphaned thread.
func (s *Store) HasCollaboration(itemID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	if len(ext.Comments[itemID]) > 0 {
		return true
	}
	if _, ok := ext.Overrides[itemID]; ok {
		return true
	}
	for _, it := range ext.Items {
		if it.ID == itemID {
			return true
		}
	}
	if b, err := os.ReadFile(s.logPath()); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			var entry Entry
			if json.Unmarshal([]byte(line), &entry) == nil && entry.Payload["item"] == itemID {
				return true
			}
		}
	}
	return false
}

// OwnerResolve clears only the overlay keys explicitly superseded by the
// private cockpit and leaves an attributed activity record.
func (s *Store) OwnerResolve(actor Identity, itemID string, keys []string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	ov, ok := ext.Overrides[itemID]
	if !ok || len(ov.Fields) == 0 {
		return nil
	}
	cleared := map[string]string{}
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if old, exists := ov.Fields[key]; exists {
			cleared[key] = old
			delete(ov.Fields, key)
		}
	}
	if len(cleared) == 0 {
		return nil
	}
	if len(ov.Fields) == 0 {
		delete(ext.Overrides, itemID)
	} else {
		ov.By, ov.At = actor.Email, now.UTC()
		ext.Overrides[itemID] = ov
	}
	return s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActOwnerResolve,
		Payload: map[string]any{"item": itemID, "cleared": cleared}})
}

// OwnerPatchTeamItem is the trusted private-cockpit lane for editing a
// team-added item. Portal callers continue to use Patch and its assignee lock.
func (s *Store) OwnerPatchTeamItem(actor Identity, itemID string, fields map[string]string, now time.Time) (TeamItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	idx := -1
	for i := range ext.Items {
		if ext.Items[i].ID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return TeamItem{}, errors.New("team item not found")
	}
	it := ext.Items[idx]
	ov := ext.Overrides[itemID]
	if ov.Fields == nil {
		ov.Fields = map[string]string{}
	}
	for key, val := range fields {
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "title":
			if val == "" {
				return TeamItem{}, errors.New("title cannot be empty")
			}
			it.Title = val
		case "kind":
			if val != "task" && val != "decision" {
				return TeamItem{}, errors.New("kind must be task or decision")
			}
			it.Kind = val
		case "owner":
			it.Owner = val
		case "rock":
			it.Rock = val
		case "due":
			it.Due = val
			delete(ov.Fields, "due")
		case "done_on":
			it.DoneOn = val
			delete(ov.Fields, "done_on")
		case "needed_by":
			it.NeededBy = val
			delete(ov.Fields, "needed_by")
		case "outcome":
			it.Outcome = val
			delete(ov.Fields, "outcome")
		case "decided":
			it.Decided = val
		case "status":
			if !PatchStatuses[val] && val != "decided" {
				return TeamItem{}, fmt.Errorf("status %q is invalid", val)
			}
			it.Status = val
			delete(ov.Fields, "status")
			if val == "done" && it.DoneOn == "" {
				it.DoneOn = now.Format("2006-01-02")
			} else if val != "done" {
				it.DoneOn = ""
			}
			delete(ov.Fields, "done_on")
			if val == "decided" && it.Decided == "" {
				it.Decided = now.Format("2006-01-02")
			}
		default:
			return TeamItem{}, fmt.Errorf("field %q is not owner-editable", key)
		}
	}
	if len(ov.Fields) == 0 {
		delete(ext.Overrides, itemID)
	} else {
		ov.By, ov.At = actor.Email, now.UTC()
		ext.Overrides[itemID] = ov
	}
	ext.Items[idx] = it
	err := s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActOwnerPatch,
		Payload: map[string]any{"item": itemID, "fields": fields}})
	return it, err
}

// Archive removes an item from active team state while preserving a complete
// snapshot, its comments, and the append-only activity trail.
func (s *Store) Archive(actor Identity, snap ArchivedItem, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	for _, a := range ext.Archives {
		if a.ID == snap.ID {
			return nil
		}
	}
	for i, it := range ext.Items {
		if it.ID == snap.ID {
			ext.Items = append(ext.Items[:i:i], ext.Items[i+1:]...)
			break
		}
	}
	delete(ext.Overrides, snap.ID)
	snap.By, snap.At = actor.Email, now.UTC()
	ext.Archives = append(ext.Archives, snap)
	return s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActArchive,
		Payload: map[string]any{"item": snap.ID, "title": snap.Title}})
}

// AddComment appends a comment (any signed-in member; the caller has already
// verified the item exists).
func (s *Store) AddComment(actor Identity, itemID, text string, now time.Time) (Comment, error) {
	return s.AddCommentFull(actor, itemID, text, nil, nil, now)
}

// AddCommentWithFiles is AddComment carrying content-addressed attachments
// (the todo-panel thread path; the portal's own composer stays text-only).
func (s *Store) AddCommentWithFiles(actor Identity, itemID, text string, files []FileRef, now time.Time) (Comment, error) {
	return s.AddCommentFull(actor, itemID, text, files, nil, now)
}

// AddCommentFull is the real writer: attachments + structural mentions
// (kairos plan Phase C — the portal composer records agent tokens here).
func (s *Store) AddCommentFull(actor Identity, itemID, text string, files []FileRef, mentions []string, now time.Time) (Comment, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 {
		return Comment{}, errors.New("empty comment")
	}
	if len(text) > 4000 {
		return Comment{}, errors.New("comment too long (4000 max)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	c := Comment{
		ID:   fmt.Sprintf("c-%d", now.UnixNano()),
		Item: itemID, Author: actor.Email, AuthorName: actor.Name,
		Text: text, Mentions: mentions, Files: files, At: now.UTC(),
	}
	ext.Comments[itemID] = append(ext.Comments[itemID], c)
	err := s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActComment,
		Payload: map[string]any{"item": itemID, "text": text, "files": len(files)}})
	return c, err
}

// LogAction appends an activity-trail-only entry (no ext state change) — the
// kairos plan's agent actions (assign/fire from the portal) record here so
// the FEED bridge cards them for the owner and the team trail stays complete.
func (s *Store) LogAction(actor Identity, action string, payload map[string]any, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendLog(Entry{TS: now.UTC(), Actor: actor.Email, Action: action, Payload: payload})
}

// DeleteComment removes a comment from an item. Only its author or the portal
// admin may delete it; anyone else gets ErrNotYours, and a missing comment is
// ErrCommentNotFound. The removal is logged (ActDeleteComment) so the activity
// trail — and the FEED bridge — record who cleared what.
func (s *Store) DeleteComment(actor Identity, itemID, commentID string, isAdmin bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	list := ext.Comments[itemID]
	idx := -1
	for i, c := range list {
		if c.ID == commentID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrCommentNotFound
	}
	c := list[idx]
	if !isAdmin && !strings.EqualFold(strings.TrimSpace(c.Author), strings.TrimSpace(actor.Email)) {
		return ErrNotYours
	}
	// full-slice expression (list[:idx:idx]) caps the head so append allocates
	// rather than clobbering the shared backing array.
	ext.Comments[itemID] = append(list[:idx:idx], list[idx+1:]...)
	if len(ext.Comments[itemID]) == 0 {
		delete(ext.Comments, itemID)
	}
	return s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActDeleteComment,
		Payload: map[string]any{"item": itemID, "comment": commentID, "text": c.Text}})
}

// Patch merges validated field changes into an item's override (the caller has
// already enforced the assignee lock). status=done stamps done_on when absent.
func (s *Store) Patch(actor Identity, itemID string, fields map[string]string, now time.Time) (Override, error) {
	clean := map[string]string{}
	for k, v := range fields {
		k = strings.TrimSpace(strings.ToLower(k))
		if !PatchFields[k] {
			return Override{}, fmt.Errorf("field %q is not team-editable", k)
		}
		clean[k] = strings.TrimSpace(v)
	}
	if len(clean) == 0 {
		return Override{}, errors.New("no fields to change")
	}
	if v, ok := clean["status"]; ok {
		if !PatchStatuses[v] {
			return Override{}, fmt.Errorf("status %q not in open|in_progress|done", v)
		}
		if v == "done" && clean["done_on"] == "" {
			clean["done_on"] = now.Format("2006-01-02")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	ov := ext.Overrides[itemID]
	if ov.Fields == nil {
		ov.Fields = map[string]string{}
	}
	for k, v := range clean {
		if v == "" {
			delete(ov.Fields, k)
		} else {
			ov.Fields[k] = v
		}
	}
	ov.By, ov.At = actor.Email, now.UTC()
	ext.Overrides[itemID] = ov
	err := s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActPatch,
		Payload: map[string]any{"item": itemID, "fields": clean}})
	return ov, err
}

// AddItem creates a member's own team/ item (direct add — no approval needed).
func (s *Store) AddItem(actor Identity, owner, kind, title, rock, due string, now time.Time) (TeamItem, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return TeamItem{}, errors.New("empty title")
	}
	if kind != "decision" {
		kind = "task"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	taken := map[string]bool{}
	for _, it := range ext.Items {
		taken[it.ID] = true
	}
	it := TeamItem{
		ID: uniqueID("team/", title, taken), Kind: kind, Title: title,
		Owner: owner, Captured: now.Format("2006-01-02"),
		Rock: strings.TrimSpace(rock), Due: strings.TrimSpace(due),
		Status: "open", Team: true, AddedBy: actor.Email,
	}
	ext.Items = append(ext.Items, it)
	err := s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActAdd,
		Payload: map[string]any{"item": it.ID, "title": title, "owner": owner}})
	return it, err
}

// Propose files an item suggestion for another person (approvals-model mirror).
func (s *Store) Propose(actor Identity, target, targetOwner, kind, title, rock, due string, now time.Time) (Proposal, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Proposal{}, errors.New("empty title")
	}
	// NOTE (ooda-portal audit): this gate is the reason a portal policy has to
	// reach the STORE and not just the Auth — without it, a partner with no
	// address under the portal's domain cannot be proposed to at all. Kept
	// ABOVE s.mu.Lock() below: EmailOverrides() (which AllowExtra reads) takes
	// no lock, but keeping the gate lock-free is the invariant to preserve.
	if !s.policy.Allows(target) {
		if s.policy.AllowExtra == nil {
			return Proposal{}, fmt.Errorf("target must be an @%s account", s.policy.DomainName())
		}
		return Proposal{}, fmt.Errorf("target must be an @%s account or an invited partner address", s.policy.DomainName())
	}
	if kind != "decision" {
		kind = "task"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	taken := map[string]bool{}
	for _, p := range ext.Proposals {
		taken[p.ID] = true
	}
	p := Proposal{
		ID: uniqueID("prop/", title, taken), Kind: kind, Title: title,
		Rock: strings.TrimSpace(rock), Due: strings.TrimSpace(due),
		Target: strings.ToLower(strings.TrimSpace(target)), TargetOwner: targetOwner,
		ProposedBy: actor.Email, ProposedName: actor.Name,
		At: now.UTC(), Status: "pending",
	}
	ext.Proposals = append(ext.Proposals, p)
	err := s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActPropose,
		Payload: map[string]any{"proposal": p.ID, "title": title, "target": p.Target}})
	return p, err
}

// Decide resolves a pending proposal (the caller has already checked that the
// decider is the admin or the target). Approval mints the team item for the
// target.
func (s *Store) Decide(actor Identity, proposalID string, approve bool, now time.Time) (Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	idx := -1
	for i, p := range ext.Proposals {
		if p.ID == proposalID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Proposal{}, errors.New("proposal not found")
	}
	p := ext.Proposals[idx]
	if p.Status != "pending" {
		return Proposal{}, fmt.Errorf("proposal already %s", p.Status)
	}
	p.DecidedBy, p.DecidedAt = actor.Email, now.UTC()
	if approve {
		p.Status = "approved"
		taken := map[string]bool{}
		for _, it := range ext.Items {
			taken[it.ID] = true
		}
		it := TeamItem{
			ID: uniqueID("team/", p.Title, taken), Kind: p.Kind, Title: p.Title,
			Owner: p.TargetOwner, Captured: now.Format("2006-01-02"),
			Rock: p.Rock, Due: p.Due, Status: "open", Team: true, AddedBy: p.ProposedBy,
		}
		ext.Items = append(ext.Items, it)
		p.ItemID = it.ID
	} else {
		p.Status = "rejected"
	}
	ext.Proposals[idx] = p
	err := s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActDecide,
		Payload: map[string]any{"proposal": p.ID, "title": p.Title, "approved": approve, "target": p.Target}})
	return p, err
}

// Activity reads the JSONL trail newest-last, dropping entries older than
// since and any malformed line (the log is append-only; readers stay lenient).
func (s *Store) Activity(since time.Time) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.logPath())
	if err != nil {
		return nil
	}
	var out []Entry
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e Entry
		if json.Unmarshal([]byte(line), &e) != nil || e.TS.Before(since) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// uniqueID slugs a title under a prefix, suffixing -2, -3… on collision.
func uniqueID(prefix, title string, taken map[string]bool) string {
	slug := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		slug = "item"
	}
	id := prefix + slug
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s%s-%d", prefix, slug, n)
	}
	return id
}
