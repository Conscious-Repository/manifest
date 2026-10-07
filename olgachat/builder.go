package olgachat

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed assets/olga-builder.json
var builderSettings []byte

//go:embed assets/builder.md
var builderPrompt string

//go:embed assets/gate.cjs
var gateScript []byte

// BuilderModel is the only model the builder runs.
const BuilderModel = "claude-opus-5-5"

// HerLayer is the only part of the repo an app change may touch.
const HerLayer = "server/web/olga/"

// GitBuilder makes app changes with Claude Code in a git worktree, previews
// them with a read-only build of her app, and ships them to her live app.
type GitBuilder struct {
	Origin     string // git remote URL to clone/push
	Seed       string // local checkout to clone from first (faster); "" → Origin
	Root       string // work root, e.g. ~/src/olga-work
	Vault      string // her vault root (for the read-only data copy)
	Runtime    string // dir holding the live binary, pending/settled markers
	ClaudeBin  string
	GoBin      string
	NodeBin    string // "" → gate skipped
	NodePath   string // NODE_PATH with playwright
	Restart    func() // exits so systemd restarts on the new binary
	Author     string // commit author for her changes
	PreviewTTL time.Duration

	mu       sync.Mutex
	previews map[string]*preview
}

type preview struct {
	cmd  *exec.Cmd
	port int
	last time.Time
}

func (g *GitBuilder) repo() string { return filepath.Join(g.Root, "repo") }

func (g *GitBuilder) run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("%s %s: %v: %s", filepath.Base(name), strings.Join(args, " "), err, tailText(out.String(), 400))
	}
	return out.String(), nil
}

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

func (g *GitBuilder) git(ctx context.Context, dir string, args ...string) (string, error) {
	return g.run(ctx, dir, []string{"GIT_TERMINAL_PROMPT=0"}, "git", args...)
}

// ensureRepo keeps a private clone that only Liber uses, so Benjamin's
// checkout is never touched.
func (g *GitBuilder) ensureRepo(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.repo(), ".git")); err != nil {
		if err := os.MkdirAll(g.Root, 0o755); err != nil {
			return err
		}
		src := firstNonEmpty(g.Seed, g.Origin)
		if _, err := g.git(ctx, g.Root, "clone", "--no-checkout", src, "repo"); err != nil {
			return err
		}
		if _, err := g.git(ctx, g.repo(), "remote", "set-url", "origin", g.Origin); err != nil {
			return err
		}
		ex := filepath.Join(g.repo(), ".git", "info", "exclude")
		_ = os.MkdirAll(filepath.Dir(ex), 0o755)
		_ = os.WriteFile(ex, []byte(".olga-data/\n.preview/\n"), 0o644)
	}
	_, err := g.git(ctx, g.repo(), "fetch", "-q", "origin", "main")
	return err
}

func (g *GitBuilder) worktree(id string) string { return filepath.Join(g.Root, id) }

// Build runs (or continues) one change.
func (g *GitBuilder) Build(ctx context.Context, ch *Change, brief string, recent []Exchange) (BuildResult, error) {
	res := BuildResult{Model: BuilderModel}
	if err := g.ensureRepo(ctx); err != nil {
		return res, err
	}
	wt := g.worktree(ch.ID)
	if ch.Worktree == "" {
		if _, err := g.git(ctx, g.repo(), "worktree", "add", "-f", "--detach", wt, "origin/main"); err != nil {
			return res, err
		}
		ch.Worktree = wt
	}
	g.stopPreview(ch.ID)
	if err := g.snapshot(filepath.Join(wt, ".olga-data")); err != nil {
		return res, err
	}
	settings := filepath.Join(g.Root, "olga-builder.json")
	if err := os.WriteFile(settings, builderSettings, 0o644); err != nil {
		return res, err
	}
	out, sess, err := g.claude(ctx, wt, settings, ch.Session, g.prompt(brief, recent, ch.Session != ""))
	res.Session = firstNonEmpty(sess, ch.Session)
	if err != nil {
		return res, err
	}
	files, outside, err := g.changed(ctx, wt)
	if err != nil {
		return res, err
	}
	res.Files = files
	if len(outside) > 0 || out.NeedsBenjamin != "" {
		// Nothing outside her layer is ever offered: undo the whole attempt.
		_, _ = g.git(ctx, wt, "checkout", "--", ".")
		_, _ = g.git(ctx, wt, "clean", "-fdq", "-e", ".olga-data")
		res.NeedsBenjamin = firstNonEmpty(out.NeedsBenjamin, out.Summary, "It needs changes outside her own part of the app.")
		if len(outside) > 0 {
			res.NeedsBenjamin += " (would touch: " + strings.Join(outside, ", ") + ")"
		}
		res.Files = nil
		return res, nil
	}
	if len(files) == 0 {
		res.NoChange = true
		return res, nil
	}
	res.Summary = out.Summary
	if p := g.startPreview(ctx, ch.ID); p != "" {
		res.Problem = p
		return res, nil
	}
	if p := g.gate(ctx, ch.ID); p != "" {
		// one retry with what broke
		out2, sess2, err := g.claude(ctx, wt, settings, res.Session, "The change broke this check on her phone-sized screen, please fix it without changing anything else:\n"+p)
		res.Session = firstNonEmpty(sess2, res.Session)
		if err != nil {
			return res, err
		}
		res.Summary = firstNonEmpty(out2.Summary, res.Summary)
		if files, outside, err = g.changed(ctx, wt); err != nil || len(outside) > 0 {
			res.Problem = "retry touched files outside her layer"
			return res, err
		}
		res.Files = files
		if p := g.startPreview(ctx, ch.ID); p != "" {
			res.Problem = p
			return res, nil
		}
		if p := g.gate(ctx, ch.ID); p != "" {
			res.Problem = "check failed twice: " + p
			return res, nil
		}
	}
	return res, nil
}

func (g *GitBuilder) prompt(brief string, recent []Exchange, follow bool) string {
	var b strings.Builder
	if !follow {
		b.WriteString(builderPrompt)
	}
	b.WriteString("\n\n## The change\n\n")
	b.WriteString(brief)
	if len(recent) > 0 {
		b.WriteString("\n\n## The conversation it came from (most recent last)\n\n")
		for _, e := range recent {
			b.WriteString(e.Who + ": " + e.Text + "\n")
		}
	}
	return b.String()
}

type builderOut struct {
	Summary       string   `json:"summary"`
	ChangedFiles  []string `json:"changed_files"`
	NeedsBenjamin string   `json:"needs_benjamin"`
}

const builderSchema = `{"type":"object","properties":{"summary":{"type":"string"},"changed_files":{"type":"array","items":{"type":"string"}},"needs_benjamin":{"type":"string"}},"required":["summary","changed_files"]}`

// ClaudeArgs is the one command line the builder runs (exported for the
// test that refuses any other shape).
func ClaudeArgs(settings, session, prompt string) []string {
	args := []string{"-p", prompt, "--model", BuilderModel, "--output-format", "json",
		"--json-schema", builderSchema, "--settings", settings, "--setting-sources", "",
		"--permission-mode", "dontAsk", "--strict-mcp-config", "--disable-slash-commands",
		"--disallowedTools", "WebFetch WebSearch"}
	if session != "" {
		args = append(args, "--resume", session)
	}
	return args
}

// CheckClaudeArgs refuses anything but that shape.
func CheckClaudeArgs(args []string) error {
	for i, a := range args {
		switch a {
		case "--dangerously-skip-permissions", "--add-dir", "--allow-dangerously-skip-permissions", "--mcp-config", "--plugin-dir":
			return fmt.Errorf("builder flag %s is not allowed", a)
		case "--model":
			if i+1 >= len(args) || args[i+1] != BuilderModel {
				return errors.New("builder must run " + BuilderModel)
			}
		case "--permission-mode":
			if i+1 >= len(args) || args[i+1] != "dontAsk" {
				return errors.New("builder must run in dontAsk mode")
			}
		}
	}
	return nil
}

func (g *GitBuilder) claude(ctx context.Context, wt, settings, session, prompt string) (builderOut, string, error) {
	args := ClaudeArgs(settings, session, prompt)
	if err := CheckClaudeArgs(args); err != nil {
		return builderOut{}, "", err
	}
	raw, err := g.run(ctx, wt, []string{"GOPROXY=off", "GOFLAGS=-mod=mod"}, firstNonEmpty(g.ClaudeBin, "claude"), args...)
	var r struct {
		Type             string          `json:"type"`
		Subtype          string          `json:"subtype"`
		IsError          bool            `json:"is_error"`
		SessionID        string          `json:"session_id"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if jerr := json.Unmarshal([]byte(lastJSONLine(raw)), &r); jerr != nil {
		if err != nil {
			return builderOut{}, "", err
		}
		return builderOut{}, "", fmt.Errorf("builder answer unreadable: %s", tailText(raw, 300))
	}
	if r.IsError || err != nil {
		return builderOut{}, r.SessionID, fmt.Errorf("builder failed (%s): %s", r.Subtype, tailText(r.Result, 300))
	}
	var out builderOut
	if len(r.StructuredOutput) > 0 {
		_ = json.Unmarshal(r.StructuredOutput, &out)
	}
	if out.Summary == "" {
		out.Summary = strings.TrimSpace(r.Result)
	}
	return out, r.SessionID, nil
}

func lastJSONLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n{"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// changed lists what the builder changed, and what of it falls outside her layer.
func (g *GitBuilder) changed(ctx context.Context, wt string) (files, outside []string, err error) {
	out, err := g.git(ctx, wt, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		p = strings.Trim(p, `"`)
		if strings.HasPrefix(p, ".olga-data/") || strings.HasPrefix(p, ".preview/") {
			continue
		}
		files = append(files, p)
		if !strings.HasPrefix(p, HerLayer) {
			outside = append(outside, p)
		}
	}
	return files, outside, nil
}

// snapshot copies her planner and the shared Home data for the builder to
// read (chat files excluded). Edits to the copy go nowhere.
func (g *GitBuilder) snapshot(dst string) error {
	_ = os.RemoveAll(dst)
	for _, sub := range []string{"system/olga", "system/home"} {
		src := filepath.Join(g.Vault, sub)
		err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(g.Vault, p)
			if d.IsDir() {
				if d.Name() == "chat" || d.Name() == "plan-history" {
					return filepath.SkipDir
				}
				return os.MkdirAll(filepath.Join(dst, rel), 0o755)
			}
			if strings.HasSuffix(p, ".lock") {
				return nil
			}
			return copyFile(p, filepath.Join(dst, rel))
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ---- previews ----

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startPreview builds her app from the worktree and runs it read-only on
// a copy of her data, under /preview/<id>/. "" on success, else what broke.
func (g *GitBuilder) startPreview(ctx context.Context, id string) string {
	g.stopPreview(id)
	wt := g.worktree(id)
	dir := filepath.Join(wt, ".preview")
	_ = os.MkdirAll(dir, 0o755)
	bin := filepath.Join(dir, "olga")
	if _, err := g.run(ctx, wt, []string{"GOPROXY=off", "CGO_ENABLED=0"}, firstNonEmpty(g.GoBin, "go"), "build", "-o", bin, "./cmd/olga"); err != nil {
		return "her app didn't build: " + tailText(err.Error(), 300)
	}
	return g.launchPreview(id)
}

func (g *GitBuilder) launchPreview(id string) string {
	wt := g.worktree(id)
	dir := filepath.Join(wt, ".preview")
	bin := filepath.Join(dir, "olga")
	if _, err := os.Stat(bin); err != nil {
		return "no preview build"
	}
	vault := filepath.Join(dir, "vault")
	if !dirExists(vault) {
		if err := g.snapshot(vault); err != nil {
			return err.Error()
		}
	}
	port, err := freePort()
	if err != nil {
		return err.Error()
	}
	cmd := exec.Command(bin, "-preview", "/preview/"+id, "-vault", vault, "-addr", "127.0.0.1:"+strconv.Itoa(port))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err.Error()
	}
	go func() { _ = cmd.Wait() }()
	for i := 0; i < 50; i++ {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond); err == nil {
			c.Close()
			g.mu.Lock()
			if g.previews == nil {
				g.previews = map[string]*preview{}
			}
			g.previews[id] = &preview{cmd: cmd, port: port, last: time.Now()}
			g.mu.Unlock()
			return ""
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return "the preview didn't start"
}

func dirExists(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

func (g *GitBuilder) stopPreview(id string) {
	g.mu.Lock()
	p := g.previews[id]
	delete(g.previews, id)
	g.mu.Unlock()
	if p != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

// PreviewPort returns the port of a change's preview, starting it again if
// the app restarted since (previews idle out after PreviewTTL).
func (g *GitBuilder) PreviewPort(id string) int {
	if !ValidID(id) {
		return 0
	}
	g.mu.Lock()
	p := g.previews[id]
	if p != nil {
		p.last = time.Now()
	}
	g.mu.Unlock()
	if p != nil {
		return p.port
	}
	if g.launchPreview(id) != "" {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if p := g.previews[id]; p != nil {
		return p.port
	}
	return 0
}

// Reap stops previews idle longer than PreviewTTL.
func (g *GitBuilder) Reap() {
	ttl := g.PreviewTTL
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	g.mu.Lock()
	var stale []string
	for id, p := range g.previews {
		if time.Since(p.last) > ttl {
			stale = append(stale, id)
		}
	}
	g.mu.Unlock()
	for _, id := range stale {
		g.stopPreview(id)
	}
}

func (g *GitBuilder) PreviewPath(ch *Change) string { return "/preview/" + ch.ID + "/" }

// gate renders the preview on a phone-sized screen and checks it still shows
// her data: no script errors, no sideways scrolling, every open Home task
// visible on TASKS, and the Timeline opening. "" when it passes or can't run.
func (g *GitBuilder) gate(ctx context.Context, id string) string {
	if g.NodeBin == "" {
		return ""
	}
	port := g.PreviewPort(id)
	if port == 0 {
		return "the preview didn't start"
	}
	script := filepath.Join(g.Root, "gate.cjs")
	if err := os.WriteFile(script, gateScript, 0o644); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := g.run(ctx, g.Root, []string{"NODE_PATH=" + g.NodePath}, g.NodeBin, script, fmt.Sprintf("http://127.0.0.1:%d/preview/%s/", port, id))
	if err != nil {
		if strings.Contains(out, "GATE-SKIP") {
			return ""
		}
		return tailText(firstNonEmpty(out, err.Error()), 600)
	}
	return ""
}

// Discard drops a change's worktree and preview.
func (g *GitBuilder) Discard(ch *Change) error {
	g.stopPreview(ch.ID)
	if ch.Worktree == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := g.git(ctx, g.repo(), "worktree", "remove", "--force", ch.Worktree)
	return err
}

// ---- shipping ----

var shipMu sync.Mutex

// Use commits her change on top of the current main, pushes it, builds and
// installs her app, and leaves the restart pending.
func (g *GitBuilder) Use(ctx context.Context, ch *Change) error {
	shipMu.Lock()
	defer shipMu.Unlock()
	if ch.Worktree == "" {
		return errors.New("the change is gone")
	}
	if _, err := g.git(ctx, ch.Worktree, "add", "-A", "--", HerLayer); err != nil {
		return err
	}
	patch, err := g.git(ctx, ch.Worktree, "diff", "--cached", "--binary", "HEAD", "--", HerLayer)
	if err != nil {
		return err
	}
	if strings.TrimSpace(patch) == "" {
		return errors.New("nothing to ship")
	}
	head, err := g.deploy(ctx, ch, func(dir string) error {
		pf := filepath.Join(g.Root, ch.ID+".patch")
		if err := os.WriteFile(pf, []byte(patch), 0o644); err != nil {
			return err
		}
		if _, err := g.git(ctx, dir, "apply", "--3way", pf); err != nil {
			return err
		}
		if _, err := g.git(ctx, dir, "add", "-A", "--", HerLayer); err != nil {
			return err
		}
		msg := "olga: " + clipWords(firstNonEmpty(ch.Brief, "a change Olga asked for"), 66) + "\n\nMade with Liber in Olga's Manifest (change " + ch.ID + ")."
		_, err := g.git(ctx, dir, "-c", "user.name=Benjamin Anderson", "-c", "user.email=me@benjaminbanderson.com", "commit", "-q", "--author", firstNonEmpty(g.Author, "Olga (via Liber) <olga@olgasobkiv.com>"), "-m", msg)
		return err
	})
	if err == nil {
		ch.Commit = head
	}
	return err
}

// Undo reverts a shipped change the same way.
func (g *GitBuilder) Undo(ctx context.Context, ch *Change) error {
	shipMu.Lock()
	defer shipMu.Unlock()
	if ch.Commit == "" {
		return errors.New("nothing to undo")
	}
	_, err := g.deploy(ctx, ch, func(dir string) error {
		_, err := g.git(ctx, dir, "-c", "user.name=Benjamin Anderson", "-c", "user.email=me@benjaminbanderson.com", "revert", "--no-edit", ch.Commit)
		return err
	})
	return err
}

// deploy prepares a clean tree at origin/main, lets edit commit, pushes
// (rebasing once if main moved), builds her app and installs it with a
// pending marker for the launcher, which proves it starts or rolls back.
func (g *GitBuilder) deploy(ctx context.Context, ch *Change, edit func(dir string) error) (string, error) {
	if err := g.ensureRepo(ctx); err != nil {
		return "", err
	}
	dir := filepath.Join(g.Root, "deploy")
	_, _ = g.git(ctx, g.repo(), "worktree", "remove", "--force", dir)
	if _, err := g.git(ctx, g.repo(), "worktree", "add", "-f", "--detach", dir, "origin/main"); err != nil {
		return "", err
	}
	defer func() { _, _ = g.git(context.Background(), g.repo(), "worktree", "remove", "--force", dir) }()
	if err := edit(dir); err != nil {
		return "", err
	}
	if _, err := g.git(ctx, dir, "push", "-q", "origin", "HEAD:main"); err != nil {
		if _, ferr := g.git(ctx, dir, "pull", "-q", "--rebase", "origin", "main"); ferr != nil {
			_, _ = g.git(ctx, dir, "rebase", "--abort")
			return "", err
		}
		if _, err := g.git(ctx, dir, "push", "-q", "origin", "HEAD:main"); err != nil {
			return "", err
		}
	}
	head, err := g.git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	head = strings.TrimSpace(head)
	if err := os.MkdirAll(g.Runtime, 0o755); err != nil {
		return "", err
	}
	next := filepath.Join(g.Runtime, "olga.next")
	if _, err := g.run(ctx, dir, []string{"GOPROXY=off", "CGO_ENABLED=0"}, firstNonEmpty(g.GoBin, "go"), "build", "-o", next, "./cmd/olga"); err != nil {
		return "", err
	}
	live := filepath.Join(g.Runtime, "olga")
	if _, err := os.Stat(live); err == nil {
		if err := os.Rename(live, filepath.Join(g.Runtime, "olga.prev")); err != nil {
			return "", err
		}
	}
	if err := os.Rename(next, live); err != nil {
		return "", err
	}
	_ = os.Remove(filepath.Join(g.Runtime, "settled"))
	return head, os.WriteFile(filepath.Join(g.Runtime, "pending"), []byte(ch.ID+"\n"), 0o644)
}

// Settled reads the launcher's verdict for a change ("live" | "failed" | "").
func (g *GitBuilder) Settled(ch *Change) string {
	b, err := os.ReadFile(filepath.Join(g.Runtime, "settled"))
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) == 2 && f[0] == ch.ID {
		return f[1]
	}
	return ""
}

// Previews lists running previews (for tests and diagnostics).
func (g *GitBuilder) Previews() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var ids []string
	for id := range g.previews {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
