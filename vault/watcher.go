package vault

import (
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"manifest/record"
)

// Watcher keeps an Index live by folding filesystem changes back into it —
// registered as a SINK on the kernel's single watch (record.Watch); the
// fsnotify plumbing lives there once. What stays here is this locator's
// POLICY: system-zone events are ignored (no dailies/goals there), bursts
// debounce at 300ms, creates/writes of .md files fold in incrementally while
// renames/removes (and new directories) trigger a full — still fast —
// Rebuild.
type Watcher struct {
	ix  *Index
	cfg Config

	mu      sync.Mutex
	timer   *time.Timer
	rebuild bool
	touched map[string]bool
}

// NewWatcher builds the sink and subscribes it to the shared watch.
func NewWatcher(ix *Index, cfg Config, w *record.Watch) *Watcher {
	s := &Watcher{ix: ix, cfg: cfg, touched: map[string]bool{}}
	w.Subscribe(s.onEvent)
	return s
}

// onEvent is the sink handler (watch-loop goroutine: bookkeeping only).
func (w *Watcher) onEvent(e record.Event) {
	if underSystemZone(w.cfg, e.Path) {
		return // engine churn: not watched for dailies/goals
	}
	w.mu.Lock()
	switch {
	case e.Op&(fsnotify.Rename|fsnotify.Remove) != 0:
		w.rebuild = true
	case e.DirCreated:
		w.rebuild = true
	case e.Op&(fsnotify.Create|fsnotify.Write) != 0 && strings.HasSuffix(e.Path, ".md"):
		w.touched[e.Path] = true
	default:
		w.mu.Unlock()
		return
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(300*time.Millisecond, w.flush)
	w.mu.Unlock()
}

func (w *Watcher) flush() {
	w.mu.Lock()
	rb := w.rebuild
	w.rebuild = false
	paths := w.touched
	w.touched = map[string]bool{}
	w.mu.Unlock()
	if rb {
		_ = w.ix.Rebuild()
		return
	}
	for p := range paths {
		w.ix.update(p)
	}
}
