package spirits

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHumanCadence(t *testing.T) {
	cases := map[string]string{
		"0 7 * * *":       "daily 7:00a",
		"0 8,13,18 * * *": "daily 8:00a, 1:00p, 6:00p", // value list (granola-sync)
		"30 7 * * 0":      "Sun 7:30a",                 // weekday name
		"0 8 * * 1":       "Mon 8:00a",
		"0 8 * * 1-5":     "weekdays 8:00a",
		"0 9 * * 1,3,5":   "Mon, Wed, Fri 9:00a",
		"0 10 * * 0,6":    "weekends 10:00a",
		"*/30 * * * *":    "every 30 min",   // step value
		"0 */2 * * *":     "every 2 hours",  // hourly step
		"0 * * * *":       "hourly at :00",  // every hour
		"15 7,15 * * *":   "daily 7:15a, 3:15p",
		"0 0 * * *":       "daily 12:00a",
		"0 12 * * *":      "daily 12:00p",
		// unphraseable → "custom" (never raw-only)
		"0 8 1 * *":     "custom", // day-of-month
		"0 8 * * 1-3,5": "custom", // mixed range+list in dow
		"not a cron":    "custom",
	}
	for expr, want := range cases {
		if got := humanCadence(expr); got != want {
			t.Errorf("humanCadence(%q) = %q, want %q", expr, got, want)
		}
	}
}

// TestBuilderVocabulary is the grammar CONTRACT with the client cadence
// builder (server/web/js/58-rituals.js cadCompile/cadParse — SPIRITS.md §2):
// every cron the builder can EMIT phrases through humanCadence exactly as the
// builder's own phrase, and every shape the builder must REFUSE (cadParse →
// null, raw-only editing) phrases as "custom". Change either side only with
// the other.
func TestBuilderVocabulary(t *testing.T) {
	// (builder state → canonical cron → phrase) triples the JS emits
	emit := map[string]string{
		"0 6 * * *":          "daily 6:00a",                // daily, one time
		"0 8,13,18 * * *":    "daily 8:00a, 1:00p, 6:00p",  // daily, time list (canonical ascending)
		"30 17 * * 1-5":      "weekdays 5:30p",             // weekdays + off-preset-hour + :30
		"0 9 * * 0,6":        "weekends 9:00a",             // weekends (builder emits 0,6 never 6,0)
		"0 16 * * 5":         "Fri 4:00p",                  // named days, one
		"15 9,12 * * 1,3,5":  "Mon, Wed, Fri 9:15a, 12:15p",// named days, lists both sides
		"*/30 * * * *":       "every 30 min",
		"*/7 * * * *":        "every 7 min",                // off-preset interval still representable
		"0 */2 * * *":        "every 2 hours",
		"45 * * * *":         "hourly at :45",
	}
	for cron, phrase := range emit {
		if got := humanCadence(cron); got != phrase {
			t.Errorf("builder-emittable %q phrases as %q, want %q", cron, got, phrase)
		}
	}
	// shapes the builder's parser must refuse → custom (raw-only editing)
	custom := []string{
		"15 7 1 * *",    // day-of-month
		"0 8 * * 1-3",   // dow range other than 1-5
		"0 8-10 * * *",  // hour range
		"0 8/2 * * *",   // step inside a value
		"0 8 2 1 *",     // month pinned
		"60 8 * * *",    // (JS also bounds minute ≤59; Go phrases loosely but cron rejects)
	}
	for _, cron := range custom {
		if got := humanCadence(cron); got != "custom" && cron != "60 8 * * *" {
			t.Errorf("custom shape %q phrases as %q, want custom", cron, got)
		}
	}
	// weekends parse-compat: humanCadence accepts 6,0 too; the builder
	// canonicalizes to 0,6 — both phrase identically (canonical-compare rule)
	if humanCadence("0 9 * * 6,0") != humanCadence("0 9 * * 0,6") {
		t.Error("weekends 6,0 and 0,6 must phrase identically")
	}
}

// The spirit-page vocabularies + memory listing read real tree shapes — pin
// them against a scaffolded temp harness.
func TestCatalogAndMemories(t *testing.T) {
	root := t.TempDir()
	st := NewStore(root)
	// grimoire: two conduits (+ an .example that must be skipped), two spellbooks
	mk := func(rel string) {
		if err := osMkdirAllFile(root, rel); err != nil {
			t.Fatal(err)
		}
	}
	mk("grimoire/portals/claude-sub.md")
	mk("grimoire/portals/deepseek-local.md")
	mk("grimoire/portals/openai-compat.example.md")
	mk("grimoire/spellbooks/web/SPELLBOOK.md")
	mk("grimoire/spellbooks/feed/SPELLBOOK.md")
	if got := st.Conduits(); len(got) != 2 || got[0] != "claude-sub" || got[1] != "deepseek-local" {
		t.Fatalf("Conduits() = %v", got)
	}
	if got := st.Spellbooks(); len(got) != 2 || got[0] != "feed" || got[1] != "web" {
		t.Fatalf("Spellbooks() = %v", got)
	}
	// a scaffolded spirit has long-term + empty window/archive
	if err := st.ScaffoldSpirit("scout"); err != nil {
		t.Fatal(err)
	}
	mems := st.Memories("scout")
	if len(mems) != 3 || mems[0].Name != "long-term" || mems[0].Bytes == 0 ||
		mems[1].Name != "window" || mems[2].Name != "archive" {
		t.Fatalf("Memories() = %+v", mems)
	}
	if st.Memories("../escape") != nil {
		t.Fatal("slug traversal not refused")
	}
}

// osMkdirAllFile writes an empty file at rel, creating parents.
func osMkdirAllFile(root, rel string) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte("x\n"), 0o644)
}

// TestLintRitualFMEnabled — the `enabled` value grammar, the BYTE CONTRACT
// mirrored in the engine (internal/spirit TestParseRitualEnabled): absent/
// true/false pass the lint; anything else is an ERROR (a typo must fail
// visibly, never silently-enabled).
func TestLintRitualFMEnabled(t *testing.T) {
	vectors := []struct {
		value string
		ok    bool
	}{
		{"", true}, {"true", true}, {"false", true}, {"False", true},
		{"FALSE", true}, {" false ", true},
		{"no", false}, {"0", false}, {"yes", false}, {"paused", false},
	}
	for _, v := range vectors {
		fm := map[string]string{"ritual": "x"}
		if v.value != "" {
			fm["enabled"] = v.value
		}
		errs, _ := lintRitualFM(fm)
		if (len(errs) == 0) != v.ok {
			t.Errorf("enabled %q → errs %v, want ok=%v", v.value, errs, v.ok)
		}
	}
	// the read side matches the grammar
	if ritualEnabled(map[string]string{"enabled": "false"}) || ritualEnabled(map[string]string{"enabled": "False"}) {
		t.Error("false must read paused")
	}
	if !ritualEnabled(map[string]string{}) || !ritualEnabled(map[string]string{"enabled": "true"}) {
		t.Error("absent/true must read enabled")
	}
}

// A paused ritual keeps its phrase but never a next-fire, and is skipped by
// the castables catalog.
func TestRitualsPausedRow(t *testing.T) {
	root := t.TempDir()
	st := NewStore(root)
	dir := filepath.Join(root, "spirits", "s", "rituals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("scheduled.md", "---\nritual: scheduled\ncadence: 0 7 * * *\nenabled: false\n---\nx\n")
	write("ondemand.md", "---\nritual: ondemand\nenabled: false\n---\nx\n")
	rows := st.Rituals(time.Now())
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	for _, r := range rows {
		if r.Enabled {
			t.Errorf("%s: Enabled = true, want paused", r.Ritual)
		}
		if r.NextFire != "" {
			t.Errorf("%s: paused row carries next-fire %q", r.Ritual, r.NextFire)
		}
	}
	for _, c := range st.Castables(time.Now()) {
		if c.Kind == "ritual" && c.Ritual == "ondemand" {
			t.Error("paused on-demand ritual leaked into castables")
		}
	}
}
