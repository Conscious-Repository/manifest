package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"manifest/mdfm"
	"manifest/record"
)

// Skill context is a read of the skill directories an adapter's runtime reads,
// taken at request time. It is not a record of what a turn loaded: Hermes and
// Claude Code load a skill only when a turn names it, and no adapter here
// reports that back. A root that cannot be read stays visible as unavailable
// rather than as an empty inventory. Nothing is written or cached, and the
// roots are derived from the session record and this process's environment,
// never from a client-supplied path.
type chatSkillRoot struct {
	Label     string `json:"label"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
	Count     int    `json:"count"`
}

type chatSkill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Root        string `json:"root"`
	Path        string `json:"path"`
}

type chatSkillInventory struct {
	Adapter   string          `json:"adapter"`
	Source    string          `json:"source"` // on-disk | not-reported
	Note      string          `json:"note"`
	Roots     []chatSkillRoot `json:"roots"`
	Skills    []chatSkill     `json:"skills"`
	Limit     int             `json:"limit"`
	Truncated bool            `json:"truncated,omitempty"`
}

const (
	chatSkillLimit     = 500
	chatSkillDepth     = 5
	chatSkillFileLimit = 64 * 1024
)

// chatSkillRoots names the directories a runtime reads skills from. Paths are
// derived from the session record and this process's environment only.
func (s *Server) chatSkillRoots(adapter, profile, cwd string) ([]chatSkillRoot, string) {
	home, _ := os.UserHomeDir()
	switch adapter {
	case adapterHermesOneshot:
		root := filepath.Join(s.hermesHome(), "skills")
		label := "Hermes default profile skills"
		if profile != "" {
			root = filepath.Join(s.hermesHome(), "profiles", profile, "skills")
			label = "Hermes profile " + profile + " skills"
		}
		return []chatSkillRoot{{Label: label, Path: root}}, "Skills present on disk for this Hermes profile at read time. A turn loads a skill only when it names it (skill_view); the runner does not report which skills a turn used."
	case adapterHerdrClaude:
		config := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
		if config == "" {
			config = filepath.Join(home, ".claude")
		}
		roots := []chatSkillRoot{{Label: "Claude Code user skills", Path: filepath.Join(config, "skills")}}
		if cwd != "" && filepath.IsAbs(cwd) {
			roots = append(roots, chatSkillRoot{Label: "Project skills in the working folder", Path: filepath.Join(cwd, ".claude", "skills")})
		}
		return roots, "Skills present on disk in the Claude Code user and project skill folders at read time. Plugin and marketplace skills are not listed; the runtime does not report which skills a turn loaded."
	case adapterHerdrCodex:
		config := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if config == "" {
			config = filepath.Join(home, ".codex")
		}
		return []chatSkillRoot{{Label: "Codex skills", Path: filepath.Join(config, "skills")}}, "Skills present on disk in the Codex skills folder at read time. The runtime does not report which skills a turn loaded."
	}
	return nil, "This adapter does not expose a skill inventory."
}

// chatSkillInventoryFor reads every root now. Symbolic links are not followed,
// each SKILL.md is read up to 64 KiB, and the whole listing is bounded.
func (s *Server) chatSkillInventoryFor(adapter, profile, cwd string) chatSkillInventory {
	roots, note := s.chatSkillRoots(adapter, profile, cwd)
	inv := chatSkillInventory{Adapter: adapter, Source: "not-reported", Note: note, Roots: []chatSkillRoot{}, Skills: []chatSkill{}, Limit: chatSkillLimit}
	if roots == nil {
		return inv
	}
	inv.Source = "on-disk"
	for _, root := range roots {
		skills, err := scanSkillRoot(root.Path, chatSkillLimit-len(inv.Skills))
		if err != nil {
			root.Error = skillRootError(err)
		} else {
			root.Available = true
			root.Count = len(skills)
			for _, sk := range skills {
				sk.Root = root.Label
				inv.Skills = append(inv.Skills, sk)
			}
		}
		inv.Roots = append(inv.Roots, root)
	}
	if len(inv.Skills) >= chatSkillLimit {
		inv.Truncated = true
	}
	sort.SliceStable(inv.Skills, func(i, j int) bool {
		a, b := strings.ToLower(inv.Skills[i].Name), strings.ToLower(inv.Skills[j].Name)
		if a == b {
			return inv.Skills[i].Path < inv.Skills[j].Path
		}
		return a < b
	})
	return inv
}

func skillRootError(err error) string {
	switch {
	case os.IsNotExist(err):
		return "folder does not exist"
	case os.IsPermission(err):
		return "folder is not readable"
	}
	return "folder could not be read"
}

// scanSkillRoot lists <root>/**/SKILL.md up to a bounded depth. The skill name
// is the frontmatter name when present, else the containing folder; both are
// reported with the relative path so equal names stay distinguishable.
func scanSkillRoot(root string, limit int) ([]chatSkill, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, os.ErrNotExist
	}
	out := []chatSkill{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // an unreadable subfolder does not hide its siblings
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= chatSkillDepth {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "SKILL.md" || rel == "SKILL.md" || len(out) >= limit || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		name, desc := skillFrontmatter(path)
		if name == "" {
			name = filepath.Base(filepath.Dir(rel))
		}
		out = append(out, chatSkill{Name: name, Description: desc, Path: filepath.ToSlash(rel)})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

func skillFrontmatter(path string) (name, description string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	buf := make([]byte, chatSkillFileLimit)
	n, _ := f.Read(buf)
	b := buf[:n]
	if !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		return "", ""
	}
	fm, _ := mdfm.Split(string(b))
	name = strings.TrimSpace(record.Unquote(fm["name"]))
	description = strings.TrimSpace(record.Unquote(fm["description"]))
	if len(description) > 200 {
		description = description[:200] + "…"
	}
	return name, description
}

// GET /api/agents/chat/{agent}/sessions/{id}/skills — the skill roots of the
// Hermes profile this private conversation is addressed to. Portal agents
// have no private session record here and answer 404.
func (s *Server) handleAgentChatSkills(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	agent, id := r.PathValue("agent"), r.PathValue("id")
	sess, _, _, ok := s.agentChat.store.Get(agent, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	profile := sess.Profile
	if agent == "alfred" {
		profile = ""
	} else if profile == "" {
		profile = agent
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, s.chatSkillInventoryFor(adapterHermesOneshot, profile, ""))
}

// GET /api/terminal/session/{id}/skills — the skill roots of a coding
// runtime row, resolved from the registry's kind and working folder.
func (s *Server) handleTermSkills(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	caps := terminalChatCapabilities(se)
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, s.chatSkillInventoryFor(caps.Adapter, "", se.Cwd))
}
