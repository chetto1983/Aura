package telegram

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	assetspkg "github.com/chetto1983/aura/internal/assets"
	tele "gopkg.in/telebot.v4"
)

func TestOnVoiceRoutesThroughAssetIngressWhenConfigured(t *testing.T) {
	t.Parallel()
	ogg := []byte("OggS\x00opus")
	rt := &recordingTurn{}
	assetIngress := &recordingAssetIngress{
		asset: assetspkg.Asset{
			ID:         "asset-voice",
			IdentityID: profileAccount().IdentityID,
			SourceKind: assetspkg.SourceTelegram,
			Modality:   assetspkg.ModalityAudio,
			Status:     assetspkg.StatusComplete,
			FileName:   "voice.ogg",
			Summary:    "ciao da nota vocale",
		},
	}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = assetIngress
	})

	bot := &dispatchBot{ogg: ogg}
	assetIngress.bot = bot
	msg := chatMsg(11)
	msg.Voice = &tele.Voice{
		File: tele.File{FileID: "voice-file", FileSize: int64(len(ogg))},
		MIME: "audio/ogg",
	}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice: %v", err)
	}
	tg.wg.Wait()

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("an asset voice note must drive 1 turn with attachment context, got %d", calls)
	}
	if !strings.Contains(msgs[0], `<attachments trust="untrusted_user_uploads">`) ||
		!strings.Contains(msgs[0], "asset-voice") ||
		!strings.Contains(msgs[0], "ciao da nota vocale") {
		t.Fatalf("turn userMsg missing protected asset context:\n%s", msgs[0])
	}
	if assetIngress.calls != 1 {
		t.Fatalf("asset ingress calls = %d, want 1", assetIngress.calls)
	}
	req := assetIngress.reqs[0]
	if req.IdentityID != profileAccount().IdentityID || req.ChatID != 11 || req.FileID != "voice-file" ||
		req.FileName != "voice.ogg" || req.MIMEType != "audio/ogg" || req.Modality != assetspkg.ModalityAudio ||
		req.SizeBytes != int64(len(ogg)) {
		t.Fatalf("voice asset request = %+v, want linked identity and voice metadata", req)
	}
	if assetIngress.payloads[0] != string(ogg) {
		t.Fatalf("voice asset payload = %q, want OGG bytes", assetIngress.payloads[0])
	}
	if assetIngress.readsBeforeIngest[0] != 0 {
		t.Fatalf("handler read Telegram stream before asset ingress: reads=%d", assetIngress.readsBeforeIngest[0])
	}
}

type recordingAssetIngress struct {
	bot               *dispatchBot
	asset             assetspkg.Asset
	err               error
	calls             int
	reqs              []assetspkg.TelegramIngestRequest
	payloads          []string
	readsBeforeIngest []int
}

func (r *recordingAssetIngress) IngestTelegramFile(_ context.Context, req assetspkg.TelegramIngestRequest) (assetspkg.Asset, error) {
	r.calls++
	if r.bot != nil {
		r.readsBeforeIngest = append(r.readsBeforeIngest, r.bot.readCount())
	}
	r.reqs = append(r.reqs, req)
	payload, err := io.ReadAll(req.Reader)
	if err != nil {
		return assetspkg.Asset{}, err
	}
	r.payloads = append(r.payloads, string(payload))
	if r.err != nil {
		return assetspkg.Asset{}, r.err
	}
	return r.asset, nil
}

func (r *recordingAssetIngress) GetForIdentity(_ context.Context, assetID, identityID string) (assetspkg.Asset, error) {
	if r.asset.ID == assetID && r.asset.IdentityID == identityID {
		return r.asset, nil
	}
	return assetspkg.Asset{}, errors.New("asset not found")
}
