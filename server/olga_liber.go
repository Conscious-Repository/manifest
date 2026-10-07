package server

// Liber — the assistant in Olga's Manifest (plan:
// system/workbench/plans/2026-10-07-olga-chat.md). The voice is her own
// Hermes profile; app changes are built by Claude Code in a worktree and
// shipped only after she taps "Use this". Everything she applies goes
// through her listener's own handlers (the gated mux), never around them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"manifest/hermes"
	"manifest/jev"
	"manifest/olgachat"
	"manifest/typesafe"
)

// LiberConfig wires her assistant. Zero values mean "off" for that part:
// no Hermes → no chat; no Builder → app changes are noted for Benjamin.
type LiberConfig struct {
	HermesBin     string // "" → hermes on PATH
	HermesProfile string // "olga"
	HermesPython  string // Hermes's venv Python, for turns with photos ("" → ~/.hermes/hermes-agent/venv/bin/python)
	VoiceModel    string // gpt-5.6-sol
	VoiceProvider string // openai-codex
	TypesafeKey   string // file holding the Jev key ("" → the voice decides)
	Builder       *olgachat.GitBuilder
	LogFile       string          // Benjamin's log (outside the vault)
	Voice         olgachat.Voice  // tests
	Router        olgachat.Router // tests
}

// ---- strict input (plan §6 rule 2) ----

var (
	inlineFieldRe = regexp.MustCompile(`\[[^\]\[]{1,40}::`)
	listMarkerRe  = regexp.MustCompile(`^\s*([-*+]\s|\d+[.)]\s|\[[ xX]\]\s)`)
	dispatchRe    = regexp.MustCompile(`(?i)(^|\s)(@[a-z][\w-]*(::\w+)?|!do)\s*$`)
)

// olgaTitleProblem explains why a task title can't be stored as given ("" = fine).
func olgaTitleProblem(t string) string {
	switch {
	case strings.ContainsAny(t, "\r\n"):
		return "A task title has to be one line."
	case inlineFieldRe.MatchString(t):
		return "A task title can't contain [ … :: ] fields."
	case listMarkerRe.MatchString(t):
		return "Start the task title with a word, not a list marker."
	case dispatchRe.MatchString(t):
		return "A task title can't end with an @-mention."
	case len(t) > 500:
		return "That title is too long."
	}
	return ""
}

var olgaWriteFields = map[string][]string{
	"/api/tasks/item":     {"text", "domain", "rock", "issue", "bucket", "owner", "stage", "container"},
	"/api/tasks/update":   {"id", "text", "domain", "waiting", "owner", "rock", "stage"},
	"/api/tasks/priority": {"id", "priority"},
	"/api/tasks/check":    {"id", "checked"},
	"/api/tasks/notes":    {"id", "description", "comment", "revision", "kind"},
}

// olgaStrictInput refuses writes outside the documented shapes: unknown
// fields, and task titles that would read as markup in either Manifest.
func olgaStrictInput(r *http.Request) error {
	allowed, ok := olgaWriteFields[r.URL.Path]
	if !ok || r.Method != http.MethodPost {
		return nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return errors.New("invalid request")
	}
	for k := range m {
		known := false
		for _, a := range allowed {
			if strings.EqualFold(k, a) {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("unexpected field %q", k)
		}
	}
	if raw, ok := m["text"]; ok && (r.URL.Path == "/api/tasks/item" || r.URL.Path == "/api/tasks/update") {
		var t string
		if json.Unmarshal(raw, &t) == nil {
			if p := olgaTitleProblem(strings.TrimSpace(t)); p != "" {
				return errors.New(p)
			}
		}
	}
	return nil
}

func olgaStaticType(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".css", ".png", ".svg", ".webp", ".jpg", ".jpeg", ".woff2", ".json", ".webmanifest", ".html":
		return true
	}
	return false
}

// ---- preview mode ----

// olgaPreviewPage rewrites her page to live under prefix and adds the banner.
func olgaPreviewPage(b []byte, prefix string) []byte {
	s := string(b)
	for _, a := range []string{`src="/`, `href="/`} {
		s = strings.ReplaceAll(s, a, a+strings.TrimPrefix(prefix, "/")+"/")
	}
	shim := `<base href="` + prefix + `/"><script>(()=>{const p=` + strconv.Quote(prefix) + `;const fix=u=>typeof u==='string'&&u.startsWith('/')&&!u.startsWith(p+'/')?p+u:u;const f=window.fetch.bind(window);window.fetch=(u,o)=>{if(u instanceof Request){const url=new URL(u.url);if(url.origin===location.origin&&!url.pathname.startsWith(p+'/')){url.pathname=p+url.pathname;u=new Request(url,u);}return f(u,o);}return f(fix(u),o);};const E=window.EventSource;if(E)window.EventSource=function(u,o){return new E(fix(u),o)};})();</script>`
	banner := `<div style="position:sticky;top:0;z-index:9999;display:flex;gap:12px;align-items:center;justify-content:center;flex-wrap:wrap;padding:10px 16px calc(10px);padding-top:calc(10px + env(safe-area-inset-top));background:#fff4d6;color:#4a3800;font:15px/1.3 system-ui;border-bottom:1px solid #e8d49a">Preview — this is how it would look. Changes here aren't saved.</div>`
	s = strings.Replace(s, "<head>", "<head>"+shim, 1)
	if i := strings.Index(s, "<body"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j >= 0 {
			s = s[:i+j+1] + banner + s[i+j+1:]
		}
	}
	return []byte(s)
}

func olgaPreviewHandler(prefix string, gated http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		p := r.URL.Path
		if p == prefix {
			http.Redirect(w, r, prefix+"/", http.StatusFound)
			return
		}
		if !strings.HasPrefix(p, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + strings.TrimPrefix(p, prefix+"/")
		r2.URL.RawPath = ""
		if r2.Method != http.MethodGet && r2.Method != http.MethodHead && r2.URL.Path != "/api/home/plan/preview" {
			http.Error(w, errOlgaPreview.Error(), http.StatusForbidden)
			return
		}
		gated.ServeHTTP(w, r2)
	})
}

// ---- Liber ----

type liber struct {
	svc     *olgachat.Service
	gated   http.Handler
	builder *olgachat.GitBuilder
}

func newLiber(opts OlgaOptions, gated http.Handler, write func(string, []byte) error) (http.Handler, error) {
	cfg := opts.Liber
	vault := opts.Vault
	store := &olgachat.Store{
		Private: filepath.Join(vault, "system", "olga", "chat"),
		Shared:  filepath.Join(vault, "system", "home", "chat"),
		Write:   write,
	}
	l := &liber{gated: gated, builder: cfg.Builder}
	svc := &olgachat.Service{Store: store, Planner: &olgaPlanner{gated: gated}, Now: time.Now}
	// The voice: her own Hermes profile, memory as its only tool.
	if cfg.Voice != nil {
		svc.Voice = cfg.Voice
	} else {
		home, _ := os.UserHomeDir()
		profile := firstNonEmptyStr(cfg.HermesProfile, "olga")
		svc.Voice = &hermesVoice{run: hermes.NewRunner(hermes.Config{Enabled: true, Bin: cfg.HermesBin}), profile: profile,
			model: firstNonEmptyStr(cfg.VoiceModel, "gpt-5.6-sol"), provider: firstNonEmptyStr(cfg.VoiceProvider, "openai-codex"),
			python: firstNonEmptyStr(cfg.HermesPython, filepath.Join(home, ".hermes", "hermes-agent", "venv", "bin", "python")),
			home:   filepath.Join(home, ".hermes", "profiles", profile)}
	}
	// The router: Jev, with her own key file.
	if cfg.Router != nil {
		svc.Router = cfg.Router
	} else if cfg.TypesafeKey != "" {
		keyFile := cfg.TypesafeKey
		c := typesafe.NewFromEnv()
		c.Key = func() string { b, _ := os.ReadFile(keyFile); return strings.TrimSpace(string(b)) }
		c.HTTP = &http.Client{Timeout: 10 * time.Second}
		c.Backoff = []time.Duration{} // the budget is 1.5 s; no retries
		svc.Router = jevRouter{&jev.Judge{Eval: c, Model: c.ModelName()}, c}
	}
	if cfg.Builder != nil {
		svc.Builder = cfg.Builder
		svc.Restart = cfg.Builder.Restart
	}
	logMu := &sync.Mutex{}
	svc.Log = func(e map[string]any) {
		if cfg.LogFile == "" {
			return
		}
		b, _ := json.Marshal(e)
		logMu.Lock()
		defer logMu.Unlock()
		f, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	notes := filepath.Join(vault, "system", "olga", "requests.md")
	notesMu := &sync.Mutex{}
	svc.Notes = func(text string) error {
		notesMu.Lock()
		defer notesMu.Unlock()
		prior, _ := os.ReadFile(notes)
		if len(prior) == 0 {
			prior = []byte("# Requests for Benjamin\n\nWhat Olga asked Liber for that needs a change outside her own part of the app.\n\n")
		}
		return write(notes, append(prior, []byte(text)...))
	}
	l.svc = svc
	svc.Recover()
	go func() {
		// the launcher writes its verdict a few seconds after start
		for i := 0; i < 60 && svc.SettleDeploys(); i++ {
			time.Sleep(time.Second)
		}
	}()
	if cfg.Builder != nil {
		go func() {
			for range time.Tick(10 * time.Minute) {
				cfg.Builder.Reap()
			}
		}()
	}
	return l, nil
}

// hermesVoice asks her profile one composed prompt.
type hermesVoice struct {
	run                      *hermes.Runner
	profile, model, provider string
	python, home             string // Hermes's Python and this profile's home (photo turns)
}

func (h *hermesVoice) Ask(ctx context.Context, prompt string, images []string) (olgachat.VoiceAnswer, error) {
	if len(images) > 0 {
		return h.askWithPhotos(ctx, prompt, images)
	}
	res, err := h.run.Run(ctx, hermes.Request{Prompt: prompt, Profile: h.profile, Model: h.model, Provider: h.provider, Toolsets: "memory", TimeoutSeconds: 170})
	tokens := 0
	if res.Usage != nil {
		tokens = int(res.Usage.TotalTokens)
	}
	return olgachat.VoiceAnswer{Text: res.Reply, Model: firstNonEmptyStr(res.Model, h.model), Tokens: tokens}, err
}

// askWithPhotos runs the embedded shim with Hermes's own Python, in the same
// profile, with the photos as image parts of her message.
func (h *hermesVoice) askWithPhotos(ctx context.Context, prompt string, images []string) (olgachat.VoiceAnswer, error) {
	ans := olgachat.VoiceAnswer{Model: h.model}
	dir, err := os.MkdirTemp("", "liber-voice-")
	if err != nil {
		return ans, err
	}
	defer os.RemoveAll(dir)
	shim, usage, reqFile := filepath.Join(dir, "voice.py"), filepath.Join(dir, "usage.json"), filepath.Join(dir, "request.json")
	if err := os.WriteFile(shim, olgachat.VoiceShim, 0o600); err != nil {
		return ans, err
	}
	var imgs []map[string]string
	for _, p := range images {
		imgs = append(imgs, map[string]string{"path": p, "mime": olgachat.ImageMime(filepath.Base(p))})
	}
	req, _ := json.Marshal(map[string]any{"prompt": prompt, "images": imgs, "home": h.home, "model": h.model, "provider": h.provider, "usage": usage})
	if err := os.WriteFile(reqFile, req, 0o600); err != nil {
		return ans, err
	}
	ctx, cancel := context.WithTimeout(ctx, 170*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.python, shim, reqFile)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.WaitDelay = 5 * time.Second
	runErr := cmd.Run()
	ans.Text = strings.TrimSpace(out.String())
	var u struct {
		Completed   *bool  `json:"completed"`
		Failed      bool   `json:"failed"`
		Model       string `json:"model"`
		TotalTokens int    `json:"total_tokens"`
	}
	if b, err := os.ReadFile(usage); err == nil && json.Unmarshal(b, &u) == nil {
		ans.Model, ans.Tokens = firstNonEmptyStr(u.Model, h.model), u.TotalTokens
		if u.Failed || (u.Completed != nil && !*u.Completed) {
			return ans, errors.New("hermes reported that the turn failed")
		}
	}
	if runErr != nil {
		return ans, fmt.Errorf("hermes (photos): %v: %s", runErr, clipLine(errb.String(), 300))
	}
	return ans, nil
}

type jevRouter struct {
	j *jev.Judge
	c *typesafe.Client
}

func (r jevRouter) Route(ctx context.Context, in jev.RouteInput) (*jev.RouteAdvice, error) {
	if !r.c.Enabled() {
		return nil, errors.New("no key")
	}
	return r.j.AdviseRoute(ctx, in)
}

// ---- routes ----

func (l *liber) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(r.URL.Path, "/preview/") {
		l.proxyPreview(w, r)
		return
	}
	if r.URL.Path == "/api/liber/upload" && r.Method == http.MethodPost {
		l.upload(w, r)
		return
	}
	if r.URL.Path == "/api/liber/file" && r.Method == http.MethodGet {
		l.file(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	switch {
	case r.URL.Path == "/api/liber/threads" && r.Method == http.MethodGet:
		l.list(w)
	case r.URL.Path == "/api/liber/thread" && r.Method == http.MethodGet:
		l.get(w, r)
	case r.URL.Path == "/api/liber/send" && r.Method == http.MethodPost:
		l.send(w, r)
	case r.URL.Path == "/api/liber/act" && r.Method == http.MethodPost:
		l.act(w, r)
	case r.URL.Path == "/api/liber/events" && r.Method == http.MethodGet:
		l.events(w, r)
	default:
		http.NotFound(w, r)
	}
}

func refFrom(q url.Values) (olgachat.Ref, bool) {
	if id := q.Get("task"); id != "" {
		return olgachat.Ref{Kind: olgachat.KindTask, ID: id}, true
	}
	id := q.Get("id")
	if id == "" || olgachat.ValidID(id) {
		return olgachat.Ref{Kind: olgachat.KindApp, ID: id}, true
	}
	return olgachat.Ref{}, false
}

func liberJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

type threadRow struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	TaskID    string    `json:"taskId,omitempty"`
	Title     string    `json:"title"`
	Shared    bool      `json:"shared,omitempty"`
	Updated   time.Time `json:"updated"`
	Last      string    `json:"last"`
	Busy      bool      `json:"busy,omitempty"`
	Pending   int       `json:"pending,omitempty"` // cards waiting for her
	TurnCount int       `json:"turns"`
}

func (l *liber) list(w http.ResponseWriter) {
	rows := []threadRow{}
	for _, t := range l.svc.Store.List() {
		row := threadRow{ID: t.ID, Kind: t.Kind, TaskID: t.TaskID, Title: firstNonEmptyStr(t.Title, t.TaskTitle), Shared: t.Shared, Updated: t.Updated, Busy: t.Busy(), TurnCount: len(t.Turns)}
		if t.Kind == olgachat.KindTask {
			if title, _, ok := l.svc.Planner.TaskInfo(t.TaskID); ok {
				row.Title = title
			}
		}
		for i := len(t.Turns) - 1; i >= 0; i-- {
			if strings.TrimSpace(t.Turns[i].Text) != "" {
				row.Last = clipLine(t.Turns[i].Text, 120)
				break
			}
		}
		for _, tu := range t.Turns {
			for _, c := range tu.Cards {
				if c.State == olgachat.StatePending || c.State == olgachat.StateReady {
					row.Pending++
				}
			}
		}
		rows = append(rows, row)
	}
	liberJSON(w, map[string]any{"threads": rows})
}

func clipLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (l *liber) get(w http.ResponseWriter, r *http.Request) {
	ref, ok := refFrom(r.URL.Query())
	if !ok || (ref.Kind == olgachat.KindApp && ref.ID == "") {
		http.NotFound(w, r)
		return
	}
	t, err := l.svc.Get(ref)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	liberJSON(w, t.Public())
}

func (l *liber) send(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID, Task, Text string
		Images         []string
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	q := url.Values{}
	if b.Task != "" {
		q.Set("task", b.Task)
	} else {
		q.Set("id", b.ID)
	}
	ref, ok := refFrom(q)
	if !ok {
		http.NotFound(w, r)
		return
	}
	t, err := l.svc.Send(ref, b.Text, b.Images...)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 400)
		return
	}
	liberJSON(w, t.Public())
}

func (l *liber) act(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID, Task, Card, Action string }
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	q := url.Values{}
	if b.Task != "" {
		q.Set("task", b.Task)
	} else {
		q.Set("id", b.ID)
	}
	ref, ok := refFrom(q)
	if !ok || ref.ID == "" {
		http.NotFound(w, r)
		return
	}
	t, err := l.svc.Act(ref, b.Card, b.Action)
	switch {
	case errors.Is(err, os.ErrNotExist):
		http.NotFound(w, r)
	case errors.Is(err, olgachat.ErrBusy):
		http.Error(w, "Liber is still answering — try again in a moment.", http.StatusConflict)
	case err != nil:
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		liberJSON(w, t.Public())
	}
}

// photoShared: does a photo for this scope belong with the shared Home data?
func (l *liber) photoShared(q url.Values) (bool, bool) {
	task := q.Get("task")
	if task == "" {
		return false, true
	}
	_, shared, ok := l.svc.Planner.TaskInfo(task)
	return shared, ok
}

// upload stores one photo (the raw image is the body) and returns its id.
func (l *liber) upload(w http.ResponseWriter, r *http.Request) {
	shared, ok := l.photoShared(r.URL.Query())
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, olgachat.MaxImageBytes+1))
	if err != nil {
		http.Error(w, "a photo can be at most 12 MB", http.StatusRequestEntityTooLarge)
		return
	}
	id, err := l.svc.Store.SaveImage(data, shared)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	liberJSON(w, map[string]string{"id": id})
}

// file serves one of her photos (behind her sign-in, like everything here).
func (l *liber) file(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	shared, ok := l.photoShared(q)
	p := ""
	if ok {
		p = l.svc.Store.ImagePath(q.Get("id"), shared)
	}
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", olgachat.ImageMime(q.Get("id")))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, p)
}

// events streams the thread whenever it changes (Server-Sent Events). The
// page also polls, so a dropped stream only slows updates down.
func (l *liber) events(w http.ResponseWriter, r *http.Request) {
	ref, ok := refFrom(r.URL.Query())
	if !ok || ref.ID == "" {
		http.NotFound(w, r)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	k := olgachat.ThreadKey(ref.Kind, ref.ID)
	if ref.Kind == olgachat.KindApp {
		k = "app:" + ref.ID
	} else {
		k = "task:" + ref.ID
	}
	ch, done := l.svc.Subscribe(k)
	defer done()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	sendThread := func() bool {
		t, err := l.svc.Get(ref)
		if err != nil {
			return false
		}
		b, _ := json.Marshal(t.Public())
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !sendThread() {
		return
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	limit := time.NewTimer(10 * time.Minute)
	defer limit.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-limit.C:
			return
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		case <-ch:
			if !sendThread() {
				return
			}
		}
	}
}

// proxyPreview forwards /preview/<change>/… to that change's read-only build,
// only after her normal sign-in (it is mounted behind the session check).
func (l *liber) proxyPreview(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/preview/")
	id := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		id = rest[:i]
	}
	if l.builder == nil || !olgachat.ValidID(id) {
		http.NotFound(w, r)
		return
	}
	port := l.builder.PreviewPort(id)
	if port == 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><body style="font:17px system-ui;padding:24px">This preview has closed. Go back to Liber and ask again if you'd like to see it.</body>`)
		return
	}
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	p := httputil.NewSingleHostReverseProxy(target)
	p.ServeHTTP(w, r)
}

// ---- planner: context and proposals through her own handlers ----

type olgaPlanner struct{ gated http.Handler }

func (p *olgaPlanner) call(method, target string, body any) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.gated.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

type plannerTask struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	State    string `json:"state"`
	Priority string `json:"priority,omitempty"`
	Area     string `json:"area"`
}

func (p *olgaPlanner) tasks() ([]plannerTask, []string) {
	code, body := p.call("GET", "/api/tasks", nil)
	if code != 200 {
		return nil, nil
	}
	var v struct {
		Domains []struct {
			Name    string        `json:"name"`
			Tasks   []plannerTask `json:"tasks"`
			Buckets []struct {
				Tasks []plannerTask `json:"tasks"`
			} `json:"buckets"`
		} `json:"domains"`
	}
	_ = json.Unmarshal(body, &v)
	var out []plannerTask
	var areas []string
	for _, d := range v.Domains {
		areas = append(areas, d.Name)
		add := func(ts []plannerTask) {
			for _, t := range ts {
				t.Area = d.Name
				out = append(out, t)
			}
		}
		add(d.Tasks)
		for _, b := range d.Buckets {
			add(b.Tasks)
		}
	}
	return out, areas
}

func (p *olgaPlanner) find(id string) (plannerTask, bool) {
	ts, _ := p.tasks()
	for _, t := range ts {
		if t.ID == id {
			return t, true
		}
	}
	return plannerTask{}, false
}

func (p *olgaPlanner) TaskInfo(id string) (string, bool, bool) {
	t, ok := p.find(id)
	return t.Text, t.Area == "Home", ok
}

func (p *olgaPlanner) TaskContext(id string) (map[string]any, error) {
	t, ok := p.find(id)
	if !ok {
		return nil, os.ErrNotExist
	}
	ctx := map[string]any{"task": t}
	if code, body := p.call("GET", "/api/tasks/notes?id="+url.QueryEscape(id), nil); code == 200 {
		var n struct {
			Description string `json:"description"`
			Comments    []struct {
				Author string `json:"author_name"`
				Text   string `json:"text"`
				At     string `json:"at"`
			} `json:"comments"`
		}
		if json.Unmarshal(body, &n) == nil {
			d := n.Description
			if len(d) > 12000 {
				d = d[:12000] + "…"
			}
			ctx["notes"] = d
			if len(n.Comments) > 20 {
				n.Comments = n.Comments[len(n.Comments)-20:]
			}
			ctx["comments"] = n.Comments
		}
	}
	all, _ := p.tasks()
	var related []map[string]string
	for _, o := range all {
		if o.Area == t.Area && o.ID != t.ID && o.State != "done" && len(related) < 40 {
			related = append(related, map[string]string{"id": o.ID, "title": o.Text})
		}
	}
	ctx["relatedTasks"] = related
	if t.Area == "Home" {
		if code, body := p.call("GET", "/api/home/plan", nil); code == 200 {
			var plan map[string]any
			if json.Unmarshal(body, &plan) == nil {
				ctx["housePlan"] = trimPlan(plan, id)
			}
		}
	}
	return ctx, nil
}

// trimPlan keeps what helps a planning conversation about one task.
func trimPlan(plan map[string]any, taskID string) map[string]any {
	out := map[string]any{}
	if rev, ok := plan["revision"]; ok {
		out["revision"] = rev
	}
	doc, _ := plan["plan"].(map[string]any)
	if doc == nil {
		doc = plan
	}
	for _, k := range []string{"horizon", "capacity", "away", "events", "reservations", "milestones", "decisions", "budget", "scenarios"} {
		if v, ok := doc[k]; ok {
			out[k] = v
		}
	}
	if ts, ok := doc["tasks"].(map[string]any); ok {
		out["thisTask"] = ts[taskID]
		out["otherPlannedTasks"] = len(ts) - 1
	}
	if d, ok := plan["derived"]; ok {
		b, _ := json.Marshal(d)
		if len(b) < 20000 {
			out["derived"] = d
		}
	}
	return out
}

func (p *olgaPlanner) Validate(threadTask string, raw olgachat.RawProposal) (*olgachat.Proposal, string, error) {
	sum := strings.TrimSpace(raw.Summary)
	switch raw.Kind {
	case "task.add":
		text := strings.TrimSpace(raw.Text)
		if text == "" {
			return nil, "", errors.New("empty title")
		}
		if pr := olgaTitleProblem(text); pr != "" {
			return nil, "", errors.New(pr)
		}
		_, areas := p.tasks()
		area := strings.TrimSpace(raw.Area)
		if area == "" {
			if t, ok := p.find(threadTask); ok {
				area = t.Area
			}
		}
		known := area == "Inbox" || area == ""
		for _, a := range areas {
			known = known || a == area
		}
		if !known {
			return nil, "", errors.New("unknown area " + area)
		}
		line := "Add task: " + text
		if area != "" && area != "Inbox" {
			line += " (" + area + ")"
		}
		return &olgachat.Proposal{Kind: raw.Kind, Text: text, Area: area}, line, nil
	case "task.update":
		t, ok := p.find(raw.ID)
		if !ok {
			return nil, "", errors.New("unknown task")
		}
		pr := &olgachat.Proposal{Kind: raw.Kind, ID: raw.ID}
		var parts []string
		if txt := strings.TrimSpace(raw.Text); txt != "" && txt != t.Text {
			if prob := olgaTitleProblem(txt); prob != "" {
				return nil, "", errors.New(prob)
			}
			pr.Text = txt
			parts = append(parts, "rename to “"+txt+"”")
		}
		if raw.Priority != nil {
			v := strings.ToLower(strings.TrimSpace(*raw.Priority))
			switch v {
			case "low", "med", "high":
			case "medium":
				v = "med"
			case "none", "":
				v = ""
			default:
				return nil, "", errors.New("bad priority")
			}
			pr.Priority = &v
			label := map[string]string{"": "no priority", "low": "low priority", "med": "medium priority", "high": "high priority"}[v]
			parts = append(parts, "set "+label)
		}
		if len(parts) == 0 {
			return nil, "", errors.New("nothing to change")
		}
		return pr, "“" + t.Text + "”: " + strings.Join(parts, ", "), nil
	case "task.note":
		t, ok := p.find(raw.ID)
		if !ok {
			return nil, "", errors.New("unknown task")
		}
		add := strings.TrimSpace(raw.Append)
		if add == "" || len(add) > 6000 {
			return nil, "", errors.New("note text missing or too long")
		}
		return &olgachat.Proposal{Kind: raw.Kind, ID: raw.ID, Append: add}, firstNonEmptyStr(sum, "Add to the notes of “"+t.Text+"”: "+clipLine(add, 90)), nil
	case "plan.patch":
		if len(raw.Patch) == 0 || raw.Patch[0] != '{' || sum == "" {
			return nil, "", errors.New("plan change needs a patch and a summary")
		}
		code, body := p.call("GET", "/api/home/plan", nil)
		if code != 200 {
			return nil, "", errors.New("house plan unavailable")
		}
		var v struct {
			Revision string `json:"revision"`
		}
		_ = json.Unmarshal(body, &v)
		code, body = p.call("POST", "/api/home/plan/preview", map[string]any{"revision": v.Revision, "patch": raw.Patch})
		if code != 200 {
			return nil, "", errors.New("plan change refused: " + clipLine(string(body), 160))
		}
		return &olgachat.Proposal{Kind: raw.Kind, Patch: raw.Patch, Revision: v.Revision}, sum, nil
	}
	return nil, "", errors.New("unknown suggestion kind")
}

func (p *olgaPlanner) Apply(pr *olgachat.Proposal) (string, error) {
	if pr == nil {
		return "", errors.New("nothing to apply")
	}
	fail := func(code int, body []byte) error {
		if code == http.StatusConflict || code == http.StatusPreconditionFailed {
			return olgachat.ErrConflict
		}
		return errors.New(clipLine(strings.TrimSpace(string(body)), 160))
	}
	switch pr.Kind {
	case "task.add":
		dom := pr.Area
		if dom == "Inbox" {
			dom = ""
		}
		if code, body := p.call("POST", "/api/tasks/item", map[string]any{"text": pr.Text, "domain": dom}); code != 200 {
			return "", fail(code, body)
		}
		return "Added", nil
	case "task.update":
		if _, ok := p.find(pr.ID); !ok {
			return "", olgachat.ErrConflict
		}
		if pr.Text != "" {
			if code, body := p.call("POST", "/api/tasks/update", map[string]any{"id": pr.ID, "text": pr.Text}); code != 200 {
				return "", fail(code, body)
			}
		}
		if pr.Priority != nil {
			if code, body := p.call("POST", "/api/tasks/priority", map[string]any{"id": pr.ID, "priority": *pr.Priority}); code != 200 {
				return "", fail(code, body)
			}
		}
		return "Updated", nil
	case "task.note":
		code, body := p.call("GET", "/api/tasks/notes?id="+url.QueryEscape(pr.ID), nil)
		if code != 200 {
			return "", fail(code, body)
		}
		var n struct{ Description, Revision string }
		_ = json.Unmarshal(body, &n)
		desc := strings.TrimRight(n.Description, "\n")
		if desc != "" {
			desc += "\n\n"
		}
		desc += pr.Append + "\n"
		if code, body := p.call("POST", "/api/tasks/notes", map[string]any{"id": pr.ID, "kind": "description", "description": desc, "revision": n.Revision}); code != 200 {
			return "", fail(code, body)
		}
		return "Added to the notes", nil
	case "plan.patch":
		code, body := p.call("POST", "/api/home/plan", map[string]any{"revision": pr.Revision, "patch": pr.Patch})
		if code != 200 {
			return "", fail(code, body)
		}
		return "Updated the house plan", nil
	}
	return "", errors.New("unknown suggestion")
}
