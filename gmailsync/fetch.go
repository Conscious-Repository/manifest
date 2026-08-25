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
	"fmt"
	"net/http"
	"net/url"
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
	http *http.Client
}

// NewClient builds a client over a member's token source (Tokens.Source).
func NewClient(src oauth2.TokenSource) *Client {
	hc := oauth2.NewClient(context.Background(), src)
	hc.Timeout = 60 * time.Second
	return &Client{http: hc}
}

func (c *Client) get(ctx context.Context, u string, into any) error {
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
		return fmt.Errorf("gmail: HTTP %d: %.300s", resp.StatusCode, msg.String())
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// ThreadIDsSince lists thread ids with any activity after `after`
// (epoch-second granularity via the `after:` search operator). The same noise
// filters as the engine's scan: promotions/social/forums/chats excluded
// server-side. One list call; max caps the page (each id costs one more GET).
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
	var list struct {
		Threads []struct {
			ID string `json:"id"`
		} `json:"threads"`
	}
	if err := c.get(ctx, listURL, &list); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list.Threads))
	for _, t := range list.Threads {
		ids = append(ids, t.ID)
	}
	return ids, nil
}

// ThreadFull fetches one thread with full bodies (one GET), returning the
// subject (first message wins) and the ordered messages with decoded
// plaintext bodies.
func (c *Client) ThreadFull(ctx context.Context, id string) (string, []Msg, error) {
	u := "https://gmail.googleapis.com/gmail/v1/users/me/threads/" + url.PathEscape(id) + "?format=full"
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
