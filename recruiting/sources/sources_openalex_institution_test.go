package sources

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// an institution server: the works server's fixtures behind an
// institution filter, plus the two ways an institution resolves.
type openAlexInstServer struct {
	*openAlexWorksServer
	inst *httptest.Server
}

func newOpenAlexInstServer(t *testing.T) *openAlexWorksServer {
	t.Helper()
	works := newOpenAlexWorksServer(t)
	inner := works.srv
	s := &openAlexWorksServer{pages: works.pages, group: works.group, batch: works.batch}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/institutions/I134446601", r.URL.Path == "/institutions/ror:02y3ad647":
			_, _ = w.Write([]byte(`{"id":"https://openalex.org/I134446601","display_name":"Yale University"}`))
		case r.URL.Path == "/institutions" && r.URL.Query().Get("search") != "":
			if strings.Contains(r.URL.Query().Get("search"), "nowhere") {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"id":"https://openalex.org/I134446601","display_name":"Yale University"}]}`))
		default:
			// everything else is the works path: forward to the works server
			resp, err := inner.Client().Get(inner.URL + r.URL.RequestURI())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			w.WriteHeader(resp.StatusCode)
			buf := make([]byte, 1<<20)
			for {
				n, rerr := resp.Body.Read(buf)
				if n > 0 {
					_, _ = w.Write(buf[:n])
				}
				if rerr != nil {
					break
				}
			}
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

var _ = sync.Mutex{}

// An institution sweep is the works path under the registry's own
// institution filter, newest first, with the resolution spelled on every
// draft — and each person dated by their newest work.
func TestOpenAlexInstitutionSweepIsTheWorksPathNewestFirst(t *testing.T) {
	for _, ref := range []string{"I134446601", "https://openalex.org/I134446601", "https://ror.org/02y3ad647", "Yale University"} {
		s := newOpenAlexInstServer(t)
		drafts, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Max: 5, Fields: map[string]string{"institution": ref}})
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if len(drafts) == 0 || ret.Unit != "works" || ret.Read == 0 {
			t.Fatalf("%s: nothing read: %+v", ref, ret)
		}
		var worksReq string
		for _, r := range s.requests() {
			if r.URL.Path == "/works" && r.URL.Query().Get("group_by") == "" {
				worksReq = r.URL.RawQuery
				break
			}
		}
		if !strings.Contains(worksReq, "authorships.institutions.id%3AI134446601") || !strings.Contains(worksReq, "sort=publication_date%3Adesc") {
			t.Fatalf("%s: the works call must filter by institution and read newest first: %s", ref, worksReq)
		}
		if strings.Contains(worksReq, "title_and_abstract.search") {
			t.Fatalf("%s: no query, no text filter: %s", ref, worksReq)
		}
		d := drafts[0]
		if d.Active == 0 {
			t.Fatalf("%s: a person read from dated works is dated: %+v", ref, d)
		}
		found := false
		for _, ev := range d.Evidence {
			if ev.Kind == EvidenceAffiliation && strings.Contains(ev.Snippet, "swept under institution Yale University (I134446601") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: every draft says which institution it was read under: %+v", ref, d.Evidence)
		}
	}
}

func TestOpenAlexInstitutionQueryNarrowsAndUnknownRefuses(t *testing.T) {
	s := newOpenAlexInstServer(t)
	if _, _, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling", Max: 5, Fields: map[string]string{"institution": "Yale University"}}); err != nil {
		t.Fatal(err)
	}
	var worksReq string
	for _, r := range s.requests() {
		if r.URL.Path == "/works" && r.URL.Query().Get("group_by") == "" {
			worksReq = r.URL.Query().Get("filter")
		}
	}
	if !strings.Contains(worksReq, "title_and_abstract.search:field cycling") || !strings.Contains(worksReq, "authorships.institutions.id:I134446601") {
		t.Fatalf("a query narrows the institution's works: %s", worksReq)
	}
	if _, err := s.adapter().Search(context.Background(), Scope{Fields: map[string]string{"institution": "nowhere at all"}}); err == nil || !strings.Contains(err.Error(), "no institution matches") {
		t.Fatalf("an unmatched name refuses in words: %v", err)
	}
}

// The run substrate checks a scope before it fetches; an institution names
// a scope without a query, and the pure check still refuses a bad window.
func TestOpenAlexInstitutionIsAScope(t *testing.T) {
	if _, err := (OpenAlex{}).PrepareScope(Scope{Fields: map[string]string{"institution": "Yale University"}}); err != nil {
		t.Fatalf("an institution is a scope on its own: %v", err)
	}
	if _, err := (OpenAlex{}).PrepareScope(Scope{Fields: map[string]string{"institution": "Yale University", "years": "soon"}}); err == nil {
		t.Fatal("a malformed years window is refused before any fetch")
	}
}

// A name that resolved to an institution with nothing under it is an
// answer in words, naming what was resolved — never a silent empty run.
func TestOpenAlexInstitutionWithNoWorksSaysWhich(t *testing.T) {
	works := newOpenAlexWorksServer(t)
	works.pages = map[string]string{"*": `{"meta":{"count":0},"results":[]}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/institutions" {
			_, _ = w.Write([]byte(`{"results":[{"id":"https://openalex.org/I999","display_name":"Hyperfine Research"}]}`))
			return
		}
		resp, err := works.srv.Client().Get(works.srv.URL + r.URL.RequestURI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	oa := OpenAlex{BaseURL: srv.URL, Client: *srv.Client()}
	_, _, err := oa.SearchCounted(context.Background(), Scope{Fields: map[string]string{"institution": "Hyperfine"}})
	if err == nil || !strings.Contains(err.Error(), "Hyperfine Research (I999, resolved by search Hyperfine) has no works") {
		t.Fatalf("say which institution was read: %v", err)
	}
}
