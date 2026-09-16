// Adapted from Excalibur engine/internal/dispatchfence/fence.go at f9c1ea1.
// Keep the lock inode and strict history format compatible with that contract.
// The shared fence serializes connector duty execution and ownership changes.
// Its files belong to operational state, never connector cursors or the vault.
package connectorhandoff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const Legacy = "excalibur"

// RecordFence is an immutable ownership revision. Evidence is an owner-supplied
// reconciliation receipt hash, not a claim that the engine verified reconciliation.
type RecordFence struct {
	Version       int    `json:"version"`
	Revision      uint64 `json:"revision"`
	Duty          string `json:"duty"`
	PreviousOwner string `json:"previous_owner"`
	Owner         string `json:"owner"`
	Action        string `json:"action"`
	Evidence      string `json:"evidence_sha256"`
	At            string `json:"at"`
}

func Managed(duty string) bool {
	switch duty {
	case "ea-coordinator/granola-sync", "ea-coordinator/pocket-sync", "ea-coordinator/email-sync",
		"extractor/aion", "extractor/real-estate", "extractor/ooda-email":
		return true
	}
	return false
}
func directory(root, duty string) string {
	return filepath.Join(root, "vessel", "state", "dispatch-fence", duty)
}

// State components must be real directories; a redirected/missing record must
// not turn an explicit transfer into the absent-record legacy default.
func ensureDirectory(root, duty string) error {
	if !Managed(duty) {
		return fmt.Errorf("unsupported duty")
	}
	parts := []string{"vessel", "state", "dispatch-fence"}
	parts = append(parts, strings.Split(duty, "/")...)
	dir := root
	for _, part := range parts {
		parent := dir
		dir = filepath.Join(dir, part)
		err := os.Mkdir(dir, 0700)
		if err != nil && !os.IsExist(err) {
			return err
		}
		if err == nil {
			// Persist newly created path components before ownership can publish.
			f, err := os.Open(parent)
			if err != nil {
				return err
			}
			err = f.Sync()
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ownership state path is not a real directory")
		}
	}
	return nil
}

func ownerOK(owner string) bool { return owner == Legacy || owner == "manifest" || owner == "blocked" }

var hashRE = regexp.MustCompile(`^[a-f0-9]{64}$`)

// lock files must never be replaced or removed; all participants use flock on
// this same inode. Nonblocking acquisition keeps a busy duty off the scheduler.
func lock(root, duty string) (*os.File, error) {
	dir := directory(root, duty)
	if err := ensureDirectory(root, duty); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(dir, "lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "dispatch-fence-lock")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("duty busy or lock unavailable")
	}
	return f, nil
}
func unlock(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }

// read validates the entire contiguous history, including duplicate JSON keys.
// No malformed, unknown, missing intermediate, or partially published revision
// may cause fallback to the implicit revision zero.
func readFence(root, duty string) (RecordFence, error) {
	current := RecordFence{Owner: Legacy, Duty: duty}
	entries, err := os.ReadDir(directory(root, duty))
	if err != nil {
		return current, err
	}
	for _, e := range entries {
		if e.Name() == "lock" || e.Name() == "refusals.jsonl" || len(e.Name()) >= 5 && e.Name()[:5] == ".tmp-" {
			continue
		}
		next := current.Revision + 1
		if e.Name() != fmt.Sprintf("%020d.json", next) || !e.Type().IsRegular() {
			return current, fmt.Errorf("invalid ownership history")
		}
		info, err := e.Info()
		if err != nil || info.Size() > 4096 {
			return current, fmt.Errorf("unreadable or oversized ownership revision")
		}
		b, err := os.ReadFile(filepath.Join(directory(root, duty), e.Name()))
		if err != nil {
			return current, fmt.Errorf("unreadable ownership revision")
		}
		var fields map[string]json.RawMessage
		dec := json.NewDecoder(bytes.NewReader(b))
		tok, err := dec.Token()
		if err != nil || tok != json.Delim('{') {
			return current, fmt.Errorf("invalid ownership JSON")
		}
		fields = map[string]json.RawMessage{}
		for dec.More() {
			tok, err = dec.Token()
			if err != nil {
				return current, fmt.Errorf("invalid ownership JSON")
			}
			key, ok := tok.(string)
			if !ok {
				return current, fmt.Errorf("invalid ownership key")
			}
			switch key {
			case "version", "revision", "duty", "previous_owner", "owner", "action", "evidence_sha256", "at":
			default:
				return current, fmt.Errorf("unknown ownership key")
			}
			if _, exists := fields[key]; exists {
				return current, fmt.Errorf("duplicate ownership key")
			}
			var v json.RawMessage
			if dec.Decode(&v) != nil {
				return current, fmt.Errorf("invalid ownership value")
			}
			fields[key] = v
		}
		if _, err = dec.Token(); err != nil {
			return current, fmt.Errorf("truncated ownership JSON")
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return current, fmt.Errorf("trailing ownership JSON")
		}
		var r RecordFence
		dec = json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if dec.Decode(&r) != nil || len(fields) != 8 || r.Version != 1 || r.Revision != next || r.Duty != duty || r.PreviousOwner != current.Owner || r.Owner == current.Owner || !ownerOK(r.Owner) || !hashRE.MatchString(r.Evidence) {
			return current, fmt.Errorf("invalid ownership record/version")
		}
		if _, err := time.Parse(time.RFC3339Nano, r.At); err != nil {
			return current, fmt.Errorf("invalid ownership timestamp")
		}
		if r.Action != action(r.Owner) {
			return current, fmt.Errorf("invalid ownership action")
		}
		current = r
	}
	return current, nil
}
func action(owner string) string {
	if owner == Legacy {
		return "rollback"
	}
	if owner == "blocked" {
		return "pause"
	}
	return "transfer"
}

// FenceSnapshot reads existing ownership without creating operational files.
func FenceSnapshot(root, source string) (RecordFence, error) {
	if source != "granola" && source != "pocket" && source != "email" {
		return RecordFence{}, fmt.Errorf("unsupported connector source")
	}
	return DutyFenceSnapshot(root, "ea-coordinator/"+source+"-sync")
}

// DutyFenceSnapshot reads a canonical duty without creating state. Extractor
// support here does not establish support in the deployed legacy engine.
func DutyFenceSnapshot(root, duty string) (RecordFence, error) {
	if !Managed(duty) || root == "" {
		return RecordFence{}, fmt.Errorf("unsupported duty or missing harness")
	}
	dir := root
	for _, part := range append([]string{"vessel", "state", "dispatch-fence"}, strings.Split(duty, "/")...) {
		dir = filepath.Join(dir, part)
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			return RecordFence{Owner: Legacy, Duty: duty}, nil
		}
		if err != nil {
			return RecordFence{}, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return RecordFence{}, fmt.Errorf("ownership state path is not a real directory")
		}
	}
	return readFence(root, duty)
}

// AcquireFence holds the same inode as legacy dispatch and owner publication.
// The caller must retain release through all successor effects and state writes.
func AcquireFence(root, source string) (RecordFence, func(), error) {
	if source != "granola" && source != "pocket" && source != "email" {
		return RecordFence{}, nil, fmt.Errorf("unsupported connector source")
	}
	return AcquireDutyFence(root, "ea-coordinator/"+source+"-sync")
}

// AcquireDutyFence retains the shared inode through execution and publication.
func AcquireDutyFence(root, duty string) (RecordFence, func(), error) {
	if !Managed(duty) || root == "" {
		return RecordFence{}, nil, fmt.Errorf("unsupported duty or missing harness")
	}
	f, err := lock(root, duty)
	if err != nil {
		return RecordFence{}, nil, err
	}
	r, err := readFence(root, duty)
	if err != nil {
		unlock(f)
		return r, nil, err
	}
	return r, func() { unlock(f) }, nil
}
