package server

import (
	"bytes"
	"context"
	"fmt"
	"manifest/artifacts"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleTermChangesSnapshot(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	release, allowed := s.guardTerminalShare(w, se)
	if !allowed {
		return
	}
	defer release()

	if s.artifactReg == nil || se.Device != "" || se.Cwd == "" {
		http.Error(w, "working-folder snapshot unavailable", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	content, err := workingChanges(ctx, se.Cwd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	content = "Snapshot of runtime " + se.ID + " (" + se.Kind + "). Captured bytes do not change after this review.\n\n" + content
	task := ""
	for _, link := range s.terminalConversation(se).Links {
		if link.Kind == "task" {
			task = link.ID
		}
	}
	result, err := s.artifactReg.Put(artifacts.Put{Kind: artifacts.KindReport, Title: "Working-folder changes", Harness: se.Kind, Ref: "artifacts/runtime/" + se.ID + "/changes.diff", Content: []byte(content), Actor: "owner", At: time.Now(), Provenance: artifacts.Provenance{Source: "runtime-changes", Task: task, Run: se.ID, Session: s.runtimeArtifactScope(se)}})
	if err != nil {
		httpError(w, err)
		return
	}
	s.artifactEvent(result, "owner")
	writeJSON(w, map[string]any{"id": result.Artifact.ID, "revision": result.Artifact.Head, "task": task, "discuss": true})
}

// Bound subprocess output without allowing Git external diff/textconv programs.
type changeOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *changeOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 1536*1024 - b.Len()
	if remaining < len(p) {
		b.overflow = true
		p = p[:remaining]
	}
	b.Buffer.Write(p)
	return n, nil
}
func workingChanges(ctx context.Context, cwd string) (string, error) {
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.quotePath=true"}, args...)...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
		var out changeOutput
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("working-folder Git review unavailable")
		}
		if out.overflow {
			return "", fmt.Errorf("changes exceed the preview limit; inspect this working folder in Terminal")
		}
		return out.String(), nil
	}
	head, err := run("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	diff, err := run("diff", "--no-ext-diff", "--no-textconv", "--no-color", strings.TrimSpace(head), "--")
	if err != nil {
		return "", err
	}
	untracked, err := run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Working folder: %s\nCompared with HEAD: %s\nRead at: %s\n\nShared working-tree changes, including staged and unstaged tracked files. These are not attributed exclusively to this runtime. Files may change while this review is being read.\n\n", cwd, strings.TrimSpace(head), time.Now().UTC().Format(time.RFC3339))
	if diff == "" {
		out.WriteString("No tracked changes against HEAD.\n")
	} else {
		out.WriteString(diff)
	}
	if untracked != "" {
		out.WriteString("\nUntracked files (contents not included):\n")
		for _, name := range strings.Split(strings.TrimSuffix(untracked, "\x00"), "\x00") {
			out.WriteString(strconv.Quote(name) + "\n")
		}
	}
	return out.String(), nil
}

func (s *Server) handleTermChanges(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	if se.Device != "" || se.Cwd == "" {
		http.Error(w, "working-folder review is unavailable for this runtime", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	text, err := workingChanges(ctx, se.Cwd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	fmt.Fprint(w, text)
}

// GET /api/terminal/session/{id}/changes/stat — the working tree against
// HEAD in numbers, for the live +N −M chip: lines added and removed in
// tracked files (staged and unstaged), files changed, binary files, and
// untracked files. Same Git safety as the review (no external diff or
// textconv, no optional locks); the review itself is …/changes.
func (s *Server) handleTermChangesStat(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	if se.Device != "" || se.Cwd == "" {
		http.Error(w, "working-folder review is unavailable for this runtime", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	st, err := workingChangesStat(ctx, se.Cwd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, st)
}

type changesStat struct {
	Added     int    `json:"added"`
	Removed   int    `json:"removed"`
	Files     int    `json:"files"`
	Binary    int    `json:"binary"`
	Untracked int    `json:"untracked"`
	Head      string `json:"head"`
	At        string `json:"at"`
}

func workingChangesStat(ctx context.Context, cwd string) (changesStat, error) {
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.quotePath=true"}, args...)...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
		var out changeOutput
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil || out.overflow {
			return "", fmt.Errorf("working-folder Git review unavailable")
		}
		return out.String(), nil
	}
	head, err := run("rev-parse", "--verify", "HEAD")
	if err != nil {
		return changesStat{}, err
	}
	st := changesStat{Head: strings.TrimSpace(head), At: time.Now().UTC().Format(time.RFC3339)}
	num, err := run("diff", "--no-ext-diff", "--no-textconv", "--numstat", "-z", st.Head, "--")
	if err != nil {
		return changesStat{}, err
	}
	// -z: "added\tremoved\tpath\0", a rename "added\tremoved\t\0old\0new\0"
	fields := strings.Split(num, "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) < 3 {
			continue
		}
		if parts[2] == "" {
			i += 2 // a rename's old and new paths follow
		}
		st.Files++
		a, errA := strconv.Atoi(parts[0])
		d, errD := strconv.Atoi(parts[1])
		if errA != nil || errD != nil {
			st.Binary++ // "-\t-": a binary file has no line counts
			continue
		}
		st.Added += a
		st.Removed += d
	}
	untracked, err := run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return changesStat{}, err
	}
	if u := strings.TrimSuffix(untracked, "\x00"); u != "" {
		st.Untracked = len(strings.Split(u, "\x00"))
	}
	return st, nil
}
