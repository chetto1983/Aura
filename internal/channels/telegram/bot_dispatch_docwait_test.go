package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"

	assetspkg "github.com/chetto1983/aura/internal/assets"
)

func docwaitMsg(chatID int64, pdf []byte) *tele.Message {
	msg := chatMsg(chatID)
	msg.Document = &tele.Document{
		File:     tele.File{FileID: "doc-file", FileSize: int64(len(pdf))},
		FileName: "manual.pdf",
		MIME:     "application/pdf",
	}
	return msg
}

// TestOnDocumentWaitsForTheIndexAndReportsStatus pins amendment #199: the turn is
// gated on WaitDocumentIndexed and the operator sees the state on-channel.
func TestOnDocumentWaitsForTheIndexAndReportsStatus(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF small")
	rt := &recordingTurn{}
	ingress := &recordingAssetIngress{
		asset: assetspkg.Asset{
			ID:         "asset-doc",
			IdentityID: profileAccount().IdentityID,
			SourceKind: assetspkg.SourceTelegram,
			Modality:   assetspkg.ModalityDocument,
			Status:     assetspkg.StatusProcessing,
			FileName:   "manual.pdf",
			MIMEType:   "application/pdf",
			DocumentID: "doc-1",
		},
	}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Assets = ingress })

	bot := &dispatchBot{ogg: pdf}
	ingress.bot = bot
	if err := tg.onDocument(context.Background())(msgContext(bot, docwaitMsg(41, pdf))); err != nil {
		t.Fatalf("onDocument: %v", err)
	}
	tg.wg.Wait()

	if len(ingress.waits) != 1 || ingress.waits[0] != "doc-1" {
		t.Fatalf("the turn must be gated on WaitDocumentIndexed(doc-1), got %v", ingress.waits)
	}
	calls, _ := rt.snapshot()
	if calls != 1 {
		t.Fatalf("an indexed document must drive exactly 1 turn, got %d", calls)
	}
	joined := strings.Join(bot.sentTexts(), "\n")
	if !strings.Contains(joined, "Ho ricevuto manual.pdf") {
		t.Fatalf("the operator must be told the document arrived, sent=%q", joined)
	}
	if !strings.Contains(joined, "manual.pdf indicizzato") {
		t.Fatalf("the operator must be told indexing finished, sent=%q", joined)
	}
}

// TestOnDocumentIndexTimeoutSkipsTheTurn: when the index never answers, the turn
// must NOT start — an honest "not yet" instead of a confabulated reading.
func TestOnDocumentIndexTimeoutSkipsTheTurn(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF small")
	rt := &recordingTurn{}
	ingress := &recordingAssetIngress{
		waitErr: errors.New("context deadline exceeded"),
		asset: assetspkg.Asset{
			ID:         "asset-doc",
			IdentityID: profileAccount().IdentityID,
			SourceKind: assetspkg.SourceTelegram,
			Modality:   assetspkg.ModalityDocument,
			Status:     assetspkg.StatusProcessing,
			FileName:   "manual.pdf",
			MIMEType:   "application/pdf",
			DocumentID: "doc-1",
		},
	}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Assets = ingress })

	bot := &dispatchBot{ogg: pdf}
	ingress.bot = bot
	if err := tg.onDocument(context.Background())(msgContext(bot, docwaitMsg(42, pdf))); err != nil {
		t.Fatalf("onDocument: %v", err)
	}
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("a document the index does not hold must not start a turn, got %d", calls)
	}
	joined := strings.Join(bot.sentTexts(), "\n")
	if !strings.Contains(joined, "non è ancora finita") {
		t.Fatalf("the operator must be told indexing is late, sent=%q", joined)
	}
}
