package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

func TestSnapshotIdentityOrderingAndPartialReads(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANIFEST_CONFIG_DIR", dir)
	writeCreds(t, dir)
	fast := make(chan struct{})
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/calendarList") || strings.Contains(r.URL.Path, "/failed/") || r.URL.Query().Get("pageToken") == "later" {
			http.Error(w, "SECRET_PROVIDER_ERROR", 503)
			return
		}
		if strings.Contains(r.URL.Path, "/a/") {
			<-fast
		}
		if strings.Contains(r.URL.Path, "/z/") {
			once.Do(func() { close(fast) })
		}
		next := ""
		if strings.Contains(r.URL.Path, "/paged/") {
			next = `,"nextPageToken":"later"`
		}
		w.Write([]byte(`{"items":[{"id":"same-event","summary":"Meeting","start":{"dateTime":"2026-09-25T10:00:00Z"},"end":{"dateTime":"2026-09-25T11:00:00Z"}}]` + next + `}`))
	}))
	defer provider.Close()
	svc, err := gcal.NewService(context.Background(), option.WithHTTPClient(provider.Client()), option.WithEndpoint(provider.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{ctx: context.Background(), loc: time.UTC, accounts: []*account{{email: "one@example.test", svc: svc, calIDs: []string{"z", "a", "failed", "paged"}}, {email: "two@example.test", svc: svc, calIDs: []string{"a"}}, {email: "unavailable@example.test", svc: svc}}}
	start := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	snapshot, err := c.EventsSnapshot(context.Background(), start, start.Add(24*time.Hour))
	if err != nil || !snapshot.Partial || len(snapshot.Events) != 4 || len(snapshot.Issues) != 3 {
		t.Fatal(snapshot, err)
	}
	want := []string{"a", "paged", "z", "a"}
	keys := map[string]bool{}
	for i, e := range snapshot.Events {
		if e.CalendarID != want[i] || e.Key() == "" || keys[e.Key()] {
			t.Fatal("source identity/order lost", snapshot.Events)
		}
		keys[e.Key()] = true
	}
	if snapshot.Events[3].Account != "two@example.test" {
		t.Fatal("account identity lost")
	}
	raw, _ := json.Marshal(snapshot.Issues)
	if strings.Contains(string(raw), "SECRET_PROVIDER") {
		t.Fatal("provider body retained")
	}
	again, err := c.EventsSnapshot(context.Background(), start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range again.Events {
		if e.Key() != snapshot.Events[i].Key() {
			t.Fatal("unstable source identity")
		}
	}
	source := NewSource(c, t.TempDir())
	source.writeCache("2026-09-25", []Slot{{Token: "8A", Title: "KNOWN COMPLETE", EventID: "old"}})
	before, _ := os.ReadFile(source.cachePath("2026-09-25"))
	if _, err := source.Slots("2026-09-25"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(source.cachePath("2026-09-25"))
	if string(after) != string(before) {
		t.Fatal("partial read replaced complete offline mirror")
	}
	c.accounts = []*account{{email: "one@example.test", svc: svc, calIDs: []string{"failed"}}}
	if s, err := c.EventsSnapshot(context.Background(), start, start.Add(24*time.Hour)); err == nil || !s.Partial || len(s.Events) != 0 {
		t.Fatal("total failure looked complete", s, err)
	}
	if (Event{ID: "provider-only"}).Key() != "" {
		t.Fatal("unqualified provider ID became durable identity")
	}
}
