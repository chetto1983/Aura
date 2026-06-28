package agui

import (
	"context"

	"github.com/chetto1983/aura/internal/assets"
)

// AssetService is the narrow asset API surface consumed by AG-UI handlers.
type AssetService interface {
	Presign(context.Context, assets.PresignRequest) (assets.PresignResponse, error)
	Finalize(context.Context, string, string) (assets.Asset, error)
	GetForIdentity(context.Context, string, string) (assets.Asset, error)
	ListForThread(context.Context, string, string) ([]assets.Asset, error)
	Promote(context.Context, string, string) (assets.Asset, error)
	Delete(context.Context, string, string) (assets.Asset, error)
	Retry(context.Context, string, string) (assets.Asset, error)
	// BuildTurnContext composes the channel-agnostic per-turn context (this turn's
	// attachments + the thread's knowledge catalog) onto the user text — the shared seam
	// the Telegram channel also uses, so the composition is not duplicated per channel.
	BuildTurnContext(ctx context.Context, identityID, threadID string, attachments []assets.Asset, userText string) string
}
