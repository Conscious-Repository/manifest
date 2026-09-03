package record_test

// The golden-corpus HEARTBEAT (kernel-extraction A0): every file in the
// owner's snapshotted corpus must parse→emit byte-identical through the
// current code, at every step of the extraction. The corpus lives OUTSIDE
// the repo (personal data): ~/.config/manifest/golden-corpus, refreshable by
// re-copying the live vault. The test skips when the corpus is absent (CI /
// other machines) — on the owner's machine it is the pass's pulse.

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/daily"
	"manifest/goals"
	"manifest/realestate"
	"manifest/recruiting"
	"manifest/tasks"
)

func corpusDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("MANIFEST_CORPUS"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	d := filepath.Join(home, ".config", "manifest", "golden-corpus")
	if _, err := os.Stat(d); err != nil {
		t.Skip("golden corpus not present — snapshot it from the live vault first")
	}
	return d
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestCorpusGoals(t *testing.T) {
	dir := corpusDir(t)
	raw := read(t, filepath.Join(dir, "goals.md"))
	if out := goals.Serialize(goals.Parse(raw)); out != raw {
		t.Fatalf("goals.md round-trip diverged:\n%s", firstDiff(raw, out))
	}
}

func TestCorpusGoalsArchives(t *testing.T) {
	dir := corpusDir(t)
	matches, _ := filepath.Glob(filepath.Join(dir, "goals *.md"))
	if len(matches) == 0 {
		t.Skip("no quarter archives in corpus")
	}
	for _, path := range matches {
		base := strings.TrimSuffix(filepath.Base(path), ".md")
		quarter := strings.TrimPrefix(base, "goals ")
		if strings.Contains(quarter, "review") {
			continue // retro files are write-only prose, no round-trip contract
		}
		raw := read(t, path)
		if out := goals.ReserializeArchive(quarter, raw); out != raw {
			t.Fatalf("%s round-trip diverged:\n%s", base, firstDiff(raw, out))
		}
	}
}

func TestCorpusTasks(t *testing.T) {
	dir := corpusDir(t)
	raw := read(t, filepath.Join(dir, "to do.md"))
	if out := tasks.Serialize(tasks.Parse(raw)); out != raw {
		t.Fatalf("to do.md round-trip diverged:\n%s", firstDiff(raw, out))
	}
}

func TestCorpusDaily(t *testing.T) {
	dir := corpusDir(t)
	notes, _ := filepath.Glob(filepath.Join(dir, "daily", "*.md"))
	if len(notes) == 0 {
		t.Skip("no daily notes in corpus")
	}
	for _, path := range notes {
		raw := read(t, path)
		if out := daily.RoundTripNote(raw); out != raw {
			t.Fatalf("%s manifest region diverged:\n%s", filepath.Base(path), firstDiff(raw, out))
		}
	}
}

func TestCorpusRealestate(t *testing.T) {
	dir := corpusDir(t)
	var mds, csvs, jsons int
	err := filepath.Walk(filepath.Join(dir, "realestate"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch {
		case strings.HasSuffix(path, ".md"):
			mds++
			raw := read(t, path)
			// assumptions.md: whole-file parse→emit fixpoint (RE spec §3)
			if strings.HasSuffix(path, string(filepath.Separator)+realestate.AssumptionsFile) {
				if got := realestate.EmitAssumptions(realestate.ParseAssumptions(raw)); got != raw {
					t.Fatalf("assumptions.md diverged:\n--- have\n%s\n--- emit\n%s", raw, got)
				}
			}
			// the fixpoint contract on records is per-SECTION (## rocks, née
			// ## work) — the store writes surgically, never whole-file
			for _, name := range []string{"work", "rocks"} {
				if sec, ok := section(raw, name); ok {
					stages := realestate.ParseWork(sec)
					if got := strings.TrimRight(realestate.EmitWork(stages), "\n"); got != strings.TrimRight(strings.Join(sec, "\n"), "\n") {
						t.Fatalf("%s ## %s diverged:\n--- have\n%s\n--- emit\n%s", filepath.Base(path), name, strings.Join(sec, "\n"), got)
					}
				}
			}
			// contract records: whole-file round-trip (they are written
			// canonically by NewContractRecord — overhaul §3.2)
			if strings.Contains(path, string(filepath.Separator)+"contracts"+string(filepath.Separator)) {
				slug := strings.TrimSuffix(filepath.Base(path), ".md")
				c := realestate.ParseContract("", slug, raw)
				if got := realestate.NewContractRecord(c); got != raw {
					t.Fatalf("%s contract round-trip diverged:\n--- have\n%s\n--- emit\n%s", filepath.Base(path), raw, got)
				}
			}
		case strings.HasSuffix(path, ".ledger.csv"):
			csvs++
			realestate.ParseLedgerBytes([]byte(read(t, path))) // must not panic; row fidelity covered by writer contract tests
		case strings.HasSuffix(path, ".json"):
			jsons++
			if !json.Valid([]byte(read(t, path))) {
				t.Fatalf("%s: invalid json", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if mds == 0 || csvs == 0 || jsons == 0 {
		t.Fatalf("corpus looks incomplete: %d md, %d csv, %d json", mds, csvs, jsons)
	}
	t.Logf("realestate corpus: %d md · %d csv · %d json", mds, csvs, jsons)
}

// TestCorpusAion registers the seven aion corpora with the heartbeat: each
// file under system/aion/ must parse→emit byte-identical through the
// domain's declared round-trip (aion.Corpora). Skips per-file when the
// corpus snapshot predates the domain.
func TestCorpusAion(t *testing.T) {
	dir := corpusDir(t)
	found := 0
	for _, name := range aion.Files {
		path := filepath.Join(dir, "system", "aion", name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		found++
		raw := read(t, path)
		if out := aion.Corpora[name](raw); out != raw {
			t.Fatalf("system/aion/%s round-trip diverged:\n%s", name, firstDiff(raw, out))
		}
	}
	if found == 0 {
		t.Skip("no system/aion files in corpus yet — refresh the snapshot after seeding")
	}
	t.Logf("aion corpus: %d/%d files", found, len(aion.Files))
}

// TestCorpusRecruiting registers the PRIVATE recruiting records with the
// heartbeat, in the shape of TestCorpusAion. Unlike aion's seven fixed
// corpora, roles and candidates are one file per record, so the walk routes
// each file through recruiting.RoundTrip's shape registry. Skips when the
// corpus snapshot predates the domain.
func TestCorpusRecruiting(t *testing.T) {
	dir := corpusDir(t)
	root := filepath.Join(dir, "system", "aion", "recruiting")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no system/aion/recruiting files in corpus yet — refresh the snapshot after seeding")
	}
	found, skipped := 0, 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fn := recruiting.RoundTrip(filepath.ToSlash(rel))
		if fn == nil {
			skipped++
			return nil // a file shape the domain does not declare (yet)
		}
		found++
		raw := read(t, p)
		if out := fn(raw); out != raw {
			t.Fatalf("system/aion/recruiting/%s round-trip diverged:\n%s", rel, firstDiff(raw, out))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Skip("no recognized recruiting records in corpus yet")
	}
	t.Logf("recruiting corpus: %d files (%d unrecognized shapes)", found, skipped)
}

// section extracts the body lines of `## <name>` (until the next ## or EOF).
func section(raw, name string) ([]string, bool) {
	lines := strings.Split(raw, "\n")
	start := -1
	for i, ln := range lines {
		if strings.EqualFold(strings.TrimRight(ln, " \t"), "## "+name) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, false
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], " \t")
		if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
			end = i
			break
		}
	}
	// trim trailing blank lines (section separators, not content)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end], true
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return "line " + itoa(i+1) + ":\n- " + al[i] + "\n+ " + bl[i]
		}
	}
	return "length differs: " + itoa(len(al)) + " vs " + itoa(len(bl)) + " lines"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
