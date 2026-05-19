package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/aura/aura/internal/llm/whisper"
	source "github.com/aura/aura/internal/storage/sources/store"
	tele "gopkg.in/telebot.v4"
)

// voiceConcurrencyLimit is the maximum number of voice memo storage jobs in
// flight at once. Voice memos are short (<60s avg) and the whisper sidecar
// (wired in US-MM-A02) handles one inference at a time on the 4-thread
// mini-PC, so serializing to 1 avoids queueing pressure at the sidecar.
const voiceConcurrencyLimit = 1

// AfterTranscribeHook is invoked once a voice source reaches StatusTranscribeComplete.
// teleCtx is the original Telegram context captured from the voice handler goroutine
// closure, so the hook can route the agent reply to the correct Telegram chat.
// Returning an error is logged but the transcript is already durable on disk —
// the voice memo will not be re-sent and the source is not rolled back.
type AfterTranscribeHook func(ctx context.Context, teleCtx tele.Context, src *source.Source, transcript string) error

// voiceHandlerConfig is the per-Bot wiring for voice memo ingestion
// (US-MM-A01: store; US-MM-A02: transcribe; US-MM-A03: Hub dispatch).
type voiceHandlerConfig struct {
	Bot     *tele.Bot
	Sources source.Repository
	// Whisper is optional. nil = store only, no transcription.
	Whisper         *whisper.Client
	WhisperLanguage string // defaults to "it" when empty
	// AfterTranscribe is optional. When set it is called after StatusTranscribeComplete
	// is persisted so the transcript can be dispatched into the agent loop.
	// nil = transcript stored on disk only; no Hub dispatch.
	AfterTranscribe AfterTranscribeHook
	Allowlist       func(userID string) bool
	Logger          *slog.Logger
	// Parent is the optional parent context. When non-nil the voiceHandler's
	// internal context derives from it so App shutdown propagates without a
	// separate Stop() call. Nil falls back to context.Background().
	Parent context.Context
}

type voiceHandler struct {
	bot             *tele.Bot
	sources         source.Repository
	whisper         *whisper.Client
	whisperLanguage string
	afterTranscribe AfterTranscribeHook
	allowed         func(string) bool
	logger          *slog.Logger
	sem             chan struct{}
	mu              sync.Mutex
	closed          bool
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
}

func newVoiceHandler(cfg voiceHandlerConfig) *voiceHandler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	parent := cfg.Parent
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	lang := cfg.WhisperLanguage
	if lang == "" {
		lang = "it"
	}
	return &voiceHandler{
		bot:             cfg.Bot,
		sources:         cfg.Sources,
		whisper:         cfg.Whisper,
		whisperLanguage: lang,
		afterTranscribe: cfg.AfterTranscribe,
		allowed:         cfg.Allowlist,
		logger:          cfg.Logger,
		sem:             make(chan struct{}, voiceConcurrencyLimit),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Stop cancels queued/running voice work and waits for all workers to exit.
func (h *voiceHandler) Stop() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
}

func (h *voiceHandler) beginWork() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.wg.Add(1)
	return true
}

func (h *voiceHandler) finishWork() {
	if h == nil {
		return
	}
	h.wg.Done()
}

// onVoiceMessage is a Telegram tele.Handler. Validates synchronously, sends
// the initial progress reply, and spawns a goroutine for the rest. Returns nil
// quickly so the Telegram update loop is never blocked.
func (h *voiceHandler) onVoiceMessage(c tele.Context) error {
	voice := c.Message().Voice
	if voice == nil {
		return nil
	}

	userID := strconv.FormatInt(c.Sender().ID, 10)
	if h.allowed != nil && !h.allowed(userID) {
		h.logger.Warn("voice message from non-allowlisted user", "user_id", userID)
		return nil
	}

	if h.sources == nil {
		_ = c.Reply("Source store unavailable.")
		return nil
	}

	if !h.beginWork() {
		return nil
	}

	progress, err := h.bot.Send(c.Chat(), "🎙️ Got it — saving voice memo…")
	if err != nil {
		h.finishWork()
		h.logger.Error("send initial progress reply failed", "user_id", userID, "err", err)
		return nil
	}

	go func() {
		defer h.finishWork()
		h.process(h.ctx, userID, voice, c, progress)
	}()
	return nil
}

// process downloads the OGG audio, stores it as an immutable source, and
// (when a Whisper client is configured) transcribes it. teleCtx is the original
// Telegram context captured from onVoiceMessage's goroutine closure — it is
// forwarded to AfterTranscribeHook so the hook can route the reply correctly.
// Each step edits the same progress message; failures replace its text with a
// single-line error.
func (h *voiceHandler) process(ctx context.Context, userID string, voice *tele.Voice, teleCtx tele.Context, progress *tele.Message) {
	// Bounded concurrency: queue if a job is already in flight.
	select {
	case h.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-h.sem }()

	editor := newProgressEditor(h.bot, progress, h.logger)

	// Step 1: download from Telegram.
	body, err := h.bot.File(&voice.File)
	if err != nil {
		editor.fail("Download failed: " + err.Error())
		return
	}
	oggBytes, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		editor.fail("Read failed: " + err.Error())
		return
	}

	// Step 2: store as immutable source (SHA-256 deduplication in store).
	filename := fmt.Sprintf("voice_%d.ogg", time.Now().Unix())
	src, _, err := h.sources.Put(ctx, source.PutInput{
		Kind:     source.KindAudio,
		Filename: filename,
		MimeType: "audio/ogg",
		Bytes:    oggBytes,
	})
	if err != nil {
		editor.fail("Save failed: " + err.Error())
		return
	}

	// Step 3: transcribe (skipped when Whisper client is nil).
	if h.whisper == nil {
		editor.set(fmt.Sprintf("✅ Stored · %s · %s · ready for transcription",
			src.ID, formatSize(src.SizeBytes)))
		h.logger.Info("voice stored (transcription disabled)",
			"user_id", userID,
			"source_id", src.ID,
			"size_bytes", src.SizeBytes,
		)
		return
	}

	editor.set(fmt.Sprintf("📥 Stored · %s · %s · transcribing…",
		src.ID, formatSize(src.SizeBytes)))

	result, err := h.whisper.Transcribe(ctx, oggBytes, "audio/ogg", h.whisperLanguage)
	if err != nil {
		_, _ = h.sources.Update(src.ID, func(s *source.Source) error {
			s.Status = source.StatusFailed
			s.Error = err.Error()
			return nil
		})
		editor.fail("Transcription failed: " + err.Error())
		h.logger.Warn("whisper transcription failed",
			"user_id", userID,
			"source_id", src.ID,
			"err", err,
		)
		return
	}

	// Step 4: write transcript.txt next to the source files.
	if err := writeNextToSource(h.sources, src.ID, "transcript.txt", []byte(result.Text)); err != nil {
		editor.fail("Write transcript failed: " + err.Error())
		return
	}

	// Step 5: delete original.ogg — voice memos are privacy-sensitive;
	// the transcript is the authoritative artifact. Only purge on success.
	durationS := float64(voice.Duration)
	originalPath := h.sources.Path(src.ID, "original.ogg")
	var deletedAt *time.Time
	if originalPath != "" {
		if removeErr := os.Remove(originalPath); removeErr != nil && !os.IsNotExist(removeErr) {
			h.logger.Warn("voice original purge failed", "source_id", src.ID, "err", removeErr)
		} else {
			t := time.Now().UTC()
			deletedAt = &t
		}
	}

	// Step 6: flip status + attach transcript metadata.
	meta := &source.TranscriptMeta{
		Text:      result.Text,
		DurationS: durationS,
		ElapsedMs: result.ElapsedMs,
		Language:  h.whisperLanguage,
	}
	updated, err := h.sources.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusTranscribeComplete
		s.Transcript = meta
		s.OriginalDeletedAt = deletedAt
		s.Error = ""
		return nil
	})
	if err != nil {
		editor.fail("Status update failed: " + err.Error())
		return
	}

	h.logger.Info("voice transcribed",
		"user_id", userID,
		"source_id", src.ID,
		"duration_s", durationS,
		"elapsed_ms", result.ElapsedMs,
		"text_len", len(result.Text),
	)

	// Hub dispatch path: when AfterTranscribe is wired, the assistant reply
	// will arrive through the normal telegramadapter outbound. Delete the
	// progress chrome so the chat shows only [voice memo] -> [assistant reply],
	// no "Transcribed..." ghost. On hook failure, keep the progress with the
	// error tail so the user sees what went wrong.
	if h.afterTranscribe != nil {
		hookCtx, hookCancel := context.WithTimeout(ctx, time.Minute)
		err := h.afterTranscribe(hookCtx, teleCtx, updated, result.Text)
		hookCancel()
		if err != nil {
			h.logger.Warn("afterTranscribe hook failed", "source_id", src.ID, "err", err)
			editor.set(fmt.Sprintf("✅ Transcribed · %s · %.0fs audio · %d chars · audio purged · agent dispatch failed: %s",
				updated.ID, durationS, len(result.Text), err.Error()))
			return
		}
		if delErr := h.bot.Delete(progress); delErr != nil {
			// Non-fatal: the chat still works, just keeps the ghost line.
			h.logger.Debug("voice progress delete failed", "source_id", src.ID, "err", delErr)
		}
		return
	}

	// No Hub dispatch wired: leave the final progress message in place so the
	// user has a confirmation (no assistant reply is coming to supersede it).
	editor.set(fmt.Sprintf("✅ Transcribed · %s · %.0fs audio · %d chars · audio purged · ready for ingest",
		updated.ID, durationS, len(result.Text)))
}
