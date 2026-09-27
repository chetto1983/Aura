package main

// serve_catalog_routes.go supplies every cockpit model picker with its models. Image and video
// come from the generation tools' own catalog on OpenRouter, whatever route the chat runs on;
// cloud speech-to-text, text-to-speech and embeddings come from OpenRouter's output-modality
// filter on the live chat route, so a route saved in Settings moves those pickers at once
// instead of one of them lagging.

import (
	"context"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mediagen"
)

// catalogRoute reports the live route the modality pickers list from, or ErrCatalogLocalRoute
// when it is not OpenRouter, a keyless local host labelled openrouter included. Reading the
// snapshot per call rather than per boot is the point: the provider update saved from Settings
// applies to the very next list.
func catalogRoute(runtime *llm.Runtime) (llm.Config, error) {
	route := runtime.Snapshot().Config
	if llm.ReasoningTarget(route.Provider, route.BaseURL) != llm.ReasoningTargetOpenRouter ||
		llm.IsKeylessLocalBaseURL(route.BaseURL) {
		return llm.Config{}, agui.ErrCatalogLocalRoute
	}
	return route, nil
}

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
	runtime *llm.Runtime
	client  *http.Client
}

var _ agui.ModalityCatalogLister = modalityCatalogRoute{}

// modalityCatalogTimeout bounds one catalogue read: the picker waits on it, and a list that
// takes longer is better reported as unavailable than left spinning.
const modalityCatalogTimeout = 30 * time.Second

func newModalityCatalogRoute(runtime *llm.Runtime) modalityCatalogRoute {
	return modalityCatalogRoute{runtime: runtime, client: &http.Client{Timeout: modalityCatalogTimeout}}
}

func (m modalityCatalogRoute) List(ctx context.Context, modality string) ([]llm.ModelCatalogEntry, error) {
	route, err := catalogRoute(m.runtime)
	if err != nil {
		return nil, err
	}
	return llm.FetchOutputModalityCatalog(ctx, m.client, route.BaseURL, modality)
}

// wireMediaCatalog gives the settings picker the tools' catalog; without media dependencies the
// picker routes stay unwired and answer 503.
func wireMediaCatalog(server *agui.Server, media *mediaDeps) {
	if media == nil {
		return
	}
	server.SetMediaCatalog(mediaCatalogRoute{catalog: media.catalog})
}
