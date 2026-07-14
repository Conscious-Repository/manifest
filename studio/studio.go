// Package studio reads and lightly writes the Content Studio's draft board — the
// excalibur artifacts/studio/drafts/*.md files the scribe/critic produce. The
// FILE FORMAT is the contract shared with the engine (internal/casts/studiocasts.go):
// frontmatter scalars + a lead text block + ## sources/scorecard/feedback/edited
// sections. The dashboard renders the board and captures the owner's feedback +
// edits (the same write-class as feed statuses — a mutex serializes writers).
package studio

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"manifest/mdfm"
)

// Draft is one draft card for the board.
type Draft struct {
	ID           string   `json:"id"`
	Text         string   `json:"text"`             // the scribe's draft (lead block)
	Edited       string   `json:"edited,omitempty"` // owner's edited text, if any
	Format       string   `json:"format"`
	Status       string   `json:"status"` // pending-audit|passed|killed|posted
	Score        string   `json:"score,omitempty"`
	Created      string   `json:"created"`
	QuotedURL    string   `json:"quotedUrl,omitempty"`
	PostedURL    string   `json:"postedUrl,omitempty"`
	Sources      string   `json:"sources,omitempty"`
	Scorecard    string   `json:"scorecard,omitempty"`
	Feedback     string   `json:"feedback,omitempty"`
	FeedbackTags []string `json:"feedbackTags,omitempty"`
}

// Effective is the text that would be posted: the edit if present, else the draft.
func (d Draft) Effective() string {
	if strings.TrimSpace(d.Edited) != "" {
		return d.Edited
	}
	return d.Text
}

// Store is the drafts directory (excalibur artifacts/studio/drafts).
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore roots the store at <excalibur>/artifacts/studio/drafts.
func NewStore(excaliburRoot string) *Store {
	return &Store{dir: filepath.Join(excaliburRoot, "artifacts", "studio", "drafts")}
}

// List returns all drafts, newest-first.
func (s *Store) List() []Draft {
	entries, _ := os.ReadDir(s.dir)
	var out []Draft
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if d, ok := s.parse(filepath.Join(s.dir, e.Name())); ok {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out
}

// Get loads one draft by id.
func (s *Store) Get(id string) (Draft, bool) {
	return s.parse(filepath.Join(s.dir, id+".md"))
}

// SetFeedback records the owner's free-text feedback + quick chips onto a draft.
func (s *Store) SetFeedback(id, text string, tags []string) (Draft, error) {
	return s.mutate(id, func(doc *draftDoc) {
		if strings.TrimSpace(text) != "" {
			doc.setSection("feedback", text)
		}
		if len(tags) > 0 {
			doc.fm["feedback-tags"] = "[" + strings.Join(tags, ", ") + "]"
		}
	})
}

// SetEdited records the owner's edited text (the version that lands on approve).
func (s *Store) SetEdited(id, text string) (Draft, error) {
	return s.mutate(id, func(doc *draftDoc) { doc.setSection("edited", text) })
}

// MarkPosted stamps a draft posted with the (optional) tweet URL.
func (s *Store) MarkPosted(id, url string) (Draft, error) {
	return s.mutate(id, func(doc *draftDoc) {
		doc.fm["status"] = "posted"
		if strings.TrimSpace(url) != "" {
			doc.fm["posted-url"] = strings.TrimSpace(url)
		}
	})
}

// ---- internals: the draft doc (mirrors the engine's studiocasts.draftDoc) ----

type draftDoc struct {
	fm       map[string]string
	lead     string
	secOrder []string
	sections map[string]string
}

func parseDraftDoc(content string) draftDoc {
	fm, body := mdfm.Split(content)
	d := draftDoc{fm: fm, sections: map[string]string{}}
	var lead []string
	cur := ""
	var buf []string
	flush := func() {
		if cur != "" {
			d.sections[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
			d.secOrder = append(d.secOrder, cur)
		}
		buf = nil
	}
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(ln, "## ") {
			flush()
			cur = strings.TrimSpace(strings.TrimPrefix(ln, "## "))
			continue
		}
		if cur == "" {
			lead = append(lead, ln)
		} else {
			buf = append(buf, ln)
		}
	}
	flush()
	d.lead = strings.TrimSpace(strings.Join(lead, "\n"))
	return d
}

func (d *draftDoc) setSection(name, content string) {
	if _, ok := d.sections[name]; !ok {
		d.secOrder = append(d.secOrder, name)
	}
	d.sections[name] = strings.TrimSpace(content)
}

func (d draftDoc) render() string {
	w := &mdfm.Writer{}
	for _, k := range []string{"type", "id", "format", "status", "score", "created", "quoted-url", "posted-url"} {
		if v := d.fm[k]; v != "" {
			w.SetRaw(k, v)
		}
	}
	if tags := d.fm["feedback-tags"]; tags != "" {
		w.SetRaw("feedback-tags", tags)
	}
	var b strings.Builder
	b.WriteString(d.lead)
	for _, name := range d.secOrder {
		b.WriteString("\n\n## " + name + "\n\n" + d.sections[name])
	}
	return w.String(b.String())
}

func (d draftDoc) toDraft() Draft {
	return Draft{
		ID: d.fm["id"], Text: d.lead, Edited: d.sections["edited"], Format: d.fm["format"],
		Status: d.fm["status"], Score: d.fm["score"], Created: d.fm["created"],
		QuotedURL: d.fm["quoted-url"], PostedURL: d.fm["posted-url"],
		Sources: d.sections["sources"], Scorecard: d.sections["scorecard"],
		Feedback: d.sections["feedback"], FeedbackTags: mdfm.List(d.fm["feedback-tags"]),
	}
}

func (s *Store) parse(path string) (Draft, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Draft{}, false
	}
	return parseDraftDoc(string(b)).toDraft(), true
}

func (s *Store) mutate(id string, fn func(*draftDoc)) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, id+".md")
	b, err := os.ReadFile(path)
	if err != nil {
		return Draft{}, err
	}
	doc := parseDraftDoc(string(b))
	fn(&doc)
	if err := os.WriteFile(path, []byte(doc.render()), 0o644); err != nil {
		return Draft{}, err
	}
	return doc.toDraft(), nil
}
