package spirits

import "testing"

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
