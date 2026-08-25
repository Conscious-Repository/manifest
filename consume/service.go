package consume

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/record"
	"manifest/secrets"
)

// Service is the lane: the subscription list in the vault, the poll caches in
// dataDir, and the fetchers between them.
//
// It never touches the vault directly. Reads and writes are injected as
// closures bound to a declared write-capability (§4, §A3) — the same shape
// every other store in this codebase uses, and the reason `rg os.WriteFile` in
// a domain package returns nothing.

// Scheduling. One goroutine walks the whole subscription list rather than one
// goroutine per subscription (the portals/ shape), because subscriptions are
// added and removed at runtime and per-sub goroutines would need a lifecycle
// nobody would remember to maintain.
const (
	tickEvery  = 30 * time.Second
	maxPerTick = 3 // staggers the boot fill: 20 feeds warm over ~3 minutes
)

// Config carries the intervals from the app config.
type Config struct {
	RSSInterval time.Duration
	XInterval   time.Duration
}

// Service holds the lane.
type Service struct {
	store *Store
	hc    *http.Client
	cfg   Config

	// vault access, injected — capability-checked and audited upstream.
	readVault  func(rel string) ([]byte, error)
	writeVault func(rel string, data []byte) error
	listVault  func(dirRel string) ([]string, error)

	curated curatedCache

	// xToken returns the API bearer token ("" when the portal is sealed).
	xToken func() string

	// sites holds the per-domain session cookies that unlock paid publications.
	sites *SiteCreds

	nowFn func() time.Time
	mu    sync.Mutex
}

// VaultIO is the injected vault access. Both halves are required; a Service
// with no vault has nowhere to keep the subscription list.
type VaultIO struct {
	Read  func(rel string) ([]byte, error)
	Write func(rel string, data []byte) error
	// List enumerates vault-relative markdown paths under a directory. It is
	// how the curated projection finds its notes; without it, curation still
	// writes but the public feed has nothing to read.
	List func(dirRel string) ([]string, error)
}

// New builds the service. dataDir holds the caches; io reaches the vault.
func New(dataDir string, io VaultIO, cfg Config) *Service {
	if cfg.RSSInterval <= 0 {
		cfg.RSSInterval = time.Hour
	}
	if cfg.XInterval <= 0 {
		cfg.XInterval = 6 * time.Hour
	}
	return &Service{
		store:      NewStore(dataDir),
		sites:      NewSiteCreds(dataDir),
		hc:         httpClient(),
		cfg:        cfg,
		readVault:  io.Read,
		writeVault: io.Write,
		listVault:  io.List,
		nowFn:      func() time.Time { return time.Now() },
	}
}

// UseXToken supplies the X API token source (Phase 3). Without it, X
// subscriptions report a sealed portal instead of failing obscurely.
func (s *Service) UseXToken(f func() string) { s.xToken = f }

// Sites exposes the site-credential store (the server layer's panel reads it).
func (s *Service) Sites() *SiteCreds { return s.sites }

// cookieFor returns the stored session for a URL's site, or "".
// ⚠ The result is a credential: never log it, never put it in an error.
func (s *Service) cookieFor(rawURL string) string {
	if s.sites == nil {
		return ""
	}
	return s.sites.Cookie(rawURL)
}

func (s *Service) now() time.Time { return s.nowFn().UTC() }

// Store exposes the cache for the server layer's reads.
func (s *Service) Store() *Store { return s.store }

// ---- subscriptions ----

// doc loads extrinsic/feeds.md. A missing file is not an error: it yields the
// scaffold, and the first subscribe writes it.
func (s *Service) doc() (*Doc, error) {
	if s.readVault == nil {
		return nil, errors.New("no vault configured")
	}
	b, err := s.readVault(feedsPath)
	if err != nil {
		return ParseFeeds(""), nil
	}
	return ParseFeeds(string(b)), nil
}

func (s *Service) save(d *Doc) error {
	if s.writeVault == nil {
		return errors.New("consume: no write capability")
	}
	return s.writeVault(feedsPath, []byte(d.String()))
}

// Subscriptions returns the current list, grouped order preserved.
func (s *Service) Subscriptions() []Subscription {
	d, err := s.doc()
	if err != nil {
		return nil
	}
	return d.Subs()
}

// Subscribe resolves what the owner pasted and adds it.
//
// The input is deliberately forgiving — a feed URL, a site URL, a bare
// hostname, or an @handle — because being made to hunt for a site's RSS link
// is the friction that stops a reader from being used.
func (s *Service) Subscribe(ctx context.Context, input, title, list, mirror string) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw := strings.TrimSpace(input)
	if raw == "" {
		return Subscription{}, errors.New("paste a feed URL, a site address, or an @handle")
	}
	d, err := s.doc()
	if err != nil {
		return Subscription{}, err
	}

	sub := Subscription{
		Title:  strings.TrimSpace(title),
		List:   strings.TrimSpace(list),
		Mirror: strings.TrimSpace(strings.ToLower(mirror)),
	}
	if sub.Mirror != MirrorExcerpt {
		sub.Mirror = MirrorFull
	}

	if strings.HasPrefix(raw, "@") || (!strings.Contains(raw, ".") && !strings.Contains(raw, "/")) {
		sub.Kind = KindX
		sub.Handle = strings.TrimPrefix(raw, "@")
		if sub.Title == "" {
			sub.Title = "@" + sub.Handle
		}
	} else {
		feedURL, feedTitle, err := (&rssFetcher{hc: s.hc}).Discover(ctx, raw)
		if err != nil {
			return Subscription{}, err
		}
		// ⚠ [url:: …] is written into extrinsic/feeds.md — the owner's VAULT,
		// a git repo that auto-commits and pushes. A private-feed URL with an
		// embedded token (Substack's podcast feeds are exactly that shape)
		// would put a live credential into version history, where it cannot be
		// recalled. Refuse, and point at the sign-in that keeps the secret in
		// the secrets tier where it belongs.
		if findings := secrets.Scan(feedURL); len(findings) > 0 {
			return Subscription{}, errors.New(
				"that URL carries what looks like a secret, and the subscription list lives in your vault — " +
					"subscribe to the public feed instead and sign in to the site to unlock paid posts")
		}
		sub.Kind = KindRSS
		sub.URL = feedURL
		if sub.Title == "" {
			sub.Title = feedTitle
		}
		if sub.Title == "" {
			sub.Title = raw
		}
	}

	for _, ex := range d.Subs() {
		if sameSource(ex, sub) {
			return Subscription{}, fmt.Errorf("already subscribed to %s", ex.Title)
		}
	}

	d.Add(sub)
	if err := s.save(d); err != nil {
		return Subscription{}, err
	}
	added, _ := d.Find(d.Subs()[len(d.Subs())-1].ID)
	for _, cand := range d.Subs() {
		if sameSource(cand, sub) {
			added = cand
			break
		}
	}
	// Warm it before returning — an empty lane right after subscribing reads
	// as broken. This is synchronous on purpose: Subscribe already made a
	// network call to discover the feed, so the caller is committed to waiting
	// either way, and returning only once items exist means the UI can render
	// them straight away. A detached goroutine here would also outlive
	// shutdown, since it has no lifetime to belong to.
	_ = s.pollOne(ctx, added)
	return added, nil
}

// Seeded counts the items a subscription archived on subscribe. The UI says
// this out loud: a brand-new subscription showing zero unread is correct and
// expected, and without a sentence saying so it reads exactly like the bug
// that prompted the backfill rule.
func (s *Service) Seeded(subID string) int {
	n := 0
	for _, it := range s.store.Items(subID) {
		if it.Seeded() {
			n++
		}
	}
	return n
}

func sameSource(a, b Subscription) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == KindX {
		return strings.EqualFold(a.Handle, b.Handle)
	}
	return strings.EqualFold(strings.TrimRight(a.URL, "/"), strings.TrimRight(b.URL, "/"))
}

// UpdateSub applies an edit (rename, regroup, mirror mode, min-chars).
func (s *Service) UpdateSub(in Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.doc()
	if err != nil {
		return err
	}
	cur, ok := d.Find(in.ID)
	if !ok {
		return fmt.Errorf("no subscription %q", in.ID)
	}
	// Only the owner-editable fields move; identity and source do not.
	cur.Title = strings.TrimSpace(firstNonEmpty(in.Title, cur.Title))
	cur.List = strings.TrimSpace(in.List)
	if m := strings.ToLower(strings.TrimSpace(in.Mirror)); m == MirrorFull || m == MirrorExcerpt {
		cur.Mirror = m
	}
	if in.MinChars >= 0 {
		cur.MinChars = in.MinChars
	}
	switch strings.ToLower(strings.TrimSpace(in.Fulltext)) {
	case FullTextAuto, FullTextOn, FullTextOff:
		cur.Fulltext = strings.ToLower(strings.TrimSpace(in.Fulltext))
	}
	if !d.Update(cur) {
		return fmt.Errorf("no subscription %q", in.ID)
	}
	return s.save(d)
}

// Unsubscribe removes the line and forgets the cache. Curated notes stay:
// unsubscribing is not un-reading, and those notes are the owner's.
func (s *Service) Unsubscribe(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.doc()
	if err != nil {
		return err
	}
	if !d.Remove(id) {
		return fmt.Errorf("no subscription %q", id)
	}
	if err := s.save(d); err != nil {
		return err
	}
	s.store.Forget(id)
	return nil
}

// ---- polling ----

// Start runs the poll loop until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.pollDue(ctx)
			}
		}
	}()
}

// pollDue polls the subscriptions whose interval has elapsed, at most
// maxPerTick of them. The cap is the stagger: without it, a restart with
// twenty feeds fires twenty simultaneous requests.
func (s *Service) pollDue(ctx context.Context) {
	now := s.now()
	n := 0
	for _, sub := range s.Subscriptions() {
		if n >= maxPerTick {
			return
		}
		if !s.due(sub, now) {
			continue
		}
		s.pollOne(ctx, sub)
		n++
	}
}

// maxBackoff caps the failure back-off. Past a day, retrying more often will
// not fix a feed that moved or died — but the subscription stays visible in
// MANAGE with its reason, so it is never silently forgotten.
const maxBackoff = 24 * time.Hour

// due decides whether to poll, honouring three things beyond the configured
// interval: the feed's own <ttl> hint, an explicit Retry-After, and an
// exponential back-off on consecutive failures.
func (s *Service) due(sub Subscription, now time.Time) bool {
	lastPoll, fails, ttl, retryAfter := s.store.Schedule(sub.ID)
	if !retryAfter.IsZero() && now.Before(retryAfter) {
		return false // the publisher told us when to come back
	}
	if lastPoll.IsZero() {
		return true
	}
	return now.Sub(lastPoll) >= s.interval(sub, ttl, fails)
}

// interval is the effective gap between polls for one subscription.
func (s *Service) interval(sub Subscription, ttl time.Duration, fails int) time.Duration {
	base := s.cfg.RSSInterval
	if sub.Kind == KindX {
		base = s.cfg.XInterval
	}
	// A feed asking to be polled at most every 6h gets that, not hourly.
	if ttl > base {
		base = ttl
	}
	if fails <= 0 {
		return base
	}
	backoff := base
	for i := 0; i < fails && backoff < maxBackoff; i++ {
		backoff *= 2
	}
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

// PollNow forces one subscription to poll, for the manage panel's button.
func (s *Service) PollNow(ctx context.Context, id string) error {
	d, err := s.doc()
	if err != nil {
		return err
	}
	sub, ok := d.Find(id)
	if !ok {
		return fmt.Errorf("no subscription %q", id)
	}
	return s.pollOne(ctx, sub)
}

// pollOne is the whole poll transaction: fetch, then exactly one Commit on
// both the success and the failure path.
func (s *Service) pollOne(ctx context.Context, sub Subscription) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	f, err := s.fetcher(sub)
	if err != nil {
		s.store.commit(sub.ID, s.now(), false, nil, nil, err.Error(), PollMeta{})
		return err
	}
	items, cursors, err := f.Fetch(ctx, sub, s.store.Cursors(sub.ID))
	meta := pollMetaOf(err)
	if err != nil {
		s.store.commit(sub.ID, s.now(), false, nil, nil, cleanErr(err), meta)
		log.Printf("consume: poll %s: %v", sub.ID, err) // errors are pre-redacted
		return err
	}
	if m, ok := f.(metaFetcher); ok {
		meta.TTLMinutes = m.LastTTL()
	}
	// Full text for feeds that publish only a teaser — a second fetch, on new
	// items only, never for X (a post is already whole).
	if sub.Kind != KindX && sub.FullText() != FullTextOff {
		items = s.fillFullText(ctx, sub, items)
	}
	s.store.commit(sub.ID, s.now(), true, items, cursors, "", meta)
	s.judgeSession(sub, items)
	return nil
}

// judgeSession decides whether a stored sign-in is still working.
//
// The evidence is already in hand: if this subscription HAS a credential and
// the poll STILL came back with preview-only items, the session is dead —
// signing in is precisely what should have prevented that. If any item came
// back whole, it is alive.
//
// ⚠ Only ever called after a SUCCESSFUL poll. A network failure, a 500, a parse
// error — none of those say anything about the session, and telling the owner
// to re-authenticate because a feed was briefly down would be worse than
// staying quiet.
func (s *Service) judgeSession(sub Subscription, items []Item) {
	if s.sites == nil || sub.Kind == KindX || len(items) == 0 {
		return
	}
	if s.cookieFor(sub.URL) == "" {
		return // nothing signed in here; nothing to judge
	}
	previews, whole := 0, 0
	for _, it := range items {
		if it.Preview != "" {
			previews++
		} else if !it.teaser {
			whole++
		}
	}
	if previews == 0 && whole == 0 {
		return // nothing conclusive in this batch
	}
	s.sites.MarkResult(SiteKey(sub.URL), whole == 0 && previews > 0, s.now())
}

// metaFetcher is the optional half of the Fetcher seam: a fetcher that learned
// a refresh hint from the feed itself.
type metaFetcher interface{ LastTTL() int }

// pollMetaOf lifts a publisher's Retry-After out of a failed poll.
func pollMetaOf(err error) PollMeta {
	var ra *retryAfterError
	if errors.As(err, &ra) {
		return PollMeta{RetryAfter: ra.at}
	}
	return PollMeta{}
}

// cleanErr is what the manage panel shows: the message without the wrapper.
func cleanErr(err error) string {
	var ra *retryAfterError
	if errors.As(err, &ra) {
		return ra.msg
	}
	return err.Error()
}

// Fetcher turns one subscription into items.
//
// This one interface is why X never became a second lane: the official API,
// a self-hosted RSSHub, a Nitter mirror and an ordinary blog are all just
// implementations, and three of those four already speak RSS — so they need no
// implementation at all, only a URL.
type Fetcher interface {
	Fetch(ctx context.Context, sub Subscription, cursors map[string]string) ([]Item, map[string]string, error)
}

func (s *Service) fetcher(sub Subscription) (Fetcher, error) {
	switch sub.Kind {
	case KindX:
		token := ""
		if s.xToken != nil {
			token = s.xToken()
		}
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("X is not connected — add an API token in PORTALS")
		}
		return &xFetcher{hc: s.hc, token: token}, nil
	default:
		return &rssFetcher{hc: s.hc, cookie: s.cookieFor(sub.URL)}, nil
	}
}

// ---- the lane ----

// Card is one CONSUME card. Its shape is the client contract for the lane
// (kindField maps this kind to "consumeItems").
type Card struct {
	ID        string `json:"id"`
	SubID     string `json:"subId"` // which subscription — the client cannot parse ids
	Kind      string `json:"kind"`  // always "consume" — the card type chip
	Type      string `json:"type"`  // rss | x, the source flavour
	Source    string `json:"source"`
	List      string `json:"list,omitempty"`
	Author    string `json:"author,omitempty"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Excerpt   string `json:"excerpt"`
	Chars     int    `json:"chars"`
	Published string `json:"published,omitempty"`
	Read      bool   `json:"read"`
	Seeded    bool   `json:"seeded"`            // archived on subscribe, never actually read
	Preview   string `json:"preview,omitempty"` // paid | partial — the rest cannot be had
	Curated   bool   `json:"curated"`
	Minutes   int    `json:"minutes"` // reading time, so a card says what it costs
}

// readingWPM is the conventional silent-reading rate. The number on the card
// is what makes "read now or later" answerable at a glance.
const readingWPM = 235

// Query is what the lane is being asked for. A struct rather than four
// positional arguments, which is where this was heading.
type Query struct {
	View string // "unread" (default) | "all" — all means everything not dismissed
	List string // group filter
	Sub  string // one subscription's history
	Q    string // free text over title, excerpt, author, source
}

// Cards returns the lane.
//
// "all" includes SEEDED items — the archive of things published before the
// owner subscribed — which is what makes the history browsable. Dismissed is
// excluded from every view; it is terminal by decision (2026-08-25).
func (s *Service) Cards(q Query) []Card {
	curated := s.curatedURLs()
	needle := strings.ToLower(strings.TrimSpace(q.Q))

	out := []Card{}
	for _, sub := range s.Subscriptions() {
		if q.Sub != "" && !strings.EqualFold(sub.ID, q.Sub) {
			continue
		}
		if q.List != "" && !strings.EqualFold(sub.List, q.List) {
			continue
		}
		for _, it := range s.store.Items(sub.ID) {
			// Dismissed means GONE — from unread, from all, from the lane.
			// (Before 2026-08-25 this leaked back into the "all" view, which
			// read as dismiss not working at all.)
			if it.DismissedAt != "" {
				continue
			}
			if q.View != "all" && !it.Unread() {
				continue
			}
			if needle != "" && !matches(it, sub, needle) {
				continue
			}
			out = append(out, card(it, sub, curated))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Published > out[j].Published })
	return out
}

// matches is the search predicate. Bodies live in their own snapshot files and
// are never loaded for a list, so search covers what the card actually shows:
// title, excerpt, author and source.
func matches(it Item, sub Subscription, needle string) bool {
	hay := strings.ToLower(strings.Join([]string{
		it.Title, it.Excerpt, it.Author, it.Source, sub.Title,
	}, " "))
	return strings.Contains(hay, needle)
}

func card(it Item, sub Subscription, curated map[string]bool) Card {
	_ = sub
	published := ""
	if !it.PublishedAt.IsZero() {
		published = it.PublishedAt.Format(time.RFC3339)
	}
	minutes := it.Chars / 5 / readingWPM
	if minutes < 1 {
		minutes = 1
	}
	return Card{
		ID: it.ID, SubID: sub.ID, Kind: "consume", Type: sub.Kind,
		Source: firstNonEmpty(it.Source, sub.Title), List: sub.List,
		Author: it.Author, Title: it.Title, URL: it.URL,
		Excerpt: it.Excerpt, Chars: it.Chars, Published: published,
		// Read means ACTUALLY opened. A seeded item is out of the queue but was
		// never read, and saying otherwise is the lie this state exists to
		// avoid — the UI must be able to tell them apart.
		Read: it.ReadAt != "", Seeded: it.Seeded(), Preview: it.Preview,
		Curated: curated[curateKey(it.URL)], Minutes: minutes,
	}
}

// Unread counts what is waiting, for the lane's own header. It is NOT the FEED
// badge: reading is not attention debt (§5 amendment).
//
// ⚠ It is SCOPED to the same list the caller is looking at. "Mark all read" is
// scoped to the active group, so a global count beside it would still read 66
// after clearing that group — which looks exactly like the button failing.
func (s *Service) Unread(list string) int {
	n := 0
	for _, sub := range s.Subscriptions() {
		if list != "" && !strings.EqualFold(sub.List, list) {
			continue
		}
		for _, it := range s.store.Items(sub.ID) {
			if it.Unread() {
				n++
			}
		}
	}
	return n
}

// subOf recovers the subscription id embedded in an item id
// ("consume:<kind>:<subID>:<hash>").
func subOf(itemID string) (string, bool) {
	parts := strings.Split(itemID, ":")
	if len(parts) != 4 || parts[0] != "consume" {
		return "", false
	}
	return parts[2], true
}

// Get returns one item with its body, for the reader.
func (s *Service) Get(itemID string) (Item, Subscription, bool) {
	subID, ok := subOf(itemID)
	if !ok {
		return Item{}, Subscription{}, false
	}
	d, err := s.doc()
	if err != nil {
		return Item{}, Subscription{}, false
	}
	sub, ok := d.Find(subID)
	if !ok {
		return Item{}, Subscription{}, false
	}
	it, ok := s.store.Get(subID, itemID)
	return it, sub, ok
}

// MarkRead / Dismiss / Undismiss are the lane's lifecycle verbs.
func (s *Service) MarkRead(itemID string) bool { return s.mark(itemID, true, false) }
func (s *Service) Dismiss(itemID string) bool  { return s.mark(itemID, false, true) }

// MarkUnread bumps an item out of the archive and back into the queue.
func (s *Service) MarkUnread(itemID string) bool {
	subID, ok := subOf(itemID)
	if !ok {
		return false
	}
	return s.store.SetUnread(subID, itemID)
}

// Undismiss restores a dismissed item — the undo behind the toast. It clears
// the tombstone too, or the next poll would immediately re-suppress it.
func (s *Service) Undismiss(itemID string) bool {
	subID, ok := subOf(itemID)
	if !ok {
		return false
	}
	return s.store.Undismiss(subID, itemID, s.now())
}

// MarkAllRead clears the unread backlog, optionally scoped to one group. The
// standard escape hatch after a week away.
func (s *Service) MarkAllRead(list string) int {
	n := 0
	for _, sub := range s.Subscriptions() {
		if list != "" && !strings.EqualFold(sub.List, list) {
			continue
		}
		n += s.store.MarkAllRead(sub.ID, s.now())
	}
	return n
}

// PollAll refreshes every subscription regardless of when it is next due, for
// the "refresh now" button. Bounded concurrency: a reader with thirty feeds
// should not open thirty sockets at once.
func (s *Service) PollAll(ctx context.Context) int {
	subs := s.Subscriptions()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for _, sub := range subs {
		wg.Add(1)
		go func(sub Subscription) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := s.pollOne(ctx, sub); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(sub)
	}
	wg.Wait()
	return ok
}

func (s *Service) mark(itemID string, read, dismissed bool) bool {
	subID, ok := subOf(itemID)
	if !ok {
		return false
	}
	return s.store.Mark(subID, itemID, read, dismissed, s.now())
}

// SubStatus is one row of the manage panel.
type SubStatus struct {
	Subscription
	LastOK  string `json:"lastOk,omitempty"`
	LastErr string `json:"lastErr,omitempty"`
	// Site is the credential domain this subscription would use, and how that
	// sign-in is doing. NEVER the cookie itself.
	Site          string `json:"site,omitempty"`
	SignedIn      bool   `json:"signedIn"`
	SignInExpired bool   `json:"signInExpired,omitempty"`
	// Paid marks a subscription whose items are preview-only — the cue to
	// offer signing in.
	Paid     bool `json:"paid,omitempty"`
	Unread   int  `json:"unread"`
	Archived int  `json:"archived"` // seeded or read — browsable, not queued
	Total    int  `json:"total"`
}

// Statuses returns the manage panel's rows: the subscription plus how its last
// poll went. A degraded feed says so here rather than just going quiet.
func (s *Service) Statuses() []SubStatus {
	out := []SubStatus{}
	for _, sub := range s.Subscriptions() {
		lastOK, lastErr := s.store.Status(sub.ID)
		st := SubStatus{Subscription: sub, LastErr: lastErr}
		if !lastOK.IsZero() {
			st.LastOK = lastOK.UTC().Format(time.RFC3339)
		}
		for _, it := range s.store.Items(sub.ID) {
			if it.DismissedAt != "" {
				continue // dismissed is gone, not archived
			}
			st.Total++
			if it.Unread() {
				st.Unread++
			} else {
				st.Archived++
			}
			if it.Preview != "" {
				st.Paid = true
			}
		}
		if sub.Kind != KindX {
			st.Site = SiteKey(sub.URL)
			st.SignedIn = s.cookieFor(sub.URL) != ""
			st.SignInExpired = s.sites != nil && s.sites.Expired(sub.URL)
		}
		out = append(out, st)
	}
	return out
}

// Lists returns the distinct group names, for the filter chips.
func (s *Service) Lists() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, sub := range s.Subscriptions() {
		// "unfiled" is the scaffold's heading for ungrouped feeds, not a group
		// the owner named — it must not appear as a filter chip.
		if sub.List == "" || strings.EqualFold(sub.List, ungrouped) || seen[sub.List] {
			continue
		}
		seen[sub.List] = true
		out = append(out, sub.List)
	}
	sort.Strings(out)
	return out
}

// slugFor is the shared title→filename rule for curated notes.
func slugFor(title string) string { return record.Slug(title, 60) }
