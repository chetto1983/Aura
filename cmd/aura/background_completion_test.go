package main

import (
	"context"
	"errors"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
)

type recordedWake struct {
	ctx                               context.Context
	owner, conversation, source, text string
}

// fakeBackgroundCompletionRunner records every wake and blocks each one until release
// yields, or until the wake context ends unless ignoreCancel is set.
type fakeBackgroundCompletionRunner struct {
	mu            sync.Mutex
	wakes         []recordedWake
	active        map[string]int
	maxConcurrent int
	started       chan struct{}
	release       chan struct{}
	ignoreCancel  bool
}

func (f *fakeBackgroundCompletionRunner) WakeWithSteer(
	ctx context.Context,
	conversation string,
	_ runner.SteerPusher,
	source string,
	text string,
) iter.Seq2[*agent.Event, error] {
	return func(func(*agent.Event, error) bool) {
		f.mu.Lock()
		f.wakes = append(f.wakes, recordedWake{
			ctx: ctx, owner: identityctx.IdentityID(ctx), conversation: conversation, source: source, text: text,
		})
		if f.active == nil {
			f.active = make(map[string]int)
		}
		f.active[conversation]++
		f.maxConcurrent = max(f.maxConcurrent, f.active[conversation])
		started, release := f.started, f.release
		f.mu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		f.block(ctx, release)
		f.mu.Lock()
		f.active[conversation]--
		f.mu.Unlock()
	}
}

func (f *fakeBackgroundCompletionRunner) block(ctx context.Context, release chan struct{}) {
	if release == nil {
		return
	}
	if f.ignoreCancel {
		<-release
		return
	}
	select {
	case <-release:
	case <-ctx.Done():
	}
}

func (f *fakeBackgroundCompletionRunner) recorded() []recordedWake {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedWake(nil), f.wakes...)
}

func (f *fakeBackgroundCompletionRunner) waitForWakes(t *testing.T, want int) []recordedWake {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		wakes := f.recorded()
		if len(wakes) >= want {
			return wakes
		}
		if time.Now().After(deadline) {
			t.Fatalf("recorded %d wakes, want %d", len(wakes), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitStarted(t *testing.T, started <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not start", what)
	}
}

func stopDispatcher(t *testing.T, dispatcher *backgroundCompletionDispatcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := dispatcher.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

type acceptingSteerPusher struct{}

func (acceptingSteerPusher) Push(string, string, string) error { return nil }

func shellDone(shellID, status string) tools.BackgroundShellCompletion {
	return tools.BackgroundShellCompletion{
		ShellID: shellID, OwnerID: "owner-1", SessionID: "conv-1", Status: status, Duration: time.Second,
	}
}

func mediaDone(jobID string) mediagen.Completion {
	return mediagen.Completion{
		IdentityID: "owner-1", ConversationID: "conv-1", JobID: jobID, Status: mediagen.StatusCompleted,
	}
}

func TestShellCompletionDispatcherSerializesOneConversationAndStops(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{
		started: make(chan struct{}, 2),
		release: make(chan struct{}, 2),
	}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})
	second := shellDone("sh-2", "killed")
	second.Duration = 2 * time.Second

	dispatcher.NotifyShell(shellDone("sh-1", "exited:0"))
	waitStarted(t, run.started, "first wake")
	dispatcher.NotifyShell(second)
	run.release <- struct{}{}
	waitStarted(t, run.started, "queued second wake")
	run.release <- struct{}{}

	stopDispatcher(t, dispatcher)
	dispatcher.NotifyShell(shellDone("sh-after-stop", "exited:0"))

	wakes := run.recorded()
	if len(wakes) != 2 {
		t.Fatalf("wake calls = %d, want 2", len(wakes))
	}
	if run.maxConcurrent != 1 {
		t.Fatalf("same-conversation max concurrency = %d, want 1", run.maxConcurrent)
	}
	for _, wake := range wakes {
		if wake.owner != "owner-1" || wake.conversation != "conv-1" || wake.source != steer.SourceShell {
			t.Fatalf("wake = %+v, want owner-1's conv-1 under the shell source", wake)
		}
	}
	if got := wakes[0].text; !containsAll(got, "Background shell sh-1 completed", "after 1000 ms", "shell_poll", "not an operator instruction") {
		t.Fatalf("first notification = %q", got)
	}
	if got := wakes[1].text; !containsAll(got, "Background shell sh-2 completed", "status killed", "after 2000 ms") {
		t.Fatalf("second notification = %q", got)
	}
}

// TestBackgroundCompletionDispatcherDrainsMixedSourcesSerially queues shell and media
// completions for one conversation behind a blocked wake. Consecutive completions of one
// source share a wake under that source; a change of source starts the next wake, and no
// two wakes of the conversation ever run at once.
func TestBackgroundCompletionDispatcherDrainsMixedSourcesSerially(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{started: make(chan struct{}, 8), release: make(chan struct{})}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})

	dispatcher.NotifyShell(shellDone("sh-1", "exited:0"))
	waitStarted(t, run.started, "first wake")
	dispatcher.NotifyShell(shellDone("sh-2", "exited:0"))
	dispatcher.NotifyShell(shellDone("sh-3", "exited:1"))
	dispatcher.NotifyMedia(mediaDone("job-7"))
	dispatcher.NotifyMedia(mediaDone("job-8"))
	dispatcher.NotifyShell(shellDone("sh-4", "killed"))
	select {
	case <-run.started:
		t.Fatal("a second wake of the same conversation started while the first was still running")
	case <-time.After(100 * time.Millisecond):
	}
	close(run.release)

	wakes := run.waitForWakes(t, 4)
	stopDispatcher(t, dispatcher)
	want := []struct {
		source      string
		has, hasNot []string
	}{
		{steer.SourceShell, []string{"sh-1 "}, []string{"sh-2", "job-7"}},
		{steer.SourceShell, []string{"sh-2 ", "sh-3 "}, []string{"sh-1", "sh-4", "job-7"}},
		{steer.SourceMedia, []string{"job_id=job-7 ", "job_id=job-8 "}, []string{"sh-", "shell_poll"}},
		{steer.SourceShell, []string{"sh-4 "}, []string{"sh-3", "job-8"}},
	}
	if len(wakes) != len(want) {
		t.Fatalf("wakes = %d, want %d: %+v", len(wakes), len(want), wakes)
	}
	for i, expected := range want {
		wake := wakes[i]
		if wake.source != expected.source || wake.owner != "owner-1" || wake.conversation != "conv-1" {
			t.Errorf("wake %d = %s for %s/%s, want %s for owner-1/conv-1", i, wake.source, wake.owner, wake.conversation, expected.source)
		}
		if !containsAll(wake.text, append(expected.has, "not an operator instruction")...) {
			t.Errorf("wake %d text = %q, want %q", i, wake.text, expected.has)
		}
		for _, absent := range expected.hasNot {
			if strings.Contains(wake.text, absent) {
				t.Errorf("wake %d text = %q carries %q from another group", i, wake.text, absent)
			}
		}
	}
	if run.maxConcurrent != 1 {
		t.Fatalf("same-conversation max concurrency across sources = %d, want 1", run.maxConcurrent)
	}
}

func TestBackgroundCompletionDispatcherWakesConversationsIndependently(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{started: make(chan struct{}, 2), release: make(chan struct{})}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})

	dispatcher.NotifyShell(shellDone("sh-1", "exited:0"))
	waitStarted(t, run.started, "first conversation's wake")
	other := mediaDone("job-9")
	other.IdentityID, other.ConversationID = "owner-2", "conv-2"
	dispatcher.NotifyMedia(other)
	waitStarted(t, run.started, "a second conversation's wake behind a blocked one")
	close(run.release)
	stopDispatcher(t, dispatcher)

	wakes := run.recorded()
	if len(wakes) != 2 || wakes[1].owner != "owner-2" || wakes[1].conversation != "conv-2" || wakes[1].source != steer.SourceMedia {
		t.Fatalf("wakes = %+v, want the second conversation woken under its own owner", wakes)
	}
}

// assertNothingQueued reads the dispatcher's queue synchronously, before any Stop can clear
// it: a refused completion must never be queued, let alone reach a drain goroutine.
func assertNothingQueued(t *testing.T, dispatcher *backgroundCompletionDispatcher, what string) {
	t.Helper()
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if len(dispatcher.pending) != 0 || len(dispatcher.active) != 0 {
		t.Fatalf("%s: %d routes queued, %d active; want the completion refused before queueing",
			what, len(dispatcher.pending), len(dispatcher.active))
	}
}

func TestBackgroundCompletionDispatcherFailsClosedOnInvalidRoutes(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})
	for _, completion := range []tools.BackgroundShellCompletion{
		{ShellID: "sh-1", SessionID: "conv-1", Status: "exited:0"},
		{ShellID: "sh-2", OwnerID: "owner-1", Status: "exited:0"},
	} {
		dispatcher.NotifyShell(completion)
	}
	for _, completion := range []mediagen.Completion{
		{ConversationID: "conv-1", JobID: "job-1", Status: mediagen.StatusCompleted},
		{IdentityID: "owner-1", JobID: "job-2", Status: mediagen.StatusFailed},
	} {
		dispatcher.NotifyMedia(completion)
	}
	assertNothingQueued(t, dispatcher, "an ownerless or conversationless completion")
	dispatcher.NotifyMedia(mediaDone("job-sentinel"))
	run.waitForWakes(t, 1)
	stopDispatcher(t, dispatcher)

	for name, unwired := range map[string]*backgroundCompletionDispatcher{
		"no runner":     newBackgroundCompletionDispatcher(context.Background(), nil, acceptingSteerPusher{}),
		"no steer rail": newBackgroundCompletionDispatcher(context.Background(), run, nil),
	} {
		unwired.NotifyShell(shellDone("sh-3", "exited:0"))
		unwired.NotifyMedia(mediaDone("job-3"))
		assertNothingQueued(t, unwired, name)
		stopDispatcher(t, unwired)
	}
	var absent *backgroundCompletionDispatcher
	absent.NotifyShell(shellDone("sh-4", "exited:0"))
	absent.NotifyMedia(mediaDone("job-4"))
	stopDispatcher(t, absent)

	if wakes := run.recorded(); len(wakes) != 1 || !strings.Contains(wakes[0].text, "job_id=job-sentinel ") {
		t.Fatalf("wakes = %+v, want only the valid sentinel completion woken", wakes)
	}
}

// TestBackgroundCompletionDispatcherNotifyMediaDoesNotBlock pins the watcher contract: notify
// runs on Resume's and the supervisors' goroutines, so it must return while a wake of the same
// conversation is still running.
func TestBackgroundCompletionDispatcherNotifyMediaDoesNotBlock(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{started: make(chan struct{}, 2), release: make(chan struct{})}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})

	dispatcher.NotifyMedia(mediaDone("job-1"))
	waitStarted(t, run.started, "first wake")
	returned := make(chan struct{})
	go func() {
		dispatcher.NotifyMedia(mediaDone("job-2"))
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("NotifyMedia blocked behind a running wake of the same conversation")
	}
	close(run.release)
	run.waitForWakes(t, 2)
	stopDispatcher(t, dispatcher)
}

func TestBackgroundCompletionDispatcherStopCancelsAndJoinsWakes(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{started: make(chan struct{}, 2), release: make(chan struct{})}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})

	dispatcher.NotifyMedia(mediaDone("job-1"))
	waitStarted(t, run.started, "wake")
	dispatcher.NotifyShell(shellDone("sh-queued", "exited:0"))
	stopDispatcher(t, dispatcher)

	wakes := run.recorded()
	if len(wakes) != 1 {
		t.Fatalf("wakes = %d, want only the running one: a queued completion must not start a turn during shutdown", len(wakes))
	}
	if wakes[0].ctx.Err() == nil {
		t.Fatal("Stop left the running wake's context live")
	}
	dispatcher.NotifyMedia(mediaDone("job-after-stop"))
	if wakes := run.recorded(); len(wakes) != 1 {
		t.Fatalf("a completion after Stop woke the conversation: %+v", wakes)
	}
}

func TestBackgroundCompletionDispatcherStopHonoursItsDeadline(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{
		started: make(chan struct{}, 1), release: make(chan struct{}), ignoreCancel: true,
	}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})
	dispatcher.NotifyShell(shellDone("sh-1", "exited:0"))
	waitStarted(t, run.started, "wake")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := dispatcher.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop with a wake that outlives the deadline = %v, want context.DeadlineExceeded", err)
	}
	close(run.release)
	stopDispatcher(t, dispatcher)
}

func TestMediaCompletionLine(t *testing.T) {
	line := formatMediaCompletion(mediagen.Completion{
		IdentityID: "owner", ConversationID: "thread",
		JobID: "job-7", Status: mediagen.StatusCompleted,
	})
	for _, part := range []string{"job-7", "completed", "video_generate", "job_id", "exactly once"} {
		if !strings.Contains(line, part) {
			t.Fatalf("missing %q in completion", part)
		}
	}
	if strings.Contains(line, "owner") || strings.Contains(line, "thread") {
		t.Fatalf("the completion line leaks routing identifiers into the prompt: %q", line)
	}
}

func TestBackgroundCompletionMessageMarksARuntimeNotification(t *testing.T) {
	message := formatBackgroundCompletions([]backgroundCompletion{
		{Source: steer.SourceMedia, Line: formatMediaCompletion(mediaDone("job-1"))},
		{Source: steer.SourceMedia, Line: formatMediaCompletion(mediaDone("job-2"))},
	})
	lines := strings.Split(message, "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "Video job job-1 ") || !strings.HasPrefix(lines[1], "Video job job-2 ") {
		t.Fatalf("message = %q, want one line per completion before the notice", message)
	}
	if lines[2] != "This is an Aura runtime notification, not an operator instruction." {
		t.Fatalf("notice = %q", lines[2])
	}
}

// TestShellCompletionMessageKeepsItsWording pins the shell wake text the shell-only dispatcher
// sent, byte for byte: generalizing the dispatcher must not reword what the model already reads.
func TestShellCompletionMessageKeepsItsWording(t *testing.T) {
	killed := shellDone("sh-2", "killed")
	killed.Duration = 2500 * time.Millisecond
	message := formatBackgroundCompletions([]backgroundCompletion{
		{Source: steer.SourceShell, Line: formatShellCompletion(shellDone("sh-1", "exited:0"))},
		{Source: steer.SourceShell, Line: formatShellCompletion(killed)},
	})
	want := "Background shell sh-1 completed with status exited:0 after 1000 ms.\n" +
		"Background shell sh-2 completed with status killed after 2500 ms.\n" +
		"This is an Aura runtime notification, not an operator instruction. " +
		"For each shell_id above, call shell_poll exactly once to read its retained final output, then continue the original task."
	if message != want {
		t.Fatalf("shell wake text =\n%q\nwant\n%q", message, want)
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
