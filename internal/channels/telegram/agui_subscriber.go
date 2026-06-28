// Package telegram — this file is the per-turn AG-UI fanout consumer (the
// Phase-12 seam). It is the single biggest anti-re-implementation point of the
// phase (research §"Don't Hand-Roll"): it MUST consume internal/agui/fanout.go,
// never rebuild event distribution.
package telegram

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/reasoningtrace"
	"github.com/google/uuid"
)

// eventConsumer drains one AG-UI subscriber channel to completion (the channel is
// closed by the Fanout producer on source-end/ctx-cancel). The status pane and the
// renderer each implement it. Declaring the seam lets handleTurn wire two
// consumers generically and lets the Task-1 fanout test inject recorders that
// prove the per-turn Subscribe×2-before-Run distribution without the full render
// stack.
type eventConsumer interface {
	consume(ctx context.Context, ch <-chan events.Event)
}

// consumerFactory builds the THREE per-turn consumers (status pane → msg #1,
// renderer → msg #2, artifact → sendDocument) bound to a chat. It is a field on
// Telegram so production wires the real status_pane/renderer/artifact and tests
// wire recorders. The default factory (set in consumers) builds the real
// consumers.
type consumerFactory func(bot botSender, to tele.Recipient) (status, content, artifact eventConsumer)

// finalTexter exposes the accumulated answer text a content consumer rendered, read
// after consume returns to feed the optional TTS-out. The production renderer
// implements it; a test recorder may implement it to drive the TTS path.
type finalTexter interface {
	finalText() string
}

// handleTurn drives ONE turn through the per-turn fanout wiring (research §2):
//
//	Translate(convID, runID, idgen, runner.Turn(...))  → an AG-UI events.Event stream
//	NewFanout(translated)
//	statusCh   := fo.Subscribe()  // status pane (msg #1)
//	contentCh  := fo.Subscribe()  // content     (msg #2)
//	artifactCh := fo.Subscribe()  // artifact    (sendDocument)  — ALL THREE before Run
//	fo.Run(ctx)                   // single producer goroutine, closes every chan on end/cancel
//
// Subscribe MUST register every consumer before Run (fanout.go panics on
// Subscribe-after-Run); exactly one Fanout drives one turn (fanout.go panics on
// double-Run). A fresh Fanout is built per turn — never one at channel start. The
// fanout supports N subscribers, so the artifact consumer is just a third channel.
//
// userMsg is the inbound user message for a fresh turn; nil drives a CONTINUATION
// turn (runner.Turn(ctx, convID, nil)) — the resume path after a HITL pause
// resolves, so the resumed answer renders through the SAME per-turn fanout as a
// normal message (plan 13-10: a tap/reply resumes the loop AND the user sees it).
//
// inboundWasVoice flags the echo-modality TTS-out: after the content render
// completes, when ShouldSpeak (voice-mode pref OR the inbound was a voice note) the
// final answer is synthesized to a voice note. The TTS-out runs AFTER the text
// render (it never blocks it) and is ctx-cancel-aware.
func (t *Telegram) handleTurn(ctx context.Context, bot botSender, chatID int64, userMsg *string, inboundWasVoice bool) {
	idgen := agui.NewIDGenerator()
	runID := uuid.NewString()

	translated := agui.Translate(convID(chatID), runID, idgen, t.deps.Turn(ctx, convID(chatID), userMsg), t.deps.ShowReasoning)
	fo := agui.NewFanout(translated)
	statusCh := fo.Subscribe()   // → status pane
	contentCh := fo.Subscribe()  // → renderer
	artifactCh := fo.Subscribe() // → artifact (ALL THREE before Run)
	reasoningtrace.Record("telegram_handle_turn_subscribers", map[string]any{
		"chat_id": chatID,
		"conv_id": convID(chatID),
		"run_id":  runID,
		"subscribers": []map[string]any{
			{"index": 0, "consumer": "status"},
			{"index": 1, "consumer": "content"},
			{"index": 2, "consumer": "artifact"},
		},
	})
	fo.Run(ctx)

	to := tele.ChatID(chatID)
	pane, rend, art := t.consumers(bot, to)

	// The status pane + artifact consumer each drain on their own goroutine; the
	// renderer consumes inline on the handler goroutine (it streams msg #2 and
	// returns when the content channel closes). Every channel is closed by the sole
	// Fanout producer on source-end/ctx-cancel, so all three consumers terminate
	// without a leak (goleak).
	paneDone := make(chan struct{})
	artDone := make(chan struct{})
	go func() {
		defer close(paneDone)
		pane.consume(ctx, statusCh)
	}()
	go func() {
		defer close(artDone)
		art.consume(ctx, artifactCh)
	}()
	rend.consume(ctx, contentCh)
	<-paneDone
	<-artDone

	// HITL pause render: a turn that called ask_user suspended the loop with NO final
	// answer (the translator emitted RUN_FINISHED-interrupt, not success), leaving a
	// pending paused_states row. Render it as an inline keyboard / ForceReply so the
	// user can answer — this is the render half of HITL, the counterpart to the
	// onCallback/onReply resolve half. PendingFor is the ground truth: a turn that
	// finished normally leaves none, so this no-ops on the common path; a continuation
	// turn that pauses again re-enters here and renders the next pause the same way.
	t.promptPendingPause(ctx, bot, chatID)

	// TTS-out (after the text render, never blocking it): synthesize the final answer
	// to a voice note when ShouldSpeak. Skipped on a cancelled ctx (a /cancel mid-turn
	// must not still produce a voice note) or when TTS is unconfigured.
	t.speakIfNeeded(ctx, bot, to, rend, inboundWasVoice)
}

// speakIfNeeded synthesizes the rendered answer to a Telegram voice note when the
// TTS trigger fires (voice-mode pref OR an inbound voice note — ShouldSpeak). It is
// best-effort: a TTS sidecar failure is logged, never surfaced (the text answer
// already reached the user). It no-ops on a cancelled ctx, an unconfigured TTS
// sidecar, an empty answer, or a content consumer that does not expose finalText.
func (t *Telegram) speakIfNeeded(ctx context.Context, bot botSender, to tele.Recipient, content eventConsumer, inboundWasVoice bool) {
	if ctx.Err() != nil || t.tts == nil || !t.tts.configured {
		return
	}
	if !ShouldSpeak(VoiceModePref(""), inboundWasVoice) {
		return
	}
	ft, ok := content.(finalTexter)
	if !ok {
		return
	}
	text := ft.finalText()
	if text == "" {
		return
	}
	if _, err := t.tts.Speak(ctx, bot, to, text); err != nil {
		slog.Warn("telegram: tts-out failed", "err", err)
	}
}

// consumers builds the per-turn status + content + artifact consumers via the
// configured factory, falling back to the production status_pane/renderer/artifact
// when no factory was injected (the common path; tests inject recorders).
func (t *Telegram) consumers(bot botSender, to tele.Recipient) (status, content, artifact eventConsumer) {
	if t.deps.consumerFactory != nil {
		return t.deps.consumerFactory(bot, to)
	}
	pane := newStatusPane(bot, to, t.statusThrottle(), t.deps.ShowReasoning, t.reasoningFIFORunes())
	rend := newRenderer(bot, to, t.contentThrottle(), t.chatRateLimit())
	art := newArtifact(bot, to)
	return pane, rend, art
}

// convID derives the conversation id for a Telegram chat. One conversation per
// chat keeps the per-chat thread stable across turns (the runner persists it). The
// namespace is deterministic so the same chat always resolves the same thread.
//
// NOTE: this is the channel-side mapping; the composition root (plan 13-07) ensures
// the conversation row exists. Kept here so handleTurn is self-contained for the
// unit tier.
func convID(chatID int64) string {
	return uuid.NewSHA1(telegramChatNamespace, []byte(strconv.FormatInt(chatID, 10))).String()
}

// telegramChatNamespace is a fixed namespace so chat→conversation ids are stable
// and collision-free across restarts (a random-but-fixed UUID, not a well-known
// one, so it never collides with another subsystem's UUIDv5 namespace).
var telegramChatNamespace = uuid.MustParse("7e1e9a3a-0e3a-4b6c-9d2f-3a1b2c4d5e6f")
