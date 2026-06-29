package agents

import (
	"os"
	"path/filepath"
	"strings"
)

// AgentDef is a `type: agent` markdown definition: frontmatter (model, schedule,
// tools, permissions, handles) plus a prose brief. Adding an agent = adding a file.
type AgentDef struct {
	Name        string   `json:"name"`
	Model       string   `json:"model"`
	Schedule    string   `json:"schedule"`
	Tools       []string `json:"tools"`
	Permissions []string `json:"permissions"`
	Handles     []string `json:"handles"`
	Brief       string   `json:"brief"`
}

// ParseAgentDef reads an agent definition file. The list fields accept either
// `key: [a, b]` or `key: a, b` (a small hand-rolled YAML subset — no dependency).
func ParseAgentDef(path string) (AgentDef, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return AgentDef{}, err
	}
	d := AgentDef{Name: strings.TrimSuffix(filepath.Base(path), ".md")}
	fm, body := splitFrontmatter(string(content))
	d.Model = fm["model"]
	d.Schedule = fm["schedule"]
	d.Tools = parseList(fm["tools"])
	d.Permissions = parseList(fm["permissions"])
	d.Handles = parseList(fm["handles"])
	d.Brief = strings.TrimSpace(body)
	return d, nil
}

// LoadAgentDefs reads every non-queue *.md definition directly under dir.
func LoadAgentDefs(dir string) []AgentDef {
	entries, _ := os.ReadDir(dir)
	var defs []AgentDef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if d, err := ParseAgentDef(filepath.Join(dir, e.Name())); err == nil {
			defs = append(defs, d)
		}
	}
	return defs
}

func parseList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(strings.Trim(p, `"'`)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
