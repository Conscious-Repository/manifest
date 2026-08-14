// Package capture is the tray (cmd-ctr's Stage, manifest idiom): quick notes,
// shared links, and images land as editable cards the owner triages — into a
// todo, a day task, a chat, or the bin. Cards live in dataDir (never the
// vault: nothing is knowledge until the owner promotes it), one JSON per card
// + a media dir. Privacy contract inherited from cmd-ctr: NOTHING here
// reaches an LLM until the owner explicitly hands one card to a chat.
package capture

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Item is one tray card.
type Item struct {
	ID        string  `json:"id"`
	CreatedAt string  `json:"createdAt"`
	Kind      string  `json:"kind"` // note | link | image
	Title     string  `json:"title,omitempty"`
	Text      string  `json:"text,omitempty"`
	URL       string  `json:"url,omitempty"`
	Media     []Media `json:"media,omitempty"`
	Source    string  `json:"source"` // share | paste | drop | manual | voice
	Status    string  `json:"status"` // open | kept
	// Trashed (soft-delete) items keep their card for 30 days (cmd-ctr
	// semantics), then purge with their media.
	TrashedAt string `json:"trashedAt,omitempty"`
}

// Media is one stored attachment.
type Media struct {
	File string `json:"file"` // filename under media/ (server-assigned)
	Name string `json:"name"` // original name, display only
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// Store is the dataDir-backed tray.
type Store struct {
	root string // <dataDir>/capture
}

const (
	maxMediaBytes = 25 << 20
	trashTTL      = 30 * 24 * time.Hour
)

var (
	idRe = regexp.MustCompile(`^[0-9a-z-]{8,64}$`)
	// media extension allowlist (images + pdf — the share_target accept list)
	extOK = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".heic": true, ".pdf": true}
)

func NewStore(dataDir string) *Store {
	s := &Store{root: filepath.Join(dataDir, "capture")}
	_ = os.MkdirAll(filepath.Join(s.root, "items"), 0o755)
	_ = os.MkdirAll(filepath.Join(s.root, "media"), 0o755)
	return s
}

func (s *Store) itemPath(id string) string { return filepath.Join(s.root, "items", id+".json") }

// MediaPath resolves a stored media filename (traversal-guarded).
func (s *Store) MediaPath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("bad media name")
	}
	return filepath.Join(s.root, "media", name), nil
}

// List returns cards newest-first (trashed excluded); PurgeTrash runs first so
// listing is also the sweeper — no background timer needed at this scale.
func (s *Store) List() []Item {
	s.purgeTrash()
	entries, _ := os.ReadDir(filepath.Join(s.root, "items"))
	var out []Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var it Item
		if b, err := os.ReadFile(filepath.Join(s.root, "items", e.Name())); err == nil && json.Unmarshal(b, &it) == nil {
			if it.TrashedAt == "" {
				out = append(out, it)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// OpenCount is the rail badge.
func (s *Store) OpenCount() int {
	n := 0
	for _, it := range s.List() {
		if it.Status == "open" {
			n++
		}
	}
	return n
}

func (s *Store) load(id string) (Item, error) {
	var it Item
	if !idRe.MatchString(id) {
		return it, fmt.Errorf("bad id")
	}
	b, err := os.ReadFile(s.itemPath(id))
	if err != nil {
		return it, fmt.Errorf("no such capture")
	}
	if err := json.Unmarshal(b, &it); err != nil {
		return it, err
	}
	return it, nil
}

func (s *Store) save(it Item) error {
	b, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.itemPath(it.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.itemPath(it.ID))
}

// Add creates a text/link card.
func (s *Store) Add(title, text, url, source string) (Item, error) {
	it := Item{
		ID:        newID(),
		CreatedAt: time.Now().Format(time.RFC3339),
		Title:     strings.TrimSpace(title),
		Text:      strings.TrimSpace(text),
		URL:       strings.TrimSpace(url),
		Source:    source,
		Status:    "open",
		Kind:      "note",
	}
	if it.URL != "" {
		it.Kind = "link"
	}
	if it.Title == "" && it.Text == "" && it.URL == "" {
		return it, fmt.Errorf("empty capture")
	}
	return it, s.save(it)
}

// AddFile attaches one uploaded file to a new or existing card. header size is
// enforced by the caller's MaxBytesReader; the extension allowlist here.
func (s *Store) AddFile(it *Item, fh *multipart.FileHeader) error {
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !extOK[ext] {
		return fmt.Errorf("file type %s not accepted (images + pdf only)", ext)
	}
	if fh.Size > maxMediaBytes {
		return fmt.Errorf("file too large (max 25MB)")
	}
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	name := fmt.Sprintf("%s-%d%s", it.ID, len(it.Media), ext)
	dst, err := os.Create(filepath.Join(s.root, "media", name))
	if err != nil {
		return err
	}
	defer dst.Close()
	n, err := dst.ReadFrom(src)
	if err != nil {
		return err
	}
	it.Media = append(it.Media, Media{File: name, Name: filepath.Base(fh.Filename), Mime: fh.Header.Get("Content-Type"), Size: n})
	if it.Kind == "note" && ext != ".pdf" {
		it.Kind = "image"
	}
	return nil
}

// NewForFiles creates the card an upload/share attaches into.
func (s *Store) NewForFiles(title, text, url, source string) Item {
	it := Item{
		ID:        newID(),
		CreatedAt: time.Now().Format(time.RFC3339),
		Title:     strings.TrimSpace(title),
		Text:      strings.TrimSpace(text),
		URL:       strings.TrimSpace(url),
		Source:    source,
		Status:    "open",
		Kind:      "note",
	}
	if it.URL != "" {
		it.Kind = "link"
	}
	return it
}

// Save persists a card built by NewForFiles/AddFile.
func (s *Store) Save(it Item) error { return s.save(it) }

// Update edits a card's text/title in place.
func (s *Store) Update(id, title, text string) error {
	it, err := s.load(id)
	if err != nil {
		return err
	}
	it.Title, it.Text = strings.TrimSpace(title), text
	return s.save(it)
}

// SetStatus flips open ↔ kept.
func (s *Store) SetStatus(id, status string) error {
	if status != "open" && status != "kept" {
		return fmt.Errorf("status must be open or kept")
	}
	it, err := s.load(id)
	if err != nil {
		return err
	}
	it.Status = status
	return s.save(it)
}

// Trash soft-deletes (30-day purge).
func (s *Store) Trash(id string) error {
	it, err := s.load(id)
	if err != nil {
		return err
	}
	it.TrashedAt = time.Now().Format(time.RFC3339)
	return s.save(it)
}

// purgeTrash removes trashed cards past TTL, with their media.
func (s *Store) purgeTrash() {
	entries, _ := os.ReadDir(filepath.Join(s.root, "items"))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var it Item
		b, err := os.ReadFile(filepath.Join(s.root, "items", e.Name()))
		if err != nil || json.Unmarshal(b, &it) != nil || it.TrashedAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, it.TrashedAt)
		if err != nil || time.Since(at) < trashTTL {
			continue
		}
		for _, m := range it.Media {
			if p, err := s.MediaPath(m.File); err == nil {
				_ = os.Remove(p)
			}
		}
		_ = os.Remove(filepath.Join(s.root, "items", e.Name()))
	}
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
