// Package agents implements a crash-proof, lock-free coordination substrate for
// AI agents over the vault: a Maildir-style queue where the folder IS the status
// and every state transition is a single atomic os.Rename. No locks, no DB.
//
// All status folders live under one root (same filesystem) so rename(2) is
// atomic: a reader never sees a half-written file, and exactly one claimer can
// win a given inbox item.
package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Task is a unit of work. Its ID doubles as the on-disk filename stem, so a
// rename moves the whole task between status folders.
type Task struct {
	ID        string
	Type      string
	CreatedAt time.Time
	Body      string
}

// Queue is the Maildir over <vault>/Agents.
type Queue struct {
	root string
	log  *RunLog
}

func NewQueue(root string) (*Queue, error) {
	q := &Queue{root: root}
	if err := q.ensureDirs(); err != nil {
		return nil, err
	}
	q.log = NewRunLog(filepath.Join(root, "run.log"))
	return q, nil
}

func (q *Queue) Root() string { return q.root }

func (q *Queue) ensureDirs() error {
	for _, d := range []string{
		"tmp", "inbox", "claimed", "done", "failed", "outbox",
		"approvals/pending", "approvals/approved", "approvals/rejected",
	} {
		if err := os.MkdirAll(filepath.Join(q.root, filepath.FromSlash(d)), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queue) dir(parts ...string) string {
	return filepath.Join(append([]string{q.root}, parts...)...)
}

// Post writes the task fully into tmp/, fsyncs, then atomically renames it into
// inbox/. A reader scanning inbox/ therefore never sees a partial file.
func (q *Queue) Post(t Task) (Task, error) {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	if t.ID == "" {
		t.ID = newID(t.Type, t.Body, t.CreatedAt)
	}
	tmp := q.dir("tmp", t.ID+".md")
	final := q.dir("inbox", t.ID+".md")
	f, err := os.Create(tmp)
	if err != nil {
		return Task{}, err
	}
	if _, err := f.WriteString(marshalTask(t)); err != nil {
		f.Close()
		return Task{}, err
	}
	if err := f.Sync(); err != nil { // durable before publish
		f.Close()
		return Task{}, err
	}
	if err := f.Close(); err != nil {
		return Task{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return Task{}, err
	}
	q.log.Append("post", t.ID, "type", t.Type)
	return t, nil
}

// Claim atomically renames one inbox item into claimed/<agent>/. Among N racing
// claimers exactly one wins (the kernel guarantees a single successful rename);
// the losers get ENOENT and move on.
func (q *Queue) Claim(agent string) (Task, bool, error) {
	entries, err := os.ReadDir(q.dir("inbox"))
	if err != nil {
		return Task{}, false, err
	}
	if err := os.MkdirAll(q.dir("claimed", agent), 0o755); err != nil {
		return Task{}, false, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		src := q.dir("inbox", e.Name())
		dst := q.dir("claimed", agent, e.Name())
		if err := os.Rename(src, dst); err != nil {
			continue // another worker won this one
		}
		// Stamp the claim time: rename preserves the post mtime, but Sweep needs
		// "time since claimed" to detect dead workers.
		now := time.Now()
		_ = os.Chtimes(dst, now, now)
		content, _ := os.ReadFile(dst)
		t := parseTask(strings.TrimSuffix(e.Name(), ".md"), string(content))
		q.log.Append("claim", t.ID, "agent", agent)
		return t, true, nil
	}
	return Task{}, false, nil
}

// Complete moves a claimed task to done/.
func (q *Queue) Complete(agent, id string) error {
	if err := os.Rename(q.dir("claimed", agent, id+".md"), q.dir("done", id+".md")); err != nil {
		return err
	}
	q.log.Append("done", id, "agent", agent)
	return nil
}

// Fail moves a claimed task to failed/, recording the reason.
func (q *Queue) Fail(agent, id, reason string) error {
	src := q.dir("claimed", agent, id+".md")
	if content, err := os.ReadFile(src); err == nil {
		_ = os.WriteFile(src, append(content, []byte("\n> failed: "+reason+"\n")...), 0o644)
	}
	if err := os.Rename(src, q.dir("failed", id+".md")); err != nil {
		return err
	}
	q.log.Append("fail", id, "agent", agent, "reason", reason)
	return nil
}

// IsDone reports whether a task id has already completed — the basis for
// idempotent replay (re-running a completed id is a no-op).
func (q *Queue) IsDone(id string) bool {
	_, err := os.Stat(q.dir("done", id+".md"))
	return err == nil
}

// Sweep reclaims stale claims: any claimed task older than timeout returns to
// inbox/ (its worker is presumed dead). At-least-once + idempotency makes this
// effectively-once. Returns the number reclaimed.
func (q *Queue) Sweep(timeout time.Duration) int {
	reclaimed := 0
	agents, _ := os.ReadDir(q.dir("claimed"))
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		es, _ := os.ReadDir(q.dir("claimed", a.Name()))
		for _, e := range es {
			if !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil || time.Since(info.ModTime()) <= timeout {
				continue
			}
			if os.Rename(q.dir("claimed", a.Name(), e.Name()), q.dir("inbox", e.Name())) == nil {
				reclaimed++
				q.log.Append("reclaim", strings.TrimSuffix(e.Name(), ".md"), "from", a.Name())
			}
		}
	}
	return reclaimed
}

// Counts returns queue depths for the dashboard.
func (q *Queue) Counts() map[string]int {
	c := map[string]int{}
	for _, d := range []string{"inbox", "done", "failed", "outbox"} {
		c[d] = countMD(q.dir(d))
	}
	claimed := 0
	agents, _ := os.ReadDir(q.dir("claimed"))
	for _, a := range agents {
		if a.IsDir() {
			claimed += countMD(q.dir("claimed", a.Name()))
		}
	}
	c["claimed"] = claimed
	return c
}

// OutboxItem is a result/digest surfaced in the dashboard.
type OutboxItem struct {
	Name    string    `json:"name"`
	Title   string    `json:"title"`
	ModTime time.Time `json:"modTime"`
}

// Outbox lists the most recent outbox entries, newest first.
func (q *Queue) Outbox(limit int) []OutboxItem {
	entries, _ := os.ReadDir(q.dir("outbox"))
	var items []OutboxItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		content, _ := os.ReadFile(q.dir("outbox", e.Name()))
		items = append(items, OutboxItem{Name: e.Name(), Title: firstTitle(string(content), e.Name()), ModTime: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ModTime.After(items[j].ModTime) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

// ----- helpers -----

func countMD(dir string) int {
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func newID(typ, body string, t time.Time) string {
	h := sha256.Sum256([]byte(typ + "\x00" + body))
	return t.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(h[:])[:8]
}

func marshalTask(t Task) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("id: " + t.ID + "\n")
	b.WriteString("type: " + t.Type + "\n")
	b.WriteString("created: " + t.CreatedAt.UTC().Format(time.RFC3339) + "\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(t.Body, "\n"))
	b.WriteString("\n")
	return b.String()
}

func parseTask(id, content string) Task {
	t := Task{ID: id}
	fm, body := splitFrontmatter(content)
	t.Body = strings.TrimRight(body, "\n")
	for k, v := range fm {
		switch k {
		case "id":
			if v != "" {
				t.ID = v
			}
		case "type":
			t.Type = v
		case "created":
			t.CreatedAt, _ = time.Parse(time.RFC3339, v)
		}
	}
	return t
}

// splitFrontmatter returns the leading ---...--- key:value map and the body.
func splitFrontmatter(content string) (map[string]string, string) {
	fm := map[string]string{}
	if !strings.HasPrefix(content, "---\n") {
		return fm, content
	}
	idx := strings.Index(content, "\n---")
	if idx < 0 {
		return fm, content
	}
	block := content[4:idx]
	rest := content[idx+4:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	for _, line := range strings.Split(block, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fm[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return fm, strings.TrimPrefix(rest, "\n")
}

func firstTitle(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "# "))
		}
		if line != "" && !strings.HasPrefix(line, "---") && !strings.Contains(line, ":") {
			return line
		}
	}
	return strings.TrimSuffix(fallback, ".md")
}
