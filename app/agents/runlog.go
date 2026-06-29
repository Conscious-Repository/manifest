package agents

import (
	"os"
	"strings"
	"sync"
	"time"
)

// RunLog is an append-only audit trail. Nothing is ever truncated or rewritten;
// together with the vault's git history it is the system's undo log.
type RunLog struct {
	path string
	mu   sync.Mutex
}

func NewRunLog(path string) *RunLog { return &RunLog{path: path} }

// Append writes one line: "<rfc3339> <action> id=<id> k=v ...". Best-effort.
func (l *RunLog) Append(action, id string, kv ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	var b strings.Builder
	b.WriteString(time.Now().UTC().Format(time.RFC3339))
	b.WriteString(" " + action + " id=" + id)
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteString(" " + kv[i] + "=" + kv[i+1])
	}
	b.WriteString("\n")
	_, _ = f.WriteString(b.String())
}
