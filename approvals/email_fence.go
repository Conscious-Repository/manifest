package approvals

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// AcquireDecisionFence serializes canonical proposal/decision publication across
// processes with cutover snapshots. Locking the existing directory inode writes
// no artifact. Legacy proposal writers are separately excluded by dispatch fence.
// Lock order is Store.decisionMu, then this fence; cutover holds no Store mutex.
func AcquireDecisionFence(artifacts string) (func(), error) {
	f, err := os.Open(filepath.Join(artifacts, "approvals"))
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("canonical approval decisions busy: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
func (s *Store) decisionFence() (func(), error) { return AcquireDecisionFence(filepath.Dir(s.dir)) }
