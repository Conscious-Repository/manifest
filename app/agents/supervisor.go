package agents

import (
	"context"
	"time"
)

// Supervisor is the sole orchestrator: it runs the startup + periodic crash-sweep
// that returns stale claims to inbox, so a dead worker's task is retried — and
// with the done/ idempotency set, executed effectively once. One supervisor, few
// workers; no agent spawns an agent. Workers (e.g. Hermes) operate the same files
// externally; the supervisor is the single owner of the sweep.
type Supervisor struct {
	q        *Queue
	timeout  time.Duration
	interval time.Duration
}

func NewSupervisor(q *Queue, timeout, interval time.Duration) *Supervisor {
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Supervisor{q: q, timeout: timeout, interval: interval}
}

// Run sweeps once immediately, then every interval until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) {
	s.q.Sweep(s.timeout)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.q.Sweep(s.timeout)
		}
	}
}
