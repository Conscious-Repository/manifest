// Command goodreads-import is a one-shot tool (reading-plan §2) that seeds book
// records under the vault's extrinsic/ zone from a Goodreads library export.
// It is DRY-RUN by default: it prints a merge report (creates, merges,
// collisions, author resolution, skips) and writes nothing until -apply.
//
// Conventions (locked with Benjamin 2026-07-08, STEP-0 audited):
//   - flat extrinsic/<slug>.md (matches the existing 22 book notes)
//   - categories: [books] + a #book body marker
//   - authors linked as wikilinks; an existing note → its exact name, else lowercase
//   - to-read shelf dropped; no goodreads-id stored
//   - idempotent: a book already present (by title) merges additively — body
//     byte-for-byte preserved, only-missing frontmatter fields filled — so a
//     re-run against the same CSV changes nothing.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"manifest/mdfm"
	"manifest/vaultindex"
)

func main() {
	csvPath := flag.String("csv", "", "path to goodreads_library_export.csv")
	vaultPath := flag.String("vault", "", "path to the Obsidian vault")
	apply := flag.Bool("apply", false, "write files (default: dry-run report only)")
	systemRoot := flag.String("system", "system", "system-zone root")
	extrinsicRoot := flag.String("extrinsic", "extrinsic", "extrinsic-zone root")
	flag.Parse()
	if *csvPath == "" || *vaultPath == "" {
		fmt.Fprintln(os.Stderr, "usage: goodreads-import -csv <export.csv> -vault <path> [-apply]")
		os.Exit(2)
	}

	rows, err := readCSV(*csvPath)
	if err != nil {
		fatal("reading CSV: %v", err)
	}
	// Read-only, throwaway in-memory index (DBPath "") so we never touch the
	// server's on-disk index.db while it may be running.
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: *vaultPath, SystemRoot: *systemRoot, ExtrinsicRoot: *extrinsicRoot})
	if err != nil {
		fatal("opening index: %v", err)
	}
	defer ix.Close()
	if _, err := ix.Rebuild(); err != nil {
		fatal("indexing vault: %v", err)
	}

	imp := &importer{ix: ix, vault: *vaultPath, extrinsicRoot: *extrinsicRoot}
	plan := imp.plan(rows)
	plan.report(os.Stdout, *apply)
	if !*apply {
		fmt.Println("\nDRY RUN — nothing written. Re-run with -apply to write.")
		return
	}
	if err := plan.write(imp); err != nil {
		fatal("writing: %v", err)
	}
	fmt.Printf("\nApplied: %d created, %d merged.\n", len(plan.creates), len(plan.merges))
}

// ---- CSV ----

type row map[string]string

func readCSV(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) < 2 {
		return nil, fmt.Errorf("empty CSV")
	}
	head := recs[0]
	var out []row
	for _, rec := range recs[1:] {
		m := row{}
		for i, h := range head {
			if i < len(rec) {
				m[h] = rec[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// ---- book record ----

type book struct {
	slug       string   // filename base (lowercase, subtitle/series stripped, sanitized)
	fullTitle  string   // original CSV title, kept when it differs from the slug title
	authors    []string // wikilink tokens, e.g. `"[[daniel kahneman]]"`
	status     string   // read | reading
	rating     int      // 0 = unrated (omitted)
	yearWr     string   // original pub year, fallback year published
	dateRead   string   // ISO, "" if none
	pages      string
	readCount  int
	reviewBody string // seeded into the body for the few rows with a review
}

var (
	seriesRe = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
	illegal  = regexp.MustCompile(`[/\\:*?"<>|]`)
	dateRe   = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2})`)
)

// buildBook turns one in-scope CSV row into a record (author links resolved).
func (im *importer) buildBook(r row) book {
	raw := strings.TrimSpace(r["Title"])
	title := raw
	if i := strings.Index(title, ":"); i >= 0 { // strip ": subtitle"
		title = title[:i]
	}
	title = seriesRe.ReplaceAllString(title, "") // strip trailing (series …)
	title = strings.TrimSpace(title)
	slug := strings.TrimSpace(illegal.ReplaceAllString(strings.ToLower(title), ""))

	b := book{slug: slug, status: shelfStatus(r["Exclusive Shelf"])}
	if strings.EqualFold(strings.TrimSpace(raw), title) == false && !strings.EqualFold(raw, slug) {
		b.fullTitle = raw // a subtitle/series was removed → preserve the original
	}
	if n := ratingOf(r["My Rating"]); n > 0 { // Goodreads writes "4.0", not "4"
		b.rating = n
	}
	b.yearWr = firstNonEmpty(r["Original Publication Year"], r["Year Published"])
	if m := dateRe.FindStringSubmatch(r["Date Read"]); m != nil {
		b.dateRead = m[1] + "-" + m[2] + "-" + m[3]
	}
	b.pages = strings.TrimSpace(r["Number of Pages"])
	if n := atoi(r["Read Count"]); n > 1 {
		b.readCount = n
	}
	b.reviewBody = strings.TrimSpace(r["My Review"])
	for _, a := range authorNames(r) {
		b.authors = append(b.authors, im.resolveAuthor(a))
	}
	return b
}

func shelfStatus(shelf string) string {
	if shelf == "currently-reading" {
		return "reading"
	}
	return "read"
}

// authorNames is the primary Author plus any Additional Authors, order preserved.
func authorNames(r row) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.Join(strings.Fields(s), " ") // trim + collapse internal whitespace
		if s != "" && !seen[strings.ToLower(s)] {
			seen[strings.ToLower(s)] = true
			out = append(out, s)
		}
	}
	add(r["Author"])
	for _, a := range strings.Split(r["Additional Authors"], ",") {
		add(a)
	}
	return out
}

// resolveAuthor returns the wikilink token for an author: an existing vault
// note's EXACT name when one exists, else the lowercased name. Never creates a
// note. Returns a quoted token so it renders as `authors: ["[[name]]"]`.
func (im *importer) resolveAuthor(name string) string {
	link := strings.ToLower(strings.TrimSpace(name))
	if e, ok := im.ix.Resolve(name); ok && e.HasNote {
		link = e.Display
		im.resolvedAuthors++
	} else {
		im.lowercasedAuthors++
	}
	return `"[[` + link + `]]"`
}

// frontmatter renders the record's fields in a stable order (only non-empty).
func (b book) frontmatter() []kv {
	fs := []kv{{"categories", "[books]"}}
	if len(b.authors) > 0 {
		fs = append(fs, kv{"authors", "[" + strings.Join(b.authors, ", ") + "]"})
	}
	fs = append(fs, kv{"status", b.status})
	if b.rating > 0 {
		fs = append(fs, kv{"rating", strconv.Itoa(b.rating)})
	}
	if b.yearWr != "" {
		fs = append(fs, kv{"year-written", b.yearWr})
	}
	if b.dateRead != "" {
		fs = append(fs, kv{"date-read", b.dateRead})
	}
	if b.pages != "" {
		fs = append(fs, kv{"pages", b.pages})
	}
	if b.readCount > 1 {
		fs = append(fs, kv{"read-count", strconv.Itoa(b.readCount)})
	}
	if b.fullTitle != "" {
		fs = append(fs, kv{"full-title", b.fullTitle})
	}
	return fs
}

type kv struct{ k, v string }

// newFileText builds a brand-new record file.
func (b book) newFileText() string {
	w := &mdfm.Writer{}
	for _, f := range b.frontmatter() {
		w.SetRaw(f.k, f.v)
	}
	body := "#book"
	if b.reviewBody != "" {
		body += "\n\n" + b.reviewBody
	}
	return w.String(body)
}

// ---- planning ----

type action struct {
	book     book
	existing string // vault-relative path of the note being merged, "" for a create
	moveTo   string // (merge only) new path when the note must move into extrinsic/, else ""
	addKeys  []kv   // (merge only) frontmatter keys to fill in
}

type plan struct {
	creates    []action
	merges     []action
	collisions []string // slug → multiple books, or a book colliding with a non-book note
	skips      []string
}

type importer struct {
	ix                *vaultindex.Index
	vault             string
	extrinsicRoot     string
	resolvedAuthors   int
	lowercasedAuthors int
}

// authorKeys are the frontmatter keys that already carry author info (either
// spelling) — if present, we never add a second one.
var authorKeys = map[string]bool{"author": true, "authors": true}

func (im *importer) plan(rows []row) plan {
	var p plan
	bySlug := map[string][]book{}
	var order []string
	for _, r := range rows {
		shelf := r["Exclusive Shelf"]
		if shelf != "read" && shelf != "currently-reading" {
			continue // drop to-read (and anything else)
		}
		b := im.buildBook(r)
		if b.slug == "" {
			p.skips = append(p.skips, "empty title: "+r["Title"])
			continue
		}
		if _, ok := bySlug[b.slug]; !ok {
			order = append(order, b.slug)
		}
		bySlug[b.slug] = append(bySlug[b.slug], b)
	}

	for _, slug := range order {
		books := bySlug[slug]
		if len(books) > 1 { // collision — disambiguate by primary-author surname
			for i := range books {
				books[i].slug = slug + " (" + surnameOf(books[i]) + ")"
			}
			p.collisions = append(p.collisions, fmt.Sprintf("%q → %d books, disambiguated by surname", slug, len(books)))
		}
		for _, b := range books {
			im.planOne(&p, b)
		}
	}
	return p
}

func (im *importer) planOne(p *plan, b book) {
	e, ok := im.ix.Resolve(b.slug)
	// slug taken by a NON-book note (a concept/person, possibly via alias — e.g.
	// "qed" → quantum-electrodynamics) → never merge into it; disambiguate the
	// book's filename by author surname and create, same rule as the two elon musks.
	if ok && e.HasNote && !im.isBookNote(e.NotePath) {
		orig := b.slug
		b.slug = orig + " (" + surnameOf(b) + ")"
		p.collisions = append(p.collisions, fmt.Sprintf("%q collides with non-book %q → %q", orig, e.NotePath, b.slug))
		e, ok = im.ix.Resolve(b.slug)
	}
	if ok && e.HasNote && im.isBookNote(e.NotePath) {
		add := im.missingKeys(e.NotePath, b)
		// a book note outside extrinsic/ (a couple live at the vault root) moves
		// into the zone; the basename is unchanged so [[links]] still resolve.
		moveTo := ""
		if im.ix.NoteZone(e.NotePath) != "extrinsic" {
			moveTo = filepath.ToSlash(filepath.Join(im.extrinsicRoot, filepath.Base(e.NotePath)))
		}
		if len(add) == 0 && moveTo == "" {
			p.skips = append(p.skips, b.slug+" (already complete)")
			return
		}
		p.merges = append(p.merges, action{book: b, existing: e.NotePath, moveTo: moveTo, addKeys: add})
		return
	}
	p.creates = append(p.creates, action{book: b})
}

func (im *importer) isBookNote(rel string) bool {
	return im.ix.NoteZone(rel) == "extrinsic" || hasBookCategory(im.ix, rel)
}

// missingKeys returns the record's frontmatter fields not already set on the
// existing note (author/authors treated as one slot; a field Benjamin set wins).
func (im *importer) missingKeys(rel string, b book) []kv {
	raw, err := os.ReadFile(filepath.Join(im.vault, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	fm, _ := mdfm.Split(string(raw))
	present := map[string]bool{}
	for k := range fm {
		present[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var add []kv
	for _, f := range b.frontmatter() {
		if f.k == "categories" {
			continue // existing book notes already carry it
		}
		if authorKeys[f.k] && (present["author"] || present["authors"]) {
			continue
		}
		if present[f.k] {
			continue
		}
		add = append(add, f)
	}
	return add
}

// ---- writing ----

func (p plan) write(im *importer) error {
	for _, a := range p.creates {
		rel := filepath.Join(im.extrinsicRoot, a.book.slug+".md")
		full := filepath.Join(im.vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(full); err == nil {
			continue // never clobber (idempotency belt)
		}
		if err := os.WriteFile(full, []byte(a.book.newFileText()), 0o644); err != nil {
			return err
		}
	}
	for _, a := range p.merges {
		full := filepath.Join(im.vault, filepath.FromSlash(a.existing))
		if err := mergeInto(full, a.addKeys); err != nil {
			return fmt.Errorf("merge %s: %w", a.existing, err)
		}
		if a.moveTo != "" { // relocate into extrinsic/ (basename unchanged → links hold)
			dst := filepath.Join(im.vault, filepath.FromSlash(a.moveTo))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Rename(full, dst); err != nil {
				return fmt.Errorf("move %s → %s: %w", a.existing, a.moveTo, err)
			}
		}
	}
	return nil
}

// mergeInto inserts add keys into an existing note's frontmatter block WITHOUT
// touching existing frontmatter lines or the body (byte-for-byte preserved).
func mergeInto(full string, add []kv) error {
	if len(add) == 0 {
		return nil
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	s := string(raw)
	var lines []string
	for _, f := range add {
		lines = append(lines, f.k+": "+f.v)
	}
	block := strings.Join(lines, "\n")
	if strings.HasPrefix(s, "---\n") {
		if end := strings.Index(s[4:], "\n---"); end >= 0 {
			cut := 4 + end // index of the '\n' before the closing ---
			out := s[:cut] + "\n" + block + s[cut:]
			return os.WriteFile(full, []byte(out), 0o644)
		}
	}
	// no frontmatter block → prepend one, body preserved verbatim
	out := "---\n" + block + "\n---\n\n" + strings.TrimLeft(s, "\n")
	return os.WriteFile(full, []byte(out), 0o644)
}

// ---- report ----

func (p plan) report(w *os.File, apply bool) {
	fmt.Fprintf(w, "== Goodreads import %s ==\n", ternary(apply, "(APPLY)", "(dry run)"))
	fmt.Fprintf(w, "creates: %d   merges: %d   collisions: %d   skipped: %d\n\n",
		len(p.creates), len(p.merges), len(p.collisions), len(p.skips))

	fmt.Fprintf(w, "-- CREATES (%d) --\n", len(p.creates))
	for _, a := range p.creates {
		fmt.Fprintf(w, "  + extrinsic/%s.md  [%s%s]  authors: %s\n",
			a.book.slug, a.book.status, ratingStr(a.book.rating), joinAuthors(a.book.authors))
	}
	fmt.Fprintf(w, "\n-- MERGES (%d) — body preserved, fields added --\n", len(p.merges))
	for _, a := range p.merges {
		var ks []string
		for _, k := range a.addKeys {
			ks = append(ks, k.k)
		}
		move := ""
		if a.moveTo != "" {
			move = "  → " + a.moveTo
		}
		fmt.Fprintf(w, "  ~ %s  += {%s}%s\n", a.existing, strings.Join(ks, ", "), move)
	}
	if len(p.collisions) > 0 {
		fmt.Fprintf(w, "\n-- COLLISIONS (%d) --\n", len(p.collisions))
		for _, c := range p.collisions {
			fmt.Fprintf(w, "  ! %s\n", c)
		}
	}
	if len(p.skips) > 0 {
		fmt.Fprintf(w, "\n-- SKIPPED (%d) --\n", len(p.skips))
		for _, s := range p.skips {
			fmt.Fprintf(w, "  · %s\n", s)
		}
	}
}

// ---- helpers ----

func hasBookCategory(ix *vaultindex.Index, rel string) bool {
	var n int
	_ = ix.DB().QueryRow(`SELECT COUNT(*) FROM note_categories WHERE path=? AND category IN ('books','book')`, rel).Scan(&n)
	return n > 0
}

// surnameOf derives a disambiguating surname from the primary author's link
// token (`"[[first last]]"` → "last"), for slug collisions like the two "elon
// musk" biographies (Isaacson vs Vance).
func surnameOf(b book) string {
	if len(b.authors) > 0 {
		parts := strings.Fields(strings.Trim(b.authors[0], `"[]`))
		if len(parts) > 0 {
			return strings.ToLower(parts[len(parts)-1])
		}
	}
	return "alt"
}

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

// ratingOf parses a Goodreads rating that may be written as a float ("4.0") or
// an int ("4"); "0"/"" mean unrated.
func ratingOf(s string) int {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	return atoi(s)
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

func joinAuthors(a []string) string {
	var out []string
	for _, s := range a {
		out = append(out, strings.Trim(s, `"[]`))
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func ratingStr(n int) string {
	if n <= 0 {
		return ""
	}
	return " ★" + strconv.Itoa(n)
}

func ternary(b bool, y, n string) string {
	if b {
		return y
	}
	return n
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
