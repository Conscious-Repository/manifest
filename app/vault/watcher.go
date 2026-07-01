package vault

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher keeps an Index live by folding filesystem changes back into it.
// fsnotify is non-recursive, so it watches every directory and adds watches for
// directories created later. Bursts are debounced; creates/writes do a cheap
// incremental update while renames/removes trigger a full (still fast) Rebuild.
type Watcher struct {
	ix  *Index
	cfg Config
	fsw *fsnotify.Watcher
}

func NewWatcher(ix *Index, cfg Config) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{ix: ix, cfg: cfg, fsw: fsw}, nil
}

// Start adds watches for every directory and processes events until ctx is done.
func (w *Watcher) Start(ctx context.Context) error {
	if err := w.addRecursive(w.cfg.Root); err != nil {
		return err
	}
	go w.loop(ctx)
	return nil
}

func (w *Watcher) Close() error { return w.fsw.Close() }

func (w *Watcher) addRecursive(root string) error {
	skip := make(map[string]bool, len(w.cfg.SkipDirs))
	for _, d := range w.cfg.SkipDirs {
		skip[d] = true
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root {
			name := d.Name()
			if skip[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
		}
		_ = w.fsw.Add(path)
		return nil
	})
}

func (w *Watcher) loop(ctx context.Context) {
	var (
		mu      sync.Mutex
		timer   *time.Timer
		rebuild bool
		touched = map[string]bool{}
	)
	flush := func() {
		mu.Lock()
		rb := rebuild
		rebuild = false
		paths := touched
		touched = map[string]bool{}
		mu.Unlock()
		if rb {
			_ = w.ix.Rebuild()
			return
		}
		for p := range paths {
			w.ix.update(p)
		}
	}
	schedule := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(300*time.Millisecond, flush)
	}
	for {
		select {
		case <-ctx.Done():
			w.fsw.Close()
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			mu.Lock()
			switch {
			case ev.Op&(fsnotify.Rename|fsnotify.Remove) != 0:
				rebuild = true
			case ev.Op&(fsnotify.Create|fsnotify.Write) != 0:
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					_ = w.fsw.Add(ev.Name) // watch a newly created directory
					rebuild = true
				} else if strings.HasSuffix(ev.Name, ".md") {
					touched[ev.Name] = true
				}
			}
			mu.Unlock()
			schedule()
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}
