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
	"github.com/chetto1983/aura/internal/identityctx"
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
	// rawText is captured BEFORE composeTurnContext reassigns text below. A steer
	// must carry the operator's raw words, never the attachment block / knowledge
	// catalog composeTurnContext adds (T-52-33) — that treatment is for a fresh
	// turn only.
	rawText := text
	composedText := t.composeTurnContext(daemonCtx, c, chatID, attachments, text)
	sender := t.sender(c)
	notifier, _ := c.Bot().(botNotifier)
	to := c.Recipient()
	t.startTurn(daemonCtx, sender, notifier, to, chatID, &composedText, inboundWasVoice,
		t.onBusyRedirect(c, chatID, rawText, composedText, len(attachments) > 0, inboundWasVoice))
}

// onBusyRedirect builds startTurn's onBusy callback for a chat where registerTurn
// found a turn already live. With a wired steer inbox: plain text (no
// attachments) redirects the running turn (D-03), an attachment is HELD in the
// per-chat pending slot and delivered when the live turn ends (D-05,
// bot_dispatch_queue.go). An unwired inbox (the composition root's explicit
// rollback) keeps today's turnBusyMessage for both.
func (t *Telegram) onBusyRedirect(c tele.Context, chatID int64, rawText, composedText string, hasAttachments, inboundWasVoice bool) func() {
	return func() {
		if t.deps.Steer == nil {
			t.reply(c, turnBusyMessage)
			return
		}
		if hasAttachments {
			t.enqueueBusyTurn(c, chatID, composedText, inboundWasVoice)
			return
		}
		t.steerBusyTurn(c, chatID, rawText)
	}
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
	// D-23/D-24 per-user routing: scope the whole turn to the linked user's Aura
	// identity so every downstream store/tool/sidecar isolates to THEM (never the
	// local admin or a sibling user, T-36-11-I2). startTurn is the SINGLE choke point
	// every inbound-turn spawn passes through — a fresh message (runTurnWithAssets),
	// the async document-convert callback (bot_dispatch.go), and a HITL-resume
	// continuation (bot_dispatch_hitl.go) — so scoping here holds for all of them
	// without per-path duplication.
	//
	// FAIL CLOSED (HI-03): if the id does not resolve, DROP the turn rather than run it
	// on an unscoped ctx. An unscoped ctx would let every downstream resolver apply its
	// `local` (admin) fallback, so a group-chat / divergent-key update would impersonate
	// the operator. This is NOT the onBusy path (that is "a previous request is still
	// running"); a refused-because-unscoped turn simply does not start.
	scoped, ok := t.scopeTurnToIdentity(daemonCtx, chatID)
	if !ok {
		slog.Warn("telegram: unscoped turn refused (no linked identity)", "chat", chatID)
		return
	}
	daemonCtx = scoped

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
	t.wg.Go(func() {
		defer cancel()
		defer t.cmds.unregisterTurn(chatID)
		stop := pulseChatAction(turnCtx, notifier, to, tele.Typing) // "Aura is working" for the whole turn
		defer stop()
		t.handleTurn(turnCtx, sender, chatID, userMsg, inboundWasVoice)
		// deliverPendingTurn runs BEFORE the deferred unregisterTurn above (defers
		// fire in reverse order strictly at return, and this call sits before the
		// function returns) — so a media message queued while THIS turn was live is
		// delivered under the SAME registration and the SAME scoped turnCtx (D-05).
		t.deliverPendingTurn(turnCtx, sender, chatID)
	})
}

// scopeTurnToIdentity wraps ctx with the linked user's Aura identity id (D-23/D-24),
// resolved from the chat's telegram user id through the SAME account seam the
// reject-unlinked gate uses. Aura's Telegram is a personal DM channel — the private-chat
// gate (requireLinkedMessage) enforces msg.Chat.ID == msg.Sender.ID, so the gate's
// sender-id key and this per-chat scope key are provably the same id.
//
// It FAILS CLOSED (HI-03): on a nil resolver OR a GetAccountByTelegramID miss it returns
// (ctx, false) so startTurn DROPS the turn — an unresolved principal is NEVER left on an
// unscoped ctx, which every downstream resolver would silently upgrade to its `local`
// (admin) fallback. Only a resolved account returns (scoped ctx, true).
func (t *Telegram) scopeTurnToIdentity(ctx context.Context, chatID int64) (context.Context, bool) {
	accounts := t.accountsForDispatch()
	if accounts == nil {
		return ctx, false
	}
	account, err := accounts.GetAccountByTelegramID(ctx, chatID)
	if err != nil {
		return ctx, false
	}
	return identityctx.WithIdentityID(ctx, account.IdentityID), true
}

func (t *Telegram) sendBusy(sender botSender, chatID int64) {
	if sender == nil {
		return
	}
	if _, err := sender.Send(tele.ChatID(chatID), turnBusyMessage); err != nil {
		slog.Warn("telegram: busy reply send failed", "chat", chatID, "err", err)
	}
}
