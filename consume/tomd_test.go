package consume

import (
	"strings"
	"testing"
)

func TestToMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
		not  []string
	}{
		{
			name: "emphasis and links",
			in:   `<p>A <strong>strong</strong> and <em>soft</em> <a href="https://e.com">link</a>.</p>`,
			want: []string{"**strong**", "*soft*", "[link](https://e.com)"},
			not:  []string{"<p>", "<a "},
		},
		{
			name: "headings",
			in:   `<h1>One</h1><h2>Two</h2><h3>Three</h3>`,
			want: []string{"# One", "## Two", "### Three"},
		},
		{
			name: "blockquote",
			in:   `<blockquote><p>Quoted line.</p></blockquote>`,
			want: []string{"> Quoted line."},
		},
		{
			name: "unordered list",
			in:   `<ul><li>one</li><li>two</li></ul>`,
			want: []string{"- one", "- two"},
		},
		{
			name: "ordered list",
			in:   `<ol><li>first</li><li>second</li></ol>`,
			want: []string{"1. first", "2. second"},
		},
		{
			name: "nested list",
			in:   `<ul><li>outer<ul><li>inner</li></ul></li></ul>`,
			want: []string{"- outer", "  - inner"},
		},
		{
			name: "code block",
			in:   "<pre><code>x := 1</code></pre>",
			want: []string{"```", "x := 1"},
		},
		{
			name: "inline code",
			in:   `<p>call <code>Sanitize()</code> first</p>`,
			want: []string{"`Sanitize()`"},
		},
		{
			name: "image and caption",
			in:   `<figure><img src="https://e.com/a.png" alt="a chart"><figcaption>Fig 1</figcaption></figure>`,
			want: []string{"![a chart](https://e.com/a.png)", "*Fig 1*"},
		},
		{
			name: "rule",
			in:   `<p>a</p><hr><p>b</p>`,
			want: []string{"---"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToMarkdown(tc.in)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q\ngot:\n%s", w, got)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(got, n) {
					t.Errorf("unexpected %q\ngot:\n%s", n, got)
				}
			}
		})
	}
}

// A real article passes through the sanitizer first, so the converter only
// ever sees the allowlist — but it must not leave stray markup either way.
func TestToMarkdownLeavesNoMarkup(t *testing.T) {
	article := Sanitize(`<div class="post"><h2>Title</h2>` +
		`<p>Intro with <a href="https://e.com/x">a link</a> and <strong>weight</strong>.</p>` +
		`<blockquote><p>A quote.</p></blockquote>` +
		`<ul><li>point one</li><li>point <em>two</em></li></ul>` +
		`<img src="https://e.com/i.png" alt="chart">` +
		`<script>alert(1)</script></div>`)
	got := ToMarkdown(article)
	if strings.Contains(got, "<") && strings.Contains(got, ">") {
		// Angle brackets can legitimately survive inside text; what must not
		// survive is a tag.
		for _, tag := range []string{"<p", "<div", "<a ", "<img", "<script", "<ul", "<li", "<h2"} {
			if strings.Contains(got, tag) {
				t.Errorf("markup survived conversion (%s):\n%s", tag, got)
			}
		}
	}
	for _, want := range []string{"## Title", "[a link](https://e.com/x)", "**weight**", "> A quote.", "- point one", "![chart]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "alert(1)") {
		t.Errorf("script content survived:\n%s", got)
	}
}

func TestToMarkdownEmptyAndPlain(t *testing.T) {
	if ToMarkdown("") != "" || ToMarkdown("   ") != "" {
		t.Error("empty in, empty out")
	}
	if got := ToMarkdown("just text"); !strings.Contains(got, "just text") {
		t.Errorf("plain text lost: %q", got)
	}
	// No runaway blank lines — the note has to be pleasant to read.
	if got := ToMarkdown("<p>a</p><p></p><p></p><p>b</p>"); strings.Contains(got, "\n\n\n") {
		t.Errorf("blank-line runs not collapsed: %q", got)
	}
}

// Emphasis around whitespace-only content is literal asterisks in markdown,
// not emphasis — it would render as visible junk in the note.
func TestToMarkdownSkipsEmptyEmphasis(t *testing.T) {
	if got := ToMarkdown("<p><strong> </strong>text</p>"); strings.Contains(got, "****") {
		t.Errorf("empty emphasis produced literal asterisks: %q", got)
	}
}
