package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
)

type lockCheckingSteer struct {
	*steertest.Fake
	runner   *Runner
	ctx      context.Context
	lockHeld bool
}

func (s *lockCheckingSteer) Push(conv, source, text string) error {
	if unlock, ok := s.runner.TryLockThread(s.ctx, conv); ok {
		unlock()
		s.lockHeld = false
	} else {
		s.lockHeld = true
	}
	return s.Fake.Push(conv, source, text)
}

func TestWakeWithSteerLocksBeforePushAndKeepsTheShellFactOutOfTheTranscript(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-wake", "wake handled")),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := context.Background()
	inbox := &lockCheckingSteer{
		Fake:   steertest.New(steer.Config{Max: 8, MaxBytes: 16384}),
		runner: r,
		ctx:    ctx,
	}
	r.steer = inbox
	message := "Background shell sh-1 completed; call shell_poll."

	if _, err := drain(r.WakeWithSteer(ctx, convID, inbox, steer.SourceShell, message)); err != nil {
		t.Fatalf("WakeWithSteer: %v", err)
	}
	if !inbox.lockHeld {
		t.Fatal("steer Push ran before the conversation lock was held")
	}
	if client.CallCount() != 1 {
		t.Fatalf("LLM calls = %d, want 1", client.CallCount())
	}
	request := client.LastRequest()
	modelSawShellEnvelope := false
	for _, msg := range request.Messages {
		if msg.Role != llm.RoleUser || !strings.Contains(msg.Content, message) {
			continue
		}
		if strings.Contains(msg.Content, `<tool_output source="shell" trust="untrusted" nonce="`) &&
			!strings.Contains(msg.Content, `<user_steer`) {
			modelSawShellEnvelope = true
		}
	}
	if !modelSawShellEnvelope {
		t.Fatalf("model request did not carry the reserved untrusted shell envelope: %+v", request.Messages)
	}

	history, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	// The fact reached the model above; the transcript keeps only the turn it started. A
	// runtime notification is Aura's, never a message the operator sent (steer.IsRuntimeSource).
	answered := false
	for _, msg := range history {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, "sh-1") {
			t.Fatalf("runtime fact persisted as an operator message; history=%+v", history)
		}
		answered = answered || (msg.Role == llm.RoleAssistant && msg.Content == "wake handled")
	}
	if !answered {
		t.Fatalf("the woken turn's answer was not persisted; history=%+v", history)
	}
	if drained := inbox.Drain(convID); len(drained) != 0 {
		t.Fatalf("wake left %d steer row(s) undrained", len(drained))
	}
}

// TestWakeWithSteerRunsUnderAHostsLock is the contract the AG-UI host relies on: it takes the
// lock with LockThread, registers the run, and hands the wake a ctx saying the lock is held.
// Locking again there would wait on itself forever.
func TestWakeWithSteerRunsUnderAHostsLock(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-wake", "wake handled")),
	)
	r, _, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)
	inbox := steertest.New(steer.Config{Max: 8, MaxBytes: 1024})
	r.steer = inbox
	ctx := context.Background()
	unlock, err := r.LockThread(ctx, convID)
	if err != nil {
		t.Fatalf("LockThread: %v", err)
	}
	defer unlock()

	done := make(chan error, 1)
	go func() {
		_, err := drain(r.WakeWithSteer(WithThreadLockHeld(ctx), convID, inbox, steer.SourceShell, "done"))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WakeWithSteer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WakeWithSteer waited for the lock its host already holds")
	}
	if client.CallCount() != 1 {
		t.Fatalf("LLM calls = %d, want 1", client.CallCount())
	}
}

func TestWakeWithSteerGivesUpWhileAnotherTurnHoldsTheConversation(t *testing.T) {
	client := agenttest.NewFakeClient()
	r, _, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)
	inbox := steertest.New(steer.Config{Max: 8, MaxBytes: 1024})
	r.steer = inbox
	unlock, err := r.LockThread(context.Background(), convID)
	if err != nil {
		t.Fatalf("LockThread: %v", err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := drain(r.WakeWithSteer(ctx, convID, inbox, steer.SourceShell, "done")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WakeWithSteer behind a held conversation = %v, want its context's deadline", err)
	}
	if queued := inbox.Drain(convID); len(queued) != 0 {
		t.Fatalf("a wake that never held the lock pushed %d message(s)", len(queued))
	}
	if client.CallCount() != 0 {
		t.Fatalf("a wake that never held the lock called the model %d time(s)", client.CallCount())
	}
}

type refusingPusher struct{}

func (refusingPusher) Push(string, string, string) error { return errors.New("inbox full") }

func TestWakeWithSteerRefusesWhatItCannotDeliver(t *testing.T) {
	client := agenttest.NewFakeClient()
	r, _, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := context.Background()
	inbox := steertest.New(steer.Config{Max: 8, MaxBytes: 1024})
	for name, wake := range map[string]func() error{
		"no pusher": func() error {
			_, err := drain(r.WakeWithSteer(ctx, convID, nil, steer.SourceShell, "done"))
			return err
		},
		"unknown conversation": func() error {
			_, err := drain(r.WakeWithSteer(ctx, newConvID(t), inbox, steer.SourceShell, "done"))
			return err
		},
		"refused push": func() error {
			_, err := drain(r.WakeWithSteer(ctx, convID, refusingPusher{}, steer.SourceShell, "done"))
			return err
		},
	} {
		if err := wake(); err == nil {
			t.Errorf("%s: WakeWithSteer returned nil error", name)
		}
	}
	if client.CallCount() != 0 {
		t.Fatalf("an undeliverable wake called the model %d time(s)", client.CallCount())
	}
	if unlock, ok := r.TryLockThread(ctx, convID); !ok {
		t.Fatal("an undeliverable wake left the conversation locked")
	} else {
		unlock()
	}
}

func TestLockThreadQueuesBehindTheHolderUntilItsContextEnds(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	ctx := context.Background()
	unlock, err := r.LockThread(ctx, convID)
	if err != nil {
		t.Fatalf("LockThread: %v", err)
	}
	if _, ok := r.TryLockThread(ctx, convID); ok {
		t.Fatal("LockThread did not take the conversation's run lock")
	}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := r.LockThread(short, convID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("LockThread on a held conversation = %v, want its context's deadline", err)
	}

	next := make(chan func(), 1)
	go func() {
		if unlockNext, err := r.LockThread(ctx, convID); err == nil {
			next <- unlockNext
		}
	}()
	select {
	case <-next:
		t.Fatal("a second LockThread took a held lock")
	case <-time.After(30 * time.Millisecond):
	}
	unlock()
	select {
	case unlockNext := <-next:
		unlockNext()
	case <-time.After(5 * time.Second):
		t.Fatal("LockThread did not take the lock once it was released")
	}
}

func TestWakeWithSteerCanceledContextNeverPushes(t *testing.T) {
	client := agenttest.NewFakeClient()
	r, _, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)
	inbox := steertest.New(steer.Config{Max: 8, MaxBytes: 1024})
	r.steer = inbox
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := drain(r.WakeWithSteer(ctx, convID, inbox, steer.SourceShell, "done")); err == nil {
		t.Fatal("canceled wake returned nil error")
	}
	if queued := inbox.Drain(convID); len(queued) != 0 {
		t.Fatalf("canceled wake pushed %d message(s)", len(queued))
	}
	if client.CallCount() != 0 {
		t.Fatalf("canceled wake called the model %d time(s)", client.CallCount())
	}
}
