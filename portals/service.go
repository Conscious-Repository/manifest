package portals

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Service owns the source portals: the credential store, one disk cache per
// polled portal, and the in-process pollers. It is the whole backend surface the
// server talks to. Calendar and the LLM portals are NOT here — they have their
// own credential stores and the server composes their rows into the panel.
type Service struct {
	store  *Store
	loc    *time.Location // digest day boundary (America/Chicago)
	nowFn  func() time.Time
	hc     *http.Client
	cuBase string // test override
	bnBase string

	mu     sync.Mutex
	caches map[string]*Cache
}

func New(dataDir string, loc *time.Location) *Service {
	if loc == nil {
		loc = time.Local
	}
	return &Service{
		store:  NewStore(dataDir),
		loc:    loc,
		nowFn:  time.Now,
		caches: map[string]*Cache{},
	}
}

func (svc *Service) now() time.Time { return svc.nowFn() }

func (svc *Service) cache(id string) *Cache {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	c, ok := svc.caches[id]
	if !ok {
		c = newCache(svc.dataDir(), id)
		svc.caches[id] = c
	}
	return c
}

// dataDir is recovered from the store's dir (…/portals → parent).
func (svc *Service) dataDir() string {
	return filepathDir(svc.store.dir)
}

func defByID(id string) (Def, bool) {
	for _, d := range Registry {
		if d.ID == id {
			return d, true
		}
	}
	return Def{}, false
}

// ---- panel rows ----

// Rows returns the source-portal rows (clickup, benchling, docusign).
func (svc *Service) Rows() []Row {
	var rows []Row
	for _, d := range Registry {
		rows = append(rows, svc.row(d))
	}
	return rows
}

func (svc *Service) row(d Def) Row {
	r := Row{Def: d}
	if !d.Polled { // docusign — registered, dormant
		r.State = StateDormant
		return r
	}
	if !svc.store.HasCreds(d.ID, d) {
		r.State = StateSealed
		r.Have = svc.store.HaveKeys(d.ID, d) // e.g. tenant set but apiKey not
		return r
	}
	r.Masked = svc.store.Masked(d.ID, d)
	r.Have = svc.store.HaveKeys(d.ID, d)
	lastOK, errMsg := svc.cache(d.ID).Status()
	if errMsg != "" {
		r.State, r.Err = StateDegraded, errMsg
	} else {
		r.State = StateOpen
	}
	if !lastOK.IsZero() {
		r.LastCrossing = lastOK.Format(time.RFC3339)
	}
	return r
}

// ---- credential actions ----

// SetCreds writes the credentials, auto-tests, and (on success) kicks a poll.
func (svc *Service) SetCreds(ctx context.Context, id string, fields map[string]string) (Row, error) {
	d, ok := defByID(id)
	if !ok || !d.Polled {
		return Row{}, errUnknownPortal
	}
	if err := svc.store.SetCreds(id, d, fields); err != nil {
		return Row{}, err
	}
	if svc.store.HasCreds(id, d) {
		svc.test(ctx, d)
		if _, err := svc.cache(id).Status(); err == "" {
			go svc.pollOne(context.Background(), d)
		}
	}
	return svc.row(d), nil
}

// Disconnect clears the credential file and the degraded/cursor state so the row
// returns to sealed.
func (svc *Service) Disconnect(id string) (Row, error) {
	d, ok := defByID(id)
	if !ok {
		return Row{}, errUnknownPortal
	}
	if err := svc.store.Clear(id); err != nil {
		return Row{}, err
	}
	return svc.row(d), nil
}

// Test runs the portal's cheapest authenticated read and records the result.
func (svc *Service) Test(ctx context.Context, id string) (Row, error) {
	d, ok := defByID(id)
	if !ok || !d.Polled {
		return Row{}, errUnknownPortal
	}
	svc.test(ctx, d)
	return svc.row(d), nil
}

func (svc *Service) test(ctx context.Context, d Def) {
	client, err := svc.client(d)
	if err != nil {
		svc.cache(d.ID).Commit(svc.now(), false, nil, nil, err.Error())
		return
	}
	err = client.Test(ctx)
	svc.cache(d.ID).Commit(svc.now(), err == nil, nil, nil, errStr(err))
}

// PollNow runs a full poll immediately (the panel's "poll now").
func (svc *Service) PollNow(ctx context.Context, id string) (Row, error) {
	d, ok := defByID(id)
	if !ok || !d.Polled {
		return Row{}, errUnknownPortal
	}
	svc.pollOne(ctx, d)
	return svc.row(d), nil
}

// ---- pollers ----

type poller interface {
	Test(ctx context.Context) error
	Poll(ctx context.Context, since, now time.Time) ([]Event, map[string]string, error)
}

func (svc *Service) client(d Def) (poller, error) {
	creds := svc.store.Creds(d.ID, d)
	switch d.ID {
	case "clickup":
		return newClickUp(creds["token"], svc.cuBase, svc.hc), nil
	case "benchling":
		return newBenchling(creds["apiKey"], creds["tenant"], svc.bnBase, svc.hc), nil
	}
	return nil, errUnknownPortal
}

// cursorKey is the cache cursor a portal advances (single high-water each).
var cursorKey = map[string]string{"clickup": "task", "benchling": "modified"}

func (svc *Service) pollOne(ctx context.Context, d Def) {
	if !svc.store.HasCreds(d.ID, d) {
		return
	}
	client, err := svc.client(d)
	if err != nil {
		return
	}
	now := svc.now()
	cache := svc.cache(d.ID)
	events, cursors, err := client.Poll(ctx, cache.Cursor(cursorKey[d.ID]), now)
	cache.Commit(now, err == nil, events, cursors, errStr(err))
}

// Start launches one ticker per polled portal; each polls immediately, then on
// its interval, until ctx is cancelled. Call once from main.
func (svc *Service) Start(ctx context.Context) {
	for _, d := range Registry {
		if !d.Polled {
			continue
		}
		d := d
		go func() {
			svc.pollOne(ctx, d) // poll on boot so the feed is warm
			t := time.NewTicker(svc.store.Interval(d.ID, d))
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					svc.pollOne(ctx, d)
				}
			}
		}()
	}
}

// ---- feed cards ----

// Card is a portal item as the FEED renders it — NOT a markdown file, never in
// the engine's feed dir, never kept/discarded. Persisted dismissal lives in the
// portal cache.
type Card struct {
	ID     string        `json:"id"`
	Type   string        `json:"type"`   // portal-item | portal-digest
	Portal string        `json:"portal"` // clickup | benchling (muted source tag)
	Title  string        `json:"title"`
	Detail string        `json:"detail"`
	URL    string        `json:"url"`
	Actor  string        `json:"actor"`
	Date   string        `json:"date"`   // RFC3339
	Pinned bool          `json:"pinned"` // today's digest / fresh, like other digests
	ForYou []DigestLine  `json:"forYou,omitempty"`
	Groups []DigestGroup `json:"groups,omitempty"`
}

// Cards is the deterministic set of portal items for the feed: ClickUp collapses
// to one card per non-empty America/Chicago day; Benchling is one card per
// change. Dismissed and expired items are excluded.
func (svc *Service) Cards() []Card {
	now := svc.now()
	var cards []Card
	// ClickUp — daily digests.
	if svc.store.HasCreds("clickup", mustDef("clickup")) {
		cu := svc.cache("clickup")
		events := cu.Events()
		today := now.In(svc.loc).Format("2006-01-02")
		for _, day := range digestDays(events, svc.loc) {
			id, forYou, groups, at := buildDigest(events, day, svc.loc)
			if len(groups) == 0 || cu.Dismissed(id) {
				continue
			}
			cards = append(cards, Card{
				ID: id, Type: "portal-digest", Portal: "clickup",
				Title: "ClickUp · " + day, Date: at.Format(time.RFC3339),
				Pinned: day == today, ForYou: forYou, Groups: groups,
			})
		}
	}
	// Benchling — itemized.
	if svc.store.HasCreds("benchling", mustDef("benchling")) {
		bn := svc.cache("benchling")
		for _, e := range bn.Events() {
			if bn.Dismissed(e.ID) {
				continue
			}
			cards = append(cards, Card{
				ID: e.ID, Type: "portal-item", Portal: "benchling",
				Title: e.Title, Detail: e.Detail, URL: e.URL, Actor: e.Actor,
				Date: e.At.Format(time.RFC3339), Pinned: false,
			})
		}
	}
	// Newest first; pinned digests float via the client's pin-sort like others.
	sort.Slice(cards, func(i, j int) bool { return cards[i].Date > cards[j].Date })
	return cards
}

// InboxCount is the number of live portal cards (for the FEED badge).
func (svc *Service) InboxCount() int { return len(svc.Cards()) }

// Dismiss suppresses a portal card by id. The id namespaces the portal
// (clickup-digest:… / benchling:…) so it routes to the right cache.
func (svc *Service) Dismiss(cardID string) {
	now := svc.now()
	switch {
	case hasPrefix(cardID, "clickup"):
		svc.cache("clickup").Dismiss(cardID, now)
	case hasPrefix(cardID, "benchling"):
		svc.cache("benchling").Dismiss(cardID, now)
	}
}
