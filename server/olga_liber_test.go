package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"manifest/jev"
	"manifest/olgachat"
	"manifest/tasks"
)

type fakeVoice struct {
	mu      sync.Mutex
	prompts []string
	answer  func(prompt string) string
}

func (f *fakeVoice) Ask(_ context.Context, prompt string) (olgachat.VoiceAnswer, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	f.mu.Unlock()
	return olgachat.VoiceAnswer{Text: f.answer(prompt), Model: "gpt-5.6-sol", Tokens: 10}, nil
}

type fakeRouter struct{ route string }

func (f fakeRouter) Route(context.Context, jev.RouteInput) (*jev.RouteAdvice, error) {
	return &jev.RouteAdvice{Route: f.route, Intent: jev.ChoiceJudgment{Probabilities: map[string]float64{"change_app": 0.9}, Confidence: 0.9}}, nil
}

func liberRig(t *testing.T, voice *fakeVoice, router olgachat.Router) (do func(method, path, body string, origin string) *httptest.ResponseRecorder, vault string) {
	t.Helper()
	vault = t.TempDir()
	home := filepath.Join(vault, "system", "home")
	os.MkdirAll(home, 0o700)
	os.WriteFile(filepath.Join(home, "goals.md"), []byte("## Home\n### Rocks (90-day)\n- [ ] Back addition [goal:: home/backyard] [quarter:: 2026-Q4]\n"), 0o600)
	os.WriteFile(filepath.Join(home, "tasks.md"), []byte("## Home\n"+
		"- [ ] roof on [todo:: home/roof-on] [rock:: home/backyard]\n"+
		"- [ ] flashing into house [todo:: home/flashing-into-house] [rock:: home/backyard]\n"+
		"- [ ] plan windows [todo:: home/plan-windows] [rock:: home/backyard]\n"+
		"- [ ] Metal finish and coated [todo:: home/metal-finish-and-coated] [rock:: home/backyard]\n"), 0o600)
	pw := filepath.Join(t.TempDir(), "password")
	os.WriteFile(pw, []byte("pw"), 0o600)
	logFile := filepath.Join(t.TempDir(), "liber.log")
	t.Cleanup(func() {
		if t.Failed() {
			b, _ := os.ReadFile(logFile)
			t.Logf("liber log:\n%s", b)
		}
	})
	h, err := NewOlgaHandlerWith(OlgaOptions{Vault: vault, PasswordFile: pw, AuditDir: t.TempDir(), Liber: &LiberConfig{Voice: voice, Router: router, LogFile: logFile}})
	if err != nil {
		t.Fatal(err)
	}
	raw := func(method, path, body, origin string, c *http.Cookie) *httptest.ResponseRecorder {
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
		h.ServeHTTP(w, r)
		return w
	}
	if w := raw("GET", "/api/liber/threads", "", "", nil); w.Code != 401 {
		t.Fatalf("liber without a session: %d", w.Code)
	}
	login := raw("POST", "/login", url.Values{"password": {"pw"}}.Encode(), "", nil)
	c := login.Result().Cookies()[0]
	seed, _ := os.ReadFile("../homeplan/testdata/plan.json")
	body, _ := json.Marshal(map[string]any{"revision": "", "patch": json.RawMessage(seed)})
	if w := raw("POST", "/api/home/plan", string(body), "", c); w.Code != 200 {
		t.Fatalf("seed plan: %d %s", w.Code, w.Body)
	}
	return func(method, path, body, origin string) *httptest.ResponseRecorder { return raw(method, path, body, origin, c) }, vault
}

func waitThread(t *testing.T, do func(string, string, string, string) *httptest.ResponseRecorder, q string, done func(*olgachat.Thread) bool) *olgachat.Thread {
	t.Helper()
	for i := 0; i < 200; i++ {
		w := do("GET", "/api/liber/thread?"+q, "", "")
		var th olgachat.Thread
		if w.Code == 200 && json.Unmarshal(w.Body.Bytes(), &th) == nil && done(&th) {
			return &th
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("thread %s never settled", q)
	return nil
}

var modelWords = regexp.MustCompile(`(?i)\b(gpt-[\w.-]+|opus|claude|codex|anthropic|openai|jev|typesafe|hermes)\b`)

func TestLiberTaskChatSuggestsAndApplies(t *testing.T) {
	voice := &fakeVoice{answer: func(p string) string {
		return "Start with the flashing, then the panels.\n```json\n" +
			`{"proposals":[{"kind":"task.add","text":"Get three roofing quotes","area":"Home","summary":"Add task: Get three roofing quotes"},` +
			`{"kind":"task.add","text":"bad [owner:: x] title","area":"Home"},` +
			`{"kind":"task.update","id":"home/nope","priority":"high"},` +
			`{"kind":"plan.patch","patch":{"tasks":{"home/roof-on":{"estimate":{"hours":12,"basis":"assistant"}}}},"summary":"Set the roof estimate to 12 hours"}]}` + "\n```"
	}}
	do, vault := liberRig(t, voice, fakeRouter{route: jev.RouteBuild})
	if w := do("POST", "/api/liber/send", `{"task":"home/roof-on","text":"What first?"}`, "https://evil.example"); w.Code != 403 {
		t.Fatalf("cross-origin send: %d", w.Code)
	}
	if w := do("POST", "/api/liber/send", `{"task":"home/roof-on","text":"What should we do first?"}`, ""); w.Code != 200 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	th := waitThread(t, do, "task=home/roof-on", func(th *olgachat.Thread) bool { return len(th.Turns) == 2 && th.Turns[1].Status == "" })
	if !th.Shared || th.Turns[1].Text != "Start with the flashing, then the panels." {
		t.Fatalf("thread %+v", th)
	}
	cards := th.Turns[1].Cards
	if len(cards) != 2 || cards[0].Summary != "Add task: Get three roofing quotes (Home)" || cards[1].Proposal.Kind != "plan.patch" {
		t.Fatalf("cards %+v — invalid suggestions must be dropped, valid ones shown", cards)
	}
	// the task chat never builds, whatever the router says
	if strings.Contains(voice.prompts[0], "Manifest will now build") || !strings.Contains(voice.prompts[0], "roof on") {
		t.Fatalf("task prompt %s", voice.prompts[0])
	}
	// the shared thread lives with the shared Home data, for Benjamin
	if _, err := os.Stat(filepath.Join(vault, "system", "home", "chat", "home_2froof-on.json")); err != nil {
		t.Fatal("shared thread file missing:", err)
	}
	// apply both; the plan change goes through the revision-checked API
	for _, c := range cards {
		if w := do("POST", "/api/liber/act", `{"task":"home/roof-on","card":"`+c.ID+`","action":"apply"}`, ""); w.Code != 200 {
			t.Fatalf("apply: %d %s", w.Code, w.Body)
		}
	}
	th = waitThread(t, do, "task=home/roof-on", func(*olgachat.Thread) bool { return true })
	for _, c := range th.Turns[1].Cards {
		if c.State != olgachat.StateApplied {
			t.Fatalf("card %+v", c)
		}
	}
	b, _ := os.ReadFile(filepath.Join(vault, "system", "home", "tasks.md"))
	if !strings.Contains(string(b), "Get three roofing quotes") {
		t.Fatalf("task not added:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(vault, "system", "home", "plan.json")); !strings.Contains(string(b), `"hours": 12`) && !strings.Contains(string(b), `"hours":12`) {
		t.Fatalf("plan not patched:\n%s", b)
	}
	// Benjamin's stores read what Olga applied (one data model, plan §6)
	owner := tasks.NewStore(t.TempDir(), "tasks.md", func(p string, b []byte) error { return os.WriteFile(p, b, 0o600) })
	owner.UseSharedHome(filepath.Join(vault, "system", "home", "tasks.md"), func(p string, b []byte) error { return os.WriteFile(p, b, 0o600) })
	doc, err := owner.Load()
	found := false
	if err == nil {
		if d := doc.Domain("Home"); d != nil {
			for _, tk := range d.Tasks {
				found = found || tk.Text == "Get three roofing quotes"
			}
		}
	}
	if !found {
		t.Fatalf("owner can't read Olga's task: %v", err)
	}
	// a stale card is a conflict, never a retry
	listBody := do("GET", "/api/liber/threads", "", "").Body.String()
	if modelWords.MatchString(listBody) || modelWords.MatchString(do("GET", "/api/liber/thread?task=home/roof-on", "", "").Body.String()) {
		t.Fatalf("model or vendor name reached Olga: %s", listBody)
	}
}

func TestLiberAppChatRoutes(t *testing.T) {
	voice := &fakeVoice{answer: func(p string) string {
		switch {
		case strings.Contains(p, "Restate what you understand"):
			return "You'd like the task titles bigger — is that right?\n```json\n{\"restatement\":\"Make the task titles bigger\"}\n```"
		case strings.Contains(p, "Manifest will now build"):
			return "Sure — I'll make that change.\n```json\n{\"brief\":\"Make task titles bigger on the board\"}\n```"
		}
		return "Happy to help.\n```json\n{\"route\":\"talk\"}\n```"
	}}
	router := &switchRouter{route: jev.RouteTalk}
	do, vault := liberRig(t, voice, router)
	w := do("POST", "/api/liber/send", `{"text":"hello there"}`, "")
	var th olgachat.Thread
	json.Unmarshal(w.Body.Bytes(), &th)
	if w.Code != 200 || th.ID == "" {
		t.Fatalf("new chat: %d %s", w.Code, w.Body)
	}
	got := waitThread(t, do, "id="+th.ID, func(th *olgachat.Thread) bool { return len(th.Turns) == 2 && th.Turns[1].Status == "" })
	if got.Turns[1].Text != "Happy to help." || len(got.Turns[1].Cards) != 0 {
		t.Fatalf("talk turn %+v", got.Turns[1])
	}
	// a probable change becomes a Confirm card
	router.set(jev.RouteConfirm)
	do("POST", "/api/liber/send", `{"id":"`+th.ID+`","text":"the titles are hard to read"}`, "")
	got = waitThread(t, do, "id="+th.ID, func(th *olgachat.Thread) bool { return len(th.Turns) == 4 && th.Turns[3].Status == "" })
	if len(got.Turns[3].Cards) != 1 || got.Turns[3].Cards[0].Kind != olgachat.CardConfirm || got.Turns[3].Cards[0].Summary != "Make the task titles bigger" {
		t.Fatalf("confirm card %+v", got.Turns[3])
	}
	// with no builder configured, "Make this change" becomes a note for Benjamin
	card := got.Turns[3].Cards[0].ID
	if w := do("POST", "/api/liber/act", `{"id":"`+th.ID+`","card":"`+card+`","action":"make"}`, ""); w.Code != 200 {
		t.Fatalf("make: %d %s", w.Code, w.Body)
	}
	got = waitThread(t, do, "id="+th.ID, func(th *olgachat.Thread) bool {
		return len(th.Turns) >= 6 && th.Turns[len(th.Turns)-1].Status == "" && th.Turns[len(th.Turns)-1].Text != ""
	})
	if b, _ := os.ReadFile(filepath.Join(vault, "system", "olga", "requests.md")); !strings.Contains(string(b), "Make the task titles bigger") {
		t.Fatalf("note for Benjamin missing:\n%s", b)
	}
	if body := do("GET", "/api/liber/thread?id="+th.ID, "", "").Body.String(); modelWords.MatchString(body) {
		t.Fatalf("model name reached Olga: %s", body)
	}
	// the planner keeps working while a turn runs (no global lock held)
	if w := do("GET", "/api/tasks", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

type switchRouter struct {
	mu    sync.Mutex
	route string
}

func (s *switchRouter) set(r string) { s.mu.Lock(); s.route = r; s.mu.Unlock() }
func (s *switchRouter) Route(context.Context, jev.RouteInput) (*jev.RouteAdvice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &jev.RouteAdvice{Route: s.route}, nil
}

func TestOlgaStrictInput(t *testing.T) {
	do, _ := liberRig(t, &fakeVoice{answer: func(string) string { return "ok" }}, nil)
	for body, want := range map[string]int{
		`{"text":"Buy paint","domain":"Home"}`:              200,
		`{"text":"Buy paint\nand brushes","domain":"Home"}`: 400,
		`{"text":"Buy paint [owner:: ben]","domain":"Home"}`: 400,
		`{"text":"- Buy paint","domain":"Home"}`:            400,
		`{"text":"Buy paint @alfred","domain":"Home"}`:      400,
		`{"text":"Buy paint","domain":"Home","secret":1}`:   400,
	} {
		if w := do("POST", "/api/tasks/item", body, ""); w.Code != want {
			t.Errorf("%s → %d, want %d (%s)", body, w.Code, want, w.Body)
		}
	}
}

func TestOlgaPreviewIsReadOnly(t *testing.T) {
	vault := t.TempDir()
	h, err := NewOlgaHandlerWith(OlgaOptions{Vault: vault, Preview: "/preview/ch-abc"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(method, p, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, p, strings.NewReader(body)))
		return w
	}
	page := get("GET", "/preview/ch-abc/", "")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Preview — this is how it would look") || !strings.Contains(page.Body.String(), `src="/preview/ch-abc/js/00-core.js"`) {
		t.Fatalf("preview page %d", page.Code)
	}
	if w := get("GET", "/preview/ch-abc/api/tasks", ""); w.Code != 200 {
		t.Fatalf("preview api %d", w.Code)
	}
	if w := get("POST", "/preview/ch-abc/api/tasks/item", `{"text":"x"}`); w.Code != 403 {
		t.Fatalf("preview accepted a write: %d", w.Code)
	}
	if w := get("GET", "/api/tasks", ""); w.Code != 404 {
		t.Fatalf("preview served outside its prefix: %d", w.Code)
	}
}
