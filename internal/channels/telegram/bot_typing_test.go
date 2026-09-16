package telegram

import (
	"context"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"
)

// recordingNotifier is the botNotifier double: it counts the chat actions the pulse
// sends AND keeps them in order, so the media-action tests can assert WHICH action
// the single turn-wide pulse carried at each tick.
type recordingNotifier struct {
	mu   sync.Mutex
	sent []tele.ChatAction
}

func (r *recordingNotifier) Notify(_ tele.Recipient, action tele.ChatAction, _ ...int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, action)
	return nil
}

func (r *recordingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// chatActions returns a copy of the recorded actions in send order.
func (r *recordingNotifier) chatActions() []tele.ChatAction {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]tele.ChatAction, len(r.sent))
	copy(out, r.sent)
	return out
}

// TestPulseChatAction proves the working indicator fires immediately and stop()
// cleanly joins the refresh goroutine (goleak TestMain catches a leak). This is the
// "Aura is working" feedback the operator asked for during the STT/OCR/turn wait.
func TestPulseChatAction(t *testing.T) {
	t.Parallel()
	rn := &recordingNotifier{}
	stop := pulseChatAction(context.Background(), rn, tele.ChatID(7), tele.Typing)
	if rn.count() < 1 {
		t.Fatalf("expected an immediate chat action, got %d", rn.count())
	}
	stop() // must return (joins the goroutine) — a hang fails the test by timeout
	stop() // idempotent: a second stop must not panic on a closed channel
}

// TestPulseChatAction_NilSafe proves a nil notifier/recipient degrades to a no-op
// stop (the channel still serves turns when the bot lacks Notify).
func TestPulseChatAction_NilSafe(t *testing.T) {
	t.Parallel()
	pulseChatAction(context.Background(), nil, tele.ChatID(1), tele.Typing)() // no panic
	pulseChatAction(context.Background(), &recordingNotifier{}, nil, tele.Typing)()
}
