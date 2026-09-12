package server

// Olga is a deliberately unwired Server: the existing planner handlers, with
// an explicit route allowlist and a vaultwriter capability for system/olga only.
import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io/fs"
	"manifest/daily"
	"manifest/goals"
	"manifest/record"
	"manifest/tasks"
	"manifest/vaultwriter"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type olgaLocator struct{ root string }

func (l olgaLocator) GoalsPath() string { return filepath.Join(l.root, "goals.md") }
func (l olgaLocator) DailyNote(date string) (string, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", err
	}
	return filepath.Join(l.root, "daily", date+".md"), nil
}

// NewOlgaHandler never receives the owner's Server or any integration objects.
// An empty/missing password file fails closed; changing it revokes sessions.
func NewOlgaHandler(vaultRoot, passwordFile, auditDir string) (http.Handler, error) {
	root := filepath.Join(vaultRoot, "system", "olga")
	if err := os.MkdirAll(filepath.Join(root, "daily"), 0700); err != nil {
		return nil, err
	}
	writer := vaultwriter.New(vaultRoot).WithAudit(auditDir).Grant(vaultwriter.Capability{Name: "olga", Zone: record.ZoneSystem, Pattern: "system/olga/**", Actor: vaultwriter.ActorPortalMember}, vaultwriter.Capability{Name: "home", Zone: record.ZoneSystem, Pattern: "system/home/**", Actor: vaultwriter.ActorPortalMember})
	write := func(path string, data []byte) error {
		rel, err := filepath.Rel(vaultRoot, path)
		if err != nil {
			return err
		}
		capName := "olga"
		if strings.HasPrefix(filepath.ToSlash(rel), "system/home/") {
			capName = "home"
		}
		return writer.WriteCap(capName, filepath.ToSlash(rel), data)
	}
	loc := olgaLocator{root}
	gs := goals.NewStore(loc, root, "goals.md", write)
	ts := tasks.NewStore(root, "tasks.md", write)
	for path, content := range map[string]string{"goals.md": "# Goals\n", "tasks.md": "# Tasks\n\n## Inbox\n"} {
		if _, err := os.Stat(filepath.Join(root, path)); os.IsNotExist(err) {
			if err = write(filepath.Join(root, path), []byte(content)); err != nil {
				return nil, err
			}
		}
	}
	sharedRoot := filepath.Join(vaultRoot, "system", "home")
	if _, err := os.Stat(filepath.Join(sharedRoot, "goals.md")); err == nil {
		gs.UseSharedHome(filepath.Join(sharedRoot, "goals.md"), write)
		ts.UseSharedHome(filepath.Join(sharedRoot, "tasks.md"), write)
	}
	svc := daily.NewService(daily.Config{VaultPath: root, ScheduleStart: 8, ScheduleEnd: 18, Write: write}, loc)
	svc.UseGoals(NewGoalsAdapter(gs, ts, nil, nil, "OS"))
	s := New(svc, gs, nil)
	s.UseTasks(ts)
	s.UsePlannerNotes(root, sharedRoot, "Olga", write)
	mux := http.NewServeMux()
	routes := map[string]http.HandlerFunc{
		"/api/day": s.handleDay, "/api/day/pull": s.handleDayPull, "/api/day/capture": s.handleDayCapture, "/api/day/focus": s.handleDayFocus, "/api/day/focus/milestone": s.handleDayFocusMilestone,
		"/api/goals": s.handleGoalsGet, "/api/areas": s.handleAreas, "/api/areas/reorder": s.handleAreasReorder,
		"/api/goals/item": s.handleGoalItem, "/api/goals/check": s.handleGoalCheck, "/api/goals/reorder": s.handleGoalsReorder, "POST /api/goals/move": s.handleGoalMove, "/api/goals/close": s.handleGoalClose, "/api/goals/archives": s.handleGoalsArchives, "/api/goals/carry": s.handleGoalCarry, "/api/goals/retro": s.handleGoalRetro,
		"GET /api/tasks/notes": s.handlePlannerNotes, "POST /api/tasks/notes": s.handlePlannerNotes,
		"GET /api/tasks": s.handleTasksGet, "POST /api/tasks/item": s.handleTaskAdd, "POST /api/tasks/check": s.handleTaskCheck, "POST /api/tasks/update": s.handleTaskUpdate, "POST /api/tasks/rank": s.handleTasksRank, "POST /api/tasks/priority": s.handleTaskPriority, "POST /api/tasks/drop": s.handleTaskDrop, "POST /api/tasks/bucket": s.handleBucketRename, "POST /api/tasks/issue": s.handleIssueAdd, "POST /api/tasks/issue/resolve": s.handleIssueResolve,
	}
	for path, h := range routes {
		mux.HandleFunc(path, h)
	}
	webfs, _ := fs.Sub(webFiles, "web")
	files := http.FileServer(http.FS(webfs))
	assets := map[string]bool{}
	for _, f := range []string{"00-core.js", "05-components.js", "10-day.js", "20-goals.js", "90-todos.js"} {
		assets["/js/"+f] = true
	}
	for _, f := range []string{"99-local-fonts.css", "00-core.css", "05-primitives.css", "07-nav.css", "10-day.css", "20-goals.css", "90-todos.css", "93-todo-panel.css", "95-mobile.css"} {
		assets["/css/"+f] = true
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if path.Clean(r.URL.Path) != r.URL.Path {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/":
			b, _ := fs.ReadFile(webfs, "olga/index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
		case "/olga.js", "/olga.css", "/manifest.webmanifest":
			r.URL.Path = "/olga/" + strings.TrimPrefix(r.URL.Path, "/")
			files.ServeHTTP(w, r)
		default:
			if assets[r.URL.Path] || strings.HasPrefix(r.URL.Path, "/fonts/") || strings.HasPrefix(r.URL.Path, "/icons/") || r.URL.Path == "/manifest.webmanifest" {
				files.ServeHTTP(w, r)
			} else {
				http.NotFound(w, r)
			}
		}
	})
	var lock sync.Mutex // serialize the whole load/mutate/save transaction across devices
	gated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		mux.ServeHTTP(w, r)
	})
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	var authMu sync.Mutex
	var failures int
	var retryAt time.Time
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "https://"+r.Host && origin != "http://"+r.Host {
				http.Error(w, "origin rejected", 403)
				return
			}
		}
		raw, err := os.ReadFile(passwordFile)
		password := strings.TrimSpace(string(raw))
		if err != nil || password == "" {
			http.Error(w, "Olga’s Manifest is being prepared. Sign-in is not configured yet.", 503)
			return
		}
		sign := func(exp string) string {
			mac := hmac.New(sha256.New, key)
			mac.Write([]byte(password + "\x00" + exp))
			return hex.EncodeToString(mac.Sum(nil))
		}
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		cookie := func(value string, age int) {
			http.SetCookie(w, &http.Cookie{Name: "olga_session", Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age})
		}
		if r.URL.Path == "/logout" && r.Method == "POST" {
			cookie("", -1)
			http.Redirect(w, r, "/", 303)
			return
		}
		if r.URL.Path == "/login" && r.Method == "POST" {
			authMu.Lock()
			defer authMu.Unlock()
			if time.Now().Before(retryAt) {
				w.Header().Set("Retry-After", "30")
				http.Error(w, "Please wait a moment before trying again.", 429)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			r.ParseForm()
			got, want := sha256.Sum256([]byte(r.FormValue("password"))), sha256.Sum256([]byte(password))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				failures++
				if failures >= 5 {
					retryAt = time.Now().Add(30 * time.Second)
				}
				http.Redirect(w, r, "/?error=1", 303)
				return
			}
			failures = 0
			exp := strconv.FormatInt(time.Now().Add(30*24*time.Hour).Unix(), 10)
			cookie(exp+"."+sign(exp), 30*24*3600)
			http.Redirect(w, r, "/", 303)
			return
		}
		valid := false
		if c, e := r.Cookie("olga_session"); e == nil {
			parts := strings.Split(c.Value, ".")
			if len(parts) == 2 {
				exp, e := strconv.ParseInt(parts[0], 10, 64)
				valid = e == nil && exp > time.Now().Unix() && hmac.Equal([]byte(parts[1]), []byte(sign(parts[0])))
			}
		}
		if !valid {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Please sign in.", 401)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			msg := ""
			if r.URL.Query().Get("error") != "" {
				msg = "Incorrect password. Try again."
			}
			fmt.Fprintf(w, olgaLogin, msg)
			return
		}
		gated.ServeHTTP(w, r)
	}), nil
}

const olgaLogin = `<!doctype html><html><meta name="viewport" content="width=device-width,initial-scale=1"><title>Olga · Manifest</title><body style="margin:0;background:#ffffff;color:#2b2b2b;font:16px system-ui;min-height:100dvh;display:grid;place-items:center"><form action="/login" method="post" style="width:min(320px,85vw)"><p style="letter-spacing:.2em;color:#925b2e">◆ MANIFEST</p><h1 style="font-size:22px;font-weight:400">Welcome, Olga</h1><label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required autofocus style="box-sizing:border-box;width:100%%;margin:12px 0;padding:12px;background:transparent;border:1px solid #b78856;color:inherit;border-radius:6px;font:inherit"><button style="padding:12px;width:100%%;cursor:pointer;background:#925b2e;color:white;border:1px solid #925b2e;border-radius:6px;font:inherit">Sign in</button><p role="alert">%s</p></form></body></html>`
