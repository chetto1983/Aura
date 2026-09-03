//go:build db_integration

// Integration tests for the per-provider route memory (aura.llm_provider_routes,
// migration 0117). Same tier and harness as store_db_test.go:
//
//	go test -tags db_integration -race ./internal/settings
package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Test-only provider names: the table is deployment-global, so a test must not overwrite
// the route the operator is actually running.
const (
	testCloudProvider = "test_openrouter"
	testLocalProvider = "test_llamacpp"
)

func cleanupRoutes(t *testing.T, pool *pgxpool.Pool, providers ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, provider := range providers {
			_, _ = pool.Exec(context.Background(),
				"DELETE FROM aura.llm_provider_routes WHERE provider = $1", provider)
		}
	})
}

func routeOf(t *testing.T, s *Store, provider string) (baseURL, model, by string, found bool) {
	t.Helper()
	rows, err := s.ListRoutes(context.Background())
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	for _, row := range rows {
		if row.Provider == provider {
			return row.BaseUrl, row.Model, row.UpdatedBy.String, true
		}
	}
	return "", "", "", false
}

func TestUpsertRouteRemembersAndReplacesPerProvider(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	s := NewStore(pool)
	cleanupRoutes(t, pool, testCloudProvider, testLocalProvider)

	if _, err := s.UpsertRoute(ctx, testLocalProvider, "  http://host.docker.internal:8084/v1  ", " gemma-4-12b ", "operator"); err != nil {
		t.Fatalf("UpsertRoute local: %v", err)
	}
	if _, err := s.UpsertRoute(ctx, testCloudProvider, "https://openrouter.ai/api/v1", "z-ai/glm-5.3", ""); err != nil {
		t.Fatalf("UpsertRoute cloud: %v", err)
	}

	baseURL, model, by, found := routeOf(t, s, testLocalProvider)
	if !found {
		t.Fatal("the local route was not remembered")
	}
	// Values arrive from a text box: a stored " gemma-4-12b " would be restored with its
	// spaces and then fail the route validation on save.
	if baseURL != "http://host.docker.internal:8084/v1" || model != "gemma-4-12b" || by != "operator" {
		t.Fatalf("local route = (%q, %q, %q), want the trimmed values and the actor", baseURL, model, by)
	}
	if _, _, _, ok := routeOf(t, s, testCloudProvider); !ok {
		t.Fatal("switching provider must not evict the other provider's memory")
	}

	// A second save of the same provider replaces its row rather than adding one.
	if _, err := s.UpsertRoute(ctx, testLocalProvider, "http://aura-llm:8084/v1", "qwen4:14b", "operator2"); err != nil {
		t.Fatalf("UpsertRoute replace: %v", err)
	}
	baseURL, model, by, _ = routeOf(t, s, testLocalProvider)
	if baseURL != "http://aura-llm:8084/v1" || model != "qwen4:14b" || by != "operator2" {
		t.Fatalf("replaced route = (%q, %q, %q)", baseURL, model, by)
	}

	rows, err := s.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	seen := 0
	for _, row := range rows {
		if row.Provider == testLocalProvider {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("provider rows = %d, want exactly one row per provider", seen)
	}
}

func TestUpsertRouteRefusesAnIncompleteRoute(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	s := NewStore(pool)
	cleanupRoutes(t, pool, testLocalProvider)

	for _, tc := range []struct {
		name                     string
		provider, baseURL, model string
	}{
		{"no provider", "", "http://aura-llm:8084/v1", "gemma-4-12b"},
		{"no base URL", testLocalProvider, "   ", "gemma-4-12b"},
		{"no model", testLocalProvider, "http://aura-llm:8084/v1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.UpsertRoute(ctx, tc.provider, tc.baseURL, tc.model, "operator"); !errors.Is(err, ErrIncompleteRoute) {
				t.Fatalf("err = %v, want ErrIncompleteRoute", err)
			}
		})
	}
	if _, _, _, found := routeOf(t, s, testLocalProvider); found {
		t.Fatal("a refused route was written anyway — a row that cannot be restored is not a memory")
	}
}
