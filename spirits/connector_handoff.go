package spirits

import (
	"path/filepath"
	"strings"

	"manifest/connectorhandoff"
)

func (s *Store) WithConnectorHandoffs(dataDir string) *Store { s.migrationDataDir = dataDir; return s }
func (s *Store) connectorSource(spirit, ritual string) string {
	name := s.harnessName
	if name == "" {
		name = filepath.Base(filepath.Clean(s.root))
	}
	if name != "excalibur" || spirit != "ea-coordinator" || s.migrationDataDir == "" {
		return ""
	}
	switch ritual {
	case "email-sync", "granola-sync", "pocket-sync":
		return strings.TrimSuffix(ritual, "-sync")
	}
	return ""
}
func (s *Store) connectorDispatchGuard(spirit, ritual string) error {
	source := s.connectorSource(spirit, ritual)
	if source == "" {
		return nil
	}
	return connectorhandoff.LegacyAllowed(s.migrationDataDir, source)
}
