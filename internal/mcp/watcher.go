// Watcher (was package mcpwatch) watches mcp.json for changes and triggers a callback.
//
// Wave 2.10.b — when the operator edits mcp.json (today by hand, tomorrow
// via dashboard) the file watcher debounces the burst of events that
// every editor produces on save (3-5 events typical) and fires exactly
// one callback. The callback is whatever the boot wiring chooses; in
// production it's `Reconciler.Notify(toolindex.ReasonMCPConfig)`.
//
// Scope: this package only DETECTS the change. Wave 2.10.b leaves the
// actual MCP server reload (connect new, disconnect old) as a deferred
// 2.10.c task because the existing internal/mcp Client lifecycle is
// boot-only. fsnotify firing today is still useful: the Reconciler runs,
// confirms no diff, and updates indexed_at — at least the operator gets
// confirmation in the logs that the edit was seen.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Callback fires once per debounce window when the watched file changes.
// Implementations are expected to be non-blocking (typically they just
// call Reconciler.Notify which is itself non-blocking).
type Callback func()

// Watcher follows the parent directory of the configured file. Watching
// the file directly is fragile because editors typically use the
// atomic-rename pattern (write to tmp, rename over the original), which
// fsnotify reports as a Rename event on the OLD inode — the watcher then
// loses sight of the file. Watching the parent and filtering by basename
// is the standard cross-platform recipe.
type Watcher struct {
	path     string // absolute path to the file (e.g. mcp.json)
	debounce time.Duration
	cb       Callback
	logger   *slog.Logger
}

// Config wires the watcher.
type Config struct {
	Path     string        // file to watch (e.g. /workspace/mcp.json)
	Debounce time.Duration // default 500 ms
	Callback Callback      // required
	Logger   *slog.Logger
}

// New validates Config and returns a Watcher. The caller must invoke
// Run(ctx) in its own goroutine to start watching; nothing happens until
// then.
func New(cfg Config) (*Watcher, error) {
	if cfg.Path == "" {
		return nil, errors.New("mcpwatch: Path required")
	}
	if cfg.Callback == nil {
		return nil, errors.New("mcpwatch: Callback required")
	}
	abs, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("mcpwatch: abs path: %w", err)
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 500 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Watcher{
		path:     abs,
		debounce: cfg.Debounce,
		cb:       cfg.Callback,
		logger:   cfg.Logger,
	}, nil
}

// Run blocks the calling goroutine, watching for changes until ctx is
// done. Errors creating the underlying watcher are returned immediately;
// errors during operation are logged but do not stop the loop (the next
// successful event continues).
func (w *Watcher) Run(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("mcpwatch: new fsnotify: %w", err)
	}
	defer fw.Close()

	parent := filepath.Dir(w.path)
	if err := fw.Add(parent); err != nil {
		return fmt.Errorf("mcpwatch: watch %s: %w", parent, err)
	}
	w.logger.Info("mcpwatch: watching", "path", w.path, "parent", parent, "debounce", w.debounce)

	basename := filepath.Base(w.path)
	var debounceTimer *time.Timer
	pendingFire := false

	// Helper closure to (re)arm the debounce timer.
	arm := func() {
		if debounceTimer == nil {
			debounceTimer = time.NewTimer(w.debounce)
		} else {
			if !debounceTimer.Stop() {
				// Drain — Stop() returned false because the timer already
				// fired and the value is sitting in the channel.
				select {
				case <-debounceTimer.C:
				default:
				}
			}
			debounceTimer.Reset(w.debounce)
		}
		pendingFire = true
	}

	// Helper for the timer channel that handles nil safely.
	timerChan := func() <-chan time.Time {
		if debounceTimer == nil {
			return nil
		}
		return debounceTimer.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			// Compare basenames; cross-platform paths may use \ on Windows.
			if filepath.Base(ev.Name) != basename {
				continue
			}
			if ev.Op&relevantOps == 0 {
				continue
			}
			w.logger.Debug("mcpwatch: event", "op", ev.Op.String(), "name", ev.Name)
			arm()

		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("mcpwatch: fsnotify error", "err", err)

		case <-timerChan():
			if pendingFire {
				pendingFire = false
				w.cb()
			}
		}
	}
}

// relevantOps filters the fsnotify operations we care about. Create +
// Write + Rename + Remove all signal "the file as seen by readers may now
// differ"; Chmod alone is irrelevant for our content-driven reconcile.
const relevantOps = fsnotify.Create | fsnotify.Write | fsnotify.Rename | fsnotify.Remove

// Path returns the absolute watched path. Useful for assertions and
// startup logging from the wiring layer.
func (w *Watcher) Path() string { return w.path }
