// Package reading is the READING surface over the vault's book records
// (reading-plan §3): the `categories: [books]` notes under the extrinsic zone,
// projected into a sortable/filterable shelf. Read-only here; the two writes
// (+ book, finish) go through the vaultwriter allow-list in the server layer.
package reading

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"manifest/mdfm"
	"manifest/vaultindex"
)

// Book is one shelf row — a projection of a `categories: [books]` record.
type Book struct {
	Path        string   `json:"path"`  // vault-relative, for the note view
	Title       string   `json:"title"` // full-title if present, else the note name
	Name        string   `json:"name"`  // the note basename (link/target key)
	Authors     []Author `json:"authors"`
	Status      string   `json:"status"`      // "read" | "reading" | "" (unknown)
	Rating      int      `json:"rating"`      // 0 = unrated
	YearWritten string   `json:"yearWritten"` // "" if unknown
	DateRead    string   `json:"dateRead"`    // ISO, "" if none
	Pages       int      `json:"pages"`       // 0 if unknown
}

// Author is a linked author: display text + the resolution key (lowercased).
type Author struct {
	Display string `json:"display"`
	Key     string `json:"key"`
}

// Service reads book records from the index + their frontmatter from disk.
type Service struct{ ix *vaultindex.Index }

func New(ix *vaultindex.Index) *Service { return &Service{ix: ix} }

var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// List returns every book record, default-sorted (currently-reading first, then
// date-read desc with undated last, then title). The frontend re-sorts/filters.
func (s *Service) List() ([]Book, error) {
	refs, err := s.ix.Category("books", vaultindex.SortNameAsc)
	if err != nil {
		return nil, err
	}
	out := make([]Book, 0, len(refs))
	for _, r := range refs {
		b, ok := s.parse(r.Path, r.Name)
		if ok {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return lessDefault(out[i], out[j]) })
	return out, nil
}

// parse reads one record's frontmatter into a Book. Tolerant: missing/garbled
// fields are simply omitted (hand edits in Obsidian read back the same way).
func (s *Service) parse(rel, name string) (Book, bool) {
	raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(rel)))
	if err != nil {
		return Book{}, false
	}
	fm, _ := mdfm.Split(string(raw))
	b := Book{Path: rel, Name: name, Title: name}
	if ft := yamlUnquote(fm["full-title"]); ft != "" {
		b.Title = ft
	}
	b.Status = strings.ToLower(strings.TrimSpace(fm["status"]))
	b.Rating = atoi(fm["rating"])
	b.YearWritten = strings.TrimSpace(fm["year-written"])
	b.DateRead = strings.TrimSpace(fm["date-read"])
	b.Pages = atoi(fm["pages"])
	// authors: `["[[a]]", "[[b]]"]` (or a hand-written `author:` variant) — pull
	// the wikilink targets so each resolves like any link.
	authorsRaw := fm["authors"]
	if authorsRaw == "" {
		authorsRaw = fm["author"]
	}
	for _, m := range wikilinkRe.FindAllStringSubmatch(authorsRaw, -1) {
		disp := strings.TrimSpace(m[1])
		if i := strings.Index(disp, "|"); i >= 0 { // [[target|display]]
			disp = strings.TrimSpace(disp[i+1:])
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		if i := strings.Index(key, "|"); i >= 0 {
			key = strings.TrimSpace(key[:i])
		}
		b.Authors = append(b.Authors, Author{Display: disp, Key: key})
	}
	return b, true
}

// lessDefault orders the shelf: currently-reading first, then most-recently-read
// (undated last), then title. Deterministic for a stable UI.
func lessDefault(a, b Book) bool {
	if ar, br := a.Status == "reading", b.Status == "reading"; ar != br {
		return ar // reading before read
	}
	if (a.DateRead == "") != (b.DateRead == "") {
		return a.DateRead != "" // dated before undated
	}
	if a.DateRead != b.DateRead {
		return a.DateRead > b.DateRead // newest first
	}
	return strings.ToLower(a.Title) < strings.ToLower(b.Title)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// yamlUnquote strips a YAML double/single-quoted scalar back to its text (the
// importer quotes full-title because subtitles carry a ": "). Tolerant: an
// unquoted value passes through unchanged.
func yamlUnquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
		v = strings.ReplaceAll(v, `\"`, `"`)
		return strings.ReplaceAll(v, `\\`, `\`)
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}
