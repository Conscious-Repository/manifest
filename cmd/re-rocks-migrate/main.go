// Command re-rocks-migrate is the one-shot Pass-1 migration for the
// real-estate overhaul (plans/realestate-overhaul-handoff.md §8, steps 1/3/4/5):
//
//  1. `## work` → `## rocks` heading rename on every property record; ids
//     frozen via FreezeWorkID wherever a ledger row tethers them (tether
//     integrity is the invariant).
//  2. The retired `## tasks` section merges INTO the tree: a line whose
//     [work:: id] back-tether names a live node transfers its metadata onto
//     that node (the Rev-3 dual-stamp twins collapse to one line); a line
//     with [stage:: Name] lands under that rock; everything else lands loose
//     under the current rock. The section is then removed.
//  3. tasks.md Real-Estate buckets whose headings link a property move their
//     OPEN tasks into that property's tree (move, not copy — naturally
//     idempotent). Loose RE-admin tasks and linkless buckets stay.
//  4. work-start + [weeks::] prefill [done-by::] on unchecked rocks.
//  5. Template records: `## stages` → `## rocks`.
//
// ACCEPTANCE (refuses to apply on failure): for every property, every
// tethered ledger row lands on the same node and the per-rock money picture
// (EstTotal / Paid / Committed / Recognized / Unreconciled) is byte-identical
// before vs after.
//
// Default is a DRY RUN printing every proposed change; pass -apply to write.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"manifest/realestate"
	"manifest/record"
	"manifest/tasks"
	"manifest/vaultwriter"
)

type propFile struct {
	rel      string // vault-relative path
	raw      string // current bytes
	fmEnd    int    // line index AFTER the closing --- (0 when no frontmatter)
	lines    []string
	section  string // "rocks" | "work" | "" (none)
	stages   []realestate.WorkStage
	legacy   []realestate.LegacyLine // ## tasks lines
	ledger   []realestate.LedgerRow
	changed  []string // human-readable change notes
	touched  bool
	workText map[string]bool
}

func main() {
	home, _ := os.UserHomeDir()
	vault := flag.String("vault", filepath.Join(home, "Documents", "index.ben"), "vault root")
	dataDir := flag.String("datadir", filepath.Join(home, ".config", "manifest"), "dataDir (write-audit log)")
	apply := flag.Bool("apply", false, "write the changes (default: dry run)")
	show := flag.String("show", "", "print the rebuilt file for one property slug (dry-run inspection)")
	flag.Parse()

	reRoot := filepath.Join("system", "realestate")
	vw := vaultwriter.New(*vault).WithZoneRoots("system", "extrinsic").WithAudit(*dataDir).Grant(
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
			Pattern: filepath.ToSlash(reRoot) + "/**", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "todos", Zone: record.ZoneKnowledge,
			Pattern: "tasks*", Actor: vaultwriter.ActorUserAction},
	)

	propsDir := filepath.Join(*vault, reRoot, "properties")
	entries, err := os.ReadDir(propsDir)
	if err != nil {
		fatal("read properties dir: %v", err)
	}
	props := map[string]*propFile{} // slug → file
	var slugs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		full := filepath.Join(propsDir, e.Name())
		raw, err := os.ReadFile(full)
		if err != nil {
			fatal("read %s: %v", e.Name(), err)
		}
		if !strings.Contains(string(raw), "categories: [property]") {
			continue
		}
		pf := &propFile{rel: filepath.ToSlash(filepath.Join(reRoot, "properties", e.Name())), raw: string(raw)}
		pf.lines = strings.Split(pf.raw, "\n")
		if led, err := os.ReadFile(record.Sidecar(full, record.SidecarLedger)); err == nil {
			pf.ledger = realestate.ParseLedgerBytes(led)
		}
		props[slug] = pf
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	// ---- per property: parse tree + legacy tasks, capture BEFORE money ----
	before := map[string]map[string]moneyRow{}
	// snapshot derives the per-rock money picture on a FRESH parse (join
	// mutates its argument; callers always pass a throwaway tree).
	snapshot := func(stages []realestate.WorkStage, ledger []realestate.LedgerRow) map[string]moneyRow {
		out := map[string]moneyRow{}
		realestate.JoinWorkLedger(stages, ledger, nil)
		for _, st := range stages {
			out[st.ID] = moneyRow{st.EstTotal, st.Paid, st.Committed, st.Recognized, st.Unreconciled}
		}
		return out
	}
	sectionLines := func(lines []string, name string) []string {
		var out []string
		in := false
		for _, ln := range lines {
			t := strings.TrimRight(ln, " \t")
			if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
				in = strings.EqualFold(t, "## "+name)
				continue
			}
			if in {
				out = append(out, ln)
			}
		}
		return out
	}
	for _, slug := range slugs {
		pf := props[slug]
		if len(sectionLines(pf.lines, "rocks")) > 0 {
			pf.section = "rocks"
		} else if hasHeading(pf.lines, "work") {
			pf.section = "work"
		}
		if pf.section != "" {
			pf.stages = realestate.ParseWork(sectionLines(pf.lines, pf.section))
		}
		pf.legacy = realestate.ParsePropertyTasks(sectionLines(pf.lines, "tasks"))
		// BEFORE money snapshot on a fresh parse (JoinWorkLedger mutates)
		before[slug] = snapshot(realestate.ParseWork(sectionLines(pf.lines, pf.section)), pf.ledger)
	}

	fmt.Println("== 1) heading rename + tether freeze ==")
	for _, slug := range slugs {
		pf := props[slug]
		if pf.section == "work" {
			pf.changed = append(pf.changed, "## work → ## rocks")
			pf.touched = true
		}
		frozen := 0
		seen := map[string]bool{}
		for _, r := range pf.ledger {
			if r.WorkID == "" || seen[r.WorkID] {
				continue
			}
			seen[r.WorkID] = true
			if realestate.FreezeWorkID(pf.stages, r.WorkID) {
				frozen++
			}
		}
		if frozen > 0 {
			pf.changed = append(pf.changed, fmt.Sprintf("froze %d tethered id(s)", frozen))
			pf.touched = true
		}
	}

	fmt.Println("== 2) `## tasks` merge into the tree ==")
	for _, slug := range slugs {
		pf := props[slug]
		if len(pf.legacy) == 0 {
			continue
		}
		list := &realestate.PropertyTaskList{Stages: pf.stages, Section: "rocks"}
		for _, ln := range pf.legacy {
			t := ln.Task
			if t == nil {
				continue // free prose in the retired section is dropped (it was never rendered)
			}
			wid := t.FieldValue("work")
			if wid != "" {
				if _, node := realestate.FindWorkNode(pf.stages, wid); node != nil {
					mergeMeta(node, t)
					pf.changed = append(pf.changed, fmt.Sprintf("merged twin %q onto %s", t.Text, wid))
					continue
				}
			}
			parent := ""
			if t.Stage != "" {
				parent = t.Stage
			}
			moved := *t
			moved.Stage = "" // position IS the placement now
			list.Append(&moved, parent)
			where := parent
			if where == "" {
				where = "current rock"
			}
			pf.changed = append(pf.changed, fmt.Sprintf("moved %q → %s", t.Text, where))
		}
		pf.stages = list.Stages
		pf.changed = append(pf.changed, "removed ## tasks section")
		pf.touched = true
	}

	fmt.Println("== 3) tasks.md RE buckets → property trees ==")
	tasksStore := tasks.NewStore(*vault, "tasks.md", vw.BindAbs("todos"))
	doc, err := tasksStore.Load()
	if err != nil {
		fatal("load tasks.md: %v", err)
	}
	tasksChanged := false
	for _, dom := range doc.Domains {
		if !strings.EqualFold(dom.Name, "Real Estate") {
			continue
		}
		for _, b := range dom.Buckets {
			slug := ""
			for _, link := range b.Links {
				cand := record.Slug(link, 60)
				if _, ok := props[cand]; ok {
					slug = cand
					break
				}
			}
			if slug == "" {
				continue // linkless / unresolvable bucket stays
			}
			var keep []*tasks.Task
			for _, t := range b.Tasks {
				if t.Checked {
					keep = append(keep, t)
					continue
				}
				pf := props[slug]
				list := &realestate.PropertyTaskList{Stages: pf.stages, Section: "rocks"}
				moved := *t
				moved.Rock, moved.Stage, moved.Issue = "", "", ""
				list.Append(&moved, "")
				pf.stages = list.Stages
				pf.touched = true
				pf.changed = append(pf.changed, fmt.Sprintf("from tasks.md bucket %q: %q", b.Name, t.Text))
				tasksChanged = true
				fmt.Printf("  tasks.md %q → %s: %q\n", b.Name, slug, t.Text)
			}
			b.Tasks = keep
		}
	}

	fmt.Println("== 4) done-by prefill from work-start + [weeks::] ==")
	for _, slug := range slugs {
		pf := props[slug]
		ws := frontmatterValue(pf.lines, "work-start")
		if ws == "" || len(pf.stages) == 0 {
			continue
		}
		for _, note := range prefillDoneBy(ws, pf.stages) {
			pf.changed = append(pf.changed, note)
			pf.touched = true
		}
	}

	// ---- rebuild property files + acceptance ----
	fmt.Println("== property changes ==")
	accept := true
	for _, slug := range slugs {
		pf := props[slug]
		if !pf.touched {
			continue
		}
		newRaw := rebuild(pf)
		if *show == slug {
			fmt.Printf("---- rebuilt %s ----\n%s\n---- end ----\n", slug, newRaw)
		}
		// acceptance: identical per-rock money before vs after
		afterStages := realestate.ParseWork(sectionLines(strings.Split(newRaw, "\n"), "rocks"))
		after := snapshot(afterStages, pf.ledger)
		if !sameMoney(before[slug], after) {
			accept = false
			fmt.Printf("  ✗ %s: MONEY DRIFT — refusing\n    before: %v\n    after:  %v\n", slug, before[slug], after)
			continue
		}
		fmt.Printf("  %s:\n", slug)
		for _, c := range pf.changed {
			fmt.Printf("    · %s\n", c)
		}
		if *apply {
			if err := vw.WriteCap("realestate", pf.rel, []byte(newRaw)); err != nil {
				fatal("write %s: %v", pf.rel, err)
			}
		}
	}
	if !accept {
		fatal("acceptance failed — nothing written for drifted records; fix and re-run")
	}

	fmt.Println("== 5) templates: ## stages → ## rocks ==")
	tplDir := filepath.Join(*vault, reRoot, "templates")
	if entries, err := os.ReadDir(tplDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			full := filepath.Join(tplDir, e.Name())
			raw, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			lines := strings.Split(string(raw), "\n")
			changed := false
			for i, ln := range lines {
				if strings.EqualFold(strings.TrimRight(ln, " \t"), "## stages") {
					lines[i] = "## rocks"
					changed = true
				}
			}
			if changed {
				fmt.Printf("  %s: ## stages → ## rocks\n", e.Name())
				if *apply {
					rel := filepath.ToSlash(filepath.Join(reRoot, "templates", e.Name()))
					if err := vw.WriteCap("realestate", rel, []byte(strings.Join(lines, "\n"))); err != nil {
						fatal("write %s: %v", rel, err)
					}
				}
			}
		}
	}

	if tasksChanged && *apply {
		if err := tasksStore.Save(doc); err != nil {
			fatal("save tasks.md: %v", err)
		}
	}

	if *apply {
		fmt.Println("\nAPPLIED. Every tethered row verified on its node; per-rock money identical pre/post.")
	} else {
		fmt.Println("\nDRY RUN — pass -apply to write.")
	}
}

// mergeMeta transfers a legacy twin line's metadata onto its tree node
// (fill-only: the node's own values win; checked state is authoritative on
// the node — the Rev-3 dual-stamp kept them in sync).
func mergeMeta(n *realestate.WorkNode, t *tasks.Task) {
	dst := n.Task
	if dst.Added == "" {
		dst.Added = t.Added
	}
	if dst.Done == "" && dst.Checked {
		dst.Done = t.Done
	}
	if dst.Owner == "" {
		dst.Owner = t.Owner
	}
	if dst.Rank == "" {
		dst.Rank = t.Rank
	}
	if dst.Waiting == "" {
		dst.Waiting = t.Waiting
	}
	if dst.Since == "" {
		dst.Since = t.Since
	}
	if pin := t.ExplicitID(); pin != "" && dst.ExplicitID() == "" {
		dst.PinID(pin)
	}
}

// prefillDoneBy walks rocks with the schedule cursor (work-start, [done::]
// pins, [weeks::] default 1) and stamps [done-by::] on unchecked rocks that
// lack one. Returns human-readable notes.
func prefillDoneBy(workStart string, stages []realestate.WorkStage) []string {
	spans := realestate.DeriveSchedule(workStart, stages)
	var notes []string
	for _, sp := range spans {
		for i := range stages {
			st := &stages[i]
			if st.ID != sp.ID || st.Checked || st.DoneBy != "" {
				continue
			}
			realestate.SetWorkField(stages, st.ID, "done-by", sp.End)
			st.DoneBy = sp.End
			notes = append(notes, fmt.Sprintf("done-by %s on %q", sp.End, st.Text))
		}
	}
	return notes
}

// rebuild reassembles the property file: heading renamed, tree section body
// re-emitted, `## tasks` section removed, every other byte untouched.
func rebuild(pf *propFile) string {
	var out []string
	emitted := strings.Split(strings.TrimRight(realestate.EmitWork(pf.stages), "\n"), "\n")
	inTree, inTasks, treeWritten := false, false, false
	for i, ln := range pf.lines {
		t := strings.TrimRight(ln, " \t")
		isHeading := strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ")
		if isHeading {
			inTree, inTasks = false, false
			switch {
			case strings.EqualFold(t, "## work"), strings.EqualFold(t, "## rocks"):
				out = append(out, "## rocks")
				out = append(out, emitted...)
				inTree, treeWritten = true, true
				continue
			case strings.EqualFold(t, "## tasks"):
				inTasks = true
				// drop a single preceding blank separator so we don't leave doubles
				if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				continue
			}
		}
		if inTree || inTasks {
			// old section bodies are replaced/dropped; keep the blank line that
			// separates this section from the NEXT heading
			if strings.TrimSpace(ln) == "" {
				next := i + 1
				for next < len(pf.lines) && strings.TrimSpace(pf.lines[next]) == "" {
					next++
				}
				if next < len(pf.lines) && strings.HasPrefix(strings.TrimRight(pf.lines[next], " \t"), "## ") {
					out = append(out, ln)
					inTree, inTasks = false, false
				}
			}
			continue
		}
		out = append(out, ln)
	}
	if !treeWritten && len(pf.stages) > 0 {
		// no tree section existed (tasks-only record): append one
		if strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, "## rocks")
		out = append(out, emitted...)
	}
	// exactly one trailing newline (a dropped tail section eats the original's)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

func hasHeading(lines []string, name string) bool {
	for _, ln := range lines {
		if strings.EqualFold(strings.TrimRight(ln, " \t"), "## "+name) {
			return true
		}
	}
	return false
}

func frontmatterValue(lines []string, key string) string {
	inFM := false
	for i, ln := range lines {
		t := strings.TrimRight(ln, " \t")
		if t == "---" {
			if i == 0 {
				inFM = true
				continue
			}
			if inFM {
				return ""
			}
		}
		if inFM {
			if v, ok := strings.CutPrefix(t, key+":"); ok {
				return strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
	}
	return ""
}

type moneyRow struct {
	est, paid, committed, recognized, unrec float64
}

func sameMoney(a, b map[string]moneyRow) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || va != vb {
			return false
		}
	}
	return true
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
