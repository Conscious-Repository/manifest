package consume

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PAPERS — the one kind of curated piece whose whole text is NOT what a
// subscriber should be handed.
//
// A blog post or an X post IS its text: mirroring it whole is the honest
// thing, and consume does that everywhere else. A journal article is not. Its
// canonical form lives at a DOI, its full text is typeset PDF that a readable-
// link fetch turns into a dump of navigation, ORCID markers and figure
// captions, and the convention every scholarly feed follows — Crossref's
// "Recommendations on RSS Feeds for Scholarly Publishers" — is abstract +
// citation + link, with the bibliographic fields exposed in Dublin Core and
// PRISM so a citation manager can read them.
//
// So this file answers one question — "is this a paper, and if so what is its
// metadata?" — from the canonical source rather than from the page:
//
//	the URL     →  a DOI, by extraction (doi.org, /doi/…, a raw 10.x/… , nature.com/articles/…)
//	the DOI     →  api.crossref.org/works/{doi}, no auth, the registry's own record
//
// The answer is used in exactly two places (curate.go's note builder and
// public.go's RSS fields) and is ALWAYS optional: a URL with no DOI, a DOI
// Crossref has never seen, a slow or down API — each of those falls straight
// back to the mirror-the-whole-piece path that predates this file. Curating
// must never fail because a metadata lookup did.

// crossrefAPI is the registry's REST endpoint. Overridden in tests.
const crossrefAPI = "https://api.crossref.org"

// crossrefTimeout bounds the lookup. It runs inside a curate click, so it is
// short: a paper published with its own excerpt is better than a spinner.
const crossrefTimeout = 10 * time.Second

// PaperMeta is one work's bibliographic record, as Crossref holds it. Every
// field is optional except DOI — registry records vary by publisher, and a
// missing abstract is common enough that it cannot mean "not a paper".
type PaperMeta struct {
	DOI       string
	Title     string
	Authors   []string
	Journal   string
	Publisher string
	Published string // YYYY-MM-DD, YYYY-MM or YYYY — whatever Crossref knows
	Abstract  string
}

// DOIURL is the citable form: the DOI as a resolvable link.
func (m PaperMeta) DOIURL() string {
	if m.DOI == "" {
		return ""
	}
	return "https://doi.org/" + m.DOI
}

// Year is the publication year alone, for the citation line.
func (m PaperMeta) Year() string {
	if len(m.Published) >= 4 {
		return m.Published[:4]
	}
	return ""
}

// doiPattern matches a DOI wherever it appears in a URL path. The prefix shape
// (10.registrant/suffix) is fixed by the DOI handbook; the suffix is publisher
// chosen, so it is bounded by what cannot appear in one — whitespace, quotes,
// and the URL delimiters that end a path.
var doiPattern = regexp.MustCompile(`10\.\d{4,9}/[^\s"'<>&?#]+`)

// natureArticle matches nature.com's article path. Springer Nature does not
// put the DOI in its URLs, but its article id IS the DOI suffix under the
// 10.1038 prefix — which is how the sub-millivolt paper (nature.com/articles/
// s41467-026-76758-z → 10.1038/s41467-026-76758-z) resolves at all.
var natureArticle = regexp.MustCompile(`(?i)^/articles/([a-z0-9._-]+)$`)

// doiTrailers are path segments publishers append to the DOI to name a
// RENDERING of the paper rather than the paper: Wiley's /full, ACS's /abs,
// bioRxiv's version and .full suffixes.
var doiTrailers = regexp.MustCompile(`(?i)(/(full|abstract|abs|pdf|epdf|meta|references|figures)|\.full(-text)?|v\d+)$`)

// DOIFromURL extracts the DOI a link points at, or "" when there is none.
//
// "" is the honest answer for everything that is not a registered work, and it
// is the answer that keeps a Substack essay or an X post on the full-text
// path: no DOI, not a paper. Exported because the server layer's retrofit and
// the tests both ask the same question.
func DOIFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// A bare DOI, pasted rather than linked.
	if strings.HasPrefix(raw, "10.") {
		return normalizeDOI(raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if m := natureArticle.FindStringSubmatch(u.Path); m != nil && strings.Contains(strings.ToLower(u.Host), "nature.com") {
		return normalizeDOI("10.1038/" + m[1])
	}
	// doi.org/… and dx.doi.org/… carry the DOI as the whole path; every other
	// publisher that carries one at all carries it somewhere inside the path.
	if m := doiPattern.FindString(u.Path); m != "" {
		return normalizeDOI(m)
	}
	return ""
}

// normalizeDOI trims what a URL adds around a DOI: a trailing slash, sentence
// punctuation, and the rendering suffixes above.
func normalizeDOI(doi string) string {
	doi = strings.TrimSpace(doi)
	doi = strings.TrimRight(doi, "/.,;:)]")
	for {
		trimmed := doiTrailers.ReplaceAllString(doi, "")
		if trimmed == doi {
			break
		}
		doi = trimmed
	}
	if !doiPattern.MatchString(doi) {
		return ""
	}
	return doi
}

// crossrefBase is where works are looked up.
func (s *Service) crossrefBase() string {
	if b := strings.TrimRight(strings.TrimSpace(s.crossref), "/"); b != "" {
		return b
	}
	return crossrefAPI
}

// paperFor answers "is the piece at this URL a paper, and what is its record?"
//
// It is the ONE decision point for content type. A false answer is always
// safe: the caller mirrors the whole piece exactly as it did before this file
// existed.
func (s *Service) paperFor(ctx context.Context, rawURL string) (PaperMeta, bool) {
	doi := DOIFromURL(rawURL)
	if doi == "" {
		return PaperMeta{}, false
	}
	return s.crossrefWork(ctx, doi)
}

// crossrefWork fetches one work record. Every failure — transport, status,
// malformed JSON — returns false, never an error: the caller has a fallback
// and no way to act on the difference between them.
func (s *Service) crossrefWork(ctx context.Context, doi string) (PaperMeta, bool) {
	if doi == "" {
		return PaperMeta{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, crossrefTimeout)
	defer cancel()

	// The DOI is a path segment, and its suffix may hold anything a publisher
	// chose — including the parentheses in old Elsevier DOIs.
	endpoint := s.crossrefBase() + "/works/" + strings.ReplaceAll(url.PathEscape(doi), "%2F", "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PaperMeta{}, false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		return PaperMeta{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PaperMeta{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return PaperMeta{}, false
	}
	var doc crossrefResponse
	if err := json.Unmarshal(raw, &doc); err != nil {
		return PaperMeta{}, false
	}
	if !strings.EqualFold(doc.Status, "ok") {
		return PaperMeta{}, false
	}
	return doc.Message.meta(doi)
}

// ---- the registry's JSON, narrowed to what a citation needs ----

type crossrefResponse struct {
	Status  string          `json:"status"`
	Message crossrefWorkMsg `json:"message"`
}

type crossrefWorkMsg struct {
	DOI            string           `json:"DOI"`
	Title          []string         `json:"title"`
	Author         []crossrefAuthor `json:"author"`
	ContainerTitle []string         `json:"container-title"`
	ShortContainer []string         `json:"short-container-title"`
	Publisher      string           `json:"publisher"`
	Abstract       string           `json:"abstract"`
	Issued         crossrefDate     `json:"issued"`
	Published      crossrefDate     `json:"published"`
	PublishedPrint crossrefDate     `json:"published-print"`
}

type crossrefAuthor struct {
	Given  string `json:"given"`
	Family string `json:"family"`
	Name   string `json:"name"` // consortia and group authors
}

type crossrefDate struct {
	DateParts [][]int `json:"date-parts"`
}

// maxAuthors bounds the citation. Papers with four hundred authors exist; a
// frontmatter list and a citation line are not where they belong.
const maxAuthors = 10

func (m crossrefWorkMsg) meta(doi string) (PaperMeta, bool) {
	out := PaperMeta{
		DOI:       firstNonEmpty(strings.TrimSpace(m.DOI), doi),
		Title:     collapseSpaces(html.UnescapeString(first(m.Title))),
		Journal:   collapseSpaces(html.UnescapeString(firstNonEmpty(first(m.ContainerTitle), first(m.ShortContainer)))),
		Publisher: collapseSpaces(html.UnescapeString(strings.TrimSpace(m.Publisher))),
		Published: firstNonEmpty(m.Published.date(), m.Issued.date(), m.PublishedPrint.date()),
		Abstract:  jatsText(m.Abstract),
	}
	for _, a := range m.Author {
		if len(out.Authors) >= maxAuthors {
			break
		}
		if n := authorName(a); n != "" {
			out.Authors = append(out.Authors, n)
		}
	}
	if out.DOI == "" {
		return PaperMeta{}, false
	}
	return out, true
}

// authorName renders one author as a person is named in prose.
//
// ⚠ No commas. The name lands in a frontmatter list, and the kernel's list
// form splits on commas with no quoting escape — "Ruhl, Philipp" would parse
// back as two authors.
func authorName(a crossrefAuthor) string {
	n := strings.TrimSpace(a.Given + " " + a.Family)
	if n == "" {
		n = strings.TrimSpace(a.Name)
	}
	n = collapseSpaces(html.UnescapeString(strings.ReplaceAll(n, ",", " ")))
	return n
}

// date renders Crossref's date-parts at whatever precision the record has.
func (d crossrefDate) date() string {
	if len(d.DateParts) == 0 || len(d.DateParts[0]) == 0 || d.DateParts[0][0] <= 0 {
		return ""
	}
	p := d.DateParts[0]
	out := strconv.Itoa(p[0])
	if len(p) > 1 && p[1] > 0 {
		out += "-" + two(p[1])
		if len(p) > 2 && p[2] > 0 {
			out += "-" + two(p[2])
		}
	}
	return out
}

func two(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func first(in []string) string {
	if len(in) == 0 {
		return ""
	}
	return strings.TrimSpace(in[0])
}

// ---- JATS ----

// jatsBlockEnd are the tags that end a paragraph of an abstract. Crossref
// stores abstracts as JATS XML, so the block structure has to be turned into
// blank lines BEFORE the tags are stripped, or a structured abstract collapses
// into one run-on paragraph.
var jatsBlockEnd = regexp.MustCompile(`(?i)</(jats:)?(p|title|sec|list-item|abstract)>`)

// jatsTag matches every remaining tag. Inline ones (<jats:italic>, <jats:sub>)
// are removed WITHOUT a space, so "V<jats:sub>m</jats:sub>" stays "Vm".
var jatsTag = regexp.MustCompile(`(?s)<[^>]*>`)

// jatsText renders a JATS abstract as plain paragraphs.
func jatsText(in string) string {
	if strings.TrimSpace(in) == "" {
		return ""
	}
	s := jatsBlockEnd.ReplaceAllString(in, "\n\n")
	s = jatsTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	var paras []string
	for _, p := range strings.Split(s, "\n\n") {
		if t := strings.Join(strings.Fields(p), " "); t != "" {
			paras = append(paras, t)
		}
	}
	// Publishers ship the word "Abstract" as the block's own <jats:title>. The
	// note renders its own heading; two would be one too many.
	if len(paras) > 0 && strings.EqualFold(strings.Trim(paras[0], " :"), "abstract") {
		paras = paras[1:]
	}
	return strings.Join(paras, "\n\n")
}
