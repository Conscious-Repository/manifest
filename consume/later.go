package consume

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"manifest/record"
)

// WATCH LATER — a deliberate queue of things to read, watch or listen to
// (owner decision 2026-09-27).
//
// Anything can go in: a link pasted from anywhere (an article, a YouTube
// video, a podcast episode, an X post), a feed item saved with "later", or a
// link shared from the phone. An entry stays until the owner marks it done;
// opening it does not remove it. Done entries stay searchable under
// "Later · done".
//
// The tier split follows feeds.md exactly:
//
//	VAULT extrinsic/later.md    the queue itself — what the owner chose.
//	                            One line per entry, hand-editable, fixpoint.
//	dataDir consume/cache/_later.json + snapshots
//	                            each entry's resolved card and readable body.
//	                            Disposable: a missing entry is resolved again
//	                            from the URL on the line.

const laterPath = "extrinsic/later.md"

// laterSub is the store key for the queue's cached items. The underscore is
// what keeps it apart from every subscription: record.Slug never emits one,
// so no feed can ever be named into this cache file.
const laterSub = "_later"

const laterScaffold = `---
categories: [later]
---
#later

Watch Later — things saved to read, watch or listen to. Hand edits are preserved; unknown fields survive.

`

// LaterEntry is one line of extrinsic/later.md.
type LaterEntry struct {
	ID     string `json:"id"` // later-<hash of the normalized URL>
	Title  string `json:"title"`
	URL    string `json:"url"`
	Source string `json:"source,omitempty"`
	Kind   string `json:"kind,omitempty"` // article | video | podcast | post
	Item   string `json:"item,omitempty"` // the feed item it was saved from
	Added  string `json:"added"`
	Done   string `json:"done,omitempty"`

	Unknown []Field `json:"-"`
}

// laterHash is the one identity both the line and the cached item derive
// from: the piece, not the spelling of its URL.
func laterHash(rawURL string) string {
	sum := sha256.Sum256([]byte(curateKey(rawURL)))
	return hex.EncodeToString(sum[:])[:12]
}

func laterEntryID(rawURL string) string { return "later-" + laterHash(rawURL) }

// LaterItemID is the cached item's id for a URL. It has the ordinary
// consume:<kind>:<sub>:<hash> shape, so the reader, read-state and curate
// routes all work on it unchanged.
func LaterItemID(rawURL string) string { return "consume:later:" + laterSub + ":" + laterHash(rawURL) }

func laterItemIDFor(e LaterEntry) string {
	if h := strings.TrimPrefix(e.ID, "later-"); h != e.ID && h != "" {
		return "consume:later:" + laterSub + ":" + h
	}
	return LaterItemID(e.URL)
}

// ---- the document ----

type laterDoc struct {
	lines   []string
	entries []laterLine
}

type laterLine struct {
	line int
	e    LaterEntry
}

func parseLater(content string) *laterDoc {
	d := &laterDoc{}
	if content == "" {
		content = laterScaffold
	}
	d.lines = strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inFrontmatter := false
	for i, ln := range d.lines {
		t := strings.TrimSpace(ln)
		if t == "---" && (i == 0 || inFrontmatter) {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter || !strings.HasPrefix(t, "- ") {
			continue
		}
		if e, ok := parseLaterLine(t); ok {
			d.entries = append(d.entries, laterLine{line: i, e: e})
		}
	}
	return d
}

func parseLaterLine(trimmed string) (LaterEntry, bool) {
	title, fields := record.ParseFields(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
	if len(fields) == 0 {
		return LaterEntry{}, false
	}
	e := LaterEntry{Title: strings.TrimSpace(strings.Trim(title, "[]"))}
	for _, f := range fields {
		switch strings.ToLower(f.Key) {
		case "id":
			e.ID = f.Value
		case "url":
			e.URL = f.Value
		case "source":
			e.Source = f.Value
		case "kind":
			e.Kind = strings.ToLower(f.Value)
		case "item":
			e.Item = f.Value
		case "added":
			e.Added = f.Value
		case "done":
			e.Done = f.Value
		default:
			e.Unknown = append(e.Unknown, Field{Key: f.Key, Value: f.Value})
		}
	}
	if strings.TrimSpace(e.URL) == "" {
		return LaterEntry{}, false
	}
	if e.ID == "" {
		// A hand-added line with only a URL still works; the next app write
		// stamps its id.
		e.ID = laterEntryID(e.URL)
	}
	return e, true
}

func (d *laterDoc) String() string { return strings.Join(d.lines, "\n") }

func (d *laterDoc) find(id string) (LaterEntry, int, bool) {
	for i, l := range d.entries {
		if l.e.ID == id {
			return l.e, i, true
		}
	}
	return LaterEntry{}, -1, false
}

func (d *laterDoc) findURL(rawURL string) (LaterEntry, bool) {
	key := curateKey(rawURL)
	for _, l := range d.entries {
		if curateKey(l.e.URL) == key {
			return l.e, true
		}
	}
	return LaterEntry{}, false
}

// add appends a line at the end of the file (after any trailing blank lines
// are folded), so the newest entry is the last line, as in any queue.
func (d *laterDoc) add(e LaterEntry) {
	for len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) == "" {
		d.lines = d.lines[:len(d.lines)-1]
	}
	d.lines = append(d.lines, renderLaterLine("", e), "")
	d.entries = append(d.entries, laterLine{line: len(d.lines) - 2, e: e})
}

func (d *laterDoc) update(e LaterEntry) bool {
	_, i, ok := d.find(e.ID)
	if !ok {
		return false
	}
	at := d.entries[i].line
	d.lines[at] = renderLaterLine(d.lines[at], e)
	d.entries[i].e = e
	return true
}

func (d *laterDoc) remove(id string) bool {
	_, i, ok := d.find(id)
	if !ok {
		return false
	}
	at := d.entries[i].line
	d.lines = append(d.lines[:at], d.lines[at+1:]...)
	d.entries = append(d.entries[:i], d.entries[i+1:]...)
	for j := range d.entries {
		if d.entries[j].line > at {
			d.entries[j].line--
		}
	}
	return true
}

// renderLaterLine writes a line, rewriting known fields in place on an
// existing one so hand-added fields keep their order and spelling.
func renderLaterLine(prior string, e LaterEntry) string {
	known := [][2]string{
		{"id", e.ID}, {"url", e.URL}, {"source", e.Source}, {"kind", e.Kind},
		{"item", e.Item}, {"added", e.Added}, {"done", e.Done},
	}
	title := strings.TrimSpace(strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(e.Title))
	if title == "" {
		title = hostOf(e.URL)
	}
	if strings.TrimSpace(prior) == "" {
		var b strings.Builder
		b.WriteString("- " + title)
		for _, kv := range known {
			if kv[1] != "" {
				b.WriteString(" " + record.EmitField(kv[0], kv[1]))
			}
		}
		for _, f := range e.Unknown {
			b.WriteString(" " + record.EmitField(f.Key, f.Value))
		}
		return b.String()
	}
	indent := prior[:len(prior)-len(strings.TrimLeft(prior, " \t"))]
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prior), "- "))
	_, fields := record.ParseFields(body)
	present := map[string]bool{}
	for _, f := range fields {
		present[strings.ToLower(f.Key)] = true
	}
	out := body
	for _, kv := range known {
		out = setField(out, kv[0], kv[1], present[kv[0]])
	}
	if i := record.FieldRe.FindStringIndex(out); i != nil {
		out = title + " " + strings.TrimSpace(out[i[0]:])
	}
	return indent + "- " + strings.TrimSpace(out)
}

// ---- the service ----

func (s *Service) laterDocLoad() (*laterDoc, error) {
	if s.readVault == nil {
		return nil, errors.New("no vault configured")
	}
	b, err := s.readVault(laterPath)
	if err != nil {
		return parseLater(""), nil
	}
	return parseLater(string(b)), nil
}

func (s *Service) laterSave(d *laterDoc) error {
	if s.writeVault == nil {
		return errors.New("consume: no write capability")
	}
	return s.writeVault(laterPath, []byte(d.String()))
}

// LaterEntries returns the queue in the order it was saved, newest first.
func (s *Service) LaterEntries() []LaterEntry {
	d, err := s.laterDocLoad()
	if err != nil {
		return nil
	}
	out := make([]LaterEntry, 0, len(d.entries))
	for i := len(d.entries) - 1; i >= 0; i-- {
		out = append(out, d.entries[i].e)
	}
	return out
}

// laterIndex maps a normalized URL to its entry, for flagging feed cards that
// are already in the queue.
func (s *Service) laterIndex() map[string]LaterEntry {
	out := map[string]LaterEntry{}
	for _, e := range s.LaterEntries() {
		out[curateKey(e.URL)] = e
	}
	return out
}

// errAlreadyLater says the piece is queued; the caller answers with the entry.
var errAlreadyLater = errors.New("already in Later")

// SaveLaterURL resolves a pasted link and queues it. The vault line is written
// FIRST, from nothing but the URL, so a slow or failing page never loses the
// save; resolution then fills the title and the cached, readable item.
func (s *Service) SaveLaterURL(ctx context.Context, rawURL string) (LaterEntry, error) {
	e, fresh, err := s.QueueLaterURL(rawURL)
	if err != nil || !fresh {
		return e, err
	}
	return s.resolveLater(ctx, e)
}

// QueueLaterURL writes the queue line for a link and returns at once; fresh
// is false when the piece was already queued. ResolveLater then reads it.
// The share sheet uses the two halves separately so the phone is answered
// before the page has been fetched.
func (s *Service) QueueLaterURL(rawURL string) (LaterEntry, bool, error) {
	clean, err := s.guardPasted(rawURL)
	if err != nil {
		return LaterEntry{}, false, err
	}
	e, err := s.laterAdd(LaterEntry{URL: clean, Title: hostOf(clean), Source: hostOf(clean)})
	if errors.Is(err, errAlreadyLater) {
		return e, false, nil
	}
	return e, err == nil, err
}

// ResolveLater reads the piece behind a queued entry (see resolveLater).
func (s *Service) ResolveLater(ctx context.Context, e LaterEntry) (LaterEntry, error) {
	return s.resolveLater(ctx, e)
}

// SaveLaterItem queues a feed item. Its card and body are copied into the
// queue's own cache, so the entry outlives the feed's 90-day retention.
func (s *Service) SaveLaterItem(itemID string) (LaterEntry, error) {
	it, sub, ok := s.Get(itemID)
	if !ok {
		return LaterEntry{}, errors.New("no such item")
	}
	if strings.TrimSpace(it.URL) == "" {
		return LaterEntry{}, errors.New("this item has no link to keep")
	}
	e := LaterEntry{
		URL: it.URL, Title: it.Title, Source: firstNonEmpty(it.Source, sub.Title),
		Kind: itemKind(it, s.fromRSSHub(sub)), Item: it.ID,
	}
	e, err := s.laterAdd(e)
	if err != nil && !errors.Is(err, errAlreadyLater) {
		return LaterEntry{}, err
	}
	cp := it
	cp.ID = laterItemIDFor(e)
	cp.SubID = laterSub
	cp.ReadAt, cp.DismissedAt, cp.SeededAt = "", "", ""
	s.store.PutItem(laterSub, cp)
	return e, nil
}

// laterAdd writes a new line, or reports the existing one.
func (s *Service) laterAdd(e LaterEntry) (LaterEntry, error) {
	s.laterMu.Lock()
	defer s.laterMu.Unlock()
	d, err := s.laterDocLoad()
	if err != nil {
		return LaterEntry{}, err
	}
	if have, ok := d.findURL(e.URL); ok {
		if have.Done != "" {
			// Saving something already finished puts it back in the queue.
			have.Done = ""
			d.update(have)
			if err := s.laterSave(d); err != nil {
				return LaterEntry{}, err
			}
		}
		return have, errAlreadyLater
	}
	e.ID = laterEntryID(e.URL)
	if e.Added == "" {
		e.Added = s.now().Format("2006-01-02")
	}
	d.add(e)
	return e, s.laterSave(d)
}

// resolveLater reads the piece behind an entry and caches it as a readable
// item, then corrects the line's title, source and kind from what it found.
func (s *Service) resolveLater(ctx context.Context, e LaterEntry) (LaterEntry, error) {
	ref := ExternalRef{ID: urlRefID(e.URL), URL: e.URL, Source: hostOf(e.URL)}
	s.resolvePasted(ctx, e.URL, &ref, false)
	it := ref.item()
	it.ID = laterItemIDFor(e)
	it.SubID = laterSub
	it.FetchedAt = s.now()
	switch {
	case ref.paper != nil:
		it.Title = firstNonEmpty(it.Title, ref.paper.Title)
		it.Body = "<p>" + escapeText(ref.paper.Abstract) + "</p>"
	case it.Embed != "" || it.Podcast():
		// A player is the piece; the page's words about it are the notes.
		it.Body = paragraphs(ref.Fallback)
	default:
		if body, ok := s.fetchExternal(ctx, it.URL, ref); ok {
			it.Body = body
		} else {
			it.Body = paragraphs(ref.Fallback)
			it.Preview = PreviewPartial
		}
	}
	text := Text(it.Body)
	it.Chars = len([]rune(text))
	it.Excerpt = Excerpt(firstNonEmpty(text, ref.Fallback), 280)
	if it.Title == "" {
		it.Title = firstNonEmpty(Excerpt(text, 80), hostOf(e.URL))
	}
	s.store.PutItem(laterSub, it)

	s.laterMu.Lock()
	defer s.laterMu.Unlock()
	d, err := s.laterDocLoad()
	if err != nil {
		return e, nil
	}
	cur, _, ok := d.find(e.ID)
	if !ok {
		return e, nil // removed while resolving
	}
	cur.Title = it.Title
	cur.Source = firstNonEmpty(it.Source, cur.Source)
	cur.Kind = itemKind(it, IsXStatusURL(e.URL))
	d.update(cur)
	if err := s.laterSave(d); err != nil {
		return cur, err
	}
	return cur, nil
}

// ResolveMissingLater re-reads entries whose cached item is gone — a wiped
// dataDir, or a hand-added line. Bounded per call; the list endpoint runs it
// in the background.
func (s *Service) ResolveMissingLater(ctx context.Context, max int) int {
	n := 0
	for _, e := range s.LaterEntries() {
		if n >= max {
			break
		}
		if _, ok := s.store.Get(laterSub, laterItemIDFor(e)); ok {
			continue
		}
		if e.Item != "" {
			if _, err := s.SaveLaterItem(e.Item); err == nil {
				n++
				continue
			}
		}
		if _, err := s.resolveLater(ctx, e); err == nil {
			n++
		}
	}
	return n
}

// MissingLater reports whether any entry lacks its cached item.
func (s *Service) MissingLater() bool {
	for _, e := range s.LaterEntries() {
		if _, ok := s.store.Get(laterSub, laterItemIDFor(e)); !ok {
			return true
		}
	}
	return false
}

// SetLaterDone marks an entry finished, or puts it back in the queue.
func (s *Service) SetLaterDone(id string, done bool) error {
	s.laterMu.Lock()
	defer s.laterMu.Unlock()
	d, err := s.laterDocLoad()
	if err != nil {
		return err
	}
	e, _, ok := d.find(id)
	if !ok {
		return errors.New("no such Later entry")
	}
	if done {
		e.Done = s.now().Format("2006-01-02")
	} else {
		e.Done = ""
	}
	d.update(e)
	return s.laterSave(d)
}

// RemoveLater deletes the line. The cached item goes too.
func (s *Service) RemoveLater(id string) error {
	s.laterMu.Lock()
	defer s.laterMu.Unlock()
	d, err := s.laterDocLoad()
	if err != nil {
		return err
	}
	e, _, ok := d.find(id)
	if !ok {
		return errors.New("no such Later entry")
	}
	d.remove(id)
	if err := s.laterSave(d); err != nil {
		return err
	}
	s.store.DeleteItem(laterSub, laterItemIDFor(e))
	return nil
}

// LaterEntryFor finds the entry holding an item: the queue's own cached copy,
// or the feed item it was saved from.
func (s *Service) LaterEntryFor(itemID string) (LaterEntry, bool) {
	for _, e := range s.LaterEntries() {
		if laterItemIDFor(e) == itemID || (e.Item != "" && e.Item == itemID) {
			return e, true
		}
	}
	return LaterEntry{}, false
}

// LaterEntryForURL finds the queue entry for a link, however it is spelled.
func (s *Service) LaterEntryForURL(rawURL string) (LaterEntry, bool) {
	if strings.TrimSpace(rawURL) == "" {
		return LaterEntry{}, false
	}
	e, ok := s.laterIndex()[curateKey(rawURL)]
	return e, ok
}

// laterCards renders the queue (done=false) or the finished list (done=true).
// An entry whose cache is gone still shows, from its line, so a wiped cache
// never empties the queue.
func (s *Service) laterCards(done bool, curated map[string]bool) []Card {
	out := []Card{}
	for _, e := range s.LaterEntries() {
		if (e.Done != "") != done {
			continue
		}
		c := s.laterCard(e, curated)
		out = append(out, c)
	}
	return out
}

func (s *Service) laterCard(e LaterEntry, curated map[string]bool) Card {
	sub := Subscription{ID: laterSub, Title: firstNonEmpty(e.Source, hostOf(e.URL)), Kind: KindRSS}
	if it, ok := s.store.Get(laterSub, laterItemIDFor(e)); ok {
		c := card(it, sub, curated, e.Kind == "post")
		c.Later, c.LaterID, c.LaterDone, c.Saved = true, e.ID, e.Done != "", e.Added
		return c
	}
	return Card{
		ID: laterItemIDFor(e), SubID: laterSub, Kind: "consume", Type: firstNonEmpty(cardType(e.Kind), "rss"),
		Source: firstNonEmpty(e.Source, hostOf(e.URL)), Title: firstNonEmpty(e.Title, e.URL), URL: e.URL,
		Later: true, LaterID: e.ID, LaterDone: e.Done != "", Saved: e.Added, Pending: true,
		Curated: curated[curateKey(e.URL)], Minutes: 1,
	}
}

// itemKind names what a piece is, in the queue's vocabulary.
func itemKind(it Item, post bool) string {
	switch {
	case post:
		return "post"
	case it.Embed != "":
		return "video"
	case it.Podcast():
		return "podcast"
	}
	return "article"
}

// cardType maps the queue's kind onto the card types the client shapes.
func cardType(kind string) string {
	switch kind {
	case "video":
		return TypeVideo
	case "podcast":
		return TypePodcast
	case "post":
		return KindX
	case "article":
		return KindRSS
	}
	return ""
}

// paragraphs renders plain text as escaped paragraphs.
func paragraphs(text string) string {
	var b strings.Builder
	for _, p := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>" + strings.ReplaceAll(escapeText(p), "\n", "<br>") + "</p>")
		}
	}
	return b.String()
}

func escapeText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
