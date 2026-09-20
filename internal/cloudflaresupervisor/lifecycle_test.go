package cloudflaresupervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestPromotedCrashCyclesRespectRetryInterval(t *testing.T) {
	root := t.TempDir()
	children := []*fakeChild{{}, {}, {}, {}}
	for _, child := range children {
		child.ready.Store(true)
	}
	l := &fakeLauncher{children: children}
	s := NewSupervisor(root, l, Options{RetryInterval: time.Second})
	ctx, now := context.Background(), time.Now()
	project(t, root, true, 1)
	s.step(ctx, now)
	s.step(ctx, now)
	for cycle := range 3 {
		children[cycle].exited.Store(true)
		s.step(ctx, now)
		for tick := range 10 {
			s.step(ctx, now.Add(time.Duration(tick)*90*time.Millisecond))
		}
		if l.count != cycle+1 {
			t.Fatalf("cycle %d bypassed crash throttle: %d starts", cycle, l.count)
		}
		if s.Status().Ready {
			t.Fatal("crashed child remains ready")
		}
		now = now.Add(time.Second)
		s.step(ctx, now)
		s.step(ctx, now)
		if !s.Status().Ready || l.count != cycle+2 {
			t.Fatal("connector did not recover after retry deadline")
		}
	}
	s.stop()
}

func TestLifecycleEventsAreStructuredBoundedAndCredentialFree(t *testing.T) {
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	root := t.TempDir()
	old, candidate := &fakeChild{}, &fakeChild{}
	old.ready.Store(true)
	l := &fakeLauncher{children: []*fakeChild{old, candidate}}
	s := NewSupervisor(root, l, Options{Logger: logger, ReadyTimeout: time.Second})
	ctx, now := context.Background(), time.Now()
	project(t, root, true, 1)
	s.step(ctx, now)
	s.step(ctx, now)
	project(t, root, true, 2)
	s.step(ctx, now)
	s.step(ctx, now.Add(2*time.Second))
	size := log.Len()
	for range 100 {
		s.step(ctx, now.Add(3*time.Second))
	}
	if log.Len() != size {
		t.Fatal("unchanged degraded state emits unbounded polling logs")
	}
	project(t, root, false, 3)
	s.step(ctx, now)
	project(t, root, true, 4)
	l.err = errors.New("synthetic-secret " + root)
	s.step(ctx, now)
	codes := make(map[string]bool)
	for line := range strings.SplitSeq(strings.TrimSpace(log.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		for key := range event {
			if key != "time" && key != "level" && key != "msg" && key != "code" && key != "generation" {
				t.Fatalf("unexpected event field: %s", key)
			}
		}
		code, ok := event["code"].(string)
		if !ok {
			t.Fatal("missing event code")
		}
		codes[code] = true
	}
	for _, code := range []string{"candidate_started", "candidate_promoted", "candidate_not_ready", "connector_stopped", "candidate_start_failed"} {
		if !codes[code] {
			t.Fatalf("missing lifecycle event %s", code)
		}
	}
	for _, forbidden := range []string{"synthetic-secret", root, "token", "hash", "/state", "aura-connector-"} {
		if strings.Contains(log.String(), forbidden) {
			t.Fatalf("event leaked %s", forbidden)
		}
	}
}
