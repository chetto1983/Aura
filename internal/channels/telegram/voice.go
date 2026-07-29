// Package telegram — this file is the speech-to-text sidecar client (UX-04). A
// Telegram voice note arrives as OGG/Opus; voice.go downloads the bytes and POSTs
// them DIRECTLY (mime/multipart) to the aura-stt /v1/audio/transcriptions
// endpoint. faster-whisper decodes Opus inline (PyAV) — there is NO ffmpeg
// pre-step (spike 027: that is exactly why faster-whisper beat whisper.cpp). The
// transcript text becomes a normal text user message fed to runner.Turn by the
// handler (plan 13-09); on a persistent sidecar failure the client hard-fails
// with the IT UX copy + a 😵 reaction.
package telegram

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/chetto1983/aura/internal/multimodal"
	tele "gopkg.in/telebot.v4"
)

// transcribeFailMessage is the user-facing hard-fail copy (always Italian — the
// output-language directive). Sent by the handler when Transcribe returns an
// error after exhausting retries.
const transcribeFailMessage = "❌ Trascrizione non disponibile."

// hardFailReaction is the 😵 emoji applied to the voice message on a hard STT
// failure (a non-intrusive signal the note could not be transcribed).
const hardFailReaction = "😵"

// defaultRetryBackoffMS is the PRD 2-retry exp backoff (1s, 2s) — three total
// attempts. voice.go retries a transient sidecar failure before hard-failing.
var defaultRetryBackoffMS = []int{1000, 2000}

// botFiler is the narrow telebot surface voice.go needs to pull the OGG bytes off
// the Telegram file server. *tele.Bot satisfies it (Bot.File). Declared as a seam
// so unit tests inject canned OGG bytes with no network.
type botFiler interface {
	File(file *tele.File) (io.ReadCloser, error)
}

// botReactor is the narrow telebot surface for the hard-fail 😵 reaction.
// *tele.Bot satisfies it (Bot.React).
type botReactor interface {
	React(to tele.Recipient, msg tele.Editable, r tele.Reactions) error
}

// voiceClient transcribes OGG/Opus voice notes via the shared STT client and owns
// the telegram glue (telebot download + retry/backoff + hard-fail reaction). Zero
// Go ML; the local↔cloud STT branch lives inside multimodal.STTClient.
type voiceClient struct {
	stt     *multimodal.STTClient
	backoff []time.Duration
}

// newVoiceClient builds an STT client over the multimodal config. The retry
// schedule comes from cfg.RetryBackoff (ms) when set, else the PRD 1s/2s default.
func newVoiceClient(cfg MultimodalConfig) *voiceClient {
	schedule := cfg.RetryBackoff
	if schedule == nil {
		schedule = defaultRetryBackoffMS
	}
	backoff := make([]time.Duration, len(schedule))
	for i, ms := range schedule {
		backoff[i] = time.Duration(ms) * time.Millisecond
	}
	return &voiceClient{
		stt: multimodal.NewSTTClient(multimodal.STTConfig{
			LocalBaseURL:      cfg.STTBaseURL,
			LocalModel:        cfg.STTModel,
			Language:          cfg.STTLanguage,
			CloudModel:        cfg.STTCloudModel,
			OpenRouterBaseURL: cfg.OpenRouterBaseURL,
			OpenRouterAPIKey:  cfg.OpenRouterAPIKey,
			TimeoutSec:        cfg.TimeoutSec,
		}),
		backoff: backoff,
	}
}

// Transcribe downloads the voice note's OGG/Opus bytes via the bot file server
// and POSTs them directly to STT_BASE_URL/audio/transcriptions (multipart, no
// ffmpeg). It retries a transient failure on the backoff schedule (2 retries by
// default) and returns the transcript text on success or the last error after the
// final attempt. The caller turns the transcript into a text user message; on
// error it sends transcribeFailMessage + HardFail.
func (v *voiceClient) Transcribe(ctx context.Context, bot botFiler, _ tele.Recipient, voice *tele.Voice) (string, error) {
	ogg, err := v.download(bot, voice)
	if err != nil {
		return "", fmt.Errorf("telegram stt: download voice: %w", err)
	}

	attempts := 1 + len(v.backoff)
	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			if !v.sleep(ctx, v.backoff[attempt-1]) {
				return "", ctx.Err()
			}
		}
		transcript, txErr := v.postTranscription(ctx, ogg)
		if txErr == nil {
			return transcript, nil
		}
		lastErr = txErr
	}
	return "", fmt.Errorf("telegram stt: transcription failed after %d attempts: %w", attempts, lastErr)
}

// HardFail applies the 😵 reaction to the inbound voice MESSAGE after a
// transcription failure (the reaction targets the message, not the media). The
// user-facing transcribeFailMessage text is sent by the handler (which holds the
// botSender); HardFail owns only the reaction so voice.go does not need the full
// send surface.
func (v *voiceClient) HardFail(bot botReactor, to tele.Recipient, msg tele.Editable) error {
	return bot.React(to, msg, tele.Reactions{
		Reactions: []tele.Reaction{{Type: tele.ReactionTypeEmoji, Emoji: hardFailReaction}},
	})
}

// download pulls the voice note's bytes off the Telegram file server. The bytes
// are the original OGG/Opus container — they are POSTed as-is (faster-whisper
// decodes Opus inline; no transcode).
func (v *voiceClient) download(bot botFiler, voice *tele.Voice) ([]byte, error) {
	rc, err := bot.File(&voice.File)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// postTranscription delegates one transcription attempt to the shared STT client.
// Telegram voice notes are OGG/Opus, so the format hint is "ogg" and the multipart
// part name is voice.ogg. A non-2xx response is a *multimodal.StatusError the retry
// loop treats as transient.
func (v *voiceClient) postTranscription(ctx context.Context, ogg []byte) (string, error) {
	return v.stt.Transcribe(ctx, ogg, "voice.ogg", "ogg")
}

// sleep waits for d honoring ctx-cancel. It returns false if ctx was cancelled
// during the wait (the caller aborts the retry loop).
func (v *voiceClient) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
