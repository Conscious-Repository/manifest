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
