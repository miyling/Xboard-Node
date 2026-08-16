package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cedar2025/xboard-node/internal/nlog"
	"github.com/fsnotify/fsnotify"
)

// Watcher reloads the config file on change (debounced) and calls onChange.
type Watcher struct {
	path         string
	debounce     time.Duration
	onChange     func(*Config)
	onChangeRoot func(*RootConfig)
	watcher      *fsnotify.Watcher

	stopOnce sync.Once
	stopCh   chan struct{}
	stopped  atomic.Bool
}

// WatchConfig watches path; invalid reloads are logged and ignored.
// Stop via Stop() or cancel ctx.
func WatchConfig(ctx context.Context, path string, onChange func(*Config)) (*Watcher, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Watch parent dir so rename/atomic saves are visible.
	dir := filepath.Dir(absPath)
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &Watcher{
		path:     absPath,
		debounce: 1 * time.Second,
		onChange: onChange,
		watcher:  fsw,
		stopCh:   make(chan struct{}),
	}

	go w.loop(ctx)
	nlog.Core().Info("config watcher started", "path", absPath)
	return w, nil
}

// WatchConfigRoot watches path and reloads the root config model.
func WatchConfigRoot(ctx context.Context, path string, onChange func(*RootConfig)) (*Watcher, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(absPath)
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &Watcher{
		path:         absPath,
		debounce:     1 * time.Second,
		onChangeRoot: onChange,
		watcher:      fsw,
		stopCh:       make(chan struct{}),
	}

	go w.loop(ctx)
	nlog.Core().Info("config watcher started", "path", absPath)
	return w, nil
}

func (w *Watcher) loop(ctx context.Context) {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
		w.watcher.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return

		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !samePath(filepath.Clean(ev.Name), w.path) {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}

			if timer == nil {
				timer = time.AfterFunc(w.debounce, w.reload)
			} else {
				timer.Reset(w.debounce)
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			nlog.Core().Warn("config watcher error", "error", err)
		}
	}
}

func (w *Watcher) reload() {
	if w.stopped.Load() {
		return
	}
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		if w.stopped.Load() {
			return
		}
		if w.reloadOnce(&lastErr) {
			return
		}
		// Atomic writers on Windows briefly expose the destination as missing
		// while MoveFileEx completes. Retry only that transient condition; a
		// malformed YAML should be reported immediately.
		if !isTransientConfigError(lastErr) {
			nlog.Core().Error("config reload failed, keeping current config", "error", lastErr)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	nlog.Core().Error("config reload failed, keeping current config", "error", lastErr)
}

func (w *Watcher) reloadOnce(lastErr *error) bool {
	if _, err := os.Stat(w.path); err != nil {
		*lastErr = err
		return false
	}
	if w.onChangeRoot != nil {
		root, err := LoadRoot(w.path)
		if err != nil {
			*lastErr = err
			return false
		}
		nlog.Core().Info("config reloaded successfully")
		w.onChangeRoot(root)
		return true
	}
	cfg, err := Load(w.path)
	if err != nil {
		*lastErr = err
		return false
	}
	nlog.Core().Info("config reloaded successfully")
	w.onChange(cfg)
	return true
}

func (w *Watcher) Stop() {
	w.stopped.Store(true)
	w.stopOnce.Do(func() { close(w.stopCh) })
}
