package spirits

// The command bar's catalog: what the summoner can cast from `/`. Two sources —
// the vault skills he authors in Obsidian (skills/<name>/SKILL.md), each cast
// through the sage spirit's skill-cast ritual, and the on-demand rituals across
// spirits (those with no cadence). Skills are discovered from the vault so the
// vault stays the source of truth for how spirits think (plans/spirits-improvement.md §2).

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/mdfm"
)

// Castable is one entry in the command-bar catalog.
type Castable struct {
	Kind        string `json:"kind"`            // "skill" | "ritual"
	Label       string `json:"label"`           // display name
	Description string `json:"description"`     // one-liner (skills only)
	Spirit      string `json:"spirit"`          // spirit that runs it
	Ritual      string `json:"ritual"`          // ritual that runs it
	Skill       string `json:"skill,omitempty"` // "skills/<name>" for skill casts
}

// maxCastableDesc trims a skill's (often long) description for the palette.
const maxCastableDesc = 240

// Castables lists everything the command bar can launch: vault skills (cast via
// sage/skill-cast) first, then the on-demand rituals. sage/skill-cast itself is
// omitted from the ritual list — it is meant to carry a skill, not run bare.
func (s *Store) Castables(now time.Time) []Castable {
	out := []Castable{}
	for _, sk := range s.vaultSkills() {
		out = append(out, Castable{
			Kind: "skill", Label: sk.label, Description: sk.desc,
			Spirit: "sage", Ritual: "skill-cast", Skill: "skills/" + sk.dir,
		})
	}
	for _, r := range s.Rituals(now) {
		if r.Cadence != "" || !r.Valid {
			continue // scheduled or broken — not an on-demand cast
		}
		if r.Spirit == "sage" && r.Ritual == "skill-cast" {
			continue // cast a skill instead
		}
		out = append(out, Castable{
			Kind: "ritual", Label: r.Spirit + " · " + r.Ritual,
			Spirit: r.Spirit, Ritual: r.Ritual,
		})
	}
	return out
}

type vaultSkill struct{ dir, label, desc string }

// vaultSkills scans <vault>/skills/*/SKILL.md. The vault is the parent of the
// excalibur harness root (the harness lives at <vault>/excalibur).
func (s *Store) vaultSkills() []vaultSkill {
	dir := filepath.Join(filepath.Dir(s.root), "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []vaultSkill
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "SKILL.md"))
		if err != nil {
			continue // a skill is a directory with a SKILL.md; skip the rest
		}
		fm, _ := mdfm.Split(string(b))
		label := e.Name()
		if n := strings.TrimSpace(fm["name"]); n != "" {
			label = n
		}
		desc := strings.TrimSpace(fm["description"])
		if len(desc) > maxCastableDesc {
			desc = strings.TrimSpace(desc[:maxCastableDesc]) + "…"
		}
		out = append(out, vaultSkill{dir: e.Name(), label: label, desc: desc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].label < out[j].label })
	return out
}

// validSkillRef accepts only "skills/<name>" with a plain name segment — the
// same shape the engine's LoadSkill enforces (defense in depth at the spool).
func validSkillRef(ref string) bool {
	segs := strings.Split(ref, "/")
	return len(segs) == 2 && segs[0] == "skills" && validID(segs[1]) && segs[1] != ""
}
