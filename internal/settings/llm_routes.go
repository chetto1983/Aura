package settings

// llm_routes.go is the per-provider route memory (aura.llm_provider_routes, migration
// 0117): the last base URL + model saved for each primary-LLM provider. It is deliberately
// NOT part of the env overlay — nothing here reaches config.Load, and OverlayEnv never
// reads it. aura.settings still owns the single active route; this table only answers
// "what did the operator last run Ollama with", so the cockpit restores a real route when
// it switches provider instead of a constant compiled into the browser bundle.

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrIncompleteRoute is returned when a route is missing one of its three coordinates. A
// half route is worse than none: restoring it would leave the operator on a provider with
// someone else's endpoint.
type incompleteRouteError struct{}

func (incompleteRouteError) Error() string {
	return "settings: a provider route needs a provider, a base URL and a model"
}

// ErrIncompleteRoute marks a route write refused for missing coordinates.
var ErrIncompleteRoute error = incompleteRouteError{}

// ListRoutes returns the remembered route of every provider, ordered by provider.
func (s *Store) ListRoutes(ctx context.Context) ([]sqlc.AuraLlmProviderRoutes, error) {
	return s.q.ListLLMProviderRoutes(ctx)
}

// UpsertRoute remembers the route a provider was last saved with. Values are trimmed
// because they arrive from a text box, and an empty coordinate is refused rather than
// stored: a row that cannot be restored is not a memory.
func (s *Store) UpsertRoute(
	ctx context.Context, provider, baseURL, model, by string,
) (sqlc.AuraLlmProviderRoutes, error) {
	provider, baseURL, model = strings.TrimSpace(provider), strings.TrimSpace(baseURL), strings.TrimSpace(model)
	if provider == "" || baseURL == "" || model == "" {
		return sqlc.AuraLlmProviderRoutes{}, ErrIncompleteRoute
	}
	var updatedBy pgtype.Text
	if by != "" {
		updatedBy = pgtype.Text{String: by, Valid: true}
	}
	var row sqlc.AuraLlmProviderRoutes
	err := s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		var err error
		row, err = q.UpsertLLMProviderRoute(ctx, sqlc.UpsertLLMProviderRouteParams{
			Provider: provider, BaseUrl: baseURL, Model: model, UpdatedBy: updatedBy,
		})
		return err
	})
	return row, err
}
