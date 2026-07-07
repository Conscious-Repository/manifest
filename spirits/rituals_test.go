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
