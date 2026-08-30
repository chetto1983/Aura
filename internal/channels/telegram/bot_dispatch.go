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
//   - OnVoice / OnPhoto / OnDocument: ONE path — getFile → assets.IngestTelegramFile
//     → the shared processor for the modality (STT / vision / catalog registration)
//     → a turn carrying the attachment block. The channel does not decide how an
//     attachment is read; it carries the bytes and renders the outcome. A failed
//     ingest sends the per-modality IT copy (plus the 😵 reaction for voice).
//   - OnCallback (callbackUnique) / OnReply: hitl resolves the pause through the
//     Runner and resumes the SAME loop, rendering the continuation through the
//     per-turn fanout.
package telegram

import (
	"context"
	"log/slog"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/runner"
	tele "gopkg.in/telebot.v4"
)

// photoMIME is the MIME the OnPhoto handler attaches to a downloaded photo.
// Telegram delivers photos as JPEG and the Photo type carries no mime_type, so the
// vision data: URL is built as image/jpeg.
const photoMIME = "image/jpeg"

// User-facing dispatch copy (always Italian — the output-language directive). An
// ingest failure degrades to a short message, never a stack trace or a silent drop.
// The refuse copies name the actual cause so a rejected upload is actionable: a file
// the operator can shrink or re-export reads differently from a sidecar being down.
const (
	describeFailMessage     = "❌ Non sono riuscito a ricevere la foto: riprova."
	convertFailMessage      = "❌ Non sono riuscito a ricevere il documento: riprova."
	assetTooLargeMessage    = "❌ File troppo grande: riprova con uno più piccolo."
	assetUnsupportedMessage = "❌ Formato non supportato: inviami un PDF, un Office, un CSV o del testo."
	turnBusyMessage         = "⏳ Sto ancora elaborando la richiesta precedente. Usa /cancel per annullarla."
)

// buildDispatch constructs the per-channel dispatch instances ONCE from Deps. Called
// under t.mu from Start. The only sidecar client left here is TTS: it is the outbound
// voice-note leg, which no other channel has. Everything inbound goes through the
// shared asset pipeline.
func (t *Telegram) buildDispatch() {
	t.cmds = newCommands(commandDeps{
		Search:  t.deps.Search,
		Cost:    t.deps.Cost,
		Spend:   t.deps.Spend,
		Clear:   t.deps.Clear,
		Prices:  t.deps.Prices,
		Model:   t.deps.Model,
		Runtime: t.deps.LLMRuntime,
	})
	var onboardStore onboardingStore
	if t.deps.Store != nil {
		onboardStore = t.deps.Store
	}
	t.onboard = newOnboarding(onboardStore)
	t.profile = newProfileOnboarding(t.deps.Profile, t.accountsForDispatch())
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
		// 1) Command intercept — a handled /command never reaches the LLM.
		if handled, reply := t.cmds.dispatchRich(daemonCtx, chatID, text); handled {
			t.replyCommand(c, reply)
			return nil
		}
		// 2) A pending pause + a non-command message → a free-text HITL answer.
		if t.hitlHandlesText(daemonCtx, c, chatID, text) {
			return nil
		}
		// The seed-form nudge is ADDITIVE: it is sent alongside the turn, never instead of
		// it. Swallowing an operator's first message to show a pointer is hostile.
		if out, ok := t.profileForDispatch().nudge(daemonCtx, chatID, telegramUserIDFromMessage(msg)); ok {
			t.reply(c, out)
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
		token, action, _, valid := parseCallback(cb.Data)
		// Dettagli reveals the full question and leaves the pause open — it must not reach
		// handleCallbackResult's resolve path at all.
		if valid && action == actionDetails {
			t.revealApprovalDetails(daemonCtx, c, chatID, token)
			return nil
		}
		// Acknowledge immediately so Telegram clears the button spinner and shows a
		// small toast before any continuation turn starts rendering.
		_ = c.Respond(callbackToast(action, valid))
		out := t.hitlFor(c, chatID).handleCallbackResult(daemonCtx, cb.Data, convID(chatID), func(callbackOutcome) {
			t.disarmCallbackKeyboard(c.Bot(), cb.Message)
			t.trackPausePrompt(chatID, nil) // this prompt is now disarmed; drop the tracked handle
		})
		if !out.submitted {
			return nil
		}
		// Render the runner's verdict — one switch for BOTH legs (in-turn relay and the
		// scheduler sweep push), which is the whole point of the consolidation.
		switch out.outcome {
		case runner.OutcomeApproved, runner.OutcomeRejected:
			// The scheduled-task ResumeHook already activated/cancelled the task and no turn
			// runs, so without this edit the press leaves only a transient toast — it reads as
			// "nothing happened" (fix-plan 1.7 defect E).
			t.editApprovalOutcome(c.Bot(), cb.Message, out.outcome)
		case runner.OutcomePending:
			t.promptPendingPause(daemonCtx, t.sender(c), chatID) // more FIFO pauses: render the next
		case runner.OutcomeContinue, runner.OutcomeTerminated:
			// Continue drove a continuation turn whose handleTurn renders any further pause
			// (double-rendering it here would duplicate the prompt); Terminated was auto-resolved
			// by the Runner and has nothing left to show.
		}
		return nil
	}
}

// revealApprovalDetails answers a Dettagli tap by editing the prompt to the pause's full bounded
// question with the keyboard left armed, so the operator reads first and decides after. It is a
// pure render over the pending read the channel already performs — no new data source, and the
// question is the server-sanitized one. A stale token (pause already resolved) leaves the message
// untouched rather than blanking it.
func (t *Telegram) revealApprovalDetails(ctx context.Context, c tele.Context, chatID int64, token string) {
	_ = c.Respond(&tele.CallbackResponse{Text: "Dettagli"})
	cb := c.Callback()
	if cb == nil || cb.Message == nil || c.Bot() == nil {
		return
	}
	question := t.hitlFor(c, chatID).questionFor(ctx, convID(chatID), token)
	// Nothing to reveal (stale token) or already revealed: editing a message to its own content
	// is a 400 from Telegram, not a no-op, so guard rather than let the API reject it.
	if question == "" || question == cb.Message.Text {
		return
	}
	// Re-arm WITHOUT Dettagli: the question is now on screen, so the button has nothing left to
	// show and a second tap would be that same rejected edit.
	if _, err := c.Bot().Edit(cb.Message, question, approvalMarkup(token)); err != nil {
		slog.Warn("telegram approval: details reveal failed", "err", err)
	}
}

// editApprovalOutcome replaces a resolved approval prompt with its outcome and clears the inline
// keyboard in one edit. The copy is channel-owned Italian: the runner emits a semantic code only
// (it is not locale-aware). Best-effort — on failure it still disarms the keyboard so a resolved
// prompt is never left tappable.
func (t *Telegram) editApprovalOutcome(bot tele.API, msg *tele.Message, outcome runner.ResolveOutcome) {
	if bot == nil || msg == nil {
		return
	}
	text := "✅ Task pianificato approvato — è attivo e partirà all'orario previsto."
	if outcome == runner.OutcomeRejected {
		text = "❌ Task pianificato rifiutato — annullato."
	}
	if _, err := bot.Edit(msg, text, &tele.ReplyMarkup{}); err != nil {
		slog.Warn("telegram approval: outcome edit failed", "err", err)
		t.disarmCallbackKeyboard(bot, msg)
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

// onVoice ingests a voice note as an audio asset. The transcription happens in the
// shared pipeline (assets.AudioProcessor over multimodal.STTClient); a failed ingest
// sends the IT copy + the 😵 reaction and drives no turn.
func (t *Telegram) onVoice(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil || msg.Voice == nil {
			return nil
		}
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // STT + turn can take seconds
		defer stop()
		return t.ingestTelegramAsset(daemonCtx, c, msg, &msg.Voice.File,
			telegramVoiceFileName(), telegramVoiceMIME(msg.Voice),
			assets.ModalityAudio, msg.Voice.FileSize, "", transcribeFailMessage, true)
	}
}

// onPhoto ingests a photo as an image asset. The vision description and the catalog
// registration both happen in the shared pipeline (assets.ImageDocumentProcessor).
func (t *Telegram) onPhoto(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil || msg.Photo == nil {
			return nil
		}
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // vision + turn can take seconds
		defer stop()
		return t.ingestTelegramAsset(daemonCtx, c, msg, &msg.Photo.File,
			"photo.jpg", photoMIME,
			assets.ModalityImage, msg.Photo.FileSize, msg.Caption, describeFailMessage, false)
	}
}

// onDocument ingests a document as an asset: the object store holds the bytes and the
// catalog holds the row, so the agent finds it with document_search and reads it with
// document_open — the same contract the cockpit relies on. The channel does no
// conversion of its own.
func (t *Telegram) onDocument(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		msg := c.Message()
		if msg == nil || msg.Chat == nil || msg.Document == nil {
			return nil
		}
		if !t.requireLinkedMessage(daemonCtx, c, msg) {
			return nil
		}
		stop := keepWorking(daemonCtx, c, tele.Typing) // download + ingest + turn
		defer stop()
		return t.ingestTelegramAsset(daemonCtx, c, msg, &msg.Document.File,
			msg.Document.FileName, msg.Document.MIME,
			assets.ModalityDocument, msg.Document.FileSize, msg.Caption, convertFailMessage, false)
	}
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
