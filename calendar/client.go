package calendar

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"golang.org/x/oauth2"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// account is one connected Google account: its token plus a lazily-built service
// and a cached list of the calendars to read from it.
type account struct {
	email  string // canonical; "" for an un-resolved legacy token until first use
	token  *oauth2.Token
	svc    *gcal.Service
	calIDs []string
}

// Client reads Google Calendar across one or more connected accounts. It is lazy
// and graceful: with no credentials or accounts it stays "disabled" and Events
// returns ErrNotConfigured — the app remains fully usable. Reset() (and
// add/remove) re-read disk so changes take effect without an app restart.
type Client struct {
	ctx context.Context
	loc *time.Location

	mu       sync.Mutex
	accounts []*account
}

// NewClient builds a client. tz is an IANA name; "" means time.Local.
func NewClient(ctx context.Context, tz string) *Client {
	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	c := &Client{ctx: ctx, loc: loc}
	c.reload()
	return c
}

func (c *Client) Location() *time.Location { return c.loc }

// reload rebuilds the in-memory account list from disk, dropping cached services
// and calendar lists.
func (c *Client) reload() {
	loaded := loadAllAccounts()
	c.mu.Lock()
	c.accounts = make([]*account, 0, len(loaded))
	for _, at := range loaded {
		c.accounts = append(c.accounts, &account{email: at.Email, token: at.Token})
	}
	c.mu.Unlock()
}

// Reset re-reads accounts (after connect/disconnect) and clears caches.
func (c *Client) Reset() { c.reload() }

// HasCreds reports whether the shared OAuth client credentials are present.
func (c *Client) HasCreds() bool {
	_, err := oauthConfig()
	return err == nil
}

// Enabled reports that at least one account is connected (and creds exist). It is
// presence-based (no network), so it stays cheap and offline-safe.
func (c *Client) Enabled() bool {
	if !c.HasCreds() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.accounts) > 0
}

// NeedsAuth reports that credentials exist but no account is connected yet.
func (c *Client) NeedsAuth() bool {
	if !c.HasCreds() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.accounts) == 0
}

// Accounts returns the connected account emails, sorted.
func (c *Client) Accounts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, a := range c.accounts {
		if a.email != "" {
			out = append(out, a.email)
		}
	}
	sort.Strings(out)
	return out
}

// AddAccount runs the OAuth flow, resolves the account email, and saves its token.
func (c *Client) AddAccount(ctx context.Context) (string, error) {
	cfg, err := oauthConfig()
	if err != nil {
		return "", err
	}
	tok, err := Authorize(ctx)
	if err != nil {
		return "", err
	}
	svc, err := gcal.NewService(ctx, option.WithTokenSource(cfg.TokenSource(ctx, tok)))
	if err != nil {
		return "", err
	}
	email, err := primaryEmail(ctx, svc)
	if err != nil {
		return "", err
	}
	if email == "" {
		return "", fmt.Errorf("could not resolve the Google account email")
	}
	if err := saveAccountToken(email, tok); err != nil {
		return "", err
	}
	c.reload() // pick up the new account (overwrites a duplicate email)
	return email, nil
}

// RemoveAccount disconnects an account by deleting its token file.
func (c *Client) RemoveAccount(email string) error {
	if err := removeAccountToken(email); err != nil {
		return err
	}
	c.reload()
	return nil
}

// Events returns normalized events overlapping [start, end), merged across every
// connected account and all of its (title-bearing) calendars. One failing account
// or calendar never aborts the rest; results are concatenated in stable account
// order so EventsToSlots's start-time sort keeps "first wins" deterministic.
func (c *Client) Events(ctx context.Context, start, end time.Time) ([]Event, error) {
	if _, err := oauthConfig(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	accts := append([]*account(nil), c.accounts...)
	c.mu.Unlock()
	if len(accts) == 0 {
		return nil, ErrNotConfigured
	}

	// Phase A: resolve each account's service + calendar ids (concurrent).
	type run struct {
		svc *gcal.Service
		ids []string
		err error
	}
	runs := make([]run, len(accts))
	var wgA sync.WaitGroup
	for i, a := range accts {
		wgA.Add(1)
		go func(i int, a *account) {
			defer wgA.Done()
			svc, err := c.ensureSvc(a)
			if err != nil {
				runs[i].err = err
				return
			}
			ids, err := c.ensureCalIDs(ctx, a, svc)
			runs[i] = run{svc: svc, ids: ids, err: err}
		}(i, a)
	}
	wgA.Wait()

	// Phase B: fetch events per (account, calendar), bounded; collect per account.
	perAcct := make([][]Event, len(accts))
	errByAcct := make([]error, len(accts))
	locks := make([]sync.Mutex, len(accts))
	sem := make(chan struct{}, 8)
	var wgB sync.WaitGroup
	for i := range runs {
		if runs[i].err != nil {
			errByAcct[i] = runs[i].err
			continue
		}
		for _, calID := range runs[i].ids {
			wgB.Add(1)
			sem <- struct{}{}
			go func(i int, svc *gcal.Service, calID string) {
				defer wgB.Done()
				defer func() { <-sem }()
				evs, err := fetchCalendar(ctx, svc, calID, start, end, c.loc)
				locks[i].Lock()
				perAcct[i] = append(perAcct[i], evs...)
				if err != nil && errByAcct[i] == nil {
					errByAcct[i] = err
				}
				locks[i].Unlock()
			}(i, runs[i].svc, calID)
		}
	}
	wgB.Wait()

	var merged []Event
	anyOK := false
	var firstErr error
	for i := range accts {
		merged = append(merged, perAcct[i]...)
		if errByAcct[i] == nil {
			anyOK = true
		} else if firstErr == nil {
			firstErr = errByAcct[i]
		}
	}
	if !anyOK && firstErr != nil {
		return nil, firstErr // every account failed -> let Source fall back to cache
	}
	return merged, nil
}

func (c *Client) ensureSvc(a *account) (*gcal.Service, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a.svc != nil {
		return a.svc, nil
	}
	cfg, err := oauthConfig()
	if err != nil {
		return nil, err
	}
	ts := &savingTokenSource{email: a.email, src: cfg.TokenSource(c.ctx, a.token), last: a.token}
	svc, err := gcal.NewService(c.ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, err
	}
	a.svc = svc
	return svc, nil
}

// ensureCalIDs lists (and caches) the account's calendars, skipping deleted and
// free/busy-only ones (which have no titles). It also resolves a legacy
// empty-email account's address from the Primary entry and migrates its token.
func (c *Client) ensureCalIDs(ctx context.Context, a *account, svc *gcal.Service) ([]string, error) {
	c.mu.Lock()
	cached := a.calIDs
	c.mu.Unlock()
	if len(cached) > 0 {
		return cached, nil
	}
	var ids []string
	var primary string
	pageToken := ""
	for {
		call := svc.CalendarList.List().ShowDeleted(false).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			if it.Deleted || it.AccessRole == "freeBusyReader" {
				continue
			}
			ids = append(ids, it.Id)
			if it.Primary {
				primary = it.Id
			}
		}
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}
	c.mu.Lock()
	a.calIDs = ids
	if a.email == "" && primary != "" { // migrate a legacy single-token account
		a.email = primary
		_ = saveAccountToken(primary, a.token)
		_ = os.Remove(legacyToken())
		a.svc = nil // rebuild next time with the email-aware saving token source
	}
	c.mu.Unlock()
	return ids, nil
}

// primaryEmail returns the id of the account's primary calendar, which equals the
// account's email address.
func primaryEmail(ctx context.Context, svc *gcal.Service) (string, error) {
	pageToken := ""
	for {
		call := svc.CalendarList.List().ShowDeleted(false).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return "", err
		}
		for _, it := range res.Items {
			if it.Primary {
				return it.Id, nil
			}
		}
		if res.NextPageToken == "" {
			return "", nil
		}
		pageToken = res.NextPageToken
	}
}

func fetchCalendar(ctx context.Context, svc *gcal.Service, calID string, start, end time.Time, loc *time.Location) ([]Event, error) {
	var out []Event
	pageToken := ""
	for {
		call := svc.Events.List(calID).
			TimeMin(start.Format(time.RFC3339)).
			TimeMax(end.Format(time.RFC3339)).
			SingleEvents(true).
			OrderBy("startTime").
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return out, err
		}
		out = append(out, normalize(res.Items, loc)...)
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}
	return out, nil
}

// normalize converts Google events to our Event type. All-day events carry a
// date-only Start.Date; timed events carry an RFC3339 Start.DateTime.
func normalize(items []*gcal.Event, loc *time.Location) []Event {
	var out []Event
	for _, it := range items {
		if it.Start == nil {
			continue
		}
		if it.Status == "cancelled" {
			continue // cancelled instances (SingleEvents expansion) aren't real events
		}
		e := Event{ID: it.Id, Title: it.Summary, Declined: selfDeclined(it), Attendees: attendeesOf(it)}
		if it.Start.Date != "" {
			e.AllDay = true
			e.Start, _ = time.ParseInLocation("2006-01-02", it.Start.Date, loc)
			if it.End != nil && it.End.Date != "" {
				e.End, _ = time.ParseInLocation("2006-01-02", it.End.Date, loc)
			} else {
				e.End = e.Start.Add(24 * time.Hour)
			}
		} else {
			e.Start, _ = time.Parse(time.RFC3339, it.Start.DateTime)
			if it.End != nil && it.End.DateTime != "" {
				e.End, _ = time.Parse(time.RFC3339, it.End.DateTime)
			} else {
				e.End = e.Start.Add(30 * time.Minute)
			}
		}
		out = append(out, e)
	}
	return out
}

// attendeesOf returns the non-self participants (name + email) for contact
// matching. The self record is skipped (it is the summoner, not a counterparty).
func attendeesOf(it *gcal.Event) []Attendee {
	var out []Attendee
	for _, a := range it.Attendees {
		if a.Self || a.Resource {
			continue
		}
		out = append(out, Attendee{Name: a.DisplayName, Email: a.Email})
	}
	return out
}

// selfDeclined reports whether the authenticated account's own attendee record
// on this event has a "declined" RSVP. Google marks that record with Self=true,
// so no email matching is needed.
func selfDeclined(it *gcal.Event) bool {
	for _, a := range it.Attendees {
		if a.Self && a.ResponseStatus == "declined" {
			return true
		}
	}
	return false
}
