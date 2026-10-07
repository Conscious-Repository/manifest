package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/tasks"
)

// The shared Home plan through both planners: Olga's authenticated listener
// and the owner's server read and write ONE file, with revision conflicts,
// validation against the real Home task list, and nothing private exposed.
func TestHomePlanSharedAcrossPlanners(t *testing.T) {
	vault := t.TempDir()
	home := filepath.Join(vault, "system", "home")
	write := func(p string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		return os.WriteFile(p, b, 0o600)
	}
	write(filepath.Join(home, "goals.md"), []byte("## Home\n### Rocks (90-day)\n- [ ] Back addition [goal:: home/backyard] [quarter:: 2026-Q4]\n"))
	write(filepath.Join(home, "tasks.md"), []byte("## Home\n"+
		"- [ ] roof on [todo:: home/roof-on] [rock:: home/backyard]\n"+
		"- [ ] flashing into house [todo:: home/flashing-into-house] [rock:: home/backyard]\n"+
		"- [ ] plan windows [todo:: home/plan-windows] [rock:: home/backyard]\n"+
		"- [ ] Metal finish and coated [todo:: home/metal-finish-and-coated] [rock:: home/backyard]\n"))
	// the owner's server: a private task list plus the shared Home
	ownerRoot := filepath.Join(vault, "owner")
	write(filepath.Join(ownerRoot, "tasks.md"), []byte("# Tasks\n## Inbox\n- [ ] OWNER PRIVATE errand [todo:: inbox/private]\n"))
	ts := tasks.NewStore(ownerRoot, "tasks.md", write)
	ts.UseSharedHome(filepath.Join(home, "tasks.md"), write)
	owner := &Server{}
	owner.UseTasks(ts)
	owner.UseHomePlan(home, write)
	ownerDo := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		switch {
		case strings.HasPrefix(path, "/api/home/plan/preview"):
			owner.handleHomePlanPreview(w, r)
		case strings.HasPrefix(path, "/api/home/plan/history"):
			owner.handleHomePlanHistory(w, r)
		default:
			owner.handleHomePlan(w, r)
		}
		return w
	}

	pw := filepath.Join(t.TempDir(), "password")
	os.WriteFile(pw, []byte("pw"), 0o600)
	olga, err := NewOlgaHandler(vault, pw, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	olgaDo := func(method, path, body string, c *http.Cookie, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if c != nil {
			r.AddCookie(c)
		}
		if path == "/login" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		olga.ServeHTTP(w, r)
		return w
	}
	if w := olgaDo("GET", "/api/home/plan", "", nil, ""); w.Code != 401 {
		t.Fatalf("unauthenticated plan read: %d", w.Code)
	}
	cookie := olgaDo("POST", "/login", url.Values{"password": {"pw"}}.Encode(), nil, "").Result().Cookies()[0]
	for _, asset := range []string{"/js/90-home-plan.js", "/css/90-home-plan.css"} {
		if w := olgaDo("GET", asset, "", cookie, ""); w.Code != 200 {
			t.Fatalf("%s: %d", asset, w.Code)
		}
	}
	// no plan yet: the Home task list only, nothing private
	w := olgaDo("GET", "/api/home/plan", "", cookie, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "OWNER PRIVATE") || !strings.Contains(w.Body.String(), "home/roof-on") || strings.Contains(w.Body.String(), `"revision":"`+"x") {
		t.Fatalf("empty plan read: %d %s", w.Code, w.Body)
	}
	seed, _ := os.ReadFile("../homeplan/testdata/plan.json")
	body, _ := json.Marshal(map[string]any{"revision": "", "patch": json.RawMessage(seed)})
	if w := olgaDo("POST", "/api/home/plan", string(body), cookie, "https://evil.example"); w.Code != 403 {
		t.Fatalf("cross-origin plan write: %d", w.Code)
	}
	w = olgaDo("POST", "/api/home/plan", string(body), cookie, "")
	if w.Code != 200 {
		t.Fatalf("seed through Olga: %d %s", w.Code, w.Body)
	}
	var fromOlga homePlanView
	json.Unmarshal(w.Body.Bytes(), &fromOlga)
	// the owner sees the same revision and the same derived capacity
	w = ownerDo("GET", "/api/home/plan?asOf=2026-10-07", "")
	var fromOwner homePlanView
	json.Unmarshal(w.Body.Bytes(), &fromOwner)
	if fromOwner.Revision == "" || fromOwner.Revision != fromOlga.Revision {
		t.Fatalf("revisions differ: owner %q olga %q", fromOwner.Revision, fromOlga.Revision)
	}
	if c := fromOwner.Derived.Capacity; c.SharedHours != 128 || c.PoolHours != 80 || c.KnownDemand != 28 {
		t.Fatalf("owner capacity = %+v", c)
	}
	if _, leaked := fromOwner.Tasks["inbox/private"]; leaked {
		t.Fatal("the plan's task join exposed a private task")
	}
	// re-running the seed is a conflict, never a second copy
	if w := olgaDo("POST", "/api/home/plan", string(body), cookie, ""); w.Code != 409 {
		t.Fatalf("re-seed: %d", w.Code)
	}
	// owner patches one estimate; Olga's stale revision then conflicts
	rev := fromOwner.Revision
	w = ownerDo("POST", "/api/home/plan", `{"revision":"`+rev+`","patch":{"tasks":{"home/roof-on":{"estimate":{"hours":36}}}}}`)
	if w.Code != 200 {
		t.Fatalf("owner patch: %d %s", w.Code, w.Body)
	}
	w = olgaDo("POST", "/api/home/plan", `{"revision":"`+rev+`","patch":{"tasks":{"home/roof-on":{"estimate":{"hours":40}}}}}`, cookie, "")
	var conflict struct{ Error, Revision string }
	json.Unmarshal(w.Body.Bytes(), &conflict)
	if w.Code != 409 || conflict.Revision == "" || conflict.Revision == rev {
		t.Fatalf("stale write: %d %s", w.Code, w.Body)
	}
	w = olgaDo("GET", "/api/home/plan?asOf=2026-10-07", "", cookie, "")
	json.Unmarshal(w.Body.Bytes(), &fromOlga)
	if fromOlga.Derived.Capacity.RemainingHours != 16 {
		t.Fatalf("Olga sees remaining %v after the owner's 36 h roof, want 16", fromOlga.Derived.Capacity.RemainingHours)
	}
	// only shared Home tasks may join; a typo'd field is refused
	for _, patch := range []string{`{"tasks":{"inbox/private":{"phase":"planning"}}}`, `{"tasks":{"home/roof-on":{"estimat":{}}}}`} {
		w = ownerDo("POST", "/api/home/plan", `{"revision":"`+fromOlga.Revision+`","patch":`+patch+`}`)
		if w.Code != 400 {
			t.Fatalf("%s accepted: %d", patch, w.Code)
		}
	}
	// a preview writes nothing
	before, _ := os.ReadFile(filepath.Join(home, "plan.json"))
	w = olgaDo("POST", "/api/home/plan/preview", `{"patch":{"tasks":{"home/roof-on":{"allocations":{"2026-10-31":8}}}}}`, cookie, "")
	after, _ := os.ReadFile(filepath.Join(home, "plan.json"))
	if w.Code != 200 || string(before) != string(after) {
		t.Fatalf("preview: %d, wrote=%v", w.Code, string(before) != string(after))
	}
	var preview homePlanView
	json.Unmarshal(w.Body.Bytes(), &preview)
	if preview.Revision != fromOlga.Revision || preview.Derived.Capacity.AllocatedHours != 8 {
		t.Fatalf("preview = %+v", preview.Derived.Capacity)
	}
	// every replaced revision is kept
	w = olgaDo("GET", "/api/home/plan/history", "", cookie, "")
	if !strings.Contains(w.Body.String(), ".json") {
		t.Fatalf("history: %s", w.Body)
	}
	// the plan lives beside the shared Home files; Olga's private tree is untouched
	if _, err := os.Stat(filepath.Join(vault, "system", "olga", "plan.json")); err == nil {
		t.Fatal("plan written into Olga's private tree")
	}
}
