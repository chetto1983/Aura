package main

import (
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/settings"
)

// wireMediaTools gives the retained media tools their live dependencies once the asset
// service exists. The catalog and client are built once here, for every media tool, and
// share one HTTP client with no overall timeout: a generation is bounded by the tool
// call's context, not by a transport deadline shorter than it.
//
// A tool that cannot be served is left with no dependencies at all, so it refuses before
// any paid request instead of running on a partial wiring.
func wireMediaTools(chat *chatEnv) {
	image := chat.toolHandles.ImageGenerate
	if image == nil || chat.assets == nil {
		return
	}
	store, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("aura serve: media settings unavailable — image generation refused", "err", err)
		return
	}
	credentials := mediaCredentials{}
	// identityLLMResolver answers a nil pointer without a pool; stored in the interface it
	// would stop reading as nil and be called through.
	if resolver := identityLLMResolver(chat); resolver != nil {
		credentials.resolver = resolver
	}
	httpClient := &http.Client{}
	maxImageBytes := chat.assets.Limits.MaxImageBytes

	image.Credentials = credentials
	image.Settings = newMediaSettings(store)
	image.Catalog = mediagen.NewCatalog(httpClient)
	image.Client = mediagen.NewClient(httpClient, maxImageBytes)
	image.References = mediaAssetAdapter{svc: chat.assets}
	image.Assets = sendFileAssetAdapter{svc: chat.assets}
	image.MaxImageBytes = maxImageBytes
}
