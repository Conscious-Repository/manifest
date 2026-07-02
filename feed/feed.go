// Package feed is the local research-feed store: one markdown file per item under
// <dataDir>/agents/feed/, deduped by a stable id. Agents (domain-scout, options-scout)
// generate items on Hermes; the dashboard MATERIALIZES their structured output here —
// the vault is never touched. keep/discard/snooze and Save-to-vault write status back.
package feed

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/mdfm"
)

// Item is one feed card. type ∈ paper|person|company|finding|artifact.
// status ∈ new|kept|discarded|snoozed.
type Item struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Why         string   `json:"why"`
	Link        string   `json:"link"`
	Source      string   `json:"source"`
	Agent       string   `json:"agent"`
	Profile     string   `json:"profile"`
	Domain      string   `json:"domain"`
	Date        string   `json:"date"` // RFC3339
	Status      string   `json:"status"`
	Confidence  string   `json:"confidence"`
	VaultNote   string   `json:"vaultNote"`
	SnoozeUntil string   `json:"snoozeUntil"`
	Tags        []string `json:"tags"`
	Body        string   `json:"body"`
}

// Filter selects items for List.
type Filter struct {
	Status string // "" = all non-discarded; else exact match
	Type   string
	Domain string
}

// Store is the feed directory.
type Store struct{ dir string }

// NewStore roots the store at <agentsDir>/feed and ensures it exists.
func NewStore(agentsDir string) *Store {
	dir := filepath.Join(agentsDir, "feed")
	_ = os.MkdirAll(dir, 0o700)
	return &Store{dir: dir}
}

// NewStoreDir roots the store at an explicit directory (used by the spirits
// package to read the excalibur artifacts/feed surface with identical
// semantics — the on-disk format is the contract between the two trees).
func NewStoreDir(dir string) *Store {
	_ = os.MkdirAll(dir, 0o755)
	return &Store{dir: dir}
}

// List returns items newest-first, applying the filter. By default (empty status)
// discarded items and still-snoozed items are hidden.
func (s *Store) List(f Filter, now time.Time) []Item {
	entries, _ := os.ReadDir(s.dir)
	var out []Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		it, err := s.parse(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		if f.Status != "" {
			if it.Status != f.Status {
				continue
			}
		} else {
			if it.Status == "discarded" {
				continue
			}
			if it.Status == "snoozed" && stillSnoozed(it, now) {
				continue
			}
		}
		if f.Type != "" && it.Type != f.Type {
			continue
		}
		if f.Domain != "" && it.Domain != f.Domain {
			continue
		}
		out = append(out, it)
	}
	// digest items pin to the top while new (the EA waiting-on digest), then
	// everything else newest-first.
	sort.Slice(out, func(i, j int) bool {
		pi := out[i].Type == "digest" && out[i].Status == "new"
		pj := out[j].Type == "digest" && out[j].Status == "new"
		if pi != pj {
			return pi
		}
		return out[i].Date > out[j].Date
	})
	return out
}

func stillSnoozed(it Item, now time.Time) bool {
	if it.SnoozeUntil == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, it.SnoozeUntil)
	if err != nil {
		return false
	}
	return now.Before(t)
}

// Get loads one item by id.
func (s *Store) Get(id string) (Item, bool) {
	it, err := s.parse(s.path(id))
	if err != nil {
		return Item{}, false
	}
	return it, true
}

// Upsert writes the item if new; if an item with the same id already exists it is
// left untouched (dedupe) and returned with created=false.
func (s *Store) Upsert(it Item) (Item, bool, error) {
	if existing, ok := s.Get(it.ID); ok {
		return existing, false, nil
	}
	if err := s.write(it); err != nil {
		return Item{}, false, err
	}
	return it, true, nil
}

// SetStatus updates an item's lifecycle status (kept|discarded|snoozed|new).
func (s *Store) SetStatus(id, status string) (Item, error) {
	return s.mutate(id, func(it *Item) { it.Status = status; it.SnoozeUntil = "" })
}

// Snooze marks an item snoozed until the given time.
func (s *Store) Snooze(id string, until time.Time) (Item, error) {
	return s.mutate(id, func(it *Item) {
		it.Status = "snoozed"
		it.SnoozeUntil = until.UTC().Format(time.RFC3339)
	})
}

// SetVaultNote records the vault path an item was saved to (and marks it kept).
func (s *Store) SetVaultNote(id, path string) (Item, error) {
	return s.mutate(id, func(it *Item) {
		it.VaultNote = path
		if it.Status == "new" {
			it.Status = "kept"
		}
	})
}

// Materialize parses an agent response's structured output (the last fenced JSON
// array) into feed items, writing only ones not already present. Returns the newly
// created items. A response with no JSON block is not an error (agents may say
// "nothing new").
func (s *Store) Materialize(raw, agent, profile string, now time.Time) ([]Item, error) {
	arr, ok := mdfm.ExtractJSONArray(raw)
	if !ok {
		return nil, nil
	}
	var raws []struct {
		Type       string   `json:"type"`
		Title      string   `json:"title"`
		Why        string   `json:"why"`
		Link       string   `json:"link"`
		Source     string   `json:"source"`
		Domain     string   `json:"domain"`
		Confidence string   `json:"confidence"`
		Body       string   `json:"body"`
		Tags       []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(arr), &raws); err != nil {
		return nil, err
	}
	var created []Item
	for _, r := range raws {
		if strings.TrimSpace(r.Title) == "" {
			continue
		}
		it := Item{
			Type: orDefault(r.Type, "finding"), Title: strings.TrimSpace(r.Title),
			Why: r.Why, Link: r.Link, Source: r.Source, Domain: r.Domain,
			Confidence: r.Confidence, Body: r.Body, Tags: r.Tags,
			Agent: agent, Profile: profile,
			Date: now.UTC().Format(time.RFC3339), Status: "new",
		}
		it.ID = itemID(it)
		saved, isNew, err := s.Upsert(it)
		if err != nil {
			return created, err
		}
		if isNew {
			created = append(created, saved)
		}
	}
	return created, nil
}

// ---- internals ----

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".md") }

func (s *Store) mutate(id string, fn func(*Item)) (Item, error) {
	it, ok := s.Get(id)
	if !ok {
		return Item{}, os.ErrNotExist
	}
	fn(&it)
	if err := s.write(it); err != nil {
		return Item{}, err
	}
	return it, nil
}

func (s *Store) write(it Item) error {
	w := (&mdfm.Writer{}).
		SetRaw("type", it.Type).
		Set("id", it.ID).
		Set("title", it.Title).
		Set("why", it.Why).
		Set("link", it.Link).
		Set("source", it.Source).
		Set("agent", it.Agent).
		Set("profile", it.Profile).
		Set("domain", it.Domain).
		Set("date", it.Date).
		Set("status", it.Status).
		Set("confidence", it.Confidence).
		Set("vault_note", it.VaultNote).
		Set("snooze_until", it.SnoozeUntil).
		SetList("tags", it.Tags)
	return os.WriteFile(s.path(it.ID), []byte(w.String(it.Body)), 0o644)
}

func (s *Store) parse(path string) (Item, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Item{}, err
	}
	fm, body := mdfm.Split(string(b))
	return Item{
		ID:          orDefault(fm["id"], strings.TrimSuffix(filepath.Base(path), ".md")),
		Type:        fm["type"],
		Title:       fm["title"],
		Why:         fm["why"],
		Link:        fm["link"],
		Source:      fm["source"],
		Agent:       fm["agent"],
		Profile:     fm["profile"],
		Domain:      fm["domain"],
		Date:        fm["date"],
		Status:      orDefault(fm["status"], "new"),
		Confidence:  fm["confidence"],
		VaultNote:   fm["vault_note"],
		SnoozeUntil: fm["snooze_until"],
		Tags:        mdfm.List(fm["tags"]),
		Body:        strings.TrimSpace(body),
	}, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// itemID is stable across runs (so re-materializing dedupes): a title slug plus a
// short hash of the canonical key (link, else title|source).
func itemID(it Item) string {
	key := strings.ToLower(strings.TrimSpace(it.Link))
	if key == "" {
		key = strings.ToLower(it.Title + "|" + it.Source)
	}
	h := sha1.Sum([]byte(key))
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(it.Title), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return slug + "-" + hex.EncodeToString(h[:])[:8]
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
