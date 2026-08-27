package consume

import (
	"strings"
	"testing"
)

func TestToHTMLRendersTheNoteVocabulary(t *testing.T) {
	md := `A paragraph with **strong**, *em*, ` + "`code`" + ` and a
line-wrapped second half.

## A heading

- one
- two
  - nested
1. first
2. second

> quoted prose
> over two lines

` + "```" + `
code := "verbatim"
` + "```" + `

---

Source: [Melissa's Newsletter](https://m.example/p/x)
`
	got := ToHTML(md)
	for _, want := range []string{
		"<strong>strong</strong>", "<em>em</em>", "<code>code</code>",
		"line-wrapped second half.</p>", // the wrap did not become two paragraphs
		"<h2>A heading</h2>",
		"<ul><li>one</li><li>two<ul><li>nested</li></ul></li></ul>",
		"<ol><li>first</li><li>second</li></ol>",
		"<blockquote><p>quoted prose over two lines</p></blockquote>",
		`code := &#34;verbatim&#34;`,
		"<hr/>",
		`<a href="https://m.example/p/x"`, "Melissa&#39;s Newsletter</a>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// The real path a note's body travels: publisher markup → ToMarkdown (what is
// archived in the vault) → ToHTML (what the public feed serves when the
// snapshot is gone). The prose must survive the round trip.
func TestToHTMLRoundTripsAnArchivedBody(t *testing.T) {
	body := Sanitize(`<p>The <strong>first</strong> claim, with a
		<a href="https://e.com/cited">citation</a>.</p>
		<h2>Second section</h2>
		<ul><li>a point</li><li>another point</li></ul>
		<blockquote><p>Someone else said this.</p></blockquote>
		<p>A closing thought.</p>`)

	got := ToHTML(ToMarkdown(body))
	for _, want := range []string{
		"<strong>first</strong>", "citation</a>", "https://e.com/cited",
		"<h2>Second section</h2>", "<li>a point</li>", "<li>another point</li>",
		"Someone else said this.", "A closing thought.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("round trip lost %q:\n%s", want, got)
		}
	}
}

// A note body is markdown the owner can edit by hand, and the public surface
// should not have to know which of its two body sources is the trusted one.
// Raw markup in a note is TEXT here — it is written back out escaped, so it
// reads as itself and cannot act — and the links and images the renderer does
// produce are held to the same allowlist as a publisher's markup.
func TestToHTMLIsSanitized(t *testing.T) {
	got := ToHTML(strings.Join([]string{
		`<script>alert(1)</script>`,
		`<img src="x" onerror="alert(1)">`,
		`[click](javascript:alert(1))`,
		`![leak](http://tracker.example/pixel.gif)`,
		`<div onclick="alert(1)">wrapped prose</div>`,
	}, "\n\n"))
	for _, banned := range []string{
		"<script", "<div", "<img", `onerror="`, `onclick="`,
		`href="javascript:`, `src="http://`,
	} {
		if strings.Contains(got, banned) {
			t.Errorf("%q survived as live markup:\n%s", banned, got)
		}
	}
	// …and every word is still there, escaped.
	for _, want := range []string{"&lt;script&gt;", "wrapped prose", "click"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q — sanitizing took the prose with it:\n%s", want, got)
		}
	}
}

// Anything outside the vocabulary is text, not markup. A note full of stray
// punctuation must read as itself rather than as a half-parsed document.
func TestToHTMLLeavesUnknownMarkupAsText(t *testing.T) {
	got := ToHTML("3 * 4 * 5 is not emphasis, and [this] is not a link. A < B & C > D.")
	for _, want := range []string{"3 * 4 * 5", "[this]", "A &lt; B &amp; C &gt; D"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<em>") {
		t.Errorf("multiplication became emphasis:\n%s", got)
	}
	if ToHTML("   \n\n  ") != "" {
		t.Error("an empty note should render nothing")
	}
}
