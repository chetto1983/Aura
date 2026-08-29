package main

import (
	"context"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/runner"
)

type fakeShellCompletionRunner struct {
	mu            sync.Mutex
	calls         []string
	concurrent    int
	maxConcurrent int
	started       chan struct{}
	release       chan struct{}
}

func (f *fakeShellCompletionRunner) WakeWithSteer(
	_ context.Context,
	_ string,
	_ runner.SteerPusher,
	_ string,
	text string,
) iter.Seq2[*agent.Event, error] {
	return func(func(*agent.Event, error) bool) {
		f.mu.Lock()
		f.calls = append(f.calls, text)
		f.concurrent++
		if f.concurrent > f.maxConcurrent {
			f.maxConcurrent = f.concurrent
		}
		started := f.started
		release := f.release
		f.mu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		if release != nil {
			<-release
		}
		f.mu.Lock()
		f.concurrent--
		f.mu.Unlock()
	}
}

type acceptingSteerPusher struct{}

func (acceptingSteerPusher) Push(string, string, string) error { return nil }

func TestShellCompletionDispatcherSerializesOneConversationAndStops(t *testing.T) {
	run := &fakeShellCompletionRunner{
		started: make(chan struct{}, 2),
		release: make(chan struct{}, 2),
	}
	dispatcher := newShellCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})
	first := tools.BackgroundShellCompletion{
		ShellID: "sh-1", OwnerID: "owner-1", SessionID: "conv-1", Status: "exited:0", Duration: time.Second,
	}
	second := tools.BackgroundShellCompletion{
		ShellID: "sh-2", OwnerID: "owner-1", SessionID: "conv-1", Status: "killed", Duration: 2 * time.Second,
	}

	dispatcher.Notify(first)
	select {
	case <-run.started:
	case <-time.After(time.Second):
		t.Fatal("first wake did not start")
	}
	dispatcher.Notify(second)
	run.release <- struct{}{}
	select {
	case <-run.started:
	case <-time.After(time.Second):
		t.Fatal("queued second wake did not start")
	}
	run.release <- struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := dispatcher.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	dispatcher.Notify(tools.BackgroundShellCompletion{
		ShellID: "sh-after-stop", OwnerID: "owner-1", SessionID: "conv-1", Status: "exited:0",
	})

	run.mu.Lock()
	defer run.mu.Unlock()
	if len(run.calls) != 2 {
		t.Fatalf("wake calls = %d, want 2", len(run.calls))
	}
	if run.maxConcurrent != 1 {
		t.Fatalf("same-conversation max concurrency = %d, want 1", run.maxConcurrent)
	}
	if got := run.calls[0]; !containsAll(got, "Background shell sh-1 completed", "shell_poll", "not an operator instruction") {
		t.Fatalf("first notification = %q", got)
	}
	if got := run.calls[1]; !containsAll(got, "Background shell sh-2 completed", "status killed") {
		t.Fatalf("second notification = %q", got)
	}
}

func containsAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
