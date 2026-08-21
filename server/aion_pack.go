package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The standing context pack for kairos (aion-context-pack plan, 2026-08-21) —
// the exact mirror of zeck's OODA pack (ooda_pack.go), closed for the AION
// domain. Kairos's open-ended asks read files, not routes — so the SAME
// contract AionLive serves the team portal (/data/*.json) is exported as flat
// markdown into AION's /shared area, the cross-host mount lab-apps reads
// natively (owner decision 2026-08-21, item 2: AION context may live in
// AION's /shared). Every file is stamped with the AionLive revision +
// timestamp, so a stale pack is immediately visible and never silently
// trusted.
//
// Rules of the projection:
//   - content is what the portal already shows members — the rendered
//     contract, nothing more. The vault is never read here; the finances
//     body, transcript quotes, and retired records are already outside the
//     contract (aion/export.go's leak canary).
//   - deterministic: same snapshot → byte-identical pack (the timestamp is
//     the snapshot's own At, never the wall clock).
//   - regenerate only when the source revision moved; the README stamp is
//     written LAST, so a torn write never claims the new revision.
//   - /private/consciousrepo stays read-only upstream and untouched here.

// aionPackFiles is the fixed manifest, README last — its stamp marks the
// pack complete, so a reader (or the drift guard) trusts README's revision.
var aionPackFiles = []string{
	"backlog.md", "goals.md", "vto.md", "people.md", "heuristics.md",
	"finances.md", "hiring.md", "references.md", "README.md",
}

var aionPackRevRe = regexp.MustCompile(`revision: ([0-9a-f]+)`)

// aionPackSnapshot pins the served contract blobs to the two facts every
// pack file must carry: the revision and the compose timestamp.
type aionPackSnapshot struct {
	Revision string
	At       time.Time
	Files    map[string][]byte // keyed by URL path (/data/backlog.json, /content/hiring.md)
}

// aionPackRevision reads the revision stamped on an existing pack ("" = no
// pack yet, or a pack too old to carry a stamp).
func aionPackRevision(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		return ""
	}
	if m := aionPackRevRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// syncAionPack writes the pack when the stamped revision differs from the
// snapshot's. Reports whether it wrote. Files land atomically (tmp+rename),
// group-readable so kairos (teamshare) can read what benjamin writes.
func syncAionPack(dir string, snap *aionPackSnapshot) (bool, error) {
	if dir == "" || snap == nil {
		return false, nil
	}
	if snap.Revision != "" && aionPackRevision(dir) == snap.Revision {
		return false, nil
	}
	files := aionPackRender(snap)
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return false, err
	}
	for _, name := range aionPackFiles {
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

// The contract shapes the renderer reads back — the portal-facing subset of
// aion/export.go's export types, deliberately re-declared here so a contract
// shape change breaks THIS projection visibly instead of silently.
type aionPackItem struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Title    string  `json:"title"`
	Owner    *string `json:"owner"`
	Source   string  `json:"source"`
	Captured string  `json:"captured"`
	Rock     *string `json:"rock"`
	Due      *string `json:"due"`
	Status   *string `json:"status"`
	DoneOn   *string `json:"done_on"`
	NeededBy *string `json:"needed_by"`
	Decided  *string `json:"decided"`
	Outcome  *string `json:"outcome"`
}

type aionPackGoal struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Horizon   string   `json:"horizon"`
	Status    string   `json:"status"`
	Serves    *string  `json:"serves"`
	ServesAll []string `json:"serves_all"`
	Closed    string   `json:"closed"`
	Owner     *string  `json:"owner"`
	Quarter   string   `json:"quarter"`
	Start     string   `json:"start"`
	Due       string   `json:"due"`
	Children  []string `json:"children"`
}

type aionPackHeuristic struct {
	Statement      string `json:"statement"`
	First          string `json:"first"`
	Reinforcements []struct {
		Source string  `json:"source"`
		Date   *string `json:"date"`
	} `json:"reinforcements"`
}

// aionPackRender composes the whole pack: file name → markdown. Pure — no
// I/O, no clock — so the drift guard can assert byte determinism.
func aionPackRender(snap *aionPackSnapshot) map[string]string {
	read := func(name string, v any) { _ = json.Unmarshal(snap.Files[name], v) }
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}

	var backlog struct {
		Items []aionPackItem `json:"items"`
	}
	read("/data/backlog.json", &backlog)
	var goals struct {
		Goals []aionPackGoal `json:"goals"`
	}
	read("/data/goals.json", &goals)
	var people struct {
		People []struct {
			Initials string `json:"initials"`
			Name     string `json:"name"`
			Role     string `json:"role"`
			Email    string `json:"email"`
		} `json:"people"`
	}
	read("/data/people.json", &people)
	var heur struct {
		Heuristics []aionPackHeuristic `json:"heuristics"`
	}
	read("/data/heuristics.json", &heur)
	var fin struct {
		Capital      *float64 `json:"capital"`
		MonthlyBurn  *float64 `json:"monthly_burn"`
		RunwayMonths *float64 `json:"runway_months"`
		AsOf         string   `json:"as_of"`
		Currency     string   `json:"currency"`
		Source       string   `json:"source"`
		Note         string   `json:"note"`
	}
	read("/data/finances.json", &fin)
	var vto struct {
		CoreValues        []string          `json:"core_values"`
		CoreFocus         map[string]string `json:"core_focus"`
		TenYearTarget     string            `json:"ten_year_target"`
		MarketingStrategy struct {
			Target  string   `json:"target"`
			Uniques []string `json:"uniques"`
		} `json:"marketing_strategy"`
		ThreeYearPicture struct {
			Date        string   `json:"date"`
			Measurables []string `json:"measurables"`
		} `json:"three_year_picture"`
		OneYearPlan struct {
			Date        string   `json:"date"`
			Goals       []string `json:"goals"`
			Measurables []string `json:"measurables"`
		} `json:"one_year_plan"`
		Quarter map[string]string `json:"quarter"`
	}
	read("/data/vto.json", &vto)

	stamp := func(title string) string {
		return fmt.Sprintf("# AION — %s\n\n> revision: %s · generated: %s · source: manifest AionLive\n\n",
			title, snap.Revision, snap.At.UTC().Format(time.RFC3339))
	}

	out := map[string]string{}

	// ---- backlog.md: tasks + decisions, contract order preserved ----
	{
		var b strings.Builder
		b.WriteString(stamp("backlog"))
		line := func(it aionPackItem) {
			status := deref(it.Status)
			mark := " "
			if status == "done" || status == "decided" {
				mark = "x"
			}
			fmt.Fprintf(&b, "- [%s] %s", mark, it.Title)
			var tags []string
			if o := deref(it.Owner); o != "" {
				tags = append(tags, "owner "+o)
			}
			if r := deref(it.Rock); r != "" {
				tags = append(tags, "rock "+r)
			}
			if d := deref(it.Due); d != "" {
				tags = append(tags, "due "+d)
			}
			if nb := deref(it.NeededBy); nb != "" {
				tags = append(tags, "needed by "+nb)
			}
			if dn := deref(it.DoneOn); dn != "" {
				tags = append(tags, "done "+dn)
			}
			if dc := deref(it.Decided); dc != "" {
				tags = append(tags, "decided "+dc)
			}
			if len(tags) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(tags, " · "))
			}
			if oc := deref(it.Outcome); oc != "" {
				fmt.Fprintf(&b, " → %s", oc)
			}
			b.WriteString("\n")
		}
		b.WriteString("## Tasks\n")
		for _, it := range backlog.Items {
			if it.Kind != "decision" {
				line(it)
			}
		}
		b.WriteString("\n## Decisions\n")
		for _, it := range backlog.Items {
			if it.Kind == "decision" {
				line(it)
			}
		}
		out["backlog.md"] = b.String()
	}

	// ---- goals.md: the ladder, one section per horizon ----
	{
		var b strings.Builder
		b.WriteString(stamp("goals"))
		for _, h := range []struct{ key, title string }{
			{"1yr", "1-year goals"}, {"rock", "Rocks (90-day)"}, {"30", "30-day steps"},
		} {
			fmt.Fprintf(&b, "## %s\n\n", h.title)
			for _, g := range goals.Goals {
				if g.Horizon != h.key {
					continue
				}
				fmt.Fprintf(&b, "### %s (%s)\n", g.Title, g.ID)
				facts := []string{"status " + orStr(g.Status, "open")}
				if o := deref(g.Owner); o != "" {
					facts = append(facts, "owner "+o)
				}
				if g.Quarter != "" {
					facts = append(facts, g.Quarter)
				}
				if g.Start != "" || g.Due != "" {
					facts = append(facts, orStr(g.Start, "?")+" → "+orStr(g.Due, "?"))
				}
				if g.Closed != "" {
					facts = append(facts, "closed "+g.Closed)
				}
				fmt.Fprintf(&b, "- %s\n", strings.Join(facts, " · "))
				serves := g.ServesAll
				if len(serves) == 0 && deref(g.Serves) != "" {
					serves = []string{*g.Serves}
				}
				if len(serves) > 0 {
					fmt.Fprintf(&b, "- serves: %s\n", strings.Join(serves, ", "))
				}
				if len(g.Children) > 0 {
					fmt.Fprintf(&b, "- children: %s\n", strings.Join(g.Children, ", "))
				}
				b.WriteString("\n")
			}
		}
		out["goals.md"] = b.String()
	}

	// ---- vto.md: the V/TO, section order fixed (never map iteration) ----
	{
		var b strings.Builder
		b.WriteString(stamp("V/TO"))
		b.WriteString("## Core values\n")
		for _, v := range vto.CoreValues {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n## Core focus\n")
		fmt.Fprintf(&b, "- purpose: %s\n- niche: %s\n", vto.CoreFocus["purpose"], vto.CoreFocus["niche"])
		fmt.Fprintf(&b, "\n## 10-year target\n- %s\n", vto.TenYearTarget)
		fmt.Fprintf(&b, "\n## Marketing strategy\n- target: %s\n", vto.MarketingStrategy.Target)
		for _, u := range vto.MarketingStrategy.Uniques {
			fmt.Fprintf(&b, "- unique: %s\n", u)
		}
		fmt.Fprintf(&b, "\n## 3-year picture (%s)\n", orStr(vto.ThreeYearPicture.Date, "no date"))
		for _, m := range vto.ThreeYearPicture.Measurables {
			fmt.Fprintf(&b, "- %s\n", m)
		}
		fmt.Fprintf(&b, "\n## 1-year plan (%s)\n", orStr(vto.OneYearPlan.Date, "no date"))
		for _, g := range vto.OneYearPlan.Goals {
			fmt.Fprintf(&b, "- goal: %s\n", g)
		}
		for _, m := range vto.OneYearPlan.Measurables {
			fmt.Fprintf(&b, "- %s\n", m)
		}
		fmt.Fprintf(&b, "\n## Current quarter\n- %s → %s\n",
			orStr(vto.Quarter["start"], "?"), orStr(vto.Quarter["end"], "?"))
		out["vto.md"] = b.String()
	}

	// ---- people.md: the roster ----
	{
		var b strings.Builder
		b.WriteString(stamp("people"))
		for _, p := range people.People {
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

	// ---- heuristics.md: live entries only (retired never enter the contract) ----
	{
		var b strings.Builder
		b.WriteString(stamp("heuristics"))
		for _, h := range heur.Heuristics {
			fmt.Fprintf(&b, "- %s (first %s)\n", h.Statement, orStr(h.First, "undated"))
			for _, r := range h.Reinforcements {
				fmt.Fprintf(&b, "  - %s%s\n", r.Source,
					map[bool]string{true: " (" + deref(r.Date) + ")", false: ""}[deref(r.Date) != ""])
			}
		}
		out["heuristics.md"] = b.String()
	}

	// ---- finances.md: the portal's own figures, nothing from the body ----
	{
		var b strings.Builder
		b.WriteString(stamp("finances"))
		money := func(label string, v *float64) {
			if v != nil {
				fmt.Fprintf(&b, "- %s: %.0f %s\n", label, *v, fin.Currency)
			}
		}
		money("capital", fin.Capital)
		money("monthly burn", fin.MonthlyBurn)
		if fin.RunwayMonths != nil {
			fmt.Fprintf(&b, "- runway: %.1f months\n", *fin.RunwayMonths)
		}
		if fin.AsOf != "" {
			fmt.Fprintf(&b, "- as of: %s\n", fin.AsOf)
		}
		if fin.Source != "" {
			fmt.Fprintf(&b, "- source: %s\n", fin.Source)
		}
		if fin.Note != "" {
			fmt.Fprintf(&b, "- note: %s\n", fin.Note)
		}
		out["finances.md"] = b.String()
	}

	// ---- hiring.md / references.md: the contract's verbatim content files,
	// under the same freshness stamp ----
	out["hiring.md"] = stamp("hiring") + string(snap.Files["/content/hiring.md"])
	out["references.md"] = stamp("references") + string(snap.Files["/content/references.md"])

	// ---- README.md: what this is + the domain in one paragraph ----
	{
		var b strings.Builder
		b.WriteString(stamp("context pack"))
		b.WriteString("This directory is the STANDING context pack for the AION team domain:\n" +
			"a flat markdown projection of the same live contract the team portal\n" +
			"serves. Manifest regenerates it whenever the vault's records change —\n" +
			"treat it as read-only, and trust the revision stamp above over any\n" +
			"cached or downloaded copy.\n\n")
		openTasks, openDecisions := 0, 0
		for _, it := range backlog.Items {
			if it.Kind == "decision" {
				if deref(it.Status) != "decided" {
					openDecisions++
				}
			} else if deref(it.Status) != "done" {
				openTasks++
			}
		}
		oneYear, rocks, thirty := 0, 0, 0
		for _, g := range goals.Goals {
			switch g.Horizon {
			case "1yr":
				oneYear++
			case "rock":
				rocks++
			case "30":
				thirty++
			}
		}
		fmt.Fprintf(&b, "Backlog: %d open tasks · %d open decisions (%d records).\n",
			openTasks, openDecisions, len(backlog.Items))
		fmt.Fprintf(&b, "Goals: %d 1-year · %d rocks · %d 30-day. Heuristics: %d. People: %d.\n",
			oneYear, rocks, thirty, len(heur.Heuristics), len(people.People))
		if fin.RunwayMonths != nil {
			fmt.Fprintf(&b, "Finances: runway %.1f months", *fin.RunwayMonths)
			if fin.AsOf != "" {
				fmt.Fprintf(&b, " (as of %s)", fin.AsOf)
			}
			b.WriteString(".\n")
		}
		b.WriteString("\n" +
			"- backlog.md — open tasks and decisions (owners, rocks, deadlines)\n" +
			"- goals.md — the goal ladder: 1-year goals, rocks, 30-day steps\n" +
			"- vto.md — the V/TO: values, focus, targets, quarter\n" +
			"- people.md — the team roster\n" +
			"- heuristics.md — the live operating heuristics\n" +
			"- finances.md — capital, burn, runway (portal figures only)\n" +
			"- hiring.md — open roles (verbatim)\n" +
			"- references.md — reading and reference links (verbatim)\n")
		out["README.md"] = b.String()
	}

	return out
}

// ---- AionLive glue ----

// UseAPack points the standing context pack at dir and catches it up
// immediately (a restart with a warm cache still refreshes a stale pack).
func (l *AionLive) UseAPack(dir string) {
	l.mu.Lock()
	l.packDir = dir
	l.syncPackLocked()
	l.mu.Unlock()
}

// syncPackLocked exports the current contract if the pack's stamp is behind.
// Best-effort: a failed write logs once per revision and never blocks the
// portal read path that triggered the refresh. The embedded bootstrap
// contract never exports — it carries no real revision to stamp.
func (l *AionLive) syncPackLocked() {
	if l.packDir == "" || len(l.files) == 0 || l.servingRev == "" || l.servingRev == "embedded" {
		return
	}
	snap := &aionPackSnapshot{Revision: l.servingRev, At: l.lastGoodAt, Files: l.files}
	wrote, err := syncAionPack(l.packDir, snap)
	if err != nil {
		if l.packErrRev != l.servingRev {
			l.packErrRev = l.servingRev
			log.Printf("aion pack: %v", err)
		}
		return
	}
	if wrote {
		log.Printf("aion pack: %s @ %s", l.packDir, l.servingRev)
	}
}

// PackLoop keeps the pack current when nobody is touching the portal —
// refresh() already regenerates on any read, so this is only the quiet-hours
// cadence. refresh is cheap when the source has not moved (a corpus hash).
func (l *AionLive) PackLoop(ctx context.Context, every time.Duration) {
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
