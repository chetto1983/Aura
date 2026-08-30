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

// TestAttachmentFileOpenFailureSendsFailCopy covers the download leg: if the Telegram
// file server will not hand over the bytes there is nothing to ingest, so the operator
// gets the per-modality copy and the pipeline is never called with a broken stream.
func TestAttachmentFileOpenFailureSendsFailCopy(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	ingress := &recordingAssetIngress{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = ingress
	})

	bot := &failingFileBot{dispatchBot: &dispatchBot{}}
	msg := chatMsg(41)
	msg.Document = &tele.Document{File: tele.File{FileID: "doc"}, FileName: "doc.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}

	if ingress.calls != 0 {
		t.Errorf("a failed download must not reach the pipeline, got %d ingest calls", ingress.calls)
	}
	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a failed download must not drive a turn, got %d", calls)
	}
	if texts := bot.sentTexts(); len(texts) != 1 || !strings.Contains(texts[0], convertFailMessage) {
		t.Errorf("a failed download must send the fail copy, got %v", texts)
	}
}

// TestMediaHandlersIgnoreMalformedUpdates pins the nil-guards on all three handlers.
// telebot can dispatch an update whose media pointer is absent; the handler must be a
// clean no-op rather than panicking the poller for every later message.
func TestMediaHandlersIgnoreMalformedUpdates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler func(*Telegram) tele.HandlerFunc
		build   func() *tele.Message
	}{
		{"voice without payload", func(tg *Telegram) tele.HandlerFunc { return tg.onVoice(context.Background()) }, func() *tele.Message { return chatMsg(51) }},
		{"photo without payload", func(tg *Telegram) tele.HandlerFunc { return tg.onPhoto(context.Background()) }, func() *tele.Message { return chatMsg(52) }},
		{"document without payload", func(tg *Telegram) tele.HandlerFunc { return tg.onDocument(context.Background()) }, func() *tele.Message { return chatMsg(53) }},
		{"voice without chat", func(tg *Telegram) tele.HandlerFunc { return tg.onVoice(context.Background()) }, func() *tele.Message {
			return &tele.Message{Voice: &tele.Voice{}}
		}},
		{"photo without chat", func(tg *Telegram) tele.HandlerFunc { return tg.onPhoto(context.Background()) }, func() *tele.Message {
			return &tele.Message{Photo: &tele.Photo{}}
		}},
		{"document without chat", func(tg *Telegram) tele.HandlerFunc { return tg.onDocument(context.Background()) }, func() *tele.Message {
			return &tele.Message{Document: &tele.Document{}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rt := &recordingTurn{}
			ingress := &recordingAssetIngress{}
			tg := dispatchChannel(t, rt, func(d *Deps) { d.Assets = ingress })
			bot := &dispatchBot{}
			if err := tc.handler(tg)(msgContext(bot, tc.build())); err != nil {
				t.Fatalf("handler on a malformed update: %v", err)
			}
			if ingress.calls != 0 {
				t.Errorf("a malformed update must not reach the pipeline, got %d", ingress.calls)
			}
			if calls, _ := rt.snapshot(); calls != 0 {
				t.Errorf("a malformed update must not drive a turn, got %d", calls)
			}
		})
	}
}

// TestAttachmentFromUnlinkedAccountAsksForActivation covers the defensive re-resolve
// inside the ingress: the auth gate passed, but the account is gone by the time the
// owner is stamped on the asset. An asset written without an owner is invisible to
// every identity-scoped read, so refusing is the only correct outcome.
func TestAttachmentFromUnlinkedAccountAsksForActivation(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	ingress := &recordingAssetIngress{}
	accounts := &vanishingAccounts{acct: profileAccount(), okCalls: 1}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = ingress
		d.profileAccounts = accounts
	})

	bot := &dispatchBot{ogg: []byte("%PDF")}
	msg := chatMsg(42)
	msg.Document = &tele.Document{File: tele.File{FileID: "doc"}, FileName: "doc.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}

	if ingress.calls != 0 {
		t.Errorf("an unresolved owner must not produce an asset, got %d ingest calls", ingress.calls)
	}
	if texts := bot.sentTexts(); len(texts) != 1 || !strings.Contains(texts[0], "non e collegata") {
		t.Errorf("an unresolved owner must get the activation copy, got %v", texts)
	}
}

// vanishingAccounts resolves for the first okCalls lookups and then fails, modelling an
// account unlinked between the auth gate and the owner stamp.
type vanishingAccounts struct {
	acct    Account
	okCalls int
	calls   int
}

func (v *vanishingAccounts) GetAccountByTelegramID(context.Context, int64) (Account, error) {
	v.calls++
	if v.calls > v.okCalls {
		return Account{}, errors.New("account unlinked")
	}
	return v.acct, nil
}

// failingFileBot is a dispatchBot whose file download always fails.
type failingFileBot struct{ *dispatchBot }

func (b *failingFileBot) File(*tele.File) (io.ReadCloser, error) {
	return nil, errors.New("telegram file server unavailable")
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

// BuildTurnContext mirrors the real Service seam for the no-store unit harness: it
// composes this turn's attachment block (the catalog needs a store, absent here), so the
// attachment context still reaches the turn exactly as the production path renders it.
func (r *recordingAssetIngress) BuildTurnContext(_ context.Context, _, _ string, attachments []assetspkg.Asset, userText string) string {
	return assetspkg.WithContextBlocks(userText, assetspkg.BuildAttachmentBlock(attachments))
}

// OpenForIdentity mirrors the real Service seam: it re-checks ownership and streams
// back the last ingested payload, so projection tests read exactly what was stored.
func (r *recordingAssetIngress) OpenForIdentity(_ context.Context, assetID, identityID string) (io.ReadCloser, assetspkg.Asset, error) {
	if r.asset.ID != assetID || r.asset.IdentityID != identityID {
		return nil, assetspkg.Asset{}, errors.New("asset not found")
	}
	payload := ""
	if len(r.payloads) > 0 {
		payload = r.payloads[len(r.payloads)-1]
	}
	return io.NopCloser(strings.NewReader(payload)), r.asset, nil
}
