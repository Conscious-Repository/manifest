package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"manifest/record"
	"manifest/vaultwriter"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type writingRig struct {
	t    *testing.T
	root string
	data string
	h    http.Handler
}

func newWritingRig(t *testing.T, files map[string]string) *writingRig {
	t.Helper()
	root, data := t.TempDir(), t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vw := vaultwriter.New(root).WithHistory(data).Grant(vaultwriter.Capability{Name: "writing", Zone: record.ZoneSystem, Pattern: "system/writing/**", Actor: vaultwriter.ActorUserAction})
	s := New(nil, nil, nil)
	s.UseVault(vw)
	s.UseWriting("system/writing")
	return &writingRig{t, root, data, s.Handler()}
}

func (g *writingRig) call(method, u string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if s, ok := body.(string); ok {
		rd = bytes.NewReader([]byte(s))
	} else {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, httptest.NewRequest(method, u, rd))
	return w
}

func (g *writingRig) search(q string) []string {
	g.t.Helper()
	w := g.call("GET", "/api/writing/search?q="+url.QueryEscape(q), nil)
	if w.Code != 200 {
		g.t.Fatalf("search %q: %d %s", q, w.Code, w.Body.String())
	}
	var out struct {
		Results []struct{ Path, Snippet string } `json:"results"`
		Total   int                              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	paths := []string{}
	for _, r := range out.Results {
		paths = append(paths, r.Path)
	}
	if out.Total < len(paths) || (len(paths) < 50 && out.Total != len(paths)) {
		g.t.Fatalf("total %d != %d results", out.Total, len(paths))
	}
	return paths
}

func TestExtractHashtags(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"# Heading\n## Sub", nil},
		{"#Alice-Rabbit and #-Alice", []string{"Alice"}},
		{"#2Alice #2024 #_x", []string{"2Alice", "_x"}},
		{"a#b c&#tag ##double", nil},
		{"(#paren), \"#quoted\" #über #日本", []string{"paren", "quoted", "über", "日本"}},
		{"```\n#incode\n```\n`#inline` after #real", []string{"real"}},
		{"~~~\n#tilde\n~~~", nil},
		{"see https://x.com/a#frag and www.y.com/#frag", nil},
		{"[link](notes.md#section) [[note#head]] [[#local]]", nil},
		{"#Tag twice #Tag and #tag", []string{"Tag", "tag"}},
		{"#tag.", []string{"tag"}},
	} {
		got := extractHashtags(c.in)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
	// Frontmatter tags are not hashtags.
	if got := extractHashtags(stripFrontmatter("---\ntitle: #nope\n---\nbody #yes")); !reflect.DeepEqual(got, []string{"yes"}) {
		t.Errorf("frontmatter: %q", got)
	}
}

func TestWritingExcerpt(t *testing.T) {
	got := writingExcerpt(stripFrontmatter("---\na: b\n---\n# Title\n\n- [ ] **bold** [link](x) and [[a|alias]]\n"), 160)
	if got != "Title bold link and alias" {
		t.Fatalf("excerpt %q", got)
	}
	long := writingExcerpt(strings.Repeat("word ", 100), 160)
	if !strings.HasSuffix(long, "…") || len([]rune(long)) > 161 {
		t.Fatalf("clip %q", long)
	}
}

func TestWritingSearchSyntax(t *testing.T) {
	g := newWritingRig(t, map[string]string{
		"alice.md":            "# Wonderland\n\nThe white rabbit ran. #story\n",
		"notes/rabbit.md":     "Rabbits everywhere.\n- [ ] feed them\n",
		"notes/café.md":       "Résumé of the CAFÉ meeting with Bob.\n- [x] booked\n",
		"far.md":              "alpha one two three four five six seven eight nine ten eleven twelve beta\n",
		"close.md":            "alpha and then beta\n",
		"code.md":             "```\n#hidden rabbit\n```\nplain #Story text\n",
		"system/writing/x.md": "rabbit in the record root",
	})
	check := func(q string, want ...string) {
		t.Helper()
		got := g.search(q)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v want %v", q, got, want)
		}
	}
	check("rabbit", "alice.md", "notes/rabbit.md", "code.md")
	check("abbit") // prefix at word start only
	check("RAB whi", "alice.md")
	check("cafe resume", "notes/café.md")
	check(`"white rabbit"`, "alice.md")
	check(`"rabbit ran" OR bob`, "alice.md", "notes/café.md")
	check("rabbit -story", "notes/rabbit.md")
	check(`rabbit -"white rabbit"`, "notes/rabbit.md", "code.md")
	check("rabbit NOT #story", "notes/rabbit.md")
	check("#story", "alice.md", "code.md")
	check("#STORY", "alice.md", "code.md")
	check("-#", "notes/rabbit.md", "notes/café.md", "far.md", "close.md")
	check("[ ]", "notes/rabbit.md")
	check("[x]", "notes/café.md")
	check("name:rab", "notes/rabbit.md")
	check("path:notes", "notes/rabbit.md", "notes/café.md")
	check("(bob OR feed) AND path:notes", "notes/rabbit.md", "notes/café.md")
	check("alpha beta", "far.md", "close.md")
	check("NEAR(alpha beta)", "close.md")
	check("NEAR(alpha beta 13)", "far.md", "close.md")
	// filename ≫ body: rabbit.md outranks alice.md's body mention.
	if got := g.search("rabbit"); got[0] != "notes/rabbit.md" {
		t.Errorf("ranking: %v", got)
	}
	for _, bad := range []string{"", "   ", `"open`, "(a", "a)", "NOT", "a OR", "NEAR(a)", "name:"} {
		if w := g.call("GET", "/api/writing/search?q="+url.QueryEscape(bad), nil); w.Code != 400 || strings.HasPrefix(w.Body.String(), "{") {
			t.Errorf("%q: %d %s", bad, w.Code, w.Body.String())
		}
	}
	w := g.call("GET", "/api/writing/search?q=meeting", nil)
	if !strings.Contains(w.Body.String(), `"snippet":"Résumé of the CAFÉ meeting with Bob. - [x] booked"`) {
		t.Errorf("snippet: %s", w.Body.String())
	}
	tags := g.call("GET", "/api/writing/tags", nil)
	if tags.Body.String() != `{"tags":[{"tag":"Story","count":2}]}`+"\n" && tags.Body.String() != `{"tags":[{"tag":"story","count":2}]}`+"\n" {
		t.Errorf("tags: %s", tags.Body.String())
	}
}

func TestWritingIndexRefreshAndFilesFields(t *testing.T) {
	g := newWritingRig(t, map[string]string{
		"one.md":             "---\ntags: x\n---\n# One\n\nFirst #Alpha body.",
		"two.md":             "Second #alpha #Alpha #beta",
		".hidden/h.md":       "secret",
		"system/agents/a.md": "engine",
	})
	type file struct {
		Path, Excerpt string
		Tags          []string
		ReadOnly      bool
	}
	files := func() map[string]file {
		w := g.call("GET", "/api/writing/files", nil)
		var out struct {
			Files   []file
			Folders []string
			VaultID string
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.VaultID == "" || out.Folders[0] != "" {
			t.Fatalf("files: %d %s", w.Code, w.Body.String())
		}
		m := map[string]file{}
		for _, f := range out.Files {
			m[f.Path] = f
		}
		return m
	}
	m := files()
	if len(m) != 2 || m["one.md"].Excerpt != "One First #Alpha body." || !reflect.DeepEqual(m["one.md"].Tags, []string{"Alpha"}) {
		t.Fatalf("%+v", m)
	}
	if !reflect.DeepEqual(m["two.md"].Tags, []string{"Alpha", "beta"}) {
		t.Fatalf("display casing: %+v", m["two.md"])
	}
	if !strings.Contains(g.call("GET", "/api/writing/files", nil).Body.String(), `"tags":[`) {
		t.Fatal("tags must be an array")
	}
	if got := g.search("first"); !slices.Equal(got, []string{"one.md"}) {
		t.Fatal(got)
	}
	// An outside edit (new size and mtime) and a deletion are seen next request.
	full := filepath.Join(g.root, "one.md")
	_ = os.WriteFile(full, []byte("Replaced words entirely"), 0o644)
	_ = os.Chtimes(full, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	if got := g.search("first"); len(got) != 0 {
		t.Fatal("stale text", got)
	}
	if got := g.search("replaced"); !slices.Equal(got, []string{"one.md"}) {
		t.Fatal(got)
	}
	_ = os.Remove(filepath.Join(g.root, "two.md"))
	if m = files(); len(m) != 1 {
		t.Fatal("deleted file kept", m)
	}
	if tags := g.call("GET", "/api/writing/tags", nil).Body.String(); tags != `{"tags":[]}`+"\n" {
		t.Fatal(tags)
	}
}

func TestWritingFolderAndNestedCreate(t *testing.T) {
	g := newWritingRig(t, map[string]string{"file.md": "x"})
	for _, bad := range []string{"", "/abs", "a/../b", "./a", "a/.hidden", `a\b`, "a//b", "system/writing", "system/writing/sub"} {
		if w := g.call("POST", "/api/writing/folder", map[string]string{"path": bad}); w.Code != 400 {
			t.Errorf("%q: %d", bad, w.Code)
		}
	}
	if w := g.call("POST", "/api/writing/folder", map[string]string{"path": "system/agents/x"}); w.Code != 403 {
		t.Errorf("engine-owned: %d", w.Code)
	}
	if w := g.call("POST", "/api/writing/folder", map[string]string{"path": "file.md"}); w.Code != 409 {
		t.Errorf("file collision: %d", w.Code)
	}
	if w := g.call("POST", "/api/writing/folder", map[string]string{"path": "a/b"}); w.Code != 200 || w.Body.String() != `{"path":"a/b"}`+"\n" {
		t.Fatal(w.Code, w.Body.String())
	}
	if fi, err := os.Stat(filepath.Join(g.root, "a/b")); err != nil || !fi.IsDir() {
		t.Fatal("folder not created")
	}
	if w := g.call("POST", "/api/writing/folder", map[string]string{"path": "a/b"}); w.Code != 200 {
		t.Fatal("existing folder", w.Code)
	}
	if !strings.Contains(g.call("GET", "/api/writing/files", nil).Body.String(), `"a/b"`) {
		t.Fatal("folder not listed")
	}
	if w := g.call("POST", "/api/writing/note", map[string]string{"path": "a/b/note.md", "body": "hi"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, bad := range []string{"missing/note.md", "a/../escape.md", "system/writing/n.md", "a/.h/n.md"} {
		if w := g.call("POST", "/api/writing/note", map[string]string{"path": bad, "body": "x"}); w.Code != 400 {
			t.Errorf("%q: %d %s", bad, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(g.root, "missing")); err == nil {
		t.Fatal("create invented a folder")
	}
}

func TestWritingLibraryRevisions(t *testing.T) {
	g := newWritingRig(t, nil)
	w := g.call("GET", "/api/writing/library", nil)
	if w.Body.String() != `{"library":{},"revision":""}`+"\n" {
		t.Fatal(w.Body.String())
	}
	lib := map[string]any{"favorites": []string{"a.md"}, "prefs": map[string]any{}}
	w = g.call("PUT", "/api/writing/library", map[string]any{"library": lib, "ifRevision": ""})
	var first struct {
		Library  map[string]any
		Revision string
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &first) != nil || first.Revision == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	// A second writer still holding "" sees the stored document.
	w = g.call("PUT", "/api/writing/library", map[string]any{"library": map[string]any{}, "ifRevision": ""})
	if w.Code != 409 || !strings.Contains(w.Body.String(), first.Revision) || !strings.Contains(w.Body.String(), "a.md") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = g.call("PUT", "/api/writing/library", map[string]any{"library": map[string]any{"smart": []any{}}, "ifRevision": first.Revision})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := g.call("GET", "/api/writing/library", nil).Body.String(); !strings.Contains(got, `{"smart":[]}`) {
		t.Fatal(got)
	}
	for _, bad := range []any{map[string]any{"library": []int{1}}, map[string]any{"library": "x"}, map[string]any{}, map[string]any{"library": map[string]string{"big": strings.Repeat("x", 257<<10)}}} {
		if w := g.call("PUT", "/api/writing/library", bad); w.Code != 400 {
			t.Errorf("%d for invalid library", w.Code)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(g.data, "writing-library"))
	if len(entries) != 1 {
		t.Fatal("expected one library file per vault", entries)
	}
}

func TestWritingAuthorshipRoundTripAndMove(t *testing.T) {
	g := newWritingRig(t, map[string]string{"draft.md": "hello world", "system/agents/a.md": "engine"})
	empty := g.call("GET", "/api/writing/authorship?path=draft.md", nil)
	if empty.Body.String() != `{"authors":[],"ranges":[],"revision":""}`+"\n" {
		t.Fatal(empty.Body.String())
	}
	doc := `{"revision":"r1","ranges":[{"from":0,"to":5,"author":"alfred","quote":"hello","prefix":"","suffix":" world"}],"authors":[{"id":"alfred","name":"Alfred","kind":"ai","color":"#a0a"}],"extra":1}`
	if w := g.call("PUT", "/api/writing/authorship?path=draft.md", doc); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got := g.call("GET", "/api/writing/authorship?path=draft.md", nil).Body.String()
	if !strings.Contains(got, `"quote":"hello"`) || !strings.Contains(got, `"extra":1`) {
		t.Fatal(got)
	}
	for _, c := range []struct{ path, body string }{
		{"missing.md", doc},
		{"../x.md", doc},
		{"draft.txt", doc},
		{"draft.md", `{"ranges":[{"from":5,"to":5}]}`},
		{"draft.md", `{"ranges":[{"from":-1,"to":5}]}`},
		{"draft.md", `{"authors":[{"kind":"robot"}]}`},
		{"draft.md", `[]`},
		{"draft.md", `{"ranges":[` + strings.Repeat(`{"from":0,"to":1},`, 5000) + `{"from":0,"to":1}]}`},
	} {
		if w := g.call("PUT", "/api/writing/authorship?path="+url.QueryEscape(c.path), c.body); w.Code != 400 {
			t.Errorf("%s %.40s: %d", c.path, c.body, w.Code)
		}
	}
	if w := g.call("PUT", "/api/writing/authorship?path=system/agents/a.md", doc); w.Code != 403 {
		t.Errorf("read-only note: %d", w.Code)
	}
	if w := g.call("PUT", "/api/writing/authorship?path=draft.md", strings.Repeat(" ", 1<<20+1)); w.Code != 413 {
		t.Errorf("oversize: %d", w.Code)
	}
	_ = os.Mkdir(filepath.Join(g.root, "drafts"), 0o755)
	rev := vaultwriter.Revision([]byte("hello world"))
	if w := g.call("POST", "/api/writing/move", map[string]any{"path": "draft.md", "to": "drafts/renamed.md", "ifRevision": rev}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := g.call("GET", "/api/writing/authorship?path=drafts/renamed.md", nil).Body.String(); !strings.Contains(got, `"quote":"hello"`) {
		t.Fatal("authorship lost on move", got)
	}
	_ = os.WriteFile(filepath.Join(g.root, "draft.md"), []byte("new note"), 0o644)
	if got := g.call("GET", "/api/writing/authorship?path=draft.md", nil).Body.String(); strings.Contains(got, "hello") {
		t.Fatal("sidecar left at old path", got)
	}
}

// TestWritingSearchLatency builds a ~2,000-note, ~24 MB synthetic vault and
// times warm searches. Owner target: well under 200 ms.
func TestWritingSearchLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 24 MB vault")
	}
	words := strings.Fields("the of and to in is that for it as with was on be by this are or at from but not have an they which one you were all we her she there been his their if has will would more when about who can said do into time so like other than could some these then them two its only see new now after over our also may most made first")
	rng := rand.New(rand.NewSource(1))
	files := map[string]string{}
	for i := range 2000 {
		var b strings.Builder
		fmt.Fprintf(&b, "---\ndate: 2026-01-01\n---\n# Note %d\n\n", i)
		for b.Len() < 12000 {
			b.WriteString(words[rng.Intn(len(words))])
			if rng.Intn(400) == 0 {
				fmt.Fprintf(&b, " #topic%d", rng.Intn(30))
			}
			if rng.Intn(15) == 0 {
				b.WriteString(".\n")
			} else {
				b.WriteByte(' ')
			}
		}
		if i%50 == 0 {
			b.WriteString("\nThe zephyr quartz appears here.\n- [ ] follow up\n")
		}
		files[fmt.Sprintf("folder%d/note-%d.md", i%20, i)] = b.String()
	}
	g := newWritingRig(t, files)
	start := time.Now()
	g.search("zephyr")
	t.Logf("cold build + search: %v", time.Since(start))
	for _, q := range []string{"zephyr", "the", `"appears here"`, "zephyr OR quartz -#topic3", "NEAR(zephyr quartz 3)", "[ ] name:note-1", "#topic7 first"} {
		g.search(q) // warm
		const n = 5
		start = time.Now()
		for range n {
			g.search(q)
		}
		per := time.Since(start) / n
		t.Logf("warm %-28q %v", q, per)
		if per > time.Second {
			t.Errorf("%q took %v", q, per)
		}
	}
}

func TestWritingIndexConcurrentReaders(t *testing.T) {
	files := map[string]string{}
	for i := range 50 {
		files[fmt.Sprintf("n%d.md", i)] = fmt.Sprintf("note %d #t%d body", i, i%5)
	}
	g := newWritingRig(t, files)
	done := make(chan bool)
	for i := range 8 {
		go func() {
			defer func() { done <- true }()
			for j := range 10 {
				if i%2 == 0 {
					_ = os.WriteFile(filepath.Join(g.root, fmt.Sprintf("n%d.md", j)), []byte(fmt.Sprintf("changed %d %d", i, j)), 0o644)
				}
				for _, u := range []string{"/api/writing/files", "/api/writing/tags", "/api/writing/search?q=note"} {
					if w := g.call("GET", u, nil); w.Code != 200 {
						t.Error(u, w.Code)
					}
				}
			}
		}()
	}
	for range 8 {
		<-done
	}
}
