package record

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watch is THE vault filesystem watcher: one fsnotify watcher over the root,
// recursive registration under the standard dir-skip policy, new directories
// joining the watch, and raw-event fan-out to N subscribed sinks. What lives
// here is the mechanical shell both index engines used to duplicate; each
// sink keeps its own POLICY — debounce, event filtering, zone rules, and
// flush shape (rebuild-vs-incremental) are domain declarations, exactly like
// outline depth policies.
//
// Sink handlers run on the watch loop goroutine: do only cheap bookkeeping
// (set inserts, timer resets) and flush from your own timer.
type Watch struct {
	root string
	skip map[string]bool
	fsw  *fsnotify.Watcher

	mu    sync.Mutex
	sinks []func(Event)
}

// Event is one raw filesystem event. DirCreated marks a Create whose path is
// a directory that passed the skip policy — the watch has ALREADY registered
// it; sinks only decide what the new directory means for their projection.
type Event struct {
	Path       string // absolute
	Op         fsnotify.Op
	DirCreated bool
}

// NewWatch builds the watcher (no directories registered yet — Start does).
func NewWatch(root string, skipDirs []string) (*Watch, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watch{root: root, skip: SkipSet(skipDirs), fsw: fsw}, nil
}

// Subscribe registers a sink. Call before Start (registration is not
// synchronized against a running loop's fan-out slice).
func (w *Watch) Subscribe(fn func(Event)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sinks = append(w.sinks, fn)
}

// Start registers every directory under root (union policy: dot-dirs + the
// skip set excluded, every zone included — sinks narrow by filtering events,
// so one descriptor set serves all projections) and processes events until
// ctx is done.
func (w *Watch) Start(ctx context.Context) error {
	WalkDirs(w.root, w.skip, nil, func(abs string) { _ = w.fsw.Add(abs) })
	go w.loop(ctx)
	return nil
}

func (w *Watch) Close() error { return w.fsw.Close() }

func (w *Watch) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			w.fsw.Close()
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			e := Event{Path: ev.Name, Op: ev.Op}
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					base := filepath.Base(ev.Name)
					if !strings.HasPrefix(base, ".") && !w.skip[base] {
						_ = w.fsw.Add(ev.Name) // a new directory joins the watch
						e.DirCreated = true
					} else {
						continue // skipped dir: never watched, never fanned out
					}
				}
			}
			for _, s := range w.sinks {
				s(e)
			}
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}
