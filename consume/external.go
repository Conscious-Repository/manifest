package consume

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

// THE BRIDGE — curating something the lane never polled.
//
// FEED cards (a domain-scout paper, an options-scout company) are research
// findings, not subscriptions: they live in the harness feed tree and have no
// Item, no Subscription and no snapshot on this side. But "I want subscribers
// to read this" is the same verb whichever surface the piece was noticed on,
// and the answer has to be the same artifact — one note under extrinsic/ with
// `categories: [articles]` and a `curated:` date — or the public feed would
// need a second source and public.go's isolation argument would need a second
// proof.
//
// So this file synthesizes exactly enough of a polled item to hand to
// writeCurated, and adds nothing to the write path. What it does own is the
// fetch: the referring card carries a link and a sentence, and the sentence is
// not the piece. One readable-link fetch turns the link into the article; when
// that fails the sentence is published as an excerpt rather than nothing, and
// the mirror field says so.

// externalPrefix namespaces a bridged item's id. It keeps a card id from ever
// colliding with a polled item's id — and, since the snapshot cache is keyed
// by that id, keeps Entries() from resolving somebody else's body into this
// note.
const externalPrefix = "ext-"

// ExternalItemID is the curated identity of a piece bridged in from another
// surface, derived from that surface's own id. Exported because un-curating
// happens from the referring surface, which knows only its own id.
func ExternalItemID(id string) string {
	return externalPrefix + strings.TrimSpace(id)
}

// ExternalRef is a piece of writing noticed somewhere other than the lane.
// Fallback is the referring surface's own words about it (a feed card's `why`)
// — used as the body only when the article itself cannot be fetched.
type ExternalRef struct {
	ID          string
	Title       string
	URL         string
	Source      string
	Author      string
	Fallback    string
	PublishedAt time.Time

	// Optional media, set only when a REAL media file URL was found (see
	// linkmeta.go's gate). Every existing caller leaves them zero, so
	// Item.Podcast() is false and episodeFields returns nil — a FEED card's
	// note is byte-identical to the note it produced before these existed.
	Audio      string
	AudioType  string
	AudioBytes int64
	Duration   int
	Episode    int
	Season     int
	Image      string
	// Embed is the allowlisted `provider:kind:id` descriptor for a platform
	// link. It reaches the note's frontmatter and the private reader; the
	// public feed has no vocabulary for it and emits nothing.
	Embed string

	// ---- set only inside this package ----

	// itemID overrides the derived id. Re-curating a URL that is ALREADY a
	// curated note must land on that note, and identity is what notePath
	// compares; without this, a piece first curated from the lane would fork
	// into a second note the first time it was curated from a pasted link.
	itemID string
	// body is a readable article the caller already fetched. Curating a
	// pasted link reads the page once for its metadata; re-fetching it here
	// would be a second request for bytes we are holding.
	body string
	// paper is a Crossref record the caller already resolved, so the registry
	// is asked once per curate rather than twice.
	paper *PaperMeta
}

// CurateExternal fetches the piece at ref.URL and curates it.
//
// It never hard-fails on the fetch. A curated note whose body is the referring
// card's sentence, stamped `mirror: excerpt`, is an honest record of something
// the owner chose to point at; refusing to curate because a publisher answered
// 403 would lose the choice as well as the article.
func (s *Service) CurateExternal(ctx context.Context, ref ExternalRef, note string) (CuratedEntry, error) {
	if s.writeVault == nil {
		return CuratedEntry{}, errNoCurateCapability
	}
	if strings.TrimSpace(ref.Title) == "" && externalURL(ref.URL) == "" {
		return CuratedEntry{}, errors.New("consume: nothing to curate")
	}
	it, sub := ref.item(), ref.subscription()
	paper, isPaper := ref.paper, ref.paper != nil
	// A PAPER is looked up, not scraped. A journal article's page is a wrapper
	// around a PDF — fetching it readable-style yields navigation, ORCID
	// markers and figure captions, which is what the ultrasound and
	// sub-millivolt notes carried before this branch existed. The registry has
	// the abstract and the citation, which is what a scholarly feed carries.
	// No DOI, or no record for it: not a paper, and the fetch below runs.
	if !isPaper {
		if got, ok := s.paperFor(ctx, it.URL); ok {
			paper, isPaper = &got, true
		}
	}
	if isPaper {
		it.Excerpt = collapseSpaces(strings.TrimSpace(ref.Fallback))
		return s.writeCurated(it, sub, note, paper)
	}
	if body, ok := s.fetchExternal(ctx, it.URL, ref); ok {
		text := Text(body)
		it.Body = body
		it.Chars = len([]rune(text))
		it.Excerpt = Excerpt(text, 280)
	} else {
		// Nothing whole to mirror. Preview is the field that says so, and
		// mirrorFor reads it — the public feed then carries the link and the
		// note, which is what a linkblog entry always was.
		it.Preview = "partial"
		it.Excerpt = collapseSpaces(strings.TrimSpace(ref.Fallback))
	}
	return s.writeCurated(it, sub, note, nil)
}

// item is the synthetic polled item. It is never stored in the lane's cache —
// the piece was not subscribed to, and pretending otherwise would put a
// research finding in the reading queue.
func (r ExternalRef) item() Item {
	id := ExternalItemID(r.ID)
	if r.itemID != "" {
		id = r.itemID
	}
	return Item{
		ID:          id,
		Source:      firstNonEmpty(strings.TrimSpace(r.Source), hostOf(r.URL)),
		Author:      strings.TrimSpace(r.Author),
		Title:       strings.TrimSpace(r.Title),
		URL:         externalURL(r.URL),
		PublishedAt: r.PublishedAt,
		Audio:       strings.TrimSpace(r.Audio),
		AudioType:   strings.TrimSpace(r.AudioType),
		AudioBytes:  r.AudioBytes,
		Duration:    r.Duration,
		Episode:     r.Episode,
		Season:      r.Season,
		Image:       strings.TrimSpace(r.Image),
		Embed:       strings.TrimSpace(r.Embed),
	}
}

// subscription is the synthetic source. Mirror is full because the owner
// pressed curate on this one piece — there is no publisher-wide setting to
// consult, and mirrorFor still downgrades it when the fetch came back empty.
func (r ExternalRef) subscription() Subscription {
	return Subscription{
		ID:     "external",
		Kind:   KindRSS,
		Title:  firstNonEmpty(strings.TrimSpace(r.Source), hostOf(r.URL), "external"),
		Mirror: MirrorFull,
	}
}

// fetchExternal is the readable-link fetch, held to the same honesty rules
// captureFull applies: a subscribe box is not the article, and a page that
// still ends in a truncation marker is still a preview.
func (s *Service) fetchExternal(ctx context.Context, pageURL string, ref ExternalRef) (string, bool) {
	if pageURL == "" {
		return "", false
	}
	body := ref.body
	if body == "" {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		body, _ = s.fetchArticle(ctx, pageURL, s.cookieFor(pageURL))
	}
	text := Text(body)
	if body == "" || looksPaywalled(text, "") || LooksTruncated(text) {
		return "", false
	}
	return body, true
}

// externalURL keeps only a real web address. A card can carry a
// harness-relative reference (artifacts/library/…) where its link would be,
// and publishing that as <link> would point subscribers at nothing.
func externalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return raw
	}
	return ""
}

// hostOf names the publisher when the referring surface didn't.
func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
}
