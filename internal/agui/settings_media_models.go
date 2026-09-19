package agui

// settings_media_models.go answers the cockpit's image and video model boxes
// (GET /api/settings/image-models and /video-models) from the daemon's one media catalog, the
// cache the generation tools clamp against. Unlike llm-models there is no probe target to
// accept: the catalog is read on the route the daemon runs on, so a picker and a tool call
// see the same models, and the browser neither names a host nor receives a credential.

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/chetto1983/aura/internal/mediagen"
)

// MediaCatalogLister is the shared media catalog on the daemon's live route. The composition
// root supplies that route's OpenRouter base URL; a route that is not OpenRouter answers
// ErrMediaCatalogLocalRoute.
type MediaCatalogLister interface {
	List(ctx context.Context, kind mediagen.Kind, refresh bool) ([]mediagen.Model, error)
}

// ErrMediaCatalogLocalRoute refuses the media and voice catalogues on a llama.cpp, Ollama or
// other non-OpenRouter route: generation and cloud speech are served by OpenRouter only.
var ErrMediaCatalogLocalRoute = errors.New(
	"image, video and voice models are listed only on the OpenRouter route: choose Cloud in Model routing and save",
)

// SetMediaCatalog wires the shared media catalog. Until set, both routes answer 503.
func (s *Server) SetMediaCatalog(lister MediaCatalogLister) { s.mediaCatalog = lister }

// mediaCatalogModelDTO is one picker row. A capability or price the catalog did not declare
// is omitted rather than zeroed: a present zero price means free, an absent one unknown.
type mediaCatalogModelDTO struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	ReferenceMax *int     `json:"reference_max,omitempty"`
	ImageMinUSD  *float64 `json:"image_min_usd,omitempty"`
	ImageMaxUSD  *float64 `json:"image_max_usd,omitempty"`
	// ImageTokenMinPer1M and ImageTokenMaxPer1M are the output-token rate of a model billed per
	// token, which has no per-image price. They are not counted in HasPrice, which keeps
	// meaning "a per-unit price the row can print as $/image or $/s".
	ImageTokenMinPer1M *float64 `json:"image_token_min_per_1m,omitempty"`
	ImageTokenMaxPer1M *float64 `json:"image_token_max_per_1m,omitempty"`
	DurationMin        *int     `json:"duration_min,omitempty"`
	DurationMax        *int     `json:"duration_max,omitempty"`
	Resolutions        []string `json:"resolutions,omitempty"`
	ImageToVideo       *bool    `json:"image_to_video,omitempty"`
	SecondMinUSD       *float64 `json:"second_min_usd,omitempty"`
	SecondMaxUSD       *float64 `json:"second_max_usd,omitempty"`
	Voices             []string `json:"voices,omitempty"`
	HasPrice           bool     `json:"has_price"`
}

type mediaCatalogDTO struct {
	Models []mediaCatalogModelDTO `json:"models"`
}

func (s *Server) handleListImageModels(w http.ResponseWriter, r *http.Request) {
	s.listMediaModels(w, r, mediagen.KindImage)
}

func (s *Server) handleListVideoModels(w http.ResponseWriter, r *http.Request) {
	s.listMediaModels(w, r, mediagen.KindVideo)
}

func (s *Server) listMediaModels(w http.ResponseWriter, r *http.Request, kind mediagen.Kind) {
	if s.mediaCatalog == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "media model catalog not configured"})
		return
	}
	models, err := s.mediaCatalog.List(r.Context(), kind, r.URL.Query().Get("refresh") == "1")
	switch {
	case errors.Is(err, ErrMediaCatalogLocalRoute):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	case err != nil:
		// The catalog's reason names the failing read ("GET videos/models: 503"), which is what
		// tells the operator whether to wait or to look at the route.
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	out := mediaCatalogDTO{Models: make([]mediaCatalogModelDTO, 0, len(models))}
	for _, model := range models {
		row := mediaCatalogModelDTO{ID: model.ID, Kind: string(kind)}
		if kind == mediagen.KindImage {
			imageCapabilities(&row, model)
		} else {
			videoCapabilities(&row, model)
		}
		out.Models = append(out.Models, row)
	}
	writeJSON(w, out)
}

// imageCapabilities reads the same input_references descriptor ClampImage enforces, the
// per-image price lines mediagen.ImagePrice can express, and the output-token rate.
func imageCapabilities(row *mediaCatalogModelDTO, model mediagen.Model) {
	if references, declared := model.Parameters["input_references"]; declared && references.Max != nil {
		row.ReferenceMax = new(max(*references.Max, 0))
	}
	if low, high, ok := mediagen.ImagePrice(model.ImagePricing); ok {
		row.ImageMinUSD, row.ImageMaxUSD, row.HasPrice = &low, &high, true
	}
	if low, high, ok := mediagen.ImageTokenPricePerMillion(model.ImagePricing); ok {
		row.ImageTokenMinPer1M, row.ImageTokenMaxPer1M = &low, &high
	}
}

// videoCapabilities reports image-to-video as ClampVideo decides it (a model whose frame images
// do not list first_frame refuses a starting image), and only when frame images are declared.
func videoCapabilities(row *mediaCatalogModelDTO, model mediagen.Model) {
	if len(model.Durations) > 0 {
		row.DurationMin, row.DurationMax = new(slices.Min(model.Durations)), new(slices.Max(model.Durations))
	}
	row.Resolutions = model.Resolutions
	if len(model.FrameImages) > 0 {
		row.ImageToVideo = new(slices.Contains(model.FrameImages, "first_frame"))
	}
	if low, high, ok := mediagen.VideoPrice(model.PricingSKUs); ok {
		row.SecondMinUSD, row.SecondMaxUSD, row.HasPrice = &low, &high, true
	}
}
