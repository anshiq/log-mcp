// Config file watcher: validates on save, publishes revisions, and drives
// the reconcile planner. Valid → new revision + reconcile; invalid → keep
// the last good revision active and raise config.invalid with file/line/col.
package config

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Change describes one validated config file change.
type Change struct {
	Path     string
	Content  []byte
	SHA256   string
	Valid    bool
	Errors   []*ValidationError
	Warnings []string
}

// Watcher watches the effective config files for one workspace.
type Watcher struct {
	mu      sync.Mutex
	w       *fsnotify.Watcher
	paths   []string
	onValid func(Change)
	onError func(Change)
	done    chan struct{}
}

// NewWatcher creates a watcher over paths (missing files are watched via
// their parent dirs so creation is observed). Call Close when done.
func NewWatcher(paths []string, onValid, onError func(Change)) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	cw := &Watcher{w: w, onValid: onValid, onError: onError, done: make(chan struct{})}
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		dir := p
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			cw.paths = append(cw.paths, p)
		} else {
			dir = filepath.Dir(p)
		}
		if !seen[dir] {
			seen[dir] = true
			_ = w.Add(dir)
		}
	}
	go cw.loop()
	return cw, nil
}

func (w *Watcher) loop() {
	// Debounce: editors write temp+rename bursts; 300ms settles them.
	var timer *time.Timer
	var pending string
	emit := func() {
		path := pending
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		errs := Validate(data)
		ch := Change{Path: path, Content: data, SHA256: SHA256(data),
			Valid: len(errs) == 0, Errors: errs, Warnings: DeprecationWarnings(data)}
		if ch.Valid && w.onValid != nil {
			w.onValid(ch)
		} else if !ch.Valid && w.onError != nil {
			w.onError(ch)
		}
	}
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			for _, p := range w.paths {
				if ev.Name == p {
					pending = p
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(300*time.Millisecond, emit)
				}
			}
		case <-w.w.Errors:
		}
	}
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.done:
	default:
		close(w.done)
	}
	return w.w.Close()
}
