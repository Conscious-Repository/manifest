package calendar

import (
	"context"
	"sync"
	"time"

	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Client reads the primary Google Calendar. It is lazy and graceful: when
// credentials or a token are missing it stays "disabled" and Events returns
// ErrNotConfigured — the app remains fully usable. After a connect/disconnect,
// Reset() makes it pick up the new state without an app restart.
type Client struct {
	ctx   context.Context
	loc   *time.Location
	calID string

	mu  sync.Mutex
	svc *gcal.Service
}

// NewClient builds a client. tz is an IANA name; "" means time.Local.
func NewClient(ctx context.Context, tz string) *Client {
	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return &Client{ctx: ctx, loc: loc, calID: "primary"}
}

func (c *Client) Location() *time.Location { return c.loc }

// service lazily builds the Google service from cached credentials + token.
func (c *Client) service() (*gcal.Service, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.svc != nil {
		return c.svc, nil
	}
	cfg, err := oauthConfig()
	if err != nil {
		return nil, err
	}
	tok, err := loadToken()
	if err != nil {
		return nil, ErrNotConfigured
	}
	ts := &savingTokenSource{src: cfg.TokenSource(c.ctx, tok), last: tok}
	svc, err := gcal.NewService(c.ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, err
	}
	c.svc = svc
	return svc, nil
}

// Reset drops the cached service so the next call re-reads credentials/token.
func (c *Client) Reset() {
	c.mu.Lock()
	c.svc = nil
	c.mu.Unlock()
}

// Enabled reports whether the calendar is authorized and ready.
func (c *Client) Enabled() bool {
	_, err := c.service()
	return err == nil
}

// NeedsAuth reports that credentials exist but no valid token does yet.
func (c *Client) NeedsAuth() bool {
	if _, err := oauthConfig(); err != nil {
		return false
	}
	_, err := loadToken()
	return err != nil
}

// Events returns normalized events overlapping [start, end).
func (c *Client) Events(ctx context.Context, start, end time.Time) ([]Event, error) {
	svc, err := c.service()
	if err != nil {
		return nil, err
	}
	var events []Event
	call := svc.Events.List(c.calID).
		TimeMin(start.Format(time.RFC3339)).
		TimeMax(end.Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime")
	err = call.Pages(ctx, func(page *gcal.Events) error {
		events = append(events, normalize(page.Items, c.loc)...)
		return nil
	})
	return events, err
}

// normalize converts Google events to our Event type. All-day events carry a
// date-only Start.Date; timed events carry an RFC3339 Start.DateTime.
func normalize(items []*gcal.Event, loc *time.Location) []Event {
	var out []Event
	for _, it := range items {
		if it.Start == nil {
			continue
		}
		e := Event{ID: it.Id, Title: it.Summary}
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
