package agui

import (
	"context"
	"io"

	"github.com/chetto1983/aura/internal/assets"
)

// AssetService is the narrow asset API surface consumed by AG-UI handlers.
type AssetService interface {
	Presign(context.Context, assets.PresignRequest) (assets.PresignResponse, error)
	Finalize(context.Context, string, string) (assets.Asset, error)
	GetForIdentity(context.Context, string, string) (assets.Asset, error)
	// OpenForIdentity streams the owner-scoped object body for the download route (WEBART-03/D-12):
	// the ownership gate precedes any store read, and it returns a stream-through ReadCloser, never
	// a presigned store URL (D-09). The caller closes the ReadCloser.
	OpenForIdentity(context.Context, string, string) (io.ReadCloser, assets.Asset, error)
	ListForThread(context.Context, string, string) ([]assets.Asset, error)
	Promote(context.Context, string, string) (assets.Asset, error)
	Delete(context.Context, string, string) (assets.Asset, error)
	Retry(context.Context, string, string) (assets.Asset, error)
	// AdoptIntoThread claims an asset presigned before its conversation existed (the
	// first attachment of a brand new chat, written with an empty thread_id). Only an
	// unclaimed row is touched, so it can never move an attachment between threads.
	AdoptIntoThread(ctx context.Context, identityID, assetID, threadID string) (assets.Asset, error)
	// BuildTurnContext composes the channel-agnostic per-turn context (this turn's
	// attachments + the thread's knowledge catalog) onto the user text — the shared seam
	// the Telegram channel also uses, so the composition is not duplicated per channel.
	BuildTurnContext(ctx context.Context, identityID, threadID string, attachments []assets.Asset, userText string) string
}
