package approvals

// This journal is deliberately incapable of vault writes or approval settlement.
// Its JSON contract reserves committing/committed, but this implementation can
// only prepare -> aborted, or quarantine an interrupted/foreign record as uncertain.
import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// ExtractionDependency describes a predicate, not merely a file that existed.
// Identity binds a category namespace/index/artifact store to its generation;
// a content hash alone cannot identify which store supplied it.
type ExtractionDependency struct {
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	ExpectedAbsent bool   `json:"expectedAbsent"`
	Hash           string `json:"sha256,omitempty"`
	Identity       string `json:"identity,omitempty"`
}

// ExtractionImage embeds exact bytes (JSON base64). Known=false is explicitly
// unavailable evidence, never an empty file. Absent is distinct from empty bytes.
type ExtractionImage struct {
	Known  bool   `json:"known"`
	Absent bool   `json:"absent"`
	Hash   string `json:"sha256,omitempty"`
	Bytes  []byte `json:"bytes"`
}
type ExtractionWrite struct {
	Path       string          `json:"path"`
	Capability string          `json:"capability"`
	Before     ExtractionImage `json:"before"`
	After      ExtractionImage `json:"after"`
}
type ExtractionTransition struct {
	State  string    `json:"state"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}
type ExtractionTransaction struct {
	Feasibility          *ExtractionFeasibility `json:"feasibility,omitempty"`
	Version              int                    `json:"version"`
	ID                   string                 `json:"id"`
	Actor                string                 `json:"actor"`
	Capabilities         []string               `json:"capabilities"`
	ApprovalStore        string                 `json:"approvalStore"`
	ApprovalID           string                 `json:"approvalId"`
	ApprovalDigest       string                 `json:"approvalDigest"`
	ApprovalBytes        []byte                 `json:"approvalBytes"`
	Snapshot             string                 `json:"snapshot"`
	Dependencies         []ExtractionDependency `json:"dependencies"`
	DependenciesComplete bool                   `json:"dependenciesComplete"`
	Writes               []ExtractionWrite      `json:"writes"`
	WritesComplete       bool                   `json:"writesComplete"`
	Missing              []string               `json:"missingEvidence"`
	Replay               bool                   `json:"replay"`
	State                string                 `json:"state"`
	Transitions          []ExtractionTransition `json:"transitions"`
}

// WithExtractionJournal configures refusal receipts outside the vault. Call
// RecoverExtractionJournal before serving. No configuration keeps the hold.
func (s *Store) WithExtractionJournal(dataDir string) *Store {
	s.extractionDataDir = dataDir
	return s
}

// Lock order: approval decisionMu -> approval directory fence -> journal
// directory flock (nonblocking). Startup recovery takes only the journal lock.
// Never acquire vaultwriter, index, artifact, or audit locks from this journal.
func (s *Store) extractionJournal(fn func(string) error) error {
	if s.extractionDataDir == "" || s.vaultRoot == "" {
		return fmt.Errorf("journal not configured")
	}
	data, err := filepath.EvalSymlinks(s.extractionDataDir)
	if err != nil {
		return err
	}
	vault, err := filepath.EvalSymlinks(s.vaultRoot)
	if err != nil {
		return err
	}
	data, err = filepath.Abs(data)
	if err != nil {
		return err
	}
	vault, err = filepath.Abs(vault)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(vault, data)
	if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("journal dataDir must be outside vault")
	}
	dir := filepath.Join(data, "extraction-transactions")
	if err = os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("journal must be a real directory")
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	// Persist the journal directory entry as well as each record's rename.
	parent, err := os.Open(data)
	if err != nil {
		return err
	}
	err = parent.Sync()
	parent.Close()
	if err != nil {
		return err
	}
	return fn(dir)
}

func journalTransition(r *ExtractionTransaction, state, reason string) {
	r.State = state
	r.Transitions = append(r.Transitions, ExtractionTransition{state, time.Now().UTC(), reason})
}

// Sync bytes before rename and sync the containing directory afterwards. A
// failed sync is an error, never evidence of a durable refusal or commit.
func saveExtractionTransaction(dir string, r ExtractionTransaction) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".prepare-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, filepath.Join(dir, r.ID+".json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func readExtractionTransaction(dir, id string) (ExtractionTransaction, error) {
	var r ExtractionTransaction
	full := filepath.Join(dir, id+".json")
	fi, err := os.Lstat(full)
	if err != nil {
		return r, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > 8<<20 {
		return r, fmt.Errorf("invalid journal record")
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	if r.Version != 1 || r.ID != id || len(r.Transitions) == 0 || r.Transitions[len(r.Transitions)-1].State != r.State {
		return r, fmt.Errorf("invalid journal identity/state")
	}
	return r, nil
}

func recoverExtractionTransaction(dir string, r *ExtractionTransaction) error {
	// Even all-after file hashes cannot prove audit + decision settlement. This
	// phase never writes committing/committed; imported such records quarantine.
	if (r.State == "aborted" || r.State == "uncertain") && !r.Replay {
		return nil
	}
	r.Replay = false
	journalTransition(r, "uncertain", "interrupted or unsupported transaction; no rollback, replay or settlement; owner reconciliation required")
	return saveExtractionTransaction(dir, *r)
}

// RecoverExtractionJournal is repeatable and never opens the vault for writing.
// Malformed records remain untouched and return an error for operator review.
func (s *Store) RecoverExtractionJournal() error {
	return s.extractionJournal(func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".json")
			if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
				return fmt.Errorf("invalid transaction filename")
			}
			r, err := readExtractionTransaction(dir, id)
			if err != nil {
				return err
			}
			if err = recoverExtractionTransaction(dir, &r); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) journalExtractionRefusal(p Proposal, reason string) (string, error) {
	// Include the exact reviewed serialized proposal and snapshot, scoped to the
	// approval store. A changed review is a different receipt, never a retry.
	digest := EvidenceHash(serialize(p))
	id := EvidenceHash(s.dir + "\n" + p.ID + "\n" + digest)
	state := ""
	err := s.extractionJournal(func(dir string) error {
		old, err := readExtractionTransaction(dir, id)
		if err == nil {
			if old.ApprovalDigest != digest || old.Snapshot != p.ExtractionSnapshot {
				return fmt.Errorf("transaction identity conflict")
			}
			if err = recoverExtractionTransaction(dir, &old); err != nil {
				return err
			}
			state = old.State
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		cap := s.reCap
		if p.Type == TypeAionBacklog || p.Type == TypeAionResolve || p.Type == TypeAionHeuristic {
			cap = s.aionCap
		}
		feasibility := CheckExtractionCommitBoundary()
		r := ExtractionTransaction{Feasibility: &feasibility, Version: 1, ID: id, Actor: "approved-proposal", Capabilities: []string{cap}, ApprovalStore: s.dir, ApprovalID: p.ID, ApprovalDigest: digest, ApprovalBytes: []byte(serialize(p)), Snapshot: p.ExtractionSnapshot,
			Dependencies: []ExtractionDependency{}, Writes: []ExtractionWrite{}, Missing: []string{"expected-absent paths", "category namespace identity", "vault index identity/revision", "artifact store identity/revision", "complete dependency manifest", "exact capability-checked before/after write set", "atomic write/audit/approval settlement"}}
		var snap ExtractionSnapshot
		b, _ := base64.RawURLEncoding.DecodeString(p.ExtractionSnapshot)
		if json.Unmarshal(b, &snap) == nil {
			for name, hash := range snap.Files {
				r.Dependencies = append(r.Dependencies, ExtractionDependency{Kind: "unverified-v1", Path: name, Hash: hash})
			}
		}
		sort.Slice(r.Dependencies, func(i, j int) bool { return r.Dependencies[i].Path < r.Dependencies[j].Path })
		// Do not run legacy transforms to invent after images or expand a write set.
		if p.ApplyPath != "" {
			r.Writes = append(r.Writes, ExtractionWrite{Path: p.ApplyPath, Capability: cap})
		}
		journalTransition(&r, "prepare", "refusal preparation only; evidence incomplete; no commit authority")
		if err = saveExtractionTransaction(dir, r); err != nil {
			return err
		}
		journalTransition(&r, "aborted", reason)
		if err = saveExtractionTransaction(dir, r); err != nil {
			return err
		}
		state = r.State
		return nil
	})
	if err != nil {
		return "", err
	}
	return "transaction=" + id + " state=" + state + "; journal substrate available; transaction not committed; semantic review required; final decommission not established", nil
}
