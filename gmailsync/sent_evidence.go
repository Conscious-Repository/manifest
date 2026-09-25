package gmailsync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// SentEvidence is provider evidence, not authorization or confirmation that an
// approved envelope was sent. A consumer must compare Raw with that envelope
// before recording a successful delivery. Raw must not be logged or published.
type SentEvidence struct {
	Mailbox   string
	ID        string
	ThreadID  string
	MessageID string
	Raw       []byte
}

var ErrSentEvidenceMissing = errors.New("sent evidence not found; delivery remains uncertain")
var ErrSentEvidenceAmbiguous = errors.New("sent evidence is ambiguous; delivery remains uncertain")
var ErrSentEvidenceInvalid = errors.New("sent evidence could not be verified; delivery remains uncertain")

// Restrict query input to the immutable IDs minted by the sending boundary.
// In particular, never let a header become additional Gmail search operators.
var evidenceMessageID = regexp.MustCompile(`^<[A-Za-z0-9._+\-]+@[A-Za-z0-9.\-]+>$`)

const maxSentEvidenceRaw = 32 << 20

// SentMessageEvidence performs two bounded, read-only requests to an explicit
// mailbox. A second result or another page is ambiguous; an empty result is not
// evidence of non-delivery. This method never retries a send or changes state.
func (c *Client) SentMessageEvidence(ctx context.Context, messageID string) (SentEvidence, error) {
	var zero SentEvidence
	a, err := mail.ParseAddress(c.mailbox)
	if err != nil || a.Address != c.mailbox || len(messageID) > 254 || !evidenceMessageID.MatchString(messageID) {
		return zero, ErrSentEvidenceInvalid
	}
	base := "https://gmail.googleapis.com/gmail/v1/users/" + url.PathEscape(c.mailbox) + "/messages"
	query := url.Values{"q": {"in:sent rfc822msgid:" + messageID}, "maxResults": {"2"}, "fields": {"messages(id,threadId),nextPageToken"}}
	var list struct {
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := c.evidenceJSON(ctx, base+"?"+query.Encode(), 64<<10, &list); err != nil {
		return zero, err
	}
	if list.NextPageToken != "" || len(list.Messages) > 1 {
		return zero, ErrSentEvidenceAmbiguous
	}
	if len(list.Messages) == 0 {
		return zero, ErrSentEvidenceMissing
	}
	listed := list.Messages[0]
	if listed.ID == "" || listed.ThreadID == "" {
		return zero, ErrSentEvidenceInvalid
	}
	var message struct {
		ID       string   `json:"id"`
		ThreadID string   `json:"threadId"`
		Labels   []string `json:"labelIds"`
		Raw      string   `json:"raw"`
	}
	u := base + "/" + url.PathEscape(listed.ID) + "?format=raw&fields=id,threadId,labelIds,raw"
	if err := c.evidenceJSON(ctx, u, int64(base64.RawURLEncoding.EncodedLen(maxSentEvidenceRaw))+64<<10, &message); err != nil {
		return zero, err
	}
	if message.ID != listed.ID || message.ThreadID != listed.ThreadID || !slices.Contains(message.Labels, "SENT") {
		return zero, ErrSentEvidenceInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(message.Raw, "="))
	if err != nil || len(raw) == 0 || len(raw) > maxSentEvidenceRaw {
		return zero, ErrSentEvidenceInvalid
	}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil || len(m.Header["Message-Id"]) != 1 || strings.TrimSpace(m.Header.Get("Message-ID")) != messageID {
		return zero, ErrSentEvidenceInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SentEvidence{Mailbox: c.mailbox, ID: message.ID, ThreadID: message.ThreadID, MessageID: messageID, Raw: raw}, nil
}

// No automatic retries: an unavailable read remains explicitly unresolved. The
// caller can repeat this read safely. Provider bodies never enter errors.
func (c *Client) evidenceJSON(ctx context.Context, u string, limit int64, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.http == nil {
		return ErrSentEvidenceInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ErrSentEvidenceInvalid
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrSentEvidenceInvalid
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrSentEvidenceInvalid
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || int64(len(b)) > limit || json.Unmarshal(b, out) != nil {
		return ErrSentEvidenceInvalid
	}
	return nil
}
