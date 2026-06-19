package telegram

import (
	"context"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	assetspkg "github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
)

// TestOnVoiceRoutesToTranscribe proves a voice update downloads the OGG bytes and
// drives a turn on the transcript (OnVoice → voiceClient.Transcribe → turn).
func TestOnVoiceRoutesToTranscribe(t *testing.T) {
	t.Parallel()
	ogg := []byte("OggS\x00opus")
	srv := httptest.NewServer(sttHandler(t, ogg, "ciao da nota vocale", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{STTBaseURL: srv.URL, STTModel: "large-v3-turbo", STTLanguage: "it"}
	})

	bot := &dispatchBot{ogg: ogg}
	msg := chatMsg(11)
	msg.Voice = &tele.Voice{}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice: %v", err)
	}
	tg.wg.Wait() // the turn now runs async off the poller — join it before asserting

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a voice note must drive 1 turn on the transcript, got %d", calls)
	}
	if msgs[0] != "ciao da nota vocale" {
		t.Errorf("turn userMsg = %q, want the transcript", msgs[0])
	}
}

// TestOnVoiceHardFailReactsAndDoesNotTurn proves a persistent STT 5xx sends the IT
// copy + the 😵 reaction and drives NO turn.
func TestOnVoiceHardFailReactsAndDoesNotTurn(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		mm := MultimodalConfig{STTBaseURL: srv.URL, RetryBackoff: []int{1, 1}}
		d.Multimodal = mm
	})

	bot := &dispatchBot{ogg: []byte("OggS")}
	msg := chatMsg(13)
	msg.Voice = &tele.Voice{}
	if err := tg.onVoice(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onVoice(hardfail): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a hard STT failure must NOT drive a turn, got %d", calls)
	}
	if react := bot.recordedReactions(); len(react) != 1 || react[0] != hardFailReaction {
		t.Errorf("hard-fail reaction = %v, want [%q]", react, hardFailReaction)
	}
	if texts := bot.sentTexts(); len(texts) == 0 || !strings.Contains(texts[0], "Trascrizione non disponibile") {
		t.Errorf("hard-fail must send the IT copy, got %v", texts)
	}
}

// TestOnPhotoRoutesToDescribe proves a photo update downloads the bytes and drives
// a turn on the vision description (OnPhoto → photoClient.Describe → turn).
func TestOnPhotoRoutesToDescribe(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(visionHandler(t, &hits, "glm-ocr", "una foto di un gatto"))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{VisionCloud: false, MultimodalBaseURL: srv.URL, MultimodalModel: "glm-ocr"}
	})

	bot := &dispatchBot{ogg: []byte("\x89PNGfake")}
	msg := chatMsg(21)
	msg.Photo = &tele.Photo{}
	msg.Caption = "cosa c'è qui?"
	if err := tg.onPhoto(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onPhoto: %v", err)
	}
	tg.wg.Wait() // the turn now runs async off the poller — join it before asserting

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a photo must drive 1 turn on the description, got %d", calls)
	}
	if msgs[0] != "una foto di un gatto" {
		t.Errorf("turn userMsg = %q, want the description", msgs[0])
	}
	if hits.Load() != 1 {
		t.Errorf("vision sidecar hits = %d, want 1", hits.Load())
	}
}

func TestOnPhotoRoutesThroughAssetIngressWhenConfigured(t *testing.T) {
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

// TestOnDocumentSyncRoutesToConvert proves a ≤5MB document downloads + converts
// inline and drives a turn on the markdown (OnDocument → documentsClient.Convert →
// turn). Stop drains any async work goleak-clean.
func TestOnDocumentSyncRoutesToConvert(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# Relazione", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	bot := &dispatchBot{ogg: []byte("small pdf")}
	msg := chatMsg(31)
	msg.Document = &tele.Document{FileName: "doc.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}
	tg.wg.Wait() // the turn now runs async off the poller — join it before asserting

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("a ≤5MB document must drive 1 turn on the markdown, got %d", calls)
	}
	if !strings.Contains(msgs[0], "Relazione") {
		t.Errorf("turn userMsg = %q, want the converted markdown", msgs[0])
	}
	if hits.Load() != 1 {
		t.Errorf("convert hits = %d, want 1", hits.Load())
	}
}

func TestOnDocumentRoutesThroughAssetIngressWhenConfigured(t *testing.T) {
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

func TestOnDocumentPipelineRoutesToIngestWhenConfigured(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	ingest := &recordingDocumentIngest{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.DocumentIngest = ingest
	})

	bot := &dispatchBot{ogg: []byte("small pdf")}
	msg := chatMsg(36)
	msg.Document = &tele.Document{FileName: "doc.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument: %v", err)
	}
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("pipeline document ingest must not drive a markdown turn, got %d", calls)
	}
	if ingest.calls != 1 || ingest.req.SourceKind != "telegram" {
		t.Fatalf("ingest calls=%d req=%#v", ingest.calls, ingest.req)
	}
	if texts := bot.sentTexts(); len(texts) == 0 || !strings.Contains(texts[len(texts)-1], "Ho indicizzato") {
		t.Fatalf("missing indexed reply, sent=%v", texts)
	}
}

// TestOnDocumentAsyncResultHonorsBusyGate proves the async document completion
// path uses the same per-chat busy gate as ordinary inbound turns.
func TestOnDocumentAsyncResultHonorsBusyGate(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# Relazione async", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	const chatID int64 = 32
	if !tg.cmds.registerTurn(chatID, func() {}) {
		t.Fatal("failed to pre-register busy turn")
	}
	defer tg.cmds.unregisterTurn(chatID)

	bot := &dispatchBot{ogg: make([]byte, asyncTierMinBytes+1)}
	msg := chatMsg(chatID)
	msg.Document = &tele.Document{FileName: "async.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(async busy): %v", err)
	}
	tg.docs.Stop(context.Background())
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("async document result must not bypass busy gate, got %d turn calls", calls)
	}
	var sawBusy bool
	for _, text := range bot.sentTexts() {
		if strings.Contains(text, turnBusyMessage) {
			sawBusy = true
		}
	}
	if !sawBusy {
		t.Fatalf("async document busy result must send busy copy, sent=%v", bot.sentTexts())
	}
	if hits.Load() != 1 {
		t.Errorf("async document should still convert once, got %d hits", hits.Load())
	}
}

// TestOnDocumentAsyncConvertFailureNotifiesUser proves an async (5-50MB)
// conversion that fails notifies the user with convertFailMessage instead of
// leaving them on "📄 …elaborando…" forever (H3). BEFORE the fix the async callback
// logged convErr and returned silently; AFTER, the captured sender delivers the
// fail copy. The sync ≤5MB sibling already did this — the async tier now matches.
func TestOnDocumentAsyncConvertFailureNotifiesUser(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // drain the >5MB upload before responding
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	bot := &dispatchBot{ogg: make([]byte, asyncTierMinBytes+1)} // > 5MB → async tier
	msg := chatMsg(35)
	msg.Document = &tele.Document{FileName: "async-fail.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(async fail): %v", err)
	}
	tg.docs.Stop(context.Background()) // drain the async convert goroutine
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a failed async conversion must NOT drive a turn, got %d", calls)
	}
	var sawFail, sawAccepted bool
	for _, text := range bot.sentTexts() {
		if strings.Contains(text, convertFailMessage) {
			sawFail = true
		}
		if strings.Contains(text, documentAcceptedMessage) {
			sawAccepted = true
		}
	}
	if !sawAccepted {
		t.Errorf("the async tier must first acknowledge the document, sent=%v", bot.sentTexts())
	}
	if !sawFail {
		t.Fatalf("a failed async conversion must notify the user with the fail copy, sent=%v", bot.sentTexts())
	}
}

func TestStopDrainsAsyncDocumentTurn(t *testing.T) {
	convertEntered := make(chan struct{})
	allowConvert := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		select {
		case <-convertEntered:
		default:
			close(convertEntered)
		}
		<-allowConvert
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markdown":"# Relazione async"}`))
	}))
	defer srv.Close()

	turnStarted := make(chan struct{})
	releaseTurn := make(chan struct{})
	rt := &recordingTurn{}
	tg := NewChannel(Deps{
		Offline:         true,
		consumerFactory: recordingFactory(),
		Multimodal:      MultimodalConfig{DocumentsBaseURL: srv.URL},
		profileAccounts: profileAccountFake{acct: profileAccount()},
		Turn: func(ctx context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
			return func(yield func(*agent.Event, error) bool) {
				rt.mu.Lock()
				rt.calls++
				rt.mu.Unlock()
				select {
				case <-turnStarted:
				default:
					close(turnStarted)
				}
				select {
				case <-releaseTurn:
				case <-ctx.Done():
				}
				yield(textEvent("ok"), nil)
			}
		},
	})
	if err := tg.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	bot := &dispatchBot{ogg: make([]byte, asyncTierMinBytes+1)}
	msg := chatMsg(34)
	msg.Document = &tele.Document{FileName: "async.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(async): %v", err)
	}
	<-convertEntered

	stopDone := make(chan error, 1)
	go func() { stopDone <- tg.Stop(context.Background()) }()
	close(allowConvert)
	<-turnStarted

	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before draining async document turn: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseTurn)
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if calls, _ := rt.snapshot(); calls != 1 {
		t.Fatalf("async document should drive exactly one drained turn, got %d", calls)
	}
}

// TestOnDocumentRefuseTier proves a >50MB document is refused with the user-facing
// message and drives NO turn + NO sidecar call.
func TestOnDocumentRefuseTier(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "nope", http.StatusOK))
	defer srv.Close()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Multimodal = MultimodalConfig{DocumentsBaseURL: srv.URL}
	})
	defer tg.docs.Stop(context.Background())

	bot := &dispatchBot{ogg: make([]byte, refuseTierMinBytes+1)}
	msg := chatMsg(33)
	msg.Document = &tele.Document{FileName: "huge.pdf"}
	if err := tg.onDocument(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onDocument(refuse): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a refused document must NOT drive a turn, got %d", calls)
	}
	if hits.Load() != 0 {
		t.Errorf("a refused document must NOT hit the sidecar, got %d", hits.Load())
	}
	if texts := bot.sentTexts(); len(texts) != 1 || !strings.Contains(texts[0], "troppo grande") {
		t.Errorf("a refused document must send the refuse copy, got %v", texts)
	}
}

type recordingDocumentIngest struct {
	calls int
	req   documents.IngestRequest
	path  string
}

func (r *recordingDocumentIngest) IngestPath(_ context.Context, req documents.IngestRequest, path string) (*documents.Job, error) {
	r.calls++
	r.req = req
	r.path = path
	return &documents.Job{
		ID:           "job-1",
		DocumentID:   "doc-1",
		FileName:     req.FileName,
		Status:       documents.JobSearchable,
		SparseChunks: 1,
	}, nil
}
