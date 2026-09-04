package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// siteEntry is one post as enumerated from the site: its slug, canonical URL,
// and (when the sitemap carries one) the lastmod date.
type siteEntry struct {
	Slug    string
	URL     string
	LastMod string // YYYY-MM-DD or ""
}

// fetcher is a polite HTTP client. Substack answers a burst with 429 and no
// Retry-After, so requests to the site are sequential, spaced by `delay`, and
// a 429/5xx backs off geometrically before giving up. Image hosts (the CDN /
// S3) get a shorter pause.
type fetcher struct {
	client   *http.Client
	delay    time.Duration
	cacheDir string
	last     map[string]time.Time
}

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) manifest-samizdat/1 (+https://github.com/Conscious-Repository/manifest)"

const maxBody = 32 << 20 // one page or image; the sanitizer caps HTML lower

func newFetcher(delay time.Duration, cacheDir string) *fetcher {
	return &fetcher{
		client:   &http.Client{Timeout: 60 * time.Second},
		delay:    delay,
		cacheDir: cacheDir,
		last:     map[string]time.Time{},
	}
}

// get fetches one URL with pacing + backoff and returns the body and the
// response content-type. Only 200 is success; 404 fails fast.
func (f *fetcher) get(ctx context.Context, target, accept string) ([]byte, string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, "", err
	}
	pause := f.delay
	if !strings.HasSuffix(u.Host, "consciousrepository.com") {
		pause = 200 * time.Millisecond
	}
	backoff := 30 * time.Second
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		if wait := pause - time.Since(f.last[u.Host]); wait > 0 {
			time.Sleep(wait)
		}
		f.last[u.Host] = time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", userAgent)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		resp, err := f.client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
			resp.Body.Close()
			switch {
			case rerr != nil:
				lastErr = rerr
			case resp.StatusCode == http.StatusOK:
				return body, resp.Header.Get("Content-Type"), nil
			case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
				return nil, "", fmt.Errorf("%s: HTTP %d", target, resp.StatusCode)
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			default:
				return nil, "", fmt.Errorf("%s: HTTP %d", target, resp.StatusCode)
			}
		}
		log.Printf("  retry %s in %s (%v)", short(target), backoff, lastErr)
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 8*time.Minute {
			backoff *= 2
		}
	}
	return nil, "", fmt.Errorf("%s: gave up: %w", target, lastErr)
}

// page returns the live HTML of one post, via the cache dir when configured
// (a hit is served from disk; a miss is fetched and stored).
func (f *fetcher) page(ctx context.Context, e siteEntry) (string, error) {
	var cachePath string
	if f.cacheDir != "" {
		cachePath = filepath.Join(f.cacheDir, e.Slug+".html")
		if b, err := os.ReadFile(cachePath); err == nil && strings.Contains(string(b), "<article") {
			return string(b), nil
		}
	}
	b, _, err := f.get(ctx, e.URL, "text/html")
	if err != nil {
		return "", err
	}
	if cachePath != "" {
		_ = os.MkdirAll(f.cacheDir, 0o755)
		_ = os.WriteFile(cachePath, b, 0o644)
	}
	return string(b), nil
}

// enumerate lists every post on the site, newest first where the source says.
// Sitemap first (full history); on failure feed.xml (latest 20) + the archive
// API, merged and deduped by slug. The returned label says which path ran.
func enumerate(ctx context.Context, f *fetcher) ([]siteEntry, string, error) {
	entries, err := fromSitemap(ctx, f)
	if err == nil && len(entries) > 0 {
		return entries, "sitemap", nil
	}
	log.Printf("sitemap unavailable (%v); falling back to feed + archive", err)
	seen := map[string]siteEntry{}
	feed, ferr := fromFeed(ctx, f)
	for _, e := range feed {
		seen[e.Slug] = e
	}
	arch, aerr := fromArchiveAPI(ctx, f)
	for _, e := range arch {
		if _, ok := seen[e.Slug]; !ok {
			seen[e.Slug] = e
		}
	}
	if len(seen) == 0 {
		return nil, "", fmt.Errorf("no posts found (feed: %v; archive: %v)", ferr, aerr)
	}
	out := make([]siteEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, "feed+archive", nil
}

var postPath = regexp.MustCompile(`^/p/([A-Za-z0-9._~-]+)/?$`)

// slugOf returns the post slug for a consciousrepository.com /p/<slug> URL, or "".
func slugOf(raw string) (slug, canonical string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.HasSuffix(u.Host, "consciousrepository.com") {
		return "", ""
	}
	m := postPath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", ""
	}
	return m[1], siteBase + "/p/" + m[1]
}

func fromSitemap(ctx context.Context, f *fetcher) ([]siteEntry, error) {
	b, _, err := f.get(ctx, siteBase+"/sitemap.xml", "application/xml")
	if err != nil {
		return nil, err
	}
	return parseSitemap(b)
}

// parseSitemap keeps the /p/ URLs of a sitemap in document order (Substack
// emits newest first) and dedupes by slug.
func parseSitemap(b []byte) ([]siteEntry, error) {
	var doc struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []siteEntry
	for _, u := range doc.URLs {
		slug, canon := slugOf(u.Loc)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		lm := strings.TrimSpace(u.LastMod)
		if len(lm) >= 10 {
			lm = lm[:10]
		}
		out = append(out, siteEntry{Slug: slug, URL: canon, LastMod: lm})
	}
	if len(out) == 0 {
		return nil, errors.New("sitemap listed no /p/ posts")
	}
	return out, nil
}

func fromFeed(ctx context.Context, f *fetcher) ([]siteEntry, error) {
	b, _, err := f.get(ctx, siteBase+"/feed.xml", "application/rss+xml")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Items []struct {
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	var out []siteEntry
	for _, it := range doc.Items {
		slug, canon := slugOf(it.Link)
		if slug == "" {
			continue
		}
		e := siteEntry{Slug: slug, URL: canon}
		if t, err := time.Parse(time.RFC1123Z, strings.TrimSpace(it.PubDate)); err == nil {
			e.LastMod = t.UTC().Format("2006-01-02")
		} else if t, err := time.Parse(time.RFC1123, strings.TrimSpace(it.PubDate)); err == nil {
			e.LastMod = t.UTC().Format("2006-01-02")
		}
		out = append(out, e)
	}
	return out, nil
}

// fromArchiveAPI pages Substack's public archive endpoint (what the /archive
// page itself calls; the HTML page is client-rendered and lists nothing).
func fromArchiveAPI(ctx context.Context, f *fetcher) ([]siteEntry, error) {
	var out []siteEntry
	for offset := 0; offset < 5000; {
		target := fmt.Sprintf("%s/api/v1/archive?sort=new&search=&offset=%d&limit=50", siteBase, offset)
		b, _, err := f.get(ctx, target, "application/json")
		if err != nil {
			return out, err
		}
		var items []struct {
			CanonicalURL string `json:"canonical_url"`
			PostDate     string `json:"post_date"`
		}
		if err := json.Unmarshal(b, &items); err != nil {
			return out, err
		}
		if len(items) == 0 {
			break
		}
		for _, it := range items {
			slug, canon := slugOf(it.CanonicalURL)
			if slug == "" {
				continue
			}
			e := siteEntry{Slug: slug, URL: canon}
			if len(it.PostDate) >= 10 {
				e.LastMod = it.PostDate[:10]
			}
			out = append(out, e)
		}
		offset += len(items)
	}
	return out, nil
}
