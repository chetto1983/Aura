package runner

import (
	"context"
	"strings"
	"testing"

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

func TestWakeWithSteerLocksBeforePushAndUsesUntrustedShellEnvelope(t *testing.T) {
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
	runtimeTurns := 0
	for _, msg := range history {
		if msg.Role == llm.RoleUser && msg.Content == message {
			runtimeTurns++
		}
	}
	if runtimeTurns != 1 {
		t.Fatalf("persisted runtime turns = %d, want 1; history=%+v", runtimeTurns, history)
	}
	if drained := inbox.Drain(convID); len(drained) != 0 {
		t.Fatalf("wake left %d steer row(s) undrained", len(drained))
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
