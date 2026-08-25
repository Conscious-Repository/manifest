package consume

import (
	"strings"
	"testing"
)

// The sanitizer gates the only innerHTML sink in the FEED, so it is tested
// adversarially: not "does prose survive" but "does anything executable get
// through". Every case here is a real payload shape from XSS filter-evasion
// literature, not a hypothetical.
func TestSanitizeDropsEverythingExecutable(t *testing.T) {
	forbidden := []string{
		"<script", "javascript:", "onerror", "onload", "onclick", "<iframe",
		"<object", "<embed", "<form", "<svg", "srcdoc", "vbscript:",
		"data:text/html", "<style", "expression(",
	}
	cases := []struct {
		name string
		in   string
	}{
		{"bare script", `<p>hi</p><script>alert(1)</script>`},
		{"inline handler", `<p onclick="alert(1)">hi</p>`},
		{"img onerror", `<img src=x onerror=alert(1)>`},
		{"svg onload", `<svg/onload=alert(1)>`},
		{"javascript href", `<a href="javascript:alert(1)">click</a>`},
		{"javascript href mixed case", `<a href="JaVaScRiPt:alert(1)">click</a>`},
		{"javascript href padded", `<a href="  javascript:alert(1)">click</a>`},
		// The tab inside the scheme is the classic prefix-check defeat: a
		// browser honours "java\tscript:", a HasPrefix check does not.
		{"javascript href with tab", "<a href=\"java\tscript:alert(1)\">click</a>"},
		{"javascript href with newline", "<a href=\"java\nscript:alert(1)\">click</a>"},
		{"data uri html", `<a href="data:text/html;base64,PHNjcmlwdD4=">x</a>`},
		{"vbscript", `<a href="vbscript:msgbox(1)">x</a>`},
		{"iframe", `<iframe src="https://evil.example"></iframe>`},
		{"iframe srcdoc", `<iframe srcdoc="<script>alert(1)</script>"></iframe>`},
		{"object", `<object data="evil.swf"></object>`},
		{"form", `<form action="https://evil.example"><input name="p"></form>`},
		{"style block", `<style>body{background:url(javascript:alert(1))}</style>`},
		{"style attr", `<p style="background:expression(alert(1))">hi</p>`},
		{"nested script in dropped parent", `<noscript><script>alert(1)</script></noscript>`},
		{"comment smuggling", `<!--<script>alert(1)</script>--><p>hi</p>`},
		{"malformed unclosed", `<p><script>alert(1)`},
		{"meta refresh", `<meta http-equiv="refresh" content="0;url=https://evil.example">`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.ToLower(Sanitize(tc.in))
			for _, bad := range forbidden {
				if strings.Contains(got, strings.ToLower(bad)) {
					t.Fatalf("sanitized output still contains %q\n in:  %s\n out: %s", bad, tc.in, got)
				}
			}
		})
	}
}

func TestSanitizeKeepsProse(t *testing.T) {
	in := `<div class="wrapper"><p>A <strong>strong</strong> claim and an <em>aside</em>.</p>` +
		`<blockquote>Quoted.</blockquote><ul><li>one</li><li>two</li></ul>` +
		`<h2>Heading</h2><pre><code>x := 1</code></pre>` +
		`<a href="https://example.com/post">the source</a>` +
		`<img src="https://example.com/i.png" alt="a chart"></div>`
	got := Sanitize(in)
	for _, want := range []string{
		"<p>", "<strong>strong</strong>", "<em>aside</em>", "<blockquote>",
		"<li>one</li>", "<h2>Heading</h2>", "<pre><code>", "the source",
		`href="https://example.com/post"`, `src="https://example.com/i.png"`,
		`alt="a chart"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prose lost %q\ngot: %s", want, got)
		}
	}
	// The styling wrapper is unwrapped, not dropped — its children survived
	// above, and its own class attribute did not.
	if strings.Contains(got, "wrapper") {
		t.Errorf("class attribute survived: %s", got)
	}
	if !strings.Contains(got, `rel="noopener noreferrer"`) {
		t.Errorf("link missing rel hardening: %s", got)
	}
	if !strings.Contains(got, `loading="lazy"`) {
		t.Errorf("image missing lazy loading: %s", got)
	}
}

// An <a> we refuse must keep its words. Dropping the subtree would silently
// delete a sentence from the middle of an article.
func TestSanitizeUnwrapsRefusedLinkButKeepsText(t *testing.T) {
	got := Sanitize(`<p>see <a href="javascript:alert(1)">this argument</a> for more</p>`)
	if !strings.Contains(got, "this argument") {
		t.Fatalf("refusing the href ate the prose: %s", got)
	}
	if strings.Contains(got, "<a") {
		t.Fatalf("the anchor survived: %s", got)
	}
}

// Relative URLs resolve against the wrong origin once a body is rendered
// outside its own site, so they are refused rather than silently broken.
func TestSanitizeRefusesRelativeAndInsecureURLs(t *testing.T) {
	if got := Sanitize(`<a href="/local/path">x</a>`); strings.Contains(got, "<a") {
		t.Errorf("relative href survived: %s", got)
	}
	if got := Sanitize(`<img src="http://example.com/i.png">`); strings.Contains(got, "<img") {
		t.Errorf("http image survived (mixed content + privacy leak): %s", got)
	}
	if got := Sanitize(`<a href="//example.com/x">x</a>`); !strings.Contains(got, "<a") {
		t.Errorf("protocol-relative href should be allowed: %s", got)
	}
}

// Idempotence matters because a body can be re-sanitized on its way to the
// vault note and the public feed. A sanitizer that mangles its own output
// would corrupt an article a little more on each pass.
func TestSanitizeIsIdempotent(t *testing.T) {
	inputs := []string{
		`<p>A <strong>claim</strong> with a <a href="https://e.com">link</a>.</p>`,
		`<div><p>nested</p><script>alert(1)</script></div>`,
		`<img src="https://e.com/a.png" alt="x">`,
		`<blockquote><p>quote</p></blockquote><hr><ul><li>a</li></ul>`,
	}
	for _, in := range inputs {
		once := Sanitize(in)
		twice := Sanitize(once)
		if once != twice {
			t.Errorf("not idempotent\n in:    %s\n once:  %s\n twice: %s", in, once, twice)
		}
	}
}

func TestTextAndExcerpt(t *testing.T) {
	in := `<h2>Title</h2><p>First para.</p><p>Second  para   here.</p><script>alert(1)</script>`
	got := Text(in)
	if strings.Contains(got, "alert") {
		t.Errorf("script text leaked into plain text: %q", got)
	}
	if !strings.Contains(got, "First para.") || !strings.Contains(got, "Second para here.") {
		t.Errorf("prose lost or whitespace not collapsed: %q", got)
	}
	// Blocks must not run together — the excerpt is two lines, not one word.
	if strings.Contains(got, "Title First") {
		t.Errorf("block boundary lost: %q", got)
	}
	if e := Excerpt("one two three four five", 100); e != "one two three four five" {
		t.Errorf("short text should pass through: %q", e)
	}
	e := Excerpt(strings.Repeat("word ", 100), 20)
	if len([]rune(e)) > 21 || !strings.HasSuffix(e, "…") {
		t.Errorf("excerpt not cut correctly: %q", e)
	}
}

// Entities must survive as entities, not be double-escaped into visible junk.
func TestSanitizeHandlesEntities(t *testing.T) {
	got := Sanitize(`<p>Tom &amp; Jerry &lt;3 &quot;quotes&quot;</p>`)
	if strings.Contains(got, "&amp;amp;") {
		t.Errorf("double-escaped: %s", got)
	}
	if !strings.Contains(got, "&amp;") {
		t.Errorf("ampersand not preserved as an entity: %s", got)
	}
}

func TestSanitizeEmptyAndOversized(t *testing.T) {
	if Sanitize("") != "" || Sanitize("   ") != "" {
		t.Error("empty input should yield empty output")
	}
	huge := strings.Repeat("<p>x</p>", maxHTML)
	if got := Sanitize(huge); len(got) > maxHTML+1024 {
		t.Errorf("oversized input not bounded: %d bytes", len(got))
	}
}
