package record

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	block, body, ok := SplitFrontmatter("---\na: 1\nb: two\n---\n\nbody text\n")
	if !ok || len(block) != 2 || block[0] != "a: 1" {
		t.Fatalf("block: %v ok=%v", block, ok)
	}
	if body != "body text\n" {
		t.Fatalf("body: %q", body)
	}

	// no frontmatter → whole content back
	if _, body, ok := SplitFrontmatter("# heading\ntext\n"); ok || body != "# heading\ntext\n" {
		t.Fatalf("no-fm: ok=%v body=%q", ok, body)
	}
	// unterminated → treated as no frontmatter
	if _, body, ok := SplitFrontmatter("---\na: 1\nno closer"); ok || !strings.HasPrefix(body, "---") {
		t.Fatalf("unterminated: ok=%v body=%q", ok, body)
	}
	// YAML `...` closer + CRLF tolerance
	if block, _, ok := SplitFrontmatter("---\r\nk: v\r\n...\r\nbody"); !ok || block[0] != "k: v" {
		t.Fatalf("crlf/dots: %v ok=%v", block, ok)
	}
}

func TestUnquoteAndParseList(t *testing.T) {
	if Unquote(`"a: b"`) != "a: b" || Unquote(`'x'`) != "x" || Unquote(` plain `) != "plain" {
		t.Fatal("unquote")
	}
	got := ParseList(`[a, "b", c]`)
	if len(got) != 3 || got[1] != "b" {
		t.Fatalf("bracketed: %v", got)
	}
	if got := ParseList(`a, b`); len(got) != 2 || got[1] != "b" {
		t.Fatalf("bare: %v", got)
	}
	if ParseList("  ") != nil || ParseList("[]") != nil {
		t.Fatal("empty lists")
	}
}

func TestFrontmatterScalar(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.md", "---\ntype: agent\ntags: [x]\n---\nbody\n")
	if got := FrontmatterScalar(a, "type"); got != "agent" {
		t.Fatalf("type: %q", got)
	}
	b := write("b.md", "#journal\nno frontmatter\n")
	if got := FrontmatterScalar(b, "type"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	c := write("c.md", "---\nfoo: bar\n---\n")
	if got := FrontmatterScalar(c, "type"); got != "" {
		t.Fatalf("expected empty type, got %q", got)
	}
}
