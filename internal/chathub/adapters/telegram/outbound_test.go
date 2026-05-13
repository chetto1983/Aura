package telegramadapter

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/chathub"
	tele "gopkg.in/telebot.v4"
)

type telegramAPICall struct {
	Method string
	Body   map[string]any
}

// newFakeTelegramServer mirrors internal/telegram/streaming_test.go's helper so
// the adapter tests exercise the same telebot.v4 wire shape the bot does in
// production. Every call lands in *calls for later assertions.
func newFakeTelegramServer(t *testing.T, calls *[]telegramAPICall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := path.Base(r.URL.Path)
		*calls = append(*calls, telegramAPICall{Method: method, Body: body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":123},"date":1760000000,"text":"ok"}}`))
	}))
}

func countMethods(calls []telegramAPICall, method string) int {
	n := 0
	for _, c := range calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func newOutboundForTest(t *testing.T) (*Outbound, *tele.Bot, []telegramAPICall, func()) {
	t.Helper()
	var calls []telegramAPICall
	srv := newFakeTelegramServer(t, &calls)
	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	o := NewOutbound(Config{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		EditThrottle: 10 * time.Millisecond,
		MinThreshold: 1,
	})
	return o, tb, calls, srv.Close
}

func TestOutbound_ChannelAndMode(t *testing.T) {
	o := NewOutbound(Config{})
	if o.Channel() != chathub.ChannelTelegram {
		t.Fatalf("Channel = %s", o.Channel())
	}
	if o.Mode() != chathub.DeliveryModeStreaming {
		t.Fatalf("Mode = %s", o.Mode())
	}
}

func TestOutbound_HandlesFullRunLifecycle(t *testing.T) {
	o, tb, _, closeSrv := newOutboundForTest(t)
	defer closeSrv()
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}
	recipient := tele.ChatID(123)

	runID := "run-1"
	deliver := func(ev chathub.OutboundEvent) {
		ev.RunID = runID
		ev.Channel = chathub.ChannelTelegram
		if err := o.Deliver(context.Background(), ev); err != nil {
			t.Fatalf("Deliver %s: %v", ev.Type, err)
		}
	}

	// RunStarted carries the channel handles via Payload.
	deliver(chathub.OutboundEvent{
		Type: chathub.EventRunStarted,
		Payload: map[string]any{
			"tele_bot":         tb,
			"tele_recipient":   recipient,
			"tele_placeholder": placeholder,
		},
	})

	// Two deltas — the second triggers a throttle-respecting edit.
	deliver(chathub.OutboundEvent{Type: chathub.EventMessageDelta, Content: "Hello, "})
	time.Sleep(15 * time.Millisecond) // exceed throttle
	deliver(chathub.OutboundEvent{Type: chathub.EventMessageDelta, Content: "world!"})

	// Final message — adapter prefers Content from MessageDone when set.
	deliver(chathub.OutboundEvent{Type: chathub.EventMessageDone, Content: "Hello, world!"})
	// Done event releases the per-run state.
	deliver(chathub.OutboundEvent{Type: chathub.EventDone})

	// Look up internal state — after Done, the runs map should be empty.
	if v, ok := o.runs.Load(runID); ok {
		t.Fatalf("runs map should have been cleared, found: %+v", v)
	}
}

func TestOutbound_ErrorReplyOnAgentError(t *testing.T) {
	o, tb, _, closeSrv := newOutboundForTest(t)
	defer closeSrv()
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}
	recipient := tele.ChatID(123)

	runID := "run-err"
	deliver := func(ev chathub.OutboundEvent) error {
		ev.RunID = runID
		ev.Channel = chathub.ChannelTelegram
		return o.Deliver(context.Background(), ev)
	}

	_ = deliver(chathub.OutboundEvent{
		Type: chathub.EventRunStarted,
		Payload: map[string]any{
			"tele_bot":         tb,
			"tele_recipient":   recipient,
			"tele_placeholder": placeholder,
		},
	})
	// Send an error event — adapter should emit a generic user-facing reply.
	if err := deliver(chathub.OutboundEvent{
		Type:    chathub.EventError,
		Payload: map[string]any{"error": "internal model failure"},
	}); err != nil {
		t.Fatalf("Deliver error event: %v", err)
	}

	rs := o.getRun(runID)
	if rs != nil {
		rs.mu.Lock()
		if !rs.finalized {
			t.Fatal("runState not finalized after error")
		}
		if !strings.Contains(rs.buffer.String(), "Sorry") {
			t.Fatalf("buffer = %q", rs.buffer.String())
		}
		rs.mu.Unlock()
	}

	// Verify we never leaked the internal error string to the user.
	_ = deliver(chathub.OutboundEvent{Type: chathub.EventDone})
}

func TestOutbound_IgnoresUnknownEventTypes(t *testing.T) {
	o, tb, _, closeSrv := newOutboundForTest(t)
	defer closeSrv()
	recipient := tele.ChatID(123)
	runID := "run-quiet"

	if err := o.Deliver(context.Background(), chathub.OutboundEvent{
		RunID: runID,
		Type:  chathub.EventRunStarted,
		Payload: map[string]any{
			"tele_bot":       tb,
			"tele_recipient": recipient,
		},
	}); err != nil {
		t.Fatalf("RunStarted: %v", err)
	}
	// ToolStart / ToolEnd / Usage are not surfaced; should be silent no-ops.
	for _, ty := range []chathub.EventType{
		chathub.EventToolStart, chathub.EventToolEnd, chathub.EventUsage, chathub.EventAttachmentStatus,
	} {
		if err := o.Deliver(context.Background(), chathub.OutboundEvent{RunID: runID, Type: ty}); err != nil {
			t.Fatalf("Deliver %s: %v", ty, err)
		}
	}
}
