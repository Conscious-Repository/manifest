package approvals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"manifest/mdfm"
)

// ConnectorInventory is a content-free checkpoint of the complete canonical
// inbox. Hash covers raw bytes, paths and decisions, including non-connectors.
// It is an observation, not a lock on the external legacy writer.
type ConnectorInventory struct {
	Hash  string              `json:"hash"`
	Items []ConnectorApproval `json:"items"`
}
type ConnectorApproval struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	SourceID string `json:"sourceId"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Path     string `json:"path"`
	Hash     string `json:"hash"`
}

// ReadConnectorInventory does not construct a Store (whose constructor creates
// directories). Missing, unreadable or conflicting history fails closed.
func ReadConnectorInventory(artifacts string) (ConnectorInventory, error) {
	return (&Store{dir: filepath.Join(artifacts, "approvals")}).connectorInventory()
}

// ConnectorSnapshot serializes with this Store's decisions and transcript publications.
// External legacy writers still require dispatch exclusion before handoff.
func (s *Store) ConnectorSnapshot() (ConnectorInventory, error) {
	s.decisionMu.Lock()
	defer s.decisionMu.Unlock()
	return s.connectorInventory()
}
func (s *Store) connectorInventory() (ConnectorInventory, error) {
	result := ConnectorInventory{Items: []ConnectorApproval{}}
	h := sha256.New()
	ids, sources := map[string]bool{}, map[string]bool{}
	for _, status := range statuses {
		entries, err := os.ReadDir(filepath.Join(s.dir, status))
		if err != nil {
			return result, fmt.Errorf("canonical approval inventory unavailable [REDACTED]")
		}
		for _, ent := range entries {
			if !strings.HasSuffix(ent.Name(), ".md") {
				continue
			}
			if !ent.Type().IsRegular() {
				return result, fmt.Errorf("non-regular approval artifact [REDACTED]")
			}
			path := filepath.Join(s.dir, status, ent.Name())
			b, err := os.ReadFile(path)
			if err != nil {
				return result, fmt.Errorf("approval unreadable [REDACTED]")
			}
			// Parse exactly the bytes hashed, never a second potentially changed read.
			fm, body := mdfm.Split(string(b))
			id := fm["id"]
			if id == "" || id+".md" != ent.Name() || ids[id] {
				return result, fmt.Errorf("duplicate or invalid canonical approval ID [REDACTED]")
			}
			ids[id] = true
			digest := sha256.Sum256(b)
			sum := hex.EncodeToString(digest[:])
			row, _ := json.Marshal([]string{status, ent.Name(), sum})
			h.Write(row)
			proposed, _ := mdfm.ExtractFencedBlock(body, "proposed")
			pf, _ := mdfm.Split(proposed)
			found := false
			for _, source := range []string{"granola", "pocket", "gmail-thread"} {
				sid := pf[source+"-id"]
				if old := pf[source+"_id"]; old != "" {
					if sid != "" && sid != old {
						return result, fmt.Errorf("conflicting source aliases [REDACTED]")
					}
					sid = old
				}
				if source == "gmail-thread" && fm["gmail-thread-id"] != "" {
					if sid != "" && sid != fm["gmail-thread-id"] {
						return result, fmt.Errorf("conflicting email source identity [REDACTED]")
					}
					sid = fm["gmail-thread-id"]
				}
				if sid == "" {
					continue
				}
				found = true
				typ := fm["type"]
				if typ != TypeCreateVaultNote && !(source == "gmail-thread" && typ == TypeAppendVaultNote) {
					return result, fmt.Errorf("invalid connector approval type [REDACTED]")
				}
				key := source + "/" + sid
				if typ == TypeCreateVaultNote {
					if sources[key] {
						return result, fmt.Errorf("duplicate connector source identity [REDACTED]")
					}
					sources[key] = true
				}
				result.Items = append(result.Items, ConnectorApproval{ID: id, Source: source, SourceID: sid, Status: status, Type: typ, Path: strings.TrimSpace(fm["apply-path"]), Hash: sum})
			}
			if !found && (fm["ritual"] == "granola-sync" || fm["ritual"] == "pocket-sync") {
				return result, fmt.Errorf("connector approval lacks source identity [REDACTED]")
			}
			if !found && fm["type"] == TypeCreateVaultNote {
				result.Items = append(result.Items, ConnectorApproval{ID: id, Status: status, Type: TypeCreateVaultNote, Path: strings.TrimSpace(fm["apply-path"]), Hash: sum})
			}
		}
	}
	result.Hash = hex.EncodeToString(h.Sum(nil))
	return result, nil
}
