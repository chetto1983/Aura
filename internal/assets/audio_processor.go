//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
)

// defaultSTTBackoff is the PRD 2-retry exponential backoff (1s, 2s) — three attempts
// total. It lives here because this processor is now the ONLY inbound speech path:
// the Telegram channel used to own an identical retry loop around its own STT client,
// and when its voice notes moved onto the asset pipeline that loop stopped running.
// Telegram ingest calls processAsset INLINE (ingest_agent.go), so the durable job
// worker's minute-scale retry never covers it — without this a single transient
// sidecar blip loses the voice note outright.
var defaultSTTBackoff = []time.Duration{time.Second, 2 * time.Second}

// AudioProcessor turns an uploaded audio asset into a transcript. The OpenAI-
// compatible speech-to-text call (local faster-whisper multipart or OpenRouter
// cloud JSON, with the one config-only swap) lives in internal/multimodal; this
// processor owns the objectstore read, the transient-failure retry, and the Result
// mapping.
type AudioProcessor struct {
	Objects objectstore.Store
	STT     *multimodal.STTClient
	// Backoff is the per-retry sleep schedule. Nil → defaultSTTBackoff. Tests pin a
	// fast schedule; a zero-length slice means a single attempt.
	Backoff []time.Duration
	// PerIdentityObjects, when set, resolves the ASSET OWNER's per-identity store so the
	// object read targets the owner's bucket with the owner's creds. Nil → the shared Objects.
	PerIdentityObjects *ObjectResolverBundle
}

// NewAudioProcessor builds an audio processor over the shared STT client.
func NewAudioProcessor(objects objectstore.Store, cfg multimodal.STTConfig) *AudioProcessor {
	return &AudioProcessor{Objects: objects, STT: multimodal.NewSTTClient(cfg)}
}

func (p *AudioProcessor) ProcessAsset(ctx context.Context, asset Asset) (Result, error) {
	if p == nil || p.Objects == nil || p.STT == nil {
		return Result{}, fmt.Errorf("audio processor is not configured")
	}
	objects, err := p.PerIdentityObjects.storeForAsset(ctx, p.Objects, asset)
	if err != nil {
		return Result{}, err
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	rc, _, err := objects.Get(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = rc.Close() }()
	audioBytes, err := io.ReadAll(rc)
	if err != nil {
		return Result{}, err
	}
	transcript, err := p.transcribe(ctx, audioBytes, asset.FileName, AudioFormat(asset.MIMEType))
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:  StatusComplete,
		Summary: transcript,
		Metadata: map[string]any{
			"transcript": transcript,
		},
	}, nil
}

// transcribe runs the STT call, retrying a transient sidecar failure on the backoff
// schedule and returning the last error once the attempts are exhausted. A cancelled
// ctx aborts the wait immediately rather than sleeping out the schedule.
func (p *AudioProcessor) transcribe(ctx context.Context, audio []byte, fileName, format string) (string, error) {
	return TranscribeAudioForRetrieval(ctx, p.STT, audio, fileName, format, p.Backoff)
}

// TranscribeAudioForRetrieval is the shared speech-to-text boundary for immediate asset
// summaries and durable Garage indexing.
func TranscribeAudioForRetrieval(
	ctx context.Context, stt *multimodal.STTClient, audio []byte, fileName, format string,
	backoff []time.Duration,
) (string, error) {
	if stt == nil {
		return "", fmt.Errorf("STT client is not configured")
	}
	if backoff == nil {
		backoff = defaultSTTBackoff
	}
	var lastErr error
	for attempt := range 1 + len(backoff) {
		if attempt > 0 {
			if !sleepCtx(ctx, backoff[attempt-1]) {
				return "", ctx.Err()
			}
		}
		transcript, err := stt.Transcribe(ctx, audio, fileName, format)
		if err == nil {
			return transcript, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("stt: transcription failed after %d attempts: %w", 1+len(backoff), lastErr)
}

// sleepCtx waits for d, reporting false if ctx was cancelled during the wait.
func sleepCtx(ctx context.Context, d time.Duration) bool {
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

// AudioFormat maps an asset MIME to the container hint the cloud STT JSON route
// needs (the local multipart route ignores it — the file part carries the
// container). It first strips any media-type parameter: a browser MediaRecorder
// blob reports "audio/webm;codecs=opus" or "audio/mp4;codecs=mp4a.40.2", so the
// bare type is taken before the first ';' (and trimmed) — otherwise every such
// recording falls through to the "ogg" default and is mis-tagged. Defaults to
// "ogg", the Telegram/voice-note container.
func AudioFormat(mimeType string) string {
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = mimeType[:i]
	}
	switch strings.TrimSpace(mimeType) {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/x-wav":
		return "wav"
	case "audio/webm":
		return "webm"
	case "audio/flac":
		return "flac"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return "m4a"
	default:
		return "ogg"
	}
}
