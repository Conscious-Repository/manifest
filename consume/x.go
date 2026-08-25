package consume

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The official X API fetcher.
//
// ⚠ COST DISCIPLINE IS A CORRECTNESS PROPERTY HERE, not an optimization.
// X bills pay-per-use per RESOURCE RETURNED — roughly $0.005 a post, $0.010 a
// user lookup — with no free tier for new developers and no minimum spend. So:
//
//   - since_id is mandatory after the first poll. Without it every poll re-reads
//     and re-pays for the same backlog, forever, on a six-hour timer.
//   - the first poll is capped at firstFill, not max_results=100. A naive
//     backfill across five accounts is a few dollars in one tick.
//   - the handle→id lookup is done ONCE and cached in the subscription's
//     cursors. It is billed at the higher user-read rate and the answer never
//     changes.
//
// TestXAlwaysSendsSinceIDAfterTheFirstPoll and TestXFirstPollIsCapped exist to
// keep those three properties true when someone edits this file later.
//
// Everything else about X arrives through rss.go: a self-hosted RSSHub, a
// Nitter mirror or any other bridge is subscribed as an ordinary feed URL and
// needs none of this.

const (
	xAPIBase = "https://api.x.com/2"
	// firstFill bounds the initial backfill of a new subscription. Enough to
	// make the lane look alive, cheap enough not to notice.
	firstFill = 20
	// pageMax is X's own ceiling for the timeline endpoint.
	pageMax = 100
)

type xFetcher struct {
	hc    *http.Client
	token string
	base  string // test override
}

func (x *xFetcher) baseURL() string {
	if x.base != "" {
		return x.base
	}
	return xAPIBase
}

func (x *xFetcher) get(ctx context.Context, path string, q url.Values, out any) error {
	u := x.baseURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+x.token)
	req.Header.Set("User-Agent", userAgent)
	resp, err := x.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("X rejected the token (%s) — replace it in PORTALS", resp.Status)
	case http.StatusTooManyRequests:
		return fmt.Errorf("X rate limit reached; the next poll will retry")
	case http.StatusPaymentRequired:
		return fmt.Errorf("X reports no available credits — top up in the X developer console")
	default:
		return fmt.Errorf("x %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type xUserResp struct {
	Data struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"data"`
	Errors []struct {
		Detail string `json:"detail"`
		Title  string `json:"title"`
	} `json:"errors"`
}

type xPost struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	NoteTweet struct {
		Text string `json:"text"`
	} `json:"note_tweet"`
	Entities struct {
		URLs []struct {
			URL         string `json:"url"`
			ExpandedURL string `json:"expanded_url"`
			DisplayURL  string `json:"display_url"`
		} `json:"urls"`
	} `json:"entities"`
}

type xTimelineResp struct {
	Data []xPost `json:"data"`
	Meta struct {
		NewestID string `json:"newest_id"`
		ResultCt int    `json:"result_count"`
	} `json:"meta"`
	Errors []struct {
		Detail string `json:"detail"`
	} `json:"errors"`
}

// Fetch polls one X account's original posts.
func (x *xFetcher) Fetch(ctx context.Context, sub Subscription, cur map[string]string) ([]Item, map[string]string, error) {
	handle := strings.TrimPrefix(strings.TrimSpace(sub.Handle), "@")
	if handle == "" {
		return nil, nil, fmt.Errorf("subscription %q has no X handle", sub.ID)
	}

	next := map[string]string{}
	for k, v := range cur {
		next[k] = v
	}

	// The user lookup is billed and immutable — resolve once, then never again.
	userID := next["userId"]
	authorName := next["userName"]
	if userID == "" {
		var ur xUserResp
		if err := x.get(ctx, "/users/by/username/"+url.PathEscape(handle), nil, &ur); err != nil {
			return nil, nil, err
		}
		if ur.Data.ID == "" {
			detail := "no such account"
			if len(ur.Errors) > 0 {
				detail = firstNonEmpty(ur.Errors[0].Detail, ur.Errors[0].Title, detail)
			}
			return nil, nil, fmt.Errorf("@%s: %s", handle, detail)
		}
		userID = ur.Data.ID
		authorName = firstNonEmpty(ur.Data.Name, handle)
		next["userId"] = userID
		next["userName"] = authorName
	}

	q := url.Values{}
	q.Set("exclude", "retweets,replies")
	q.Set("tweet.fields", "created_at,note_tweet,entities")
	since := strings.TrimSpace(cur["sinceId"])
	if since != "" {
		q.Set("since_id", since)
		q.Set("max_results", strconv.Itoa(pageMax))
	} else {
		// First contact: a bounded sample, not the whole timeline.
		q.Set("max_results", strconv.Itoa(firstFill))
	}

	var tl xTimelineResp
	if err := x.get(ctx, "/users/"+url.PathEscape(userID)+"/tweets", q, &tl); err != nil {
		return nil, nil, err
	}

	// Advance the cursor even when everything was filtered out, or short posts
	// would be re-fetched — and re-billed — on every single poll.
	if tl.Meta.NewestID != "" {
		next["sinceId"] = tl.Meta.NewestID
	} else if newest := newestID(tl.Data); newest != "" {
		next["sinceId"] = newest
	}

	now := time.Now().UTC()
	min := sub.Min()
	out := make([]Item, 0, len(tl.Data))
	for _, p := range tl.Data {
		text := firstNonEmpty(p.NoteTweet.Text, p.Text)
		// Rune count, not bytes: the filter is about how much someone wrote.
		if len([]rune(strings.TrimSpace(text))) <= min {
			continue // a remark, not writing
		}
		body := xBody(text, p)
		out = append(out, Item{
			ID:          itemID(KindX, sub.ID, p.ID),
			SubID:       sub.ID,
			Source:      firstNonEmpty(sub.Title, "@"+handle),
			List:        sub.List,
			Author:      firstNonEmpty(authorName, "@"+handle),
			Title:       Excerpt(collapse(text), 80),
			URL:         "https://x.com/" + handle + "/status/" + p.ID,
			Excerpt:     Excerpt(collapse(text), 280),
			Chars:       len([]rune(text)),
			PublishedAt: parseDate(p.CreatedAt),
			FetchedAt:   now,
			Body:        body,
		})
	}
	return out, next, nil
}

// newestID is the fallback cursor when meta.newest_id is absent. X ids are
// numeric and monotonic, so the largest is the newest — compared by length
// first because they overflow a signed 64-bit int as decimal strings.
func newestID(posts []xPost) string {
	ids := make([]string, 0, len(posts))
	for _, p := range posts {
		if p.ID != "" {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
	return ids[len(ids)-1]
}

// xBody renders post text as HTML deterministically: blank lines become
// paragraphs and t.co links are restored to the URL the author actually
// shared. No scraping, no LLM, no unfurling.
func xBody(text string, p xPost) string {
	for _, u := range p.Entities.URLs {
		if u.URL != "" && u.ExpandedURL != "" {
			text = strings.ReplaceAll(text, u.URL, u.ExpandedURL)
		}
	}
	var b strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		b.WriteString("<p>")
		for i, line := range strings.Split(para, "\n") {
			if i > 0 {
				b.WriteString("<br>")
			}
			b.WriteString(linkify(line))
		}
		b.WriteString("</p>")
	}
	// Through the sanitizer like everything else: the text is third-party and
	// gets no exemption for having come from an API instead of a feed.
	return Sanitize(b.String())
}

// linkify turns bare URLs into anchors and escapes everything else.
func linkify(line string) string {
	var b strings.Builder
	for i, tok := range strings.Split(line, " ") {
		if i > 0 {
			b.WriteString(" ")
		}
		if strings.HasPrefix(tok, "https://") || strings.HasPrefix(tok, "http://") {
			esc := escapeAttr(tok)
			b.WriteString(`<a href="` + esc + `">` + esc + `</a>`)
			continue
		}
		b.WriteString(escapeAttr(tok))
	}
	return b.String()
}

func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}
