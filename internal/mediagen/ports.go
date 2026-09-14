package mediagen

import (
	"context"
	"time"
)

// Kind selects which live model a Settings port resolves.
type Kind string

// The two generation kinds a Settings port's Model resolves a live model for.
const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
)

// Default values for the four live media settings (ruling R3: aura.settings rows,
// like the primary LLM model — no env var is read directly for any of them). A
// stored row overrides its default; cmd/aura's mediagen.Settings implementation
// reads the store on every call, so a saved value is live without a restart.
const (
	DefaultImageModel = "microsoft/mai-image-2.6"
	DefaultVideoModel = "minimax/hailuo-3-max"
	// DefaultVideoInlineWaitSec is the ceiling seconds a video generation call
	// waits inline before detaching to the watcher. Zero is a valid stored value
	// (detach immediately); negative is invalid.
	DefaultVideoInlineWaitSec = 45
	// DefaultAssetMaxVideoBytes is read once at boot (row -> default), never
	// per call: it sizes assets.Limits.MaxVideoBytes and the media path.
	DefaultAssetMaxVideoBytes int64 = 52428800
)

// MediaCredentials resolves the OpenRouter base URL + API key an identity's
// image/video generation call uses, reusing the same per-identity credit
// decision (CRED-05) the chat LLM path already makes instead of a second one.
type MediaCredentials interface {
	For(ctx context.Context, identityID string) (baseURL, apiKey string, err error)
}

// Settings is the live media-model/wait port over aura.settings (ruling R3).
// Both methods read the store fresh on every call.
type Settings interface {
	// Model returns the live model for kind: the stored aura.settings row when
	// present, else the matching Default*Model constant.
	Model(ctx context.Context, kind Kind) (string, error)
	// VideoInlineWait returns the live ceiling a video generation call waits
	// inline before detaching to the watcher.
	VideoInlineWait(ctx context.Context) (time.Duration, error)
}
