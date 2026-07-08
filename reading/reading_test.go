package reading

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/vaultindex"
)

func harness(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a created record, a hand-authored merge (body + singular author:), a reading
	write("extrinsic/esoterika.md", "---\ncategories: [books]\nauthors: [\"[[mitch horowitz]]\"]\nstatus: read\nrating: 4\nyear-written: 2026\ndate-read: 2026-07-04\npages: 306\n---\n#book\n")
	write("extrinsic/thinking, fast and slow.md", "---\ncategories: [books]\nauthor: [[Daniel Kahneman]]\nstatus: read\ndate-read: 2019-11-07\n---\n#book\n\nWritten by [[Daniel Kahneman]]. My notes survive.\n")
	write("extrinsic/meaning in absurdity.md", "---\ncategories: [books]\nstatus: reading\n---\n#book\n")
	write("extrinsic/undated.md", "---\ncategories: [books]\nstatus: read\n---\n#book\n")
	// a NON-book extrinsic note must not appear on the shelf
	write("extrinsic/some article.md", "---\ncategories: [papers]\n---\n#paper\n")

	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	return New(ix)
}

func TestListParsesAndSorts(t *testing.T) {
	books, err := harness(t).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 4 {
		t.Fatalf("want 4 book records (papers excluded), got %d: %+v", len(books), books)
	}
	// default sort: reading first, then date-read desc, undated last
	if books[0].Name != "meaning in absurdity" || books[0].Status != "reading" {
		t.Fatalf("currently-reading must sort first, got %q", books[0].Name)
	}
	if books[len(books)-1].Name != "undated" {
		t.Fatalf("undated read book must sort last, got %q", books[len(books)-1].Name)
	}

	byName := map[string]Book{}
	for _, b := range books {
		byName[b.Name] = b
	}
	eso := byName["esoterika"]
	if eso.Rating != 4 || eso.YearWritten != "2026" || eso.DateRead != "2026-07-04" || eso.Pages != 306 {
		t.Fatalf("esoterika fields = %+v", eso)
	}
	if len(eso.Authors) != 1 || eso.Authors[0].Key != "mitch horowitz" {
		t.Fatalf("esoterika author = %+v", eso.Authors)
	}
	// the merged record: singular author: with a differently-cased link still
	// resolves to a lowercased key, and the title falls back to the note name
	tfs := byName["thinking, fast and slow"]
	if len(tfs.Authors) != 1 || tfs.Authors[0].Key != "daniel kahneman" || tfs.Authors[0].Display != "Daniel Kahneman" {
		t.Fatalf("tfs author (author: variant) = %+v", tfs.Authors)
	}
	if tfs.Rating != 0 { // unrated → 0, never a fake value
		t.Fatalf("unrated book must have rating 0, got %d", tfs.Rating)
	}
}
