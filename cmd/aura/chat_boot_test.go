package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chat_boot_test.go pins QUAL-04b: no boot error path leaks a pool or an MCP
// subprocess. The real leak was the CommandHookManagerFromEnv failure path, which
// returned after the pool + registry were built with neither pool.Close nor an
// mcpClosers drain; the fix is a deferred close-on-error guard in assembleChatEnv.
// db.Open eagerly Pings, so the concrete boot cannot be driven past pool-open
// without a live Postgres — these tests use a NON-pinging pgxpool (pgxpool.New never
// connects with MinConns=0) plus an injectable pool opener, keeping the whole suite
// a fast, PG-free unit tier (verify: go test -race ./cmd/aura/ -run Boot).

// orderRecorder is a mutex-guarded append-only log: releaseBootResources'
// closeMCPServers now fans its closers out CONCURRENTLY (D-11, 38-05), so any
// recorder shared across closer functions needs its own synchronization — a bare
// `*[]string` mutated by two goroutines racing on append would be a data race
// (and occasionally a corrupted/short slice), not merely a flaky assertion.
type orderRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecorder) add(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, name)
}

func (r *orderRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// poolCloseSpy is a minimal interface{ Close() } stand-in so releaseBootResources'
// close-on-error contract (drain closers, then close the pool) is asserted without a
// live pool. It records its close into the shared orderRecorder.
type poolCloseSpy struct{ order *orderRecorder }

func (p poolCloseSpy) Close() { p.order.add("pool") }

// validBootConfig is the minimal dev-profile config that PASSES cfg.Validate: a
// non-empty DB DSN (the all-tier required secret) and a loopback web bind. RunDir is
// left empty so ScanOrphans is a no-op (no pool query).
func validBootConfig() *config.Config {
	return &config.Config{
		Profile:  config.ProfileDev,
		DB:       db.Config{URL: "postgres://u:p@127.0.0.1:1/aura"},
		AGUIBind: "127.0.0.1:9080",
		LLM:      llm.Config{Provider: "openrouter", Model: "test/model"},
	}
}

// unreachablePool builds a real pgxpool that never connects (MinConns=0, an
// unreachable DSN). It spawns pgxpool's background goroutine, so the code path under
// test MUST close it — which is exactly what these error-path tests assert.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/aura")
	if err != nil {
		t.Fatalf("build non-pinging pool: %v", err)
	}
	return pool
}

// assertPoolClosed pings the pool: a CLOSED pgxpool returns puddle's "closed pool"
// error, distinct from the connection error an open-but-unreachable pool returns.
func assertPoolClosed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("pool must be closed on the boot error path; Ping err = %v", err)
	}
}

// TestBootReleaseResourcesDrainsClosersAndClosesPool proves the close-on-error
// primitive: every MCP closer drains AND the pool closes, closers BEFORE the pool
// (the reasoning learner writes through a closer-held graph client), and it is
// nil-safe. This is the drain assertion the full-boot paths reuse via the defer.
func TestBootReleaseResourcesDrainsClosersAndClosesPool(t *testing.T) {
	rec := &orderRecorder{}
	closers := []func() error{
		func() error { rec.add("closer-a"); return nil },
		func() error { rec.add("closer-b"); return nil },
	}
	releaseBootResources(poolCloseSpy{order: rec}, closers)

	order := rec.snapshot()
	if len(order) != 3 {
		t.Fatalf("release fired %d actions, want 3 (2 closers + pool): %v", len(order), order)
	}
	if order[len(order)-1] != "pool" {
		t.Fatalf("pool must close AFTER the closers drain (concurrently, then joined), got %v", order)
	}
	sawA, sawB := false, false
	for _, o := range order {
		sawA = sawA || o == "closer-a"
		sawB = sawB || o == "closer-b"
	}
	if !sawA || !sawB {
		t.Fatalf("both MCP closers must drain, got %v", order)
	}
	// Nil-safe: an error before any pool/closers exist must not panic.
	releaseBootResources(nil, nil)
}

func TestBootChatCompositionRejectsIncompatibleMigrationBeforeAssembly(t *testing.T) {
	tests := []struct {
		name     string
		checkErr error
	}{
		{
			name: "dirty tracker",
			checkErr: errors.New(
				"migration tracker incompatible: version=62 dirty=true want=62",
			),
		},
		{
			name: "stale head",
			checkErr: errors.New(
				"migration tracker incompatible: version=61 dirty=false want=62",
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := unreachablePool(t)
			cfg := validBootConfig()
			cfg.DB.MigrateURL = "postgres://migrate:secret@127.0.0.1:1/aura"
			assembled := false
			check := func(_ context.Context, migrateURL string) error {
				if migrateURL != cfg.DB.MigrateURL {
					t.Errorf("migration checker URL = %q, want configured migrate URL", migrateURL)
				}
				return tt.checkErr
			}
			assemble := func(
				context.Context,
				*config.Config,
				*pgxpool.Pool,
			) (*chatEnv, error) {
				assembled = true
				return &chatEnv{}, nil
			}

			env, err := assembleChatEnvAtMigrationHead(
				context.Background(), cfg, pool, check, rlsEnforced, assemble,
			)
			if env != nil || !errors.Is(err, tt.checkErr) {
				t.Fatalf("incompatible migration result = env %v err %v", env, err)
			}
			if !strings.Contains(err.Error(), "postgres migration compatibility") {
				t.Fatalf("migration error lacks composition context: %v", err)
			}
			if assembled {
				t.Fatal("chat environment assembled before migration compatibility passed")
			}
			assertPoolClosed(t, pool)
		})
	}
}

// rlsEnforced is the injected stand-in for db.VerifyRLSEnforced on the boot paths that
// are not exercising the RLS gate itself: the real check needs a live Postgres.
func rlsEnforced(context.Context, *pgxpool.Pool) error { return nil }

// TestBootChatCompositionRefusesRLSBypassRole pins the boot-time refusal that migration
// 0087's policies cannot enforce themselves: connected as a superuser or a BYPASSRLS
// role, Postgres skips every policy and the daemon would serve with no tenant isolation
// and no symptom. Boot must stop, and must release the pool it already opened.
func TestBootChatCompositionRefusesRLSBypassRole(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	cfg.DB.MigrateURL = "postgres://migrate:secret@127.0.0.1:1/aura"
	assembled := false
	bypass := fmt.Errorf("%w: connected as \"aura\"", db.ErrRLSBypass)

	env, err := assembleChatEnvAtMigrationHead(
		context.Background(), cfg, pool,
		func(context.Context, string) error { return nil },
		func(_ context.Context, gotPool *pgxpool.Pool) error {
			if gotPool != pool {
				t.Errorf("rls checker pool = %p, want the opened pool %p", gotPool, pool)
			}
			return bypass
		},
		func(context.Context, *config.Config, *pgxpool.Pool) (*chatEnv, error) {
			assembled = true
			return &chatEnv{}, nil
		},
	)
	if env != nil || !errors.Is(err, db.ErrRLSBypass) {
		t.Fatalf("rls bypass boot result = env %v err %v, want nil env and ErrRLSBypass", env, err)
	}
	if assembled {
		t.Fatal("chat environment assembled despite a role that bypasses row-level security")
	}
	assertPoolClosed(t, pool)
}

func TestBootChatCompositionAcceptsCompatibleMigration(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	cfg.DB.MigrateURL = "postgres://migrate:secret@127.0.0.1:1/aura"
	checked := false
	assembled := false
	check := func(_ context.Context, migrateURL string) error {
		checked = true
		if migrateURL != cfg.DB.MigrateURL {
			t.Errorf("migration checker URL = %q, want configured migrate URL", migrateURL)
		}
		return nil
	}
	assemble := func(
		_ context.Context,
		gotCfg *config.Config,
		gotPool *pgxpool.Pool,
	) (*chatEnv, error) {
		assembled = true
		if gotCfg != cfg || gotPool != pool {
			t.Fatalf("assembler inputs = cfg %p pool %p, want cfg %p pool %p", gotCfg, gotPool, cfg, pool)
		}
		return &chatEnv{pool: gotPool}, nil
	}

	env, err := assembleChatEnvAtMigrationHead(
		context.Background(), cfg, pool, check, rlsEnforced, assemble,
	)
	if err != nil {
		t.Fatalf("compatible migration composition: %v", err)
	}
	if env == nil || !checked || !assembled {
		t.Fatalf("compatible composition = env %v checked %t assembled %t", env, checked, assembled)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil && strings.Contains(err.Error(), "closed") {
		t.Fatalf("compatible migration gate closed the pool before assembly ownership: %v", err)
	}
	env.close()
	assertPoolClosed(t, pool)
}

// TestBootCloseOnFinalValidateFailure covers the post-overlay (reloaded) Validate
// failure: assembleChatEnv's first act is to re-Validate the reloaded config, and a
// failure there must release the already-open pool.
func TestBootCloseOnFinalValidateFailure(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	cfg.DB.URL = "" // the reloaded config no longer validates (missing DB secret)

	env, err := assembleChatEnv(context.Background(), cfg, pool)
	if err == nil || env != nil {
		t.Fatalf("assembleChatEnv must fail on an invalid reloaded config, got env=%v err=%v", env, err)
	}
	if !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatalf("want a named config validation error, got %v", err)
	}
	assertPoolClosed(t, pool)
}

// TestBootCloseOnCommandHookFailure is THE real-leak regression (QUAL-04b): the
// command-hook config is invalid, so CommandHookManagerFromEnv fails AFTER the pool
// and registry are built. The deferred guard must have closed the pool (previously it
// leaked). mcpClosers is empty here (no MCP servers); the drain itself is asserted by
// TestBootReleaseResourcesDrainsClosersAndClosesPool.
func TestBootCloseOnCommandHookFailure(t *testing.T) {
	withMemoryMCPRegistry(t)
	t.Setenv("AURA_AGENT_COMMAND_HOOKS", "{not-json")
	pool := unreachablePool(t)
	cfg := validBootConfig() // RunDir "" => ScanOrphans is a no-op, no pool query

	env, err := assembleChatEnv(context.Background(), cfg, pool)
	if err == nil || env != nil {
		t.Fatalf("assembleChatEnv must fail on a bad command-hook config, got env=%v err=%v", env, err)
	}
	if !strings.Contains(err.Error(), "command hooks") {
		t.Fatalf("want the command-hooks failure, got %v", err)
	}
	assertPoolClosed(t, pool)
}

// TestBootCloseOnReloadFailure covers resolveConfigAndPool's post-overlay reload
// failure: the pool is opened, the settings overlay applied, then the reload
// loadConfig fails — the freshly-opened pool must be closed before returning.
func TestBootCloseOnReloadFailure(t *testing.T) {
	pool := unreachablePool(t)
	sentinel := errors.New("reload boom")
	calls := 0
	loadConfig := func() (*config.Config, error) {
		calls++
		if calls == 1 {
			return validBootConfig(), nil // passes the pre-open Validate
		}
		return nil, sentinel // post-overlay reload fails
	}
	opened := false
	opener := func(_ context.Context, _ *db.Config) (*pgxpool.Pool, error) {
		opened = true
		return pool, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg, gotPool, err := resolveConfigAndPoolWithSettings(
		ctx,
		loadConfig,
		opener,
		bootSettingsOps{
			overlay: func(context.Context, *pgxpool.Pool) error { return nil },
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("want the reload sentinel error, got %v", err)
	}
	if cfg != nil || gotPool != nil {
		t.Fatalf("a reload failure must return no cfg/pool, got cfg=%v pool=%v", cfg, gotPool)
	}
	if !opened {
		t.Fatal("opener must have been called (Validate passed, db.Open reached)")
	}
	assertPoolClosed(t, pool)
}

// TestBootFailFastBeforeDBOpen locks the double-Validate contract's load-bearing
// half (QUAL-04b prohibition): a config that fails the pre-open Validate must error
// BEFORE db.Open is ever reached — no pool is opened on a misconfigured deploy.
func TestBootFailFastBeforeDBOpen(t *testing.T) {
	cfg := validBootConfig()
	cfg.DB.URL = "" // invalid: fails Validate before any db.Open
	opened := false
	opener := func(_ context.Context, _ *db.Config) (*pgxpool.Pool, error) {
		opened = true
		return nil, nil
	}
	loadConfig := func() (*config.Config, error) { return cfg, nil }

	gotCfg, pool, err := resolveConfigAndPool(context.Background(), loadConfig, opener)
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatalf("want a pre-open validation error, got %v", err)
	}
	if opened {
		t.Fatal("db.Open must NOT be reached when the config fails the pre-open Validate (fail-fast preserved)")
	}
	if gotCfg != nil || pool != nil {
		t.Fatalf("no cfg/pool must be returned on the fail-fast path, got cfg=%v pool=%v", gotCfg, pool)
	}
}

func TestResolveConfigAndPoolOverlaysSettingsBeforeTheReload(t *testing.T) {
	tests := []struct {
		name      string
		firstErr  error
		openEvent string
	}{
		{name: "normal load", openEvent: "open"},
		{name: "keyless first load", firstErr: llm.ErrMissingAPIKey, openEvent: "open-keyless"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := unreachablePool(t)
			cfg := validBootConfig()
			var order []string
			loads := 0
			loadConfig := func() (*config.Config, error) {
				loads++
				order = append(order, fmt.Sprintf("load-%d", loads))
				if loads == 1 && tt.firstErr != nil {
					return nil, tt.firstErr
				}
				return cfg, nil
			}
			open := func(context.Context, *db.Config) (*pgxpool.Pool, error) {
				order = append(order, "open")
				if tt.openEvent != "open" {
					t.Fatal("normal opener called on keyless path")
				}
				return pool, nil
			}
			ops := bootSettingsOps{
				openKeyless: func(context.Context) (*pgxpool.Pool, bool, error) {
					order = append(order, "open-keyless")
					if tt.openEvent != "open-keyless" {
						t.Fatal("keyless opener called on normal path")
					}
					return pool, true, nil
				},
				overlay: func(context.Context, *pgxpool.Pool) error {
					order = append(order, "overlay")
					return nil
				},
			}

			gotCfg, gotPool, err := resolveConfigAndPoolWithSettings(
				context.Background(), loadConfig, open, ops,
			)
			if err != nil {
				t.Fatalf("resolveConfigAndPoolWithSettings: %v", err)
			}
			if gotCfg != cfg || gotPool != pool {
				t.Fatalf("resolved cfg/pool = %p/%p, want %p/%p", gotCfg, gotPool, cfg, pool)
			}
			wantOrder := []string{"load-1", tt.openEvent, "overlay", "load-2"}
			if !reflect.DeepEqual(order, wantOrder) {
				t.Fatalf("boot order = %v, want %v", order, wantOrder)
			}
			pool.Close()
		})
	}
}

// TestNewSteerInboxWiresConfigCaps closes the D-11-shaped drift 52-04 exists
// to fix: internal/steer's own package-level fallbacks (defaultMax=32,
// defaultMaxBytes=32768) disagree with the ratified amendment #132 item 10
// values (Max=8, MaxBytes=16384, internal/config/config_agui_steer.go). It
// behaviorally proves the WIRED MaxBytes equals the config value, never
// internal/steer's own fallback.
//
// Phase 51 plan 02 (D-06) moved newSteerInbox's backing store to Postgres, so
// this test can no longer prove the FULL round trip (a successful Push
// touches the database) without a live connection this package's fast unit
// tier does not have. MaxBytes is provably testable DB-less because
// steer.PostgresStore.Push validates it in pure Go BEFORE ever touching the
// pool (the SAME order the deleted in-memory Inbox used: empty, then
// oversize, then the DB-backed capacity check) — the Max (queue-depth) cap,
// by contrast, is enforced INSIDE the guarded INSERT and is covered instead
// by internal/steer/pg_store_test.go's db_integration tier
// (TestPostgresSteerQueueRespectsMaxCap), which is where a full push/drain
// round trip belongs now.
func TestNewSteerInboxWiresConfigCaps(t *testing.T) {
	cfg := &config.Config{AGUISteer: config.AGUISteerConfig{Enabled: true, Max: 8, MaxBytes: 16384}}
	inbox := newSteerInbox(nil, cfg)

	// A message one byte over 16384 must be refused regardless of the pool
	// (the size check runs before any DB call). Were the wired cap silently
	// 32768, this push would fall through to the (nil-pool) DB-touch error
	// instead of ErrTooLarge.
	over := strings.Repeat("a", 16385)
	if err := inbox.Push("conv-bytes", "test", over); !errors.Is(err, steer.ErrTooLarge) {
		t.Fatalf("oversize push = %v, want ErrTooLarge (wired MaxBytes must be 16384, not internal/steer's own default of 32768)", err)
	}

	// A message exactly AT the byte cap must clear the size check (a nil pool
	// then refuses it for an UNRELATED reason — no configured store — proving
	// the size gate itself did not fire).
	atCap := strings.Repeat("a", 16384)
	if err := inbox.Push("conv-bytes-2", "test", atCap); errors.Is(err, steer.ErrTooLarge) {
		t.Fatalf("push at exactly MaxBytes wrongly refused as oversize: %v", err)
	}
}

// TestNewSteerInboxDisabledLeavesFieldNil pins assembleChatEnv's gating: when
// AGUISteer.Enabled is false the composition root never calls newSteerInbox
// at all, leaving chatEnv.steer and runner.Deps.Steer both genuinely nil —
// the "unwired surface" contract the steer route and the agent's drain both
// rely on, mirroring AGUIRun.Detach's own gating of RunRegistry.
func TestNewSteerInboxDisabledLeavesFieldNil(t *testing.T) {
	cfg := &config.Config{}
	cfg.AGUISteer = config.AGUISteerConfig{Enabled: false, Max: 8, MaxBytes: 16384}
	var steerInbox *steer.PostgresStore
	if cfg.AGUISteer.Enabled {
		steerInbox = newSteerInbox(nil, cfg)
	}
	if steerInbox != nil {
		t.Fatalf("steerInbox = %#v, want nil when AGUISteer.Enabled is false", steerInbox)
	}
}

type emptyProjectionSource struct{}

func (emptyProjectionSource) ListProjectionTurns(
	context.Context,
	string,
	conversations.ProjectionCursor,
	int,
) ([]conversations.ProjectionTurn, conversations.ProjectionCursor, error) {
	return nil, conversations.ProjectionCursor{}, nil
}

type emptyProjectionSink struct{}

func (emptyProjectionSink) ApplyConversationProjection(context.Context, arcadedb.ConversationProjection) error {
	return nil
}
func (emptyProjectionSink) DeleteConversationProjection(context.Context, string, string) error {
	return nil
}
func (emptyProjectionSink) DeleteIdentityConversationProjections(context.Context, string) error {
	return nil
}
func (emptyProjectionSink) PruneConversationProjections(context.Context, string, []string) error {
	return nil
}

func TestChatBootMemoryProjection(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", strings.Repeat("s", 32))
	cfg := validBootConfig()
	cfg.ArcadeDB.BaseURL = "http://127.0.0.1:2480"
	projector := newChatConversationProjector(cfg, emptyProjectionSource{})
	if projector == nil {
		t.Fatal("configured chat boot did not construct the conversation projector")
	}

	owned := runner.NewConversationProjector(emptyProjectionSource{}, emptyProjectionSink{}, 1)
	env := &chatEnv{conversationProjector: owned}
	env.close()
	if owned.OfferConversation("00000000-0000-0000-0000-000000000001") {
		t.Fatal("chat shutdown left the boot-owned projector accepting work")
	}
	_ = projector.Close(context.Background())
}

type countingProjectionRoster struct{ calls int }

func (r *countingProjectionRoster) IdentityIDs(context.Context) ([]string, error) {
	r.calls++
	return []string{"00000000-0000-0000-0000-000000000001"}, nil
}

func TestConversationProjectionBootReconcileWiring(t *testing.T) {
	projector := runner.NewConversationProjector(emptyProjectionSource{}, emptyProjectionSink{}, 1)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })
	reconciler := runner.NewDeleteReconciler(nil, time.Hour)
	roster := &countingProjectionRoster{}
	wireChatConversationReconciliation(reconciler, projector, roster)
	if err := reconciler.ReconcileConversationProjection(context.Background()); err != nil {
		t.Fatalf("reconcile projection: %v", err)
	}
	if roster.calls != 1 {
		t.Fatalf("boot reconciliation roster calls = %d, want 1", roster.calls)
	}
}

func TestReasoningRetentionBoot(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", strings.Repeat("s", 32))
	cfg := validBootConfig()
	cfg.ArcadeDB.BaseURL = "http://127.0.0.1:2480"
	cfg.Retention.ReasoningSuccessTTL = 21 * 24 * time.Hour
	cfg.Retention.ReasoningFailedTTL = 5 * 24 * time.Hour
	runtime := newChatReasoningMemory(cfg)
	if runtime == nil || runtime.sink == nil || runtime.retention == nil || runtime.deletion == nil {
		t.Fatal("configured chat boot did not construct one shared reasoning sink/lifecycle")
	}
	if any(runtime.sink) != any(runtime.retention) || any(runtime.sink) != any(runtime.deletion) {
		t.Fatal("chat boot constructed separate reasoning sink and lifecycle owners")
	}
	store, ok := runtime.sink.(*tenantReasoningMemory)
	if !ok || store.policy.SuccessTTL != 21*24*time.Hour || store.policy.FailedTTL != 5*24*time.Hour {
		t.Fatalf("reasoning sink policy = %#v, want validated 21d/5d overrides", runtime.sink)
	}
}
