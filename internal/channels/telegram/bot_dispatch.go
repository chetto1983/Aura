// Package telegram — this file is the inbound DISPATCH: the OnText command/HITL
// intercept wrapper + the OnVoice/OnPhoto/OnDocument media handlers + the
// OnCallback/OnReply HITL resume, plus the per-channel dispatch instances they
// reuse. It is split out of bot.go (refactor-on-touch, CLAUDE.md ≤600 LOC) so
// bot.go keeps only the telebot lifecycle.
//
// This is the integration the prior Gate-3 (13-09) assumed but did not have: the
// channel registered ONLY tele.OnText, so /command, voice, photo, document, an
// ask_user button-tap, and a send_file artifact NEVER reached their handler
// (UX-02/03/04 unreachable). registerHandlers wires them all.
//
// Routing rules (plan 13-10):
//   - OnText  : /start <token> consumes onboarding first; then linked-account auth;
//     then commands.dispatch
//     intercepts (a /command never drives the LLM — T-13-10-CmdToLLM); else a
//     pending pause → hitl.handleTextReply; else a turn.
//   - OnVoice : getFile → voiceClient.Transcribe → turn driven by the transcript;
//     on a hard STT failure send the IT copy + the 😵 reaction.
//   - OnPhoto : getFile → photoClient.Describe (the single AURA_VISION_CLOUD
//     branch) → turn driven by the description.
//   - OnDocument: getFile → documentsClient.Convert; ≤5MB sync → turn on the
//     markdown; 5-50MB async → turn when the per-request callback fires; >50MB → the refuse
//     copy.
//   - OnCallback (callbackUnique) / OnReply: hitl resolves the pause through the
//     Runner and resumes the SAME loop, rendering the continuation through the
//     per-turn fanout.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	tele "gopkg.in/telebot.v4"
)

// photoMIME is the MIME the OnPhoto handler attaches to a downloaded photo.
// Telegram delivers photos as JPEG and the Photo type carries no mime_type, so the
// vision data: URL is built as image/jpeg.
const photoMIME = "image/jpeg"

// User-facing dispatch copy (always Italian — the output-language directive). The
// hard-fail copies mirror voice.go/documents.go: a sidecar failure degrades to a
// short message, never a stack trace or a silent drop.
const (
	describeFailMessage     = "❌ Analisi dell'immagine non disponibile."
	convertFailMessage      = "❌ Conversione del documento non disponibile."
	documentAcceptedMessage = "📄 Documento ricevuto, lo sto elaborando…"
	turnBusyMessage         = "⏳ Sto ancora elaborando la richiesta precedente. Usa /cancel per annullarla."
)

// buildDispatch constructs the per-channel dispatch instances ONCE from Deps. The
// document conversion callbacks are passed per request where the bot + chat
// context is available (the >5MB tier drives a turn on the inbound chat). Called
// under t.mu from Start.
func (t *Telegram) buildDispatch() {
	t.cmds = newCommands(commandDeps{
		Search: t.deps.Search,
		Cost:   t.deps.Cost,
		Clear:  t.deps.Clear,
		Prices: t.deps.Prices,
		Model:  t.deps.Model,
	})
	var onboardStore onboardingStore
	if t.deps.Store != nil {
		onboardStore = t.deps.Store
	}
	t.onboard = newOnboarding(onboardStore)
	t.profile = newProfileOnboarding(t.deps.Profile, t.accountsForDispatch())
	t.profile.extractor = t.deps.AnswerExtractor
	t.voice = newVoiceClient(t.deps.Multimodal)
	t.photo = newPhotoClient(t.deps.Multimodal)
	t.docs = newDocumentsClient(t.deps.Multimodal)
	t.tts = newTTSClient(t.deps.Multimodal)
}

// registerHandlers wires every inbound endpoint on the bot. It binds the document
// async-result callback to drive a turn on the originating chat, then registers
// OnText (intercept), the three media handlers, the HITL callback (under the
// callbackUnique inline-button endpoint), and OnReply (the ForceReply answer leg).
func (t *Telegram) registerHandlers(daemonCtx context.Context, bot *tele.Bot) {
	bot.Handle(tele.OnText, t.onText(daemonCtx))
	bot.Handle(tele.OnVoice, t.onVoice(daemonCtx))
	bot.Handle(tele.OnPhoto, t.onPhoto(daemonCtx))
	bot.Handle(tele.OnDocument, t.onDocument(daemonCtx))
	// The HITL inline buttons all carry Unique == callbackUnique; telebot routes
	// such callbacks to the handler registered under that button and strips the
	// \f<unique> prefix, leaving the raw token|action|value payload parseCallback
	// expects. This is the SOLE approval endpoint: the scheduler approval-reminder
	// sweep push (Amendment #92 revised) renders through the same builder, so both
	// legs resolve their pause through one handler (Phase A consolidation).
	bot.Handle(&tele.InlineButton{Unique: callbackUnique}, t.onCallback(daemonCtx))
	bot.Handle(&tele.InlineButton{Unique: searchCallbackUnique}, t.onSearchCallback())
	bot.Handle(&tele.InlineButton{Unique: statusCancelUnique}, t.onStatusCancelCallback())
	bot.Handle(&tele.InlineButton{Unique: profileCallbackUnique}, t.onProfileCallback(daemonCtx))
	// A callback NOT matching the HITL button falls through to OnCallback: ack it so
	// the client clears the spinner; it carries no live pause to resolve (a forged
	// or stale callback is a no-op — T-13-10-PauseHijack).
	bot.Handle(tele.OnCallback, t.onCallbackFallback())
	bot.Handle(tele.OnReply, t.onReply(daemonCtx))
}

// onText is the command/HITL-intercept text handler. A /command is intercepted
// BEFORE any LLM dispatch (T-13-10-CmdToLLM); a free-text answer to a pending
// pause is routed to HITL; everything else drives a normal turn.
func (t *Telegram) onText(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil {
			return nil
		}
		chatID := msg.Chat.ID
		text := c.Text()
		if msg.ReplyTo != nil && t.takeHitlReplyHandled(chatID, msg.ID) {
			return nil
		}
		if reply, ok := t.handleStartPayload(daemonCtx, msg, text); ok {
			t.reply(c, reply)
			return nil
		}
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			return nil
		}

		// 0) /cancel during a pending ask_user pause cancels the PAUSE, not a turn
		// (M-e). This must run BEFORE the command intercept: /cancel is a command, so
		// dispatchRich would otherwise consume it and only no-op the (absent) in-flight
		// turn ctx, orphaning the paused_states row + leaving the keyboard live. With no
		// pending pause cancelPendingPause is inert and the ordinary command path (the
		// turn-cancel) still handles /cancel below.
		if name, _ := splitCommand(text); name == "/cancel" && t.cancelPendingPause(daemonCtx, c, chatID) {
			return nil
		}
		if name, _ := splitCommand(text); name == "/onboard" {
			t.replyProfile(c, t.profileForDispatch().restart(daemonCtx, chatID, telegramUserIDFromMessage(msg)))
			return nil
		}
		// 1) Command intercept — a handled /command never reaches the LLM.
		if handled, reply := t.cmds.dispatchRich(daemonCtx, chatID, text); handled {
			t.replyCommand(c, reply)
			return nil
		}
		// 2) A pending pause + a non-command message → a free-text HITL answer.
		if t.hitlHandlesText(daemonCtx, c, chatID, text) {
			return nil
		}
		if out, handled := t.profileForDispatch().handleText(daemonCtx, chatID, text); handled {
			t.replyProfile(c, out)
			return nil
		}
		if out, handled := t.profileForDispatch().maybeStart(daemonCtx, chatID, telegramUserIDFromMessage(msg)); handled {
			t.replyProfile(c, out)
			return nil
		}
		// 3) Ordinary message → a normal turn (runTurn runs it async + shows the
		// "Aura is working" indicator, so the poller stays free for /cancel).
		t.runTurn(daemonCtx, c, chatID, text, false)
		return nil
	}
}

// handleStartPayload consumes a Telegram deep-link "/start <token>" before the
// generic command dispatcher sees it. A bare /start is left to commands.dispatch
// so the ordinary greeting remains unchanged.
func (t *Telegram) handleStartPayload(ctx context.Context, msg *tele.Message, text string) (reply string, handled bool) {
	name, _ := splitCommand(text)
	if name != "/start" {
		return "", false
	}
	token := parseStartPayload(text)
	if token == "" {
		return "", false
	}

	onboard := t.onboard
	if onboard == nil {
		var onboardStore onboardingStore
		if t.deps.Store != nil {
			onboardStore = t.deps.Store
		}
		onboard = newOnboarding(onboardStore)
	}
	reply, _ = onboard.handleStart(ctx, startMsgFromMessage(msg, token))
	return reply, true
}

func startMsgFromMessage(msg *tele.Message, token string) startMsg {
	out := startMsg{Token: token}
	if msg == nil {
		return out
	}
	if msg.Chat != nil {
		out.TelegramUserID = msg.Chat.ID
		out.Username = msg.Chat.Username
		out.FirstName = msg.Chat.FirstName
	}
	if msg.Sender != nil {
		out.TelegramUserID = msg.Sender.ID
		out.Username = msg.Sender.Username
		out.FirstName = msg.Sender.FirstName
	}
	return out
}

// onReply handles a ForceReply answer to an ask_user clarification. telebot fires
// OnReply (then OnText) for a reply; routing the answer here keeps the ForceReply
// leg explicit. With no pending pause it is a no-op so OnText drives the message as
// an ordinary turn (handleTextReply returns false → no double-handling).
func (t *Telegram) onReply(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil {
			return nil
		}
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			t.markHitlReplyHandled(msg.Chat.ID, msg.ID)
			return nil
		}
		t.hitlHandlesReply(daemonCtx, c, msg.Chat.ID, msg.ID, c.Text())
		return nil
	}
}

// onCallback resolves an inline-button press (approval/choice) through the Runner
// and resumes the loop when nothing is left unresolved.
func (t *Telegram) onCallback(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		cb := c.Callback()
		if cb == nil || cb.Message == nil || cb.Message.Chat == nil {
			return nil
		}
		if !t.requireLinkedCallback(daemonCtx, c, cb) {
			return nil
		}
		chatID := cb.Message.Chat.ID
		_, action, _, valid := parseCallback(cb.Data)
		// Acknowledge immediately so Telegram clears the button spinner and shows a
		// small toast before any continuation turn starts rendering.
		_ = c.Respond(callbackToast(action, valid))
		out := t.hitlFor(c, chatID).handleCallbackResult(daemonCtx, cb.Data, convID(chatID), func(callbackOutcome) {
			t.disarmCallbackKeyboard(c.Bot(), cb.Message)
			t.trackPausePrompt(chatID, nil) // this prompt is now disarmed; drop the tracked handle
		})
		if out.submitted && !out.resumed {
			// Not resumed → either more FIFO pauses remain (render the next one) or it
			// was a cancel/no-op (PendingFor is empty after the cancel auto-resolve, so
			// the render no-ops). A resume (resumed==true) drove a continuation turn
			// whose handleTurn already rendered any further pause — don't double-render.
			t.promptPendingPause(daemonCtx, t.sender(c), chatID)
		}
		return nil
	}
}

func callbackToast(action string, valid bool) *tele.CallbackResponse {
	return &tele.CallbackResponse{Text: callbackToastText(action, valid)}
}

func callbackToastText(action string, valid bool) string {
	if !valid {
		return "Non disponibile"
	}
	switch action {
	case "accept":
		return "Confermato ✓"
	case "decline":
		return "Rifiutato"
	case "cancel":
		return "Annullato"
	default:
		return "Ricevuto"
	}
}

func (t *Telegram) disarmCallbackKeyboard(bot tele.API, msg *tele.Message) {
	if bot == nil || msg == nil {
		return
	}
	if _, err := bot.EditReplyMarkup(msg, nil); err != nil {
		slog.Warn("telegram hitl: prompt markup clear failed", "err", err)
	}
}

// onCallbackFallback acks a callback that did not match the HITL inline-button
// endpoint (no live pause to resolve) so the client's button spinner clears. A
// forged/stale callback reaching here is a deliberate no-op (the Runner stays the
// sole writer of paused_states — T-13-10-PauseHijack).
func (t *Telegram) onCallbackFallback() tele.HandlerFunc {
	return func(c tele.Context) error {
		if cb := c.Callback(); cb != nil && !t.requireLinkedCallback(context.Background(), c, cb) {
			return nil
		}
		_ = c.Respond()
		return nil
	}
}

// onVoice transcribes a voice note via the STT sidecar and drives a turn on the
// transcript. A hard STT failure (after retries) sends the IT copy + the 😵
// reaction and drives no turn.
func (t *Telegram) onVoice(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil || msg.Voice == nil {
			return nil
		}
		chatID := msg.Chat.ID
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			return nil
		}
		filer, ok := c.Bot().(botFiler)
		if !ok {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // STT + turn can take seconds
		defer stop()
		if t.deps.Assets != nil {
			return t.ingestTelegramAsset(daemonCtx, c, msg, &msg.Voice.File, telegramVoiceFileName(msg.Voice), telegramVoiceMIME(msg.Voice), assets.ModalityAudio, msg.Voice.FileSize, transcribeFailMessage, true)
		}
		transcript, err := t.voice.Transcribe(daemonCtx, filer, tele.ChatID(chatID), msg.Voice)
		if err != nil {
			slog.Warn("telegram: voice transcription failed", "chat", chatID, "err", err)
			t.reply(c, transcribeFailMessage)
			if reactor, rok := c.Bot().(botReactor); rok {
				_ = t.voice.HardFail(reactor, tele.ChatID(chatID), msg)
			}
			return nil
		}
		t.runTurn(daemonCtx, c, chatID, transcript, true)
		return nil
	}
}

// onPhoto describes a photo via the vision route (the single AURA_VISION_CLOUD
// branch) and drives a turn on the description. The caption (if any) is the prompt.
func (t *Telegram) onPhoto(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil || msg.Photo == nil {
			return nil
		}
		chatID := msg.Chat.ID
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			return nil
		}
		filer, ok := c.Bot().(botFiler)
		if !ok {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // OCR + turn can take seconds
		defer stop()
		if t.deps.Assets != nil {
			return t.ingestTelegramAsset(daemonCtx, c, msg, &msg.Photo.File, "photo.jpg", photoMIME, assets.ModalityImage, msg.Photo.FileSize, describeFailMessage, false)
		}
		image, err := downloadFile(filer, &msg.Photo.File)
		if err != nil {
			slog.Warn("telegram: photo download failed", "chat", chatID, "err", err)
			return nil
		}
		description, err := t.photo.Describe(daemonCtx, image, photoMIME, msg.Caption)
		if err != nil {
			slog.Warn("telegram: photo describe failed", "chat", chatID, "err", err)
			t.reply(c, describeFailMessage)
			return nil
		}
		t.runTurn(daemonCtx, c, chatID, description, false)
		return nil
	}
}

// onDocument converts a document via the markitdown sidecar (size-tiered) and
// drives a turn on the markdown. A ≤5MB document converts inline; a 5-50MB one is
// accepted async and drives a turn when its per-request callback fires; a >50MB one is
// refused with a user-facing message.
func (t *Telegram) onDocument(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil || msg.Document == nil {
			return nil
		}
		chatID := msg.Chat.ID
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			return nil
		}
		filer, ok := c.Bot().(botFiler)
		if !ok {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // download + convert + turn
		defer stop()
		if t.deps.Assets != nil {
			return t.ingestTelegramAsset(daemonCtx, c, msg, &msg.Document.File, msg.Document.FileName, msg.Document.MIME, assets.ModalityDocument, msg.Document.FileSize, convertFailMessage, false)
		}
		payload, err := downloadFile(filer, &msg.Document.File)
		if err != nil {
			slog.Warn("telegram: document download failed", "chat", chatID, "err", err)
			return nil
		}
		if t.deps.DocumentIngest != nil {
			return t.ingestTelegramDocument(daemonCtx, c, chatID, payload, msg.Document.FileName)
		}
		sender := t.sender(c)
		notifier, _ := sender.(botNotifier)
		asyncResult := func(_ string, markdown string, convErr error) {
			if convErr != nil {
				slog.Warn("telegram: async document convert failed", "chat", chatID, "err", convErr)
				// Notify the user the async conversion failed instead of leaving them on
				// "📄 …elaborando…" forever (H3). Mirrors the sync ≤5MB sibling
				// (t.reply(c, convertFailMessage)) via the captured sender — the
				// tele.Context is gone by the time this per-request callback fires.
				if sender != nil {
					if _, err := sender.Send(tele.ChatID(chatID), convertFailMessage); err != nil {
						slog.Warn("telegram: async convert-fail notice send failed", "chat", chatID, "err", err)
					}
				}
				return
			}
			t.startTurn(daemonCtx, sender, notifier, tele.ChatID(chatID), chatID, &markdown, false,
				func() { t.sendBusy(sender, chatID) })
		}

		res, err := t.docs.Convert(daemonCtx, payload, msg.Document.FileName, asyncResult)
		if err != nil {
			slog.Warn("telegram: document convert failed", "chat", chatID, "err", err)
			t.reply(c, convertFailMessage)
			return nil
		}
		switch res.Status {
		case ConvertSync:
			t.runTurn(daemonCtx, c, chatID, res.Markdown, false)
		case ConvertRefused:
			t.reply(c, res.Message)
		case ConvertAsync:
			t.reply(c, documentAcceptedMessage)
		}
		return nil
	}
}

func (t *Telegram) ingestTelegramDocument(ctx context.Context, c tele.Context, chatID int64, payload []byte, fileName string) error {
	switch {
	case len(payload) > refuseTierMinBytes:
		t.reply(c, documentRefuseMessage)
		return nil
	case len(payload) > asyncTierMinBytes:
		t.reply(c, documentAcceptedMessage)
	}

	path, cleanup, err := writeTelegramDocumentTemp(payload, fileName)
	if err != nil {
		slog.Warn("telegram: document temp write failed", "chat", chatID, "err", err)
		t.reply(c, convertFailMessage)
		return nil
	}
	defer cleanup()

	start := time.Now()
	job, err := t.deps.DocumentIngest.IngestPath(ctx, documents.IngestRequest{
		SourceID:   fmt.Sprintf("telegram:%d", chatID),
		SourceKind: "telegram",
		FileName:   fileName,
	}, path)
	if err != nil {
		slog.Warn("telegram: document ingest failed", "chat", chatID, "err", err)
		t.reply(c, convertFailMessage)
		return nil
	}
	t.reply(c, fmt.Sprintf("Ho indicizzato %q in %.1fs. Puoi farmi domande sul documento.", job.FileName, time.Since(start).Seconds()))
	return nil
}

func writeTelegramDocumentTemp(payload []byte, fileName string) (string, func(), error) {
	suffix := filepath.Ext(fileName)
	if suffix == "" {
		suffix = ".bin"
	}
	f, err := os.CreateTemp("", "aura-telegram-doc-*"+suffix)
	if err != nil {
		return "", func() {}, err
	}
	path := f.Name()
	if _, err = f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", func() {}, err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return "", func() {}, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// reply sends a plain user-facing message (a command reply / a fail copy). It is
// best-effort: a failed send is logged, never surfaced to the poller.
func (t *Telegram) reply(c tele.Context, text string) {
	if text == "" {
		return
	}
	if err := c.Send(text); err != nil {
		slog.Warn("telegram: reply send failed", "err", err)
	}
}

func (t *Telegram) replyProfile(c tele.Context, out profileReply) {
	if out.text == "" {
		return
	}
	if out.markup == nil {
		t.reply(c, out.text)
		return
	}
	if err := c.Send(out.text, &tele.SendOptions{ReplyMarkup: out.markup}); err != nil {
		slog.Warn("telegram: profile onboarding reply send failed", "err", err)
	}
}

func (t *Telegram) replyCommand(c tele.Context, out commandReply) {
	if out.text == "" {
		return
	}
	if out.markup == nil {
		t.reply(c, out.text)
		return
	}
	if err := c.Send(out.text, &tele.SendOptions{ReplyMarkup: out.markup}); err != nil {
		slog.Warn("telegram: command reply send failed", "err", err)
	}
}
