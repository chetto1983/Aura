package agui

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// The embedding route changes only through a measured preview and a confirmed apply (spec §4).
// A route change moves every stored vector into another space: both families turn lexical until
// they are re-embedded, and a hosted model bills every token of it. The generic settings writes
// therefore refuse the three route keys, and the operator reaches them only here, after seeing
// what the change costs.

// embeddingRouteKeys decide the route; together they name the space every new vector is in.
var embeddingRouteKeys = []string{"AURA_EMBED_BASE_URL", "AURA_EMBED_MODEL", "AURA_EMBED_CLOUD_BASE_URL"}

// refuseEmbeddingRouteKey answers 409 for a route key on the generic settings writes, and
// reports whether it did.
func refuseEmbeddingRouteKey(w http.ResponseWriter, key string) bool {
	if !slices.Contains(embeddingRouteKeys, key) {
		return false
	}
	writeJSONStatus(w, http.StatusConflict, map[string]string{
		"error": key + " changes only through POST /api/settings/embedding-route, after its preview",
	})
	return true
}

// minEmbeddingInputLimit is the chunker's passage ceiling in tokens: a model that takes fewer
// would cut every full passage (spec §4).
const minEmbeddingInputLimit = 2048

// Refusal codes: the cockpit translates the code, and shows the detail as it came.
const (
	refusalNoRoute            = "no_route"
	refusalKeyMissing         = "key_missing"
	refusalProbeFailed        = "probe_failed"
	refusalWidthTooNarrow     = "width_too_narrow"
	refusalInputLimitTooSmall = "input_limit_too_small"
)

// EmbeddingRouteValues is a route as the three settings rows hold it. An empty value is an
// empty row, which is a choice: an empty model is the local sidecar, an empty cloud base is
// OpenRouter itself.
type EmbeddingRouteValues struct {
	BaseURL      string `json:"AURA_EMBED_BASE_URL"`
	Model        string `json:"AURA_EMBED_MODEL"`
	CloudBaseURL string `json:"AURA_EMBED_CLOUD_BASE_URL"`
}

func (v EmbeddingRouteValues) embedConfig() config.EmbedConfig {
	return config.EmbedConfig{
		BaseURL: strings.TrimSpace(v.BaseURL), CloudModel: strings.TrimSpace(v.Model),
		CloudBaseURL: strings.TrimSpace(v.CloudBaseURL),
	}
}

func (v EmbeddingRouteValues) rows() map[string]string {
	embed := v.embedConfig()
	return map[string]string{
		"AURA_EMBED_BASE_URL": embed.BaseURL, "AURA_EMBED_MODEL": embed.CloudModel,
		"AURA_EMBED_CLOUD_BASE_URL": embed.CloudBaseURL,
	}
}

// EmbeddingRoutes is what the three endpoints read; the composition root implements it over
// the daemon's embedders, the tenant walk and the route probe.
type EmbeddingRoutes interface {
	// Current names the spaces the daemon reads the memory and documents families in.
	Current(ctx context.Context) (memory, documents embeddings.Space, err error)
	Reports(ctx context.Context, memorySpace, documentSpace string) ([]arcadedb.TenantSpaceReport, error)
	// Probe measures embed at the documents width, and names the memory family's target space.
	Probe(ctx context.Context, embed config.EmbedConfig) (embeddings.RouteProbe, embeddings.Space, error)
	Work(ctx context.Context, memorySpace, documentSpace string, limitChars int) (arcadedb.CorpusWork, error)
	// Dimensions is AURA_EMBED_DIMENSIONS, the documents family's width.
	Dimensions() int
}

// SetEmbeddingRoutes wires the embedding route endpoints; until then they answer 503.
func (s *Server) SetEmbeddingRoutes(routes EmbeddingRoutes) { s.embeddingRoutes = routes }

type embeddingSpaceDTO struct {
	Space               string                       `json:"space"`
	SpaceLabel          string                       `json:"space_label"`
	DocumentsSpace      string                       `json:"documents_space"`
	DocumentsSpaceLabel string                       `json:"documents_space_label"`
	SpaceError          string                       `json:"space_error,omitempty"`
	FloorsCalibrated    bool                         `json:"floors_calibrated"`
	Tenants             []arcadedb.TenantSpaceReport `json:"tenants"`
}

type routeRefusal struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type embeddingRoutePreview struct {
	Space           string              `json:"space"`
	SpaceLabel      string              `json:"space_label"`
	MemorySpace     string              `json:"memory_space"`
	NativeWidth     int                 `json:"native_width"`
	Dimensions      int                 `json:"dimensions"`
	WidthWarning    bool                `json:"width_warning"`
	CharsPerSecond  float64             `json:"chars_per_second"`
	InputLimit      int                 `json:"input_limit"`
	Work            arcadedb.CorpusWork `json:"work"`
	Tokens          int                 `json:"tokens"`
	CostUSD         *float64            `json:"cost_usd"` // null: the catalogue publishes no price
	Local           bool                `json:"local"`
	DurationSeconds float64             `json:"duration_seconds"`
	// FloorsCalibrated false: dense relevance floors were measured for another model (spec §9).
	FloorsCalibrated bool           `json:"floors_calibrated"`
	Refusals         []routeRefusal `json:"refusals"`

	pricePer1M float64
}

// price turns the corpus work into tokens, a duration and a cost. Tokens are characters over
// the chunker's fallback ratio, an overshoot by design, so a cost is never under-announced.
func (p *embeddingRoutePreview) price(work arcadedb.CorpusWork) {
	p.Work = work
	chars := 0
	for _, typed := range work.Types {
		chars += typed.Chars
	}
	p.Tokens = (chars + embeddings.CharsPerTokenFallback - 1) / embeddings.CharsPerTokenFallback
	if p.CharsPerSecond > 0 {
		p.DurationSeconds = math.Ceil(float64(chars) / p.CharsPerSecond)
	}
	if p.CostUSD != nil && !p.Local {
		cost := float64(p.Tokens) * p.pricePer1M / 1e6
		p.CostUSD = &cost
	}
}

type embeddingRouteApplied struct {
	Space           string `json:"space"`
	Restarting      bool   `json:"restarting"`
	RestartRequired bool   `json:"restart_required"`
}

func (s *Server) registerEmbeddingRouteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/embedding-space", s.handleEmbeddingSpace)
	mux.HandleFunc("POST /api/settings/embedding-route/preview", s.handleEmbeddingRoutePreview)
	mux.HandleFunc("POST /api/settings/embedding-route", s.handleApplyEmbeddingRoute)
}

func (s *Server) handleEmbeddingSpace(w http.ResponseWriter, r *http.Request) {
	if s.embeddingRoutes == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "embedding routes not configured"})
		return
	}
	memory, documents, err := s.embeddingRoutes.Current(r.Context())
	if err != nil {
		// Counted against no space, every row would read as out of place: say why instead.
		writeJSON(w, embeddingSpaceDTO{SpaceError: err.Error(), Tenants: []arcadedb.TenantSpaceReport{}})
		return
	}
	reports, err := s.embeddingRoutes.Reports(r.Context(), memory.ID, documents.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "embedding space report unavailable"})
		return
	}
	writeJSON(w, embeddingSpaceDTO{
		Space: memory.ID, SpaceLabel: memory.Label, DocumentsSpace: documents.ID, DocumentsSpaceLabel: documents.Label,
		FloorsCalibrated: arcadedb.FloorsCalibrated(documents.ID), Tenants: reports,
	})
}

func (s *Server) handleEmbeddingRoutePreview(w http.ResponseWriter, r *http.Request) {
	var route EmbeddingRouteValues
	if _, ok := s.embeddingRouteRequest(w, r, &route); !ok {
		return
	}
	preview := s.probeEmbeddingRoute(r.Context(), route)
	// A route that answered has a space to measure the corpus against, refused or not: the
	// operator sees what a narrower or shorter model would have cost too.
	if preview.Space != "" {
		limit := 0
		if !preview.Local {
			limit = preview.InputLimit
		}
		work, err := s.embeddingRoutes.Work(r.Context(), preview.MemorySpace, preview.Space, limit)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "embedding corpus unavailable"})
			return
		}
		preview.price(work)
	}
	writeJSON(w, preview)
}

func (s *Server) handleApplyEmbeddingRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EmbeddingRouteValues
		ConfirmSpace string `json:"confirm_space"`
	}
	actor, ok := s.embeddingRouteRequest(w, r, &body)
	if !ok {
		return
	}
	if s.settings == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "settings not configured"})
		return
	}
	preview := s.probeEmbeddingRoute(r.Context(), body.EmbeddingRouteValues)
	if len(preview.Refusals) > 0 {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"error": "route_refused", "refusals": preview.Refusals})
		return
	}
	// The space is recomputed, never taken from the browser: a GGUF swapped since the preview,
	// or a model id edited after it, names a space the operator did not confirm.
	if preview.Space != strings.TrimSpace(body.ConfirmSpace) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "space_changed", "space": preview.Space})
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if _, err := s.settings.ReplaceMany(r.Context(), body.rows(), nil, actor); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	// Nothing else: the pass and ingest start because rows now carry another space (spec §4).
	applied := embeddingRouteApplied{Space: preview.Space, Restarting: s.restartTrigger != nil}
	applied.RestartRequired = !applied.Restarting
	writeJSON(w, applied)
	if s.restartTrigger != nil {
		s.restartTrigger()
	}
}

// embeddingRouteRequest answers for everything a route request must pass before it is read:
// the seam, the caller, the capability the route keys require, and a body that decodes.
func (s *Server) embeddingRouteRequest(w http.ResponseWriter, r *http.Request, body any) (string, bool) {
	if s.embeddingRoutes == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "embedding routes not configured"})
		return "", false
	}
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", false
	}
	if !s.authorizeSettingWrite(w, r, actor, false, embeddingRouteKeys...) {
		return "", false
	}
	raw, ok := readCappedBody(w, r)
	if !ok {
		return "", false
	}
	if err := json.Unmarshal(raw, body); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return "", false
	}
	return actor, true
}

// probeEmbeddingRoute measures route and applies the refusal rules. A probe that fails is a
// refusal the cockpit shows, never a server error: the route is what failed, not the request.
func (s *Server) probeEmbeddingRoute(ctx context.Context, route EmbeddingRouteValues) embeddingRoutePreview {
	embed := route.embedConfig()
	dims := s.embeddingRoutes.Dimensions()
	preview := embeddingRoutePreview{
		Dimensions: dims, Local: config.EmbedRouteKind(embed) == config.EmbedLocal, Refusals: []routeRefusal{},
	}
	probe, memory, err := s.embeddingRoutes.Probe(ctx, embed)
	switch {
	case errors.Is(err, embeddings.ErrNoRoute):
		preview.Refusals = append(preview.Refusals, routeRefusal{Code: refusalNoRoute})
		return preview
	case errors.Is(err, embeddings.ErrNoCredential):
		preview.Refusals = append(preview.Refusals, routeRefusal{Code: refusalKeyMissing})
		return preview
	case err != nil:
		preview.Refusals = append(preview.Refusals, routeRefusal{Code: refusalProbeFailed, Detail: err.Error()})
		return preview
	}
	preview.Space, preview.SpaceLabel, preview.MemorySpace = probe.Space.ID, probe.Space.Label, memory.ID
	preview.NativeWidth, preview.CharsPerSecond, preview.InputLimit = probe.NativeWidth, probe.CharsPerSecond, probe.InputLimit
	preview.FloorsCalibrated, preview.pricePer1M = arcadedb.FloorsCalibrated(probe.Space.ID), probe.PricePer1M
	if probe.HasPrice || preview.Local {
		cost := 0.0
		preview.CostUSD = &cost
	}
	stored := max(arcadedb.MemoryDimensions, dims)
	switch {
	case probe.NativeWidth < stored:
		preview.Refusals = append(preview.Refusals, routeRefusal{Code: refusalWidthTooNarrow})
	case probe.NativeWidth > stored:
		preview.WidthWarning = true
	}
	if probe.InputLimit < minEmbeddingInputLimit {
		preview.Refusals = append(preview.Refusals, routeRefusal{Code: refusalInputLimitTooSmall})
	}
	return preview
}
