package consume

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"manifest/mdfm"
	"manifest/record"
)

// CURATE — the button, and the only thing in this package that writes the
// owner's vault.
//
// One curated item is one note under extrinsic/, beside the books, carrying
// `categories: [articles]`. That placement is the whole design: what the owner
// picked out and what he said about it are HIS, they belong in the medium he
// keeps his thinking in, and they are versioned by the same git history as
// everything else there. The public feed is then a projection of those notes
// and of nothing else — which is what makes the isolation argument in
// public.go a structural claim rather than a filtering promise.
//
// Two rules protect the vault side:
//
//   - Re-curating an existing note NEVER clobbers it. The owner may have
//     written underneath the mirrored article; a second click updates the
//     metadata lines and leaves the body alone.
//   - Un-curating never deletes. It clears one frontmatter field, the feed
//     stops selecting the note, and the archive survives.

const (
	articlesCategory = "articles"
	curatedField     = "curated"
)

// CuratedEntry is one note, projected. It is the entire vocabulary the public
// feed has — see public.go.
type CuratedEntry struct {
	ItemID    string `json:"itemId"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	Note      string `json:"note"`
	Published string `json:"published"`
	Curated   string `json:"curated"`
	Mirror    string `json:"mirror"`
	Body      string `json:"-"` // markdown, the mirrored article
	// HTML is the sanitized body, resolved from the dataDir snapshot at serve
	// time. It is deliberately NOT stored in the note: the note is the
	// archive, in markdown, for a person to read in Obsidian.
	HTML string `json:"-"`
}

// curateKey normalizes a URL for identity. Two feeds can deliver the same
// essay with different tracking parameters; the owner curated the writing, not
// the query string.
func curateKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "ref" || lk == "s" || lk == "r" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Host = strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	u.Scheme = "https"
	return strings.TrimRight(u.String(), "/")
}

// Curate writes (or refreshes) the note for one item.
func (s *Service) Curate(itemID, note string) (CuratedEntry, error) {
	if s.writeVault == nil {
		return CuratedEntry{}, errors.New("consume: no write capability for curation")
	}
	it, sub, ok := s.Get(itemID)
	if !ok {
		return CuratedEntry{}, fmt.Errorf("no item %q", itemID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	note = collapseSpaces(strings.ReplaceAll(strings.TrimSpace(note), "\n", " "))
	now := s.now()
	rel := s.notePath(it, sub)

	var content string
	if prior, err := s.readVault(rel); err == nil && len(prior) > 0 {
		// The note already exists — refresh its metadata, keep the owner's
		// body and any fields he added.
		content = string(prior)
		content = setFrontmatter(content, curatedField, now.Format("2006-01-02"))
		if note != "" {
			content = setFrontmatter(content, "note", yamlScalar(note))
		}
	} else {
		content = s.buildNote(it, sub, note, now)
	}

	if err := s.writeVault(rel, []byte(content)); err != nil {
		return CuratedEntry{}, err
	}
	s.invalidateCurated()
	entry, _ := parseCurated(rel, content)
	return entry, nil
}

// Uncurate clears the curated marker. The note stays — it is the owner's
// archive of something he read, and a second click is not a delete.
func (s *Service) Uncurate(itemID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.curatedEntries() {
		if e.ItemID != itemID {
			continue
		}
		raw, err := s.readVault(e.Path)
		if err != nil {
			return err
		}
		if err := s.writeVault(e.Path, []byte(setFrontmatter(string(raw), curatedField, ""))); err != nil {
			return err
		}
		s.invalidateCurated()
		return nil
	}
	return fmt.Errorf("%q is not curated", itemID)
}

// notePath is where one item's note lives. Titles collide (every newsletter
// has a "Weekly Roundup"), so a colliding slug gets the source appended rather
// than two different essays sharing one note.
func (s *Service) notePath(it Item, sub Subscription) string {
	base := slugFor(it.Title)
	if base == "" {
		base = slugFor(sub.Title + " item")
	}
	rel := "extrinsic/" + base + ".md"
	if prior, err := s.readVault(rel); err == nil {
		if e, ok := parseCurated(rel, string(prior)); ok && e.ItemID != it.ID {
			rel = "extrinsic/" + base + "-" + slugFor(firstNonEmpty(sub.Title, sub.ID)) + ".md"
		}
	}
	return rel
}

// buildNote renders a fresh curated note.
func (s *Service) buildNote(it Item, sub Subscription, note string, now time.Time) string {
	w := (&mdfm.Writer{}).SetList("categories", []string{articlesCategory})
	w.Set("source", yamlScalar(firstNonEmpty(it.Source, sub.Title)))
	if it.Author != "" {
		w.Set("author", yamlScalar(it.Author))
	}
	w.Set("url", it.URL)
	if !it.PublishedAt.IsZero() {
		w.Set("published", it.PublishedAt.Format("2006-01-02"))
	}
	w.Set(curatedField, now.Format("2006-01-02"))
	w.Set("item", it.ID)
	w.Set("mirror", mirrorOf(sub))
	if note != "" {
		w.Set("note", yamlScalar(note))
	}

	var b strings.Builder
	b.WriteString("#article\n\n")
	b.WriteString("# " + strings.TrimSpace(it.Title) + "\n\n")
	if body := ToMarkdown(it.Body); body != "" {
		b.WriteString(body + "\n")
	} else if it.Excerpt != "" {
		b.WriteString(it.Excerpt + "\n\n")
	}
	if it.URL != "" {
		b.WriteString("\n---\n\nSource: [" + firstNonEmpty(it.Source, sub.Title, it.URL) + "](" + it.URL + ")\n")
	}
	return w.String(strings.TrimRight(b.String(), "\n"))
}

func mirrorOf(sub Subscription) string {
	if sub.Mirrors() {
		return MirrorFull
	}
	return MirrorExcerpt
}

// yamlScalar quotes a value that would otherwise break the block. Titles carry
// colons constantly ("The World Behind the World: Consciousness…") and an
// unquoted one silently truncates the note's frontmatter.
func yamlScalar(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.ContainsAny(v, ":#[]{}\",'&*?|<>=!%@`") || strings.HasPrefix(v, "-") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

// setFrontmatter rewrites one key in place, appends it before the closing
// fence when absent, or removes its line when value is empty. Everything else
// in the file — other keys, the body, the owner's own additions — is
// untouched, which is the fixpoint discipline applied to frontmatter.
func setFrontmatter(content, key, value string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		if value == "" {
			return content
		}
		return "---\n" + key + ": " + value + "\n---\n" + content
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return content
	}
	for i := 1; i < end; i++ {
		k, _, ok := strings.Cut(lines[i], ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), key) {
			continue
		}
		if value == "" {
			return strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n")
		}
		lines[i] = strings.TrimSpace(k) + ": " + value
		return strings.Join(lines, "\n")
	}
	if value == "" {
		return strings.Join(lines, "\n")
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, key+": "+value)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// ---- reading the curated set back ----

type curatedCache struct {
	mu      sync.Mutex
	entries []CuratedEntry
	at      time.Time
}

// curatedTTL bounds how stale the projection can be. Curating through the app
// invalidates immediately; the TTL only covers edits made directly in Obsidian.
const curatedTTL = 30 * time.Second

func (s *Service) invalidateCurated() {
	s.curated.mu.Lock()
	s.curated.at = time.Time{}
	s.curated.mu.Unlock()
}

// curatedEntries scans extrinsic/ for curated article notes, newest first.
func (s *Service) curatedEntries() []CuratedEntry {
	s.curated.mu.Lock()
	defer s.curated.mu.Unlock()
	if !s.curated.at.IsZero() && time.Since(s.curated.at) < curatedTTL {
		return s.curated.entries
	}
	out := []CuratedEntry{}
	if s.listVault != nil && s.readVault != nil {
		paths, err := s.listVault("extrinsic")
		if err == nil {
			for _, rel := range paths {
				raw, err := s.readVault(rel)
				if err != nil {
					continue
				}
				if e, ok := parseCurated(rel, string(raw)); ok {
					out = append(out, e)
				}
			}
		}
	}
	sortCurated(out)
	s.curated.entries = out
	s.curated.at = time.Now()
	return out
}

// parseCurated projects one note, and is THE selection rule: a note qualifies
// only if it declares categories: [articles] AND carries a curated date.
// Reading something is not publishing it.
func parseCurated(rel, content string) (CuratedEntry, bool) {
	fm, body := mdfm.Split(content)
	if fm == nil {
		return CuratedEntry{}, false
	}
	if !hasCategory(fm["categories"], articlesCategory) {
		return CuratedEntry{}, false
	}
	curated := record.Unquote(strings.TrimSpace(fm[curatedField]))
	if curated == "" {
		return CuratedEntry{}, false
	}
	title := record.Unquote(strings.TrimSpace(fm["title"]))
	if title == "" {
		title = headingOf(body)
	}
	if title == "" {
		title = strings.TrimSuffix(pathBase(rel), ".md")
	}
	return CuratedEntry{
		ItemID:    strings.TrimSpace(fm["item"]),
		Path:      rel,
		Title:     title,
		Source:    record.Unquote(strings.TrimSpace(fm["source"])),
		Author:    record.Unquote(strings.TrimSpace(fm["author"])),
		URL:       strings.TrimSpace(fm["url"]),
		Note:      record.Unquote(strings.TrimSpace(fm["note"])),
		Published: record.Unquote(strings.TrimSpace(fm["published"])),
		Curated:   curated,
		Mirror:    strings.TrimSpace(fm["mirror"]),
		Body:      stripLeadingHeading(body),
	}, true
}

func hasCategory(raw, want string) bool {
	for _, c := range mdfm.List(raw) {
		if strings.EqualFold(strings.TrimSpace(c), want) {
			return true
		}
	}
	return false
}

// headingOf returns the note's first `# ` heading.
func headingOf(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return ""
}

// stripLeadingHeading drops the tag line and the title heading, which the feed
// renders from the entry's own fields.
func stripLeadingHeading(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for len(lines) > 0 {
		t := strings.TrimSpace(lines[0])
		if t == "" || strings.HasPrefix(t, "#article") || strings.HasPrefix(t, "# ") {
			lines = lines[1:]
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func pathBase(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

func sortCurated(in []CuratedEntry) {
	// Newest curation first: the feed is "what I am paying attention to now".
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j].Curated > in[j-1].Curated; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}

// Curated returns the curated notes, newest first.
func (s *Service) Curated() []CuratedEntry { return s.curatedEntries() }

// curatedURLs is the lane's "already curated" lookup.
func (s *Service) curatedURLs() map[string]bool {
	out := map[string]bool{}
	for _, e := range s.curatedEntries() {
		if e.URL != "" {
			out[curateKey(e.URL)] = true
		}
	}
	return out
}

// SameLink reports whether two URLs point at the same piece of writing,
// ignoring tracking parameters and trivial spelling differences. Exported for
// the server layer, which matches a polled item against the curated set.
func SameLink(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	return curateKey(a) == curateKey(b)
}
