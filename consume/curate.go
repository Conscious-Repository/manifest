package consume

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"manifest/mdfm"
	"manifest/record"
)

// CURATE — the button, and the only thing in this package that writes the
// owner's vault.
//
// One curated item is one note under extrinsic/, beside the books, carrying
// `categories: [articles]`. That placement is the whole design: what the owner
// picked out and what he said about it are HIS, they belong in the medium he
// keeps his thinking in, and they are versioned by the same git history as
// everything else there. The public feed is then a projection of those notes
// and of nothing else — which is what makes the isolation argument in
// public.go a structural claim rather than a filtering promise.
//
// Two rules protect the vault side:
//
//   - Re-curating an existing note NEVER clobbers it. The owner may have
//     written underneath the mirrored article; a second click updates the
//     metadata lines and leaves the body alone.
//   - Un-curating never deletes. It clears one frontmatter field, the feed
//     stops selecting the note, and the archive survives.

const (
	articlesCategory = "articles"
	curatedField     = "curated"
)

// errNoCurateCapability is what every entrance to the verb fails with when the
// service was built without a vault write. One sentinel because it is one
// condition, whichever surface asked.
var errNoCurateCapability = errors.New("consume: no write capability for curation")

// CuratedEntry is one note, projected. It is the entire vocabulary the public
// feed has — see public.go.
type CuratedEntry struct {
	ItemID    string `json:"itemId"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	Note      string `json:"note"`
	Published string `json:"published"`
	Curated   string `json:"curated"`
	Mirror    string `json:"mirror"`
	// Bibliographic fields, present only on a PAPER — a curated piece whose
	// URL resolved to a DOI at Crossref (see crossref.go). They are what the
	// public feed emits as Dublin Core and PRISM, and DOI is the flag: an
	// entry with one is a paper, an entry without one is a post.
	DOI     string   `json:"doi,omitempty"`
	Journal string   `json:"journal,omitempty"`
	Authors []string `json:"authors,omitempty"`
	// The EPISODE fields, present only on a curated podcast — the enclosure
	// the publisher attached, carried through the note so the public feed can
	// re-attach it. Audio is the flag, exactly as DOI is for a paper: an entry
	// with one is an episode, an entry without one is a piece of writing and
	// its feed item is byte-identical to what it was before podcasts existed
	// here.
	Audio      string `json:"audio,omitempty"`
	AudioType  string `json:"audioType,omitempty"`
	AudioBytes int64  `json:"audioBytes,omitempty"`
	Duration   int    `json:"duration,omitempty"` // seconds
	Episode    int    `json:"episode,omitempty"`
	Season     int    `json:"season,omitempty"`
	// Embed is the private side's player descriptor for a curated platform
	// link (see Item.Embed). public.go emits nothing for it — the public feed
	// carries the link, the title and the note, and no third-party markup.
	Embed string `json:"embed,omitempty"`
	Body  string `json:"-"` // markdown, the mirrored article
	// HTML is the sanitized body, resolved from the dataDir snapshot at serve
	// time. It is deliberately NOT stored in the note: the note is the
	// archive, in markdown, for a person to read in Obsidian.
	HTML string `json:"-"`
}

// curateKey normalizes a URL for identity. Two feeds can deliver the same
// essay with different tracking parameters; the owner curated the writing, not
// the query string.
func curateKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "ref" || lk == "s" || lk == "r" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Host = strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	u.Scheme = "https"
	return strings.TrimRight(u.String(), "/")
}

// Curate writes (or refreshes) the note for one item.
func (s *Service) Curate(ctx context.Context, itemID, note string) (CuratedEntry, error) {
	if s.writeVault == nil {
		return CuratedEntry{}, errNoCurateCapability
	}
	it, sub, ok := s.Get(itemID)
	if !ok {
		return CuratedEntry{}, fmt.Errorf("no item %q", itemID)
	}
	// A PAPER is asked of the registry, not of the page. Its whole text is a
	// typeset PDF behind a publisher's furniture, and the scholarly convention
	// is abstract + citation + link — so the completion fetch below is skipped
	// entirely and the note is built from the Crossref record. Everything that
	// is not a paper takes the path it always did.
	if paper, ok := s.paperFor(ctx, it.URL); ok {
		return s.writeCurated(it, sub, note, &paper)
	}
	// Curating means amplifying the WHOLE piece. If what we hold is a preview —
	// or the snapshot is gone — this is the moment to complete it: one fetch,
	// signed in where a session exists, before anything is written. Done before
	// taking the lock; a slow page must not stall the lane.
	if got, improved := s.captureFull(ctx, it, sub); improved {
		it = got
		s.store.Complete(sub.ID, it)
	}
	return s.writeCurated(it, sub, note, nil)
}

// writeCurated is THE vault write behind curation, and the only one. Both the
// lane's own button and the bridge in external.go land here, so "curating is
// one note under extrinsic/, refreshed rather than clobbered" is a property of
// this function instead of a convention two call sites have to keep.
//
// paper is the Crossref record when the piece is one, nil when it is not. It
// changes what the note SAYS (abstract + citation instead of a mirrored body)
// and nothing about where or how it is written.
func (s *Service) writeCurated(it Item, sub Subscription, note string, paper *PaperMeta) (CuratedEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	note = collapseSpaces(strings.ReplaceAll(strings.TrimSpace(note), "\n", " "))
	now := s.now()
	// An X POST is titled by its handle, whichever entrance wrote it — see
	// xpost.go. The rename happens BEFORE the note is built and AFTER the
	// filename is chosen, because the two want different strings: `@melissa
	// on X` is the same title for every post that account ever wrote, and
	// slugging it would pile a whole feed into one note. The post's own words
	// are what tell two apart, which is what the RSSHub side always slugged.
	xTitle := ""
	if paper == nil && s.isXPost(it, sub) {
		xTitle = xPostTitle(it.URL, it.Author, firstNonEmpty(it.Source, sub.Title))
	}
	rel := s.notePath(it, sub, xSlugBase(it, xTitle))
	if xTitle != "" {
		it.Title = xTitle
	}
	mirror := s.mirrorFor(it, sub)
	if paper != nil {
		// A paper's note carries the registry's abstract and citation — it is
		// whole in the only sense a paper is ever whole in a feed, whatever the
		// page fetch did or didn't return.
		mirror = MirrorFull
	}

	var content string
	if prior, err := s.readVault(rel); err == nil && len(prior) > 0 {
		// The note already exists — refresh its metadata, keep the owner's
		// body and any fields he added. Mirror is machine-owned metadata
		// derived from the subscription's setting, so refreshing it here is
		// how a re-click heals a note the retired paid-source rule stamped
		// excerpt.
		content = string(prior)
		content = setFrontmatter(content, curatedField, now.Format("2006-01-02"))
		content = setFrontmatter(content, "mirror", mirror)
		if note != "" {
			content = setFrontmatter(content, "note", yamlScalar(note))
		}
		if paper != nil {
			content = applyPaper(content, *paper, it.Title, firstNonEmpty(it.Source, sub.Title), it.URL)
		}
		// An episode's enclosure is machine-owned metadata like mirror, so a
		// re-click heals a note written before the lane knew about audio.
		content = applyEpisode(content, it)
		// An X note whose mirror is the provider's PREVIEW — cut at an
		// ellipsis — is repaired when this curate holds the whole post. It is
		// the narrowest write this branch makes: a mirror that does not end
		// clipped, or a body that is not the same post, comes back untouched,
		// so the owner's own words under a note are never at risk.
		if xTitle != "" {
			content = applyXBody(content, ToMarkdown(it.Body))
		}
	} else {
		content = s.buildNote(it, sub, note, now, paper, mirror)
	}
	// Both branches, one rule: a fresh note already carries the handle heading
	// and this only cleans the mirrored blockquote; a note written before the
	// convention existed gets retitled here. It is the same call the retrofit
	// makes, and it is a fixpoint — running it on its own output changes
	// nothing, which is what makes re-clicking curate safe.
	if xTitle != "" {
		content = applyXPost(content, xTitle)
	}

	if err := s.writeVault(rel, []byte(content)); err != nil {
		return CuratedEntry{}, err
	}
	s.invalidateCurated()
	entry, _ := parseCurated(rel, content)
	return entry, nil
}

// Uncurate clears the curated marker. The note stays — it is the owner's
// archive of something he read, and a second click is not a delete.
func (s *Service) Uncurate(itemID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.curatedEntries() {
		if e.ItemID != itemID {
			continue
		}
		raw, err := s.readVault(e.Path)
		if err != nil {
			return err
		}
		if err := s.writeVault(e.Path, []byte(setFrontmatter(string(raw), curatedField, ""))); err != nil {
			return err
		}
		s.invalidateCurated()
		return nil
	}
	return fmt.Errorf("%q is not curated", itemID)
}

// isXPost reports whether this curate is of an X post.
//
// The canonical address settles it on its own. The subscription is the second
// answer, for a post reached through a mirror — and it is narrowed to the
// bridge's /twitter/ route, because RSSHub carries a hundred other sites and
// none of them are X.
func (s *Service) isXPost(it Item, sub Subscription) bool {
	if IsXStatusURL(it.URL) {
		return true
	}
	if sub.Kind == KindX {
		return true
	}
	return s.fromRSSHub(sub) && strings.Contains(strings.ToLower(sub.URL), "/twitter/")
}

// xSlugBase is what an X note's FILENAME is made of: the post's own words,
// since its title is no longer unique to it. Empty xTitle means this is not an
// X post and the title is the base, as it always was.
func xSlugBase(it Item, xTitle string) string {
	if xTitle == "" {
		return it.Title
	}
	if text := collapseSpaces(Text(it.Body)); text != "" {
		return text
	}
	if t := strings.TrimSpace(it.Title); t != "" && t != xTitle {
		return t
	}
	return firstNonEmpty(collapseSpaces(it.Excerpt), xTitle)
}

// notePath is where one item's note lives. Titles collide (every newsletter
// has a "Weekly Roundup"), so a colliding slug gets the source appended rather
// than two different essays sharing one note.
//
// base is what to slug, which is the item's title for everything but an X post
// (see xSlugBase).
func (s *Service) notePath(it Item, sub Subscription, base string) string {
	// A piece ALREADY curated keeps the note it is in, whatever its title has
	// since become. Titles move — a publisher re-titles a post, and the X
	// convention renamed every one of them at once — and a filename derived
	// from a title that moved would fork the note in silence, which is the one
	// failure here the owner would never see.
	if rel, ok := s.curatedNotePath(it); ok {
		return rel
	}
	base = firstNonEmpty(slugFor(base), slugFor(it.Title), slugFor(sub.Title+" item"))
	rel := "extrinsic/" + base + ".md"
	if prior, err := s.readVault(rel); err == nil {
		if e, ok := parseCurated(rel, string(prior)); ok && e.ItemID != it.ID {
			rel = "extrinsic/" + base + "-" + slugFor(firstNonEmpty(sub.Title, sub.ID)) + ".md"
		}
	}
	return rel
}

// curatedNotePath finds the note this piece is already curated in, by the
// identity notePath's caller would otherwise have to guess at: the item id
// first, then the link.
func (s *Service) curatedNotePath(it Item) (string, bool) {
	for _, e := range s.curatedEntries() {
		if it.ID != "" && e.ItemID == it.ID {
			return e.Path, true
		}
		if SameLink(e.URL, it.URL) {
			return e.Path, true
		}
	}
	return "", false
}

// buildNote renders a fresh curated note.
func (s *Service) buildNote(it Item, sub Subscription, note string, now time.Time, paper *PaperMeta, mirror string) string {
	w := (&mdfm.Writer{}).SetList("categories", []string{articlesCategory})
	w.Set("source", yamlScalar(firstNonEmpty(it.Source, sub.Title)))
	if it.Author != "" {
		w.Set("author", yamlScalar(it.Author))
	}
	w.Set("url", it.URL)
	published := ""
	if !it.PublishedAt.IsZero() {
		published = it.PublishedAt.Format("2006-01-02")
	}
	if paper != nil {
		// The registry's date is the PAPER's date. A card's date is the day
		// the scout noticed it, which is not what a citation means by it.
		published = firstNonEmpty(paper.Published, published)
	}
	w.Set("published", published)
	w.Set(curatedField, now.Format("2006-01-02"))
	w.Set("item", it.ID)
	w.Set("mirror", mirror)
	if paper != nil {
		w.Set("doi", paper.DOI)
		w.Set("journal", yamlScalar(paper.Journal))
		w.SetList("authors", paper.Authors)
	}
	for k, v := range mediaFields(it) {
		w.Set(k, v)
	}
	if note != "" {
		w.Set("note", yamlScalar(note))
	}

	if paper != nil {
		// No leading prose. A fresh paper note is the registry's record and
		// nothing else — the referring surface's sentence about the paper is
		// machine-written, and the owner's own words reach the note through
		// `note:` (frontmatter, rendered as his commentary), never as body
		// text that reads like the paper's.
		return w.String(strings.TrimRight(
			paperNoteBody("", it.Title, *paper,
				firstNonEmpty(it.Source, sub.Title), it.URL), "\n"))
	}

	var b strings.Builder
	b.WriteString("#article\n\n")
	b.WriteString("# " + strings.TrimSpace(it.Title) + "\n\n")
	if body := ToMarkdown(it.Body); body != "" {
		b.WriteString(body + "\n")
	} else if it.Excerpt != "" {
		b.WriteString(it.Excerpt + "\n\n")
	}
	// The episode itself, written into the note so the archive holds the thing
	// and not only words about it. It is also what the public feed falls back
	// to when the snapshot is gone (see bodyHTML).
	if it.Podcast() {
		line := "\nAudio: [listen](" + it.Audio + ")"
		if it.Duration > 0 {
			line += " · " + FormatDuration(it.Duration)
		}
		b.WriteString(line + "\n")
	}
	if it.URL != "" {
		b.WriteString("\n---\n\nSource: [" + firstNonEmpty(it.Source, sub.Title, it.URL) + "](" + it.URL + ")\n")
	}
	return w.String(strings.TrimRight(b.String(), "\n"))
}

// episodeFields is the note's frontmatter for one episode — empty for
// everything that is not one, which is what keeps an article's note exactly
// the note it was.
//
// Duration is written as a plain count of SECONDS. Writing it 1:12:33 would
// read better and would also be a YAML sexagesimal literal — the kind of trap
// that turns an hour into the number 4353 in whatever else reads the vault.
// The human-readable form goes in the note's body instead, where it is text.
// parseSeconds accepts both, so a hand-edited note reads back either way.
func episodeFields(it Item) map[string]string {
	if !it.Podcast() {
		return nil
	}
	out := map[string]string{"audio": it.Audio}
	if it.AudioType != "" {
		out["audioType"] = it.AudioType
	}
	if it.AudioBytes > 0 {
		out["audioBytes"] = strconv.FormatInt(it.AudioBytes, 10)
	}
	if it.Duration > 0 {
		out["duration"] = strconv.Itoa(it.Duration)
	}
	if it.Episode > 0 {
		out["episode"] = strconv.Itoa(it.Episode)
	}
	if it.Season > 0 {
		out["season"] = strconv.Itoa(it.Season)
	}
	return out
}

// mediaFields is episodeFields plus the private-side embed descriptor — the
// machine-owned metadata a re-click heals. Empty for everything that is
// neither an episode nor a platform link.
func mediaFields(it Item) map[string]string {
	out := episodeFields(it)
	if embed := strings.TrimSpace(it.Embed); embed != "" {
		if out == nil {
			out = map[string]string{}
		}
		out["embed"] = embed
	}
	return out
}

// applyEpisode refreshes an existing note's episode fields in place.
func applyEpisode(content string, it Item) string {
	for k, v := range mediaFields(it) {
		content = setFrontmatter(content, k, v)
	}
	return content
}

// mirrorFor decides how much of a curated item the PUBLIC feed carries.
//
// Curation is deliberate amplification: the owner picked this piece to carry
// in his own feed, paid source or not, and weighed the republishing question
// at the moment he clicked. So the subscription's own mirror setting decides —
// this used to force excerpt for any signed-in source, and the attribution
// header keeping credit and traffic pointed home is what the owner judged
// sufficient when he retired that rule. One honesty carve-out remains: an item
// whose body is still a preview has nothing full to mirror, and stamping
// `full` on a stub would publish the stub as if it were the piece. The startup
// backfill retries those captures and flips the note once the full body lands.
func (s *Service) mirrorFor(it Item, sub Subscription) string {
	if it.Preview != "" {
		return MirrorExcerpt
	}
	if sub.Mirrors() {
		return MirrorFull
	}
	return MirrorExcerpt
}

// ---- the paper format ----
//
// A paper's note has two halves, and the split is the whole point:
//
//	above the marker   the owner's — his heading, and only prose he wrote
//	below the marker   the registry's — abstract, citation, source link
//
// Above the marker is OWNER-AUTHORED ONLY. Nothing generated goes there: not a
// scout card's `why`, not a digest sentence, not a publisher's blurb. A note
// the owner has written nothing into leaves that half blank, which is what a
// fresh paper note is. His commentary on a piece lives in `note:` — one field,
// one meaning, and the public feed renders it as his.
//
// Everything below paperMarker is machine-owned and rewritten on every curate;
// everything above it is never touched. That is the same fixpoint discipline
// setFrontmatter applies to one key, applied to one section of the body — and
// it is what makes re-curating a paper, or retrofitting one written before this
// format existed, safe to run repeatedly.
const paperMarker = "<!-- crossref -->"

// paperKeepMax bounds what a note written BEFORE the marker existed can
// contribute as owner prose. A curated sentence is a sentence; a scraped
// article page is a hundred kilobytes of navigation, ORCID markers and figure
// captions, which is exactly what the paper format exists to stop publishing.
// Above the bound, the prior body is the mirrored page and the abstract
// replaces it; below it, it is prose and it is kept.
const paperKeepMax = 1200

// paperNoteBody renders a paper note's body, preserving the owner's half.
//
// prior is the existing note body ("" for a fresh note). A fresh note gets no
// prose above the marker at all; the only prose that ever appears there is
// what the owner already wrote, carried across from prior.
func paperNoteBody(prior, title string, m PaperMeta, source, link string) string {
	section := paperSection(m, source, link)
	owner := ""
	if strings.TrimSpace(prior) != "" {
		if i := strings.Index(prior, paperMarker); i >= 0 {
			return strings.TrimRight(prior[:i], " \t\n") + "\n\n" + section + "\n"
		}
		// First upgrade of a note written the old way. Its own heading is the
		// owner's title — the registry's may be the formal one he paraphrased.
		if h := headingOf(prior); h != "" {
			title = h
		}
		owner = stripSourceFooter(stripLeadingHeading(prior))
		if len([]rune(owner)) > paperKeepMax {
			owner = ""
		}
	}
	var b strings.Builder
	b.WriteString("#article\n\n# " + strings.TrimSpace(title) + "\n\n")
	if w := strings.TrimSpace(owner); w != "" {
		b.WriteString(w + "\n\n")
	}
	b.WriteString(section + "\n")
	return b.String()
}

// paperSection is the registry's half: what Crossref knows, as a person reads
// it. The DOI is rendered as a link because that is the citable address of the
// work — the publisher URL beside it is one route to it, not its identity.
func paperSection(m PaperMeta, source, link string) string {
	var b strings.Builder
	b.WriteString(paperMarker + "\n")
	if m.Abstract != "" {
		b.WriteString("\n## Abstract\n\n" + m.Abstract + "\n")
	}
	if c := citation(m); c != "" {
		b.WriteString("\n## Citation\n\n" + c + "\n")
	}
	if link != "" {
		b.WriteString("\nSource: [" + firstNonEmpty(source, m.Journal, link) + "](" + link + ")\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// citation is the one-line reference: authors, title, journal, year, DOI.
func citation(m PaperMeta) string {
	var parts []string
	if len(m.Authors) > 0 {
		who := strings.Join(m.Authors, ", ")
		if len(m.Authors) >= maxAuthors {
			who += ", et al."
		}
		parts = append(parts, who)
	}
	if m.Title != "" {
		parts = append(parts, "\u201c"+strings.TrimRight(m.Title, ".")+".\u201d")
	}
	if venue := firstNonEmpty(m.Journal, m.Publisher); venue != "" {
		if y := m.Year(); y != "" {
			venue += ", " + y
		}
		parts = append(parts, "*"+venue+"*")
	} else if y := m.Year(); y != "" {
		parts = append(parts, y)
	}
	if m.DOI != "" {
		parts = append(parts, "DOI: ["+m.DOI+"]("+m.DOIURL()+")")
	}
	return strings.Join(parts, ". ")
}

// applyPaper rewrites one existing note into the paper format: the
// bibliographic frontmatter, then the registry's half of the body.
func applyPaper(content string, m PaperMeta, title, source, link string) string {
	content = setFrontmatter(content, "doi", m.DOI)
	if m.Journal != "" {
		content = setFrontmatter(content, "journal", yamlScalar(m.Journal))
	}
	if len(m.Authors) > 0 {
		content = setFrontmatter(content, "authors", "["+strings.Join(m.Authors, ", ")+"]")
	}
	if m.Published != "" {
		content = setFrontmatter(content, "published", m.Published)
	}
	_, body := mdfm.Split(content)
	return replaceBody(content, paperNoteBody(body, title, m, source, link))
}

// replaceBody swaps a note's body, leaving its frontmatter block byte-for-byte
// alone. A note with no frontmatter has no metadata to keep.
func replaceBody(content, body string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return strings.TrimSpace(body) + "\n"
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[:i+1], "\n") + "\n\n" + strings.TrimSpace(body) + "\n"
		}
	}
	return content // unterminated frontmatter: not a document we can edit safely
}

// stripSourceFooter drops the trailing `---` / `Source: [...]` block buildNote
// appends. The paper section renders its own.
func stripSourceFooter(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for len(lines) > 0 {
		t := strings.TrimSpace(lines[len(lines)-1])
		if t == "" || t == "---" || strings.HasPrefix(t, "Source: [") {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// stripPaperMarker removes the marker line from a PROJECTED body. It is a seam
// in the file, not content: a reader should never see it, and the excerpt a
// feed builds from the body should never start with it.
func stripPaperMarker(body string) string {
	if !strings.Contains(body, paperMarker) {
		return body
	}
	out := make([]string, 0, 8)
	for _, ln := range strings.Split(body, "\n") {
		if strings.TrimSpace(ln) == paperMarker {
			continue
		}
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// captureFull completes an item that is about to be published with less than
// its whole body, returning the completed item and whether anything improved.
//
// It exists because fillFullText serves the READER and lets a page's paywall
// MARKUP veto its own extraction — Substack embeds audience:"only_paid" in the
// page source whether or not the session unlocked it, so a signed-in fetch
// that returned the whole article still gets thrown away there. At curate time
// the veto keys on what the page visibly SAYS instead: subscribe-box prose
// means the fetch got the wrapper rather than the writing, and a body that is
// no longer than what we already hold proves nothing was gained.
func (s *Service) captureFull(ctx context.Context, it Item, sub Subscription) (Item, bool) {
	if !s.needsCapture(it, sub) {
		return it, false
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	body, _ := s.fetchArticle(ctx, it.URL, s.cookieFor(it.URL))
	text := Text(body)
	if body == "" || looksPaywalled(text, "") || LooksTruncated(text) {
		return it, false
	}
	if it.Body != "" && len([]rune(text)) <= it.Chars {
		return it, false
	}
	it.Body = body
	it.Chars = len([]rune(text))
	it.Excerpt = Excerpt(text, 280)
	it.Preview = ""
	return it, true
}

// needsCapture reports whether a curate-time completion fetch is worth making:
// the item is a known preview, its snapshot is gone, or its body still ends in
// a truncation marker. An X post (bridge included) is already whole, and
// fulltext:off is the owner saying never fetch this publisher's pages —
// honoured here as everywhere.
func (s *Service) needsCapture(it Item, sub Subscription) bool {
	if it.URL == "" || sub.Kind == KindX || s.fromRSSHub(sub) || sub.FullText() == FullTextOff {
		return false
	}
	return it.Preview != "" || it.Body == "" || LooksTruncated(Text(it.Body))
}

// BackfillCurated repairs curated notes the retired paid-source rule left
// excerpt-only. mirrorFor used to stamp `mirror: excerpt` on anything from a
// signed-in site whatever the subscription said, and a note does not rewrite
// itself when a rule does. For every curated note still marked excerpt whose
// subscription says full, this completes the item's body if it needs it and
// flips that one frontmatter field — the body and everything else in the note
// is the owner's and stays untouched. A capture that fails (a real paywall,
// no working session) leaves the note excerpt-only for the next boot to retry.
//
// It also retrofits X POSTS onto the handle title convention (backfillXPosts)
// and papers into the paper format (backfillPapers). Same argument, newer rule: a
// note written before the paper format existed carries a scraped page (or a
// bare sentence) where an abstract and a citation belong, and a note does not
// rewrite itself when a rule does.
func (s *Service) BackfillCurated(ctx context.Context) int {
	if s.writeVault == nil {
		return 0
	}
	n := s.backfillPapers(ctx)
	n += s.backfillXPosts()
	n += s.backfillXBodies(ctx)
	for _, e := range s.Curated() {
		if !strings.EqualFold(e.Mirror, MirrorExcerpt) {
			continue
		}
		it, sub, ok := s.Get(e.ItemID)
		if !ok || !sub.Mirrors() {
			continue // item gone, or excerpt is the owner's own setting
		}
		if got, improved := s.captureFull(ctx, it, sub); improved {
			it = got
			s.store.Complete(sub.ID, it)
		}
		if s.mirrorFor(it, sub) != MirrorFull {
			continue // still a preview — nothing full to publish
		}
		s.mu.Lock()
		raw, err := s.readVault(e.Path)
		if err == nil {
			err = s.writeVault(e.Path, []byte(setFrontmatter(string(raw), "mirror", MirrorFull)))
		}
		s.mu.Unlock()
		if err == nil {
			n++
		}
	}
	if n > 0 {
		s.invalidateCurated()
	}
	return n
}

// backfillXPosts brings X notes curated before the handle convention onto it.
//
// The selection rule is IsXStatusURL and nothing else: a note whose `url:` is
// not one X post's canonical address is never read past its frontmatter, which
// is what keeps a retrofit out of the rest of the owner's extrinsic/. What it
// writes is applyXPost — the same call a fresh curate makes — so a retrofitted
// note and a newly written one are the same document, and a note already on
// the convention comes back byte-identical and is not written at all.
//
// It asks the network nothing. The handle is in the URL the note already
// carries, so this is deterministic, offline, and safe to run on every boot.
func (s *Service) backfillXPosts() int {
	n := 0
	for _, e := range s.Curated() {
		if !IsXStatusURL(e.URL) {
			continue
		}
		want := xPostTitle(e.URL, e.Author, e.Source)
		s.mu.Lock()
		raw, err := s.readVault(e.Path)
		if err == nil {
			if content := applyXPost(string(raw), want); content != string(raw) {
				if err = s.writeVault(e.Path, []byte(content)); err == nil {
					n++
				}
			}
		}
		s.mu.Unlock()
	}
	if n > 0 {
		s.invalidateCurated()
	}
	return n
}

// backfillXBodies repairs X notes that carry the oEmbed PREVIEW instead of the
// post — the notes this entrance wrote before it asked the bridge first.
//
// The gate is two cheap offline tests before any network at all: the note's
// `url:` is one canonical X status (IsXStatusURL), and its mirrored body ends
// where the provider cut it (xLooksClipped). A vault of whole notes therefore
// makes no request on boot, and the ones that do ask are asking the local
// bridge for the account's own timeline.
//
// What it writes is applyXBody, which refuses the swap unless the recovered
// post OPENS with the words the note already holds — so a bridge that cannot
// page back far enough to reach an old post leaves that note exactly as it
// was, rather than filling it with a different one. Nothing is invented, and
// the next boot retries.
func (s *Service) backfillXBodies(ctx context.Context) int {
	n := 0
	for _, e := range s.Curated() {
		if !IsXStatusURL(e.URL) {
			continue
		}
		s.mu.Lock()
		raw, err := s.readVault(e.Path)
		s.mu.Unlock()
		if err != nil {
			continue
		}
		_, body := mdfm.Split(string(raw))
		if !xLooksClipped(xMirrorText(body)) {
			continue
		}
		post, ok := s.recoverXPost(ctx, e.URL)
		if !ok {
			continue
		}
		content := applyXBody(string(raw), ToMarkdown(post.Body))
		if content == string(raw) {
			continue
		}
		// The note now carries the whole post, so the field that said it did
		// not is stale. It is the same machine-owned metadata a re-click
		// heals, written here for the same reason.
		content = setFrontmatter(content, "mirror", MirrorFull)
		s.mu.Lock()
		err = s.writeVault(e.Path, []byte(content))
		s.mu.Unlock()
		if err == nil {
			n++
		}
	}
	if n > 0 {
		s.invalidateCurated()
	}
	return n
}

// backfillPapers brings already-curated papers into the paper format.
//
// The selection rule is the cheap one first: a note that already carries a
// `doi:` is done, and asks the registry nothing ever again. Of the rest, only
// a URL that yields a DOI is looked up at all — so an ordinary boot with a
// vault full of essays makes no network call, and one that fails (Crossref
// down, a DOI it has never seen) simply leaves the note as it was for the next
// boot to retry.
//
// The write is applyPaper's, so a retrofitted note and a freshly curated one
// are the same document produced by the same code, and the owner's half above
// the marker survives both.
func (s *Service) backfillPapers(ctx context.Context) int {
	n := 0
	for _, e := range s.Curated() {
		if strings.TrimSpace(e.DOI) != "" {
			continue
		}
		paper, ok := s.paperFor(ctx, e.URL)
		if !ok {
			continue
		}
		s.mu.Lock()
		raw, err := s.readVault(e.Path)
		if err == nil {
			content := applyPaper(string(raw), paper, e.Title, e.Source, e.URL)
			err = s.writeVault(e.Path, []byte(setFrontmatter(content, "mirror", MirrorFull)))
		}
		s.mu.Unlock()
		if err == nil {
			n++
		}
	}
	if n > 0 {
		s.invalidateCurated()
	}
	return n
}

// yamlScalar quotes a value that would otherwise break the block. Titles carry
// colons constantly ("The World Behind the World: Consciousness…") and an
// unquoted one silently truncates the note's frontmatter.
func yamlScalar(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.ContainsAny(v, ":#[]{}\",'&*?|<>=!%@`") || strings.HasPrefix(v, "-") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

// setFrontmatter rewrites one key in place, appends it before the closing
// fence when absent, or removes its line when value is empty. Everything else
// in the file — other keys, the body, the owner's own additions — is
// untouched, which is the fixpoint discipline applied to frontmatter.
func setFrontmatter(content, key, value string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		if value == "" {
			return content
		}
		return "---\n" + key + ": " + value + "\n---\n" + content
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return content
	}
	for i := 1; i < end; i++ {
		k, _, ok := strings.Cut(lines[i], ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), key) {
			continue
		}
		if value == "" {
			return strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n")
		}
		lines[i] = strings.TrimSpace(k) + ": " + value
		return strings.Join(lines, "\n")
	}
	if value == "" {
		return strings.Join(lines, "\n")
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, key+": "+value)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// ---- reading the curated set back ----

type curatedCache struct {
	mu      sync.Mutex
	entries []CuratedEntry
	at      time.Time
}

// curatedTTL bounds how stale the projection can be. Curating through the app
// invalidates immediately; the TTL only covers edits made directly in Obsidian.
const curatedTTL = 30 * time.Second

func (s *Service) invalidateCurated() {
	s.curated.mu.Lock()
	s.curated.at = time.Time{}
	s.curated.mu.Unlock()
}

// curatedEntries scans extrinsic/ for curated article notes, newest first.
func (s *Service) curatedEntries() []CuratedEntry {
	s.curated.mu.Lock()
	defer s.curated.mu.Unlock()
	if !s.curated.at.IsZero() && time.Since(s.curated.at) < curatedTTL {
		return s.curated.entries
	}
	out := []CuratedEntry{}
	if s.listVault != nil && s.readVault != nil {
		paths, err := s.listVault("extrinsic")
		if err == nil {
			for _, rel := range paths {
				raw, err := s.readVault(rel)
				if err != nil {
					continue
				}
				if e, ok := parseCurated(rel, string(raw)); ok {
					out = append(out, e)
				}
			}
		}
	}
	sortCurated(out)
	s.curated.entries = out
	s.curated.at = time.Now()
	return out
}

// parseCurated projects one note, and is THE selection rule: a note qualifies
// only if it declares categories: [articles] AND carries a curated date.
// Reading something is not publishing it.
func parseCurated(rel, content string) (CuratedEntry, bool) {
	fm, body := mdfm.Split(content)
	if fm == nil {
		return CuratedEntry{}, false
	}
	if !hasCategory(fm["categories"], articlesCategory) {
		return CuratedEntry{}, false
	}
	curated := record.Unquote(strings.TrimSpace(fm[curatedField]))
	if curated == "" {
		return CuratedEntry{}, false
	}
	title := record.Unquote(strings.TrimSpace(fm["title"]))
	if title == "" {
		title = headingOf(body)
	}
	if title == "" {
		title = strings.TrimSuffix(pathBase(rel), ".md")
	}
	return CuratedEntry{
		ItemID:    strings.TrimSpace(fm["item"]),
		Path:      rel,
		Title:     title,
		Source:    record.Unquote(strings.TrimSpace(fm["source"])),
		Author:    record.Unquote(strings.TrimSpace(fm["author"])),
		URL:       strings.TrimSpace(fm["url"]),
		Note:      record.Unquote(strings.TrimSpace(fm["note"])),
		Published: record.Unquote(strings.TrimSpace(fm["published"])),
		Curated:   curated,
		Mirror:    strings.TrimSpace(fm["mirror"]),
		DOI:       strings.TrimSpace(fm["doi"]),
		Journal:   record.Unquote(strings.TrimSpace(fm["journal"])),
		Authors:   mdfm.List(fm["authors"]),
		// Audio present ⇒ this note is an episode, and the public feed
		// re-attaches the enclosure. parseSeconds accepts both 01:12:33 and a
		// bare count, so a hand-edited note reads back the same.
		Audio:      strings.TrimSpace(fm["audio"]),
		AudioType:  strings.TrimSpace(fm["audioType"]),
		AudioBytes: parseBytes(fm["audioBytes"]),
		Duration:   parseSeconds(record.Unquote(strings.TrimSpace(fm["duration"]))),
		Episode:    positiveInt(fm["episode"]),
		Season:     positiveInt(fm["season"]),
		Embed:      strings.TrimSpace(fm["embed"]),
		Body:       stripPaperMarker(stripLeadingHeading(body)),
	}, true
}

func hasCategory(raw, want string) bool {
	for _, c := range mdfm.List(raw) {
		if strings.EqualFold(strings.TrimSpace(c), want) {
			return true
		}
	}
	return false
}

// headingOf returns the note's first `# ` heading.
func headingOf(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return ""
}

// stripLeadingHeading drops the tag line and the title heading, which the feed
// renders from the entry's own fields.
func stripLeadingHeading(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for len(lines) > 0 {
		t := strings.TrimSpace(lines[0])
		if t == "" || strings.HasPrefix(t, "#article") || strings.HasPrefix(t, "# ") {
			lines = lines[1:]
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func pathBase(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

func sortCurated(in []CuratedEntry) {
	// Newest curation first: the feed is "what I am paying attention to now".
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j].Curated > in[j-1].Curated; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}

// Curated returns the curated notes, newest first.
func (s *Service) Curated() []CuratedEntry { return s.curatedEntries() }

// CuratedFor answers "is the piece at this URL already curated, and which note
// is it?" — the lookup behind both the reader's curated chip and the pasted
// link's identity check.
//
// It lives here rather than in the server layer because the answer is
// curateKey's, and a caller that has to know how two URLs are compared is a
// caller that will one day compare them differently.
func (s *Service) CuratedFor(rawURL string) (CuratedEntry, bool) {
	if strings.TrimSpace(rawURL) == "" {
		return CuratedEntry{}, false
	}
	for _, e := range s.curatedEntries() {
		if SameLink(e.URL, rawURL) {
			return e, true
		}
	}
	return CuratedEntry{}, false
}

// curatedURLs is the lane's "already curated" lookup.
func (s *Service) curatedURLs() map[string]bool {
	out := map[string]bool{}
	for _, e := range s.curatedEntries() {
		if e.URL != "" {
			out[curateKey(e.URL)] = true
		}
	}
	return out
}

// SameLink reports whether two URLs point at the same piece of writing,
// ignoring tracking parameters and trivial spelling differences. Exported for
// the server layer, which matches a polled item against the curated set.
func SameLink(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	return curateKey(a) == curateKey(b)
}

// UpdateCuratedNote edits the `note:` line of one curated note and nothing
// else.
//
// It exists because Curate is the wrong verb for this. Curate resolves an
// ACTIVE store item — it may fetch the whole piece, rebuild the body, restamp
// the mirror — and the curated panel lists notes, not items. A note curated
// from a pasted link, an external bridge or a subscription long since retired
// carries an `item:` id (`ext-…`) that no store holds, so routing the panel's
// "edit note" through Curate failed with `no item "ext-…"` for exactly the
// entries the panel exists to audit. The identity here is therefore the note's
// PATH, which is the thing the projection actually names.
//
// The write is the narrowest one in this file: one frontmatter key, through
// the same vault writer curation uses, on a note the projection already agrees
// is curated. The body — the mirrored article and whatever the owner wrote
// under it — is never touched.
func (s *Service) UpdateCuratedNote(itemID, path, note string) (CuratedEntry, error) {
	if s.writeVault == nil {
		return CuratedEntry{}, errNoCurateCapability
	}
	note = collapseSpaces(strings.ReplaceAll(strings.TrimSpace(note), "\n", " "))
	if note == "" {
		// Clearing a note is a deletion of the owner's words, and the curate
		// path has always treated an empty note as "keep what's there". Saying
		// so is better than a silent no-op the panel would report as saved.
		return CuratedEntry{}, errors.New("a note is required — to clear one, edit the note in your vault")
	}
	rel := strings.TrimSpace(path)
	if !curatedNotePathOK(rel) {
		return CuratedEntry{}, fmt.Errorf("not a curated note %q", path)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// The projection is the authority on what may be edited here: a path it
	// does not list is either outside extrinsic/, not an article, or not
	// curated, and all three are the same refusal.
	var entry CuratedEntry
	found := false
	for _, e := range s.curatedEntries() {
		if e.Path == rel {
			entry, found = e, true
			break
		}
	}
	if !found {
		return CuratedEntry{}, fmt.Errorf("not a curated note %q", path)
	}
	// The id is a cross-check, never the lookup — the panel sends what it
	// rendered, and a mismatch means the list it rendered from has moved on.
	if itemID != "" && entry.ItemID != "" && entry.ItemID != itemID {
		return CuratedEntry{}, fmt.Errorf("%q is not the note at %q — reload the curated list", itemID, rel)
	}

	raw, err := s.readVault(rel)
	if err != nil {
		return CuratedEntry{}, err
	}
	content := setFrontmatter(string(raw), "note", yamlScalar(note))
	if err := s.writeVault(rel, []byte(content)); err != nil {
		return CuratedEntry{}, err
	}
	s.invalidateCurated()
	out, _ := parseCurated(rel, content)
	return out, nil
}

// curatedNotePathOK is the path guard: a vault-relative markdown note directly
// under extrinsic/, with nothing that could climb out of it. The projection
// check above would refuse a stray path anyway; this refuses it before a read.
func curatedNotePathOK(rel string) bool {
	if rel == "" || !strings.HasSuffix(rel, ".md") {
		return false
	}
	if strings.Contains(rel, "..") || strings.Contains(rel, `\`) || strings.HasPrefix(rel, "/") {
		return false
	}
	base, ok := strings.CutPrefix(rel, "extrinsic/")
	return ok && base != "" && !strings.Contains(base, "/")
}
