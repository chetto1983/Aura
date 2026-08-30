// Package telegram — this file is the SINGLE inbound-attachment ingress. Every
// Telegram voice note, photo and document goes through it onto the shared asset
// pipeline (assets.Service), the same path a cockpit upload travels: object store →
// catalog row → the modality's processor → a turn carrying the attachment block.
// The channel carries bytes and renders outcomes; it does not decide how an
// attachment is read.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/assets"
	tele "gopkg.in/telebot.v4"
)

// defaultAttachmentTurnText drives the turn when the operator sent the file without a
// caption; a caption is the operator's own instruction and takes its place.
const defaultAttachmentTurnText = "Analizza l'allegato Telegram."

func (t *Telegram) ingestTelegramAsset(
	ctx context.Context,
	c tele.Context,
	msg *tele.Message,
	file *tele.File,
	fileName string,
	mimeType string,
	modality assets.Modality,
	sizeBytes int64,
	turnText string,
	failCopy string,
	inboundWasVoice bool,
) error {
	if t.deps.Assets == nil {
		// The composition root always wires this (serve_channels.go). Reaching here means
		// the channel was built without an asset service, and there is no second path to
		// fall back to any more — say so rather than dropping the file silently.
		slog.Error("telegram: attachment received but no asset service wired", "chat", msg.Chat.ID)
		t.handleAssetIngressFailure(c, msg, failCopy, inboundWasVoice)
		return nil
	}
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
	// Stream the Telegram file straight into the pipeline — never buffer it here. The
	// Bot-API getFile ceiling bounds the read upstream (T-13-10-MediaDoS) and the
	// per-modality cap is enforced by assets.Limits before a byte is stored.
	rc, err := filer.File(file)
	if err != nil {
		slog.Warn("telegram: open asset file failed", "chat", msg.Chat.ID, "err", err)
		t.handleAssetIngressFailure(c, msg, failCopy, inboundWasVoice)
		return nil
	}
	defer func() { _ = rc.Close() }()
	asset, err := t.deps.Assets.IngestTelegramFile(ctx, assets.TelegramIngestRequest{
		IdentityID: account.IdentityID,
		ThreadID:   convID(msg.Chat.ID),
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
		t.handleAssetIngressFailure(c, msg, refusalCopy(err, failCopy), inboundWasVoice)
		return nil
	}
	// The shared seam composes this asset's attachment block AND the thread's knowledge
	// catalog (other searchable docs) — same path as a no-attachment text turn.
	text := strings.TrimSpace(turnText)
	if text == "" {
		text = defaultAttachmentTurnText
	}
	t.runTurnWithAssets(ctx, c, msg.Chat.ID, text, []assets.Asset{asset}, inboundWasVoice)
	return nil
}

// refusalCopy maps a refused ingest onto copy the operator can act on. The size and
// format ceilings are the shared pipeline's (assets.Limits), so the ceiling moves in
// one place and the channel just reports it; anything else is ours to fix and keeps
// the caller's generic per-modality copy.
func refusalCopy(err error, failCopy string) string {
	switch {
	case errors.Is(err, assets.ErrAssetTooLarge):
		return assetTooLargeMessage
	case errors.Is(err, assets.ErrAssetUnsupported):
		return assetUnsupportedMessage
	default:
		return failCopy
	}
}

func (t *Telegram) handleAssetIngressFailure(c tele.Context, msg *tele.Message, failCopy string, inboundWasVoice bool) {
	t.reply(c, failCopy)
	if inboundWasVoice {
		if reactor, ok := c.Bot().(botReactor); ok {
			_ = reactHardFail(reactor, tele.ChatID(msg.Chat.ID), msg)
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

// telegramVoiceFileName is the name a voice note is stored under. Telegram voice
// notes carry no file name and are always OGG/Opus, so it is a constant.
func telegramVoiceFileName() string { return "voice.ogg" }

func telegramVoiceMIME(voice *tele.Voice) string {
	if voice != nil && voice.MIME != "" {
		return voice.MIME
	}
	return "audio/ogg"
}
