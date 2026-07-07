package spirits

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// castablesHarness builds a temp vault with an excalibur harness and one skill,
// returning the harness root the Store is meant to point at.
func castablesHarness(t *testing.T) string {
	t.Helper()
	vault := t.TempDir()
	harness := filepath.Join(vault, "excalibur")
	write := func(rel, content string) {
		p := filepath.Join(vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a vault skill
	write("skills/jungian-dream-analysis/SKILL.md", "---\nname: jungian-dream-analysis\ndescription: Analyze a dream.\n---\nbody")
	// an on-demand ritual (no cadence) and a scheduled one
	write("excalibur/chargebook.md", "---\ndefault_run_ceiling_usd: 0.50\n---\n")
	write("excalibur/spirits/domain-scout/rituals/targeted.md", "---\nritual: targeted\ncharge_usd: 1.00\n---\nbrief")
	write("excalibur/spirits/domain-scout/rituals/daily.md", "---\nritual: daily\ncadence: 0 7 * * *\n---\nscan")
	// sage/skill-cast is on-demand but must be excluded from the ritual list
	write("excalibur/spirits/sage/rituals/skill-cast.md", "---\nritual: skill-cast\n---\ncast")
	return harness
}

func TestCastables(t *testing.T) {
	s := NewStore(castablesHarness(t))
	got := s.Castables(time.Now())

	var skills, rituals int
	var sawTargeted, sawSkillCast, sawDaily bool
	for _, c := range got {
		switch c.Kind {
		case "skill":
			skills++
			if c.Skill != "skills/jungian-dream-analysis" || c.Spirit != "sage" || c.Ritual != "skill-cast" {
				t.Errorf("skill castable wrong: %+v", c)
			}
		case "ritual":
			rituals++
			if c.Spirit == "domain-scout" && c.Ritual == "targeted" {
				sawTargeted = true
			}
			if c.Ritual == "skill-cast" {
				sawSkillCast = true
			}
			if c.Ritual == "daily" {
				sawDaily = true
			}
		}
	}
	if skills != 1 {
		t.Errorf("skills=%d want 1", skills)
	}
	if !sawTargeted {
		t.Error("on-demand targeted ritual missing from castables")
	}
	if sawSkillCast {
		t.Error("sage/skill-cast must be excluded from the ritual list (cast a skill instead)")
	}
	if sawDaily {
		t.Error("scheduled ritual (daily) must not appear in castables")
	}
}

func TestValidSkillRef(t *testing.T) {
	ok := []string{"skills/jungian-dream-analysis", "skills/foo"}
	bad := []string{"", "skills", "skills/", "skills/a/b", "notskills/x", "skills/../x", "/skills/x"}
	for _, r := range ok {
		if !validSkillRef(r) {
			t.Errorf("validSkillRef(%q) = false, want true", r)
		}
	}
	for _, r := range bad {
		if validSkillRef(r) {
			t.Errorf("validSkillRef(%q) = true, want false", r)
		}
	}
}

func TestSpoolRunNowSkill(t *testing.T) {
	harness := castablesHarness(t)
	s := NewStore(harness)
	if err := s.SpoolRunNow("sage", "skill-cast", "I dreamt of a snake", "skills/jungian-dream-analysis"); err != nil {
		t.Fatalf("SpoolRunNow: %v", err)
	}
	// a bad skill ref is refused
	if err := s.SpoolRunNow("sage", "skill-cast", "x", "../etc/passwd"); err == nil {
		t.Fatal("SpoolRunNow accepted a bad skill ref")
	}
	dir := filepath.Join(harness, "vessel", "spool")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("spool has %d files, want 1", len(entries))
	}
	b, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !contains(string(b), `"skill":"skills/jungian-dream-analysis"`) {
		t.Fatalf("spooled request missing skill field: %s", b)
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (indexOf(hay, needle) >= 0)
}
func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
