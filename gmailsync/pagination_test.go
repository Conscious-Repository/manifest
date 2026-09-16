package gmailsync

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type pageTransport func(*http.Request) (*http.Response, error)

func (f pageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestThreadListingConsumesPages(t *testing.T) {
	calls := 0
	c := &Client{http: &http.Client{Transport: pageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"threads":[{"id":"first"}],"nextPageToken":"next"}`
		if calls == 2 {
			if r.URL.Query().Get("pageToken") != "next" {
				t.Fatal("missing page token")
			}
			body = `{"threads":[{"id":"second"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	ids, err := c.ThreadIDsSince(context.Background(), time.Now(), 100)
	if err != nil || strings.Join(ids, ",") != "first,second" || calls != 2 {
		t.Fatalf("%v %v (%d calls)", ids, err, calls)
	}
}

func TestUnreadThreadRetainsCheckpoint(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	f := &fakeFetch{ids: []string{"missing", "good"}, threads: map[string]struct {
		subject string
		msgs    []Msg
	}{
		"good": {"fixture", []Msg{msgAt("m", "known@example.com", "partner@gmail.com", now, "fixture")}},
	}}
	l, c := testLoop(t, f, fakeRoster{"known@example.com": "Known"})
	before := now.Add(-48 * time.Hour)
	c.AdvanceWatermark("partner@gmail.com", before)
	l.Pass(context.Background())
	if !c.Watermark("partner@gmail.com").Equal(before) {
		t.Fatal("advanced past unread thread")
	}
	if len(c.List(StatusPending)) != 1 {
		t.Fatal("healthy thread was not processed")
	}
}

func TestThreadListingFailureDoesNotReturnPartialPage(t *testing.T) {
	calls := 0
	c := &Client{http: &http.Client{Transport: pageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		status, body := 200, `{"threads":[{"id":"first"}],"nextPageToken":"next"}`
		if calls == 2 {
			status, body = 503, `unavailable`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	ids, err := c.ThreadIDsSince(context.Background(), time.Now(), 100)
	if err == nil || len(ids) != 0 {
		t.Fatalf("partial page must not advance checkpoint: %v %v", ids, err)
	}
}
