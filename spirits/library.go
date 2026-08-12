package spirits

// The artifact library (artifacts/library/) is where a run leaves the thing it
// MADE — the brief, the draft, the roadmap. Run reports narrate; library briefs
// are the deliverable. The dashboard resolves "show me the result" against this
// index: by the run that wrote it (frontmatter `run:`), or by the delegation
// token the brief carries. Read-only — the library is engine territory (§8).

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"manifest/mdfm"
)

// LibraryDoc is one brief: its harness-relative ref (what the read API opens)
// plus the frontmatter and body a resolver matches on.
type LibraryDoc struct {
	Ref   string `json:"ref"` // "artifacts/library/<name>.md"
	Title string `json:"title"`
	Run   string `json:"run"`  // the run id that wrote it
	Date  string `json:"date"` // RFC3339
	Body  string `json:"body"`
}

// Library lists the harness's briefs, newest first.
func (s *Store) Library() []LibraryDoc {
	dir := filepath.Join(s.root, "artifacts", "library")
	entries, _ := os.ReadDir(dir)
	out := []LibraryDoc{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		fm, body := mdfm.Split(string(b))
		out = append(out, LibraryDoc{
			Ref:   "artifacts/library/" + e.Name(),
			Title: fm["title"],
			Run:   fm["run"],
			Date:  fm["date"],
			Body:  strings.TrimSpace(body),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].Ref > out[j].Ref
	})
	return out
}
