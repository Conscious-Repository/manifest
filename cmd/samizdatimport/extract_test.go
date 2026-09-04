package main

import (
	"strings"
	"testing"
)

// A trimmed Substack post page: head metadata, the header chrome that must
// NOT reach the note, and an available-content body exercising every
// component the converter rewrites.
const fixturePage = `<!DOCTYPE html><html><head>
<title>A boring MRI - by Benjamin Anderson</title>
<meta property="og:title" content="A boring MRI"/>
<meta property="og:description" content="And why it&#x27;s super exciting"/>
<meta property="og:url" content="https://www.consciousrepository.com/p/a-boring-mri"/>
<link rel="canonical" href="https://www.consciousrepository.com/p/a-boring-mri"/>
<script type="application/ld+json">{"@type":"NewsArticle","datePublished":"2026-09-03T12:07:42+00:00","dateModified":"2026-09-04T00:00:00+00:00"}</script>
</head><body>
<article class="typography newsletter-post post">
<div class="post-header"><h1 class="post-title">A boring MRI</h1><h3 class="subtitle">And why it's super exciting</h3>
<img src="https://substackcdn.com/image/fetch/$s_!OJ-d!,w_36,h_36/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Favatar.jpeg" alt="avatar"/></div>
<div class="available-content"><div class="body markup">
<p>First paragraph with a <a href="https://example.com/x" target="_blank" rel="noopener">link</a>.<a data-component-name="FootnoteAnchorToDOM" id="footnote-anchor-1" href="#footnote-1" class="footnote-anchor">1</a></p>
<div class="captioned-image-container"><figure>
<a target="_blank" href="https://substackcdn.com/image/fetch/$s_!65mj!,f_auto/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fabc_1756x1144.png" data-component-name="Image2ToDOM" class="image-link image2">
<div class="image2-inset"><picture><source type="image/webp" srcset="https://substackcdn.com/image/fetch/w_424,f_webp/x.png 424w"/>
<img src="https://substackcdn.com/image/fetch/$s_!65mj!,w_1456,c_limit,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fabc_1756x1144.png" data-attrs="{&quot;src&quot;:&quot;https://substack-post-media.s3.amazonaws.com/public/images/abc_1756x1144.png&quot;}" alt="" width="1456"/></picture>
<div class="image-link-expand">expand</div></div></a>
<figcaption class="image-caption">The scan</figcaption></figure></div>
<div id="youtube2-Rq_q93wtSNM" data-attrs="{&quot;videoId&quot;:&quot;Rq_q93wtSNM&quot;}" data-component-name="Youtube2ToDOM" class="youtube-wrap"><div class="youtube-inner"><iframe src="https://www.youtube-nocookie.com/embed/Rq_q93wtSNM?rel=0"></iframe></div></div>
<a href="https://mobile.twitter.com/rravi/status/147" target="_blank" data-component-name="Twitter2ToDOM"><div data-attrs="{&quot;url&quot;:&quot;https://mobile.twitter.com/rravi/status/147&quot;,&quot;full_text&quot;:&quot;&lt;span class=\&quot;tweet-fake-link\&quot;&gt;@drmichaellevin&lt;/span&gt; Any comments?&quot;,&quot;username&quot;:&quot;rravi&quot;}" class="pencraft tweet-fWkQfo twitter-embed"><div>ℝ𝕒𝕧𝕚 @rravi</div><div>@drmichaellevin Any comments?</div></div></a>
<div aria-label="Audio embed player" role="region" data-component-name="AudioEmbedPlayer" class="pencraft"><button>play</button><div>0:00</div><div>-14:51</div><audio src="/api/v1/audio/upload/5269d67f/src" preload="none"></audio></div>
<div data-component-name="VideoEmbedPlayer" id="media-e8c3c905" class="videoScrollTarget"><div class="placeholder"></div></div>
<div class="pullquote"><p><em>attention is your bandwidth</em></p></div>
<p class="button-wrapper" data-component-name="ButtonCreateButton"><a href="https://www.consciousrepository.com/p/a-boring-mri?action=share" class="button primary"><span>Share</span></a></p>
<p class="button-wrapper" data-component-name="ButtonCreateButton"><a href="https://share.transistor.fm/s/a7c" class="button primary"><span>Listen to the Audio</span></a></p>
<div class="subscription-widget-wrap"><div class="subscription-widget show-subscribe"><div class="preamble"><p>Enjoying this post? Subscribe.</p></div><form action="/api/v1/free"><input type="email"/></form></div></div>
<p>Last paragraph.</p>
<h2></h2>
<div class="footnote" data-component-name="FootnoteToDOM"><a id="footnote-1" href="#footnote-anchor-1" class="footnote-number">1</a><div class="footnote-content"><p>See <a href="https://qri.org/team">the team</a>.</p><p>Second para.</p></div></div>
</div></div>
<div class="visibility-check"></div>
<div class="post-footer"><button>Like</button><p>Share this post</p></div>
</article></body></html>`

func TestExtractFixture(t *testing.T) {
	e := siteEntry{Slug: "a-boring-mri", URL: siteBase + "/p/a-boring-mri", LastMod: "2026-09-04"}
	p, err := extract(fixturePage, e)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "A boring MRI" {
		t.Errorf("title %q", p.Title)
	}
	if p.Subtitle != "And why it's super exciting" {
		t.Errorf("subtitle %q", p.Subtitle)
	}
	if p.Description != "" {
		t.Errorf("description should fold into subtitle when identical, got %q", p.Description)
	}
	if p.Published != "2026-09-03" {
		t.Errorf("published %q: JSON-LD datePublished must beat sitemap lastmod", p.Published)
	}
	if p.URL != siteBase+"/p/a-boring-mri" {
		t.Errorf("url %q", p.URL)
	}
	md := p.Markdown
	want := []string{
		"First paragraph with a [link](https://example.com/x).[^1]",
		"![](https://substackcdn.com/image/fetch/$s_!65mj!,w_1456,c_limit,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fabc_1756x1144.png)",
		"*The scan*",
		"[YouTube: https://www.youtube.com/watch?v=Rq_q93wtSNM](https://www.youtube.com/watch?v=Rq_q93wtSNM)",
		"> @drmichaellevin Any comments?",
		"> — [@rravi](https://mobile.twitter.com/rravi/status/147)",
		"[Audio: https://www.consciousrepository.com/api/v1/audio/upload/5269d67f/src](https://www.consciousrepository.com/api/v1/audio/upload/5269d67f/src)",
		"[Video: https://www.consciousrepository.com/api/v1/video/upload/e8c3c905/src](https://www.consciousrepository.com/api/v1/video/upload/e8c3c905/src)",
		"> *attention is your bandwidth*",
		"[Listen to the Audio](https://share.transistor.fm/s/a7c)",
		"Last paragraph.\n\n[^1]: See [the team](https://qri.org/team). Second para.",
	}
	for _, w := range want {
		if !strings.Contains(md, w) {
			t.Errorf("markdown missing %q\n---\n%s", w, md)
		}
	}
	for _, bad := range []string{"expand", "0:00", "14:51", "Share", "Enjoying this post", "avatar", "Like", "https://mobile.twitter.com/rravi/status/147)[", "iframe", "<"} {
		if strings.Contains(md, bad) {
			t.Errorf("markdown should not contain %q\n---\n%s", bad, md)
		}
	}
	if len(p.Images) != 1 {
		t.Fatalf("images: want 1 body image (header avatar excluded), got %+v", p.Images)
	}
	if p.Images[0].Original != "https://substack-post-media.s3.amazonaws.com/public/images/abc_1756x1144.png" {
		t.Errorf("original %q", p.Images[0].Original)
	}
	note := renderNote(p)
	for _, w := range []string{
		"---\ntitle: \"A boring MRI\"\n",
		"subtitle: \"And why it's super exciting\"\n",
		"published: 2026-09-03\n",
		"url: https://www.consciousrepository.com/p/a-boring-mri\n",
		"source: substack\ncategories: [samizdat, substack]\n---\n\n",
	} {
		if !strings.Contains(note, w) {
			t.Errorf("note missing %q\n---\n%s", w, note)
		}
	}
	if strings.Contains(note, "#substack") || strings.Contains(note, "#samizdat") {
		t.Error("no inline hashtags in a samizdat note")
	}
}

func TestExtractFallbacks(t *testing.T) {
	page := `<html><head><title>Old post - by Benjamin</title></head><body>
<article><div class="post-header"><h1 class="post-title">Old post</h1></div>
<div class="available-content"><p>Body text.</p></div></article></body></html>`
	p, err := extract(page, siteEntry{Slug: "old-post", URL: siteBase + "/p/old-post", LastMod: "2020-05-05"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Old post" || p.Published != "2020-05-05" || p.URL != siteBase+"/p/old-post" {
		t.Errorf("fallbacks: %+v", p)
	}
	if _, err := extract(`<html><body><p>Too Many Requests</p></body></html>`, siteEntry{Slug: "x"}); err == nil {
		t.Error("a page without an article block must fail, not produce an empty note")
	}
}

func TestOriginalImageURL(t *testing.T) {
	cdn := "https://substackcdn.com/image/fetch/$s_!65mj!,w_1456,c_limit,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fabc_1756x1144.png"
	if got := originalImageURL(cdn, ""); got != "https://substack-post-media.s3.amazonaws.com/public/images/abc_1756x1144.png" {
		t.Errorf("cdn unwrap: %q", got)
	}
	if got := originalImageURL(cdn, `{"src":"https://bucket/direct.png"}`); got != "https://bucket/direct.png" {
		t.Errorf("data-attrs wins: %q", got)
	}
	if got := originalImageURL("https://example.com/a.jpg", ""); got != "https://example.com/a.jpg" {
		t.Errorf("plain url is its own original: %q", got)
	}
}

func TestParseSitemap(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<url><loc>https://www.consciousrepository.com/archive</loc><changefreq>daily</changefreq></url>
<url><loc>https://www.consciousrepository.com/p/a-boring-mri</loc><lastmod>2026-09-03</lastmod></url>
<url><loc>https://www.consciousrepository.com/p/on-speed</loc></url>
<url><loc>https://www.consciousrepository.com/p/a-boring-mri</loc><lastmod>2026-09-03</lastmod></url>
</urlset>`
	got, err := parseSitemap([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Slug != "a-boring-mri" || got[0].LastMod != "2026-09-03" || got[1].Slug != "on-speed" || got[1].LastMod != "" {
		t.Errorf("got %+v", got)
	}
	if got[0].URL != siteBase+"/p/a-boring-mri" {
		t.Errorf("canonical url %q", got[0].URL)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"the fdas hammer problem":            "the-fdas-hammer-problem",
		"The FDA's hammer problem":           "the-fdas-hammer-problem",
		"'til death do us part":              "til-death-do-us-part",
		"why are you standing on the ledge?": "why-are-you-standing-on-the-ledge",
		"Molecules Won’t Defeat Aging":       "molecules-wont-defeat-aging",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCandidateMatches(t *testing.T) {
	body := strings.Repeat("It's February 2024, and I'm navigating back to Saint Louis from upstate New York. ", 3)
	p := &post{Slug: "a-fundamental-error-in-biology", Title: "A Fundamental Error in Biology", Markdown: body}

	byName := candidate{Name: "a fundamental error in biology.md", slug: slugify("a fundamental error in biology")}
	if !byName.matches(p) {
		t.Error("same slug must match")
	}
	byProse := candidate{Name: "renamed.md", slug: "renamed",
		key: proseKey("_subtitle line_\n\npublished [[April 1, 2025]]\n\n"+body, keyLen)}
	if !byProse.matches(p) {
		t.Error("same opening prose must match (subtitle/published lines skipped)")
	}
	draft := candidate{Name: "it is hard to get a hammer approved by the FDA.md", slug: "it-is-hard-to-get-a-hammer-approved-by-the-fda",
		key: proseKey("Starting off, I thought that there was only one path to market for a medical device with a 1:many value proposition.", keyLen)}
	if draft.matches(p) {
		t.Error("a different draft must not match")
	}
	short := candidate{Name: "short.md", slug: "short", key: proseKey("It's February 2024", keyLen)}
	if short.matches(p) {
		t.Error("a key under minKey must never match on prose alone")
	}
}

func TestHasWritingTag(t *testing.T) {
	if !hasWritingTag([]string{"categories: [writing, published, substack]"}) {
		t.Error("inline list")
	}
	if !hasWritingTag([]string{"categories:", "  - substack", "  - essays", "status:"}) {
		t.Error("block list")
	}
	if hasWritingTag([]string{"categories: [people]", "aliases:", "  - writing"}) {
		t.Error("a list under another key is not categories")
	}
}

func TestImageExt(t *testing.T) {
	for _, tc := range []struct{ ctype, src, want string }{
		{"image/jpeg", "x", ".jpg"},
		{"image/png; charset=binary", "x", ".png"},
		{"binary/octet-stream", "https://h/a/b.JPEG?x=1", ".jpg"},
		{"application/octet-stream", "https://h/a/b.webp", ".webp"},
		{"text/html", "https://h/a/b.png", ""},
		{"", "https://h/a/b", ""},
	} {
		if got := imageExt(tc.ctype, tc.src); got != tc.want {
			t.Errorf("imageExt(%q,%q)=%q want %q", tc.ctype, tc.src, got, tc.want)
		}
	}
}

func TestYamlQuote(t *testing.T) {
	if got := yamlQuote(`He said "hi": a\b`); got != `"He said \"hi\": a\\b"` {
		t.Errorf("got %s", got)
	}
}
