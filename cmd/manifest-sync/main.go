// Command manifest-sync keeps N git roots (the vault, the harnesses repo)
// converged across machines, hands-free (big-change Phase 2). Per root:
// fsnotify watch → debounce (~15s of quiet) → `git add -A` + commit →
// `git pull --rebase` → push; plus a steady interval pull so remote-only
// changes land without local activity.
//
// Conflict doctrine: STOP, PARK, MARK. A rebase that conflicts is aborted
// (the tree returns to its pre-pull state), a conflict state file is written
// to <state>/<name>.conflict.json, and the root goes quiet — no retries, no
// resolution attempts. The dashboard's SyncConflicts signal emitter reads
// that file into a FEED card; the HUMAN resolves (pull/rebase by hand), and
// the parked root auto-resumes once the repo shows no rebase-in-progress and
// no unmerged paths. The human is the mutex; the FEED is where the mutex
// gets paged.
//
// Same binary everywhere: launchd on the laptop, systemd on metis (units in
// deploy/). dataDir-style state is per-machine and never synced.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"manifest/record"
)

// rootSpec is one synced repository, named for state files + commit messages.
type rootSpec struct {
	Name string
	Path string
}

type rootList []rootSpec

func (r *rootList) String() string { return fmt.Sprint(*r) }
func (r *rootList) Set(v string) error {
	name, path, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
		return fmt.Errorf("want name=path, got %q", v)
	}
	*r = append(*r, rootSpec{Name: strings.TrimSpace(name), Path: expand(strings.TrimSpace(path))})
	return nil
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// conflictState is the park marker (and the signal emitter's source).
type conflictState struct {
	Root    string    `json:"root"`
	Name    string    `json:"name"`
	Paths   []string  `json:"paths"` // unmerged files at the moment of the conflict
	Since   time.Time `json:"since"`
	Message string    `json:"message"` // the git error, for the log-minded
}

// syncer runs one root's converge loop.
type syncer struct {
	spec     rootSpec
	stateDir string
	host     string
	debounce time.Duration
	interval time.Duration

	mu     sync.Mutex // serializes cycles
	kick   chan struct{}
	parked bool
}

func main() {
	var roots rootList
	stateDir := flag.String("state", expand("~/.config/manifest/sync"), "conflict/state dir (per-machine, never synced)")
	debounce := flag.Duration("debounce", 15*time.Second, "quiet period after a change before a sync cycle")
	interval := flag.Duration("interval", 60*time.Second, "steady pull interval (remote-only changes)")
	flag.Var(&roots, "root", "name=path of a git root to sync (repeatable)")
	flag.Parse()
	if len(roots) == 0 {
		log.Fatal("no roots — pass at least one -root name=path")
	}
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		log.Fatal(err)
	}
	host, _ := os.Hostname()
	host = strings.TrimSuffix(host, ".local")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	for _, spec := range roots {
		if _, err := os.Stat(filepath.Join(spec.Path, ".git")); err != nil {
			log.Fatalf("%s: %s is not a git root", spec.Name, spec.Path)
		}
		s := &syncer{spec: spec, stateDir: *stateDir, host: host,
			debounce: *debounce, interval: *interval, kick: make(chan struct{}, 1)}
		s.parked = s.conflictFileExists()
		wg.Add(1)
		go func() { defer wg.Done(); s.run(ctx) }()
		log.Printf("%s: syncing %s (debounce %s, interval %s)%s",
			spec.Name, spec.Path, *debounce, *interval, map[bool]string{true: " [PARKED on prior conflict]"}[s.parked])
	}
	wg.Wait()
}

// run wires the watcher (events → debounce timer) and the steady ticker into
// serialized cycles. A failed watcher degrades to interval-only syncing.
func (s *syncer) run(ctx context.Context) {
	var timerMu sync.Mutex
	timer := time.NewTimer(s.debounce) // first cycle shortly after start
	if w, err := record.NewWatch(s.spec.Path, []string{".git", "vessel", "node_modules"}); err == nil {
		w.Subscribe(func(record.Event) {
			timerMu.Lock()
			timer.Reset(s.debounce)
			timerMu.Unlock()
		})
		if err := w.Start(ctx); err != nil {
			log.Printf("%s: watch start failed (interval-only): %v", s.spec.Name, err)
		}
		defer w.Close()
	} else {
		log.Printf("%s: watch unavailable (interval-only): %v", s.spec.Name, err)
	}
	tick := time.NewTicker(s.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.cycle()
		case <-tick.C:
			s.cycle()
		}
	}
}

// cycle is one converge attempt: resume-check → add/commit → pull --rebase
// (conflict → abort + park) → push. Errors other than conflicts just log —
// the next tick retries.
func (s *syncer) cycle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parked {
		if !s.canResume() {
			return
		}
		s.clearConflict()
		log.Printf("%s: conflict resolved by hand — resuming", s.spec.Name)
	}
	// stage + commit local changes (everything: this medium's commits are the
	// sync record, not authored checkpoints)
	if _, err := s.git("add", "-A"); err != nil {
		log.Printf("%s: add: %v", s.spec.Name, err)
		return
	}
	if _, err := s.git("diff", "--cached", "--quiet"); err != nil { // exit 1 = staged changes
		msg := fmt.Sprintf("sync: %s %s", s.host, time.Now().Format("2006-01-02 15:04:05"))
		if out, err := s.git("-c", "user.name=manifest-sync ("+s.host+")",
			"-c", "user.email=manifest-sync@"+s.host+".invalid",
			"commit", "-q", "-m", msg); err != nil {
			log.Printf("%s: commit: %v — %s", s.spec.Name, err, firstLine(out))
			return
		}
		log.Printf("%s: committed local changes", s.spec.Name)
	}
	// converge with the remote
	if out, err := s.git("pull", "--rebase", "-q"); err != nil {
		if s.rebaseInProgress() || strings.Contains(out, "CONFLICT") || strings.Contains(out, "could not apply") {
			s.park(out)
			return
		}
		log.Printf("%s: pull: %v — %s (will retry)", s.spec.Name, err, firstLine(out))
		return
	}
	// push if ahead
	if out, err := s.git("rev-list", "--count", "@{u}..HEAD"); err == nil && strings.TrimSpace(out) != "0" {
		if pout, err := s.git("push", "-q"); err != nil {
			log.Printf("%s: push: %v — %s (will retry)", s.spec.Name, err, firstLine(pout))
			return
		}
		log.Printf("%s: pushed", s.spec.Name)
	}
}

// park: capture the unmerged paths, abort the rebase (tree restored), write
// the conflict marker, go quiet. STOP, PARK, MARK — nothing else is touched.
func (s *syncer) park(gitOut string) {
	paths := []string{}
	if out, err := s.git("diff", "--name-only", "--diff-filter=U"); err == nil {
		for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
			if ln != "" {
				paths = append(paths, ln)
			}
		}
	}
	_, _ = s.git("rebase", "--abort")
	st := conflictState{Root: s.spec.Path, Name: s.spec.Name, Paths: paths,
		Since: time.Now(), Message: firstLine(gitOut)}
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := os.WriteFile(s.conflictFile(), b, 0o644); err != nil {
		log.Printf("%s: writing conflict state: %v", s.spec.Name, err)
	}
	s.parked = true
	log.Printf("%s: CONFLICT — parked (%d file(s): %s). Resolve rebase-style "+
		"(git pull --rebase → fix → add → rebase --continue); sync resumes itself.",
		s.spec.Name, len(paths), strings.Join(paths, ", "))
}

// canResume: the human finished — no rebase in progress, no unmerged paths.
func (s *syncer) canResume() bool {
	if s.rebaseInProgress() {
		return false
	}
	out, err := s.git("diff", "--name-only", "--diff-filter=U")
	return err == nil && strings.TrimSpace(out) == ""
}

func (s *syncer) rebaseInProgress() bool {
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(s.spec.Path, ".git", d)); err == nil {
			return true
		}
	}
	return false
}

func (s *syncer) conflictFile() string {
	return filepath.Join(s.stateDir, s.spec.Name+".conflict.json")
}
func (s *syncer) conflictFileExists() bool {
	_, err := os.Stat(s.conflictFile())
	return err == nil
}
func (s *syncer) clearConflict() {
	_ = os.Remove(s.conflictFile())
	s.parked = false
}

func (s *syncer) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", s.spec.Path}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0") // never hang on an auth prompt
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}
