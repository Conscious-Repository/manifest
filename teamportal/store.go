package teamportal

import (
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
	dir string
	mu  sync.Mutex
}

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

func (s *Store) Dir() string       { return s.dir }
func (s *Store) extPath() string   { return filepath.Join(s.dir, "items.ext.json") }
func (s *Store) logPath() string   { return filepath.Join(s.dir, "activity.log") }
func (s *Store) emailsPath() string { return filepath.Join(s.dir, "emails.json") }

// Comment is one team comment on an item (any signed-in member may comment).
type Comment struct {
	ID         string    `json:"id"`
	Item       string    `json:"item"`
	Author     string    `json:"author"`      // email
	AuthorName string    `json:"author_name"` // display name from Google
	Text       string    `json:"text"`
	At         time.Time `json:"at"`
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
	Team     bool   `json:"team"`               // the distinguishing mark
	AddedBy  string `json:"added_by,omitempty"` // email of the creator
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
	Comments  map[string][]Comment `json:"comments"`  // item id → comments (oldest first)
	Overrides map[string]Override  `json:"overrides"` // item id → latest team field state
	Items     []TeamItem           `json:"items"`     // team/-tagged member adds
	Proposals []Proposal           `json:"proposals"`
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

// AddComment appends a comment (any signed-in member; the caller has already
// verified the item exists).
func (s *Store) AddComment(actor Identity, itemID, text string, now time.Time) (Comment, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Comment{}, errors.New("empty comment")
	}
	if len(text) > 4000 {
		return Comment{}, errors.New("comment too long (4000 max)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ext := s.readExt()
	c := Comment{
		ID:     fmt.Sprintf("c-%d", now.UnixNano()),
		Item:   itemID, Author: actor.Email, AuthorName: actor.Name,
		Text: text, At: now.UTC(),
	}
	ext.Comments[itemID] = append(ext.Comments[itemID], c)
	err := s.writeExt(ext, Entry{TS: now.UTC(), Actor: actor.Email, Action: ActComment,
		Payload: map[string]any{"item": itemID, "text": text}})
	return c, err
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
	if !Authorized(target) {
		return Proposal{}, fmt.Errorf("target must be an @%s account", Domain)
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
