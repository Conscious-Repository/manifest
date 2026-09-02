package consume

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	entry, err := s.Curate(context.Background(), it.ID, "the middle third is the whole argument")
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
	entry, err := s.Curate(context.Background(), it.ID, "first take")
	if err != nil {
		t.Fatal(err)
	}

	// The owner adds his own thinking, in Obsidian.
	original := v.read(t, entry.Path)
	edited := original + "\n\n## my response\n\nI think this is wrong because…\n"
	if err := v.io().Write(entry.Path, []byte(edited)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Curate(context.Background(), it.ID, "second take"); err != nil {
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
	entry, err := s.Curate(context.Background(), it.ID, "a note")
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
	if _, err := s.Curate(context.Background(), it.ID, "back on"); err != nil {
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
	first, err := s.Curate(context.Background(), it.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	other := it
	other.ID = itemID(KindRSS, "melissa", "guid-2")
	other.URL = "https://other.example/p/same-title"
	s.store.Commit("melissa", time.Now().UTC(), true, []Item{other}, nil, "")
	second, err := s.Curate(context.Background(), other.ID, "")
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

	entry, err := s.Curate(context.Background(), it.ID, `he said "no" — twice`)
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
	if _, err := s.Curate(context.Background(), "consume:rss:nope:deadbeef", ""); err == nil {
		t.Error("curating a nonexistent item should fail")
	}
	if err := s.Uncurate("consume:rss:nope:deadbeef"); err == nil {
		t.Error("un-curating a nonexistent item should fail")
	}
}

// fullArticlePage is a full article page for the curate-time completion fetch.
// Long real paragraphs, a bit of furniture, no subscribe-box prose.
const fullArticlePage = `<!doctype html><html><head><title>t</title></head><body>
<nav><a href="/">home</a> <a href="/about">about</a></nav>
<article>
<p>The first paragraph of the full essay, which runs long enough to count as real prose for the extractor.</p>
<p>The second paragraph continues the argument with enough characters to matter to the scoring heuristic.</p>
<p>The third paragraph closes the piece and repeats nothing about signing anywhere at all.</p>
</article>
</body></html>`

// Curating a preview must capture the WHOLE piece: the owner is amplifying it,
// so the note, the snapshot, and therefore the public feed get the full body —
// even when the source is paid and fillFullText had already given up on it.
func TestCurateCompletesAPreviewItem(t *testing.T) {
	v := newVault(t)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fullArticlePage))
	}))
	defer page.Close()

	s := New(t.TempDir(), v.io(), Config{})
	s.hc = page.Client()
	sub := Subscription{ID: "paidsub", Kind: KindRSS, Title: "Paid Thing", URL: page.URL + "/feed", Mirror: MirrorFull}
	d := ParseFeeds("")
	d.Add(sub)
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	sub = d.Subs()[0]

	it := Item{
		ID: itemID(KindRSS, sub.ID, "guid-paid"), SubID: sub.ID,
		Source: "Paid Thing", Title: "A Paid Essay",
		URL:  page.URL + "/p/essay",
		Body: "<p>Just the teaser.</p>", Excerpt: "Just the teaser.", Chars: 16,
		Preview:     PreviewPaid,
		PublishedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		FetchedAt:   time.Now().UTC(),
	}
	s.store.Commit(sub.ID, time.Now().UTC(), true, []Item{it}, nil, "")

	entry, err := s.Curate(context.Background(), it.ID, "worth amplifying")
	if err != nil {
		t.Fatal(err)
	}
	note := v.read(t, entry.Path)
	for _, want := range []string{
		"mirror: full",
		"The first paragraph of the full essay",
		"The third paragraph closes the piece",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q — the full body was not captured:\n%s", want, note)
		}
	}
	// The snapshot serves the feed; it must hold the full body too.
	if body := s.store.Body(it.ID); !strings.Contains(body, "The second paragraph continues") {
		t.Errorf("snapshot still holds the teaser: %q", body)
	}
	// The cached record stops calling it a preview.
	for _, got := range s.store.Items(sub.ID) {
		if got.ID == it.ID && got.Preview != "" {
			t.Errorf("completed item still labelled preview %q", got.Preview)
		}
	}
	// And the public feed carries it inline.
	out := FeedXML(s.Entries(), PublicConfig{Title: "reading"})
	if !strings.Contains(out, "The first paragraph of the full essay") {
		t.Errorf("public feed does not carry the completed body inline:\n%s", out)
	}
	if !strings.Contains(out, "read at the source") {
		t.Error("attribution header missing from the mirrored body")
	}
}

// Notes the retired paid-source rule stamped `mirror: excerpt` get their one
// frontmatter field flipped at startup — and nothing else: the body, including
// anything the owner wrote, is his.
func TestBackfillFlipsWronglyExcerptedNotes(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	entry, err := s.Curate(context.Background(), it.ID, "kept")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate what the old rule left behind: mirror: excerpt on a mirror:full
	// subscription, plus the owner's own writing under the article.
	raw := strings.Replace(v.read(t, entry.Path), "mirror: full", "mirror: excerpt", 1)
	raw += "\n\n## my response\n\nstill thinking about this\n"
	if err := v.io().Write(entry.Path, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	s.invalidateCurated()

	if n := s.BackfillCurated(context.Background()); n != 1 {
		t.Fatalf("backfill flipped %d notes, want 1", n)
	}
	after := v.read(t, entry.Path)
	if !strings.Contains(after, "mirror: full") {
		t.Errorf("note not flipped to mirror:full:\n%s", after)
	}
	if !strings.Contains(after, "still thinking about this") {
		t.Fatalf("backfill destroyed the owner's writing:\n%s", after)
	}
	out := FeedXML(s.Entries(), PublicConfig{Title: "reading"})
	if !strings.Contains(out, "<strong>strong</strong>") {
		t.Errorf("backfilled entry does not mirror its full body:\n%s", out)
	}
	// A second pass finds nothing left to do.
	if n := s.BackfillCurated(context.Background()); n != 0 {
		t.Errorf("backfill is not idempotent: flipped %d more", n)
	}
}

// mirror:excerpt on the SUBSCRIPTION is the owner's own choice, and the
// backfill must not override it.
func TestBackfillRespectsOwnersExcerptChoice(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	entry, err := s.Curate(context.Background(), it.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSub(Subscription{ID: it.SubID, Mirror: MirrorExcerpt}); err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(v.read(t, entry.Path), "mirror: full", "mirror: excerpt", 1)
	if err := v.io().Write(entry.Path, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	s.invalidateCurated()

	if n := s.BackfillCurated(context.Background()); n != 0 {
		t.Fatalf("backfill overrode the owner's excerpt setting on %d notes", n)
	}
	if !strings.Contains(v.read(t, entry.Path), "mirror: excerpt") {
		t.Error("note no longer records excerpt")
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

func TestCurateRSSHubXPostUsesHandleTitleAndFullBody(t *testing.T) {
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{})
	sub := Subscription{ID: "melissa-2", Kind: KindRSS, Title: "@melissa", URL: defaultRSSHubBase + "/twitter/user/melissa", Mirror: MirrorFull, List: "people"}
	d := ParseFeeds("")
	d.Add(sub)
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	sub, _ = d.Find(d.Subs()[0].ID)

	post := "i'm not sure ai detectors need to exist. it does not super matter if you're not using ai if you sound like ai\n\ni want to see people talk to their robots."
	it := Item{
		ID: itemID(KindRSS, sub.ID, "2091877437294666036"), SubID: sub.ID,
		Source: "@melissa", Author: "@melissa",
		Title: "i'm not sure ai detectors need to exist. it does not super matter if you're not using ai if you sound like ai i want to see people talk to their robot...",
		URL:   "https://x.com/melissa/status/2091877437294666036",
		Body:  xBody(post, xPost{}), Excerpt: post, Chars: len([]rune(post)),
		PublishedAt: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
		FetchedAt:   time.Now().UTC(),
	}
	s.store.Commit(sub.ID, time.Now().UTC(), true, []Item{it}, nil, "")

	entry, err := s.Curate(context.Background(), it.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "@melissa on X" {
		t.Fatalf("title = %q", entry.Title)
	}
	note := v.read(t, entry.Path)
	if !strings.Contains(note, "# @melissa on X") {
		t.Fatalf("note heading not normalized:\n%s", note)
	}
	if !strings.Contains(note, "i want to see people talk to their robots") {
		t.Fatalf("full post body missing:\n%s", note)
	}
	if strings.Contains(note, "# i'm not sure ai detectors") {
		t.Fatalf("post text is still the note title:\n%s", note)
	}
}

func TestBackfillXPostsRetitlesExistingCuratedNotes(t *testing.T) {
	v := newVault(t)
	s, _ := svcWithItem(t, v)
	old := `---
categories: [articles]
source: "@melissa"
author: "@melissa"
url: https://x.com/melissa/status/2091877437294666036
published: 2026-08-24
curated: 2026-08-27
item: consume:rss:melissa-2:a148b2dad743
mirror: full
---

#article

# i'm not sure ai detectors need to exist. it does not super matter if you're not using ai if you sound like ai i want to see people talk to their robot...

i'm not sure ai detectors need to exist.

i want to see people talk to their robots.

---

Source: [@melissa](https://x.com/melissa/status/2091877437294666036)
`
	if err := v.io().Write("extrinsic/old-melissa.md", []byte(old)); err != nil {
		t.Fatal(err)
	}
	s.invalidateCurated()
	if n := s.BackfillCurated(context.Background()); n != 1 {
		t.Fatalf("backfilled %d notes, want 1", n)
	}
	after := v.read(t, "extrinsic/old-melissa.md")
	if !strings.Contains(after, "# @melissa on X") {
		t.Fatalf("heading not normalized:\n%s", after)
	}
	if !strings.Contains(after, "i want to see people talk to their robots") {
		t.Fatalf("post body was lost:\n%s", after)
	}
	if strings.Contains(after, "# i'm not sure ai detectors") {
		t.Fatalf("old text-title survived:\n%s", after)
	}
	if n := s.BackfillCurated(context.Background()); n != 0 {
		t.Fatalf("backfill is not a fixpoint; second run changed %d notes", n)
	}
}
