package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Codex rollout identity is the CLI's session_meta record, never title, cwd
// alone, or newest-file order. Missing/ambiguous identity returns no transcript.
// Read-only discovery preserves the existing JSONL parser and offset contract.
func (c *termCfg) codexTranscriptPath(se termSession) string {
	if se.Device != "" || !resumeIDRe.MatchString(se.ResumeID) {
		return ""
	}
	root := c.codexSessions
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".codex", "sessions")
	}
	cwd := se.Cwd
	if cwd == "" {
		cwd = c.defaultWd
	}
	var found string
	ambiguous := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), "-"+se.ResumeID+".jsonl") {
			return nil
		}
		id, dir := codexRolloutIdentity(path)
		if id != se.ResumeID || filepath.Clean(dir) != filepath.Clean(cwd) {
			return nil
		}
		if found != "" {
			ambiguous = true
			return fs.SkipAll
		}
		found = path
		return nil
	})
	if err != nil || ambiguous {
		return ""
	}
	return found
}

func codexRolloutIdentity(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 2*1024*1024)
	if !scan.Scan() {
		return "", ""
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(scan.Bytes(), &meta) != nil || meta.Type != "session_meta" {
		return "", ""
	}
	if meta.Payload.SessionID != "" && meta.Payload.SessionID != meta.Payload.ID {
		return "", ""
	}
	if !resumeIDRe.MatchString(meta.Payload.ID) || !filepath.IsAbs(meta.Payload.Cwd) {
		return "", ""
	}
	return meta.Payload.ID, meta.Payload.Cwd
}

// codexProcessRollout reads only files actually held open by a foreground Codex
// process belonging to this pane. It never scans other processes or chooses by
// timestamp. Linux is the supported host; other hosts leave identity unresolved.
func (h *herdrTerminalRuntime) codexProcessRollout(ctx context.Context, se termSession) (string, error) {
	if se.Kind != "codex" || se.Device != "" {
		return "", nil
	}
	if err := h.checked(ctx, se.Runtime); err != nil {
		return "", err
	}
	r, err := h.callGeneration(ctx, "pane.process_info", map[string]any{"pane_id": se.Runtime.Pane}, se.Runtime.Generation)
	if err != nil {
		return "", err
	}
	root := h.server.terminal.codexSessions
	if root == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		root = filepath.Join(home, ".codex", "sessions")
	}
	cwd := se.Cwd
	if cwd == "" {
		cwd = h.server.terminal.defaultWd
	}
	found := ""
	for _, p := range r.ProcessInfo.Processes {
		if p.Name != "codex" || p.PID <= 0 {
			continue
		}
		entries, e := os.ReadDir(filepath.Join("/proc", strconv.Itoa(p.PID), "fd"))
		if e != nil {
			continue
		}
		for _, entry := range entries {
			fd := filepath.Join("/proc", strconv.Itoa(p.PID), "fd", entry.Name())
			path, e := os.Readlink(fd)
			if e != nil {
				continue
			}
			id := codexOpenRolloutIdentity(root, path, fd, cwd)
			if id == "" {
				continue
			}
			if found != "" && found != id {
				return "", errors.New("multiple open Codex conversations; identity unresolved")
			}
			found = id
		}
	}
	if err := h.checked(ctx, se.Runtime); err != nil {
		return "", err
	}
	return found, nil
}

func codexOpenRolloutIdentity(root, path, fd, cwd string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	if !strings.HasPrefix(filepath.Base(path), "rollout-") || !strings.HasSuffix(path, ".jsonl") {
		return ""
	}
	file, err := os.Stat(path)
	if err != nil {
		return ""
	}
	opened, err := os.Stat(fd)
	if err != nil || !os.SameFile(file, opened) {
		return ""
	}
	id, dir := codexRolloutIdentity(path)
	if filepath.Clean(dir) != filepath.Clean(cwd) || !strings.HasSuffix(path, "-"+id+".jsonl") {
		return ""
	}
	return id
}
