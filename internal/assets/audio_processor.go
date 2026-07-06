//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"context"
	"fmt"
	"io"

	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
)

// AudioProcessor turns an uploaded audio asset into a transcript. The OpenAI-
// compatible speech-to-text call (local faster-whisper multipart or OpenRouter
// cloud JSON, with the one config-only swap) lives in internal/multimodal; this
// processor owns only the objectstore read and the Result mapping.
type AudioProcessor struct {
	Objects objectstore.Store
	STT     *multimodal.STTClient
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
	transcript, err := p.STT.Transcribe(ctx, audioBytes, asset.FileName, audioFormat(asset.MIMEType))
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

// audioFormat maps an asset MIME to the container hint the cloud STT JSON route
// needs (the local multipart route ignores it — the file part carries the
// container). Defaults to "ogg", the Telegram/voice-note container.
func audioFormat(mimeType string) string {
	switch mimeType {
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
