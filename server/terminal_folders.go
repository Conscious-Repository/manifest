package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GET /api/terminal/folders — where a new coding session can start, for the
// new-chat folder chip: the home folder, the git repositories directly under
// ~/src (the owner's known repos), and the folders local sessions recently
// ran in, newest first. Paths only; nothing is read inside them.
func (s *Server) handleTermFolders(w http.ResponseWriter, _ *http.Request) {
	if s.terminal == nil {
		writeJSON(w, map[string]any{"enabled": false, "home": "", "repos": []string{}, "recent": []string{}})
		return
	}
	home := s.terminal.defaultWd
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	repos := []string{}
	if h, err := os.UserHomeDir(); err == nil {
		root := filepath.Join(h, "src")
		if entries, err := os.ReadDir(root); err == nil {
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				dir := filepath.Join(root, e.Name())
				if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
					repos = append(repos, dir)
				}
			}
		}
	}
	sort.Strings(repos)
	list := s.terminal.load()
	sort.SliceStable(list, func(i, j int) bool { return list[i].LastUsed > list[j].LastUsed })
	recent, seen := []string{}, map[string]bool{}
	for _, se := range list {
		cwd := strings.TrimRight(se.Cwd, "/")
		if se.Device != "" || cwd == "" || seen[cwd] || (se.Kind != "claude" && se.Kind != "codex") {
			continue
		}
		seen[cwd] = true
		recent = append(recent, cwd)
		if len(recent) == 8 {
			break
		}
	}
	writeJSON(w, map[string]any{"enabled": true, "home": home, "repos": repos, "recent": recent})
}
