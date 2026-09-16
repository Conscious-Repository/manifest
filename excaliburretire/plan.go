// Package excaliburretire implements a read-only, hash-bound retirement plan.
// Retirement pauses capabilities; it makes no claim of extractor migration.
package excaliburretire

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"manifest/connectorhandoff"
	"manifest/mdfm"
	"manifest/personalemail"
	"manifest/transcriptsync"
)

var Duties = []string{"ea-coordinator/email-sync", "ea-coordinator/granola-sync", "ea-coordinator/pocket-sync", "extractor/aion", "extractor/ooda-email", "extractor/real-estate"}
var Paused = []string{"extractor/aion", "extractor/ooda-email", "extractor/real-estate"}

type Config struct{ Root, DataDir string }
type Entry struct {
	Kind     string `json:"kind"`
	PathHash string `json:"pathSha256"`
	SHA256   string `json:"sha256"`
	Bytes    int64  `json:"bytes"`
}
type Inventory struct {
	Label   string  `json:"label"`
	Files   int     `json:"files"`
	Bytes   int64   `json:"bytes"`
	SHA256  string  `json:"sha256"`
	Entries []Entry `json:"entries"`
}
type Service struct {
	Name           string `json:"name"`
	Active         string `json:"active"`
	Enabled        string `json:"enabled"`
	DefinitionHash string `json:"definitionSha256"`
	BinaryHash     string `json:"binarySha256"`
	ConsumersHash  string `json:"consumersSha256"`
	Consumers      int    `json:"consumers"`
}
type Lane struct {
	Duty           string `json:"duty"`
	Owner          string `json:"owner"`
	Revision       uint64 `json:"revision"`
	LegacyDisabled bool   `json:"legacyScheduleDisabled"`
	Target         string `json:"target"`
}
type Plan struct {
	Lanes       []Lane      `json:"lanes"`
	Version     int         `json:"version"`
	Binding     string      `json:"bindingSha256"`
	Engine      string      `json:"engine"`
	Paused      []string    `json:"pausedCapabilities"`
	Disposition string      `json:"legacyStateDisposition"`
	Replay      bool        `json:"replay"`
	Inventories []Inventory `json:"inventories"`
	Services    []Service   `json:"services"`
	Blockers    []string    `json:"blockers"`
}

func Hash(b []byte) string   { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func (p Plan) Bytes() []byte { b, _ := json.MarshalIndent(p, "", "  "); return append(b, '\n') }
func (p Plan) Hash() string  { return Hash(p.Bytes()) }

// InventoryTree never emits names or content. Missing roots, symlinks, special
// files and changing files are errors, not empty inventories. Relative path
// hashes bind renames as well as bytes. Caller explicitly handles optional roots.
func InventoryTree(label, root string) (Inventory, error) {
	for path := filepath.Clean(root); ; path = filepath.Dir(path) {
		st, e := os.Lstat(path)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return Inventory{Label: label}, fmt.Errorf("inventory path unavailable or redirected")
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	inv := Inventory{Label: label, Entries: []Entry{}}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("inventory unavailable")
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			inv.Entries = append(inv.Entries, Entry{Kind: "directory", PathHash: Hash([]byte(filepath.ToSlash(rel))), SHA256: Hash([]byte("directory"))})
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("inventory contains redirected or special state")
		}
		before, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("inventory unreadable")
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return fmt.Errorf("inventory changed during read")
		}
		rel, _ := filepath.Rel(root, path)
		inv.Entries = append(inv.Entries, Entry{"file", Hash([]byte(filepath.ToSlash(rel))), Hash(b), int64(len(b))})
		inv.Bytes += int64(len(b))
		inv.Files++
		return nil
	})
	sort.Slice(inv.Entries, func(i, j int) bool { return inv.Entries[i].PathHash < inv.Entries[j].PathHash })
	b, _ := json.Marshal(inv.Entries)
	inv.SHA256 = Hash(b)
	return inv, err
}

// Manager inspects the actual service manager, never a caller-supplied status file.
// Retire must preserve the original unit and verify inactive+masked before success.
type Manager interface {
	Snapshot() ([]Service, []string, error)
	Retire() error
}

func Build(c Config, m Manager) Plan {
	p := Plan{Version: 1, Binding: Hash([]byte(c.Root + "\x00" + c.DataDir)), Engine: "deprecated; retirement pending explicit apply", Paused: append([]string{}, Paused...), Disposition: "preserve all inventoried legacy bytes in place; queued and uncertain history held unavailable; no replay or deletion", Inventories: []Inventory{}, Services: []Service{}, Blockers: []string{}}
	block := func(s string) { p.Blockers = append(p.Blockers, s) }
	if !filepath.IsAbs(c.Root) || !filepath.IsAbs(c.DataDir) || c.Root == c.DataDir {
		block("explicit distinct absolute harness and data directories required")
		return p
	}
	for _, duty := range Duties {
		f, err := connectorhandoff.DutyFenceSnapshot(c.Root, duty)
		owner := "manifest"
		if strings.HasPrefix(duty, "extractor/") {
			owner = "blocked"
		}
		if err != nil || f.Revision == 0 || f.Owner != owner {
			block(duty + ": required ownership fence absent or invalid")
		}
		parts := strings.Split(duty, "/")
		b, err := os.ReadFile(filepath.Join(c.Root, "spirits", parts[0], "rituals", parts[1]+".md"))
		fm, _ := mdfm.Split(string(b))
		target := "Manifest successor-owned"
		if strings.HasPrefix(duty, "extractor/") {
			target = "paused/unavailable; no migration or parity claim"
		}
		observedOwner := f.Owner
		if observedOwner == "" {
			observedOwner = "unknown"
		}
		p.Lanes = append(p.Lanes, Lane{Duty: duty, Owner: observedOwner, Revision: f.Revision, LegacyDisabled: err == nil && fm["enabled"] == "false", Target: target})
		if err != nil || fm["enabled"] != "false" {
			block(duty + ": legacy schedule must be explicitly disabled")
		}
	}
	// Every remaining ritual is a potential consumer, even without a cadence.
	paths, err := filepath.Glob(filepath.Join(c.Root, "spirits", "*", "rituals", "*.md"))
	if err != nil || len(paths) == 0 {
		block("ritual inventory unavailable")
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		fm, _ := mdfm.Split(string(b))
		if err != nil || fm["enabled"] != "false" {
			block("remaining enabled or unreadable ritual: " + Hash([]byte(path)))
		}
	}
	if _, enabled, err := personalemail.OwnershipSnapshot(c.DataDir, c.Root); err != nil || !enabled {
		block("email successor activation/account binding unavailable or disabled")
	}
	var wc struct {
		DataDir        string                `json:"dataDir"`
		TranscriptSync transcriptsync.Config `json:"transcriptSync"`
	}
	b, err := os.ReadFile(filepath.Join(c.DataDir, "transcript-worker.json"))
	if err != nil || json.Unmarshal(b, &wc) != nil || wc.DataDir != c.DataDir || wc.TranscriptSync.LegacyRoot != c.Root {
		block("transcript worker configuration unavailable or mismatched")
	} else {
		svc := transcriptsync.New(c.DataDir, wc.TranscriptSync, nil, nil).WithHandoffGuard(c.DataDir)
		for _, source := range []string{"granola", "pocket"} {
			f, e1 := connectorhandoff.FenceSnapshot(c.Root, source)
			r, e2 := connectorhandoff.Read(c.DataDir, source)
			st, enabled, e3 := svc.OwnershipSnapshot(c.DataDir, c.Root, source, f, r)
			if e1 != nil || e2 != nil || e3 != nil || !enabled || st.Error != "" || st.LastSuccess.IsZero() || st.LastSuccess.Before(st.LastAttempt) {
				block(source + ": successor continuity/ownership unavailable or uncertain")
			}
		}
	}
	// Full preserved state, not only successful or finished rows. No private names
	// appear in receipts. Optional successor extractor state is explicitly bound.
	for _, v := range []struct {
		label, path string
		optional    bool
	}{
		{"legacy-state", filepath.Join(c.Root, "vessel", "state"), false},
		{"legacy-spool", filepath.Join(c.Root, "vessel", "spool"), false},
		{"legacy-artifacts", filepath.Join(c.Root, "artifacts"), false},
		{"legacy-spirits", filepath.Join(c.Root, "spirits"), false},
		{"connector-handoffs", filepath.Join(c.DataDir, "connector-handoff"), false},
		{"email-successor", filepath.Join(c.DataDir, "personal-email"), false},
		{"transcript-successor", filepath.Join(c.DataDir, "transcript-sync"), false},
		{"email-config", filepath.Join(c.DataDir, "personal-email-worker.json"), false},
		{"transcript-config", filepath.Join(c.DataDir, "transcript-worker.json"), false},
		{"extractor-successor", filepath.Join(c.DataDir, "domain-extraction"), true},
		{"aion-dispatch", filepath.Join(c.DataDir, "aion"), true},
		{"realestate-dispatch", filepath.Join(c.DataDir, "realestate"), true},
	} {
		if _, e := os.Lstat(v.path); v.optional && os.IsNotExist(e) {
			p.Inventories = append(p.Inventories, Inventory{Label: v.label, SHA256: Hash([]byte("absent")), Entries: []Entry{}})
			continue
		}
		inv, e := InventoryTree(v.label, v.path)
		p.Inventories = append(p.Inventories, inv)
		if e != nil {
			block(v.label + ": incomplete or uncertain inventory")
		}
	}
	// Unknown/manual chat consumers cannot be silently parked as extractor work.
	entries, e := os.ReadDir(filepath.Join(c.Root, "vessel", "spool"))
	if e != nil {
		block("spool unreadable")
	}
	for _, entry := range entries {
		b, e := os.ReadFile(filepath.Join(c.Root, "vessel", "spool", entry.Name()))
		var r struct{ Spirit, Ritual, Kind string }
		if e != nil || json.Unmarshal(b, &r) != nil || (r.Kind != "" && r.Kind != "run-now") || !connectorhandoff.Managed(r.Spirit+"/"+r.Ritual) {
			block("unaccounted or malformed queued consumer: " + Hash([]byte(entry.Name())))
		}
	}
	// In-flight non-fenced chat/other runs cannot be dismissed as historical data.
	for _, spec := range []struct {
		dir, key string
		terminal map[string]bool
	}{
		{"runs", "outcome", map[string]bool{"completed": true, "error": true, "error (protocol)": true, "stopped-steps": true, "stopped-charge": true, "interrupted": true}},
		{"chats", "status", map[string]bool{"idle": true, "error": true}},
	} {
		paths, _ := filepath.Glob(filepath.Join(c.Root, "artifacts", spec.dir, "*.md"))
		for _, path := range paths {
			b, e := os.ReadFile(path)
			fm, _ := mdfm.Split(string(b))
			if e != nil || !spec.terminal[fm[spec.key]] {
				block("active or uncertain engine consumer: " + Hash([]byte(path)))
			}
		}
	}
	services, blockers, e := m.Snapshot()
	p.Services = services
	p.Blockers = append(p.Blockers, blockers...)
	if e != nil {
		block("service manager inspection unavailable")
	}
	sort.Strings(p.Blockers)
	return p
}
