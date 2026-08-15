package todos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

const sample = `# To Do

## Inbox
- [ ] undomained thing

## Aion
- [ ] bio · reach out to [[Ulrike Granögger]] [added:: 2026-07-26]
- [ ] get Ameren to move the power line [waiting:: Ameren] [since:: 2026-07-20] [added:: 2026-07-10]
- [ ] side letter [todo:: aion/yoshiro-side-letter] [waiting:: [[Yoshiro]]] [since:: 2026-07-01] [added:: 2026-07-01]

## Real Estate
- [x] call someone to hang banners [added:: 2026-07-01] [done:: 2026-07-20]
- [ ] gutters 761 [added:: 2026-07-26]
[[x posts]]
`

func TestFixpoint(t *testing.T) {
	d := Parse(sample)
	out := Serialize(d)
	if out2 := Serialize(Parse(out)); out2 != out {
		t.Fatalf("not a fixpoint:\n--- first\n%s\n--- second\n%s", out, out2)
	}
	// the sample is already canonical — round-trip must be byte-identical
	if out != sample {
		t.Fatalf("canonical sample changed:\n%s", out)
	}
}

func TestParseStates(t *testing.T) {
	d := Parse(sample)
	dom := d.Domain("Aion")
	if dom == nil || len(dom.Todos) != 3 {
		t.Fatalf("aion domain: %+v", dom)
	}
	if st := dom.Todos[0].State(); st != "open" {
		t.Fatalf("open state = %s", st)
	}
	amere := dom.Todos[1]
	if amere.State() != "waiting" || amere.Waiting != "Ameren" || amere.AgeDays(now) != 6 {
		t.Fatalf("waiting free text: %+v age %d", amere, amere.AgeDays(now))
	}
	yos := dom.Todos[2]
	if yos.ID != "aion/yoshiro-side-letter" || yos.WaitingPerson() != "Yoshiro" {
		t.Fatalf("wikilink waiting: id=%s person=%s", yos.ID, yos.WaitingPerson())
	}
	if open := dom.Todos[0]; open.AgeDays(now) != 0 {
		t.Fatalf("added-today age = %d", open.AgeDays(now))
	}
	// derived ids
	if d.Domains[0].Todos[0].ID != "inbox/undomained-thing" {
		t.Fatalf("derived id = %s", d.Domains[0].Todos[0].ID)
	}
	// extra line preserved
	re := d.Domain("Real Estate")
	if len(re.extra) != 1 || re.extra[0] != "[[x posts]]" {
		t.Fatalf("extra lines = %v", re.extra)
	}
}

func TestPromoteAndSync(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, "to do.md", testWrite)
	if err := os.WriteFile(s.Path(), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Load()
	if !d.Promote("real-estate/gutters-761") {
		t.Fatal("promote missed")
	}
	_ = s.Save(d)
	raw, _ := os.ReadFile(s.Path())
	if !strings.Contains(string(raw), "[todo:: real-estate/gutters-761]") {
		t.Fatalf("pin not written:\n%s", raw)
	}
	missed, err := s.SyncChecks(map[string]bool{
		"real-estate/gutters-761": true,
		"aion/reworded-away":      true,
	}, now)
	if err != nil || len(missed) != 1 || missed[0] != "aion/reworded-away" {
		t.Fatalf("sync: missed=%v err=%v", missed, err)
	}
	d2, _ := s.Load()
	_, g := d2.Find("real-estate/gutters-761")
	if g == nil || !g.Checked || g.Done != "2026-07-26" {
		t.Fatalf("synced todo: %+v", g)
	}
}

// TestPinSurvivesRewording — plan D1: once pinned, editing the text must NOT
// change the id (panel artifacts key on it), and the pin round-trips.
func TestPinSurvivesRewording(t *testing.T) {
	d := Parse(sample)
	if !d.Promote("inbox/undomained-thing") {
		t.Fatal("promote missed")
	}
	out := Serialize(d)
	if !strings.Contains(out, "undomained thing [todo:: inbox/undomained-thing]") {
		t.Fatalf("pin not serialized:\n%s", out)
	}
	// reword the text — the derived slug would change, the pin must not
	d2 := Parse(out)
	_, tt := d2.Find("inbox/undomained-thing")
	if tt == nil {
		t.Fatal("pinned id lost after round-trip")
	}
	tt.Text = "completely different wording"
	d3 := Parse(Serialize(d2))
	_, tt3 := d3.Find("inbox/undomained-thing")
	if tt3 == nil || tt3.Text != "completely different wording" {
		t.Fatalf("pinned id must survive rewording; got %+v", tt3)
	}
	if tt3.ExplicitID() != "inbox/undomained-thing" {
		t.Fatalf("explicit id = %q", tt3.ExplicitID())
	}
	// pinning is idempotent — a second Promote adds nothing
	before := Serialize(d3)
	d3.Promote("inbox/undomained-thing")
	if Serialize(d3) != before {
		t.Fatal("second promote changed the file")
	}
	// fixpoint holds with the pin present
	if again := Serialize(Parse(before)); again != before {
		t.Fatalf("pin broke the fixpoint:\n%s\nvs\n%s", before, again)
	}
}

func TestSweepAndDrop(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, "to do.md", testWrite)
	if err := os.WriteFile(s.Path(), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := s.Sweep(now) // banners done 2026-07-20, >48h old
	if err != nil || n != 1 {
		t.Fatalf("sweep n=%d err=%v", n, err)
	}
	live, _ := os.ReadFile(s.Path())
	if strings.Contains(string(live), "hang banners") {
		t.Fatal("swept item still live")
	}
	arch, _ := os.ReadFile(filepath.Join(dir, "to do archive.md"))
	if !strings.Contains(string(arch), "## 2026-07") || !strings.Contains(string(arch), "hang banners") ||
		!strings.Contains(string(arch), "[domain:: Real Estate]") {
		t.Fatalf("archive:\n%s", arch)
	}
	// drop with stamp
	if err := s.Drop("real-estate/gutters-761", now); err != nil {
		t.Fatal(err)
	}
	arch, _ = os.ReadFile(filepath.Join(dir, "to do archive.md"))
	if !strings.Contains(string(arch), "gutters 761") || !strings.Contains(string(arch), "[dropped:: 2026-07-26]") {
		t.Fatalf("dropped archive:\n%s", arch)
	}
	live, _ = os.ReadFile(s.Path())
	if strings.Contains(string(live), "gutters 761") {
		t.Fatal("dropped item still live")
	}
}

const sampleV2 = `# To Do

## Real Estate
- [ ] hang banners in fountain park [added:: 2026-07-26]
- [ ] order windows [rock:: real-estate/new-rock] [added:: 2026-07-26]

### 4848 & 4852 · [[4848 fountain ave]] [[4852 fountain ave]]
- [ ] get variance packet to attorney [issue:: real-estate/zoning-variance-for-4848-4852] [added:: 2026-07-26]

### 743 · [[743 n euclid]]
- [ ] schedule structural engineer walk [added:: 2026-07-26]

### issues
- [ ] zoning variance for 4848 & 4852 [issue:: real-estate/zoning-variance-for-4848-4852]

### backlog
- corner lot mural idea
- talk to city about alley vacation
`

func TestV2Fixpoint(t *testing.T) {
	out := Serialize(Parse(sampleV2))
	if out != sampleV2 {
		t.Fatalf("canonical v2 sample changed:\n%s", out)
	}
	if Serialize(Parse(out)) != out {
		t.Fatal("v2 not a fixpoint")
	}
}

func TestV2Structure(t *testing.T) {
	d := Parse(sampleV2)
	re := d.Domain("Real Estate")
	if len(re.Todos) != 2 || len(re.Buckets) != 2 || len(re.Issues) != 1 || len(re.Backlog) != 2 {
		t.Fatalf("structure: %d loose %d buckets %d issues %d backlog",
			len(re.Todos), len(re.Buckets), len(re.Issues), len(re.Backlog))
	}
	if re.Todos[1].Rock != "real-estate/new-rock" {
		t.Fatalf("rock tether: %+v", re.Todos[1])
	}
	b := re.Buckets[0]
	if b.Name != "4848 & 4852" || b.Slug != "4848-4852" || len(b.Links) != 2 || b.Links[0] != "4848 fountain ave" {
		t.Fatalf("bucket heading: %+v", b)
	}
	if b.Todos[0].Issue != "real-estate/zoning-variance-for-4848-4852" {
		t.Fatalf("issue tether: %+v", b.Todos[0])
	}
	is := re.Issues[0]
	if is.ID != "real-estate/zoning-variance-for-4848-4852" || is.Checked {
		t.Fatalf("issue: %+v", is)
	}
	v := d.View(now)
	if v.Domains[0].Issues[0].OpenTasks != 1 {
		t.Fatalf("issue open-task count: %+v", v.Domains[0].Issues[0])
	}
	// bucket todo findable by id
	if _, ft := d.Find(b.Todos[0].ID); ft == nil {
		t.Fatal("bucket todo not findable")
	}
	// auto-assigned issue id pins on normalize
	d2 := Parse("# To Do\n\n## Aion\n\n### issues\n- [ ] decide MRI vendor\n")
	out2 := Serialize(d2)
	if !strings.Contains(out2, "[issue:: aion/decide-mri-vendor]") {
		t.Fatalf("issue id not auto-pinned:\n%s", out2)
	}
	if Serialize(Parse(out2)) != out2 {
		t.Fatal("issue pin not a fixpoint")
	}
}

const legacy = `- Pay federal taxes (2021 & 2023) - ~19k?

- bio
	- reach out to [[Ulrike Granögger]]
	- GCSF clinical trial design?

- real estate
	- call someone to hang banners
	- gutters 761

- blogs:
	- troubleshooting in bio
	- architect matrix

[[x posts]]
`

func TestMigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, "to do.md", testWrite)
	if err := os.WriteFile(s.Path(), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	areas := []string{"Aion", "Real Estate", "Home", "Personal"}
	did, err := s.Migrate(now, areas)
	if err != nil || !did {
		t.Fatalf("migrate did=%v err=%v", did, err)
	}
	if _, err := os.Stat(s.Path() + ".pre-migration"); err != nil {
		t.Fatal("no backup written")
	}
	d, _ := s.Load()
	if d.Domain("Aion") == nil || len(d.Domain("Aion").Todos) != 2 {
		t.Fatalf("aion todos: %+v", d.Domain("Aion"))
	}
	if got := d.Domain("Aion").Todos[0].Text; got != "bio · reach out to [[Ulrike Granögger]]" {
		t.Fatalf("bio prefix: %q", got)
	}
	per := d.Domain("Personal")
	if len(per.Todos) != 3 { // taxes + 2 blogs
		t.Fatalf("personal todos: %d", len(per.Todos))
	}
	if !strings.HasPrefix(per.Todos[1].Text, "blog · ") {
		t.Fatalf("blog prefix: %q", per.Todos[1].Text)
	}
	if len(d.Domain("Real Estate").Todos) != 2 {
		t.Fatal("real estate todos")
	}
	raw, _ := os.ReadFile(s.Path())
	if !strings.Contains(string(raw), "[[x posts]]") {
		t.Fatal("tail lost")
	}
	// every migrated item stamped with today's added date
	for _, dom := range d.Domains {
		for _, td := range dom.Todos {
			if td.Added != "2026-07-26" {
				t.Fatalf("missing added stamp: %+v", td)
			}
		}
	}
	// idempotent
	did2, _ := s.Migrate(now, areas)
	if did2 {
		t.Fatal("second migrate ran")
	}
	// migrated file is a fixpoint
	out := Serialize(d)
	if Serialize(Parse(out)) != out {
		t.Fatal("migrated file not a fixpoint")
	}
}

// testWrite is the tests' plain write path (prod injects a vaultwriter capability).
func testWrite(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }

// Dropping a BUCKET todo must remove the live line (Find sees buckets; the
// removal must too) — the 3×-archived-never-removed regression.
func TestDropBucketTodo(t *testing.T) {
	dir := t.TempDir()
	raw := "# To Do\n\n## Real Estate\n- [ ] loose one [added:: 2026-07-01]\n\n### Tools · [bucket:: tools]\n- [ ] formalize heuristics tool [added:: 2026-07-30]\n"
	if err := os.WriteFile(filepath.Join(dir, "to do.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	st := NewStore(dir, "to do.md", func(p string, b []byte) error { return os.WriteFile(p, b, 0o644) })
	if err := st.Drop("real-estate/formalize-heuristics-tool", now); err != nil {
		t.Fatalf("drop: %v", err)
	}
	live, _ := os.ReadFile(filepath.Join(dir, "to do.md"))
	if strings.Contains(string(live), "formalize heuristics tool") {
		t.Fatalf("bucket todo survived the drop:\n%s", live)
	}
	arch, _ := os.ReadFile(filepath.Join(dir, "to do archive.md"))
	if !strings.Contains(string(arch), "[dropped:: ") || !strings.Contains(string(arch), "[domain:: Real Estate / Tools]") {
		t.Fatalf("archive record wrong:\n%s", arch)
	}
	// loose drops still work
	if err := st.Drop("real-estate/loose-one", now); err != nil {
		t.Fatalf("loose drop: %v", err)
	}
	live, _ = os.ReadFile(filepath.Join(dir, "to do.md"))
	if strings.Contains(string(live), "loose one") {
		t.Fatalf("loose todo survived the drop:\n%s", live)
	}
}
