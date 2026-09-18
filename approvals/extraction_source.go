package approvals

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckNoExtractionSource fails closed on unreadable receipts, including prior
// decisions. Used only before explicit pre-provider reconciliation.
func (s *Store) CheckNoExtractionSource(source string) error {
	s.decisionMu.Lock()
	defer s.decisionMu.Unlock()
	for _, status := range statuses {
		entries, err := os.ReadDir(filepath.Join(s.dir, status))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			p, err := s.parse(filepath.Join(s.dir, status, entry.Name()))
			if err != nil {
				return fmt.Errorf("unreadable proposal receipt")
			}
			if p.ExtractionSnapshot != "" {
				b, err := base64.RawURLEncoding.DecodeString(p.ExtractionSnapshot)
				var snap ExtractionSnapshot
				if err != nil || json.Unmarshal(b, &snap) != nil {
					return fmt.Errorf("unreadable extraction receipt")
				}
				if _, ok := snap.Files[source]; ok {
					return fmt.Errorf("extraction proposal receipt already exists")
				}
			}
			if p.Agent == "extractor" && strings.Contains(p.Body, source) {
				return fmt.Errorf("extractor output already exists")
			}
		}
	}
	return nil
}
