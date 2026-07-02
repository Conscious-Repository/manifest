// Package vaultindex is the headless-Dataview reader over the whole Obsidian
// vault: it parses every markdown note the way Dataview does — categories in
// both YAML styles, alias/aliases, inline [key:: value] fields, [[wikilinks]] —
// and projects the result into a rebuildable SQLite index (with FTS5) the
// dashboard, funder lens, and spirits query. It is the index-layer KERNEL
// (plans/obsidian-as-database.md); it NEVER writes the vault.
//
// Ground rules baked in here (plans/vault-audit-and-revised-recs.md §0/§3/§5):
//   - categories come inline (`categories: [x]`) AND as block dash-lists; parse both.
//   - match category values EXACTLY — never normalize or rewrite.
//   - a note has a date only from a dated filename (YYYY-MM-DD prefix) or a
//     `date:` frontmatter field; undated notes never produce date claims.
//   - a link TARGET is an entity whether or not a note exists behind it.
//   - content under the AI-authored regions (Agents/**, excalibur/**) is indexed
//     and searchable but tagged so it can be excluded from interaction/last-met.
package vaultindex

import (
	"path"
	"regexp"
	"strings"
)

// Note is one parsed markdown file — the row the indexer projects into SQLite.
type Note struct {
	Path          string   // vault-relative, forward-slash
	Name          string   // basename without .md
	Date          string   // "YYYY-MM-DD" or ""
	DateSource    string   // "filename" | "frontmatter" | ""
	Categories    []string // exact frontmatter values, order preserved, de-duped
	Aliases       []string // union of alias: and aliases:
	Emails        []string // union of email: and emails: (confirm-once contact matching)
	InlineFields  []InlineField
	Links         []Link
	Tasks         []Task
	AIAuthored    bool
	HasTranscript bool // body carries a speaker-labelled transcript (Granola-export shape)
	MTime         int64
	Body          string // text after the frontmatter block (FTS source)
}

// Link is one [[wikilink]] occurrence. Key is the resolution target (lowercased
// basename, heading/block/display stripped) — the entity identity. Display is
// what the note showed the reader.
type Link struct {
	Key     string
	Display string
}

// InlineField is a Dataview inline field: [key:: value] or a line-level key:: value.
type InlineField struct{ Key, Value string }

// Task is an open-loop candidate: a checkbox item, or a plain line under a
// next-step-ish heading. Line is the ABSOLUTE 0-based file line (so a toggle
// edits the right line). Kind ∈ checkbox|nextstep; only checkboxes toggle.
type Task struct {
	Line    int
	Text    string
	Checked bool
	Kind    string
}

var (
	datedFileRe    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)
	isoDateRe      = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)
	wikilinkRe     = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	bracketFieldRe = regexp.MustCompile(`\[([A-Za-z0-9 _/-]+?)::\s*([^\]]*)\]`)
	lineFieldRe    = regexp.MustCompile(`(?m)^([A-Za-z0-9 _/-]+?)::[ \t]*(.+?)\s*$`)
	// a Granola-export transcript turn: a **speaker:** label at line start
	speakerLineRe = regexp.MustCompile(`(?m)^\s*\*\*[^*\n]{1,40}:\*\*`)
	// task + heading extraction (open loops)
	headingRe  = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*\S)\s*$`)
	checkboxRe = regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]\s?(.*)$`)
	nextStepRe = regexp.MustCompile(`(?i)(next[- ]?steps?|action[- ]?items?|to-?dos?|follow[- ]?ups?)`)
)

// ParseNote parses one note. relPath is vault-relative (forward-slash); content
// is the raw file bytes; mtime is unix seconds; aiRegions are the vault-relative
// path prefixes whose content is AI-authored (e.g. "Agents/", "excalibur/").
func ParseNote(relPath string, content []byte, mtime int64, aiRegions []string) Note {
	relPath = path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	n := Note{Path: relPath, Name: strings.TrimSuffix(path.Base(relPath), ".md"), MTime: mtime}

	fm, body := splitFrontmatter(string(content))
	n.Body = body

	n.Categories = dedupe(fm["categories"])
	n.Aliases = dedupe(append(append([]string{}, fm["alias"]...), fm["aliases"]...))
	n.Emails = dedupe(append(append([]string{}, fm["email"]...), fm["emails"]...))

	// date: dated filename wins; else a date: frontmatter field.
	if m := datedFileRe.FindStringSubmatch(n.Name); m != nil {
		n.Date, n.DateSource = m[1], "filename"
	} else if d := firstNonEmpty(fm["date"]); d != "" {
		if mm := isoDateRe.FindStringSubmatch(d); mm != nil {
			n.Date, n.DateSource = mm[1], "frontmatter"
		}
	}

	// links + inline fields are read from the WHOLE file (Dataview counts links
	// anywhere, including attendee rows and frontmatter values).
	whole := string(content)
	n.Links = extractLinks(whole)
	n.InlineFields = extractInlineFields(body) // fields live in the body, not the YAML block
	// a speaker-labelled body (≥3 turns) marks a transcript note (funder §4)
	n.HasTranscript = len(speakerLineRe.FindAllStringIndex(body, 4)) >= 3
	n.Tasks = extractTasks(whole)

	for _, r := range aiRegions {
		if r != "" && (relPath == strings.TrimSuffix(r, "/") || strings.HasPrefix(relPath, strings.TrimSuffix(r, "/")+"/")) {
			n.AIAuthored = true
			break
		}
	}
	return n
}

// extractLinks pulls every [[target]] / [[target|display]] / [[target#h]] and
// normalizes the target to an entity key (lowercased basename, no #/^/|display).
func extractLinks(s string) []Link {
	var out []Link
	seen := map[string]bool{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(s, -1) {
		raw := m[1]
		display := raw
		if i := strings.Index(raw, "|"); i >= 0 {
			display = strings.TrimSpace(raw[i+1:])
			raw = raw[:i]
		}
		if i := strings.IndexAny(raw, "#"); i >= 0 { // drop heading/block ref
			raw = raw[:i]
		}
		target := strings.TrimSpace(raw)
		if target == "" {
			continue
		}
		if i := strings.LastIndex(target, "/"); i >= 0 { // link may carry a path
			target = target[i+1:]
		}
		key := strings.ToLower(strings.TrimSpace(target))
		if key == "" {
			continue
		}
		if display == m[1] { // no explicit display → show the target text
			display = strings.TrimSpace(target)
		}
		if seen[key] {
			continue // one edge per (src,target); multiplicity isn't needed
		}
		seen[key] = true
		out = append(out, Link{Key: key, Display: display})
	}
	return out
}

// extractTasks pulls open-loop candidates with ABSOLUTE file line numbers: every
// checkbox (`- [ ]` / `- [x]`, toggleable), plus plain content lines under a
// next-step-ish heading (surfaced but not toggleable). Scanning the whole file
// keeps line numbers absolute so a toggle edits the right line.
func extractTasks(content string) []Task {
	var out []Task
	inNextStep := false
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		if m := headingRe.FindStringSubmatch(line); m != nil {
			inNextStep = nextStepRe.MatchString(m[1])
			continue
		}
		if m := checkboxRe.FindStringSubmatch(line); m != nil {
			out = append(out, Task{
				Line: i, Text: strings.TrimSpace(m[2]),
				Checked: m[1] == "x" || m[1] == "X", Kind: "checkbox",
			})
			continue
		}
		if inNextStep {
			t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*•"))
			if t != "" {
				out = append(out, Task{Line: i, Text: t, Kind: "nextstep"})
			}
		}
	}
	return out
}

func extractInlineFields(body string) []InlineField {
	var out []InlineField
	seen := map[string]bool{}
	add := func(k, v string) {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			return
		}
		sig := k + "\x00" + v
		if seen[sig] {
			return
		}
		seen[sig] = true
		out = append(out, InlineField{Key: k, Value: v})
	}
	for _, m := range bracketFieldRe.FindAllStringSubmatch(body, -1) {
		add(m[1], m[2])
	}
	// line-level `key:: value` (Dataview's unbracketed form), skipping ones that
	// were already inside brackets on that line.
	for _, m := range lineFieldRe.FindAllStringSubmatch(body, -1) {
		if strings.Contains(m[0], "[") {
			continue
		}
		add(m[1], m[2])
	}
	return out
}

func firstNonEmpty(xs []string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

func dedupe(xs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}
