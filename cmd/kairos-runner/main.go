// Command kairos-runner adapts a NousResearch hermes-agent installation to
// the harness file contract (kairos plan Phase B). It is the "engine" of the
// kairos harness: it POLLS <root>/vessel/spool/*.json (polling, not inotify —
// the tree lives on an NFS mount shared between metis and lab-apps, and
// events don't propagate across network mounts), runs the agent headless with
// the work-order text, and writes the results in the contract shapes manifest
// already reads:
//
//	artifacts/runs/<YYYY-MM-DD>-kairos-<runid>.md   (run report; running → final)
//	artifacts/library/<runid>-brief.md              (the answer)
//	vessel/state/engine.heartbeat                    (touched every 30s)
//
// Nothing in manifest knows or cares that the runtime is hermes-agent rather
// than the excalibur engine — the file shapes ARE the interface.
//
// Crash recovery: an interrupted claim (vessel/state/active.json) is re-run
// on startup; a lingering `outcome: running` report is finalized as failed so
// manifest's double-spool guard (IsActive) can never wedge the harness.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type spoolOrder struct {
	Kind        string `json:"kind"` // "" = run order; "chat" etc. are not ours
	Spirit      string `json:"spirit"`
	Ritual      string `json:"ritual"`
	Request     string `json:"request"`
	RequestedAt string `json:"requested_at"`
}

type runner struct {
	root    string
	bin     string
	args    []string // {reqfile} substitutes a temp file path; otherwise request rides stdin
	model   string
	timeout time.Duration
}

func main() {
	root := flag.String("root", "/shared/apps/kairos", "harness tree root")
	bin := flag.String("bin", "hermes", "hermes-agent binary")
	argStr := flag.String("args", "", "space-separated args for the headless invocation; {request} = the request text as one argv element, {reqfile} = a temp file carrying it; neither → the request is piped on stdin")
	model := flag.String("model", "", "informational model label stamped on run reports")
	timeout := flag.Duration("timeout", 10*time.Minute, "per-run ceiling")
	poll := flag.Duration("poll", 3*time.Second, "spool poll interval")
	flag.Parse()

	r := &runner{root: *root, bin: *bin, model: *model, timeout: *timeout}
	if strings.TrimSpace(*argStr) != "" {
		r.args = strings.Fields(*argStr)
	}
	for _, d := range []string{
		filepath.Join(*root, "vessel", "spool"),
		filepath.Join(*root, "vessel", "state"),
		filepath.Join(*root, "artifacts", "runs"),
		filepath.Join(*root, "artifacts", "library"),
	} {
		if err := os.MkdirAll(d, 0o775); err != nil {
			log.Fatalf("mkdir %s: %v", d, err)
		}
	}
	r.failLingering()
	go r.heartbeat()
	log.Printf("kairos-runner: serving %s (bin=%s poll=%s timeout=%s)", *root, *bin, *poll, *timeout)

	// crash recovery: an interrupted claim runs first
	if _, err := os.Stat(r.activePath()); err == nil {
		r.process(r.activePath())
	}
	t := time.NewTicker(*poll)
	for range t.C {
		if p := r.oldestOrder(); p != "" {
			if err := os.Rename(p, r.activePath()); err != nil {
				continue // racing writer or vanished — next tick
			}
			r.process(r.activePath())
		}
	}
}

func (r *runner) activePath() string { return filepath.Join(r.root, "vessel", "state", "active.json") }

func (r *runner) heartbeat() {
	hb := filepath.Join(r.root, "vessel", "state", "engine.heartbeat")
	for {
		now := time.Now()
		_ = os.WriteFile(hb, []byte(now.Format(time.RFC3339)+"\n"), 0o664)
		_ = os.Chtimes(hb, now, now)
		time.Sleep(30 * time.Second)
	}
}

// oldestOrder returns the oldest consumable spool file ("" when none). Spool
// filenames lead with unix-nanos, so lexicographic order is arrival order.
func (r *runner) oldestOrder() string {
	dir := filepath.Join(r.root, "vessel", "spool")
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var o spoolOrder
		if json.Unmarshal(b, &o) != nil || o.Kind != "" || strings.TrimSpace(o.Request) == "" {
			continue // not a run order (chat spools etc. are someone else's)
		}
		return filepath.Join(dir, n)
	}
	return ""
}

// failLingering finalizes any `outcome: running` report left by a crash —
// otherwise manifest's IsActive guard refuses new work forever.
func (r *runner) failLingering() {
	dir := filepath.Join(r.root, "artifacts", "runs")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(b), "outcome: running") {
			continue
		}
		next := strings.Replace(string(b), "outcome: running", "outcome: failed", 1)
		if os.WriteFile(p, []byte(next), 0o664) == nil {
			log.Printf("kairos-runner: finalized crashed run %s as failed", e.Name())
		}
	}
}

func (r *runner) process(claimPath string) {
	b, err := os.ReadFile(claimPath)
	if err != nil {
		return
	}
	var o spoolOrder
	if json.Unmarshal(b, &o) != nil || strings.TrimSpace(o.Request) == "" {
		_ = os.Remove(claimPath)
		return
	}
	started := time.Now()
	runID := started.Format("20060102-150405") + "-" + randHex(4)
	reportPath := filepath.Join(r.root, "artifacts", "runs",
		started.Format("2006-01-02")+"-kairos-"+runID+".md")
	r.writeReport(reportPath, o, runID, started, time.Time{}, "running", "")
	log.Printf("kairos-runner: run %s started (%s/%s)", runID, o.Spirit, o.Ritual)

	answer, runErr := r.invoke(o.Request)
	finished := time.Now()
	outcome := "completed"
	if runErr != nil || strings.TrimSpace(answer) == "" {
		outcome = "failed"
		if runErr != nil {
			answer = strings.TrimSpace(answer + "\n\n[kairos-runner] agent invocation failed: " + runErr.Error())
		}
	}
	if outcome == "completed" {
		r.writeBrief(runID, started, answer)
	}
	r.writeReport(reportPath, o, runID, started, finished, outcome, answer)
	_ = os.Remove(claimPath)
	log.Printf("kairos-runner: run %s %s (%.0fs)", runID, outcome, finished.Sub(started).Seconds())
}

// invoke runs the hermes-agent binary headless with the work-order text.
func (r *runner) invoke(request string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	args := make([]string, 0, len(r.args))
	delivered := false // request already rides argv or a temp file → skip stdin
	var reqFile string
	for _, a := range r.args {
		if strings.Contains(a, "{request}") {
			a = strings.ReplaceAll(a, "{request}", request)
			delivered = true
			args = append(args, a)
			continue
		}
		if strings.Contains(a, "{reqfile}") {
			if reqFile == "" {
				f, err := os.CreateTemp("", "kairos-req-*.md")
				if err != nil {
					return "", err
				}
				reqFile = f.Name()
				if _, err := f.WriteString(request); err != nil {
					f.Close()
					return "", err
				}
				f.Close()
				defer os.Remove(reqFile)
			}
			a = strings.ReplaceAll(a, "{reqfile}", reqFile)
			delivered = true
		}
		args = append(args, a)
	}
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir = r.root // the agent's working dir is its own tree
	if !delivered {
		cmd.Stdin = strings.NewReader(request)
	}
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil && errb.Len() > 0 {
		err = fmt.Errorf("%v: %s", err, strings.TrimSpace(lastLines(errb.String(), 5)))
	}
	return strings.TrimSpace(out.String()), err
}

// writeReport writes the contract run report. The request rides frontmatter
// in FULL on one line (we control the writer — no truncation, tokens intact)
// and the body carries request + result verbatim.
func (r *runner) writeReport(path string, o spoolOrder, runID string, started, finished time.Time, outcome, answer string) {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("run: " + runID + "\n")
	b.WriteString("spirit: " + orStr(o.Spirit, "kairos") + "\n")
	b.WriteString("ritual: " + orStr(o.Ritual, "delegate") + "\n")
	b.WriteString("request: " + fmt.Sprintf("%q", oneLine(o.Request)) + "\n")
	b.WriteString("started: " + started.UTC().Format(time.RFC3339) + "\n")
	if !finished.IsZero() {
		b.WriteString("finished: " + finished.UTC().Format(time.RFC3339) + "\n")
	}
	b.WriteString("outcome: " + outcome + "\n")
	if r.model != "" {
		b.WriteString("model: " + r.model + "\n")
	}
	b.WriteString("portal: hermes-agent\n")
	b.WriteString("---\n\n## request\n\n" + o.Request + "\n")
	if answer != "" {
		b.WriteString("\n## result\n\n" + answer + "\n")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o664); err == nil {
		_ = os.Rename(tmp, path)
	}
}

// writeBrief writes the library brief manifest's ingestion reads (the answer
// IS the deliverable — plan, questions, reply or result per the protocol).
func (r *runner) writeBrief(runID string, started time.Time, answer string) {
	title := firstLine(answer, 60)
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + fmt.Sprintf("%q", title) + "\n")
	b.WriteString("run: " + runID + "\n")
	b.WriteString("date: " + started.UTC().Format(time.RFC3339) + "\n")
	b.WriteString("---\n" + answer + "\n")
	path := filepath.Join(r.root, "artifacts", "library", runID+"-brief.md")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o664); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimLeft(s, "# ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func orStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
