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

// waitRun asserts a run handler returned 202 + runId, then polls the in-memory run
// registry until the background goroutine finishes, returning its terminal state.
func waitRun(t *testing.T, s *Server, rec *httptest.ResponseRecorder) runState {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	var started struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil || started.RunID == "" {
		t.Fatalf("no runId in response: %s", rec.Body.String())
	}
	for i := 0; i < 200; i++ { // ~2s ceiling
		if st, ok := s.runStatus(started.RunID); ok && st.Status != "running" {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish", started.RunID)
	return runState{}
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

	if st := waitRun(t, s, rec); st.Status != "done" {
		t.Fatalf("run status = %s, err = %q", st.Status, st.Err)
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

	if st := waitRun(t, s, rec); st.Status != "done" {
		t.Fatalf("run status = %s, err = %q", st.Status, st.Err)
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

func TestRun_SurfacesError(t *testing.T) {
	dir := t.TempDir()
	profs := profiles.NewStore(dir)
	_, _ = profs.Save(profiles.Profile{Name: "options-scout", Brief: "x"})
	s := New(nil, nil, nil)
	// Unreachable box → RunOnce fails → the run must end in status "error", not hang.
	s.UseHermes(hermes.NewClient(hermes.Config{BaseURL: "http://127.0.0.1:1", APIKey: "x"}))
	s.UseProfiles(profs)
	s.UseFeed(feed.NewStore(dir))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/feed/run",
		strings.NewReader(`{"profile":"options-scout","request":"find something"}`))
	s.handleFeedRun(rec, req)

	st := waitRun(t, s, rec)
	if st.Status != "error" || st.Err == "" {
		t.Fatalf("want error status with a message, got status=%q err=%q", st.Status, st.Err)
	}
}

func TestRunStatus_UnknownRun404(t *testing.T) {
	s := New(nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agents/runs/nope", nil)
	req.SetPathValue("id", "nope")
	s.handleRunStatus(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run: status = %d, want 404", rec.Code)
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
