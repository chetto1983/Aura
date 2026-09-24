package hostupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWriteActivityThenReadActivityRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Activity{
		WrittenAt:      time.Unix(1790316960, 0).UTC(),
		LastActivityAt: time.Unix(1790316000, 0).UTC(),
		LiveRuns:       2,
	}
	if err := WriteActivity(dir, want); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ActivityFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "written_at=1790316960\nlast_activity_at=1790316000\nlive_runs=2\n" {
		t.Fatalf("activity body %q", body)
	}
	got, ok, err := ReadActivity(dir)
	if err != nil || !ok || got != want {
		t.Fatalf("got %+v ok=%v err=%v, want %+v", got, ok, err, want)
	}
}

func TestReadActivityRefusesANegativeOrMissingRunCount(t *testing.T) {
	for _, live := range []string{"-1", "", "many"} {
		dir := t.TempDir()
		writeFile(t, dir, ActivityFile, "written_at=1790316960\nlast_activity_at=0\nlive_runs="+live+"\n")
		if _, _, err := ReadActivity(dir); err == nil {
			t.Fatalf("ReadActivity accepted live_runs=%q", live)
		}
	}
}

func TestWriteActivityWithNoActivityEverWritesZero(t *testing.T) {
	dir := t.TempDir()
	if err := WriteActivity(dir, Activity{WrittenAt: time.Unix(1790316960, 0)}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ActivityFile))
	if string(body) != "written_at=1790316960\nlast_activity_at=0\nlive_runs=0\n" {
		t.Fatalf("activity body %q", body)
	}
}

type fakeSource struct {
	mu    sync.Mutex
	calls int
	last  time.Time
	live  int
	err   error
}

func (f *fakeSource) Activity(context.Context) (time.Time, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.last, f.live, f.err
}

func TestActivityWriterPublishesOnStartAndEveryTick(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{last: time.Unix(1790316000, 0), live: 1}
	now := time.Unix(1790316960, 0)
	w := NewActivityWriter(dir, src, 10*time.Millisecond, func() time.Time { return now })
	w.Start(context.Background())
	defer w.Stop()

	waitFor(t, func() bool {
		src.mu.Lock()
		defer src.mu.Unlock()
		return src.calls >= 2
	})
	got, ok, err := ReadActivity(dir)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	want := Activity{WrittenAt: now.UTC(), LastActivityAt: time.Unix(1790316000, 0).UTC(), LiveRuns: 1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// A failed read must leave the file to go stale: the updater reads a stale activity file as
// "Aura is down", which is the truth when Aura cannot reach its own database.
func TestActivityWriterWritesNothingWhenTheSourceFails(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{err: errors.New("postgres: connection refused")}
	w := NewActivityWriter(dir, src, 10*time.Millisecond, time.Now)
	w.Start(context.Background())
	waitFor(t, func() bool {
		src.mu.Lock()
		defer src.mu.Unlock()
		return src.calls >= 2
	})
	w.Stop()
	if _, err := os.Stat(filepath.Join(dir, ActivityFile)); !os.IsNotExist(err) {
		t.Fatalf("activity written despite a failing source: %v", err)
	}
}

func TestActivityWriterStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := NewActivityWriter(t.TempDir(), &fakeSource{}, time.Hour, time.Now)
	w.Start(ctx)
	cancel()
	w.Stop()
}

func TestActivityWriterStopIsSafeWhenNeverStarted(t *testing.T) {
	NewActivityWriter(t.TempDir(), &fakeSource{}, time.Hour, time.Now).Stop()
	var nilWriter *ActivityWriter
	nilWriter.Start(context.Background())
	nilWriter.Stop()
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
