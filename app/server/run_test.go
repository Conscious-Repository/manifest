package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/feed"
	"manifest/hermes"
	"manifest/profiles"
)

// fakeHermes stands in for the Hermes box: /v1/chat/completions returns a single
// non-streaming completion whose content is `reply`. Lets us exercise the full
// run-and-materialize path (handler → RunOnce → Materialize) without the live box.
func fakeHermes(t *testing.T, reply string) *hermes.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": reply}}},
		})
	}))
	t.Cleanup(srv.Close)
	return hermes.NewClient(hermes.Config{BaseURL: srv.URL, APIKey: "test", Model: "hermes-agent"})
}

func TestHandleFeedRun_MaterializesArtifact(t *testing.T) {
	dir := t.TempDir()
	profs := profiles.NewStore(dir)
	if _, err := profs.Save(profiles.Profile{Name: "options-scout", Permissions: []string{"read-only"}, Brief: "compare options"}); err != nil {
		t.Fatal(err)
	}
	fd := feed.NewStore(dir)
	reply := "Here are your options:\n```json\n" +
		`[{"type":"artifact","title":"3D printers under $2k","why":"best value pick","body":"| model | price |"}]` +
		"\n```"
	s := New(nil, nil, nil)
	s.UseHermes(fakeHermes(t, reply))
	s.UseProfiles(profs)
	s.UseFeed(fd)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/feed/run",
		strings.NewReader(`{"profile":"options-scout","request":"buy a 3d printer, find 5 options"}`))
	s.handleFeedRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	items := fd.List(feed.Filter{}, time.Now())
	if len(items) != 1 {
		t.Fatalf("want 1 materialized item, got %d", len(items))
	}
	if items[0].Type != "artifact" || items[0].Profile != "options-scout" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestHandleFeedRun_RequiresProfileAndRequest(t *testing.T) {
	dir := t.TempDir()
	s := New(nil, nil, nil)
	s.UseHermes(fakeHermes(t, "[]"))
	s.UseProfiles(profiles.NewStore(dir))
	s.UseFeed(feed.NewStore(dir))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/feed/run", strings.NewReader(`{"profile":"","request":""}`))
	s.handleFeedRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty profile/request: status = %d, want 400", rec.Code)
	}
}

func TestHandleApprovalRun_DraftsToPending(t *testing.T) {
	dir := t.TempDir()
	profs := profiles.NewStore(dir)
	if _, err := profs.Save(profiles.Profile{Name: "ea-coordinator", Permissions: []string{"propose-only"}, Brief: "draft only"}); err != nil {
		t.Fatal(err)
	}
	ap := approvals.NewStore(dir)
	reply := "Drafted:\n```json\n" +
		`[{"action":"Send email to Lee","body":"Hi Lee, are you free Tue or Wed?"}]` +
		"\n```"
	s := New(nil, nil, nil)
	s.UseHermes(fakeHermes(t, reply))
	s.UseProfiles(profs)
	s.UseApprovals(ap)

	rec := httptest.NewRecorder()
	// No profile field → defaults to ea-coordinator.
	req := httptest.NewRequest(http.MethodPost, "/api/agents/approvals/run",
		strings.NewReader(`{"request":"draft a reply to Lee with time suggestions"}`))
	s.handleApprovalRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	pending := ap.List("pending")
	if len(pending) != 1 || pending[0].Action != "Send email to Lee" {
		t.Fatalf("want 1 pending 'Send email to Lee', got %+v", pending)
	}
	// Draft-only invariant: nothing is auto-approved or -rejected.
	if n := len(ap.List("approved")) + len(ap.List("rejected")); n != 0 {
		t.Fatalf("run should only draft to pending; found %d decided", n)
	}
}

func TestHandleApprovalRun_RequiresRequest(t *testing.T) {
	dir := t.TempDir()
	profs := profiles.NewStore(dir)
	_, _ = profs.Save(profiles.Profile{Name: "ea-coordinator", Brief: "x"})
	s := New(nil, nil, nil)
	s.UseHermes(fakeHermes(t, "[]"))
	s.UseProfiles(profs)
	s.UseApprovals(approvals.NewStore(dir))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agents/approvals/run", strings.NewReader(`{"request":"  "}`))
	s.handleApprovalRun(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank request: status = %d, want 400", rec.Code)
	}
}
