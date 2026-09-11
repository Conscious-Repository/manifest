package sources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fastScholarlyRetries(t *testing.T) *[]time.Duration {
	t.Helper()
	old := scholarlyWait
	delays := []time.Duration{}
	scholarlyWait = func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return ctx.Err() }
	t.Cleanup(func() { scholarlyWait = old })
	return &delays
}

func TestScholarlyAdaptersRetry(t *testing.T) {
	for _, source := range []string{"pubmed", "openalex", "orcid"} {
		for _, tc := range []struct {
			name      string
			statuses  []int
			retry     string
			wantCalls int
			wantError string
		}{
			{"429 twice", []int{429, 429, 200}, "", 3, ""},
			{"exhausted", []int{429}, "", scholarlyAttempts, "HTTP 429: slow down"},
			{"503", []int{503, 200}, "", 2, ""},
			{"retry after", []int{429, 200}, "7", 2, ""},
			{"permanent", []int{400}, "", 1, "HTTP 400"},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				delays := fastScholarlyRetries(t)
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("User-Agent") != PubMedUserAgent || r.Header.Get("Accept") != "application/json" || r.URL.Query().Get("q") != "test" {
						t.Errorf("request changed: %+v", r)
					}
					i := calls
					calls++
					if i >= len(tc.statuses) {
						i = len(tc.statuses) - 1
					}
					w.Header().Set("Retry-After", tc.retry)
					w.WriteHeader(tc.statuses[i])
					if tc.statuses[i] == 200 {
						w.Write([]byte(`{"ok":true}`))
					} else {
						w.Write([]byte("slow down"))
					}
				}))
				defer srv.Close()
				var get func(context.Context, string, url.Values) ([]byte, error)
				switch source {
				case "pubmed":
					get = (PubMed{BaseURL: srv.URL}).get
				case "openalex":
					get = (OpenAlex{BaseURL: srv.URL}).get
				case "orcid":
					get = (ORCID{BaseURL: srv.URL}).get
				}
				body, err := get(context.Background(), "/test", url.Values{"q": {"test"}})
				if tc.wantError == "" {
					if err != nil || string(body) != `{"ok":true}` {
						t.Fatalf("body=%s err=%v", body, err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error=%v", err)
				}
				if calls != tc.wantCalls || len(*delays) != calls-1 {
					t.Fatalf("calls=%d delays=%v", calls, *delays)
				}
				for i, d := range *delays {
					base := (500 * time.Millisecond) << i
					if d < base || tc.retry == "" && d >= base+base/2 {
						t.Fatalf("backoff=%v", *delays)
					}
				}
				if tc.retry != "" && (*delays)[0] != 7*time.Second {
					t.Fatalf("Retry-After ignored: %v", *delays)
				}
			})
		}
	}
}

func TestScholarlyRetryAfterDate(t *testing.T) {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	if got := scholarlyDelay(now.Add(12*time.Second).Format(http.TimeFormat), 0, now); got != 12*time.Second {
		t.Fatal(got)
	}
	for _, value := range []string{"bad", "-1", now.Add(-time.Hour).Format(http.TimeFormat)} {
		if got := scholarlyDelay(value, 0, now); got < 500*time.Millisecond || got >= 750*time.Millisecond {
			t.Fatalf("%s: %v", value, got)
		}
	}
}

func TestScholarlyRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	old := scholarlyWait
	scholarlyWait = func(ctx context.Context, d time.Duration) error { cancel(); return waitScholarlyRetry(ctx, d) }
	defer func() { scholarlyWait = old }()
	_, err := (PubMed{BaseURL: srv.URL}).get(ctx, "/test", nil)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestScholarlyRetryWaitUsesClientDeadline(t *testing.T) {
	// A long Retry-After cannot escape the client's total fetch bound.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := (PubMed{BaseURL: srv.URL, Client: http.Client{Timeout: 20 * time.Millisecond}}).get(ctx, "/test", nil)
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("client deadline did not bound retry: %v, parent=%v", err, ctx.Err())
	}
}

func TestPubMedSearchRecoversAcrossBothFetches(t *testing.T) {
	delays := fastScholarlyRetries(t)
	search := pubmedFixture(t, "pubmed-esearch.json")
	records := pubmedFixture(t, "pubmed-efetch.xml")
	searchCalls, fetchCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pubmedSearchPath:
			searchCalls++
			if searchCalls <= 2 {
				w.WriteHeader(429)
				return
			}
			w.Write([]byte(search))
		case pubmedFetchPath:
			fetchCalls++
			if fetchCalls == 1 {
				w.WriteHeader(503)
				return
			}
			w.Write([]byte(records))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	drafts, err := (PubMed{BaseURL: srv.URL}).Search(context.Background(), Scope{Query: "mri", Max: 5})
	if err != nil || len(drafts) == 0 || searchCalls != 3 || fetchCalls != 2 || len(*delays) != 3 {
		t.Fatalf("drafts=%v err=%v calls=%d/%d waits=%v", drafts, err, searchCalls, fetchCalls, *delays)
	}
	for _, d := range drafts {
		if len(d.Evidence) == 0 {
			t.Fatal("lost citations")
		}
	}
}
