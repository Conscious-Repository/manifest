package consume

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// THE PODCAST DIRECTORY — Apple's unauthenticated lookup, used the way
// crossref.go uses the DOI registry: to turn a platform's own id for a piece
// into the CANONICAL address of that piece.
//
// A Spotify episode link, an Apple episode link and a YouTube watch link all
// name the same kind of thing — a podcast episode — in three private
// vocabularies, none of which a subscriber's client can play. The canonical
// name is the pair (feed URL, item guid), and Apple's directory is the one
// public index that maps a show or an episode to it without credentials, a
// bearer token or a paid tier. That is the whole reason this file exists and
// the whole of what it is allowed to do.
//
// ⚠ IT IS A DIRECTORY, NOT A SOURCE. Nothing Apple says about an episode ever
// reaches a curated note. What it supplies is a feedUrl and, at best, a guid;
// feedresolve.go then fetches THAT FEED and reads the episode out of the
// publisher's own XML. So a wrong or stale directory row costs a failed match,
// never a wrong enclosure — which is the property that makes an unauthenticated
// third-party index safe to consult at curate time.
//
// Every failure returns false. A show Apple has never indexed, a search that
// is ambiguous, a slow or down API: each of those falls straight back to the
// platform-metadata path that predates this file. Curating must never fail
// because a lookup did.

// appleAPI is the directory's base. Overridden in tests.
const appleAPI = "https://itunes.apple.com"

// appleTimeout bounds one lookup. It runs inside a curate click, so it is
// short: an honest platform entry is better than a spinner.
const appleTimeout = 10 * time.Second

// maxAppleBody bounds one response. An episode listing for a long-running show
// is the big case, and it is measured in hundreds of kilobytes.
const maxAppleBody = 8 << 20

// appleEpisodeLimit is how many of a show's episodes a listing asks for. Apple
// caps the parameter at 200; a show with more than that is matched by search
// or by the feed itself, never by paging the directory.
const appleEpisodeLimit = 200

// applePodcastHit is one directory row, narrowed to the fields that can name a
// canonical item.
type applePodcastHit struct {
	CollectionID   int64
	CollectionName string
	TrackName      string
	FeedURL        string
	EpisodeGUID    string
	EpisodeURL     string
	TrackViewURL   string
}

// appleResult is the directory's own row shape. Apple returns shows and
// episodes through the same endpoint and tells them apart with wrapperType.
type appleResult struct {
	WrapperType    string `json:"wrapperType"`
	Kind           string `json:"kind"`
	CollectionID   int64  `json:"collectionId"`
	CollectionName string `json:"collectionName"`
	TrackName      string `json:"trackName"`
	FeedURL        string `json:"feedUrl"`
	EpisodeGUID    string `json:"episodeGuid"`
	EpisodeURL     string `json:"episodeUrl"`
	TrackViewURL   string `json:"trackViewUrl"`
}

func (r appleResult) hit() applePodcastHit {
	return applePodcastHit{
		CollectionID:   r.CollectionID,
		CollectionName: strings.TrimSpace(r.CollectionName),
		TrackName:      strings.TrimSpace(r.TrackName),
		FeedURL:        strings.TrimSpace(r.FeedURL),
		EpisodeGUID:    strings.TrimSpace(r.EpisodeGUID),
		EpisodeURL:     strings.TrimSpace(r.EpisodeURL),
		TrackViewURL:   strings.TrimSpace(r.TrackViewURL),
	}
}

// isEpisode reports whether the row is an episode rather than the show it
// belongs to. A lookup for a show's episodes returns the show as its first row.
func (r appleResult) isEpisode() bool {
	return strings.EqualFold(r.WrapperType, "podcastEpisode") ||
		strings.EqualFold(r.Kind, "podcast-episode")
}

type appleResponse struct {
	ResultCount int           `json:"resultCount"`
	Results     []appleResult `json:"results"`
}

// appleBase is where the directory is asked.
func (s *Service) appleBase() string {
	if b := strings.TrimRight(strings.TrimSpace(s.apple), "/"); b != "" {
		return b
	}
	return appleAPI
}

// appleGet issues one directory request. It uses the ordinary client, not the
// guarded one: the host is a constant in this file rather than anything a
// pasted link chose, exactly as api.crossref.org is.
func (s *Service) appleGet(ctx context.Context, path string, q url.Values) []appleResult {
	ctx, cancel := context.WithTimeout(ctx, appleTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.appleBase()+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAppleBody))
	if err != nil {
		return nil
	}
	var doc appleResponse
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	return doc.Results
}

// appleLookupPodcast resolves a show's collection id to its feed URL.
func (s *Service) appleLookupPodcast(ctx context.Context, collectionID string) (applePodcastHit, bool) {
	if !isDigits(collectionID) {
		return applePodcastHit{}, false
	}
	for _, r := range s.appleGet(ctx, "/lookup", url.Values{
		"id":     {collectionID},
		"entity": {"podcast"},
	}) {
		if hit := r.hit(); hit.FeedURL != "" {
			return hit, true
		}
	}
	return applePodcastHit{}, false
}

// appleLookupEpisode resolves ONE episode's own track id — the `i=` an Apple
// Podcasts episode link carries — to its feed URL and guid.
func (s *Service) appleLookupEpisode(ctx context.Context, episodeID string) (applePodcastHit, bool) {
	if !isDigits(episodeID) {
		return applePodcastHit{}, false
	}
	for _, r := range s.appleGet(ctx, "/lookup", url.Values{
		"id":     {episodeID},
		"entity": {"podcastEpisode"},
	}) {
		if !r.isEpisode() {
			continue
		}
		if hit := r.hit(); hit.FeedURL != "" || hit.EpisodeGUID != "" {
			return hit, true
		}
	}
	return applePodcastHit{}, false
}

// appleShowEpisodes lists a show's episodes, for matching an Apple link that
// names the show but not the episode's own track id.
func (s *Service) appleShowEpisodes(ctx context.Context, collectionID string) []applePodcastHit {
	if !isDigits(collectionID) {
		return nil
	}
	var out []applePodcastHit
	for _, r := range s.appleGet(ctx, "/lookup", url.Values{
		"id":     {collectionID},
		"entity": {"podcastEpisode"},
		"limit":  {strconv.Itoa(appleEpisodeLimit)},
	}) {
		if r.isEpisode() {
			out = append(out, r.hit())
		}
	}
	return out
}

// appleSearchPodcastEpisode finds the canonical feed for an episode known only
// by its title — the Spotify case, where the platform states a title and
// nothing else a feed can be found by.
//
// ⚠ AMBIGUITY IS A REFUSAL. Episode titles repeat across shows ("Episode 12",
// "Introduction"), and picking the most popular of several matches would
// silently attach one show's audio to another show's note. So a title that
// matches rows from two different feeds resolves to nothing at all, and the
// pasted link stays the honest platform entry it was. `show` narrows the
// candidates when the platform said which show it was; it is a hint, never a
// requirement, because platforms name shows inconsistently.
func (s *Service) appleSearchPodcastEpisode(ctx context.Context, title, show string) (applePodcastHit, bool) {
	want := titleKey(title)
	if want == "" {
		return applePodcastHit{}, false
	}
	term := strings.TrimSpace(title)
	if show = strings.TrimSpace(show); show != "" {
		term += " " + show
	}
	rows := s.appleGet(ctx, "/search", url.Values{
		"term":   {term},
		"media":  {"podcast"},
		"entity": {"podcastEpisode"},
		"limit":  {"25"},
	})

	var exact []applePodcastHit
	for _, r := range rows {
		hit := r.hit()
		if hit.FeedURL == "" || titleKey(hit.TrackName) != want {
			continue
		}
		exact = append(exact, hit)
	}
	if len(exact) == 0 {
		return applePodcastHit{}, false
	}
	// The show hint, applied only if it actually narrows: a hint that matches
	// nothing is a hint we read wrong, not evidence against the results.
	if showKey := titleKey(show); showKey != "" {
		var narrowed []applePodcastHit
		for _, hit := range exact {
			if titleKey(hit.CollectionName) == showKey {
				narrowed = append(narrowed, hit)
			}
		}
		if len(narrowed) > 0 {
			exact = narrowed
		}
	}
	// Two feeds claiming the same episode title is the ambiguity this refuses.
	// Several rows from ONE feed are the same episode listed twice, which is
	// not ambiguous — prefer the row that carries a guid.
	best := exact[0]
	for _, hit := range exact[1:] {
		if !SameLink(hit.FeedURL, best.FeedURL) {
			return applePodcastHit{}, false
		}
		if best.EpisodeGUID == "" && hit.EpisodeGUID != "" {
			best = hit
		}
	}
	return best, true
}

// isDigits keeps a directory id to what an id can be. The value comes out of a
// pasted URL's path or query, and it is about to be a request parameter.
func isDigits(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 20 {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
