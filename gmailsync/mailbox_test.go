package gmailsync

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mailboxTransport func(*http.Request) (*http.Response, error)

func (f mailboxTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestThreadReadPinsMailboxAndCarriesSentLabel(t *testing.T) {
	c := &Client{mailbox: "ben@ooda.group", http: &http.Client{Transport: mailboxTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/gmail/v1/users/ben@ooda.group/threads/exact-thread" {
			t.Fatal(r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"messages":[{"id":"sent","labelIds":["SENT"],"internalDate":"1700000000000","payload":{"headers":[{"name":"From","value":"alias@example.com"}]}}]}`)), Header: make(http.Header)}, nil
	})}}
	_, messages, err := c.ThreadFull(context.Background(), "exact-thread")
	if err != nil || len(messages) != 1 || !messages[0].Sent {
		t.Fatal(messages, err)
	}
}
