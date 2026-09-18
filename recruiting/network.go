package recruiting

import (
	"manifest/recruiting/sources"
	"strconv"
	"strings"

	"manifest/graph"
	"manifest/record"
)

var (
	networkPersonKeys = []string{"id", "name", "type", "email", "linkedin", "github",
		"org", "title", "source", "consent", "added", "source_ref", "orcid",
		// archived: a connector is ARCHIVED, never deleted (the owner's rule,
		// 2026-09-05) · ref: the vault contact this person came from
		"archived", "ref"}
	edgeKeys = []string{"from", "to", "kind", "basis", "confidence", "inferred",
		"source", "evidence", "observed", "work"}
)

func networkPersonRecognized(r *Row) bool { return r.Has("id") }
func edgeRecognized(r *Row) bool          { return r.Has("from") && r.Has("to") }

// ParseNetworkPeople reads network/people.md.
func ParseNetworkPeople(content string) *PeopleDoc {
	d := &PeopleDoc{}
	d.DocFM, d.Lines = parseRows(content, networkPersonRecognized)
	return d
}

// SerializeNetworkPeople is the fixpoint emitter for network/people.md.
func SerializeNetworkPeople(d *PeopleDoc) string { return serializeRows(d.DocFM, d.Lines) }

// People collects the rows in order.
func (d *PeopleDoc) People() []NetworkPerson {
	out := []NetworkPerson{}
	for _, ln := range d.Lines {
		if ln.Row == nil {
			continue
		}
		r := ln.Row
		out = append(out, personOf(r))
	}
	return out
}

// personOf is THE projection of one row — read by People() and by the editor
// in mutate.go, so a field added here reaches both without a second copy.
func personOf(r *Row) NetworkPerson {
	return NetworkPerson{
		ID: r.Get("id"), Name: r.Get("name"), Type: r.Get("type"),
		Email: r.Get("email"), LinkedIn: r.Get("linkedin"), GitHub: r.Get("github"),
		Org: r.Get("org"), Title: r.Get("title"), Source: r.Get("source"),
		Consent: r.Get("consent"), Added: r.Get("added"),
		Archived: r.Get("archived"), Ref: r.Get("ref"), SourceRef: r.Get("source_ref"), ORCID: r.Get("orcid"),
		Unknown: unknownFields(r, networkPersonKeys...),
	}
}

// Add appends one network node. `email` is only ever filled in by hand (D15);
// no adapter may set it, and the converter drops it if one tries.
func (d *PeopleDoc) Add(p NetworkPerson) (NetworkPerson, error) {
	if strings.TrimSpace(p.Name) == "" {
		return NetworkPerson{}, errf("a network person needs a name")
	}
	if p.Type != "" && !ValidPersonType(p.Type) {
		return NetworkPerson{}, errf("person type must be one of %s", strings.Join(PersonTypes, ", "))
	}
	if p.Consent != "" && !ValidConsent(p.Consent) {
		return NetworkPerson{}, errf("consent must be one of %s", strings.Join(ConsentKinds, ", "))
	}
	taken := map[string]bool{}
	for _, have := range d.People() {
		taken[have.ID] = true
	}
	if p.ID == "" {
		base := "aion-net/" + record.Slug(p.Name, 48)
		p.ID = base
		for n := 2; taken[p.ID]; n++ {
			p.ID = base + "-" + itoa(n)
		}
	}
	if taken[p.ID] {
		return NetworkPerson{}, errf("network person %q already exists", p.ID)
	}
	r := newRow("id", p.ID, "name", p.Name)
	for _, kv := range [][2]string{{"type", p.Type}, {"email", p.Email}, {"linkedin", p.LinkedIn},
		{"github", p.GitHub}, {"org", p.Org}, {"title", p.Title}, {"source", p.Source},
		{"consent", p.Consent}, {"added", p.Added}, {"ref", p.Ref}, {"source_ref", p.SourceRef}, {"orcid", p.ORCID}} {
		if kv[1] != "" {
			r.Set(kv[0], kv[1])
		}
	}
	for _, f := range p.Unknown {
		r.Set(f.Key, f.Value)
	}
	d.Lines = append(d.Lines, Line{Row: r})
	return p, nil
}

// ParseEdges reads network/edges.md.
func ParseEdges(content string) *EdgesDoc {
	d := &EdgesDoc{}
	d.DocFM, d.Lines = parseRows(content, edgeRecognized)
	return d
}

// SerializeEdges is the fixpoint emitter for network/edges.md. It is TOTAL by
// construction — a hand-edited row round-trips even when it is invalid, so
// the corpus heartbeat can never be broken by a bad edit. The refusal lives
// on the write path instead: ValidateEdges gates every save (Store.SaveEdges),
// so this package cannot PERSIST an edge without a basis.
func SerializeEdges(d *EdgesDoc) string { return serializeRows(d.DocFM, d.Lines) }

// Edges collects the rows in order.
func (d *EdgesDoc) Edges() []Edge {
	out := []Edge{}
	for _, ln := range d.Lines {
		if ln.Row == nil {
			continue
		}
		r := ln.Row
		out = append(out, Edge{
			From: r.Get("from"), To: r.Get("to"), Kind: r.Get("kind"),
			Basis: r.Get("basis"), Confidence: r.Get("confidence"),
			Inferred: boolField(r.Get("inferred")), Source: r.Get("source"),
			Evidence: r.Get("evidence"), Observed: r.Get("observed"),
			Works:   parseWorks(r.GetAll("work")),
			Unknown: unknownFields(r, edgeKeys...),
		})
	}
	return out
}

// A work rides on the row as `[work:: <ref>@<year>/<authors>]`, one key per
// work, so a row with three shared papers has three `work` fields — the
// record kernel's repeated-key shape, the same one a profile's websites use.
func formatWork(w sources.WorkRef) string {
	s := strings.TrimSpace(w.Ref)
	if w.Year > 0 || w.Authors > 0 {
		s += "@" + itoa(w.Year)
	}
	if w.Authors > 0 {
		s += "/" + itoa(w.Authors)
	}
	return s
}

func parseWorks(vals []string) []sources.WorkRef {
	var out []sources.WorkRef
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		w := sources.WorkRef{Ref: v}
		if i := strings.LastIndex(v, "@"); i > 0 {
			w.Ref = v[:i]
			rest := v[i+1:]
			if j := strings.Index(rest, "/"); j >= 0 {
				w.Authors, _ = strconv.Atoi(rest[j+1:])
				rest = rest[:j]
			}
			w.Year, _ = strconv.Atoi(rest)
		}
		out = append(out, w)
	}
	return out
}

// mergeWorks unions two work lists by ref, first-seen order.
func mergeWorks(have, add []sources.WorkRef) []sources.WorkRef {
	seen := map[string]bool{}
	out := append([]sources.WorkRef(nil), have...)
	for _, w := range have {
		seen[strings.TrimSpace(w.Ref)] = true
	}
	for _, w := range add {
		k := strings.TrimSpace(w.Ref)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, w)
	}
	return out
}

// Merge is Add for a claim that may already be on file. The same pair and
// kind from a second work ACCUMULATES: the works union, the confidence keeps
// the higher stated value, the observed date keeps the earlier one, and the
// basis stays as first written. Refusing the second claim — what saveDraft
// did before D-I — threw away exactly the fact that makes a tie strong.
// Returns the row as it now stands and whether an existing row absorbed it.
func (d *EdgesDoc) Merge(e Edge) (Edge, bool, error) {
	if err := ValidateEdge(e); err != nil {
		return Edge{}, false, err
	}
	want := edgeKey(e.From, e.To, e.Kind)
	for _, ln := range d.Lines {
		r := ln.Row
		if r == nil || edgeKey(r.Get("from"), r.Get("to"), r.Get("kind")) != want {
			continue
		}
		works := mergeWorks(parseWorks(r.GetAll("work")), e.Works)
		if len(works) > 0 {
			vals := make([]string, 0, len(works))
			for _, w := range works {
				vals = append(vals, formatWork(w))
			}
			r.SetAll("work", vals)
		}
		if c := strings.TrimSpace(e.Confidence); c != "" {
			if have := strings.TrimSpace(r.Get("confidence")); have == "" || parseConf(c) > parseConf(have) {
				r.Set("confidence", c)
			}
		}
		if o := strings.TrimSpace(e.Observed); o != "" {
			if have := strings.TrimSpace(r.Get("observed")); have == "" || o < have {
				r.Set("observed", o)
			}
		}
		if e.Evidence != "" && strings.TrimSpace(r.Get("evidence")) == "" {
			r.Set("evidence", e.Evidence)
		}
		return edgeOfRow(r), true, nil
	}
	out, err := d.Add(e)
	return out, false, err
}

func parseConf(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func edgeOfRow(r *Row) Edge {
	return Edge{
		From: r.Get("from"), To: r.Get("to"), Kind: r.Get("kind"),
		Basis: r.Get("basis"), Confidence: r.Get("confidence"),
		Inferred: boolField(r.Get("inferred")), Source: r.Get("source"),
		Evidence: r.Get("evidence"), Observed: r.Get("observed"),
		Works:   parseWorks(r.GetAll("work")),
		Unknown: unknownFields(r, edgeKeys...),
	}
}

// ValidateEdge is the "no claim without a basis" rule. An edge with no
// [basis::] is a bug: we would be asserting a relationship we cannot explain.
// The rule itself lives in graph.Validate (P2 lifted it out of here
// verbatim); this runs it over the recruiting vocabulary.
func ValidateEdge(e Edge) error {
	if err := graph.Validate(e.Graph(), EdgeVocabulary()); err != nil {
		return errf("%s", err.Error())
	}
	return nil
}

// ValidateEdges checks every row of the document.
func (d *EdgesDoc) Validate() error {
	for _, e := range d.Edges() {
		if err := ValidateEdge(e); err != nil {
			return errf("%s → %s: %w", e.From, e.To, err)
		}
	}
	return nil
}

// Add appends one edge claim. `inferred` is written explicitly — never
// present an inferred overlap as a real relationship, and that has to be a
// stored fact rather than a rendering habit.
func (d *EdgesDoc) Add(e Edge) (Edge, error) {
	if err := ValidateEdge(e); err != nil {
		return Edge{}, err
	}
	r := newRow("from", e.From, "to", e.To, "kind", e.Kind, "basis", e.Basis)
	if e.Confidence != "" {
		r.Set("confidence", e.Confidence)
	}
	r.Set("inferred", emitBool(e.Inferred))
	for _, kv := range [][2]string{{"source", e.Source}, {"evidence", e.Evidence}, {"observed", e.Observed}} {
		if kv[1] != "" {
			r.Set(kv[0], kv[1])
		}
	}
	if len(e.Works) > 0 {
		vals := make([]string, 0, len(e.Works))
		for _, w := range e.Works {
			vals = append(vals, formatWork(w))
		}
		r.SetAll("work", vals)
	}
	for _, f := range e.Unknown {
		r.Set(f.Key, f.Value)
	}
	d.Lines = append(d.Lines, Line{Row: r})
	return e, nil
}

// FormatConfidence renders a confidence-table constant the way the records
// carry it (two decimals, so 0.95 never reads as 0.9500000000000001).
func FormatConfidence(v float64) string { return graph.FormatConfidence(v) }
