//go:build db_integration

package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

const localIdentityID = "00000000-0000-0000-0000-000000000001"

func TestStorePostgresContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	app := openIdempotencyPool(t, ctx, requiredIntegrationEnv(t, "AURA_DB_URL"))
	migrate := openIdempotencyPool(t, ctx, requiredIntegrationEnv(t, "AURA_DB_MIGRATE_URL"))
	assertDisposableMigratedDatabase(t, ctx, app, migrate)

	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	store := New(app, Config{Now: func() time.Time { return now }, LeaseDuration: 2 * time.Minute, RetryAfter: 5 * time.Second, ExpiryBatch: 2})

	t.Run("concurrent begin elects exactly one owner", func(t *testing.T) {
		req := integrationRequest(t, localIdentityID, "concurrent-"+uuid.NewString())
		const contenders = 48
		var acquired atomic.Int32
		var wg sync.WaitGroup
		errs := make(chan error, contenders)
		for range contenders {
			wg.Add(1)
			go func() {
				defer wg.Done()
				decision, err := store.Begin(ctx, req)
				if err != nil {
					errs <- err
					return
				}
				switch decision.Decision {
				case DecisionAcquired:
					acquired.Add(1)
				case DecisionInProgress:
					if decision.RetryAfter != 5*time.Second {
						errs <- fmt.Errorf("retry=%v, want 5s", decision.RetryAfter)
					}
				default:
					errs <- fmt.Errorf("unexpected decision %q", decision.Decision)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if got := acquired.Load(); got != 1 {
			t.Fatalf("acquired=%d, want exactly 1", got)
		}
	})

	t.Run("completed replay and conflict", func(t *testing.T) {
		req := integrationRequest(t, localIdentityID, "replay-"+uuid.NewString())
		mustAcquire(t, ctx, store, req)
		result := ReplayResult{Body: []byte(`{"status":"ok"}`), Preview: "ok", SidecarRef: "replays/ok", ExpiresAt: now.Add(time.Hour)}
		if err := store.Complete(ctx, CompleteRequest{Operation: req.Operation, Fingerprint: req.Fingerprint, Result: result}); err != nil {
			t.Fatal(err)
		}
		replay, err := store.Begin(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if replay.Decision != DecisionReplay || replay.Replay == nil || !sameJSON(replay.Replay.Body, result.Body) {
			t.Fatalf("replay=%+v", replay)
		}
		conflictReq := req
		conflictReq.Fingerprint[0] ^= 0xff
		conflict, err := store.Begin(ctx, conflictReq)
		if conflict.Decision != DecisionConflict || !errors.Is(err, ErrConflict) {
			t.Fatalf("conflict=%+v err=%v", conflict, err)
		}
	})

	t.Run("indeterminate is terminal and stale completion is rejected", func(t *testing.T) {
		req := integrationRequest(t, localIdentityID, "indeterminate-"+uuid.NewString())
		mustAcquire(t, ctx, store, req)
		if err := store.MarkIndeterminate(ctx, req.Operation, req.Fingerprint); err != nil {
			t.Fatal(err)
		}
		decision, err := store.Begin(ctx, req)
		if err != nil || decision.Decision != DecisionIndeterminate {
			t.Fatalf("Begin=%+v err=%v", decision, err)
		}
		result := ReplayResult{Body: []byte(`{"late":true}`), ExpiresAt: now.Add(time.Hour)}
		if err := store.Complete(ctx, CompleteRequest{Operation: req.Operation, Fingerprint: req.Fingerprint, Result: result}); !errors.Is(err, ErrStaleTransition) {
			t.Fatalf("late Complete error=%v", err)
		}
	})

	t.Run("operation key is isolated by identity", func(t *testing.T) {
		otherID := uuid.NewString()
		if _, err := migrate.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, otherID, "idempotency-"+otherID); err != nil {
			t.Fatal(err)
		}
		key := "shared-" + uuid.NewString()
		mustAcquire(t, ctx, store, integrationRequest(t, localIdentityID, key))
		mustAcquire(t, ctx, store, integrationRequest(t, otherID, key))
	})

	t.Run("expiry cutoff and bounded batch preserve metadata", func(t *testing.T) {
		cutoff := now.Add(30 * time.Minute)
		expiries := []time.Time{cutoff.Add(-time.Second), cutoff, cutoff.Add(time.Second)}
		requests := make([]BeginRequest, len(expiries))
		for i, expiry := range expiries {
			req := integrationRequest(t, localIdentityID, fmt.Sprintf("expiry-%d-%s", i, uuid.NewString()))
			audit := &AuditLink{ConversationID: uuid.NewString(), RequestID: uuid.NewString(), ToolCallID: fmt.Sprintf("call-%d", i)}
			req.Audit = audit
			mustAcquire(t, ctx, store, req)
			result := ReplayResult{Body: []byte(fmt.Sprintf(`{"row":%d}`, i)), Preview: "kept-metadata", ExpiresAt: expiry}
			if err := store.Complete(ctx, CompleteRequest{Operation: req.Operation, Fingerprint: req.Fingerprint, Result: result}); err != nil {
				t.Fatal(err)
			}
			requests[i] = req
		}

		first, err := store.ExpireReplayBodies(ctx, cutoff)
		if err != nil {
			t.Fatal(err)
		}
		if first.Cleared != 2 {
			t.Fatalf("first expiry=%+v, want cap-sized batch", first)
		}
		assertReplayMetadata(t, ctx, app, requests[0], true)
		assertReplayMetadata(t, ctx, app, requests[1], true)
		assertReplayMetadata(t, ctx, app, requests[2], false)

		second, err := store.ExpireReplayBodies(ctx, cutoff.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if second.Cleared != 1 {
			t.Fatalf("second expiry=%+v, want cap-1 remainder", second)
		}
		assertReplayMetadata(t, ctx, app, requests[2], true)
	})

	t.Run("wall clock expiry never emits retained response envelopes", func(t *testing.T) {
		for _, tc := range []struct {
			name       string
			expiresAt  time.Time
			want       Decision
			wantReplay bool
		}{
			{name: "before", expiresAt: now.Add(time.Second), want: DecisionReplay, wantReplay: true},
			{name: "equal", expiresAt: now, want: DecisionResultExpired},
			{name: "after", expiresAt: now.Add(-time.Second), want: DecisionResultExpired},
		} {
			t.Run(tc.name, func(t *testing.T) {
				req := integrationRequest(t, localIdentityID, "wall-clock-expiry-"+tc.name+"-"+uuid.NewString())
				mustAcquire(t, ctx, store, req)
				result := ReplayResult{
					Body: []byte(`{"surface":"http-or-tool"}`), StatusCode: 202,
					Headers: map[string]string{"Location": "/operations/retained"}, ExpiresAt: tc.expiresAt,
				}
				if err := store.Complete(ctx, CompleteRequest{Operation: req.Operation, Fingerprint: req.Fingerprint, Result: result}); err != nil {
					t.Fatal(err)
				}
				var bodyRetained, notCleared bool
				if err := app.QueryRow(ctx, `
					SELECT replay_body IS NOT NULL, replay_cleared_at IS NULL
					FROM aura.idempotency_operations
					WHERE identity_id=$1 AND operation_scope=$2 AND operation_key=$3`,
					req.Operation.IdentityID, req.Operation.Scope, req.Operation.Key,
				).Scan(&bodyRetained, &notCleared); err != nil {
					t.Fatal(err)
				}
				decision, err := store.Begin(ctx, req)
				if err != nil || decision.Decision != tc.want || (decision.Replay != nil) != tc.wantReplay {
					t.Fatalf("Begin=%+v err=%v, want %s replay=%t", decision, err, tc.want, tc.wantReplay)
				}
				if !bodyRetained || !notCleared {
					t.Fatalf("physical GC ran before decision: body-retained=%t not-cleared=%t", bodyRetained, notCleared)
				}
			})
		}
	})
}

func requiredIntegrationEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("db_integration requires %s under CI", key)
		}
		t.Skipf("db_integration requires %s", key)
	}
	return value
}

func openIdempotencyPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertDisposableMigratedDatabase(t *testing.T, ctx context.Context, app, migrate *pgxpool.Pool) {
	t.Helper()
	var appName, migrateName string
	if err := app.QueryRow(ctx, `SELECT current_database()`).Scan(&appName); err != nil {
		t.Fatal(err)
	}
	if err := migrate.QueryRow(ctx, `SELECT current_database()`).Scan(&migrateName); err != nil {
		t.Fatal(err)
	}
	if appName != migrateName {
		t.Fatalf("app database %q and migration database %q differ", appName, migrateName)
	}
	if appName == "aura" || appName == "postgres" {
		t.Fatalf("refusing db_integration database %q; use a disposable database", appName)
	}
	var version int
	if err := migrate.QueryRow(ctx, `SELECT version FROM public.schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("query migration version: %v", err)
	}
	if version < 43 {
		t.Fatalf("database %q is at migration %d; run `aura db migrate` against the disposable database", appName, version)
	}
}

func integrationRequest(t *testing.T, identityID, key string) BeginRequest {
	t.Helper()
	fingerprint, err := FingerprintTyped(struct {
		Key string `json:"key"`
	}{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	return BeginRequest{Operation: OperationKey{IdentityID: identityID, Scope: ScopeHTTPMutation, Key: key}, Fingerprint: fingerprint}
}

func mustAcquire(t *testing.T, ctx context.Context, store *Store, req BeginRequest) {
	t.Helper()
	decision, err := store.Begin(ctx, req)
	if err != nil || decision.Decision != DecisionAcquired {
		t.Fatalf("Begin=%+v err=%v", decision, err)
	}
}

func assertReplayMetadata(t *testing.T, ctx context.Context, pool *pgxpool.Pool, req BeginRequest, wantCleared bool) {
	t.Helper()
	var state, conversationID, requestID, toolCallID string
	var replayCleared bool
	err := pool.QueryRow(ctx, `
		SELECT state, replay_body IS NULL, audit_conversation_id::text, audit_request_id::text, audit_tool_call_id
		FROM aura.idempotency_operations
		WHERE identity_id=$1 AND operation_scope=$2 AND operation_key=$3`,
		req.Operation.IdentityID, req.Operation.Scope, req.Operation.Key,
	).Scan(&state, &replayCleared, &conversationID, &requestID, &toolCallID)
	if err != nil {
		t.Fatal(err)
	}
	if state != string(StateCompleted) || replayCleared != wantCleared || req.Audit == nil ||
		conversationID != req.Audit.ConversationID || requestID != req.Audit.RequestID || toolCallID != req.Audit.ToolCallID {
		t.Fatalf("state=%q cleared=%t audit=(%s,%s,%s), want completed/%t/%+v", state, replayCleared, conversationID, requestID, toolCallID, wantCleared, req.Audit)
	}
}

var _ sqlc.DBTX = (*pgxpool.Pool)(nil)

func sameJSON(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
