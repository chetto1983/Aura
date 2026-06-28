// Package telegram — this file is the per-turn LIFECYCLE: the single inbound-turn
// entry point (runTurn / runTurnWithAssets), the channel-agnostic turn-context
// injection (composeTurnContext), and the off-poller turn spawn (startTurn). Split out
// of bot_dispatch.go (refactor-on-touch, CLAUDE.md ≤600 LOC) so the dispatch file keeps
// only the inbound routing.
package telegram

import (
	"context"
	"log/slog"

	"github.com/chetto1983/aura/internal/assets"
	tele "gopkg.in/telebot.v4"
)

// runTurn drives ONE inbound turn through the per-turn fanout under a cancellable
// ctx registered with the command dispatcher so a concurrent /cancel aborts it
// (SC#3). The cancel func is deregistered when the turn returns so a later /cancel
// never fires a stale one. inboundWasVoice flags the echo-modality TTS-out path
// (plan 13-10 Task 3 reads it).
func (t *Telegram) runTurn(daemonCtx context.Context, c tele.Context, chatID int64, text string, inboundWasVoice bool) {
	t.runTurnWithAssets(daemonCtx, c, chatID, text, nil, inboundWasVoice)
}

// runTurnWithAssets is the single inbound-turn entry point: it injects the
// channel-agnostic turn context (this turn's attachments + the thread's knowledge
// catalog) once, then drives the turn. Text handlers call runTurn (nil attachments);
// the asset handler passes the just-ingested asset so its attachment block and the
// catalog of the thread's other docs are composed by the SAME shared seam the AG-UI
// gateway uses — no per-channel duplication.
func (t *Telegram) runTurnWithAssets(daemonCtx context.Context, c tele.Context, chatID int64, text string, attachments []assets.Asset, inboundWasVoice bool) {
	text = t.composeTurnContext(daemonCtx, c, chatID, attachments, text)
	sender := t.sender(c)
	notifier, _ := c.Bot().(botNotifier)
	to := c.Recipient()
	t.startTurn(daemonCtx, sender, notifier, to, chatID, &text, inboundWasVoice,
		func() { t.reply(c, turnBusyMessage) })
}

// composeTurnContext augments the inbound text with the channel-agnostic per-turn
// context via the shared assets seam, keyed by the resolved identity and the chat's
// conversation thread (convID). It is best-effort: with no asset service it returns the
// text unchanged; an unresolved account drops only the catalog (the attachment block,
// which needs no identity, still renders). It MUST run on the handler goroutine while
// the tele.Context is still live (startTurn captures the text and recycles c).
func (t *Telegram) composeTurnContext(ctx context.Context, c tele.Context, chatID int64, attachments []assets.Asset, text string) string {
	if t.deps.Assets == nil {
		return text
	}
	var identityID string
	if msg := c.Message(); msg != nil {
		if account, err := t.linkedAccountForMessage(ctx, msg); err == nil {
			identityID = account.IdentityID
		}
	}
	return t.deps.Assets.BuildTurnContext(ctx, identityID, convID(chatID), attachments, text)
}

func (t *Telegram) startTurn(
	daemonCtx context.Context,
	sender botSender,
	notifier botNotifier,
	to tele.Recipient,
	chatID int64,
	text *string,
	inboundWasVoice bool,
	onBusy func(),
) {
	if t.deps.Turn == nil {
		slog.Warn("telegram: inbound message but no turn driver wired", "chat", chatID)
		return
	}
	turnCtx, cancel := context.WithCancel(daemonCtx)
	if !t.cmds.registerTurn(chatID, cancel) {
		cancel()
		if onBusy != nil {
			onBusy()
		}
		return
	}
	// Run the turn OFF the telebot handler goroutine: telebot dispatches updates
	// sequentially, so a synchronous turn blocks the poller — a long/hung turn would
	// freeze the whole bot AND make /cancel undeliverable (it can never reach its
	// handler to fire the cancel). Capture the stable bot + recipient before spawning
	// (the tele.Context is recycled once the handler returns). The goroutine is
	// tracked by t.wg so Stop drains it (goleak-clean).
	var userMsg *string
	if text != nil {
		msg := *text
		userMsg = &msg
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer cancel()
		defer t.cmds.unregisterTurn(chatID)
		stop := pulseChatAction(turnCtx, notifier, to, tele.Typing) // "Aura is working" for the whole turn
		defer stop()
		t.handleTurn(turnCtx, sender, chatID, userMsg, inboundWasVoice)
	}()
}

func (t *Telegram) sendBusy(sender botSender, chatID int64) {
	if sender == nil {
		return
	}
	if _, err := sender.Send(tele.ChatID(chatID), turnBusyMessage); err != nil {
		slog.Warn("telegram: busy reply send failed", "chat", chatID, "err", err)
	}
}
