//go:build db_integration

package idempotency

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMaintenanceRegistryPostgresContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrateURL := requiredIntegrationEnv(t, "AURA_DB_MIGRATE_URL")
	migrate := openIdempotencyPool(t, ctx, migrateURL)
	assertDisposableMaintenanceDatabase(t, ctx, migrate)

	bootstrap, err := OpenMaintenanceRegistry(ctx, migrateURL)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap.Close()
	if _, err := migrate.Exec(ctx, `TRUNCATE public.aura_maintenance_operations`); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC)

	t.Run("live owner blocks duplicates then replays a completed receipt", func(t *testing.T) {
		owner := openMaintenanceRegistry(t, ctx, migrateURL, now)
		duplicate := openMaintenanceRegistry(t, ctx, migrateURL, now)
		request := maintenanceRequest(t, "replay-"+uuid.NewString())

		assertMaintenanceDecision(t, ctx, owner, request, DecisionAcquired)
		blocked, err := duplicate.Begin(ctx, request)
		if err != nil || blocked.Decision != DecisionInProgress || blocked.RetryAfter <= 0 || blocked.RetryAfter > MaxRetryAfter {
			t.Fatalf("duplicate Begin=%+v err=%v", blocked, err)
		}

		conflictRequest := request
		conflictRequest.Fingerprint[0] ^= 0xff
		conflict, err := duplicate.Begin(ctx, conflictRequest)
		if conflict.Decision != DecisionConflict || !errors.Is(err, ErrConflict) {
			t.Fatalf("conflict Begin=%+v err=%v", conflict, err)
		}

		result := ReplayResult{Body: []byte(`{"status":"reset"}`), ExpiresAt: now.Add(time.Hour)}
		completion := CompleteRequest{Operation: request.Operation, Fingerprint: request.Fingerprint, Result: result}
		if err := owner.Complete(ctx, completion); err != nil {
			t.Fatal(err)
		}
		replay, err := duplicate.Begin(ctx, request)
		if err != nil || replay.Decision != DecisionReplay || replay.Replay == nil || !sameJSON(replay.Replay.Body, result.Body) {
			t.Fatalf("replay Begin=%+v err=%v", replay, err)
		}
		if err := owner.Complete(ctx, completion); !errors.Is(err, ErrStaleTransition) {
			t.Fatalf("second Complete error=%v, want stale transition", err)
		}
		if err := owner.MarkIndeterminate(ctx, request.Operation, request.Fingerprint); !errors.Is(err, ErrStaleTransition) {
			t.Fatalf("completed MarkIndeterminate error=%v, want stale transition", err)
		}

		if _, err := migrate.Exec(ctx, `
			UPDATE public.aura_maintenance_operations
			SET replay_expires_at = $1
			WHERE identity_id = $2 AND operation_scope = $3 AND operation_key = $4`,
			now, request.Operation.IdentityID, request.Operation.Scope, request.Operation.Key); err != nil {
			t.Fatal(err)
		}
		expired, err := duplicate.Begin(ctx, request)
		if err != nil || expired.Decision != DecisionResultExpired || expired.Replay != nil {
			t.Fatalf("expired Begin=%+v err=%v", expired, err)
		}
	})

	t.Run("explicit indeterminate state is terminal", func(t *testing.T) {
		owner := openMaintenanceRegistry(t, ctx, migrateURL, now)
		duplicate := openMaintenanceRegistry(t, ctx, migrateURL, now)
		request := maintenanceRequest(t, "indeterminate-"+uuid.NewString())
		assertMaintenanceDecision(t, ctx, owner, request, DecisionAcquired)
		if err := owner.MarkIndeterminate(ctx, request.Operation, request.Fingerprint); err != nil {
			t.Fatal(err)
		}
		assertMaintenanceDecision(t, ctx, duplicate, request, DecisionIndeterminate)
	})

	t.Run("expired owner-free lease is sealed without re-execution", func(t *testing.T) {
		owner := openMaintenanceRegistry(t, ctx, migrateURL, now)
		request := maintenanceRequest(t, "recovery-"+uuid.NewString())
		assertMaintenanceDecision(t, ctx, owner, request, DecisionAcquired)
		owner.Close()
		if _, err := migrate.Exec(ctx, `
			UPDATE public.aura_maintenance_operations
			SET lease_expires_at = $1
			WHERE identity_id = $2 AND operation_scope = $3 AND operation_key = $4`,
			now.Add(-time.Second), request.Operation.IdentityID, request.Operation.Scope, request.Operation.Key); err != nil {
			t.Fatal(err)
		}

		recovery := openMaintenanceRegistry(t, ctx, migrateURL, now)
		assertMaintenanceDecision(t, ctx, recovery, request, DecisionIndeterminate)
		observer := openMaintenanceRegistry(t, ctx, migrateURL, now)
		assertMaintenanceDecision(t, ctx, observer, request, DecisionIndeterminate)
	})

	t.Run("retry guidance is bounded", func(t *testing.T) {
		request := maintenanceRequest(t, "retry-"+uuid.NewString())
		if _, err := migrate.Exec(ctx, `
			INSERT INTO public.aura_maintenance_operations (
				identity_id, operation_scope, operation_key, payload_hash, state,
				retry_after, lease_expires_at, created_at, updated_at
			) VALUES ($1, $2, $3, $4, 'in_progress', $5, $6, $7, $7)`,
			request.Operation.IdentityID, request.Operation.Scope, request.Operation.Key, request.Fingerprint[:],
			now.Add(2*time.Hour), now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		registry := openMaintenanceRegistry(t, ctx, migrateURL, now)
		decision, err := registry.Begin(ctx, request)
		if err != nil || decision.Decision != DecisionInProgress || decision.RetryAfter != MaxRetryAfter {
			t.Fatalf("Begin=%+v err=%v, want bounded retry %v", decision, err, MaxRetryAfter)
		}
	})

	t.Run("validation rejects non CLI operations before persistence", func(t *testing.T) {
		registry := openMaintenanceRegistry(t, ctx, migrateURL, now)
		request := maintenanceRequest(t, "wrong-scope-"+uuid.NewString())
		request.Operation.Scope = ScopeHTTPMutation
		if _, err := registry.Begin(ctx, request); err == nil || err.Error() != "maintenance registry only accepts CLI operations" {
			t.Fatalf("Begin error=%v", err)
		}
		completion := CompleteRequest{
			Operation: request.Operation, Fingerprint: request.Fingerprint,
			Result: ReplayResult{Body: []byte(`{"ok":true}`), ExpiresAt: now.Add(time.Hour)},
		}
		if err := registry.Complete(ctx, completion); err == nil || err.Error() != "maintenance registry only accepts CLI operations" {
			t.Fatalf("Complete error=%v", err)
		}
		if err := registry.MarkIndeterminate(ctx, request.Operation, request.Fingerprint); err == nil || err.Error() != "maintenance registry only accepts CLI operations" {
			t.Fatalf("MarkIndeterminate error=%v", err)
		}
		if _, err := registry.Begin(ctx, BeginRequest{}); err == nil {
			t.Fatal("Begin with empty request succeeded")
		}
		if err := registry.Complete(ctx, CompleteRequest{}); err == nil {
			t.Fatal("Complete with empty request succeeded")
		}
		if err := registry.MarkIndeterminate(ctx, OperationKey{}, [32]byte{}); err == nil {
			t.Fatal("MarkIndeterminate with empty request succeeded")
		}
	})

	if _, err := OpenMaintenanceRegistry(ctx, ""); err == nil || err.Error() != "maintenance registry URL is empty" {
		t.Fatalf("OpenMaintenanceRegistry empty URL error=%v", err)
	}
	var nilRegistry *MaintenanceRegistry
	nilRegistry.Close()
}

func openMaintenanceRegistry(t *testing.T, ctx context.Context, migrateURL string, now time.Time) *MaintenanceRegistry {
	t.Helper()
	registry, err := OpenMaintenanceRegistry(ctx, migrateURL)
	if err != nil {
		t.Fatal(err)
	}
	registry.now = func() time.Time { return now }
	t.Cleanup(registry.Close)
	return registry
}

func maintenanceRequest(t *testing.T, key string) BeginRequest {
	t.Helper()
	fingerprint, err := FingerprintTyped(struct {
		Key string `json:"key"`
	}{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	return BeginRequest{
		Operation:   OperationKey{IdentityID: localIdentityID, Scope: ScopeCLICommand, Key: key},
		Fingerprint: fingerprint,
	}
}

func assertMaintenanceDecision(t *testing.T, ctx context.Context, registry *MaintenanceRegistry, request BeginRequest, want Decision) {
	t.Helper()
	decision, err := registry.Begin(ctx, request)
	if err != nil || decision.Decision != want {
		t.Fatalf("Begin=%+v err=%v, want %s", decision, err, want)
	}
}

func assertDisposableMaintenanceDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var name string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if protectedLocalIntegrationDatabase(name) {
		t.Fatalf("refusing maintenance integration test database %q; use a disposable database", name)
	}
}
