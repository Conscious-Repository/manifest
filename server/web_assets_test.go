package server

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// index.html is the whole cockpit's module graph, written by hand, with no
// bundler and no build step to check it. Nothing else in this repo notices when
// a <script src> and the file it names stop agreeing:
//
//   - add js/96-aion-recruiting.js, forget the tag, and the page loads clean
//     while the tab that module defines is simply never wired — no error, no
//     404, the module just never runs.
//   - rename or delete a file that IS tagged, and the browser gets the SPA
//     shell back for the missing URL, so the console shows "Unexpected token
//     '<'" pointing at a line of HTML, and every module after it in load order
//     dies with it. That is the same failure web_sw_test.go documents.
//
// Both are one-character mistakes, and both survive `go build`, `go test` and a
// deploy. This is the gate: every tag resolves inside the embed, and every
// first-party module is tagged.

var (
	// src="…" on a <script>, href="…" on a <link rel="stylesheet">. Attribute
	// order varies, so match the tag, then pull the attributes out of it.
	htmlScriptTag = regexp.MustCompile(`(?is)<script\b[^>]*\bsrc\s*=\s*"([^"]+)"[^>]*>`)
	htmlLinkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	htmlHrefAttr  = regexp.MustCompile(`(?is)\bhref\s*=\s*"([^"]+)"`)
	htmlRelAttr   = regexp.MustCompile(`(?is)\brel\s*=\s*"([^"]+)"`)
)

// firstPartyUnreferenced lists files under web/js|web/css that are deliberately
// not loaded by index.html. Empty on purpose: today every one of them is
// tagged, and a new orphan should have to be argued for here rather than drift
// in unnoticed. The portals' own sources live under web/portal|web/ooda and are
// not scanned by this test at all — they have their own pages and their own
// guard in web_chat_assets_test.go.
var firstPartyUnreferenced = map[string]bool{}

// scanAssetRefs returns the paths that page references via <script src> and
// <link rel="stylesheet" href>, joined onto dir and with any ?v= cache-buster
// stripped. Off-origin and inline sources are skipped — they are not ours to
// resolve.
func scanAssetRefs(src, dir string) []string {
	var raw []string
	for _, m := range htmlScriptTag.FindAllStringSubmatch(src, -1) {
		raw = append(raw, m[1])
	}
	for _, tag := range htmlLinkTag.FindAllString(src, -1) {
		rel := htmlRelAttr.FindStringSubmatch(tag)
		href := htmlHrefAttr.FindStringSubmatch(tag)
		if rel == nil || href == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rel[1]), "stylesheet") {
			continue // manifest, icons, preconnect — not part of the module graph
		}
		raw = append(raw, href[1])
	}

	var out []string
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" || strings.HasPrefix(r, "//") || strings.HasPrefix(r, "data:") ||
			strings.Contains(r, "://") {
			continue
		}
		if i := strings.IndexAny(r, "?#"); i >= 0 {
			r = r[:i] // css/92-aion.css?v=3 → css/92-aion.css
		}
		out = append(out, path.Join(dir, strings.TrimPrefix(r, "/")))
	}
	sort.Strings(out)
	return out
}

// indexAssetRefs is scanAssetRefs over the embedded cockpit page.
func indexAssetRefs(t *testing.T) []string {
	t.Helper()
	b, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatalf("read web/index.html: %v", err)
	}
	refs := scanAssetRefs(string(b), "web")
	if len(refs) < 20 {
		t.Fatalf("only %d asset refs parsed out of index.html — the scanner is "+
			"broken, not the page", len(refs))
	}
	return refs
}

// Every tag on the page must name a file that actually ships in the embed.
func TestIndexAssetsResolveInEmbed(t *testing.T) {
	for _, ref := range indexAssetRefs(t) {
		if _, err := fs.Stat(webFiles, ref); err != nil {
			t.Errorf("index.html references %s, which is not in the web embed: %v\n"+
				"  a missing sub-resource is answered with the SPA shell, so the "+
				"browser parses HTML as JS/CSS and every module after it dies too", ref, err)
		}
	}
}

// And every first-party module must be named by a tag — the other direction,
// which is the "added the file, forgot the tag" half.
func TestFirstPartyAssetsAreReferenced(t *testing.T) {
	referenced := map[string]bool{}
	for _, ref := range indexAssetRefs(t) {
		referenced[ref] = true
	}
	for _, dir := range []string{"web/js", "web/css"} {
		ents, err := fs.ReadDir(webFiles, dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			name := path.Join(dir, e.Name())
			if !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css") {
				continue
			}
			if firstPartyUnreferenced[name] {
				continue
			}
			if !referenced[name] {
				t.Errorf("%s ships in the embed but no tag in index.html loads it — "+
					"the module never runs, silently. Add the tag, or record the "+
					"exception in firstPartyUnreferenced with a reason.", name)
			}
		}
	}
}

// Vendor files ride the same tags and are resolved by the check above; this
// pins the two the terminal actually depends on, because losing either is a
// blank TERMINAL tab rather than a visible error.
func TestVendorTerminalAssetsWired(t *testing.T) {
	referenced := map[string]bool{}
	for _, ref := range indexAssetRefs(t) {
		referenced[ref] = true
	}
	for _, want := range []string{"web/vendor/xterm.js", "web/vendor/xterm.css"} {
		if !referenced[want] {
			t.Errorf("index.html no longer loads %s — the TERMINAL tab needs both "+
				"the module and its stylesheet", want)
		}
	}
}

// The scanner itself, against the shapes the real page uses: a cache-buster must
// not hide a reference, a non-stylesheet <link> must not be mistaken for one, an
// off-origin URL and an inline <script> must both be skipped.
func TestAssetRefScannerShape(t *testing.T) {
	const page = `<head>
  <link rel="manifest" href="manifest.webmanifest" />
  <link rel="apple-touch-icon" href="icons/icon-192.png" />
  <link rel="stylesheet" href="css/00-core.css" />
  <link rel="stylesheet" href="css/92-aion.css?v=3" />
  <link rel="stylesheet" href="https://cdn.example.com/x.css" />
</head>
<body>
  <script src="js/00-core.js?v=2"></script>
  <script src="vendor/xterm.js"></script>
  <script>inline()</script>
</body>`
	got := strings.Join(scanAssetRefs(page, "web"), ",")
	want := strings.Join([]string{
		"web/css/00-core.css",
		"web/css/92-aion.css",
		"web/js/00-core.js",
		"web/vendor/xterm.js",
	}, ",")
	if got != want {
		t.Fatalf("scanner drifted:\n got %v\nwant %v", got, want)
	}

	// and a tag naming a file that does not exist has to be visible to the
	// resolve check, cache-buster and all
	if refs := scanAssetRefs(`<script src="js/96-aion-recruiting.js?v=1"></script>`, "web"); len(refs) != 1 ||
		refs[0] != "web/js/96-aion-recruiting.js" {
		t.Fatalf("a versioned script tag did not resolve to its file: %v", refs)
	}
}
