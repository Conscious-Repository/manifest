package excaliburretire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"manifest/connectorhandoff"
)

type Receipt struct {
	Version  int      `json:"version"`
	PlanHash string   `json:"planSha256"`
	Engine   string   `json:"engine"`
	Paused   []string `json:"pausedCapabilities"`
	Replay   bool     `json:"replay"`
}

func controlDir(data string) string { return filepath.Join(data, "excalibur-decommission") }

// Retired requires the exact retained plan as well as the committed receipt.
// A partial attempt is unavailable/uncertain, never falsely reported retired.
func Retired(data string) bool {
	if data == "" {
		return false
	}
	b, e := os.ReadFile(filepath.Join(controlDir(data), "retired.json"))
	if e != nil {
		return false
	}
	var r Receipt
	if json.Unmarshal(b, &r) != nil || r.Version != 1 || r.Engine != "retired; unavailable" || r.Replay || !connectorhandoff.ValidHash(r.PlanHash) {
		return false
	}
	p, e := os.ReadFile(filepath.Join(controlDir(data), r.PlanHash+".json"))
	return e == nil && Hash(p) == r.PlanHash
}

// Unavailable also covers unreadable/corrupt retirement evidence. It blocks
// runtime controls without claiming that an incomplete receipt proves retirement.
func Unavailable(data string) bool {
	if data == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(controlDir(data), "retired.json"))
	return !os.IsNotExist(err)
}

// Apply has no data mutation callback: its only effect besides receipts is
// service retirement. All six shared fence locks span validation and stop/mask.
func Apply(c Config, m Manager, plan []byte, expected string) error {
	if !connectorhandoff.ValidHash(expected) || Hash(plan) != expected {
		return fmt.Errorf("plan hash mismatch")
	}
	if !filepath.IsAbs(c.Root) || !filepath.IsAbs(c.DataDir) {
		return fmt.Errorf("absolute paths required")
	}
	if Unavailable(c.DataDir) && !Retired(c.DataDir) {
		return fmt.Errorf("retirement evidence uncertain; inspect before applying")
	}
	dir := controlDir(c.DataDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("control directory unavailable or redirected")
	}
	fd, err := syscall.Open(filepath.Join(dir, "lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return fmt.Errorf("decommission busy")
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	releases := []func(){}
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	for _, duty := range Duties {
		// Never create a missing fence to make an uncertain state look complete.
		f, e := connectorhandoff.DutyFenceSnapshot(c.Root, duty)
		if e != nil || f.Revision == 0 {
			return fmt.Errorf("required fence unavailable")
		}
		info, e := os.Lstat(filepath.Join(c.Root, "vessel/state/dispatch-fence", duty, "lock"))
		if e != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("existing duty lock unavailable")
		}
		_, release, e := connectorhandoff.AcquireDutyFence(c.Root, duty)
		if e != nil {
			return fmt.Errorf("duty busy or uncertain")
		}
		releases = append(releases, release)
	}
	current := Build(c, m)
	if len(current.Blockers) != 0 {
		return fmt.Errorf("preconditions refused: %v", current.Blockers)
	}
	if Retired(c.DataDir) {
		b, e := os.ReadFile(filepath.Join(dir, "retired.json"))
		var r Receipt
		if e != nil || json.Unmarshal(b, &r) != nil || r.PlanHash != expected {
			return fmt.Errorf("different retirement already recorded")
		}
		var original Plan
		if json.Unmarshal(plan, &original) != nil || !preserved(original, current) {
			return fmt.Errorf("state or service drift since retirement")
		}
		for _, s := range current.Services {
			if s.Name == "excalibur-engine.service" && s.Active == "inactive" && s.Enabled == "masked" {
				return nil
			}
		}
		return fmt.Errorf("retirement receipt contradicts service state")
	}
	if !bytes.Equal(current.Bytes(), plan) {
		return fmt.Errorf("state or plan drift; generate and review a new plan")
	}
	if err := publish(filepath.Join(dir, expected+".json"), plan); err != nil {
		return err
	}
	// Persist intent first. Failure leaves this for diagnosis; never auto-restart.
	if err := publish(filepath.Join(dir, expected+".applying"), []byte(expected+"\n")); err != nil {
		return err
	}
	if err := m.Retire(); err != nil {
		return fmt.Errorf("retirement incomplete; preserve state and inspect service before retry: %w", err)
	}
	after := Build(c, m)
	if len(after.Blockers) != 0 {
		return fmt.Errorf("post-stop preconditions uncertain; no retired receipt")
	}
	// Bind all preserved data, including queues. Service state necessarily changes.
	if !preserved(current, after) {
		return fmt.Errorf("state changed during stop; service stays stopped, no retired receipt")
	}
	retired := false
	for _, s := range after.Services {
		if s.Name == "excalibur-engine.service" {
			retired = s.Active == "inactive" && s.Enabled == "masked"
		}
	}
	if !retired {
		return fmt.Errorf("engine not confirmed inactive and masked")
	}
	r := Receipt{Version: 1, PlanHash: expected, Engine: "retired; unavailable", Paused: append([]string{}, Paused...)}
	b, _ := json.MarshalIndent(r, "", "  ")
	return publish(filepath.Join(dir, "retired.json"), append(b, '\n'))
}

// Immutable publication is idempotent, fsynced, and never overwrites evidence.
func publish(path string, b []byte) error {
	if old, e := os.ReadFile(path); e == nil {
		if bytes.Equal(old, b) {
			return nil
		}
		return fmt.Errorf("existing receipt differs")
	} else if !os.IsNotExist(e) {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".receipt-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Link(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// Only the engine's availability and optional WantedBy links may change.
// Successor definitions/binaries and every inventoried byte remain bound.
func preserved(before, after Plan) bool {
	if before.Binding != after.Binding {
		return false
	}
	b, _ := json.Marshal(before.Inventories)
	a, _ := json.Marshal(after.Inventories)
	if !bytes.Equal(b, a) {
		return false
	}
	normalize := func(in []Service) []Service {
		out := append([]Service{}, in...)
		for i := range out {
			if out[i].Name == engineUnit {
				out[i].Active = ""
				out[i].Enabled = ""
				out[i].Consumers = 0
				out[i].ConsumersHash = ""
			}
		}
		return out
	}
	b, _ = json.Marshal(normalize(before.Services))
	a, _ = json.Marshal(normalize(after.Services))
	return bytes.Equal(b, a)
}
