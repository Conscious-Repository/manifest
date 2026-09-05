// Package decisions is the DECISION LEDGER (manifest P3 Phase 1): the second
// of the three fundamental entities (tasks, decisions, heuristics — the aion
// triad) as a dedicated, cross-domain object rather than a tagged backlog
// line. It is the displacement of aion's KindDecision item — the same
// provenance/outcome/captured contract, with the fuller shape a decision
// needs to be reconstructible later:
//
//   - the decision itself (title, owner, status: open → deliberating →
//     decided → revisited), when it was captured / needed / decided / revisited;
//   - WHY — the rationale, as prose;
//   - EVIDENCE at the time — refs to the facts that were on the table (an
//     artifact id, a ledger object, a URL, a vault note);
//   - ALTERNATIVES considered, each with its tradeoff;
//   - the EXPECTED outcome, and later the ACTUAL outcome;
//   - DOWNSTREAM — the tasks and outcomes the decision affected;
//   - SOURCES — provenance wikilinks, the aion `[source:: [[note]]]` pattern.
//
// File-as-truth: one note per decision, <vault>/system/decisions/<id>.md, a
// frontmatter block of scalars over a body of `## …` sections. The note is
// the owner's to edit; Parse → Serialize is a byte-identical fixpoint for any
// note (an unknown section, an unknown frontmatter key, a stray line all
// survive in place), and a Set* mutation rewrites ONLY its own section. The
// id is the filename stem (the address, like an artifact's objects/<id>.json).
//
// This package is UI-agnostic and imports only the stdlib and the record
// kernel — no regex of its own (the kernel's field grammar and frontmatter
// fences are the only pattern code). Writes go through an injected write
// func (main binds the `decisions` vaultwriter capability). The ledger event
// for a create/decide/revisit is the SERVER's job (it owns the ledger), fed
// by Change; the graph edges a decision implies (evidence → decision,
// task → decision) are DERIVED by the server from the record, never written.
package decisions

import (
	"errors"
	"fmt"
	"strings"

	"manifest/record"
)

// Statuses — state, not narration. A decision is open (captured, not yet
// worked), deliberating (being worked), decided (a choice was made, Decided
// stamped), or revisited (its actual outcome was recorded against the
// expected one, Revisited stamped). A revisited decision is still decided;
// the status says the loop was closed.
const (
	StatusOpen         = "open"
	StatusDeliberating = "deliberating"
	StatusDecided      = "decided"
	StatusRevisited    = "revisited"
)

// Statuses is the closed set, in lifecycle order.
var Statuses = []string{StatusOpen, StatusDeliberating, StatusDecided, StatusRevisited}

// ValidStatus reports membership.
func ValidStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

// Link is one evidence or downstream bullet: a ref plus a note on it. Ref is
// `kind:id` (the graph/ledger wire form: artifact:1f2e…, task:inbox/x,
// heuristic:h1a2), a URL, or a vault note as `[[wikilink]]` (brackets kept —
// they ARE the ref's shape; a wikilink cannot sit inside a [ref::] field).
type Link struct {
	Ref  string `json:"ref"`
	Note string `json:"note,omitempty"`
}

// Alternative is one option that was considered and its tradeoff.
type Alternative struct {
	Option   string `json:"option"`
	Tradeoff string `json:"tradeoff,omitempty"`
}

// Decision is the ledger entity — the projection of one note.
type Decision struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Owner  string `json:"owner,omitempty"`
	Status string `json:"status"`

	// Outcome is WHAT was decided — aion's [outcome::], the resolution of the
	// question the title asks ("Pick the vendor" → "beta, 12-month contract").
	Outcome         string        `json:"outcome,omitempty"`
	Why             string        `json:"why,omitempty"`
	Evidence        []Link        `json:"evidence"`
	Alternatives    []Alternative `json:"alternatives"`
	ExpectedOutcome string        `json:"expectedOutcome,omitempty"`
	ActualOutcome   string        `json:"actualOutcome,omitempty"`
	Downstream      []Link        `json:"downstream"`

	Captured  string   `json:"captured,omitempty"`  // date the decision was recorded
	NeededBy  string   `json:"neededBy,omitempty"`  // date it must be made by
	Decided   string   `json:"decided,omitempty"`   // date it was made
	Revisited string   `json:"revisited,omitempty"` // date the actual outcome was recorded
	Sources   []string `json:"sources"`             // provenance wikilink targets, brackets stripped
	Source    string   `json:"source,omitempty"`    // who/what captured it: owner | agent:… | aion (a projected backlog item)

	// Unknown frontmatter keys, round-tripped (a hand-added scalar survives).
	Unknown []record.Field `json:"unknown,omitempty"`
}

// Ref is the entity's graph/ledger endpoint form, `decision:<id>`.
func (d Decision) Ref() string { return "decision:" + d.ID }

// Kind is the entity kind this package mints (graph.KindDecision, ledger.ObjDecision).
const Kind = "decision"

// Validate refuses a decision without a title or with a status outside the set.
func Validate(d Decision) error {
	if strings.TrimSpace(d.Title) == "" {
		return errors.New("a decision needs a title")
	}
	if d.Status != "" && !ValidStatus(d.Status) {
		return fmt.Errorf("status %q is not one of %s", d.Status, strings.Join(Statuses, "/"))
	}
	return nil
}

// ---- the note ----

// Section headings — matched case-insensitively on read, emitted lowercase
// (the vault's convention). The order is the canonical layout New lays down.
const (
	SecWhy          = "why"
	SecEvidence     = "evidence"
	SecAlternatives = "alternatives"
	SecExpected     = "expected outcome"
	SecActual       = "actual outcome"
	SecDownstream   = "downstream"
	SecSources      = "sources"
)

var sectionOrder = []string{SecWhy, SecEvidence, SecAlternatives, SecExpected, SecActual, SecDownstream, SecSources}

// Frontmatter keys the projection reads; anything else is Unknown.
var frontKeys = []string{"title", "owner", "status", "outcome", "captured", "needed-by", "decided", "revisited", "source"}

// Section is one `## heading` block: the heading text and its verbatim lines.
type Section struct {
	Heading string
	Lines   []string
}

// Doc is one decision note held at fixpoint: frontmatter carrier, the lines
// before the first heading, and the sections in order.
type Doc struct {
	record.DocFM
	Preamble []string
	Sections []*Section
}

// Parse reads a note. Every line lands somewhere verbatim: the preamble, or
// the open section. No line is interpreted here — projection is Decision().
func Parse(content string) *Doc {
	d := &Doc{}
	var body string
	d.DocFM, body = record.ParseDocFM(content)
	var cur *Section
	for _, line := range record.BodyLines(body) {
		if strings.HasPrefix(line, "## ") {
			cur = &Section{Heading: strings.TrimPrefix(line, "## ")}
			d.Sections = append(d.Sections, cur)
			continue
		}
		if cur == nil {
			d.Preamble = append(d.Preamble, line)
		} else {
			cur.Lines = append(cur.Lines, line)
		}
	}
	return d
}

// Serialize is the fixpoint emitter.
func Serialize(d *Doc) string {
	var out []string
	out = append(out, d.Preamble...)
	for _, s := range d.Sections {
		out = append(out, "## "+s.Heading)
		out = append(out, s.Lines...)
	}
	return d.Emit(record.JoinLines(out))
}

// Section finds a section by heading (case-insensitive, trimmed); nil if absent.
func (d *Doc) Section(heading string) *Section {
	for _, s := range d.Sections {
		if strings.EqualFold(strings.TrimSpace(s.Heading), heading) {
			return s
		}
	}
	return nil
}

// ensure returns the section, creating it at its canonical position when
// absent: after the last known section that precedes it in the layout, else
// at the end (an unknown section the owner added stays where it is).
func (d *Doc) ensure(heading string) *Section {
	if s := d.Section(heading); s != nil {
		return s
	}
	s := &Section{Heading: heading}
	rank := map[string]int{}
	for i, h := range sectionOrder {
		rank[h] = i
	}
	mine := rank[heading]
	at := -1
	for i, have := range d.Sections {
		r, known := rank[strings.ToLower(strings.TrimSpace(have.Heading))]
		if known && r < mine {
			at = i + 1 // right after the last known predecessor
		}
	}
	if at < 0 { // no predecessor: before the first known successor, else the end
		at = len(d.Sections)
		for i, have := range d.Sections {
			if r, known := rank[strings.ToLower(strings.TrimSpace(have.Heading))]; known && r > mine {
				at = i
				break
			}
		}
	}
	d.Sections = append(d.Sections[:at], append([]*Section{s}, d.Sections[at:]...)...)
	return s
}

// ---- projection (read) ----

// Decision projects the note. ID is not in the note (it is the filename);
// the store fills it.
func (d *Doc) Decision() Decision {
	dec := Decision{
		Title: d.Get("title"), Owner: d.Get("owner"), Status: strings.ToLower(d.Get("status")),
		Outcome: d.Get("outcome"), Captured: d.Get("captured"), NeededBy: d.Get("needed-by"), Decided: d.Get("decided"),
		Revisited: d.Get("revisited"), Source: d.Get("source"),
		Evidence: []Link{}, Alternatives: []Alternative{}, Downstream: []Link{}, Sources: []string{},
	}
	if dec.Status == "" {
		dec.Status = StatusOpen
	}
	for _, ln := range d.FM {
		k, v, ok := strings.Cut(ln, ":")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		known := false
		for _, fk := range frontKeys {
			if strings.EqualFold(strings.TrimSpace(k), fk) {
				known = true
				break
			}
		}
		if !known {
			dec.Unknown = append(dec.Unknown, record.Field{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		}
	}
	dec.Why = prose(d.Section(SecWhy))
	dec.ExpectedOutcome = prose(d.Section(SecExpected))
	dec.ActualOutcome = prose(d.Section(SecActual))
	dec.Evidence = links(d.Section(SecEvidence))
	dec.Downstream = links(d.Section(SecDownstream))
	dec.Alternatives = alternatives(d.Section(SecAlternatives))
	dec.Sources = wikilinks(d.Section(SecSources))
	return dec
}

// prose is a section's text: its lines joined, outer blank lines trimmed.
func prose(s *Section) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(strings.Join(s.Lines, "\n"))
}

// links reads a bullet list of refs: `- [ref:: kind:id] note` or
// `- [[note]] note`. Non-bullet lines are not links (they stay verbatim).
func links(s *Section) []Link {
	out := []Link{}
	if s == nil {
		return out
	}
	for _, line := range s.Lines {
		content, ok := record.BulletContent(line)
		if !ok {
			continue
		}
		if l, ok := parseLink(content); ok {
			out = append(out, l)
		}
	}
	return out
}

// parseLink reads one bullet's content as a Link.
func parseLink(content string) (Link, bool) {
	content = strings.TrimSpace(content)
	if target, rest, ok := leadingWikilink(content); ok {
		return Link{Ref: "[[" + target + "]]", Note: rest}, true
	}
	text, fields := record.ParseFields(content)
	for _, f := range fields {
		if strings.EqualFold(f.Key, "ref") && f.Value != "" {
			return Link{Ref: f.Value, Note: text}, true
		}
	}
	if text == "" {
		return Link{}, false
	}
	// a bare bullet: the first token is the ref when it has the kind:id shape,
	// else the whole line is a ref-less note (kept as Note only)
	head, rest, _ := strings.Cut(text, " ")
	if _, _, ok := strings.Cut(head, ":"); ok {
		return Link{Ref: head, Note: strings.TrimSpace(rest)}, true
	}
	return Link{Note: text}, true
}

// leadingWikilink splits `[[target]] rest`; ok=false when content does not
// open with a closed wikilink.
func leadingWikilink(content string) (target, rest string, ok bool) {
	if !strings.HasPrefix(content, "[[") {
		return "", "", false
	}
	end := strings.Index(content, "]]")
	if end < 0 {
		return "", "", false
	}
	target = strings.TrimSpace(content[2:end])
	if target == "" {
		return "", "", false
	}
	return target, strings.TrimSpace(content[end+2:]), true
}

// emitLink renders a bullet for a link (a wikilink ref stays a wikilink).
func emitLink(l Link) string {
	ref, note := strings.TrimSpace(l.Ref), strings.TrimSpace(l.Note)
	switch {
	case ref == "":
		return record.ComposeLine("- ", note, nil)
	case strings.HasPrefix(ref, "[[") && strings.HasSuffix(ref, "]]"):
		return record.ComposeLine("- ", strings.TrimSpace(ref+" "+note), nil)
	}
	return record.ComposeLine("- ", note, []string{record.EmitField("ref", record.StripBracket(ref, false))})
}

// alternatives reads `- option [tradeoff:: …]` bullets.
func alternatives(s *Section) []Alternative {
	out := []Alternative{}
	if s == nil {
		return out
	}
	for _, line := range s.Lines {
		content, ok := record.BulletContent(line)
		if !ok {
			continue
		}
		text, fields := record.ParseFields(content)
		a := Alternative{Option: text}
		for _, f := range fields {
			if strings.EqualFold(f.Key, "tradeoff") {
				a.Tradeoff = f.Value
			}
		}
		if a.Option == "" && a.Tradeoff == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func emitAlternative(a Alternative) string {
	var fields []string
	if t := strings.TrimSpace(a.Tradeoff); t != "" {
		fields = append(fields, record.EmitField("tradeoff", record.StripBracket(t, false)))
	}
	return record.ComposeLine("- ", record.StripBracket(a.Option, false), fields)
}

// wikilinks reads `- [[note]]` bullets (the aion reinforcement shape).
func wikilinks(s *Section) []string {
	out := []string{}
	if s == nil {
		return out
	}
	for _, line := range s.Lines {
		content, ok := record.BulletContent(line)
		if !ok {
			continue
		}
		if target, _, ok := leadingWikilink(strings.TrimSpace(content)); ok {
			out = append(out, target)
		}
	}
	return out
}

func emitWikilink(target string) string {
	target = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(target), "[["), "]]")
	return "- [[" + target + "]]"
}

// ---- mutation (write) — each setter rewrites only its own section ----

// SetScalar upserts one frontmatter key; an empty value drops the line so a
// cleared date does not linger as `decided:`.
func (d *Doc) SetScalar(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		var kept []string
		for _, ln := range d.FM {
			if k, _, ok := strings.Cut(ln, ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
				continue
			}
			kept = append(kept, ln)
		}
		d.FM = kept
		return
	}
	if d.FM == nil {
		d.FM, d.FMBlank = []string{}, true
	}
	d.Set(key, value)
}

// setProse replaces a prose section's lines (a trailing blank keeps the next
// heading separated, the layout every seed has).
func (d *Doc) setProse(heading, text string) {
	s := d.ensure(heading)
	s.Lines = []string{""}
	if t := strings.TrimSpace(text); t != "" {
		s.Lines = append(strings.Split(t, "\n"), "")
	}
}

// SetWhy / SetExpected / SetActual rewrite the prose sections.
func (d *Doc) SetWhy(text string)      { d.setProse(SecWhy, text) }
func (d *Doc) SetExpected(text string) { d.setProse(SecExpected, text) }
func (d *Doc) SetActual(text string)   { d.setProse(SecActual, text) }

func (d *Doc) setBullets(heading string, bullets []string) {
	s := d.ensure(heading)
	s.Lines = append(append([]string(nil), bullets...), "")
}

// SetEvidence / SetDownstream rewrite the link lists.
func (d *Doc) SetEvidence(ls []Link)   { d.setBullets(SecEvidence, emitLinks(ls)) }
func (d *Doc) SetDownstream(ls []Link) { d.setBullets(SecDownstream, emitLinks(ls)) }

func emitLinks(ls []Link) []string {
	var out []string
	for _, l := range ls {
		if strings.TrimSpace(l.Ref) == "" && strings.TrimSpace(l.Note) == "" {
			continue
		}
		out = append(out, emitLink(l))
	}
	return out
}

// SetAlternatives rewrites the alternatives list.
func (d *Doc) SetAlternatives(as []Alternative) {
	var out []string
	for _, a := range as {
		if strings.TrimSpace(a.Option) == "" && strings.TrimSpace(a.Tradeoff) == "" {
			continue
		}
		out = append(out, emitAlternative(a))
	}
	d.setBullets(SecAlternatives, out)
}

// SetSources rewrites the provenance wikilinks.
func (d *Doc) SetSources(targets []string) {
	var out []string
	for _, t := range targets {
		if strings.TrimSpace(t) != "" {
			out = append(out, emitWikilink(t))
		}
	}
	d.setBullets(SecSources, out)
}

// New lays down the canonical note for a decision: every scalar that is set,
// every section in layout order (empty ones too, so the owner sees the shape).
func New(dec Decision) *Doc {
	d := &Doc{}
	d.FM, d.FMBlank = []string{}, true
	for _, kv := range [][2]string{
		{"title", dec.Title}, {"owner", dec.Owner}, {"status", dec.Status}, {"outcome", dec.Outcome},
		{"captured", dec.Captured}, {"needed-by", dec.NeededBy}, {"decided", dec.Decided},
		{"revisited", dec.Revisited}, {"source", dec.Source},
	} {
		d.SetScalar(kv[0], kv[1])
	}
	for _, f := range dec.Unknown {
		d.SetScalar(f.Key, f.Value)
	}
	d.SetWhy(dec.Why)
	d.SetEvidence(dec.Evidence)
	d.SetAlternatives(dec.Alternatives)
	d.SetExpected(dec.ExpectedOutcome)
	d.SetActual(dec.ActualOutcome)
	d.SetDownstream(dec.Downstream)
	d.SetSources(dec.Sources)
	return d
}

// ---- refs ----

// RefKind splits a `kind:id` ref (the first colon); ok=false for a wikilink,
// a URL-less bare word, or an empty ref. A URL (`https://…`) reads as kind
// "https" — callers checking a vocabulary refuse it naturally.
func RefKind(ref string) (kind, id string, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "[[") {
		return "", "", false
	}
	kind, id, ok = strings.Cut(ref, ":")
	kind, id = strings.TrimSpace(kind), strings.TrimSpace(id)
	if !ok || kind == "" || id == "" {
		return "", "", false
	}
	return kind, id, true
}
