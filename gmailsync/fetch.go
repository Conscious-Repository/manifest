package gmailsync

// The minimal Gmail REST surface the sync loop needs, ported from the
// engine's casts/gmail.go (ThreadIDsSince / ThreadFull / MIME-part walking) so
// the two pipelines agree on what a "message" is. Read-only: the token's only
// scope is gmail.readonly, and the only verbs here are GETs.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Msg is one fetched message of a full thread — the deterministic conversion
// input. Body is decoded plaintext (or a stripped HTML fallback), verbatim
// sender text.
type Msg struct {
	Sent        bool // provider SENT label; excludes aliases of the sending mailbox
	ID          string
	From        string
	To          string
	Cc          string
	Subject     string
	Internal    time.Time
	Body        string
	HasCalendar bool // carries a text/calendar MIME part (invite/RSVP machinery)
	// MessageID is the RFC 822 Message-ID header — the ONLY identity that is
	// the same in every mailbox holding a copy of this message. Gmail thread
	// ids are mailbox-local, which is why the same conversation used to
	// surface once per member in the FEED.
	MessageID string
}

// Client is one member's read-only mailbox handle.
type Client struct {
	mailbox string
	http    *http.Client
	pace    time.Duration
	wait    func(context.Context, time.Duration) error
}

// NewClient builds a client over a member's token source (Tokens.Source).
func NewClient(src oauth2.TokenSource) *Client {
	hc := oauth2.NewClient(context.Background(), src)
	hc.Timeout = 60 * time.Second
	return &Client{http: hc, pace: time.Second}
}

type gmailReadError struct {
	status    int
	message   string
	retryable bool
}

func (e *gmailReadError) Error() string {
	return fmt.Sprintf("gmail: HTTP %d: %.300s", e.status, e.message)
}
func waitForMail(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (c *Client) get(ctx context.Context, u string, into any) error {
	wait := c.wait
	if wait == nil {
		wait = waitForMail
	}
	for attempt := 0; ; attempt++ {
		delay := c.pace
		if attempt > 0 {
			delay = time.Second * time.Duration(1<<uint(attempt-1))
		}
		if delay > 0 {
			if err := wait(ctx, delay); err != nil {
				return err
			}
		}
		err := c.getOnce(ctx, u, into)
		var apiErr *gmailReadError
		if err == nil || !errors.As(err, &apiErr) || !apiErr.retryable || attempt >= 7 {
			return err
		}
	}
}
func (c *Client) getOnce(ctx context.Context, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		_, _ = msg.ReadFrom(resp.Body)
		body := msg.String()
		rate := resp.StatusCode == 429 || (resp.StatusCode == 403 && (strings.Contains(body, "rateLimitExceeded") || strings.Contains(body, "userRateLimitExceeded") || strings.Contains(body, "Quota exceeded")))
		return &gmailReadError{resp.StatusCode, body, rate || resp.StatusCode >= 500}
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// ThreadIDsSince lists thread ids with any activity after `after`
// (epoch-second granularity via the `after:` search operator). The same noise
// filters as the engine's scan: promotions/social/forums/chats excluded
// server-side. All pages are consumed; max controls each page size. A listing failure
// returns no partial result so the caller cannot advance past unseen mail.
func (c *Client) ThreadIDsSince(ctx context.Context, after time.Time, max int) ([]string, error) {
	if max <= 0 {
		max = 50
	}
	if max > 100 {
		max = 100
	}
	q := fmt.Sprintf("after:%d -category:promotions -category:social -category:forums -in:chats", after.Unix())
	listURL := "https://gmail.googleapis.com/gmail/v1/users/me/threads?maxResults=" +
		strconv.Itoa(max) + "&q=" + url.QueryEscape(q)
	var ids []string
	page := ""
	seen := map[string]bool{}
	for {
		u := listURL
		if page != "" {
			u += "&pageToken=" + url.QueryEscape(page)
		}
		var list struct {
			Threads []struct {
				ID string `json:"id"`
			} `json:"threads"`
			Next string `json:"nextPageToken"`
		}
		if err := c.get(ctx, u, &list); err != nil {
			return nil, err
		}
		for _, t := range list.Threads {
			ids = append(ids, t.ID)
		}
		if list.Next == "" {
			return ids, nil
		}
		if seen[list.Next] {
			return nil, fmt.Errorf("gmail: repeated thread page token")
		}
		seen[list.Next] = true
		page = list.Next
	}

}

// NewMailboxClient pins requests to the exact connected account.
func NewMailboxClient(src oauth2.TokenSource, mailbox string) *Client {
	c := NewClient(src)
	c.mailbox = mailbox
	return c
}

// ThreadFull fetches one thread with full bodies (one GET), returning the
// subject (first message wins) and the ordered messages with decoded
// plaintext bodies.
func (c *Client) ThreadFull(ctx context.Context, id string) (string, []Msg, error) {
	mailbox := c.mailbox
	if mailbox == "" {
		mailbox = "me"
	}
	u := "https://gmail.googleapis.com/gmail/v1/users/" + url.PathEscape(mailbox) + "/threads/" + url.PathEscape(id) + "?format=full"
	var tr struct {
		Messages []gmailMessage `json:"messages"`
	}
	if err := c.get(ctx, u, &tr); err != nil {
		return "", nil, err
	}
	if len(tr.Messages) == 0 {
		return "", nil, fmt.Errorf("empty thread")
	}
	subject := tr.Messages[0].header("Subject")
	if subject == "" {
		subject = tr.Messages[len(tr.Messages)-1].header("Subject")
	}
	msgs := make([]Msg, 0, len(tr.Messages))
	for _, m := range tr.Messages {
		var when time.Time
		if ms, err := strconv.ParseInt(strings.TrimSpace(m.InternalDate), 10, 64); err == nil {
			when = time.UnixMilli(ms)
		}
		msgs = append(msgs, Msg{
			ID:          m.ID,
			Sent:        slices.Contains(m.LabelIDs, "SENT"),
			From:        m.header("From"),
			To:          m.header("To"),
			Cc:          m.header("Cc"),
			Subject:     m.header("Subject"),
			MessageID:   strings.Trim(strings.TrimSpace(m.header("Message-Id")), "<>"),
			Internal:    when,
			Body:        partPlainText(m.Payload),
			HasCalendar: hasPartType(m.Payload, "text/calendar"),
		})
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].Internal.Before(msgs[j].Internal) })
	return subject, msgs, nil
}

type gmailMessage struct {
	LabelIDs     []string  `json:"labelIds"`
	ID           string    `json:"id"`
	InternalDate string    `json:"internalDate"` // ms epoch, string
	Payload      gmailPart `json:"payload"`
}

// gmailPart is one MIME part of a format=full message — recursive, so the
// text/plain leaf can be found under multipart/alternative etc.
type gmailPart struct {
	MimeType string `json:"mimeType"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Body struct {
		Data string `json:"data"` // base64url
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

func (m gmailMessage) header(name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// partPlainText walks a message's MIME tree for the first text/plain leaf,
// falling back to a tag-stripped text/html leaf.
func partPlainText(p gmailPart) string {
	if s, ok := findPart(p, "text/plain"); ok {
		return s
	}
	if s, ok := findPart(p, "text/html"); ok {
		return stripHTML(s)
	}
	return ""
}

func hasPartType(p gmailPart, mime string) bool {
	if strings.HasPrefix(strings.ToLower(p.MimeType), mime) {
		return true
	}
	for _, child := range p.Parts {
		if hasPartType(child, mime) {
			return true
		}
	}
	return false
}

func findPart(p gmailPart, mime string) (string, bool) {
	if strings.HasPrefix(strings.ToLower(p.MimeType), mime) && p.Body.Data != "" {
		if b, err := base64.RawURLEncoding.DecodeString(p.Body.Data); err == nil {
			return string(b), true
		}
	}
	for _, child := range p.Parts {
		if s, ok := findPart(child, mime); ok {
			return s, true
		}
	}
	return "", false
}
