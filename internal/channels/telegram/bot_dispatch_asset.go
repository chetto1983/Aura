package telegram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chetto1983/aura/internal/assets"
	tele "gopkg.in/telebot.v4"
)

func (t *Telegram) ingestTelegramAsset(
	ctx context.Context,
	c tele.Context,
	msg *tele.Message,
	file *tele.File,
	fileName string,
	mimeType string,
	modality assets.Modality,
	sizeBytes int64,
	failCopy string,
	inboundWasVoice bool,
) error {
	account, err := t.linkedAccountForMessage(ctx, msg)
	if err != nil {
		slog.Warn("telegram: resolve linked account for asset", "chat", msg.Chat.ID, "err", err)
		t.reply(c, activationRequiredMsg)
		return nil
	}
	filer, ok := c.Bot().(botFiler)
	if !ok {
		return nil
	}
	rc, err := openTelegramFile(filer, file)
	if err != nil {
		slog.Warn("telegram: open asset file failed", "chat", msg.Chat.ID, "err", err)
		t.handleAssetIngressFailure(c, msg, failCopy, inboundWasVoice)
		return nil
	}
	defer func() { _ = rc.Close() }()
	asset, err := t.deps.Assets.IngestTelegramFile(ctx, assets.TelegramIngestRequest{
		IdentityID: account.IdentityID,
		ChatID:     msg.Chat.ID,
		MessageID:  msg.ID,
		FileID:     file.FileID,
		FileName:   fileName,
		MIMEType:   mimeType,
		Modality:   modality,
		SizeBytes:  sizeBytes,
		Reader:     rc,
	})
	if err != nil {
		slog.Warn("telegram: asset ingest failed", "chat", msg.Chat.ID, "err", err)
		t.handleAssetIngressFailure(c, msg, failCopy, inboundWasVoice)
		return nil
	}
	userText := assets.WithAttachmentBlock("Analizza l'allegato Telegram.", []assets.Asset{asset})
	t.runTurn(ctx, c, msg.Chat.ID, userText, inboundWasVoice)
	return nil
}

func (t *Telegram) handleAssetIngressFailure(c tele.Context, msg *tele.Message, failCopy string, inboundWasVoice bool) {
	t.reply(c, failCopy)
	if inboundWasVoice && t.voice != nil {
		if reactor, ok := c.Bot().(botReactor); ok {
			_ = t.voice.HardFail(reactor, tele.ChatID(msg.Chat.ID), msg)
		}
	}
}

func (t *Telegram) linkedAccountForMessage(ctx context.Context, msg *tele.Message) (Account, error) {
	accounts := t.accountsForDispatch()
	if accounts == nil {
		return Account{}, fmt.Errorf("telegram account resolver is not configured")
	}
	return accounts.GetAccountByTelegramID(ctx, telegramUserIDFromMessage(msg))
}

func telegramVoiceFileName(voice *tele.Voice) string {
	return "voice.ogg"
}

func telegramVoiceMIME(voice *tele.Voice) string {
	if voice != nil && voice.MIME != "" {
		return voice.MIME
	}
	return "audio/ogg"
}
