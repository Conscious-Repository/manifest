package realestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The .source.json sidecars are the LIVE canonical for the owner's public-site
// data (admin-portal plan): engine-shaped objects the re-portal proforma engine
// consumes. Manifest edits them patch-in-place — the client round-trips the
// full object, so unknown fields survive every save.

// SourceRel maps a record's .md path to its source sidecar path.
func SourceRel(mdRel string) string {
	return strings.TrimSuffix(mdRel, ".md") + ".source.json"
}

// Source reads a record's source sidecar (nil, false when absent/bad).
func (s *Service) Source(mdRel string) (json.RawMessage, bool) {
	full := filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(SourceRel(mdRel)))
	raw, err := os.ReadFile(full)
	if err != nil || !json.Valid(raw) {
		return nil, false
	}
	return json.RawMessage(raw), true
}

// PrettySource re-indents a source object for stable on-disk diffs.
func PrettySource(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}
