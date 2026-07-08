package server

import (
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"manifest/reading"
)

// READING — the book shelf over the extrinsic zone (reading-plan §3). Reads come
// from the reading service; the two writes (+ book, finish) go through the
// vaultwriter database-class allow-list and reindex the touched record.

func (s *Server) handleReadingList(w http.ResponseWriter, r *http.Request) {
	if s.reading == nil {
		writeJSON(w, map[string]any{"books": []any{}})
		return
	}
	books, err := s.reading.List()
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"books": books})
}

var bookIllegal = regexp.MustCompile(`[/\\:*?"<>|]`)

// handleReadingCreate is the "+ book" ghost row: title (lowercased slug),
// optional authors/year/status → a new extrinsic/<slug>.md record.
func (s *Server) handleReadingCreate(w http.ResponseWriter, r *http.Request) {
	if s.reading == nil || s.vault == nil || s.index == nil {
		http.Error(w, "reading not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
		Year    string   `json:"year"`
		Status  string   `json:"status"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Title) == "" {
		httpError(w, errBadRequest("title is required"))
		return
	}
	slug := strings.TrimSpace(bookIllegal.ReplaceAllString(strings.ToLower(b.Title), ""))
	if slug == "" {
		httpError(w, errBadRequest("title has no usable characters"))
		return
	}
	status := strings.ToLower(strings.TrimSpace(b.Status))
	if status != "read" {
		status = "reading" // + book defaults to a book you're starting
	}
	// resolve authors like the importer: existing note → its exact name, else lowercase
	var authorToks []string
	for _, a := range b.Authors {
		a = strings.Join(strings.Fields(a), " ")
		if a == "" {
			continue
		}
		link := strings.ToLower(a)
		if e, ok := s.index.Resolve(a); ok && e.HasNote {
			link = e.Display
		}
		authorToks = append(authorToks, `"[[`+link+`]]"`)
	}

	var fm strings.Builder
	fm.WriteString("---\ncategories: [books]\n")
	if len(authorToks) > 0 {
		fm.WriteString("authors: [" + strings.Join(authorToks, ", ") + "]\n")
	}
	fm.WriteString("status: " + status + "\n")
	if y := strings.TrimSpace(b.Year); y != "" {
		fm.WriteString("year-written: " + y + "\n")
	}
	fm.WriteString("---\n\n#book\n")

	rel := path.Join(s.extrinsicRoot(), slug+".md")
	if _, err := s.vault.CreateRecord(rel, fm.String()); err != nil {
		httpError(w, err)
		return
	}
	_ = s.index.ReindexPaths([]string{rel})
	s.respondBook(w, rel)
}

// handleReadingFinish marks a book read: status: read, date-read: today, and an
// optional rating (skippable). The body is preserved.
func (s *Server) handleReadingFinish(w http.ResponseWriter, r *http.Request) {
	if s.reading == nil || s.vault == nil || s.index == nil {
		http.Error(w, "reading not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Path   string `json:"path"`
		Rating int    `json:"rating"`
	}
	if err := decode(r, &b); err != nil || b.Path == "" {
		httpError(w, errBadRequest("path is required"))
		return
	}
	if err := s.vault.SetFrontmatterField(b.Path, "status", "read"); err != nil {
		httpError(w, err)
		return
	}
	_ = s.vault.SetFrontmatterField(b.Path, "date-read", time.Now().Format("2006-01-02"))
	if b.Rating > 0 {
		_ = s.vault.SetFrontmatterField(b.Path, "rating", itoa(b.Rating))
	}
	_ = s.index.ReindexPaths([]string{b.Path})
	s.respondBook(w, b.Path)
}

// handleReadingRating sets (or clears, when 0) a book's rating.
func (s *Server) handleReadingRating(w http.ResponseWriter, r *http.Request) {
	if s.reading == nil || s.vault == nil || s.index == nil {
		http.Error(w, "reading not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Path   string `json:"path"`
		Rating int    `json:"rating"`
	}
	if err := decode(r, &b); err != nil || b.Path == "" || b.Rating < 0 || b.Rating > 5 {
		httpError(w, errBadRequest("path and rating (0-5) are required"))
		return
	}
	if err := s.vault.SetFrontmatterField(b.Path, "rating", itoa(b.Rating)); err != nil {
		httpError(w, err)
		return
	}
	_ = s.index.ReindexPaths([]string{b.Path})
	s.respondBook(w, b.Path)
}

// respondBook re-reads the shelf and returns the single record at rel (so the
// client gets the freshly-parsed book after a write).
func (s *Server) respondBook(w http.ResponseWriter, rel string) {
	books, _ := s.reading.List()
	for _, bk := range books {
		if bk.Path == rel {
			writeJSON(w, bk)
			return
		}
	}
	writeJSON(w, reading.Book{Path: rel})
}

func (s *Server) extrinsicRoot() string {
	if s.extrinsicRootName != "" {
		return s.extrinsicRootName
	}
	return "extrinsic"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
