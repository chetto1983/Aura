// Package telegram — deferred document turns (amendment #199). A document turn must
// start only when the index actually holds the document: measured 2026-08-30, a turn
// started at ingest got a document_search miss (the sidecar was still extracting) and
// the model confabulated the PDF's content. The wait itself is the shared pipeline's
// (assets.WaitDocumentIndexed, the same DocumentScope seam the knowledge catalog
// asks); this file only renders the status on the channel, per the wrapper rule.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	tele "gopkg.in/telebot.v4"
)

// docIndexWaitTimeout bounds the wait for the sidecar. Indexing measured at 37s on
// 2026-08-17 for a small upload; 3 minutes covers a fat PDF without leaving the
// operator hanging forever.
const docIndexWaitTimeout = 3 * time.Minute

const (
	docReceivedMessage  = "📄 Ho ricevuto %s: lo sto indicizzando, ti rispondo appena è pronto."
	docIndexedMessage   = "✅ %s indicizzato: ci lavoro ora."
	docIndexLateMessage = "⏳ L'indicizzazione di %s non è ancora finita: riprova a chiedermelo tra qualche minuto."
)

// startDocumentTurnWhenIndexed tells the operator the document arrived, then waits —
// off the poller goroutine — for the index to hold it before driving the turn. The
// tele.Context is recycled once the handler returns, so everything the deferred turn
// needs (sender, recipient, message id, the asset's own identity) is captured NOW.
// On a wait timeout the turn does NOT start: an honest "not yet" beats a confident
// invention.
func (t *Telegram) startDocumentTurnWhenIndexed(daemonCtx context.Context, c tele.Context, chatID int64, text string, asset assets.Asset) {
	t.reply(c, fmt.Sprintf(docReceivedMessage, asset.FileName))
	sender := t.sender(c)
	notifier, _ := c.Bot().(botNotifier)
	to := c.Recipient()
	messageID := 0
	if msg := c.Message(); msg != nil {
		messageID = msg.ID
	}
	t.wg.Go(func() {
		waitCtx, cancel := context.WithTimeout(daemonCtx, docIndexWaitTimeout)
		defer cancel()
		if err := t.deps.Assets.WaitDocumentIndexed(waitCtx, asset.IdentityID, asset.DocumentID); err != nil {
			slog.Warn("telegram: document index wait failed", "chat", chatID, "document", asset.DocumentID, "err", err)
			t.sendPlain(sender, chatID, fmt.Sprintf(docIndexLateMessage, asset.FileName))
			return
		}
		t.sendPlain(sender, chatID, fmt.Sprintf(docIndexedMessage, asset.FileName))
		// Composed AFTER indexing on purpose: the attachment block's retrieval line and
		// the thread catalog now describe a document that document_search can really find.
		composed := t.deps.Assets.BuildTurnContext(daemonCtx, asset.IdentityID, convID(chatID), []assets.Asset{asset}, text)
		t.startTurn(daemonCtx, sender, notifier, to, chatID, messageID, &composed, false, func() {
			if t.enqueuePendingTurn(chatID, composed, false) {
				t.sendPlain(sender, chatID, turnQueuedForNextTurnMessage)
				return
			}
			t.sendPlain(sender, chatID, turnQueueFullMessage)
		})
	})
}
