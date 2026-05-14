package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew_RejectsMissingDeps(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"empty path", Config{Callback: func() {}}},
		{"nil callback", Config{Path: "/tmp/mcp.json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestWatcher_FiresOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	fired := make(chan struct{}, 4)
	w, err := New(Config{
		Path:     path,
		Debounce: 30 * time.Millisecond,
		Callback: func() { fired <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Give the watcher a moment to register the inotify subscription.
	time.Sleep(50 * time.Millisecond)

	if err := os.WriteFile(path, []byte(`{"updated":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not fire after write")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not exit on cancel")
	}
}

func TestWatcher_CoalescesBurst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var firedCount atomic.Int64
	w, err := New(Config{
		Path:     path,
		Debounce: 100 * time.Millisecond,
		Callback: func() { firedCount.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	// Hammer the file with 8 writes inside the debounce window.
	for i := 0; i < 8; i++ {
		_ = os.WriteFile(path, []byte("{}"), 0o644)
		time.Sleep(10 * time.Millisecond)
	}

	// Allow the debounce window to elapse + a small buffer.
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond) // let Run exit

	got := firedCount.Load()
	// The 8 events were all inside a single debounce window (110 ms <
	// 100 ms × 2). The callback must fire exactly once. The "at most 2"
	// guard tolerates a race where the very last write arrived just
	// after the timer fired and started a new window.
	if got < 1 || got > 2 {
		t.Fatalf("expected 1-2 callback fires (debounce), got %d", got)
	}
}

func TestWatcher_IgnoresSiblingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "unrelated.txt")

	var firedCount atomic.Int64
	w, _ := New(Config{
		Path:     path,
		Debounce: 30 * time.Millisecond,
		Callback: func() { firedCount.Add(1) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	// Write to the wrong file 5 times — callback must not fire.
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(other, []byte("noise"), 0o644)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	if got := firedCount.Load(); got != 0 {
		t.Fatalf("watcher fired %d times on sibling-file changes (must be 0)", got)
	}
}

func TestWatcher_FiresOnAtomicRename(t *testing.T) {
	// Atomic-rename pattern: write to tmp, rename over the original. This
	// is what most editors and `mv` do. The watcher MUST see this as a
	// change event.
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	fired := make(chan struct{}, 4)
	w, _ := New(Config{
		Path:     path,
		Debounce: 30 * time.Millisecond,
		Callback: func() { fired <- struct{}{} },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(`{"changed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not fire after atomic rename")
	}
}
