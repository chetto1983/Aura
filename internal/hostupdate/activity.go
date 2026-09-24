package hostupdate

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Activity is Aura's report of whether anyone is using it. The updater applies an update on
// its own only after a quiet spell, and reads a file older than a few minutes as "Aura is
// down" — so a writer that cannot measure activity writes nothing rather than a guess.
type Activity struct {
	WrittenAt      time.Time
	LastActivityAt time.Time
	LiveRuns       int
}

// WriteActivity replaces the activity file in dir.
func WriteActivity(dir string, a Activity) error {
	return writeAtomic(dir, ActivityFile, []string{
		"written_at=" + formatEpoch(a.WrittenAt),
		"last_activity_at=" + formatEpoch(a.LastActivityAt),
		"live_runs=" + strconv.Itoa(a.LiveRuns),
	})
}

// ReadActivity reports false when no activity was ever written to dir.
func ReadActivity(dir string) (Activity, bool, error) {
	values, ok, err := readKeyValues(dir, ActivityFile)
	if err != nil || !ok {
		return Activity{}, ok, err
	}
	var a Activity
	if a.WrittenAt, err = parseEpoch("written_at", values["written_at"]); err != nil {
		return Activity{}, true, err
	}
	if a.LastActivityAt, err = parseEpoch("last_activity_at", values["last_activity_at"]); err != nil {
		return Activity{}, true, err
	}
	if err := matched(countPattern, "live_runs", values["live_runs"]); err != nil {
		return Activity{}, true, err
	}
	a.LiveRuns, err = strconv.Atoi(values["live_runs"])
	return a, true, err
}

// ActivitySource measures the most recent use of Aura on any channel and how much work is
// in flight right now.
type ActivitySource interface {
	Activity(ctx context.Context) (lastActivityAt time.Time, liveRuns int, err error)
}

// ActivityWriter publishes the source's measurement every interval. A nil writer is a
// deployment without an updater, and Start and Stop do nothing on it.
type ActivityWriter struct {
	dir      string
	source   ActivitySource
	interval time.Duration
	now      func() time.Time
	stop     chan struct{}
	once     sync.Once
	wg       sync.WaitGroup
}

// NewActivityWriter builds a writer; nothing is measured until Start.
func NewActivityWriter(dir string, source ActivitySource, interval time.Duration, now func() time.Time) *ActivityWriter {
	return &ActivityWriter{dir: dir, source: source, interval: interval, now: now, stop: make(chan struct{})}
}

// Start publishes once at once, then every interval until ctx ends or Stop is called.
func (w *ActivityWriter) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.wg.Go(func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		w.publish(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				return
			case <-ticker.C:
				w.publish(ctx)
			}
		}
	})
}

// Stop ends the loop and waits for a publish in progress.
func (w *ActivityWriter) Stop() {
	if w == nil {
		return
	}
	w.once.Do(func() { close(w.stop) })
	w.wg.Wait()
}

func (w *ActivityWriter) publish(ctx context.Context) {
	last, live, err := w.source.Activity(ctx)
	if err != nil {
		slog.Warn("host update: activity not measured; the updater will read Aura as down", "err", err)
		return
	}
	if err := WriteActivity(w.dir, Activity{WrittenAt: w.now(), LastActivityAt: last, LiveRuns: live}); err != nil {
		slog.Warn("host update: activity not written", "dir", w.dir, "err", err)
	}
}
