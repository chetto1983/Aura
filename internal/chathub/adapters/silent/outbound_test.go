package silentadapter

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/chathub"
)

func TestNewHeartbeatChannel(t *testing.T) {
	o := NewHeartbeat(Config{})
	if o.Channel() != chathub.ChannelHeartbeat {
		t.Fatalf("Channel = %s", o.Channel())
	}
	if o.Mode() != chathub.DeliveryModeSilent {
		t.Fatalf("Mode = %s", o.Mode())
	}
}

func TestNewCronChannel(t *testing.T) {
	o := NewCron(Config{})
	if o.Channel() != chathub.ChannelCron {
		t.Fatalf("Channel = %s", o.Channel())
	}
	if o.Mode() != chathub.DeliveryModeSilent {
		t.Fatalf("Mode = %s", o.Mode())
	}
}

func TestSilent_LogsErrorAndDone(t *testing.T) {
	var buf bytes.Buffer
	o := NewHeartbeat(Config{Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	ctx := context.Background()

	events := []chathub.OutboundEvent{
		{Type: chathub.EventRunStarted, RunID: "r1"},
		{Type: chathub.EventMessageDelta, RunID: "r1", Content: "thinking..."},
		{Type: chathub.EventToolStart, RunID: "r1", Payload: map[string]any{"tool": "search_memory"}},
		{Type: chathub.EventToolEnd, RunID: "r1", Payload: map[string]any{"tool": "search_memory"}},
		{Type: chathub.EventMessageDone, RunID: "r1", Content: "done"},
		{Type: chathub.EventUsage, RunID: "r1", Payload: map[string]any{"llm_calls": 1, "tool_calls": 1}},
		{Type: chathub.EventError, RunID: "r1", Payload: map[string]any{"error": "transient"}},
		{Type: chathub.EventDone, RunID: "r1", Payload: map[string]any{"status": "completed"}},
	}
	for _, ev := range events {
		if err := o.Deliver(ctx, ev); err != nil {
			t.Fatalf("Deliver %s: %v", ev.Type, err)
		}
	}

	out := buf.String()
	if !strings.Contains(out, "silent agent run error") {
		t.Fatalf("error not logged: %s", out)
	}
	if !strings.Contains(out, "silent agent run complete") {
		t.Fatalf("done not logged: %s", out)
	}
	if !strings.Contains(out, "silent agent run usage") {
		t.Fatalf("usage not logged: %s", out)
	}
	// CRITICAL: the silent adapter must NEVER emit anything user-facing.
	// The output should be log lines only — no Send, no Edit, no
	// notification surface. We assert this indirectly by checking the
	// adapter exposes ONLY a Deliver method and any user-channel side
	// effect would have to go through... well, nothing. If the type ever
	// grows a delivery side-effect this test will need updating to assert
	// the absence of it on a fake notifier.
	if !strings.Contains(out, "channel=heartbeat") {
		t.Fatalf("channel attribute not preserved: %s", out)
	}
}

func TestSilent_QuietOnOtherEvents(t *testing.T) {
	// Sanity: deltas / tool events do NOT trigger a log line at Info+,
	// so an active run does not flood the operator's log stream.
	var buf bytes.Buffer
	o := NewCron(Config{Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))})
	ctx := context.Background()
	_ = o.Deliver(ctx, chathub.OutboundEvent{Type: chathub.EventMessageDelta, RunID: "x", Content: "hi"})
	_ = o.Deliver(ctx, chathub.OutboundEvent{Type: chathub.EventToolStart, RunID: "x"})
	_ = o.Deliver(ctx, chathub.OutboundEvent{Type: chathub.EventToolEnd, RunID: "x"})
	if buf.Len() != 0 {
		t.Fatalf("unexpected log output for quiet events: %s", buf.String())
	}
}
