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
//   - OnText  : /start <token> consumes onboarding first; then commands.dispatch
//     intercepts (a /command never drives the LLM — T-13-10-CmdToLLM); else a
//     pending pause → hitl.handleTextReply; else a turn.
//   - OnVoice : getFile → voiceClient.Transcribe → turn driven by the transcript;
//     on a hard STT failure send the IT copy + the 😵 reaction.
//   - OnPhoto : getFile → photoClient.Describe (the single AURA_VISION_CLOUD
//     branch) → turn driven by the description.
//   - OnDocument: getFile → documentsClient.Convert; ≤5MB sync → turn on the
//     markdown; 5-50MB async → turn when OnAsyncResult fires; >50MB → the refuse
//     copy.
//   - OnCallback (callbackUnique) / OnReply: hitl resolves the pause through the
//     Runner and resumes the SAME loop, rendering the continuation through the
//     per-turn fanout.
package telegram

import (
	"context"
	"io"
	"log/slog"

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
// documents client's OnAsyncResult is left nil here and bound in registerHandlers
// where the bot + chat context is available (the >5MB tier drives a turn on the
// inbound chat). Called under t.mu from Start.
func (t *Telegram) buildDispatch() {
	t.cmds = newCommands(commandDeps{
		Search: t.deps.Search,
		Cost:   t.deps.Cost,
		Prices: t.deps.Prices,
		Model:  t.deps.Model,
	})
	var onboardStore onboardingStore
	if t.deps.Store != nil {
		onboardStore = t.deps.Store
	}
	t.onboard = newOnboarding(onboardStore)
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
	// expects.
	bot.Handle(&tele.InlineButton{Unique: callbackUnique}, t.onCallback(daemonCtx))
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
		if reply, ok := t.handleStartPayload(daemonCtx, msg, text); ok {
			t.reply(c, reply)
			return nil
		}

		// 1) Command intercept — a handled /command never reaches the LLM.
		if handled, reply := t.cmds.dispatch(daemonCtx, chatID, text); handled {
			t.reply(c, reply)
			return nil
		}
		// 2) A pending pause + a non-command message → a free-text HITL answer.
		if t.hitlHandlesText(daemonCtx, c, chatID, text) {
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
		t.hitlHandlesText(daemonCtx, c, msg.Chat.ID, c.Text())
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
		chatID := cb.Message.Chat.ID
		t.hitlFor(c, chatID).handleCallback(daemonCtx, cb.Data, convID(chatID))
		// Acknowledge the callback so the client clears the button's spinner.
		_ = c.Respond()
		return nil
	}
}

// onCallbackFallback acks a callback that did not match the HITL inline-button
// endpoint (no live pause to resolve) so the client's button spinner clears. A
// forged/stale callback reaching here is a deliberate no-op (the Runner stays the
// sole writer of paused_states — T-13-10-PauseHijack).
func (t *Telegram) onCallbackFallback() tele.HandlerFunc {
	return func(c tele.Context) error {
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
		filer, ok := c.Bot().(botFiler)
		if !ok {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // STT + turn can take seconds
		defer stop()
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
		filer, ok := c.Bot().(botFiler)
		if !ok {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // OCR + turn can take seconds
		defer stop()
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
// accepted async and drives a turn when OnAsyncResult fires; a >50MB one is
// refused with a user-facing message.
func (t *Telegram) onDocument(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil || msg.Document == nil {
			return nil
		}
		chatID := msg.Chat.ID
		filer, ok := c.Bot().(botFiler)
		if !ok {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // download + convert + turn
		defer stop()
		payload, err := downloadFile(filer, &msg.Document.File)
		if err != nil {
			slog.Warn("telegram: document download failed", "chat", chatID, "err", err)
			return nil
		}
		// Bind the async-result callback to THIS chat before the convert call so the
		// >5MB tier drives a turn on the originating chat when it completes.
		sender := t.sender(c)
		t.docs.OnAsyncResult = func(_ string, markdown string, convErr error) {
			if convErr != nil {
				slog.Warn("telegram: async document convert failed", "chat", chatID, "err", convErr)
				return
			}
			t.handleTurn(daemonCtx, sender, chatID, &markdown, false)
		}

		res, err := t.docs.Convert(daemonCtx, payload, msg.Document.FileName)
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

// hitlHandlesText routes a free-text message to HITL when a pause is pending for
// the chat. It returns true when HITL consumed the message (a pending answer), so
// the caller does NOT also drive an ordinary turn. With no pending pause it returns
// false (handleTextReply is a no-op), so an ordinary message is never stolen.
func (t *Telegram) hitlHandlesText(daemonCtx context.Context, c tele.Context, chatID int64, text string) bool {
	if t.deps.Resume == nil {
		return false
	}
	pending, err := t.deps.Resume.PendingFor(daemonCtx, convID(chatID))
	if err != nil || len(pending) == 0 {
		return false
	}
	t.hitlFor(c, chatID).handleTextReply(daemonCtx, convID(chatID), text)
	return true
}

// hitlFor builds a HITL surface whose resume renders the continuation turn through
// the channel's per-turn fanout on THIS chat (so a resumed answer reaches the
// user, not just the Runner). The runner seam is Deps.Resume; the resume closure
// captures the bot + chat id available only at dispatch time.
func (t *Telegram) hitlFor(c tele.Context, chatID int64) *hitl {
	sender := t.sender(c)
	resume := func(ctx context.Context, _ string) {
		// nil userMsg → a continuation turn; inboundWasVoice=false (a resume is a
		// button/text answer, never itself a voice note).
		t.handleTurn(ctx, sender, chatID, nil, false)
	}
	return newHitl(t.deps.Resume, resume)
}

// runTurn drives ONE inbound turn through the per-turn fanout under a cancellable
// ctx registered with the command dispatcher so a concurrent /cancel aborts it
// (SC#3). The cancel func is deregistered when the turn returns so a later /cancel
// never fires a stale one. inboundWasVoice flags the echo-modality TTS-out path
// (plan 13-10 Task 3 reads it).
func (t *Telegram) runTurn(daemonCtx context.Context, c tele.Context, chatID int64, text string, inboundWasVoice bool) {
	if t.deps.Turn == nil {
		slog.Warn("telegram: inbound message but no turn driver wired", "chat", chatID)
		return
	}
	turnCtx, cancel := context.WithCancel(daemonCtx)
	if !t.cmds.registerTurn(chatID, cancel) {
		cancel()
		t.reply(c, turnBusyMessage) // a turn is already running for this chat
		return
	}
	// Run the turn OFF the telebot handler goroutine: telebot dispatches updates
	// sequentially, so a synchronous turn blocks the poller — a long/hung turn would
	// freeze the whole bot AND make /cancel undeliverable (it can never reach its
	// handler to fire the cancel). Capture the stable bot + recipient before spawning
	// (the tele.Context is recycled once the handler returns). The goroutine is
	// tracked by t.wg so Stop drains it (goleak-clean).
	sender := t.sender(c)
	notifier, _ := c.Bot().(botNotifier)
	to := c.Recipient()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer cancel()
		defer t.cmds.unregisterTurn(chatID)
		stop := pulseChatAction(turnCtx, notifier, to, tele.Typing) // "Aura is working" for the whole turn
		defer stop()
		t.handleTurn(turnCtx, sender, chatID, &text, inboundWasVoice)
	}()
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

// downloadFile pulls a media file's bytes off the Telegram file server via the
// narrow botFiler seam (the same surface voice.go downloads through). The Bot-API
// getFile endpoint caps a download at the 20MB file ceiling, so the read is bounded
// upstream (T-13-10-MediaDoS); the document size tiers add the convert-side guard.
func downloadFile(filer botFiler, file *tele.File) ([]byte, error) {
	rc, err := filer.File(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}
