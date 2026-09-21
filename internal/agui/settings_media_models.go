package agui

// settings_media_models.go answers the cockpit's image and video model boxes
// (GET /api/settings/image-models and /video-models) from the daemon's one media catalog, the
// cache the generation tools clamp against, in the shared shape of settings_catalog.go.
// Unlike llm-models there is no probe target to accept: the catalog is read on the route the
// daemon runs on, so a picker and a tool call see the same models, and the browser neither
// names a host nor receives a credential.

import (
	"context"
	"net/http"
	"slices"

	"github.com/chetto1983/aura/internal/mediagen"
)

// MediaCatalogLister is the shared media catalog on the daemon's live route. The composition
// root supplies that route's OpenRouter base URL; a route that is not OpenRouter answers
// ErrCatalogLocalRoute.
type MediaCatalogLister interface {
	List(ctx context.Context, kind mediagen.Kind, refresh bool) ([]mediagen.Model, error)
}

// SetMediaCatalog wires the shared media catalog. Until set, both routes answer 503.
func (s *Server) SetMediaCatalog(lister MediaCatalogLister) { s.mediaCatalog = lister }

func (s *Server) handleListImageModels(w http.ResponseWriter, r *http.Request) {
	s.listMediaModels(w, r, mediagen.KindImage)
}

func (s *Server) handleListVideoModels(w http.ResponseWriter, r *http.Request) {
	s.listMediaModels(w, r, mediagen.KindVideo)
}

func (s *Server) listMediaModels(w http.ResponseWriter, r *http.Request, kind mediagen.Kind) {
	refresh := r.URL.Query().Get("refresh") == "1"
	writeCatalogRows(w, s.mediaCatalog != nil, "media",
		func() ([]mediagen.Model, error) { return s.mediaCatalog.List(r.Context(), kind, refresh) },
		func(model mediagen.Model) (catalogModelDTO, bool) {
			row := catalogModelDTO{ID: model.ID, Kind: string(kind)}
			if kind == mediagen.KindImage {
				imageCapabilities(&row, model)
			} else {
				videoCapabilities(&row, model)
			}
			return row, true
		})
}

// imageCapabilities reads the same input_references descriptor ClampImage enforces, the
// per-image price lines mediagen.ImagePrice can express, and the output-token rate.
func imageCapabilities(row *catalogModelDTO, model mediagen.Model) {
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
func videoCapabilities(row *catalogModelDTO, model mediagen.Model) {
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
