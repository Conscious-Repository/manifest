package consume

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RECOVERING ONE X POST'S WHOLE TEXT.
//
// X's oEmbed endpoint is the only source for a pasted post that needs no
// token, and it TRUNCATES: past roughly a screenful the blockquote ends in an
// ellipsis and the rest of the post is simply not in the answer. A note
// written from it is a preview of something the owner chose to amplify —
// while the same post curated from the lane is whole, because RSSHub's
// /twitter/user/ route carries the entire text in <description>.
//
// One post, two entrances, two different bodies is not a convention; it is a
// bug with a plausible excuse. So a pasted status asks the bridge FIRST, and
// falls back to oEmbed only when the bridge cannot produce that post.
//
// Three properties keep this from becoming a second feed source:
//
//   - it is asked ONLY for a canonical X status URL (IsXStatusURL), so an
//     ordinary pasted link makes no extra request;
//   - it goes out on s.hc, the lane's own client, because RSSHub is local
//     trusted infrastructure this package already dials — the pasted-link
//     guard is untouched and still refuses to let a PASTED address reach
//     loopback (fetchguard.go);
//   - it returns an ordinary Item through itemFrom, which is the same
//     projection an RSSHub subscription's poll produces. Nothing downstream
//     can tell which entrance the post came in through, which is the point.

// xRecoverTimeout bounds the bridge lookup. RSSHub answers a timeline in well
// under this; a curate must not hang on it either way, because oEmbed is
// waiting behind it.
const xRecoverTimeout = 15 * time.Second

// recoverXPost asks the RSSHub bridge for the post at a canonical status URL.
//
// It never errors: a bridge that is down, an account it cannot read, a post
// too far back in the timeline to still be in the feed all mean the same thing
// to the caller — nothing recovered, take the fallback.
func (s *Service) recoverXPost(ctx context.Context, statusURL string) (Item, bool) {
	handle, id := xHandleIn(statusURL), xStatusID(statusURL)
	if handle == "" || id == "" || s.hc == nil {
		return Item{}, false
	}
	feedURL := s.rsshubBase() + "/twitter/user/" + url.PathEscape(handle)

	ctx, cancel := context.WithTimeout(ctx, xRecoverTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return Item{}, false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")
	resp, err := s.hc.Do(req)
	if err != nil {
		return Item{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Item{}, false
	}
	feed, err := decodeFeed(resp.Body)
	if err != nil {
		return Item{}, false
	}

	// The synthetic subscription exists so itemFrom can do its ordinary job.
	// It is never saved — this is a lookup, not a subscribe.
	sub := Subscription{ID: "x-post", Kind: KindRSS, Title: "@" + handle, URL: feedURL, Mirror: MirrorFull}
	now := time.Now().UTC()
	for _, xi := range feed.items() {
		if !xRefersTo(id, bestLink(xi.Links), xi.GUID, xi.ID) {
			continue
		}
		it, ok := itemFrom(xi, sub, feed.title(), now)
		if !ok || strings.TrimSpace(Text(it.Body)) == "" {
			continue // an entry with no text is not the post we came for
		}
		// The pasted address is what the owner curated and what the note's
		// `url:` already says; a mirror's own spelling of it is not.
		it.URL = statusURL
		return it, true
	}
	return Item{}, false
}

// xRefersTo reports whether any of a feed entry's identities names this status.
//
// The id is matched as a WHOLE number rather than as a substring: X ids are
// long and numeric, and `…/status/1234` must not answer for `…/status/12345`.
func xRefersTo(id string, vals ...string) bool {
	if id == "" {
		return false
	}
	for _, v := range vals {
		for i := 0; i+len(id) <= len(v); i++ {
			if v[i:i+len(id)] != id {
				continue
			}
			if i > 0 && isASCIIDigit(v[i-1]) {
				continue
			}
			if j := i + len(id); j < len(v) && isASCIIDigit(v[j]) {
				continue
			}
			return true
		}
	}
	return false
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }
