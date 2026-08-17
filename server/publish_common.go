package server

// Shared git/diff primitives for the remaining Real Estate publisher. AION
// deliberately does not import or use this file: its contract is projected
// live by AionLive.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type publishPortalCfg struct{ Path, Remote, Branch string }

func gitRun(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		tail := text
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		return text, fmt.Errorf("git %s: %v — %s", strings.Join(args, " "), err, tail)
	}
	return text, nil
}

const (
	gitLocalTimeout = 30 * time.Second
	gitPushTimeout  = 120 * time.Second
	gitPushTries    = 3
)

func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "non-fast-forward") || strings.Contains(s, "fetch first")
}

func gitPushRetry(dir, remote, branch string) (string, error) {
	var out string
	var err error
	for attempt := 0; attempt < gitPushTries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		out, err = gitRun(dir, gitPushTimeout, "push", remote, branch)
		if err == nil || isNonFastForward(err) {
			return out, err
		}
	}
	return out, err
}

func reconcileAndRetryPush(p publishPortalCfg, commit string, pushErr error) (string, error) {
	if _, err := gitRun(p.Path, gitPushTimeout, "fetch", p.Remote, p.Branch); err != nil {
		return commit, pushErr
	}
	if _, err := gitRun(p.Path, gitLocalTimeout, "rebase", "FETCH_HEAD"); err != nil {
		_, _ = gitRun(p.Path, gitLocalTimeout, "rebase", "--abort")
		return commit, pushErr
	}
	if h, err := gitRun(p.Path, gitLocalTimeout, "rev-parse", "HEAD"); err == nil {
		commit = strings.TrimSpace(h)
	}
	if _, err := gitPushRetry(p.Path, p.Remote, p.Branch); err != nil {
		return commit, err
	}
	return commit, nil
}

type publishRecord struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Stage        string   `json:"stage,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Files        []string `json:"files,omitempty"`
	Error        string   `json:"error,omitempty"`
	At           string   `json:"at"`
	Acknowledged bool     `json:"acknowledged"`
}

type publishLog struct {
	mu   sync.Mutex
	path string
}

func (l *publishLog) list() []publishRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.load()
}

func (l *publishLog) load() []publishRecord {
	var out []publishRecord
	if b, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (l *publishLog) append(r publishRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	recs := append(l.load(), r)
	if os.MkdirAll(filepath.Dir(l.path), 0o755) != nil {
		return
	}
	b, _ := json.MarshalIndent(recs, "", "  ")
	_ = os.WriteFile(l.path, b, 0o644)
}

func (l *publishLog) ack(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recs := l.load()
	for i := range recs {
		if recs[i].ID == id {
			recs[i].Acknowledged = true
			b, _ := json.MarshalIndent(recs, "", "  ")
			_ = os.WriteFile(l.path, b, 0o644)
			return true
		}
	}
	return false
}

type publishFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Diff   string `json:"diff,omitempty"`
}

func sectionNames(paths []string) []string {
	var out []string
	for _, p := range paths {
		base := filepath.Base(p)
		out = append(out, strings.TrimSuffix(strings.TrimSuffix(base, ".json"), ".md"))
	}
	sort.Strings(out)
	return out
}

func lineDiff(a, b []byte) string {
	al, bl := splitLines(a), splitLines(b)
	n, m := len(al), len(bl)
	if n*m > 4_000_000 {
		return fmt.Sprintf("(diff too large: %d → %d lines)", n, m)
	}
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && al[i] == bl[j]:
			i, j = i+1, j+1
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			out, i = append(out, "- "+al[i]), i+1
		default:
			out, j = append(out, "+ "+bl[j]), j+1
		}
	}
	if len(out) > 200 {
		out = append(out[:200], fmt.Sprintf("… %d more lines", len(out)-200))
	}
	return strings.Join(out, "\n")
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}
