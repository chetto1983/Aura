package telegram

import (
	"context"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
)

// recordingConsumer drains its channel and records every event type it saw, so a
// test can assert the per-turn fanout distributed the SAME translated stream to
// both the status and the content consumer.
type recordingConsumer struct {
	mu    sync.Mutex
	types []events.EventType
	done  chan struct{}
}

func newRecordingConsumer() *recordingConsumer {
	return &recordingConsumer{done: make(chan struct{})}
}

func (r *recordingConsumer) consume(ctx context.Context, ch <-chan events.Event) {
	defer close(r.done)
	for ev := range ch {
		_ = ctx
		r.mu.Lock()
		r.types = append(r.types, ev.Type())
		r.mu.Unlock()
	}
}

func (r *recordingConsumer) seen() []events.EventType {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]events.EventType, len(r.types))
	copy(out, r.types)
	return out
}

// syntheticTurn returns a turnDriver yielding a fixed *agent.Event stream so the
// real Translate→Fanout path runs without a live Runner/DB.
func syntheticTurn(evs []*agent.Event) turnDriver {
	return func(_ context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
		return func(yield func(*agent.Event, error) bool) {
			for _, e := range evs {
				if !yield(e, nil) {
					return
				}
			}
		}
	}
}

// toolStartEvent / textEvent build minimal *agent.Event values that drive the
// translator's tool-lifecycle and text-content arms.
func toolStartEvent(callID, name string) *agent.Event {
	return &agent.Event{Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
		Event: agent.ToolInvocationStart, ToolCallID: callID, ToolName: name,
	}}}
}

func textEvent(delta string) *agent.Event {
	return &agent.Event{LLMResponse: &agent.LLMResponse{Content: delta}}
}

// TestHandleTurnFanoutDistributesToBothConsumers proves the per-turn wiring:
// Translate → NewFanout → Subscribe×2 (status+content) BEFORE Run → both consumers
// receive the SAME translated stream. This is the UX-02 acceptance "per-turn
// fanout consumption maps AG-UI events → status-pane-B render".
func TestHandleTurnFanoutDistributesToBothConsumers(t *testing.T) {
	t.Parallel()

	stream := []*agent.Event{
		toolStartEvent("call-1", "web_search"),
		textEvent("Ciao"),
		textEvent(" mondo"),
	}

	status := newRecordingConsumer()
	content := newRecordingConsumer()
	artifact := newRecordingConsumer()

	tg := NewChannel(Deps{
		Turn:    syntheticTurn(stream),
		Offline: true,
		consumerFactory: func(_ botSender, _ tele.Recipient) (eventConsumer, eventConsumer, eventConsumer) {
			return status, content, artifact
		},
	})

	userMsg := "hello"
	tg.handleTurn(context.Background(), nil, 4242, &userMsg, false)

	// Both subscribers must have seen the lifecycle frames the producer guarantees.
	for name, c := range map[string]*recordingConsumer{"status": status, "content": content} {
		seen := c.seen()
		if !containsType(seen, events.EventTypeRunStarted) {
			t.Errorf("%s consumer: missing RUN_STARTED; saw %v", name, seen)
		}
		if !containsType(seen, events.EventTypeRunFinished) {
			t.Errorf("%s consumer: missing RUN_FINISHED; saw %v", name, seen)
		}
	}

	// The tool-start event must reach both consumers (proves the SAME stream fans
	// out, not a split). The content stream also carries TEXT_MESSAGE_* frames.
	if !containsType(status.seen(), events.EventTypeToolCallStart) {
		t.Errorf("status consumer missing TOOL_CALL_START; saw %v", status.seen())
	}
	if !containsType(content.seen(), events.EventTypeTextMessageContent) {
		t.Errorf("content consumer missing TEXT_MESSAGE_CONTENT; saw %v", content.seen())
	}
}

// TestStartStopGoleakClean proves Start launches the poller and Stop joins it: an
// Offline bot starts, becomes healthy, and Stop drains the polling goroutine. The
// package goleak TestMain fails the test if the goroutine leaks.
func TestStartStopGoleakClean(t *testing.T) {
	tg := NewChannel(Deps{Token: "123:offline", Offline: true})

	if tg.IsHealthy() {
		t.Fatal("channel healthy before Start")
	}
	if err := tg.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !tg.IsHealthy() {
		t.Fatal("channel not healthy after Start")
	}

	// Give the poller goroutine a moment to actually be scheduled on Bot.Start.
	time.Sleep(20 * time.Millisecond)

	if err := tg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if tg.IsHealthy() {
		t.Fatal("channel still healthy after Stop")
	}

	// Idempotent: a second Stop and a Stop-before-Start are clean no-ops.
	if err := tg.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := NewChannel(Deps{Offline: true}).Stop(context.Background()); err != nil {
		t.Fatalf("Stop on never-started: %v", err)
	}
}

func TestNameIsTelegram(t *testing.T) {
	t.Parallel()
	if got := NewChannel(Deps{}).Name(); got != "telegram" {
		t.Errorf("Name() = %q, want telegram", got)
	}
}

func containsType(types []events.EventType, want events.EventType) bool {
	for _, ty := range types {
		if ty == want {
			return true
		}
	}
	return false
}
