package cloudflaresupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeChild struct {
	ready   atomic.Bool
	stopped atomic.Bool
	exited  atomic.Bool
}

func (c *fakeChild) Ready(context.Context) bool { return c.ready.Load() && !c.exited.Load() }
func (c *fakeChild) Exited() bool               { return c.exited.Load() }
func (c *fakeChild) Stop()                      { c.stopped.Store(true); c.exited.Store(true) }

type fakeLauncher struct {
	children []*fakeChild
	count    int
	err      error
}

func (l *fakeLauncher) Start(string) (Child, error) {
	if l.err != nil {
		return nil, l.err
	}
	c := l.children[l.count]
	l.count++
	return c, nil
}

func project(t *testing.T, root string, enabled bool, generation int64) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "token"), []byte("synthetic-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(desiredState{Enabled: enabled, Generation: generation})
	if err := os.WriteFile(filepath.Join(root, "desired.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFailedCandidateKeepsOldConnector(t *testing.T) {
	root := t.TempDir()
	old, candidate := &fakeChild{}, &fakeChild{}
	old.ready.Store(true)
	l := &fakeLauncher{children: []*fakeChild{old, candidate}}
	s := NewSupervisor(root, l, Options{ReadyTimeout: 100 * time.Millisecond})
	project(t, root, true, 1)
	s.step(context.Background(), time.Now())
	s.step(context.Background(), time.Now())
	project(t, root, true, 2)
	now := time.Now()
	s.step(context.Background(), now)
	s.step(context.Background(), now.Add(10*time.Millisecond))
	if old.stopped.Load() || s.Status().ActiveGeneration != 1 {
		t.Fatal("unready candidate replaced healthy connector")
	}
	s.step(context.Background(), now.Add(time.Second))
	if old.stopped.Load() || !candidate.stopped.Load() || s.Status().State != "degraded" || s.Status().ActiveGeneration != 1 {
		t.Fatalf("handoff lost old connector: %+v", s.Status())
	}
	s.stop()
}

func TestReadyCandidateReplacesOldAndDisableStopsIt(t *testing.T) {
	root := t.TempDir()
	old, candidate := &fakeChild{}, &fakeChild{}
	old.ready.Store(true)
	candidate.ready.Store(true)
	l := &fakeLauncher{children: []*fakeChild{old, candidate}}
	s := NewSupervisor(root, l, Options{})
	for _, generation := range []int64{1, 2} {
		project(t, root, true, generation)
		s.step(context.Background(), time.Now())
		s.step(context.Background(), time.Now())
	}
	if !old.stopped.Load() || candidate.stopped.Load() || s.Status().ActiveGeneration != 2 {
		t.Fatalf("handoff: %+v", s.Status())
	}
	project(t, root, false, 3)
	s.step(context.Background(), time.Now())
	if !candidate.stopped.Load() || s.Status().State != "idle" {
		t.Fatalf("disable: %+v", s.Status())
	}
}

func TestIdleAndShutdown(t *testing.T) {
	s := NewSupervisor(t.TempDir(), &fakeLauncher{}, Options{PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
	if s.Status().State != "idle" {
		t.Fatalf("idle: %+v", s.Status())
	}
}

func TestCandidateFailureAndCrashRetry(t *testing.T) {
	root := t.TempDir()
	old, failed, replacement := &fakeChild{}, &fakeChild{}, &fakeChild{}
	old.ready.Store(true)
	replacement.ready.Store(true)
	l := &fakeLauncher{children: []*fakeChild{old, failed, replacement}}
	s := NewSupervisor(root, l, Options{RetryInterval: time.Second})
	ctx, now := context.Background(), time.Now()
	project(t, root, true, 1)
	s.step(ctx, now)
	s.step(ctx, now)
	old.ready.Store(false)
	s.step(ctx, now)
	if s.Status().ErrorCode != "connector_not_ready" {
		t.Fatal("readiness loss not observed")
	}
	old.exited.Store(true)
	s.step(ctx, now)
	now = now.Add(time.Second)
	s.step(ctx, now)
	failed.exited.Store(true)
	s.step(ctx, now)
	if s.Status().Ready || s.Status().ActiveGeneration != 0 {
		t.Fatal("dead child still ready")
	}
	s.step(ctx, now.Add(time.Millisecond))
	if l.count != 2 {
		t.Fatal("retry backoff bypassed")
	}
	s.step(ctx, now.Add(2*time.Second))
	s.step(ctx, now.Add(2*time.Second))
	if !s.Status().Ready || l.count != 3 {
		t.Fatal("crashed child not replaced")
	}
	s.stop()
}

func TestInvalidProjectionAndLaunchFailurePreserveActive(t *testing.T) {
	root := t.TempDir()
	old := &fakeChild{}
	old.ready.Store(true)
	l := &fakeLauncher{children: []*fakeChild{old}}
	s := NewSupervisor(root, l, Options{})
	ctx, now := context.Background(), time.Now()
	project(t, root, true, 1)
	s.step(ctx, now)
	s.step(ctx, now)
	for _, invalid := range []string{`{`, `{"enabled":true,"generation":2,"token":"synthetic"}`, `{"generation":-1}`, `{} {}`} {
		if err := os.WriteFile(filepath.Join(root, "desired.json"), []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		s.step(ctx, now)
		if s.Status().ErrorCode != "projection_invalid" || old.stopped.Load() {
			t.Fatal("invalid projection interrupted old connector")
		}
	}
	project(t, root, true, 2)
	if err := os.Remove(filepath.Join(root, "token")); err != nil {
		t.Fatal(err)
	}
	s.step(ctx, now)
	if s.Status().ErrorCode != "token_unavailable" || old.stopped.Load() {
		t.Fatal("missing token interrupted old connector")
	}
	project(t, root, true, 2)
	l.err = errors.New("synthetic-secret must not escape")
	s.step(ctx, now)
	if s.Status().ErrorCode != "candidate_start_failed" || old.stopped.Load() {
		t.Fatal("launch failure interrupted old connector")
	}
	s.stop()
}

func TestSupersededCandidateCannotPromote(t *testing.T) {
	root := t.TempDir()
	obsolete, current := &fakeChild{}, &fakeChild{}
	current.ready.Store(true)
	l := &fakeLauncher{children: []*fakeChild{obsolete, current}}
	s := NewSupervisor(root, l, Options{})
	ctx, now := context.Background(), time.Now()
	project(t, root, true, 1)
	s.step(ctx, now)
	project(t, root, true, 2)
	obsolete.ready.Store(true)
	s.step(ctx, now)
	s.step(ctx, now)
	if !obsolete.stopped.Load() || s.Status().ActiveGeneration != 2 {
		t.Fatal("obsolete candidate promoted")
	}
	s.stop()
}

func TestRunIsObservableAndCancellationStopsActive(t *testing.T) {
	root := t.TempDir()
	old := &fakeChild{}
	old.ready.Store(true)
	s := NewSupervisor(root, &fakeLauncher{children: []*fakeChild{old}}, Options{PollInterval: time.Millisecond})
	project(t, root, true, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !s.Status().Ready && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if !old.stopped.Load() {
		t.Fatal("shutdown did not reap active child")
	}
}
