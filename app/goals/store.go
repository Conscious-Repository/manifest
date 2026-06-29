package goals

import (
	"os"
	"path/filepath"

	"manifest/vault"
)

// Store reads and writes the single goals.md master file. The path is resolved
// through the vault Index (so a hand-moved goals.md is still found); a fallback
// at the vault root is used when none has been indexed yet.
type Store struct {
	idx      *vault.Index
	fallback string
}

func NewStore(idx *vault.Index, vaultRoot, goalsName string) *Store {
	if goalsName == "" {
		goalsName = "goals.md"
	}
	return &Store{idx: idx, fallback: filepath.Join(vaultRoot, goalsName)}
}

// Path returns the goals.md path: the indexed one, or the root fallback.
func (s *Store) Path() string {
	if p := s.idx.GoalsPath(); p != "" {
		return p
	}
	return s.fallback
}

// Load parses the current goals.md (an empty doc if the file is absent).
func (s *Store) Load() *Doc {
	b, _ := os.ReadFile(s.Path())
	return Parse(string(b))
}

func (s *Store) Save(d *Doc) error {
	p := s.Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(Serialize(d)), 0o644)
}

// Seed creates a starter goals.md with the standard life areas if none exists.
func (s *Store) Seed() error {
	if _, err := os.Stat(s.Path()); err == nil {
		return nil // already exists — never overwrite
	}
	return s.Save(seedDoc())
}

// HorizonForMe implements daily.GoalsProvider: open, owner==me goal texts.
func (s *Store) HorizonForMe(horizon string) []string {
	return s.Load().HorizonTextsForMe(Horizon(horizon))
}

func seedDoc() *Doc {
	area := func(name string, horizons bool) *Area {
		return &Area{Name: name, has90: horizons, has30: horizons}
	}
	return &Doc{
		preamble: "# Goals",
		Areas: []*Area{
			area("Aion", true),
			area("OODA Group", true),
			area("House", true),
			area("Personal", true),
			area("Sidequests", false),
		},
	}
}
