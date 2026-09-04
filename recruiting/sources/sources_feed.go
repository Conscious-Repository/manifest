package sources

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FEED — a show or a blog, and the people it names (intake plan §5 stage 2,
// owner's Q2: RSS yes, X never).
//
// A podcast or a blog is a standing publication: the seed class is `media`,
// its episodes are `work`, and its feed is the whole interface. This adapter
// reads one feed and does two separate jobs, kept separate on purpose:
//
//	Preview  names the SHOW — title, site, description, how many episodes and
//	         the latest — so the intake scaffold can say what the thing is
//	         before anything is written. This is the deterministic fill.
//	Search   names the PEOPLE the episodes credit, and only where the credit
//	         is EXPLICIT: "with Jane Ng", "guest: Jane Ng", "featuring …",
//	         "interview with …". Low recall by design. A show whose titles are
//	         prose yields nobody, and that is the honest answer — reading a
//	         description and understanding who was on it is the model's job
//	         (stage C), not a regex's.
//
// Every draft cites the EPISODE it came from, verbatim, so an accepted person
// carries the sentence that named them. Trust is Low: an episode title is a
// marketing line, not a registry record.
type Feed struct {
	// BaseURL is unused for feeds (the scope carries a whole URL); kept for
	// symmetry with the other adapters' test injection.
	BaseURL string
	Client  http.Client
}

var _ Adapter = Feed{}

const (
	feedUserAgent   = "manifest-aion-recruiting/intake"
	feedTimeout     = 25 * time.Second
	feedMaxBody     = 8 << 20
	feedDefaultMax  = 25
	feedMaxEpisodes = 60
	feedSnippetMax  = 400
)

func (Feed) ID() string { return "feed" }
func (Feed) Kind() Kind { return KindWeb }
func (Feed) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "feed_url", Label: "feed URL", Placeholder: "the show or blog's RSS", Required: true},
		{Key: "query", Label: "only episodes mentioning", Placeholder: "optional filter"},
		{Key: "max", Label: "max results", Placeholder: strconv.Itoa(feedDefaultMax)},
	}
}

// ---- the wire shapes: RSS 2.0 and Atom, which is all a podcast or blog is ----

type feedDoc struct {
	XMLName xml.Name    `xml:"-"`
	Channel feedChannel `xml:"channel"`
	// Atom
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle"`
	Links    []atomLink  `xml:"link"`
	Entries  []feedEntry `xml:"entry"`
}

type feedChannel struct {
	Title       string      `xml:"title"`
	Link        string      `xml:"link"`
	Description string      `xml:"description"`
	Author      string      `xml:"author"`
	Items       []feedEntry `xml:"item"`
}

// atomLink covers both dialects at once: RSS spells a link as element TEXT,
// Atom as an href attribute, and encoding/xml refuses two fields sharing a
// tag — so one type reads whichever the feed used.
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Text string `xml:",chardata"`
}

type feedEntry struct {
	Title       string     `xml:"title"`
	Links       []atomLink `xml:"link"`
	Description string     `xml:"description"`
	Summary     string     `xml:"summary"`
	Content     string     `xml:"encoded"` // content:encoded
	PubDate     string     `xml:"pubDate"`
	Published   string     `xml:"published"`
	Updated     string     `xml:"updated"`
}

func (e feedEntry) link() string {
	for _, l := range e.Links {
		if l.Rel != "" && l.Rel != "alternate" {
			continue
		}
		if h := strings.TrimSpace(l.Href); h != "" {
			return h
		}
		if t := strings.TrimSpace(l.Text); t != "" {
			return t
		}
	}
	return ""
}

func (e feedEntry) text() string {
	return strings.TrimSpace(strings.Join(nonEmptyStr(e.Description, e.Summary, e.Content), " "))
}

func (e feedEntry) when() string {
	return strings.TrimSpace(orStr(e.PubDate, orStr(e.Published, e.Updated)))
}

// show is the feed's own identity, whichever dialect it speaks.
func (d feedDoc) show() (title, link, desc string) {
	title = strings.TrimSpace(orStr(d.Channel.Title, d.Title))
	link = strings.TrimSpace(d.Channel.Link)
	if link == "" {
		for _, l := range d.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = strings.TrimSpace(orStr(l.Href, l.Text))
				break
			}
		}
	}
	desc = strings.TrimSpace(orStr(d.Channel.Description, d.Subtitle))
	return title, link, desc
}

func (d feedDoc) entries() []feedEntry {
	if len(d.Channel.Items) > 0 {
		return d.Channel.Items
	}
	return d.Entries
}

// ---- fetch ----

func (f Feed) fetch(ctx context.Context, feedURL string) (feedDoc, error) {
	var doc feedDoc
	u := strings.TrimSpace(feedURL)
	if u == "" {
		return doc, errors.New("feed: name a feed URL")
	}
	if !strings.HasPrefix(strings.ToLower(u), "http://") && !strings.HasPrefix(strings.ToLower(u), "https://") {
		return doc, fmt.Errorf("feed: %q is not an http(s) URL", feedURL)
	}
	if f.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, feedTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return doc, fmt.Errorf("feed: %v", err)
	}
	req.Header.Set("User-Agent", feedUserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.8")
	resp, err := f.Client.Do(req)
	if err != nil {
		return doc, fmt.Errorf("feed: GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, feedMaxBody))
	if err != nil {
		return doc, fmt.Errorf("feed: reading %s: %v", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return doc, fmt.Errorf("feed: GET %s returned HTTP %d", u, resp.StatusCode)
	}
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(&doc); err != nil {
		return doc, fmt.Errorf("feed: %s is not RSS or Atom: %v", u, err)
	}
	if title, _, _ := doc.show(); title == "" && len(doc.entries()) == 0 {
		return doc, fmt.Errorf("feed: %s carries no show and no episodes", u)
	}
	return doc, nil
}

// Preview names the show. It fills the scaffold; it claims nothing about
// people.
func (f Feed) Preview(ctx context.Context, ref string) (PreviewFacts, error) {
	out := PreviewFacts{Ref: strings.TrimSpace(ref), Kind: "media"}
	doc, err := f.fetch(ctx, ref)
	if err != nil {
		return out, err
	}
	title, link, desc := doc.show()
	entries := doc.entries()
	out.Name = title
	out.URL = orStr(link, strings.TrimSpace(ref))
	out.Feed = strings.TrimSpace(ref)
	out.Note = clip(stripTags(desc), feedSnippetMax)
	out.Total = len(entries)
	if link != "" {
		out.Links = append(out.Links, link)
	}
	out.fact("name", title, "feed", out.Feed)
	out.fact("site", link, "feed", out.Feed)
	out.fact("episodes", strconv.Itoa(len(entries)), "feed", out.Feed)
	if len(entries) > 0 {
		out.fact("latest", clip(stripTags(entries[0].Title), 120), "feed", entries[0].link())
	}
	return out, nil
}

// ---- credits ----

// feedCreditRe matches the ways an episode says outright who was on it. The
// name must be Capitalised Words — two or three of them — because a lowercase
// run after "with" is a sentence, not a person ("with less noise").
var feedCreditRe = regexp.MustCompile(
	`\b(?i:guests?|featuring|feat\.?|interview with|in conversation with|talks? (?:to|with)|joined by|with)\s*:?\s+` +
		`([A-Z][\p{L}'\x{2019}-]+(?:\s+(?:van|von|de|del|da|di|bin|al)\b)?(?:\s+[A-Z][\p{L}'\x{2019}.-]*){1,2})`)

// feedStopNames are the capitalised things that follow "with" and are not
// people. Small on purpose: the rule that does the work is the shape of a
// name, not a blocklist.
var feedStopNames = map[string]bool{
	"The": true, "A": true, "An": true, "And": true, "But": true, "Our": true,
	"New": true, "How": true, "Why": true, "What": true, "This": true, "That": true,
}

// creditsIn returns the people an episode credits explicitly, in order.
func creditsIn(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range feedCreditRe.FindAllStringSubmatch(text, -1) {
		name := strings.Trim(strings.Join(strings.Fields(m[1]), " "), ".,;:!? ")
		first := strings.SplitN(name, " ", 2)[0]
		if feedStopNames[first] || len(strings.Fields(name)) < 2 {
			continue
		}
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	return out
}

// Search reads one feed and drafts the people its episodes credit outright.
func (f Feed) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	feedURL := strings.TrimSpace(s.Fields["feed_url"])
	if feedURL == "" {
		return nil, errors.New("feed: a run needs the feed URL")
	}
	doc, err := f.fetch(ctx, feedURL)
	if err != nil {
		return nil, err
	}
	max := s.Max
	if max <= 0 {
		max = feedDefaultMax
	}
	show, _, _ := doc.show()
	filter := strings.ToLower(strings.TrimSpace(s.Query))
	retrieved := time.Now().UTC()

	out := []CandidateDraft{}
	byName := map[string]int{}
	for i, e := range doc.entries() {
		if i >= feedMaxEpisodes {
			break
		}
		title := clip(stripTags(e.Title), 200)
		body := stripTags(e.text())
		hay := title + " — " + body
		if filter != "" && !strings.Contains(strings.ToLower(hay), filter) {
			continue
		}
		link := e.link()
		if link == "" {
			link = feedURL
		}
		for _, name := range creditsIn(hay) {
			ev := Evidence{
				SourceID: f.ID(), URLOrFile: link, RetrievedAt: retrieved,
				Snippet: clip(strings.TrimSpace(show+" — "+title+" · "+body), feedSnippetMax),
				Kind:    EvidencePage, Trust: TrustLow,
			}
			if j, seen := byName[strings.ToLower(name)]; seen {
				out[j].Evidence = append(out[j].Evidence, ev)
				continue
			}
			if len(out) >= max {
				continue
			}
			byName[strings.ToLower(name)] = len(out)
			out = append(out, CandidateDraft{
				SourceID: f.ID(),
				Name:     name,
				Role:     strings.TrimSpace(s.Role),
				Note:     "credited on " + show + ": " + title,
				Links:    []string{link},
				Evidence: []Evidence{ev},
			})
		}
	}
	return out, nil
}

// Enrich is a no-op: the feed said everything it says in one fetch.
func (Feed) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges is empty by design. Appearing on the same show is not a
// relationship — a host interviews strangers, which is the point of the
// format — and the edge vocabulary has no kind that would honestly describe
// it. Adding one would be a written decision, not a convenience.
func (Feed) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// ---- small helpers ----

var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)
var wsRe = regexp.MustCompile(`\s+`)

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&apos;", "'", "&nbsp;", " ").Replace(s)
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func nonEmptyStr(in ...string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
