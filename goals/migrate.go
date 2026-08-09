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

// needsMigration reports whether raw goals.md bytes are the pre-v2 format,
// signalled by an old "### 90-day" heading. (A bare `due::` no longer signals
// legacy — it was re-activated as a rock's ISO timeline end, portal §7 — so only
// the heading distinguishes a pre-v2 file that still needs the one-time upgrade.)
func needsMigration(raw string) bool {
	return legacy90Re.MatchString(raw)
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

// stripDue removes the retired legacy `due` (old 30-day deadline semantics)
// from a goal and its whole subtree. `due` was re-activated as a rock's ISO
// timeline end (portal §7), rebuilt from g.Due — so clear the struct field too,
// not just the passthrough Fields, or the legacy value would re-emit.
func stripDue(g *Goal) {
	g.Due = ""
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
