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
		feedURL, feedTitle, err := rssFetcher{hc: s.hc}.Discover(ctx, raw)
		if err != nil {
			return Subscription{}, err
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

func (s *Service) due(sub Subscription, now time.Time) bool {
	interval := s.cfg.RSSInterval
	if sub.Kind == KindX {
		interval = s.cfg.XInterval
	}
	st := s.store.read(sub.ID)
	if st.LastPoll == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, st.LastPoll)
	if err != nil {
		return true
	}
	return now.Sub(last) >= interval
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
		s.store.Commit(sub.ID, s.now(), false, nil, nil, err.Error())
		return err
	}
	items, cursors, err := f.Fetch(ctx, sub, s.store.Cursors(sub.ID))
	if err != nil {
		s.store.Commit(sub.ID, s.now(), false, nil, nil, err.Error())
		log.Printf("consume: poll %s: %v", sub.ID, err)
		return err
	}
	s.store.Commit(sub.ID, s.now(), true, items, cursors, "")
	return nil
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
		return rssFetcher{hc: s.hc}, nil
	}
}

// ---- the lane ----

// Card is one CONSUME card. Its shape is the client contract for the lane
// (kindField maps this kind to "consumeItems").
type Card struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // always "consume" — the card type chip
	Type      string `json:"type"` // rss | x, the source flavour
	Source    string `json:"source"`
	List      string `json:"list,omitempty"`
	Author    string `json:"author,omitempty"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Excerpt   string `json:"excerpt"`
	Chars     int    `json:"chars"`
	Published string `json:"published,omitempty"`
	Read      bool   `json:"read"`
	Curated   bool   `json:"curated"`
	Minutes   int    `json:"minutes"` // reading time, so a card says what it costs
}

// readingWPM is the conventional silent-reading rate. The number on the card
// is what makes "read now or later" answerable at a glance.
const readingWPM = 235

// Cards returns the lane. view is "unread" (default) or "all"; list filters to
// one group.
func (s *Service) Cards(view, list string) []Card {
	subs := s.Subscriptions()
	byID := map[string]Subscription{}
	for _, sub := range subs {
		byID[sub.ID] = sub
	}
	curated := s.curatedURLs()

	out := []Card{}
	for _, sub := range subs {
		if list != "" && !strings.EqualFold(sub.List, list) {
			continue
		}
		for _, it := range s.store.Items(sub.ID) {
			if it.DismissedAt != "" && view != "all" {
				continue
			}
			if view != "all" && !it.Unread() {
				continue
			}
			out = append(out, card(it, sub, curated))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Published > out[j].Published })
	return out
}

func card(it Item, sub Subscription, curated map[string]bool) Card {
	published := ""
	if !it.PublishedAt.IsZero() {
		published = it.PublishedAt.Format(time.RFC3339)
	}
	minutes := it.Chars / 5 / readingWPM
	if minutes < 1 {
		minutes = 1
	}
	return Card{
		ID: it.ID, Kind: "consume", Type: sub.Kind,
		Source: firstNonEmpty(it.Source, sub.Title), List: sub.List,
		Author: it.Author, Title: it.Title, URL: it.URL,
		Excerpt: it.Excerpt, Chars: it.Chars, Published: published,
		Read: !it.Unread(), Curated: curated[curateKey(it.URL)], Minutes: minutes,
	}
}

// Unread counts what is waiting, for the lane's own header. It is NOT the FEED
// badge: reading is not attention debt (§5 amendment).
func (s *Service) Unread() int {
	n := 0
	for _, sub := range s.Subscriptions() {
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

// MarkRead / Dismiss are the lane's lifecycle verbs.
func (s *Service) MarkRead(itemID string) bool { return s.mark(itemID, true, false) }
func (s *Service) Dismiss(itemID string) bool  { return s.mark(itemID, false, true) }

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
	Unread  int    `json:"unread"`
	Total   int    `json:"total"`
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
			st.Total++
			if it.Unread() {
				st.Unread++
			}
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
		if sub.List == "" || seen[sub.List] {
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
