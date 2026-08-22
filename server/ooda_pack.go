package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The standing context pack (ooda-context-pack plan, 2026-08-21). Zeck's
// open-ended asks ("draw the map") read files, not routes — so the SAME
// composed snapshot the portal serves is exported as flat markdown into his
// readable tree under /private/harnesses/zeck/. Every file is stamped with
// the OodaLive revision + timestamp, so a stale pack is immediately visible
// and never silently trusted.
//
// Rules of the projection:
//   - content is what the portal already shows members — the dashboard
//     figures, the work groups, the visible portfolio. No bank accounts,
//     no ownership splits, nothing the vault holds that the portal hides.
//   - deterministic: same snapshot → byte-identical pack (the timestamp is
//     the snapshot's own At, never the wall clock).
//   - regenerate only when the source revision moved; the README stamp is
//     written LAST, so a torn write never claims the new revision.
//   - everything stays inside /private. The vault itself is never touched.

// oodaPackFiles is the fixed manifest, README last — its stamp marks the
// pack complete, so a reader (or the drift guard) trusts README's revision.
var oodaPackFiles = []string{
	"properties.md", "entities.md", "deals.md", "contractors.md",
	"contracts.md", "backlog.md", "people.md", "README.md",
}

var oodaPackRevRe = regexp.MustCompile(`revision: ([0-9a-f]+)`)

// oodaPackRevision reads the revision stamped on an existing pack ("" = no
// pack yet, or a pack too old to carry a stamp).
func oodaPackRevision(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		return ""
	}
	if m := oodaPackRevRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// syncOodaPack writes the pack when the stamped revision differs from the
// snapshot's. Reports whether it wrote. Files land atomically (tmp+rename),
// group-readable so zeck (zeckshare) can read what benjamin writes.
func syncOodaPack(dir string, snap *oodaSnapshot) (bool, error) {
	if dir == "" || snap == nil {
		return false, nil
	}
	if snap.Revision != "" && oodaPackRevision(dir) == snap.Revision {
		return false, nil
	}
	files := oodaPackRender(snap)
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return false, err
	}
	for _, name := range oodaPackFiles {
		path := filepath.Join(dir, name)
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(files[name]), 0o664); err != nil {
			return false, err
		}
		if err := os.Rename(tmp, path); err != nil {
			return false, err
		}
	}
	return true, nil
}

// oodaPackRender composes the whole pack: file name → markdown. Pure — no
// I/O, no clock — so the drift guard can assert byte determinism.
func oodaPackRender(snap *oodaSnapshot) map[string]string {
	today := snap.At.Format("2006-01-02")
	// no team overrides here, deliberately: the pack regenerates only when the
	// VAULT revision moves, so overlay state baked in would go silently stale
	// on every portal PATCH — worse than the vault-only view it replaces. The
	// determinism guard also pins this: same snapshot, byte-identical pack.
	d := buildOodaDashboard(snap, today, nil)
	props := oodaVisibleProps(snap)
	sort.Slice(props, func(i, j int) bool { return props[i].Slug < props[j].Slug })

	stamp := func(title string) string {
		return fmt.Sprintf("# OODA real estate — %s\n\n> revision: %s · generated: %s · source: manifest OodaLive\n\n",
			title, snap.Revision, snap.At.UTC().Format(time.RFC3339))
	}

	out := map[string]string{}

	// ---- README.md: what this is + the portfolio in one paragraph ----
	{
		var b strings.Builder
		b.WriteString(stamp("context pack"))
		b.WriteString("This directory is the STANDING context pack for the OODA real-estate\n" +
			"domain: a flat markdown projection of the same live snapshot the team\n" +
			"portal serves. Manifest regenerates it whenever the vault's records\n" +
			"change — treat it as read-only, and trust the revision stamp above\n" +
			"over any cached or downloaded copy.\n\n")
		k := d.KPIs
		fmt.Fprintf(&b, "Portfolio: %d properties (%d owned · %d under contract · %d pipeline) across %d entities.\n",
			k.PortfolioSize, k.Owned, k.UnderContract, k.Pipeline, k.Entities)
		fmt.Fprintf(&b, "Money: committed %.0f · contracted %.0f · paid %.0f · plan to go %.0f · to close %.0f.\n",
			k.Committed, k.Contracted, k.Paid, k.PlanToGo, k.ToClose)
		fmt.Fprintf(&b, "Live projects: %d · over plan: %d", k.LiveProjects, k.OverPlan)
		if k.AsOf != "" {
			fmt.Fprintf(&b, " · ledger as of %s", k.AsOf)
		}
		b.WriteString(".\n\n")
		b.WriteString("- properties.md — every shared property: status, money, rocks, open work\n" +
			"- entities.md — holding entities: owned/acquiring/pipeline counts + money\n" +
			"- deals.md — deal bundles and their member parcels\n" +
			"- contractors.md — contractor records (scopes + contact)\n" +
			"- contracts.md — contracts: status, totals, draw-down, allocations\n" +
			"- backlog.md — open work and decisions, grouped by owner\n" +
			"- people.md — the team roster\n")
		out["README.md"] = b.String()
	}

	// ---- properties.md: the portfolio, one section per record ----
	{
		var b strings.Builder
		b.WriteString(stamp("properties"))
		for _, p := range props {
			f := oodaPropertyFacts(p, today)
			fmt.Fprintf(&b, "## %s (%s)\n", f.Short, p.Slug)
			fmt.Fprintf(&b, "- %s — %s · %s (%s) · acq: %s\n",
				orStr(p.Address, f.Short), orStr(p.Entity, "no entity"),
				strings.ReplaceAll(p.Status, "_", " "), f.Phase, f.Acq)
			if p.Deal != "" {
				fmt.Fprintf(&b, "- deal: %s\n", p.Deal)
			}
			fmt.Fprintf(&b, "- money: plan %.0f · committed %.0f · paid %.0f · to go %.0f\n",
				f.Plan, f.Committed, f.Paid, f.ToGo)
			if f.ToClose > 0 {
				fmt.Fprintf(&b, "- to close: %.0f\n", f.ToClose)
			}
			if f.Rock != "" {
				fmt.Fprintf(&b, "- current rock: %s%s\n", f.Rock,
					map[bool]string{true: " (due " + f.DoneBy + ")", false: ""}[f.DoneBy != ""])
			}
			if f.HasStages {
				fmt.Fprintf(&b, "- progress: %.0f%% of %d rocks · open work: %d\n", f.PctDone, len(p.Work), f.Open)
				for i := range p.Work {
					st := p.Work[i]
					mark := " "
					if st.Checked {
						mark = "x"
					}
					fmt.Fprintf(&b, "  - [%s] %s%s\n", mark, st.Text,
						map[bool]string{true: " (due " + st.DoneBy + ")", false: ""}[st.DoneBy != "" && !st.Checked])
				}
			}
			if flags := oodaAttentionWords(f); flags != "" {
				fmt.Fprintf(&b, "- attention: %s\n", flags)
			}
			for _, c := range snap.Contracts {
				for _, al := range c.Allocations {
					if strings.EqualFold(al.Property, p.Slug) {
						fmt.Fprintf(&b, "- contract: %s — %s, %.0f (%s)\n", c.Name, c.Contractor, al.Amount, c.Status)
						break
					}
				}
			}
			b.WriteString("\n")
		}
		out["properties.md"] = b.String()
	}

	// ---- entities.md: the dashboard's entity board, verbatim semantics ----
	// (owned and acquiring are NEVER summed — entities.go:44)
	{
		var b strings.Builder
		b.WriteString(stamp("entities"))
		for _, e := range d.Entities {
			name := e.Entity
			if name == "" {
				name = "— unassigned —"
			}
			fmt.Fprintf(&b, "## %s\n", name)
			fmt.Fprintf(&b, "- holdings: %d owned · %d acquiring (under contract) · %d pipeline\n",
				e.Owned, e.Acquiring, e.Pipeline)
			fmt.Fprintf(&b, "- money: committed %.0f · paid %.0f · to close %.0f\n", e.Committed, e.Paid, e.ToClose)
			fmt.Fprintf(&b, "- open work: %d\n\n", e.OpenWork)
		}
		out["entities.md"] = b.String()
	}

	// ---- deals.md ----
	{
		var b strings.Builder
		b.WriteString(stamp("deals"))
		members := map[string][]string{}
		for _, deal := range snap.Deals {
			for _, p := range props {
				if strings.EqualFold(p.Deal, deal.Slug) || strings.EqualFold(p.Deal, deal.Name) {
					members[deal.Slug] = append(members[deal.Slug], orStr(p.Short, p.Slug))
				}
			}
		}
		for _, row := range d.Deals {
			fmt.Fprintf(&b, "## %s (%s)\n", row.Name, row.Slug)
			fmt.Fprintf(&b, "- status: %s · members: %d · committed %.0f · paid %.0f\n",
				orStr(row.Status, "unknown"), row.Members, row.Committed, row.Paid)
			for _, m := range members[row.Slug] {
				fmt.Fprintf(&b, "  - %s\n", m)
			}
			b.WriteString("\n")
		}
		out["deals.md"] = b.String()
	}

	// ---- contractors.md ----
	{
		var b strings.Builder
		b.WriteString(stamp("contractors"))
		for _, c := range snap.Contractors {
			fmt.Fprintf(&b, "## %s (%s)\n", c.Name, c.Slug)
			scopes := strings.Join(c.Scopes, ", ")
			if scopes == "" {
				scopes = c.Trade
			}
			if scopes != "" {
				fmt.Fprintf(&b, "- scopes: %s\n", scopes)
			}
			if c.Email != "" {
				fmt.Fprintf(&b, "- email: %s\n", c.Email)
			}
			if c.Website != "" {
				fmt.Fprintf(&b, "- website: %s\n", c.Website)
			}
			b.WriteString("\n")
		}
		out["contractors.md"] = b.String()
	}

	// ---- contracts.md ----
	{
		var b strings.Builder
		b.WriteString(stamp("contracts"))
		for _, c := range snap.Contracts {
			fmt.Fprintf(&b, "## %s (%s)\n", c.Name, c.Slug)
			fmt.Fprintf(&b, "- %s · %s · total %.0f · drawn %.0f\n",
				orStr(c.Contractor, "no contractor"), orStr(c.Status, "no status"), c.Total, c.Drawn)
			if c.Date != "" || c.Expires != "" {
				fmt.Fprintf(&b, "- dated %s%s\n", orStr(c.Date, "—"),
					map[bool]string{true: " · expires " + c.Expires, false: ""}[c.Expires != ""])
			}
			for _, al := range c.Allocations {
				fmt.Fprintf(&b, "  - %s → %s: %.0f\n", al.Property, orStr(al.NodeID, "unallocated"), al.Amount)
			}
			b.WriteString("\n")
		}
		out["contracts.md"] = b.String()
	}

	// ---- backlog.md: the WORK tab as markdown — open items only, grouped
	// by owner, unassigned last (unassigned work is the finding) ----
	{
		var b strings.Builder
		b.WriteString(stamp("open work"))
		line := func(it oodaWorkItem, note string) {
			where := it.Container
			if it.Rock != "" {
				where = strings.TrimPrefix(where+" · "+it.Rock, " · ")
			}
			fmt.Fprintf(&b, "- [ ] %s (%s%s)%s\n", it.Title, it.Kind,
				map[bool]string{true: " · " + where, false: ""}[where != ""], note)
		}
		for _, g := range buildOodaWork(snap, today, nil) {
			name := orStr(g.Name, g.Owner)
			if g.Owner == "" {
				name = "— unassigned —"
			}
			fmt.Fprintf(&b, "## %s\n", name)
			for _, it := range g.Overdue {
				line(it, " — OVERDUE, was due "+it.Due)
			}
			for _, it := range g.DueThisWeek {
				line(it, " — due "+it.Due)
			}
			for _, it := range g.Open {
				line(it, "")
			}
			for _, it := range g.Decisions {
				line(it, map[bool]string{true: " — needed by " + it.Due, false: ""}[it.Due != ""])
			}
			for _, it := range g.Waiting {
				line(it, " — waiting")
			}
			b.WriteString("\n")
		}
		out["backlog.md"] = b.String()
	}

	// ---- people.md ----
	{
		var b strings.Builder
		b.WriteString(stamp("people"))
		for _, p := range snap.People {
			if p == nil {
				continue
			}
			fmt.Fprintf(&b, "- %s — %s", orStr(p.Initials, "?"), p.Name)
			if p.Role != "" {
				fmt.Fprintf(&b, " (%s)", p.Role)
			}
			if p.Email != "" {
				fmt.Fprintf(&b, " · %s", p.Email)
			}
			b.WriteString("\n")
		}
		out["people.md"] = b.String()
	}

	return out
}

// oodaAttentionWords names the lit attention rules, cockpit vocabulary.
func oodaAttentionWords(f oodaFacts) string {
	var w []string
	if f.Over {
		w = append(w, "over plan")
	}
	if f.Late {
		w = append(w, "late")
	}
	if f.Stalled {
		w = append(w, "stalled")
	}
	return strings.Join(w, ", ")
}

// ---- OodaLive glue ----

// UsePack points the standing context pack at dir and catches it up
// immediately (a restart with a warm cache still refreshes a stale pack).
func (l *OodaLive) UsePack(dir string) {
	l.mu.Lock()
	l.packDir = dir
	l.syncPackLocked()
	l.mu.Unlock()
}

// syncPackLocked exports the current snapshot if the pack's stamp is behind.
// Best-effort: a failed write logs once per revision and never blocks the
// portal read path that triggered the refresh.
func (l *OodaLive) syncPackLocked() {
	if l.packDir == "" || l.snap == nil {
		return
	}
	wrote, err := syncOodaPack(l.packDir, l.snap)
	if err != nil {
		if l.packErrRev != l.snap.Revision {
			l.packErrRev = l.snap.Revision
			log.Printf("ooda pack: %v", err)
		}
		return
	}
	if wrote {
		log.Printf("ooda pack: %s @ %s", l.packDir, l.snap.Revision)
	}
}

// PackLoop keeps the pack current when nobody is touching the portal —
// refresh() already regenerates on any read, so this is only the quiet-hours
// cadence. refresh is cheap when the source has not moved (a stat walk).
func (l *OodaLive) PackLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = l.refresh(false)
		}
	}
}
