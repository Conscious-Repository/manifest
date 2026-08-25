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
	"net/http"
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

// teaserUnder is the "this is a teaser" threshold in characters of plain text.
// Roughly two paragraphs — below it, a feed is almost certainly withholding the
// article rather than publishing a genuinely short post.
const teaserUnder = 1200

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

	// Body is the sanitized HTML. It is populated only when a caller asks for
	// one item (the reader, or a curate), never when listing the lane.
	Body string `json:"-"`

	// teaser records that this item's body came from <description>/<summary>
	// rather than <content:encoded>/<content>. That distinction is the honest
	// signal for "the feed is withholding the article": a publisher who ships
	// content:encoded is giving you everything they have, however short it is.
	// Length alone would send us fetching every link post and open thread.
	teaser bool
}

// Unread reports whether the item still wants reading.
func (i Item) Unread() bool { return i.ReadAt == "" && i.DismissedAt == "" }

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
func httpClient() *http.Client { return &http.Client{Timeout: 20 * time.Second} }
