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
// inbox, or one source when read with ReadConnectorInventoryForSource.
// Hash covers raw bytes, paths and decisions of the included artifacts.
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

// ReadConnectorInventoryForSource reads only approvals attributable to source.
// email uses gmail-thread identities. Unreadable artifacts and unattributed
// creates/appends fail closed because their source cannot safely be excluded.
// Known non-connector rituals without connector hints are excluded.
// Like the global reader, this never creates or changes approval history.
func ReadConnectorInventoryForSource(artifacts, source string) (ConnectorInventory, error) {
	if source == "email" {
		source = "gmail-thread"
	}
	switch source {
	case "gmail-thread", "granola", "pocket":
	default:
		return ConnectorInventory{}, fmt.Errorf("invalid connector inventory source")
	}
	return (&Store{dir: filepath.Join(artifacts, "approvals")}).connectorInventoryForSource(source)
}

// ConnectorSnapshot serializes with this Store's decisions and transcript publications.
// External legacy writers still require dispatch exclusion before handoff.
func (s *Store) ConnectorSnapshot() (ConnectorInventory, error) {
	s.decisionMu.Lock()
	defer s.decisionMu.Unlock()
	return s.connectorInventory()
}
func (s *Store) connectorInventory() (ConnectorInventory, error) {
	return s.connectorInventoryForSource("")
}
func (s *Store) connectorInventoryForSource(scope string) (ConnectorInventory, error) {
	return s.connectorInventoryWithReconciliation(scope, false)
}
func (s *Store) connectorInventoryWithReconciliation(scope string, owner bool) (ConnectorInventory, error) {
	return s.connectorInventoryMode(scope, owner, false)
}

// ReadEmailContinuityInventory preserves duplicate source claims for quarantine.
// Malformed identities and duplicate proposal IDs still fail closed. This grants
// no owner reconciliation or permission to apply an approval.
func ReadEmailContinuityInventory(artifacts string) (ConnectorInventory, error) {
	return (&Store{dir: filepath.Join(artifacts, "approvals")}).connectorInventoryMode("gmail-thread", false, true)
}

func (s *Store) connectorInventoryMode(scope string, owner, continuity bool) (ConnectorInventory, error) {
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
			proposed, _ := mdfm.ExtractFencedBlock(body, "proposed")
			pf, _ := mdfm.Split(proposed)
			if scope != "" {
				include, err := connectorArtifactInScope(string(b), proposed, fm, pf, scope)
				if err != nil {
					return result, err
				}
				if !include {
					continue
				}
			}
			id := fm["id"]
			if id == "" || id+".md" != ent.Name() || ids[id] {
				return result, fmt.Errorf("duplicate or invalid canonical approval ID [REDACTED]")
			}
			ids[id] = true
			digest := sha256.Sum256(b)
			sum := hex.EncodeToString(digest[:])
			row, _ := json.Marshal([]string{status, ent.Name(), sum})
			h.Write(row)
			found := false
			for _, source := range []string{"granola", "pocket", "gmail-thread"} {
				if scope != "" && source != scope {
					continue
				}
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
					if sources[key] && !continuity && !(owner && source == "gmail-thread" && sid == "19fdd282744d15a0" && status == "rejected" && (id == "2a71f54cd7b3" || id == "d75af2b821e6")) {
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

// Attribution uses both canonical and legacy aliases plus ritual metadata.
// Conflicting hints block every implicated source, never select a winner.
func connectorArtifactInScope(raw, proposed string, fm, pf map[string]string, scope string) (bool, error) {
	hints := map[string]bool{}
	identities := map[string]string{}
	invalid := false
	for _, source := range []string{"granola", "pocket", "gmail-thread"} {
		for _, fields := range []map[string]string{fm, pf} {
			for _, key := range []string{source + "-id", source + "_id"} {
				if sid, present := fields[key]; present {
					hints[source] = true
					if sid == "" || malformedIdentity(sid) || identities[source] != "" && identities[source] != sid {
						invalid = true
					}
					identities[source] = sid
				}
			}
		}
		if repeatedAlias(raw, source) || repeatedAlias(proposed, source) {
			invalid = true
		}
	}
	switch fm["ritual"] {
	case "granola-sync":
		hints["granola"] = true
	case "pocket-sync":
		hints["pocket"] = true
	case "email-sync", PersonalEmailRitual:
		hints["gmail-thread"] = true
	}
	if len(hints) == 0 {
		// Only explicitly known non-connector rituals establish exclusion.
		// Connector hints above take precedence, including invalid aliases.
		switch fm["ritual"] {
		case "delegate", "waiting-on":
			return false, nil
		}
		if fm["type"] == TypeCreateVaultNote || fm["type"] == TypeAppendVaultNote || fm["id"] == "" || fm["type"] == "" {
			return false, fmt.Errorf("approval source attribution unavailable [REDACTED]")
		}
		return false, nil
	}
	if !hints[scope] {
		return false, nil
	}
	if invalid || len(hints) != 1 || identities[scope] == "" {
		return false, fmt.Errorf("ambiguous or missing connector source identity [REDACTED]")
	}
	pf[scope+"-id"] = identities[scope]
	return true, nil
}
