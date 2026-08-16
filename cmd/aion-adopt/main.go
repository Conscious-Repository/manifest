// Command aion-adopt is a one-shot, RE-RUNNABLE migration that ADOPTS the
// pre-bridge AION todos out of `to do.md` into the aion backlog. Before the
// day-capture→backlog bridge (manifest a4b6303) a task typed under an AION rock
// became a rock-tethered `to do.md` todo; those never reached
// system/aion/backlog.md, so they don't show in the AION tab and can't publish
// to the aionbio portal. This moves every to-do.md todo whose [rock::] is an
// aion/ rock into the backlog as a [kind:: task] item — preserving open/done
// state, captured/done dates, owner, and rank — removes the to-do.md line, and
// rewrites any daily-note [todo:: <old-id>] backlink to the new aion:<id> so
// seated day tasks stay synced.
//
// DRY-RUN by default: prints every would-be move and writes nothing until
// -apply. RE-RUNNABLE: a todo already present in the backlog (by title-derived
// id) is skipped; a second run after -apply is a no-op. Writes go through the
// audited vaultwriter todos + aion + daily capabilities.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"manifest/aion"
	"manifest/record"
	"manifest/tasks"
	"manifest/vaultwriter"
)

func main() {
	home, _ := os.UserHomeDir()
	vault := flag.String("vault", filepath.Join(home, "Documents", "index.ben"), "vault root")
	dataDir := flag.String("datadir", filepath.Join(home, ".config", "manifest"), "dataDir (write-audit log)")
	todosFile := flag.String("todos", "to do.md", "vault-root todos file")
	dailyDir := flag.String("daily", "intrinsic", "vault-relative daily-notes dir (for backlink rewrite)")
	apply := flag.Bool("apply", false, "write the changes (default: dry run)")
	flag.Parse()

	aionRoot := filepath.ToSlash(filepath.Join("system", "aion"))
	vw := vaultwriter.New(*vault).WithZoneRoots("system", "extrinsic").WithAudit(*dataDir).
		Grant(
			vaultwriter.Capability{Name: "todos", Zone: record.ZoneKnowledge,
				Pattern: strings.TrimSuffix(*todosFile, ".md") + "*", Actor: vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "aion", Zone: record.ZoneSystem,
				Pattern: aionRoot + "/**", Actor: vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "daily", Zone: record.ZoneKnowledge,
				Pattern: "????-??-??.md", Actor: vaultwriter.ActorUserAction},
		)
	tasksStore := tasks.NewStore(*vault, *todosFile, vw.BindAbs("todos"))
	aionStore := aion.NewStore(*vault, aionRoot, vw.BindAbs("aion"))

	doc, err := tasksStore.Load()
	if err != nil {
		fatal("load %s: %v", *todosFile, err)
	}

	// collect every aion/-rock-tethered todo (loose + bucketed), with the domain
	// + bucket it lives in so we can excise the exact line on -apply
	type victim struct {
		t      *tasks.Task
		dom    *tasks.Domain
		bucket *tasks.Bucket
	}
	var victims []victim
	for _, dm := range doc.Domains {
		for _, t := range dm.Tasks {
			if strings.HasPrefix(t.Rock, "aion/") {
				victims = append(victims, victim{t, dm, nil})
			}
		}
		for _, bk := range dm.Buckets {
			for _, t := range bk.Tasks {
				if strings.HasPrefix(t.Rock, "aion/") {
					victims = append(victims, victim{t, dm, bk})
				}
			}
		}
	}
	if len(victims) == 0 {
		fmt.Println("no aion/-tethered todos in", *todosFile, "— nothing to adopt.")
		return
	}

	backlog := aionStore.LoadBacklog()
	// oldID → new aion:<hex>, for the daily-note backlink rewrite
	rewrites := map[string]string{}
	adopted := 0
	for _, v := range victims {
		t := v.t
		newID := aion.ItemID(aion.KindTask, t.Text)
		state := "open"
		if t.Checked {
			state = "done"
		}
		where := v.dom.Name
		if v.bucket != nil {
			where += " / " + v.bucket.Name
		}
		if backlog.Find(newID) != nil {
			fmt.Printf("  · already in backlog, skipping: %s\n", truncate(t.Text, 60))
			continue
		}
		fmt.Printf("  → %-60s [%s] rock=%s  (from %s)\n", truncate(t.Text, 60), state, t.Rock, where)
		rewrites[t.ID] = "aion:" + newID
		adopted++
	}
	fmt.Printf("\n%d todo(s) to adopt into the aion backlog (%d already there)\n", adopted, len(victims)-adopted)

	if !*apply {
		fmt.Println("\nDRY RUN — nothing written. Re-run with -apply to migrate.")
		return
	}

	// 1) append to the backlog, preserving state/dates/owner/rank
	for _, v := range victims {
		t := v.t
		if backlog.Find(aion.ItemID(aion.KindTask, t.Text)) != nil {
			continue
		}
		status := aion.StatusOpen
		if t.Checked {
			status = aion.StatusDone
		}
		owner := t.Owner
		if strings.TrimSpace(owner) == "" {
			owner = "BA"
		}
		it := &aion.BacklogItem{
			Kind: aion.KindTask, Text: t.Text, Checked: t.Checked,
			Owner: owner, Captured: t.Added, Rock: t.Rock,
			Status: status, DoneOn: t.Done, Rank: t.Rank,
		}
		if err := aionStore.AddItem(it); err != nil {
			fatal("add %q: %v", t.Text, err)
		}
	}

	// 2) excise the adopted lines from to do.md (byte-stable fixpoint save)
	adopt := map[*tasks.Task]bool{}
	for _, v := range victims {
		adopt[v.t] = true
	}
	for _, dm := range doc.Domains {
		dm.Tasks = keepTasks(dm.Tasks, adopt)
		for _, bk := range dm.Buckets {
			bk.Tasks = keepTasks(bk.Tasks, adopt)
		}
	}
	if err := tasksStore.Save(doc); err != nil {
		fatal("save %s: %v", *todosFile, err)
	}

	// 3) rewrite daily-note [todo:: <old>] → [todo:: aion:<new>] so seated tasks
	// keep syncing to the backlog item
	notesFixed := rewriteBacklinks(*vault, *dailyDir, rewrites, vw.BindAbs("daily"))

	fmt.Printf("\napplied: %d todo(s) adopted into %s/backlog.md, removed from %s, %d daily-note backlink(s) rewritten\n",
		adopted, aionRoot, *todosFile, notesFixed)
}

func keepTasks(list []*tasks.Task, drop map[*tasks.Task]bool) []*tasks.Task {
	out := list[:0:0]
	for _, t := range list {
		if !drop[t] {
			out = append(out, t)
		}
	}
	return out
}

var todoRefRe = regexp.MustCompile(`\[todo::\s*([^\]]+)\]`)

func rewriteBacklinks(vault, dailyDir string, rewrites map[string]string, write func(string, []byte) error) int {
	if len(rewrites) == 0 {
		return 0
	}
	dir := filepath.Join(vault, filepath.FromSlash(dailyDir))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	fixed := 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		changed := false
		next := todoRefRe.ReplaceAllStringFunc(string(raw), func(m string) string {
			sub := todoRefRe.FindStringSubmatch(m)
			if sub == nil {
				return m
			}
			if newID, ok := rewrites[strings.TrimSpace(sub[1])]; ok {
				changed = true
				return "[todo:: " + newID + "]"
			}
			return m
		})
		if !changed {
			continue
		}
		if err := write(full, []byte(next)); err != nil {
			fatal("rewrite %s: %v", e.Name(), err)
		}
		fixed++
	}
	return fixed
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "aion-adopt: "+format+"\n", args...)
	os.Exit(1)
}
