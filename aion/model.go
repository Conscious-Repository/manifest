// Package aion is the AION domain declaration over the record kernel
// (ARCHITECTURE §3): seven system-zone corpora under <vault>/system/aion/ —
// people, vto, backlog, heuristics, hiring, references, finances — each a
// parse/serialize fixpoint pair built ONLY on kernel primitives. No parsing
// regexes, no serialization code of its own beyond composing record.*.
//
// Goals stay in the goals package (`## Aion` area) and are read-only from
// this domain. Every vault write goes through the injected write func (§A3
// boundary — main binds it to a vaultwriter capability).
package aion

import "manifest/record"

// Field is the kernel's inline-field pair.
type Field = record.Field

// DocFM is the shared frontmatter carrier: raw block lines (nil = no block)
// plus whether a blank line separated the fence from the body — preserved so
// parse→emit stays byte-identical (the kernel's SplitFrontmatter drops the
// blank; we recover it by length accounting in parseDocFM).
type DocFM struct {
	FM      []string `json:"-"`
	FMBlank bool     `json:"-"`
}

// ---- people.md ----

// Person is one registry row: `- [initials:: HZ] [name:: …] [role:: …]`.
type Person struct {
	Initials string  `json:"initials"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Email    string  `json:"email,omitempty"` // work email — drives portal sign-in → person attribution
	Unknown  []Field `json:"-"`               // unrecognized fields, round-tripped in place
}

// PeopleLine is one body line: a Person row or a verbatim line — the
// interleaved model keeps hand-edits (comments, headings) byte-stable.
type PeopleLine struct {
	Person *Person
	Raw    string
}

type PeopleDoc struct {
	DocFM
	Lines []PeopleLine
}

// People collects the registry rows in order.
func (d *PeopleDoc) People() []*Person {
	var out []*Person
	for _, ln := range d.Lines {
		if ln.Person != nil {
			out = append(out, ln.Person)
		}
	}
	return out
}

// ---- vto.md ----

// VTOEntry is one bullet inside a V/TO section: clean text + inline fields.
type VTOEntry struct {
	Text   string  `json:"text"`
	Fields []Field `json:"fields"`
}

// VTOLine is one section body line: an entry bullet or a verbatim line.
type VTOLine struct {
	Entry *VTOEntry
	Raw   string
}

// VTOSection is one `## NN …` block of the V/TO sheet.
type VTOSection struct {
	Heading string // full heading text after "## "
	Lines   []VTOLine
}

type VTODoc struct {
	DocFM
	Preamble []string // lines before the first section heading
	Sections []*VTOSection
}

// ---- backlog.md ----

// Backlog item kinds and statuses (spec §1/§2).
const (
	KindTask     = "task"
	KindDecision = "decision"

	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusDecided    = "decided"
)

// BacklogItem is one substrate line. Tasks are checkboxes; decisions are
// plain bullets. Kind is the [kind::] field; ID is derived (never persisted).
type BacklogItem struct {
	ID      string `json:"id"` // persisted aion-bl/<slug>; legacy rows use the old hash until migrated
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
	Kind    string `json:"kind"`

	// common
	Owner    string   `json:"owner"`
	Sources  []string `json:"sources"` // wikilink targets, brackets stripped
	Captured string   `json:"captured"`

	// task extras
	Rock   string `json:"rock"`
	Due    string `json:"due"`
	Status string `json:"status"`
	DoneOn string `json:"doneOn"`
	Rank   string `json:"rank,omitempty"` // [rank:: n] — unified-todos drag-to-rank (redesign stage 4)

	// decision extras
	NeededBy string `json:"neededBy"`
	Decided  string `json:"decided"`
	Outcome  string `json:"outcome"`

	Unknown  []Field        `json:"-"`
	Children []*BacklogItem `json:"children,omitempty"`
	// RawChildren are unclaimed indented lines under this item, verbatim,
	// emitted after the parsed children (canonical position).
	RawChildren []string `json:"-"`
	// Plain marks a plain-bullet line (no checkbox) — the owner's corpus
	// writes decisions BOTH ways; each line keeps its own style.
	Plain bool `json:"-"`
	// IDPersisted distinguishes a real [id::] token from the legacy derived
	// hash used while reading an unmigrated line. Reading an old file therefore
	// remains a byte-for-byte fixpoint.
	IDPersisted bool `json:"-"`
	// toks is the parsed field stream in source order — recognized values
	// re-render from struct state AT THEIR ORIGINAL POSITION, so hand-
	// authored field orderings round-trip byte-identically.
	toks []fieldTok
}

// BacklogLine is one section body line: an item or a verbatim line.
type BacklogLine struct {
	Item *BacklogItem
	Raw  string
}

// BacklogSection is one `## …` block (Tasks / Decisions by convention; the
// grammar does not enforce headings — kind lives on the item).
type BacklogSection struct {
	Heading string
	Lines   []BacklogLine
}

type BacklogDoc struct {
	DocFM
	Preamble []string
	Sections []*BacklogSection
}

// Items collects every top-level item across sections, in order.
func (d *BacklogDoc) Items() []*BacklogItem {
	var out []*BacklogItem
	for _, s := range d.Sections {
		for _, ln := range s.Lines {
			if ln.Item != nil {
				out = append(out, ln.Item)
			}
		}
	}
	return out
}

// AllItems includes nested checklist children for identity migration and
// collision detection. Active portal views continue to use top-level Items.
func (d *BacklogDoc) AllItems() []*BacklogItem {
	var out []*BacklogItem
	var walk func(*BacklogItem)
	walk = func(it *BacklogItem) {
		out = append(out, it)
		for _, child := range it.Children {
			walk(child)
		}
	}
	for _, it := range d.Items() {
		walk(it)
	}
	return out
}

// Find returns the top-level item with the given derived id (nil if absent).
func (d *BacklogDoc) Find(id string) *BacklogItem {
	for _, it := range d.AllItems() {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// ---- heuristics.md ----

// Reinforcement is one child line under a heuristic: `- [[note]] [date:: …]`.
type Reinforcement struct {
	Note string `json:"note"` // wikilink target, brackets stripped
	Date string `json:"date"`
}

// Heuristic is one living-synthesis statement line plus its reinforcements.
type Heuristic struct {
	ID      string          `json:"id"` // sha1("heuristic|"+normalized text)[:8]
	Text    string          `json:"text"`
	First   string          `json:"first"`
	Sources []Reinforcement `json:"sources"`
	Unknown []Field         `json:"-"`
	Raw     []string        `json:"-"` // unclaimed child lines, verbatim
	// ChildIndent is the reinforcement lines' verbatim indent (the owner's
	// corpus uses tabs) — preserved so round-trips stay byte-identical.
	ChildIndent string `json:"-"`
}

// HeuristicLine is one body line at the top level of a zone (live/retired):
// a heuristic entry or a verbatim line.
type HeuristicLine struct {
	Heuristic *Heuristic
	Raw       string
}

type HeuristicsDoc struct {
	DocFM
	Preamble []string        // lines before the first entry / retired heading
	Live     []HeuristicLine // authored order — curation is meaning
	// RetiredHeading is the verbatim `## retired` line ("" when absent).
	RetiredHeading string
	Retired        []HeuristicLine
}

// LiveEntries collects the live heuristics in order.
func (d *HeuristicsDoc) LiveEntries() []*Heuristic {
	var out []*Heuristic
	for _, ln := range d.Live {
		if ln.Heuristic != nil {
			out = append(out, ln.Heuristic)
		}
	}
	return out
}

// FindLive returns the live heuristic with the given id (nil if absent).
func (d *HeuristicsDoc) FindLive(id string) *Heuristic {
	for _, h := range d.LiveEntries() {
		if h.ID == id {
			return h
		}
	}
	return nil
}

// ---- hiring.md ----

// HiringItem is one pipeline row:
// `- [role:: …] [candidate:: …] [stage:: …] [next:: …] [priority:: 1]`.
type HiringItem struct {
	Role      string  `json:"role"`
	Candidate string  `json:"candidate"`
	Stage     string  `json:"stage"`
	Next      string  `json:"next"`
	Priority  string  `json:"priority"`
	Unknown   []Field `json:"-"`
}

type HiringLine struct {
	Item *HiringItem
	Raw  string
}

type HiringDoc struct {
	DocFM
	Lines []HiringLine
}

// ---- references.md ----

// Reference is one row: `- [url:: …] [source:: …] [date:: …]`.
type Reference struct {
	Text    string  `json:"text"`
	URL     string  `json:"url"`
	Source  string  `json:"source"`
	Date    string  `json:"date"`
	Unknown []Field `json:"-"`
}

type ReferenceLine struct {
	Ref *Reference
	Raw string
}

type ReferencesDoc struct {
	DocFM
	Lines []ReferenceLine
}

// ---- finances.md ----

// FinancesDoc is frontmatter-only data plus a private verbatim body. The
// body is never rendered in the UI and never exported (spec §2).
type FinancesDoc struct {
	DocFM
	Body string // verbatim, private
}

// Recognized finance keys (schema ready for a future bank poller).
var FinanceKeys = []string{"capital", "monthly_burn", "as_of", "currency", "source", "note"}
