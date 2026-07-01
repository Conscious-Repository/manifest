package goals

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// legacy90Re matches an old "### 90-day" heading (but not "### Rocks (90-day)",
// which starts with "Rocks").
var legacy90Re = regexp.MustCompile(`(?m)^###[ \t]+90`)

// needsMigration reports whether raw goals.md bytes are the pre-v2 format: an old
// "### 90-day" heading or any retired `due::` field. Already-migrated files (which
// use "### Rocks (90-day)" / "### 1-year" and carry no due::) return false.
func needsMigration(raw string) bool {
	return legacy90Re.MatchString(raw) || strings.Contains(raw, "due::")
}

// CurrentQuarter formats a time as the "2026-Q3" quarter slug — the value stamped
// on a Rock at creation and at carry.
func CurrentQuarter(t time.Time) string {
	q := (int(t.Month())-1)/3 + 1
	return fmt.Sprintf("%d-Q%d", t.Year(), q)
}

// migrateFromLegacy converts a doc parsed from the old format in place: the old
// "### 90-day" roots are already parsed into Rocks, so this stamps each Rock with
// the current quarter, strips retired `due::` fields, and ensures every area has a
// (possibly empty) "### 1-year — <year>" section. Rocks get no `serves::` — the
// needs-setup nudge links them later. Idempotent: fields already set are left alone.
func (d *Doc) migrateFromLegacy(now time.Time) {
	q := CurrentQuarter(now)
	year := now.Format("2006")
	for _, a := range d.Areas {
		if !a.hasAnnual {
			a.hasAnnual = true
		}
		if a.yearLabel == "" {
			a.yearLabel = year
		}
		for _, rock := range a.Rocks {
			if rock.Quarter == "" {
				rock.Quarter = q
			}
			stripDue(rock)
		}
	}
}

// stripDue removes any retired `due` inline field from a goal and its whole
// subtree (serialize would drop it anyway, but this keeps the in-memory doc clean).
func stripDue(g *Goal) {
	if len(g.Fields) > 0 {
		kept := g.Fields[:0]
		for _, f := range g.Fields {
			if strings.EqualFold(f.Key, "due") {
				continue
			}
			kept = append(kept, f)
		}
		g.Fields = kept
	}
	for _, c := range g.Children {
		stripDue(c)
	}
}
