package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"

	assetspkg "github.com/chetto1983/aura/internal/assets"
)

// This file covers the inbound-media dispatch AFTER the channel became a wrapper.
// Every handler has exactly one path: hand the Telegram stream to the shared asset
// pipeline and render the outcome. The tests that used to live here drove the
// channel's own markitdown/vision/STT clients; those clients are gone, the pipeline
// they duplicated is exercised in internal/assets, and asserting them here would be
// asserting a second Aura.

func TestOnPhotoRoutesThroughAssetIngress(t *testing.T) {
	t.Parallel()
	image := []byte("\xff\xd8fake-jpeg")
	rt := &recordingTurn{}
	assetIngress := &recordingAssetIngress{
		asset: assetspkg.Asset{
			ID:         "asset-photo",
			IdentityID: profileAccount().IdentityID,
			SourceKind: assetspkg.SourceTelegram,
			Modality:   assetspkg.ModalityImage,
			Status:     assetspkg.StatusComplete,
			FileName:   "photo.jpg",
			Summary:    "una foto di un gatto",
		},
	}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = assetIngress
	})

	bot := &dispatchBot{ogg: image}
	assetIngress.bot = bot
	msg := chatMsg(21)
	msg.Photo = &tele.Photo{File: tele.File{FileID: "photo-file", FileSize: int64(len(image))}}
	msg.Caption = "cosa c'e qui?"
	if err := tg.onPhoto(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onPhoto: %v", err)
	}
	tg.wg.Wait()

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("an asset photo must drive 1 turn with attachment context, got %d", calls)
	}
	if !strings.Contains(msgs[0], "asset-photo") || !strings.Contains(msgs[0], "una foto di un gatto") {
		t.Fatalf("turn userMsg missing photo asset context:\n%s", msgs[0])
	}
	if !strings.Contains(msgs[0], "cosa c'e qui?") {
		t.Fatalf("the photo caption is the operator's instruction and must drive the turn, got:\n%s", msgs[0])
	}
	req := assetIngress.reqs[0]
	if req.IdentityID != profileAccount().IdentityID || req.ChatID != 21 || req.FileID != "photo-file" ||
		req.FileName != "photo.jpg" || req.MIMEType != photoMIME || req.Modality != assetspkg.ModalityImage ||
		req.SizeBytes != int64(len(image)) {
		t.Fatalf("photo asset request = %+v, want linked identity and photo metadata", req)
	}
	if assetIngress.payloads[0] != string(image) || assetIngress.readsBeforeIngest[0] != 0 {
		t.Fatalf("photo stream was not handed directly to asset ingress, payload=%q readsBefore=%d", assetIngress.payloads[0], assetIngress.readsBeforeIngest[0])
	}
}

// TestOnDocumentRoutesThroughAssetIngress is the acceptance test for the whole
// change: a Telegram document produces a catalog-registered asset (document_search
// names it, document_open reads it) instead of channel-local converted markdown.
func TestOnDocumentRoutesThroughAssetIngress(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF small")
	rt := &recordingTurn{}
	assetIngress := &recordingAssetIngress{
		asset: assetspkg.Asset{
			ID:         "asset-doc",
			IdentityID: profileAccount().IdentityID,
			SourceKind: assetspkg.SourceTelegram,
			Modality:   assetspkg.ModalityDocument,
			Status:     assetspkg.StatusSearchable,
			FileName:   "manual.pdf",
			MIMEType:   "application/pdf",
			DocumentID: "doc-1",
			Summary:    "manual indexed",
		},
	}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = assetIngress
	})

	bot := &dispatchBot{ogg: pdf}
	assetIngress.bot = bot
	msg := chatMsg(31)
	msg.Document = &tele.Document{
		File:     tele.File{FileID: "doc-file", FileSize: int64(len(pdf))},
		FileName: "manual.pdf",
		MIME:     "application/pdf",
	}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}
	tg.wg.Wait()

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("an asset document must drive 1 turn with attachment context, got %d", calls)
	}
	if !strings.Contains(msgs[0], "asset-doc") ||
		!strings.Contains(msgs[0], `document_id="doc-1"`) ||
		!strings.Contains(msgs[0], "manual indexed") {
		t.Fatalf("turn userMsg missing document asset context:\n%s", msgs[0])
	}
	req := assetIngress.reqs[0]
	if req.IdentityID != profileAccount().IdentityID || req.ChatID != 31 || req.FileID != "doc-file" ||
		req.FileName != "manual.pdf" || req.MIMEType != "application/pdf" ||
		req.Modality != assetspkg.ModalityDocument || req.SizeBytes != int64(len(pdf)) {
		t.Fatalf("document asset request = %+v, want linked identity and document metadata", req)
	}
	if assetIngress.payloads[0] != string(pdf) || assetIngress.readsBeforeIngest[0] != 0 {
		t.Fatalf("document stream was not handed directly to asset ingress, payload=%q readsBefore=%d", assetIngress.payloads[0], assetIngress.readsBeforeIngest[0])
	}
}

func TestOnPhotoAssetIngressErrorSendsFailCopyAndDoesNotTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	assetIngress := &recordingAssetIngress{err: errors.New("object store down")}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = assetIngress
	})

	bot := &dispatchBot{ogg: []byte("jpeg")}
	assetIngress.bot = bot
	msg := chatMsg(22)
	msg.Photo = &tele.Photo{File: tele.File{FileID: "photo-file", FileSize: 4}}
	if err := tg.onPhoto(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onPhoto: %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("asset ingress error must not drive a turn, got %d", calls)
	}
	if texts := bot.sentTexts(); len(texts) == 0 || !strings.Contains(texts[0], describeFailMessage) {
		t.Fatalf("asset ingress error must send existing photo fail copy, sent=%v", texts)
	}
}

// TestOnVoiceIngressFailureReactsAndDoesNotTurn preserves the hard-fail UX the old
// STT client owned: a voice note that cannot be ingested gets the IT copy AND the 😵
// reaction, and drives no turn. Only the failing layer changed.
func TestOnVoiceIngressFailureReactsAndDoesNotTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	assetIngress := &recordingAssetIngress{err: errors.New("stt sidecar down")}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = assetIngress
	})

	bot := &dispatchBot{ogg: []byte("OggS")}
	assetIngress.bot = bot
	msg := chatMsg(13)
	msg.Voice = &tele.Voice{File: tele.File{FileID: "voice-file", FileSize: 4}}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice: %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a failed voice ingest must NOT drive a turn, got %d", calls)
	}
	if react := bot.recordedReactions(); len(react) != 1 || react[0] != hardFailReaction {
		t.Errorf("hard-fail reaction = %v, want [%q]", react, hardFailReaction)
	}
	if texts := bot.sentTexts(); len(texts) == 0 || !strings.Contains(texts[0], "Trascrizione non disponibile") {
		t.Errorf("hard-fail must send the IT copy, got %v", texts)
	}
}

// TestRefusalCopyNamesTheCause is the replacement for the old >50MB refuse tier. The
// ceiling itself now lives in assets.Limits (one place for every channel); what the
// channel still owns is telling the operator which refusal they hit, because "too
// big" is theirs to fix and "sidecar down" is ours.
func TestRefusalCopyNamesTheCause(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"too large", fmt.Errorf("wrapped: %w", assetspkg.ErrAssetTooLarge), assetTooLargeMessage},
		{"unsupported", fmt.Errorf("wrapped: %w", assetspkg.ErrAssetUnsupported), assetUnsupportedMessage},
		{"anything else keeps the modality copy", errors.New("object store down"), convertFailMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := refusalCopy(tc.err, convertFailMessage); got != tc.want {
				t.Errorf("refusalCopy = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOnDocumentTooLargeSendsTheSizeCopy(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	assetIngress := &recordingAssetIngress{
		err: fmt.Errorf("%w: document exceeds 104857600 bytes", assetspkg.ErrAssetTooLarge),
	}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = assetIngress
	})

	bot := &dispatchBot{ogg: []byte("%PDF")}
	assetIngress.bot = bot
	msg := chatMsg(33)
	msg.Document = &tele.Document{File: tele.File{FileID: "huge"}, FileName: "huge.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a refused document must NOT drive a turn, got %d", calls)
	}
	if texts := bot.sentTexts(); len(texts) != 1 || !strings.Contains(texts[0], "troppo grande") {
		t.Errorf("a refused document must send the size copy, got %v", texts)
	}
}

// TestAttachmentWithoutAssetServiceFailsLoudly pins the removal of the fallback: with
// no asset service there is no second ingestion path, so the file must be reported as
// unreadable rather than silently dropped.
func TestAttachmentWithoutAssetServiceFailsLoudly(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Assets = nil
	})

	bot := &dispatchBot{ogg: []byte("%PDF")}
	msg := chatMsg(34)
	msg.Document = &tele.Document{File: tele.File{FileID: "doc"}, FileName: "doc.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("no asset service must NOT drive a turn, got %d", calls)
	}
	if texts := bot.sentTexts(); len(texts) != 1 || !strings.Contains(texts[0], convertFailMessage) {
		t.Errorf("no asset service must still answer the operator, got %v", texts)
	}
}
