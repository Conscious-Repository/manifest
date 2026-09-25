package gmailsync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const evidenceID = "<0123456789abcdef@example.com>"
const evidenceRaw = "From: owner@example.com\r\nTo: recipient@example.net\r\nMessage-ID: " + evidenceID + "\r\nSubject: frozen\r\n\r\nExact body\r\n"

func evidenceFixture(t *testing.T, list string, edit func(map[string]any), status int) (*Client, *int) {
	t.Helper()
	calls := new(int)
	c := &Client{mailbox: "owner@example.com", http: &http.Client{Transport: mailboxTransport(func(r *http.Request) (*http.Response, error) {
		*calls++
		if r.Method != http.MethodGet {
			t.Fatalf("effect: %s", r.Method)
		}
		body := list
		if *calls == 1 {
			if r.URL.Path != "/gmail/v1/users/owner@example.com/messages" || r.URL.Query().Get("q") != "in:sent rfc822msgid:"+evidenceID || r.URL.Query().Get("maxResults") != "2" {
				t.Fatal(r.URL)
			}
		} else {
			if *calls != 2 || r.URL.Path != "/gmail/v1/users/owner@example.com/messages/provider-id" || r.URL.Query().Get("format") != "raw" {
				t.Fatal(r.URL, *calls)
			}
			m := map[string]any{"id": "provider-id", "threadId": "thread-id", "labelIds": []string{"SENT"}, "raw": base64.RawURLEncoding.EncodeToString([]byte(evidenceRaw))}
			if edit != nil {
				edit(m)
			}
			b, _ := json.Marshal(m)
			body = string(b)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	return c, calls
}

const evidenceList = `{"messages":[{"id":"provider-id","threadId":"thread-id"}]}`

func TestSentEvidenceExactRead(t *testing.T) {
	c, calls := evidenceFixture(t, evidenceList, nil, 200)
	got, err := c.SentMessageEvidence(context.Background(), evidenceID)
	if err != nil || *calls != 2 || got.Mailbox != "owner@example.com" || got.ID != "provider-id" || got.ThreadID != "thread-id" || got.MessageID != evidenceID || string(got.Raw) != evidenceRaw {
		t.Fatal(got, err, *calls)
	}
}
func TestSentEvidenceRefusesUncertainSearch(t *testing.T) {
	for _, tc := range []struct {
		name, list string
		want       error
	}{
		{"missing", `{"messages":[]}`, ErrSentEvidenceMissing},
		{"multiple", `{"messages":[{},{}]}`, ErrSentEvidenceAmbiguous},
		{"page", `{"messages":[{"id":"provider-id","threadId":"thread-id"}],"nextPageToken":"more"}`, ErrSentEvidenceAmbiguous},
		{"empty-page", `{"nextPageToken":"more"}`, ErrSentEvidenceAmbiguous},
		{"no-thread", `{"messages":[{"id":"provider-id"}]}`, ErrSentEvidenceInvalid},
		{"malformed", `{`, ErrSentEvidenceInvalid},
		{"oversize", strings.Repeat(" ", 65<<10), ErrSentEvidenceInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, calls := evidenceFixture(t, tc.list, nil, 200)
			got, err := c.SentMessageEvidence(context.Background(), evidenceID)
			if !errors.Is(err, tc.want) || *calls != 1 || got.ID != "" || got.Raw != nil {
				t.Fatal(got, err, *calls)
			}
		})
	}
}
func TestSentEvidenceRefusesInvalidProviderIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"message", func(m map[string]any) { m["id"] = "different" }},
		{"thread", func(m map[string]any) { m["threadId"] = "different" }},
		{"not-sent", func(m map[string]any) { m["labelIds"] = []string{"INBOX"} }},
		{"base64", func(m map[string]any) { m["raw"] = "!" }},
		{"wrong-header", func(m map[string]any) {
			m["raw"] = base64.RawURLEncoding.EncodeToString([]byte(strings.ReplaceAll(evidenceRaw, evidenceID, "<other@example.com>")))
		}},
		{"duplicate-header", func(m map[string]any) {
			m["raw"] = base64.RawURLEncoding.EncodeToString([]byte("Message-ID: " + evidenceID + "\r\n" + evidenceRaw))
		}},
		{"no-header", func(m map[string]any) {
			m["raw"] = base64.RawURLEncoding.EncodeToString([]byte("From: owner@example.com\r\n\r\nbody"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := evidenceFixture(t, evidenceList, tc.edit, 200)
			got, err := c.SentMessageEvidence(context.Background(), evidenceID)
			if !errors.Is(err, ErrSentEvidenceInvalid) || got.ID != "" || got.Raw != nil {
				t.Fatal(got, err)
			}
		})
	}
}
func TestSentEvidenceRejectsUnqualifiedInputsAndCancellation(t *testing.T) {
	for _, mailbox := range []string{"", "me", "Owner <owner@example.com>", "owner@example.com"} {
		ids := []string{evidenceID}
		if mailbox == "owner@example.com" {
			ids = []string{"", "<id@example.com> OR in:inbox", "<id@example.com>\r\n", "<" + strings.Repeat("a", 255) + "@example.com>"}
		}
		for _, id := range ids {
			c, calls := evidenceFixture(t, evidenceList, nil, 200)
			c.mailbox = mailbox
			_, err := c.SentMessageEvidence(context.Background(), id)
			if !errors.Is(err, ErrSentEvidenceInvalid) || *calls != 0 {
				t.Fatal(mailbox, id, err, *calls)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, calls := evidenceFixture(t, evidenceList, nil, 200)
	_, err := c.SentMessageEvidence(ctx, evidenceID)
	if !errors.Is(err, context.Canceled) || *calls != 0 {
		t.Fatal(err, *calls)
	}
	c.http.Transport = mailboxTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel = context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.SentMessageEvidence(ctx, evidenceID); done <- err }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSentEvidenceRedactsReadFailure(t *testing.T) {
	c, calls := evidenceFixture(t, "PRIVATE PROVIDER CONTENT", nil, 503)
	_, err := c.SentMessageEvidence(context.Background(), evidenceID)
	if !errors.Is(err, ErrSentEvidenceInvalid) || *calls != 1 || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal(err, *calls)
	}
}

func TestSentEvidenceBodyBoundsAndLateCancellation(t *testing.T) {
	t.Run("decoded-limit", func(t *testing.T) {
		c, _ := evidenceFixture(t, evidenceList, func(m map[string]any) {
			m["raw"] = base64.RawURLEncoding.EncodeToString([]byte(evidenceRaw + strings.Repeat("x", maxSentEvidenceRaw)))
		}, 200)
		got, err := c.SentMessageEvidence(context.Background(), evidenceID)
		if !errors.Is(err, ErrSentEvidenceInvalid) || got.Raw != nil {
			t.Fatal(err)
		}
	})
	t.Run("late-cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c, _ := evidenceFixture(t, evidenceList, func(m map[string]any) { cancel() }, 200)
		got, err := c.SentMessageEvidence(ctx, evidenceID)
		if !errors.Is(err, context.Canceled) || got.Raw != nil {
			t.Fatal(err)
		}
	})
	t.Run("failed-second-read", func(t *testing.T) {
		c, calls := evidenceFixture(t, evidenceList, nil, 200)
		original := c.http.Transport
		c.http.Transport = mailboxTransport(func(r *http.Request) (*http.Response, error) {
			if *calls == 1 {
				return nil, errors.New("PRIVATE TRANSPORT ERROR")
			}
			return original.RoundTrip(r)
		})
		got, err := c.SentMessageEvidence(context.Background(), evidenceID)
		if !errors.Is(err, ErrSentEvidenceInvalid) || got.Raw != nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal(err)
		}
	})
}
