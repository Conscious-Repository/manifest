package consume

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeVault is a real directory standing in for the vault, wired through the
// same injected VaultIO the server binds to a write-capability. Using disk
// rather than a map keeps the path handling honest.
type fakeVault struct {
	root   string
	writes int
}

func newVault(t *testing.T) *fakeVault {
	t.Helper()
	return &fakeVault{root: t.TempDir()}
}

func (v *fakeVault) io() VaultIO {
	return VaultIO{
		Read: func(rel string) ([]byte, error) { return os.ReadFile(filepath.Join(v.root, rel)) },
		Write: func(rel string, data []byte) error {
			v.writes++
			p := filepath.Join(v.root, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			return os.WriteFile(p, data, 0o644)
		},
		List: func(dir string) ([]string, error) {
			var out []string
			entries, err := os.ReadDir(filepath.Join(v.root, dir))
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					out = append(out, dir+"/"+e.Name())
				}
			}
			return out, nil
		},
	}
}

func (v *fakeVault) read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// svcWithItem builds a service holding one polled item ready to curate.
func svcWithItem(t *testing.T, v *fakeVault) (*Service, Item) {
	t.Helper()
	s := New(t.TempDir(), v.io(), Config{})
	sub := Subscription{ID: "melissa", Kind: KindRSS, Title: "Melissa's Newsletter", URL: "https://m.example/feed", Mirror: MirrorFull, List: "essays"}
	d := ParseFeeds("")
	d.Add(sub)
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	sub, _ = d.Find(d.Subs()[0].ID)

	it := Item{
		ID: itemID(KindRSS, sub.ID, "guid-1"), SubID: sub.ID,
		Source: "Melissa's Newsletter", Author: "Melissa",
		Title:   "The Dictatorship of the Articulate",
		URL:     "https://m.example/p/dictatorship?utm_source=feed",
		Body:    "<p>A <strong>strong</strong> claim.</p><blockquote>Quoted.</blockquote>",
		Excerpt: "A strong claim.", Chars: 200,
		PublishedAt: time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC),
		FetchedAt:   time.Now().UTC(),
	}
	s.store.Commit(sub.ID, time.Now().UTC(), true, []Item{it}, nil, "")
	return s, it
}

func TestCurateWritesAnExtrinsicNote(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)

	entry, err := s.Curate(it.ID, "the middle third is the whole argument")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(entry.Path, "extrinsic/") {
		t.Fatalf("note written outside extrinsic/: %s", entry.Path)
	}
	note := v.read(t, entry.Path)

	for _, want := range []string{
		"categories: [articles]",
		"url: https://m.example/p/dictatorship",
		"curated: ",
		"item: " + it.ID,
		"note: ",
		"the middle third is the whole argument",
		"# The Dictatorship of the Articulate",
		"**strong**", // the body arrived as markdown, not HTML
		"> Quoted.",  // blockquote converted
		"Source: [Melissa's Newsletter]",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "<p>") || strings.Contains(note, "<strong>") {
		t.Errorf("raw HTML leaked into the vault note:\n%s", note)
	}
	// It is now curated, and the lane knows.
	if !s.curatedURLs()[curateKey(it.URL)] {
		t.Error("curated lookup did not see the new note")
	}
	if got := s.Curated(); len(got) != 1 || got[0].Title != it.Title {
		t.Errorf("curated projection: %+v", got)
	}
}

// ⚠ The owner may write underneath the mirrored article. A second click must
// never take that with it.
func TestRecurateDoesNotClobberTheOwnersEdits(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	entry, err := s.Curate(it.ID, "first take")
	if err != nil {
		t.Fatal(err)
	}

	// The owner adds his own thinking, in Obsidian.
	original := v.read(t, entry.Path)
	edited := original + "\n\n## my response\n\nI think this is wrong because…\n"
	if err := v.io().Write(entry.Path, []byte(edited)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Curate(it.ID, "second take"); err != nil {
		t.Fatal(err)
	}
	after := v.read(t, entry.Path)
	if !strings.Contains(after, "I think this is wrong because") {
		t.Fatalf("re-curating destroyed the owner's writing:\n%s", after)
	}
	if !strings.Contains(after, "second take") {
		t.Errorf("the new note did not land:\n%s", after)
	}
}

// Un-curate clears one field. The note — the owner's archive — survives.
func TestUncurateKeepsTheNote(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	entry, err := s.Curate(it.ID, "a note")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Uncurate(it.ID); err != nil {
		t.Fatal(err)
	}
	after := v.read(t, entry.Path)
	if strings.Contains(after, "\ncurated:") {
		t.Errorf("curated field not cleared:\n%s", after)
	}
	for _, want := range []string{"categories: [articles]", "# The Dictatorship", "a note"} {
		if !strings.Contains(after, want) {
			t.Errorf("un-curating destroyed %q — the note is the owner's archive:\n%s", want, after)
		}
	}
	if len(s.Curated()) != 0 {
		t.Error("un-curated note still selected by the feed")
	}
	// And it can be curated again.
	if _, err := s.Curate(it.ID, "back on"); err != nil {
		t.Fatal(err)
	}
	if len(s.Curated()) != 1 {
		t.Error("re-curating after un-curating did not take")
	}
}

// THE selection rule. Reading something is not publishing it.
func TestOnlyCuratedArticleNotesAreSelected(t *testing.T) {
	v := newVault(t)
	s, _ := svcWithItem(t, v)
	write := func(name, body string) {
		if err := v.io().Write("extrinsic/"+name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write("a-book.md", "---\ncategories: [books]\nstatus: read\n---\n#book\n")
	write("private-thought.md", "---\ncategories: [research]\n---\nsomething private\n")
	write("read-not-curated.md", "---\ncategories: [articles]\nurl: https://e.com/1\n---\n# Read but not curated\n")
	write("curated.md", "---\ncategories: [articles]\nurl: https://e.com/2\ncurated: 2026-08-24\n---\n# Actually curated\n")
	write("no-frontmatter.md", "just a note\n")

	got := s.Curated()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 curated entry, got %d: %+v", len(got), got)
	}
	if got[0].Title != "Actually curated" {
		t.Errorf("selected the wrong note: %+v", got[0])
	}
}

func TestCuratedAreNewestFirst(t *testing.T) {
	v := newVault(t)
	s, _ := svcWithItem(t, v)
	for name, date := range map[string]string{"old.md": "2026-01-01", "new.md": "2026-08-24", "mid.md": "2026-05-05"} {
		body := "---\ncategories: [articles]\nurl: https://e.com/" + name + "\ncurated: " + date + "\n---\n# " + name + "\n"
		if err := v.io().Write("extrinsic/"+name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	got := s.Curated()
	if len(got) != 3 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Curated != "2026-08-24" || got[2].Curated != "2026-01-01" {
		t.Errorf("not newest-first: %v %v %v", got[0].Curated, got[1].Curated, got[2].Curated)
	}
}

// Titles collide constantly across newsletters; two different essays must not
// end up sharing one note.
func TestCollidingTitlesGetDistinctNotes(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	first, err := s.Curate(it.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	other := it
	other.ID = itemID(KindRSS, "melissa", "guid-2")
	other.URL = "https://other.example/p/same-title"
	s.store.Commit("melissa", time.Now().UTC(), true, []Item{other}, nil, "")
	second, err := s.Curate(other.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatalf("two different essays share one note: %s", first.Path)
	}
	if len(s.Curated()) != 2 {
		t.Errorf("want 2 curated notes, got %d", len(s.Curated()))
	}
}

// A colon in a title breaks an unquoted YAML block and silently truncates the
// note's frontmatter. Real titles are full of them.
func TestTitlesWithColonsAndQuotesSurvive(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	nasty := `The World: "Consciousness", Free Will & the Limits`
	updated := it
	updated.Title = nasty
	updated.Source = `Someone's: Newsletter`
	s.store.Commit(it.SubID, time.Now().UTC(), true, []Item{updated}, nil, "")

	entry, err := s.Curate(it.ID, `he said "no" — twice`)
	if err != nil {
		t.Fatal(err)
	}
	reparsed, ok := parseCurated(entry.Path, v.read(t, entry.Path))
	if !ok {
		t.Fatalf("note with a colon in its title no longer parses:\n%s", v.read(t, entry.Path))
	}
	if reparsed.URL != it.URL {
		t.Errorf("frontmatter truncated at the colon: %+v", reparsed)
	}
	if !strings.Contains(reparsed.Note, `he said`) {
		t.Errorf("note lost: %q", reparsed.Note)
	}
}

// Tracking parameters must not make the same essay look uncurated.
func TestCurateKeyIgnoresTrackingParams(t *testing.T) {
	same := []string{
		"https://e.com/post",
		"https://e.com/post/",
		"https://www.e.com/post?utm_source=feed&utm_campaign=x",
		"http://e.com/post#section",
	}
	want := curateKey(same[0])
	for _, u := range same[1:] {
		if got := curateKey(u); got != want {
			t.Errorf("%q normalized to %q, want %q", u, got, want)
		}
	}
	if curateKey("https://e.com/other") == want {
		t.Error("different posts collapsed to one key")
	}
}

func TestCurateUnknownItemFails(t *testing.T) {
	v := newVault(t)
	s, _ := svcWithItem(t, v)
	if _, err := s.Curate("consume:rss:nope:deadbeef", ""); err == nil {
		t.Error("curating a nonexistent item should fail")
	}
	if err := s.Uncurate("consume:rss:nope:deadbeef"); err == nil {
		t.Error("un-curating a nonexistent item should fail")
	}
}

func TestSetFrontmatter(t *testing.T) {
	in := "---\na: 1\nb: 2\n---\nbody\n"
	if got := setFrontmatter(in, "b", "9"); got != "---\na: 1\nb: 9\n---\nbody\n" {
		t.Errorf("update in place: %q", got)
	}
	if got := setFrontmatter(in, "c", "3"); got != "---\na: 1\nb: 2\nc: 3\n---\nbody\n" {
		t.Errorf("append: %q", got)
	}
	if got := setFrontmatter(in, "a", ""); got != "---\nb: 2\n---\nbody\n" {
		t.Errorf("remove: %q", got)
	}
	// No frontmatter at all, and nothing to remove: leave it alone.
	if got := setFrontmatter("plain\n", "x", ""); got != "plain\n" {
		t.Errorf("no-op case rewrote the file: %q", got)
	}
}
