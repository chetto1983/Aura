package agui

import (
	"context"

	"github.com/chetto1983/aura/internal/assets"
)

type AssetService interface {
	Presign(context.Context, assets.PresignRequest) (assets.PresignResponse, error)
	Finalize(context.Context, string, string) (assets.Asset, error)
	GetForIdentity(context.Context, string, string) (assets.Asset, error)
	ListForThread(context.Context, string, string) ([]assets.Asset, error)
	Promote(context.Context, string, string) (assets.Asset, error)
	Delete(context.Context, string, string) (assets.Asset, error)
	Retry(context.Context, string, string) (assets.Asset, error)
}
