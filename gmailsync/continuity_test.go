package gmailsync

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAnchorGETOnlyPinnedAndMinimal(t *testing.T) {
	for _, body := range []string{`{"id":"t","messages":[{"id":"m","internalDate":"123"}]}`, `{"id":"wrong","messages":[{"id":"m","internalDate":"123"}]}`, `{"id":"t","messages":[{"id":"m","internalDate":"124"}]}`, `{"id":"t","messages":[]}`} {
		c := &Client{mailbox: "owner@example.com", http: &http.Client{Transport: mailboxTransport(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" || r.URL.Path != "/gmail/v1/users/owner@example.com/threads/t" || r.URL.Query().Get("format") != "minimal" || r.URL.Query().Get("fields") != "id,messages(id,internalDate)" || r.Body != nil {
				t.Fatal(r)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}}
		err := c.ThreadAnchor(context.Background(), "t", "m", 123)
		if (err == nil) != (strings.Contains(body, `"id":"t"`) && strings.Contains(body, `"123"`)) {
			t.Fatal(body, err)
		}
	}
}
