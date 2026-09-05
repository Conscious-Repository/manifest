package graph

import (
	"fmt"
	"strings"

	"manifest/record"
)

// The two row documents (record.ParseRows fixpoint — a hand-edited row
// round-trips byte-identically, recognized or not).
//
//	entities.md   - [id:: inbox/gutters] [kind:: task] [title:: …] [ref:: …] [source:: owner] [added:: 2026-09-05]
//	edges.md      - [from:: task:inbox/gutters] [to:: artifact:1f2e…] [kind:: produced] [basis:: …]
//	                [confidence:: 0.90] [inferred:: false] [source:: owner] [evidence:: …] [observed:: 2026-09-05]
//
// Serialize is TOTAL: an invalid hand-edited row still emits, so the corpus
// heartbeat can never be broken by a bad edit. The refusal is on the write
// path — Store.SaveEdges validates every row — so this package cannot
// PERSIST a claim without a basis (recruiting's rule, unchanged).

var (
	entityKeys = []string{"id", "kind", "title", "ref", "source", "added"}
	edgeKeys   = []string{"from", "to", "kind", "basis", "confidence", "inferred", "source", "evidence", "observed"}
)

func entityRecognized(r *record.Row) bool { return r.Has("id") && r.Has("kind") }
func edgeRecognized(r *record.Row) bool   { return r.Has("from") && r.Has("to") }

// EntitiesDoc is entities.md.
type EntitiesDoc struct {
	record.DocFM
	Lines []record.Line
}

// ParseEntities reads entities.md.
func ParseEntities(content string) *EntitiesDoc {
	d := &EntitiesDoc{}
	d.DocFM, d.Lines = record.ParseRows(content, entityRecognized)
	return d
}

// SerializeEntities is the fixpoint emitter for entities.md.
func SerializeEntities(d *EntitiesDoc) string { return record.SerializeRows(d.DocFM, d.Lines) }

// Entities collects the rows in order.
func (d *EntitiesDoc) Entities() []Entity {
	out := []Entity{}
	for _, r := range record.Rows(d.Lines) {
		out = append(out, Entity{
			ID: r.Get("id"), Kind: r.Get("kind"), Title: r.Get("title"), Ref: r.Get("ref"),
			Source: r.Get("source"), Added: r.Get("added"), Unknown: r.UnknownFields(entityKeys...),
		})
	}
	return out
}

// Find returns the registered entity behind a ref, if any.
func (d *EntitiesDoc) Find(ref Ref) (Entity, bool) {
	for _, e := range d.Entities() {
		if e.Kind == ref.Kind && e.ID == ref.ID {
			return e, true
		}
	}
	return Entity{}, false
}

// Add appends one registration. The same (kind, id) twice is refused: an
// entity is registered once and its object lives elsewhere.
func (d *EntitiesDoc) Add(e Entity, v Vocabulary) (Entity, error) {
	e.ID, e.Kind = strings.TrimSpace(e.ID), strings.TrimSpace(e.Kind)
	if err := ValidateEntity(e, v); err != nil {
		return Entity{}, err
	}
	if _, ok := d.Find(e.AsRef()); ok {
		return Entity{}, fmt.Errorf("entity %s is already registered", e.AsRef())
	}
	r := record.NewRow("id", e.ID, "kind", e.Kind)
	for _, kv := range [][2]string{{"title", e.Title}, {"ref", e.Ref}, {"source", e.Source}, {"added", e.Added}} {
		if strings.TrimSpace(kv[1]) != "" {
			r.Set(kv[0], strings.TrimSpace(kv[1]))
		}
	}
	for _, f := range e.Unknown {
		r.Set(f.Key, f.Value)
	}
	d.Lines = append(d.Lines, record.Line{Row: r})
	return e, nil
}

// EdgesDoc is edges.md.
type EdgesDoc struct {
	record.DocFM
	Lines []record.Line
}

// ParseEdges reads edges.md.
func ParseEdges(content string) *EdgesDoc {
	d := &EdgesDoc{}
	d.DocFM, d.Lines = record.ParseRows(content, edgeRecognized)
	return d
}

// SerializeEdges is the fixpoint emitter for edges.md (total by construction).
func SerializeEdges(d *EdgesDoc) string { return record.SerializeRows(d.DocFM, d.Lines) }

// Edges collects the rows in order.
func (d *EdgesDoc) Edges() []Edge {
	out := []Edge{}
	for _, r := range record.Rows(d.Lines) {
		out = append(out, EdgeFromRow(r))
	}
	return out
}

// EdgeFromRow reads one row's edge (the same projection Edges applies).
func EdgeFromRow(r *record.Row) Edge {
	return Edge{
		From: ParseRef(r.Get("from")), To: ParseRef(r.Get("to")), Kind: r.Get("kind"),
		Basis: r.Get("basis"), Confidence: r.Get("confidence"),
		Inferred: boolField(r.Get("inferred")), Source: r.Get("source"),
		Evidence: r.Get("evidence"), Observed: r.Get("observed"),
		Unknown: r.UnknownFields(edgeKeys...),
	}
}

// Find returns the stored claim with this key (from, to, kind), if any.
func (d *EdgesDoc) Find(key string) (Edge, bool) {
	for _, e := range d.Edges() {
		if e.Key() == key {
			return e, true
		}
	}
	return Edge{}, false
}

// Validate checks every row against the vocabulary.
func (d *EdgesDoc) Validate(v Vocabulary) error {
	for _, e := range d.Edges() {
		if err := Validate(e, v); err != nil {
			return fmt.Errorf("%s → %s: %w", e.From, e.To, err)
		}
	}
	return nil
}

// Add appends one edge claim after validating it. `inferred` is written
// explicitly — never present an inferred overlap as a real relationship, and
// that has to be a stored fact rather than a rendering habit.
func (d *EdgesDoc) Add(e Edge, v Vocabulary) (Edge, error) {
	if err := Validate(e, v); err != nil {
		return Edge{}, err
	}
	d.Lines = append(d.Lines, record.Line{Row: EdgeRow(e)})
	return e, nil
}

// EdgeRow renders an edge as its row (the emit order every writer shares).
func EdgeRow(e Edge) *record.Row {
	r := record.NewRow("from", e.From.String(), "to", e.To.String(), "kind", strings.TrimSpace(e.Kind), "basis", strings.TrimSpace(e.Basis))
	if c := strings.TrimSpace(e.Confidence); c != "" {
		r.Set("confidence", c)
	}
	r.Set("inferred", emitBool(e.Inferred))
	for _, kv := range [][2]string{{"source", e.Source}, {"evidence", e.Evidence}, {"observed", e.Observed}} {
		if strings.TrimSpace(kv[1]) != "" {
			r.Set(kv[0], strings.TrimSpace(kv[1]))
		}
	}
	for _, f := range e.Unknown {
		r.Set(f.Key, f.Value)
	}
	return r
}

func boolField(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "y", "1":
		return true
	}
	return false
}

func emitBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
