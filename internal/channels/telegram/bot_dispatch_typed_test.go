package telegram

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	tele "gopkg.in/telebot.v4"

	assetspkg "github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
)

// testCatalog is the knowledge catalog BuildTurnContext composes in front of every message
// once the identity has an indexed document. Measured 2026-10-07 with the local
// EmbeddingGemma on the document template: two unrelated messages 0.572 apart drop to
// 0.080 once both carry a two-document catalog, inside the 0.10 recall radius. So the
// runner must persist what was sent, and the catalog goes to the model for one round.
const testCatalog = "<knowledge_base trust=\"operator_pinned_context\">\n- [1] document_id=d1 filename=a.pdf\n</knowledge_base>\n\n"

func TestTextTurnWithACatalogPersistsWhatWasTyped(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Assets = &recordingAssetIngress{catalog: testCatalog}
	})
	msg := chatMsg(15)
	msg.Text = "che temperatura ci sarà sabato mattina a Bra?"
	if err := tg.onText(context.Background())(msgContext(&dispatchBot{}, msg)); err != nil {
		t.Fatalf("onText: %v", err)
	}
	tg.wg.Wait()

	want := TurnMessage{Visible: msg.Text, Model: testCatalog + "User message:\n" + msg.Text}
	if got := rt.turnsSnapshot(); len(got) != 1 || got[0] != want {
		t.Fatalf("turns = %+v, want %+v", got, want)
	}
}

func TestTextTurnWithNothingComposedIsAPlainTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Assets = &recordingAssetIngress{}
	})
	msg := chatMsg(16)
	msg.Text = "scrivimi una poesia di quattro righe sull'autunno"
	if err := tg.onText(context.Background())(msgContext(&dispatchBot{}, msg)); err != nil {
		t.Fatalf("onText: %v", err)
	}
	tg.wg.Wait()

	want := TurnMessage{Visible: msg.Text, Model: msg.Text}
	if got := rt.turnsSnapshot(); len(got) != 1 || got[0] != want {
		t.Fatalf("turns = %+v, want the plain %+v", got, want)
	}
}

// A voice note's request is its transcript, and an audio asset is not in the knowledge
// catalog: persisting the default attachment text would lose what was said from every
// later round.
func TestVoiceNotePersistsItsTranscript(t *testing.T) {
	t.Parallel()
	const transcript = "ricordami domani alle 9 di chiamare il dentista"
	rt := &recordingTurn{}
	assetIngress := &recordingAssetIngress{asset: assetspkg.Asset{
		ID: "asset-voice", IdentityID: profileAccount().IdentityID, SourceKind: assetspkg.SourceTelegram,
		Modality: assetspkg.ModalityAudio, Status: assetspkg.StatusComplete, FileName: "voice.ogg", Summary: transcript,
	}}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Assets = assetIngress })
	ogg := []byte("OggS\x00opus")
	bot := &dispatchBot{ogg: ogg}
	assetIngress.bot = bot
	msg := chatMsg(17)
	msg.Voice = &tele.Voice{FileID: "voice-file", FileSize: int64(len(ogg)), MIME: "audio/ogg"}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice: %v", err)
	}
	tg.wg.Wait()

	got := rt.turnsSnapshot()
	if len(got) != 1 || got[0].Visible != transcript {
		t.Fatalf("turns = %+v, want the transcript persisted", got)
	}
	if !strings.Contains(got[0].Model, `<attachments trust="untrusted_user_uploads">`) ||
		!strings.HasSuffix(got[0].Model, "User message:\n"+defaultAttachmentTurnText) {
		t.Fatalf("model message = %q, want the attachment block and the default request", got[0].Model)
	}
}

func TestQueuedAttachmentTurnPersistsWhatWasSent(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	rt := &recordingTurn{}
	driver, gates := gatedTurnDriver(&calls)
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Steer = steertest.New(steer.Config{})
		d.Turn = driver
		d.Assets = &recordingAssetIngress{}
	})
	bot := &dispatchBot{}
	first := chatMsg(26)
	first.Text = "richiesta lunga"
	if err := tg.onText(context.Background())(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	hop1 := <-gates

	const caption = "quanto ho speso?"
	attachment := assetspkg.Asset{ID: "a1", FileName: "scontrino.jpg", Modality: assetspkg.ModalityImage}
	tg.runTurnWithAssets(context.Background(), msgContext(bot, chatMsg(26)), 26, caption, []assetspkg.Asset{attachment}, false)
	close(hop1.gate)
	hop2 := <-gates
	close(hop2.gate)
	tg.wg.Wait()

	want := TurnMessage{Visible: caption, Model: assetspkg.WithAttachmentBlock(caption, []assetspkg.Asset{attachment})}
	if hop2.msg != want {
		t.Fatalf("queued turn = %+v, want %+v", hop2.msg, want)
	}
}
