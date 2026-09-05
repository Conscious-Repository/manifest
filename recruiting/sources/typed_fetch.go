package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// TYPED FETCH — rung 3 and rung 5 of the intake cascade.
//
// A lab page and a company page are the same shape to a URL: one host, one
// path, no identifier. But most of them SAY what they are, in a
// schema.org/JSON-LD block their own CMS wrote, and that is a fact read off
// the page rather than a guess about its words.
//
// This file only READS and REPORTS. It maps nothing onto our classes — that
// table lives in recruiting/intake_refine.go, next to the classes it names —
// because the moment a fetcher starts deciding what a page means, the meaning
// becomes untestable without a network.
//
// It reuses the Web adapter whole: the same SSRF refusal, the same dial
// guard, the same robots group, the same polite User-Agent, the same bounded
// read. A probe is one GET of one page the owner pasted — no frontier, no
// depth, no links followed.

// PageTypes is what one page declared about itself.
type PageTypes struct {
	URL      string   `json:"url"`                // the URL the answer came from (after redirects)
	JSONLD   []string `json:"jsonld,omitempty"`   // every @type token, document order, deduped
	OGType   string   `json:"ogType,omitempty"`   // og:type, "" when absent
	Name     string   `json:"name,omitempty"`     // schema.org name / og:title
	SiteName string   `json:"siteName,omitempty"` // og:site_name
}

// Empty reports a page that said nothing about itself.
func (p PageTypes) Empty() bool { return len(p.JSONLD) == 0 && p.OGType == "" }

// probeMaxTypes caps how many @type tokens are carried off one page. A
// schema graph can be long (every breadcrumb is a ListItem); the classifier
// reads the first few and the rest are noise.
const probeMaxTypes = 12

// ProbeTypes GETs one page and reports its declared types. Every guard the
// crawler applies to a stranger's link applies here to the owner's paste:
// refused hosts, private addresses, robots.txt, the body cap.
func (w Web) ProbeTypes(ctx context.Context, target string) (PageTypes, error) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return PageTypes{}, fmt.Errorf("web: %v", err)
	}
	if why := webRefuse(u); why != "" {
		return PageTypes{}, fmt.Errorf("web: %s", why)
	}
	if !w.allowedByRobots(ctx, map[string]*webRobots{}, u) {
		return PageTypes{}, fmt.Errorf("web: robots.txt disallows %s", u)
	}
	body, final, ctype, err := w.get(ctx, u.String(), webMaxBody)
	if err != nil {
		return PageTypes{}, err
	}
	if mt, _, _ := mime.ParseMediaType(ctype); mt != "text/html" && mt != "application/xhtml+xml" {
		return PageTypes{}, fmt.Errorf("web: %s is %q, not HTML", final, ctype)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return PageTypes{}, fmt.Errorf("web: %s did not parse: %v", final, err)
	}
	out := PageTypes{URL: final}
	readPageTypes(doc, &out)
	return out, nil
}

// readPageTypes walks the document once, collecting the ld+json blocks and
// the handful of meta properties worth reading.
func readPageTypes(n *html.Node, out *PageTypes) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script":
			if strings.Contains(strings.ToLower(attr(n, "type")), "ld+json") {
				collectLD(textOf(n), out)
			}
		case "meta":
			prop := strings.ToLower(attr(n, "property"))
			if prop == "" {
				prop = strings.ToLower(attr(n, "name"))
			}
			content := strings.TrimSpace(attr(n, "content"))
			switch prop {
			case "og:type":
				if out.OGType == "" {
					out.OGType = strings.ToLower(content)
				}
			case "og:site_name":
				if out.SiteName == "" {
					out.SiteName = content
				}
			case "og:title":
				if out.Name == "" {
					out.Name = content
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		readPageTypes(c, out)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// collectLD decodes one ld+json block and walks it for @type. The block may
// be an object, an array of objects, or an object with an @graph — all three
// are ordinary on real pages, and a block that does not decode is skipped
// rather than failing the probe, because half the web ships broken JSON-LD.
func collectLD(raw string, out *PageTypes) {
	var v any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &v); err != nil {
		return
	}
	walkLD(v, out, 0)
}

func walkLD(v any, out *PageTypes, depth int) {
	if depth > 6 || len(out.JSONLD) >= probeMaxTypes {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		addTypes(t["@type"], out)
		if out.Name == "" {
			if s, ok := t["name"].(string); ok {
				out.Name = strings.TrimSpace(s)
			}
		}
		for _, key := range []string{"@graph", "mainEntity", "about", "itemListElement"} {
			if sub, ok := t[key]; ok {
				walkLD(sub, out, depth+1)
			}
		}
	case []any:
		for _, e := range t {
			walkLD(e, out, depth+1)
		}
	}
}

// addTypes appends the @type token(s) of one node, deduped, preserving the
// order the page wrote them in — the first is the one the page led with.
func addTypes(v any, out *PageTypes) {
	var vals []string
	switch t := v.(type) {
	case string:
		vals = []string{t}
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				vals = append(vals, s)
			}
		}
	}
	for _, s := range vals {
		// schema.org types arrive both bare and as full URLs
		if i := strings.LastIndexAny(s, "/#"); i >= 0 {
			s = s[i+1:]
		}
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		for _, have := range out.JSONLD {
			if strings.EqualFold(have, s) {
				return
			}
		}
		if len(out.JSONLD) < probeMaxTypes {
			out.JSONLD = append(out.JSONLD, s)
		}
	}
}
