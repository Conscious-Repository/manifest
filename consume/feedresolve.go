package consume

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// CANONICAL FEED RESOLUTION — a platform link is not the piece.
//
// open.spotify.com/episode/5vOrCLPaezy61FYE417CTp is Spotify's name for
// something Spotify did not make. The episode itself is an mp3 named by an
// <enclosure> in the publisher's own RSS, with show notes, a duration and a
// guid beside it; the platform page has a title, a thumbnail, an iframe and —
// on Spotify — a sixty-second PREVIEW CLIP that looks exactly like an episode
// file and is not one.
//
// So before the metadata ladder's answer is accepted, this file tries to find
// the FEED ITEM the pasted link is pointing at, in the order the answer can be
// trusted:
//
//	1. a feed the owner already subscribes to   his own list, no directory, no guess
//	2. the podcast directory                     Apple, unauthenticated (applepodcasts.go)
//	3. the page's own <link rel=alternate>       what the publisher advertises
//
// Then it FETCHES THAT FEED and matches one item in it by guid, by enclosure
// URL, by link, by video id, and — last and only when the match is unique —
// by normalized title. Whatever it returns came out of the publisher's XML.
//
// ⚠ WHAT THIS FILE MAY NOT DO. It writes nothing: the result is an enrichment
// of the LinkMeta CurateURL already holds, and the vault write stays where it
// was. It never invents an enclosure — no match, no audio, and the pasted link
// remains the honest platform entry it was. It never fails a curate: every
// path here returns (zero, false) and the caller carries on.

// canonicalRef is one pasted link resolved to a real feed item.
type canonicalRef struct {
	// FeedURL is the feed the item was read out of — the canonical foreign key
	// a platform id is not.
	FeedURL string
	// Source is the feed's channel title: the SHOW, which is what the note and
	// the public feed should attribute to, rather than the platform that
	// happened to be linked.
	Source string
	Item   Item
}

// the platform shapes this file knows how to look a feed up for.
const (
	hintSpotifyEpisode = "spotify-episode"
	hintApplePodcast   = "apple-podcast"
	hintYouTubeVideo   = "youtube-video"
	hintPageFeed       = "page-feed"
)

// feedHint is what a pasted platform URL and its metadata say about the piece,
// translated into the only vocabulary a feed can be searched with.
type feedHint struct {
	kind string
	// title is the episode's own title, as the platform states it.
	title string
	// show is the podcast/channel the platform says it belongs to, when it
	// says. A hint for disambiguation, never a requirement.
	show string
	// The platform's own ids, where the URL states them.
	videoID      string
	collectionID string
	episodeID    string
	// feedHref is a feed the PAGE advertised (rss.go's autodiscovery shape,
	// read out of the same head scan the metadata ladder already made).
	feedHref string
}

// maxCanonicalFeeds bounds how many feeds one paste may fetch. Resolution is a
// best effort inside a click, not a crawl.
const maxCanonicalFeeds = 3

// resolveCanonicalFeedItem answers "which feed item is this pasted link?",
// or false when it cannot say so confidently.
func (s *Service) resolveCanonicalFeedItem(ctx context.Context, pageURL string, m LinkMeta) (canonicalRef, bool) {
	hint := feedHintFor(pageURL, m)
	if hint.kind == "" {
		return canonicalRef{}, false
	}

	// 1 — the owner's own list first. A show he already subscribes to needs no
	// directory and cannot be confused with a different show of the same name.
	if ref, ok := s.matchSubscribedFeed(ctx, hint); ok {
		return ref, true
	}

	// 2/3 — the candidate feeds an outside index or the page itself names.
	for _, cand := range s.canonicalCandidates(ctx, hint) {
		if ref, ok := s.matchFeedURL(ctx, cand, hint, s.curateClient()); ok {
			return ref, true
		}
	}
	return canonicalRef{}, false
}

// candidate is one feed worth fetching, plus whatever the source that named it
// also knew about which item inside it to take.
type candidate struct {
	feedURL string
	guid    string
	audio   string
}

// canonicalCandidates names the feeds to try, most authoritative first.
func (s *Service) canonicalCandidates(ctx context.Context, hint feedHint) []candidate {
	var out []candidate
	add := func(c candidate) {
		if c.feedURL == "" || len(out) >= maxCanonicalFeeds {
			return
		}
		for _, have := range out {
			if SameLink(have.feedURL, c.feedURL) {
				return
			}
		}
		out = append(out, c)
	}

	switch hint.kind {
	case hintApplePodcast:
		// The episode's own track id is the precise question, and its answer
		// carries the guid — so the feed is matched on identity rather than on
		// a title.
		if hit, ok := s.appleLookupEpisode(ctx, hint.episodeID); ok {
			add(candidate{feedURL: hit.FeedURL, guid: hit.EpisodeGUID, audio: hit.EpisodeURL})
		}
		if hit, ok := s.appleLookupPodcast(ctx, hint.collectionID); ok {
			add(candidate{feedURL: hit.FeedURL})
		}
		// A show link with no episode id still resolves the show; the title
		// rung below is what picks the item, and only if it is unique.
		for _, hit := range s.appleShowEpisodes(ctx, hint.collectionID) {
			if titleKey(hit.TrackName) == titleKey(hint.title) {
				add(candidate{feedURL: hit.FeedURL, guid: hit.EpisodeGUID, audio: hit.EpisodeURL})
			}
		}
	case hintSpotifyEpisode:
		// Spotify states a title and nothing a feed can be found by. The
		// directory turns that title into a feed — or refuses, if the title
		// belongs to more than one show (applepodcasts.go).
		if hit, ok := s.appleSearchPodcastEpisode(ctx, hint.title, hint.show); ok {
			add(candidate{feedURL: hit.FeedURL, guid: hit.EpisodeGUID, audio: hit.EpisodeURL})
		}
	}
	// What the page itself advertised, for everything else — the same
	// <link rel="alternate"> subscribing already follows.
	add(candidate{feedURL: hint.feedHref})
	return out
}

// matchSubscribedFeed looks for the piece in a feed the owner already follows.
//
// The cached items are the INDEX, not the answer: they narrow twenty feeds to
// one without a single request, and the feed is then fetched and read
// properly, so a cache written before the lane knew about enclosures cannot
// cost the note its audio. Two subscriptions matching is an ambiguity and
// resolves to nothing.
func (s *Service) matchSubscribedFeed(ctx context.Context, hint feedHint) (canonicalRef, bool) {
	want := titleKey(hint.title)
	var found Subscription
	for _, sub := range s.Subscriptions() {
		if sub.Kind != KindRSS || strings.TrimSpace(sub.URL) == "" {
			continue
		}
		hit := false
		for _, it := range s.store.Items(sub.ID) {
			if hintNamesItem(hint, want, it) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if found.ID != "" && found.ID != sub.ID {
			return canonicalRef{}, false // two of his feeds claim it
		}
		found = sub
	}
	if found.ID == "" {
		return canonicalRef{}, false
	}
	// A subscription URL is the owner's own choice and is already polled with
	// the ordinary client — including the loopback RSSHub bridge, which the
	// pasted-link guard exists to refuse.
	return s.matchFeedURL(ctx, candidate{feedURL: found.URL}, hint, s.hc)
}

// hintNamesItem is the cheap cache-side test: does this cached item look like
// the thing the pasted link named?
func hintNamesItem(hint feedHint, wantTitle string, it Item) bool {
	if hint.videoID != "" && strings.Contains(it.URL, hint.videoID) {
		return true
	}
	return wantTitle != "" && titleKey(it.Title) == wantTitle
}

// matchFeedURL fetches one feed and reads the pasted link's item out of it.
func (s *Service) matchFeedURL(ctx context.Context, c candidate, hint feedHint, hc *http.Client) (canonicalRef, bool) {
	feed, ok := s.fetchCanonicalFeed(ctx, c.feedURL, hc)
	if !ok {
		return canonicalRef{}, false
	}
	xi, ok := matchFeedItem(feed, c, hint)
	if !ok {
		return canonicalRef{}, false
	}
	title := feed.title()
	sub := Subscription{ID: "external", Kind: KindRSS, Title: title, Mirror: MirrorFull}
	it, ok := itemFrom(xi, sub, title, time.Now().UTC())
	if !ok {
		return canonicalRef{}, false
	}
	return canonicalRef{FeedURL: c.feedURL, Source: title, Item: it}, true
}

// fetchCanonicalFeed reads one feed. hc decides which client: a feed named by
// the OWNER's subscription list is fetched with the ordinary client, a feed
// named by an outside directory or by a pasted page with the guarded one —
// that URL is as arbitrary as the link it came from (fetchguard.go).
func (s *Service) fetchCanonicalFeed(ctx context.Context, feedURL string, hc *http.Client) (*xmlFeed, bool) {
	if externalURL(feedURL) == "" {
		return nil, false
	}
	if hc == s.curateClient() {
		if _, err := s.guardPasted(feedURL); err != nil {
			return nil, false
		}
	}
	ctx, cancel := context.WithTimeout(ctx, guardTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	feed, err := decodeFeed(resp.Body)
	if err != nil {
		return nil, false
	}
	return feed, true
}

// matchFeedItem picks the one item a pasted link named, or nothing.
//
// The rungs are ordered by how much they PROVE. A guid or an enclosure URL is
// an identity: two things carrying it are the same episode. A link is nearly
// as good. A title is neither — shows reuse titles and so do episodes within a
// show — so it is accepted only when exactly one item in the feed carries it,
// which is what stops "Episode 12" attaching to the wrong hour of audio.
func matchFeedItem(feed *xmlFeed, c candidate, hint feedHint) (xmlItem, bool) {
	items := feed.items()

	if guid := strings.TrimSpace(c.guid); guid != "" {
		for _, xi := range items {
			if strings.EqualFold(strings.TrimSpace(xi.GUID), guid) ||
				strings.EqualFold(strings.TrimSpace(xi.ID), guid) {
				return xi, true
			}
		}
	}
	if audio := strings.TrimSpace(c.audio); audio != "" {
		for _, xi := range items {
			if enc, ok := audioEnclosure(xi.Enclosures); ok && SameLink(enc.URL, audio) {
				return xi, true
			}
		}
	}
	if hint.videoID != "" {
		for _, xi := range items {
			if strings.TrimSpace(xi.VideoID) == hint.videoID {
				return xi, true
			}
			if link := bestLink(xi.Links); link != "" && strings.Contains(link, hint.videoID) {
				return xi, true
			}
		}
	}
	// The last rung, and the only one that can be wrong: a title must be the
	// feed's ONLY item with that title to count.
	if want := titleKey(hint.title); want != "" {
		var hit xmlItem
		n := 0
		for _, xi := range items {
			if titleKey(xi.Title) == want {
				hit, n = xi, n+1
			}
		}
		if n == 1 {
			return hit, true
		}
	}
	return xmlItem{}, false
}

// ---- reading the hint off a pasted link ----

// spotifyEpisodePath is one episode's address on Spotify. Only an EPISODE is
// resolved: a show, a track or a playlist is not a piece with a feed item.
var spotifyEpisodePath = regexp.MustCompile(`^/(?:intl-[a-z-]+/)?episode/([A-Za-z0-9]+)`)

// applePodcastID matches the collection id Apple hangs off a show or episode
// path: /us/podcast/<slug>/id1376929139.
var applePodcastID = regexp.MustCompile(`/id(\d+)`)

// feedHintFor translates a pasted URL and what the ladder read off it into the
// question a feed can be asked.
func feedHintFor(pageURL string, m LinkMeta) feedHint {
	hint := feedHint{title: strings.TrimSpace(m.Title), feedHref: strings.TrimSpace(m.feedHref)}
	u, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || u.Host == "" {
		return feedHint{}
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")

	switch {
	case host == "open.spotify.com" || strings.HasSuffix(host, ".spotify.com"):
		if !spotifyEpisodePath.MatchString(u.Path) {
			return feedHint{}
		}
		hint.kind = hintSpotifyEpisode
		hint.show = spotifyShow(m)
	case host == "podcasts.apple.com" || host == "music.apple.com":
		mm := applePodcastID.FindStringSubmatch(u.Path)
		if mm == nil {
			return feedHint{}
		}
		hint.kind = hintApplePodcast
		hint.collectionID = mm[1]
		if i := strings.TrimSpace(u.Query().Get("i")); isDigits(i) {
			hint.episodeID = i
		}
	case host == "youtube.com" || host == "m.youtube.com" || host == "music.youtube.com" || host == "youtu.be":
		id := strings.TrimPrefix(youtubeEmbed(pageURL), "youtube:video:")
		if id == "" || id == youtubeEmbed(pageURL) {
			return feedHint{}
		}
		hint.kind = hintYouTubeVideo
		hint.videoID = id
	case hint.feedHref != "":
		// Not a platform we know — but the page advertised a feed, and the
		// piece may be in it whole where the page gave only a teaser.
		hint.kind = hintPageFeed
	}
	if hint.title == "" && hint.videoID == "" {
		return feedHint{} // nothing to search a feed with
	}
	return hint
}

// spotifyShow reads the show name out of what Spotify's page says about
// itself: og:description on an episode page is "<Show> · Episode" (or the
// localized equivalent), and the part before the separator is the show.
//
// It is a HINT and nothing rides on it: a wrong reading narrows nothing (see
// appleSearchPodcastEpisode), and og:site_name — "Spotify" — is the platform,
// not the publisher, which is exactly the confusion this whole file exists to
// undo.
func spotifyShow(m LinkMeta) string {
	desc := strings.TrimSpace(m.Description)
	if desc == "" {
		return ""
	}
	for _, sep := range []string{" · ", " • ", " | "} {
		if head, _, ok := strings.Cut(desc, sep); ok {
			return strings.TrimSpace(head)
		}
	}
	return ""
}

// titleKey normalizes a title for comparison across a platform, a directory
// and a feed — three places that disagree about punctuation, casing and
// whitespace for the same episode. "Did Jung and Tolkien visit the same
// psychic realms???" and the feed's own spelling of it collapse to one string.
//
// Letters and digits survive; everything else becomes a space. That is
// deliberately blunt: it is only ever used for EQUALITY, and only where a
// unique match is required anyway.
func titleKey(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return collapseSpaces(b.String())
}

// ---- applying the answer ----

// applyCanonical replaces what the platform said with what the publisher's
// feed says.
//
// It overwrites rather than fills in, and the media fields are overwritten
// WHOLESALE — including to empty. That is the point: the platform's audio, if
// it stated any, is a preview clip or a player, and a canonical item that
// carries no enclosure is a canonical item with no enclosure. The one thing
// kept from the platform is Embed, which is private-side only (the reader
// builds a player from it) and is the one thing the feed cannot say.
func (m *LinkMeta) applyCanonical(c canonicalRef) {
	it := c.Item
	m.Title = firstNonEmpty(it.Title, m.Title)
	m.Source = firstNonEmpty(it.Source, c.Source, m.Source)
	m.Author = firstNonEmpty(it.Author, m.Author)
	if !it.PublishedAt.IsZero() {
		m.Published = it.PublishedAt
	}
	if it.Excerpt != "" {
		m.Description = it.Excerpt
	}
	m.body = it.Body
	m.Audio, m.AudioType, m.AudioBytes = it.Audio, it.AudioType, it.AudioBytes
	m.Duration, m.Episode, m.Season = it.Duration, it.Episode, it.Season
	if it.Image != "" {
		m.Image = it.Image
	}
	if it.Podcast() {
		m.Kind = linkEpisode
	}
}
