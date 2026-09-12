package goals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sharedLocator string

func (l sharedLocator) GoalsPath() string { return string(l) }
func TestSharedHomeAcrossPlanners(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "home.md")
	write := func(p string, b []byte) error { return os.WriteFile(p, b, 0600) }
	write(shared, []byte("## Home\n\n### Rocks (90-day)\n- [ ] Paint [goal:: home/paint]\n"))
	create := func(name string) *Store {
		p := filepath.Join(root, name)
		write(p, []byte("# Goals\n\n## "+name+"\n> Private\n"))
		s := NewStore(sharedLocator(p), root, name, write)
		s.UseSharedHome(shared, write)
		return s
	}
	a, b := create("Work"), create("Personal")
	d := a.Load()
	_, g := d.FindGoal("home/paint")
	if g == nil {
		t.Fatal("shared goal missing")
	}
	g.Text = "Paint kitchen"
	if err := a.Save(d); err != nil {
		t.Fatal(err)
	}
	other := b.Load()
	_, g = other.FindGoal("home/paint")
	if g == nil || g.Text != "Paint kitchen" {
		t.Fatal("change did not propagate")
	}
	if other.FindArea("Work") != nil {
		t.Fatal("private area leaked")
	}
	raw, _ := os.ReadFile(a.Path())
	if strings.Contains(string(raw), "Home") {
		t.Fatal("shared area copied into private source")
	}
	if !other.View().Areas[len(other.Areas)-1].Shared {
		t.Fatal("shared marker missing")
	}
}
