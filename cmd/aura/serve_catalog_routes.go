package main

// serve_catalog_routes.go supplies every cockpit model picker with its models, all from
// OpenRouter whatever route the chat runs on: image and video from the generation tools' own
// catalog, cloud speech-to-text, text-to-speech and embeddings from OpenRouter's
// output-modality filter. Each of those backends runs on OpenRouter itself (mediaBaseURL,
// config.SpeechCloudRoute, config.EmbedRoute), so a picker lists what its backend can call.

import (
	"context"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mediagen"
)

// mediaCatalogRoute lists the shared catalog for the settings picker and the Studio on
// mediaBaseURL, the endpoint every generation credential resolves to, so the picker fills the
// same cache entry the tools read.
type mediaCatalogRoute struct {
	catalog *mediagen.Catalog
}

var _ agui.MediaCatalogLister = mediaCatalogRoute{}

func (m mediaCatalogRoute) List(ctx context.Context, kind mediagen.Kind, refresh bool) ([]mediagen.Model, error) {
	return m.catalog.List(ctx, mediaBaseURL, kind, refresh)
}

// modalityCatalogRoute lists the models OpenRouter publishes under one output modality --
// cloud STT, TTS and embeddings -- for the settings pickers.
type modalityCatalogRoute struct {
	client *http.Client
}

var _ agui.ModalityCatalogLister = modalityCatalogRoute{}

// modalityCatalogTimeout bounds one catalogue read: the picker waits on it, and a list that
// takes longer is better reported as unavailable than left spinning.
const modalityCatalogTimeout = 30 * time.Second

func newModalityCatalogRoute() modalityCatalogRoute {
	return modalityCatalogRoute{client: &http.Client{Timeout: modalityCatalogTimeout}}
}

func (m modalityCatalogRoute) List(ctx context.Context, modality string) ([]llm.ModelCatalogEntry, error) {
	return llm.FetchOutputModalityCatalog(ctx, m.client, llm.DefaultBaseURL, modality)
}

// wireMediaCatalog gives the settings picker the tools' catalog; without media dependencies the
// picker routes stay unwired and answer 503.
func wireMediaCatalog(server *agui.Server, media *mediaDeps) {
	if media == nil {
		return
	}
	server.SetMediaCatalog(mediaCatalogRoute{catalog: media.catalog})
}
