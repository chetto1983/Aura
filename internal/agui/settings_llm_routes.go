package agui

// settings_llm_routes.go serves the per-provider route memory the cockpit reads when the
// operator switches provider (aura.llm_provider_routes, migration 0117).
//
// Before it existed the Cloud/Local/Ollama buttons wrote three constants compiled into the
// browser bundle, so a round trip away from a provider and back replaced a working
// endpoint with a guess. The daemon is the only side that knows what each provider was
// actually run with, so it is the side that remembers.

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/redact"
)

// llmRouteStore is the aura.llm_provider_routes seam (internal/settings.Store satisfies
// it). Separate from settingsStore because it is a separate table with a separate meaning:
// aura.settings is the ACTIVE route, this is the memory of past ones.
type llmRouteStore interface {
	ListRoutes(ctx context.Context) ([]sqlc.AuraLlmProviderRoutes, error)
	UpsertRoute(ctx context.Context, provider, baseURL, model, by string) (sqlc.AuraLlmProviderRoutes, error)
}

// SetLLMRouteStore wires the per-provider route memory. Until set, GET answers 503 and
// saves simply remember nothing — the cockpit then falls back to its own defaults.
func (s *Server) SetLLMRouteStore(store llmRouteStore) { s.llmRoutes = store }

type llmProviderRouteDTO struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

type llmProviderRoutesDTO struct {
	Routes []llmProviderRouteDTO `json:"routes"`
}

func (s *Server) handleListLLMRoutes(w http.ResponseWriter, r *http.Request) {
	if s.llmRoutes == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "settings not configured"})
		return
	}
	rows, err := s.llmRoutes.ListRoutes(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
		return
	}
	out := llmProviderRoutesDTO{Routes: make([]llmProviderRouteDTO, 0, len(rows))}
	for _, row := range rows {
		item := llmProviderRouteDTO{Provider: row.Provider, BaseURL: row.BaseUrl, Model: row.Model}
		if row.UpdatedAt.Valid {
			item.UpdatedAt = row.UpdatedAt.Time.UTC().Format(time.RFC3339)
		}
		if row.UpdatedBy.Valid {
			item.UpdatedBy = row.UpdatedBy.String
		}
		out.Routes = append(out.Routes, item)
	}
	writeJSON(w, out)
}

// rememberProviderRoute records the route a just-persisted profile resolves to. It is
// called AFTER the write the operator asked for has succeeded, so a failure here must not
// fail that write: the profile is already live and the memory is a convenience. It is
// logged rather than swallowed — an operator whose switches keep landing on defaults
// deserves a line saying why.
//
// overrides is the merged aura.settings view of the hot profile; a coordinate absent from
// it falls back to the process environment, which is what the daemon is actually serving
// for that key.
func (s *Server) rememberProviderRoute(ctx context.Context, overrides map[string]string, actor string) {
	if s.llmRoutes == nil {
		return
	}
	provider := routeCoordinate(overrides, "AURA_LLM_PROVIDER")
	baseURL := routeCoordinate(overrides, "AURA_LLM_BASE_URL")
	model := routeCoordinate(overrides, "AURA_LLM_MODEL")
	if provider == "" || baseURL == "" || model == "" {
		return
	}
	if _, err := s.llmRoutes.UpsertRoute(ctx, provider, baseURL, model, actor); err != nil {
		// redact.Line, not the raw value: the provider reaches here from a request body,
		// and a value carrying newlines would forge log records (CodeQL go/log-injection).
		// It is the same treatment the composition root gives provider/model.
		slog.Warn("agui: remember provider route", "provider", redact.Line(provider), "err", err)
	}
}

func routeCoordinate(overrides map[string]string, key string) string {
	if value, ok := overrides[key]; ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(os.Getenv(key))
}
