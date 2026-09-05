package graph

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store reads and writes the two graph records under <vault>/<root>/
// (root = "system/graph"). Every write goes through the injected write func —
// main binds it to the narrow `graph` vaultwriter capability. This package
// never opens a vault file to write, and a nil writer fails loudly on save
// rather than silently succeeding.
type Store struct {
	vaultRoot string
	root      string // vault-relative, slash form
	write     func(abs string, data []byte) error
	vocab     Vocabulary
	mu        sync.Mutex
}

// File names.
const (
	EntitiesFile = "entities.md"
	EdgesFile    = "edges.md"
)

// SeedFiles are the write-once records Ensure lays down when absent.
var SeedFiles = map[string]string{
	EntitiesFile: "# entities\n\nRegistered graph nodes — one `[id::] [kind::]` row each. The object itself lives in its own store; this row says the graph may point at it.\n\n",
	EdgesFile:    "# edges\n\nRelationship claims, one row each. Every row needs a `[basis::]` and a `[source::]`; `[inferred:: true]` marks a derived overlap, never a stated fact.\n\n",
}

// NewStore builds the record store over the default vocabulary.
func NewStore(vaultRoot, root string, write func(string, []byte) error) *Store {
	if write == nil {
		write = func(string, []byte) error {
			return errors.New("graph: no vault writer injected (§A3 write boundary)")
		}
	}
	return &Store{vaultRoot: vaultRoot, root: filepath.ToSlash(root), write: write, vocab: Default()}
}

// UseVocabulary swaps the closed kind set (a domain composing its own).
func (s *Store) UseVocabulary(v Vocabulary) { s.vocab = v }

// Vocabulary is the kind set this store validates against.
func (s *Store) Vocabulary() Vocabulary { return s.vocab }

// Root returns the vault-relative record root.
func (s *Store) Root() string { return s.root }

// Rel returns the vault-relative slash path of a record.
func (s *Store) Rel(name string) string { return s.root + "/" + name }

// Path returns the absolute path of a record.
func (s *Store) Path(name string) string {
	return filepath.Join(s.vaultRoot, filepath.FromSlash(s.root), filepath.FromSlash(name))
}

func (s *Store) raw(name string) string {
	b, _ := os.ReadFile(s.Path(name))
	return string(b)
}

func (s *Store) save(name, content string) error {
	return s.write(s.Path(name), []byte(content))
}

// Ensure writes the write-once seeds for anything absent. It never overwrites
// an existing record — the vault is the source of truth, not this file.
func (s *Store) Ensure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range []string{EntitiesFile, EdgesFile} {
		if _, err := os.Stat(s.Path(name)); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := s.save(name, SeedFiles[name]); err != nil {
			return fmt.Errorf("seeding %s: %w", name, err)
		}
	}
	return nil
}

// LoadEntities / SaveEntities are the entities.md pair.
func (s *Store) LoadEntities() *EntitiesDoc { return ParseEntities(s.raw(EntitiesFile)) }
func (s *Store) SaveEntities(d *EntitiesDoc) error {
	for _, e := range d.Entities() {
		if err := ValidateEntity(e, s.vocab); err != nil {
			return fmt.Errorf("%s: %w", e.ID, err)
		}
	}
	return s.save(EntitiesFile, SerializeEntities(d))
}

// LoadEdges reads edges.md.
func (s *Store) LoadEdges() *EdgesDoc { return ParseEdges(s.raw(EdgesFile)) }

// SaveEdges validates before it writes: this package cannot persist an edge
// without a basis, a source, or a kind in the closed set.
func (s *Store) SaveEdges(d *EdgesDoc) error {
	if err := d.Validate(s.vocab); err != nil {
		return err
	}
	return s.save(EdgesFile, SerializeEdges(d))
}

// AddEdge appends one claim, idempotently: the same (from, to, kind) already
// on file is returned as-is with added=false, so a replayed registration is
// not a second claim. A second SOURCE for a known claim belongs in its
// evidence, not in a duplicate row.
func (s *Store) AddEdge(e Edge) (Edge, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.From, e.To = R(e.From.Kind, e.From.ID), R(e.To.Kind, e.To.ID)
	if err := Validate(e, s.vocab); err != nil {
		return Edge{}, false, err
	}
	doc := s.LoadEdges()
	if have, ok := doc.Find(e.Key()); ok {
		return have, false, nil
	}
	if _, err := doc.Add(e, s.vocab); err != nil {
		return Edge{}, false, err
	}
	if err := s.SaveEdges(doc); err != nil {
		return Edge{}, false, err
	}
	return e, true, nil
}

// AddEntity registers one node, idempotently by (kind, id).
func (s *Store) AddEntity(e Entity) (Entity, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ValidateEntity(e, s.vocab); err != nil {
		return Entity{}, false, err
	}
	doc := s.LoadEntities()
	if have, ok := doc.Find(e.AsRef()); ok {
		return have, false, nil
	}
	if _, err := doc.Add(e, s.vocab); err != nil {
		return Entity{}, false, err
	}
	if err := s.SaveEntities(doc); err != nil {
		return Entity{}, false, err
	}
	return e, true, nil
}

// Graph builds the traversal graph over the stored claims plus any DERIVED
// edges the caller projects from other stores (a task's [depends::] /
// [outputs::] / [inputs::] fields, an artifact's provenance) — the file
// holds what was stated; the projection adds what the objects already imply.
// A derived edge that duplicates a stored claim's key is dropped: the stated
// row wins.
func (s *Store) Graph(derived ...Edge) *Graph {
	return Build(Merge(s.LoadEdges().Edges(), derived), s.vocab)
}

// Merge appends the derived edges whose key no stored edge already claims.
func Merge(stored, derived []Edge) []Edge {
	have := map[string]bool{}
	out := append([]Edge(nil), stored...)
	for _, e := range stored {
		have[e.Key()] = true
	}
	for _, e := range derived {
		if have[e.Key()] {
			continue
		}
		have[e.Key()] = true
		out = append(out, e)
	}
	return out
}
