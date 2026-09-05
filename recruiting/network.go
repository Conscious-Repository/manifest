package recruiting

import (
	"strings"

	"manifest/graph"
	"manifest/record"
)

var (
	networkPersonKeys = []string{"id", "name", "type", "email", "linkedin", "github",
		"org", "title", "source", "consent", "added",
		// archived: a connector is ARCHIVED, never deleted (the owner's rule,
		// 2026-09-05) · ref: the vault contact this person came from
		"archived", "ref"}
	edgeKeys = []string{"from", "to", "kind", "basis", "confidence", "inferred",
		"source", "evidence", "observed"}
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
		Archived: r.Get("archived"), Ref: r.Get("ref"),
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
		{"consent", p.Consent}, {"added", p.Added}, {"ref", p.Ref}} {
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
			Unknown: unknownFields(r, edgeKeys...),
		})
	}
	return out
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
	for _, f := range e.Unknown {
		r.Set(f.Key, f.Value)
	}
	d.Lines = append(d.Lines, Line{Row: r})
	return e, nil
}

// FormatConfidence renders a confidence-table constant the way the records
// carry it (two decimals, so 0.95 never reads as 0.9500000000000001).
func FormatConfidence(v float64) string { return graph.FormatConfidence(v) }
