package telegram

import (
	"context"
	"sync"
	"time"

	tele "gopkg.in/telebot.v4"
)

// typingPulse refreshes the chat action under Telegram's ~5s expiry so the user
// sees a continuous "Aura sta scrivendo…" through the whole STT/OCR/turn latency.
const typingPulse = 4 * time.Second

// botNotifier is the narrow telebot surface for the "doing something" chat action
// (Telegram auto-clears it when the bot next sends a message). *tele.Bot satisfies
// it; tests inject a recorder.
type botNotifier interface {
	Notify(to tele.Recipient, action tele.ChatAction, threadID ...int) error
}

// keepWorking shows a persistent working indicator (the chat action, e.g. the
// "typing…" bubble) for the whole multi-second STT/OCR/convert + turn latency so
// the user gets immediate, lasting feedback instead of silence. It returns a stop
// func the caller MUST defer. No-op when the bot does not support Notify.
func keepWorking(ctx context.Context, c tele.Context, action tele.ChatAction) (stop func()) {
	n, ok := c.Bot().(botNotifier)
	if !ok {
		return func() {}
	}
	return pulseChatAction(ctx, n, c.Recipient(), action)
}

// pulseChatAction sends the chat action immediately and refreshes it every
// typingPulse until stop() is called. goleak-safe: stop() closes done AND joins the
// goroutine; the loop also exits on ctx cancellation (daemon shutdown). Best-effort
// — a Notify error (no permission, stale chat) is ignored.
func pulseChatAction(ctx context.Context, n botNotifier, to tele.Recipient, action tele.ChatAction) (stop func()) {
	if n == nil || to == nil {
		return func() {}
	}
	_ = n.Notify(to, action) // immediate feedback
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(typingPulse)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = n.Notify(to, action)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}
}
