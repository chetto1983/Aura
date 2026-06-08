package telegram

import (
	"context"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"
)

type recordingNotifier struct {
	mu sync.Mutex
	n  int
}

func (r *recordingNotifier) Notify(_ tele.Recipient, _ tele.ChatAction, _ ...int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return nil
}

func (r *recordingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
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
