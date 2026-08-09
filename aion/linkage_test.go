package aion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAbs(abs string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0o644)
}

func writeTemp(t *testing.T, root, rel, content string) error {
	t.Helper()
	return writeAbs(filepath.Join(root, filepath.FromSlash(rel)), []byte(content))
}

const linkageFixture = `# AION — backlog

## Tasks
- [ ] Ultrasound bath [kind:: task] [rock:: ultrasound-read] [owner:: MO] [captured:: 2026-07-27] [status:: open]
- [ ] Prototype scan [kind:: task] [rock:: aion/mouse-to-pig] [owner:: YS] [captured:: 2026-07-20] [status:: open]
- [ ] Deck [kind:: task] [rock:: fundraising] [owner:: JR] [captured:: 2026-07-31] [status:: open]
- [ ] Kyrylo item [kind:: task] [owner:: KY] [captured:: 2026-04-20] [status:: open]

## Decisions
- Outsource pig work [kind:: decision] [status:: decided] [outcome:: CRO] [owner:: BA/HZ] [source:: [[derya]]] [captured:: 2026-07-27]
- Pursue thymus [kind:: decision] [rock:: thymus program / FDA] [status:: decided] [decided:: 2026-07-28] [outcome:: yes] [owner:: BA] [captured:: 2026-07-28]
`

func TestBackfillLinkage(t *testing.T) {
	dir := t.TempDir()
	if err := writeTemp(t, dir, "system/aion/backlog.md", linkageFixture); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir, "system/aion", func(abs string, data []byte) error {
		return writeAbs(abs, data)
	})
	rockMap := map[string]string{
		"ultrasound-read":      "",                        // → null
		"thymus program / FDA": "aion/cell-state-control", // compound → id
		// "fundraising" intentionally ABSENT — resolves via alias, keep raw
	}
	ownerMap := map[string]string{"YS": "Y", "MO": "MM", "KY": ""}

	// dry run writes nothing
	rep, err := s.BackfillLinkage(rockMap, ownerMap, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RocksNulled != 1 || rep.RocksRewritten != 1 || rep.DecidedFilled != 1 || rep.OwnersFixed != 3 {
		t.Fatalf("dry report: %+v", rep)
	}
	if s.RawFile("backlog.md") != linkageFixture {
		t.Fatal("dry run wrote to disk")
	}
	// apply
	if _, err := s.BackfillLinkage(rockMap, ownerMap, false); err != nil {
		t.Fatal(err)
	}
	out := s.RawFile("backlog.md")

	checks := map[string]bool{
		"[rock:: aion/cell-state-control]": true,  // compound rewritten
		"[rock:: aion/mouse-to-pig]":       true,  // canonical untouched
		"[rock:: fundraising]":             true,  // alias-resolved, kept raw
		"[rock:: ultrasound-read]":         false, // nulled
		"[rock:: thymus program / FDA]":    false, // rewritten away
		"[owner:: MM]":                     true,  // MO→MM
		"[owner:: Y]":                      true,  // YS→Y
		"[decided:: 2026-07-27]":           true,  // decided filled from captured
	}
	for sub, want := range checks {
		if strings.Contains(out, sub) != want {
			t.Errorf("contains(%q)=%v, want %v\n%s", sub, !want, want, out)
		}
	}
	// the KY item lost its owner entirely (dropped, no empty [owner::])
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "Kyrylo item") && strings.Contains(ln, "owner") {
			t.Errorf("KY owner not dropped: %s", ln)
		}
	}
	// still a fixpoint
	if SerializeBacklog(ParseBacklog(out)) != out {
		t.Fatal("post-backfill not a fixpoint")
	}
}

func TestRemapOwner(t *testing.T) {
	m := map[string]string{"YS": "Y", "MO": "MM", "KY": ""}
	cases := []struct {
		in, want string
		changed  bool
	}{
		{"YS", "Y", true},
		{"BA/YS", "BA/Y", true},
		{"MO", "MM", true},
		{"KY", "", true},
		{"BA/KY", "BA", true},
		{"BA/HZ", "BA/HZ", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, changed := remapOwner(c.in, m)
		if got != c.want || changed != c.changed {
			t.Errorf("remapOwner(%q) = (%q,%v), want (%q,%v)", c.in, got, changed, c.want, c.changed)
		}
	}
}

func TestBatchLink(t *testing.T) {
	dir := t.TempDir()
	if err := writeTemp(t, dir, "system/aion/backlog.md", linkageFixture); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir, "system/aion", func(abs string, data []byte) error { return writeAbs(abs, data) })

	// find the decided decision missing a date + an open task, by title
	doc := s.LoadBacklog()
	var pigID, deckID string
	for _, it := range doc.Items() {
		if it.Text == "Outsource pig work" {
			pigID = it.ID
		}
		if it.Text == "Deck" {
			deckID = it.ID
		}
	}
	if pigID == "" || deckID == "" {
		t.Fatal("fixture items not found")
	}
	rock := "aion/mouse-to-pig"
	date := "2026-07-27"
	n, err := s.BatchLink([]LinkEdit{
		{ID: pigID, Rock: &rock, Decided: &date}, // decided decision — metadata edit allowed
		{ID: deckID, Rock: &rock},
		{ID: "nonexistent", Rock: &rock}, // skipped
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("changed %d, want 2", n)
	}
	out := s.RawFile("backlog.md")
	// the decided decision got a rock AND a date without losing its outcome/status
	pigLine := ""
	for _, ln := range splitLinesT(out) {
		if strings.Contains(ln, "Outsource pig work") {
			pigLine = ln
		}
	}
	for _, sub := range []string{"[rock:: aion/mouse-to-pig]", "[decided:: 2026-07-27]", "[outcome:: CRO]", "[status:: decided]"} {
		if !strings.Contains(pigLine, sub) {
			t.Fatalf("pig line missing %q:\n%s", sub, pigLine)
		}
	}
	if SerializeBacklog(ParseBacklog(out)) != out {
		t.Fatal("post-batchlink not a fixpoint")
	}
}

func splitLinesT(s string) []string { return strings.Split(s, "\n") }
