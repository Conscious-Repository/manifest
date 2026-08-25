// Package consume is the CONSUME lane (ARCHITECTURE §5, fifth kind, amended
// 2026-08-24): the writing the owner subscribed to — RSS/Atom feeds and X
// accounts — polled on an interval, read inside manifest, and curated out to a
// public feed.
//
// It is a §6 portal in every sense that matters — polls in, caches under
// dataDir, cursor-based, failure ≠ empty — and it is modeled on portals/
// deliberately rather than sharing it, because its output is a different
// attention kind with a different lifecycle and an export contract. Nothing
// here is agentic: no LLM is ever in the loop, so what lands in the lane is a
// deterministic function of what the publisher published.
//
// The tier split is the load-bearing decision (§2):
//
//	VAULT extrinsic/    what the owner authored — the subscription list, and
//	                    one note per curated item. Irreplaceable, git-versioned.
//	dataDir consume/    what can be re-fetched — poll caches, article bodies,
//	                    the API token. Disposable by definition.
//
// Deleting dataDir costs a re-poll. Nothing else.
package consume

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// userAgent follows the house convention (geocode/books): the tool, a version,
// and a parenthetical saying who is actually asking. Feeds are served by
// people, and a polite identifiable poller is the difference between being
// tolerated and being blocked.
const userAgent = "manifest-dashboard/1.0 (personal reader; one subscriber)"

// Kinds of subscription. The set is small on purpose: everything that is not
// the X API is a feed URL, including every bridge and mirror that fronts X.
const (
	KindRSS = "rss"
	KindX   = "x"
)

// Mirror modes decide how much of an item the PUBLIC feed carries once curated.
// Per-subscription rather than global, so one publisher asking to be excerpted
// is a one-field edit and not a redesign.
const (
	MirrorFull    = "full"
	MirrorExcerpt = "excerpt"
)

// FullText modes decide whether a teaser-only feed gets its article fetched.
//
//	auto — fetch when the feed body is clearly a teaser (the default)
//	on   — always fetch the article page
//	off  — never; show exactly what the feed published
const (
	FullTextAuto = "auto"
	FullTextOn   = "on"
	FullTextOff  = "off"
)

// teaserUnder is the length below which a body that arrived as <description>
// is assumed to be a teaser rather than a genuinely short post.
//
// ⚠ It applies ONLY to that provenance guess. A body ending in a truncation
// MARKER is withheld at any length — The Habsburg Way publishes 10,000-character
// previews that still stop at "Read more", and gating those on length meant the
// reader never even tried to complete them and never said they were partial.
const teaserUnder = 1200

// Preview classifications: what we learned when a truncated item could NOT be
// completed. Empty means the item is whole (or we never had to find out).
const (
	PreviewPaid    = "paid"    // the page says it is for paying subscribers
	PreviewPartial = "partial" // truncated, and the page yielded nothing better
)

// truncationMarkers are what a publisher appends when it is withholding the
// rest of a piece. Matched at the very END of the plain text.
//
// ⚠ This is the signal that actually correlates, and provenance is not.
// Substack sends `content:encoded` for every item and truncates INSIDE it — a
// 367-character body ending "Read more" beside a 20,000-character sibling in
// the same feed. Keying on "did this arrive as <description>" misses every one
// of them.
var truncationMarkers = []string{
	"read more", "continue reading", "read the rest", "keep reading",
	"read the full", "continue to the full",
}

// LooksTruncated reports whether a body's plain text ends the way a withheld
// article ends. A 20,000-character post never does.
func LooksTruncated(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.TrimRight(t, ".!…â€¦ \t\n→>»")
	t = strings.TrimSpace(t)
	for _, m := range truncationMarkers {
		if strings.HasSuffix(t, m) {
			return true
		}
	}
	return false
}

// defaultMinChars is the X length filter: below this, a post is a remark, not
// writing. Only applies to KindX — an RSS feed is already curated by its author.
const defaultMinChars = 350

// Subscription is one source the owner follows — a line in extrinsic/feeds.md.
type Subscription struct {
	ID       string `json:"id"`       // stable slug, derived from Title at subscribe time
	Kind     string `json:"kind"`     // rss | x
	Title    string `json:"title"`    // the owner's display name for it
	URL      string `json:"url"`      // rss: the resolved feed URL
	Handle   string `json:"handle"`   // x: the account, no @
	List     string `json:"list"`     // group ("essays"); "" = ungrouped
	Mirror   string `json:"mirror"`   // full | excerpt
	MinChars int    `json:"minChars"` // x only; 0 = defaultMinChars
	Fulltext string `json:"fulltext"` // auto | on | off
	Added    string `json:"added"`    // ISO date

	// Unknown carries inline fields this build does not recognize, so a
	// hand-added [tag:: x] in Obsidian survives a round trip through the app.
	// The fixpoint guarantee (§3) is the whole reason this exists.
	Unknown []Field `json:"-"`
}

// Field is one unrecognized inline field, preserved verbatim.
type Field struct{ Key, Value string }

// Mirrors reports whether this subscription's curated items carry their full
// body into the public feed.
func (s Subscription) Mirrors() bool { return !strings.EqualFold(s.Mirror, MirrorExcerpt) }

// FullText returns the effective full-text mode.
func (s Subscription) FullText() string {
	switch strings.ToLower(strings.TrimSpace(s.Fulltext)) {
	case FullTextOn:
		return FullTextOn
	case FullTextOff:
		return FullTextOff
	default:
		return FullTextAuto
	}
}

// Min returns the effective minimum length for an X post.
func (s Subscription) Min() int {
	if s.MinChars > 0 {
		return s.MinChars
	}
	return defaultMinChars
}

// Item is one polled piece of writing. The body does NOT live here — it is
// written to its own snapshot file, because this struct is re-read from a JSON
// cache on every feed request and a few hundred articles of inline HTML would
// make that read cost real money in latency.
type Item struct {
	ID      string `json:"id"`
	SubID   string `json:"subId"`
	Source  string `json:"source"` // subscription title at fetch time
	List    string `json:"list"`
	Author  string `json:"author"`
	Title   string `json:"title"`
	URL     string `json:"url"` // canonical link to the original
	Excerpt string `json:"excerpt"`
	Chars   int    `json:"chars"` // rune count of the plain text

	PublishedAt time.Time `json:"publishedAt"`
	FetchedAt   time.Time `json:"fetchedAt"`

	ReadAt      string `json:"readAt,omitempty"`
	DismissedAt string `json:"dismissedAt,omitempty"`

	// SeededAt marks an item that arrived in a subscription's FIRST poll —
	// published before the owner subscribed. It is kept, browsable and
	// searchable, but never counted as unread: following a new feed should not
	// drop fifty articles into the queue.
	//
	// It is deliberately its own state rather than a back-dated ReadAt. Marking
	// something "read" that was never opened is a lie the UI would then repeat
	// back forever; the archive can honestly say "arrived before you
	// subscribed" instead.
	SeededAt string `json:"seededAt,omitempty"`

	// Body is the sanitized HTML. It is populated only when a caller asks for
	// one item (the reader, or a curate), never when listing the lane.
	Body string `json:"-"`

	// Preview records that this item cannot be completed: "paid" when the
	// publisher's page says it is for subscribers, "partial" when the page
	// simply yielded nothing better. Empty means whole.
	//
	// ⚠ Only ever set on an item we KNOW was truncated and then failed to
	// improve. A short post published in full is not a preview, and saying so
	// would be exactly the kind of lie SeededAt exists to avoid.
	Preview string `json:"preview,omitempty"`

	// teaser records that this item's body came from <description>/<summary>
	// rather than <content:encoded>/<content>. That distinction is the honest
	// signal for "the feed is withholding the article": a publisher who ships
	// content:encoded is giving you everything they have, however short it is.
	// Length alone would send us fetching every link post and open thread.
	//
	// It is ALSO set when the body ends in a truncation marker, whatever
	// element carried it — see LooksTruncated.
	teaser bool

	// truncated is the DEFINITIVE half of that: the body ends in a truncation
	// marker, so the publisher is demonstrably withholding the rest. Unlike the
	// provenance guess, this needs no length test — a long preview is still a
	// preview.
	truncated bool
}

// Unread reports whether the item still wants reading.
func (i Item) Unread() bool { return i.ReadAt == "" && i.DismissedAt == "" && i.SeededAt == "" }

// Seeded reports whether the item came from the backfill rather than from a
// poll that ran while the owner was subscribed.
func (i Item) Seeded() bool { return i.SeededAt != "" && i.ReadAt == "" && i.DismissedAt == "" }

// itemID is the stable identity of one piece of writing. It is deterministic —
// the same post re-polled forever yields the same id, which is what makes
// dedupe, read-state and curation idempotent.
//
// Colons and never slashes: the id travels as a single {id} path segment.
func itemID(kind, subID, external string) string {
	sum := sha256.Sum256([]byte(external))
	return "consume:" + kind + ":" + subID + ":" + hex.EncodeToString(sum[:])[:12]
}

// snapshotName maps an item id to its body filename. Hashed rather than
// escaped: ids contain colons, and a hash cannot collide with a sibling
// subscription's id no matter what characters a feed puts in a guid.
func snapshotName(itemID string) string {
	sum := sha256.Sum256([]byte(itemID))
	return hex.EncodeToString(sum[:])[:24] + ".html"
}

// httpClient is the shared fetch client. 20s matches portals/; feeds are small
// and a slow one must not hold a poll goroutine open indefinitely.
//
// ⚠ CheckRedirect strips the Cookie header whenever a redirect leaves the host
// it was set for. Go copies most headers across redirects, so without this one
// sloppy redirect — an http→https bounce onto a CDN, a shortener, a hijacked
// domain — would hand somebody else the owner's full session. A credential is
// scoped to a site; the transport has to enforce that, not the caller.
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			if len(via) > 0 && !sameSite(via[0].URL, req.URL) {
				req.Header.Del("Cookie")
			}
			return nil
		},
	}
}

// sameSite reports whether two URLs share a registrable domain — the same rule
// a browser uses to decide whether a cookie travels.
func sameSite(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	ka, kb := SiteKey(a.String()), SiteKey(b.String())
	return ka != "" && ka == kb
}

// redactURL is what any error, log line or cached status may say about a URL.
//
// ⚠ Scheme, host and path only. Query strings and userinfo are where tokens
// live — Substack's podcast private feeds put one straight in the path/query —
// and consume's failure paths flow into THREE places that are not the secrets
// tier: the process log, the 0644 poll cache, and the /api response the manage
// panel renders. Redacting at the source is the only place that covers all
// three.
func redactURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "the feed"
	}
	out := u.Scheme + "://" + u.Host + u.Path
	if u.RawQuery != "" {
		out += "?…"
	}
	return out
}

// errNoHost is returned when a credential is offered with no site to attach to.
var errNoHost = errors.New("no site for that credential")
