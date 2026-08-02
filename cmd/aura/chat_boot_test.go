package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
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
// non-empty DB DSN + Neo4j password (the two all-tier required secrets) and a
// loopback web bind. RunDir is left empty so ScanOrphans is a no-op (no pool query).
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
				context.Background(), cfg, pool, check, assemble,
			)
			if env != nil || !errors.Is(err, tt.checkErr) {
				t.Fatalf("incompatible migration result = env %v err %v", env, err)
			}
			if !strings.Contains(err.Error(), "postgres migration compatibility") {
				t.Fatalf("migration error lacks composition context: %v", err)
			}
			if assembled {
				t.Fatal("adaptive chat environment assembled before migration compatibility passed")
			}
			assertPoolClosed(t, pool)
		})
	}
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
		context.Background(), cfg, pool, check, assemble,
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

func TestAssembleChatEnvWiresProductionDynamicRecallControl(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	cfg.LLM.Provider = "openrouter"
	cfg.MemoryRecall = true
	cfg.MemoryRecallMaxItems = 8

	var projectorOrder []string
	env, err := assembleChatEnvWithAdaptivePolicy(
		context.Background(),
		cfg,
		pool,
		func(context.Context, *config.ArcadeDBConfig) (adaptive.GraphWriter, error) {
			return &adaptiveProjectorWriterSpy{order: &projectorOrder}, nil
		},
		adaptiveBootPolicyReaderFunc(func(context.Context) (adaptive.Policy, error) {
			return validAdaptiveBootTestPolicy(adaptive.PolicyShadow), nil
		}),
		io.Discard,
	)
	if err != nil {
		t.Fatalf("assembleChatEnv: %v", err)
	}
	t.Cleanup(env.close)
	if env.adaptiveProjector == nil {
		t.Fatal("production composition has no adaptive projector")
	}

	control := reflect.ValueOf(env.run).
		Elem().
		FieldByName("dynamicRecallControl")
	if !control.IsValid() || control.IsNil() {
		t.Fatal("production runner has no dynamic recall control")
	}
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
			recover: func(context.Context, *pgxpool.Pool) error { return nil },
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

func TestResolveConfigAndPoolRecoversOverrideBeforeEveryOverlay(t *testing.T) {
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
				recover: func(context.Context, *pgxpool.Pool) error {
					order = append(order, "recover")
					return nil
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
			wantOrder := []string{"load-1", tt.openEvent, "recover", "overlay", "load-2"}
			if !reflect.DeepEqual(order, wantOrder) {
				t.Fatalf("boot order = %v, want %v", order, wantOrder)
			}
			pool.Close()
		})
	}
}

func TestResolveConfigAndPoolFailsClosedOnOverrideRecoveryError(t *testing.T) {
	tests := []struct {
		name       string
		firstErr   error
		recoverErr error
		openEvent  string
	}{
		{
			name:       "normal active override",
			recoverErr: settings.ErrBenchmarkOverrideActive,
			openEvent:  "open",
		},
		{
			name:       "normal recovery failure",
			recoverErr: errors.New("restore failed"),
			openEvent:  "open",
		},
		{
			name:       "keyless active override",
			firstErr:   llm.ErrMissingAPIKey,
			recoverErr: settings.ErrBenchmarkOverrideActive,
			openEvent:  "open-keyless",
		},
		{
			name:       "keyless recovery failure",
			firstErr:   llm.ErrMissingAPIKey,
			recoverErr: errors.New("restore failed"),
			openEvent:  "open-keyless",
		},
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
				return pool, nil
			}
			ops := bootSettingsOps{
				openKeyless: func(context.Context) (*pgxpool.Pool, bool, error) {
					order = append(order, "open-keyless")
					return pool, true, nil
				},
				recover: func(context.Context, *pgxpool.Pool) error {
					order = append(order, "recover")
					return tt.recoverErr
				},
				overlay: func(context.Context, *pgxpool.Pool) error {
					order = append(order, "overlay")
					return nil
				},
			}

			gotCfg, gotPool, err := resolveConfigAndPoolWithSettings(
				context.Background(), loadConfig, open, ops,
			)
			if !errors.Is(err, tt.recoverErr) {
				t.Fatalf("recovery error = %v, want %v", err, tt.recoverErr)
			}
			if gotCfg != nil || gotPool != nil {
				t.Fatalf("failed recovery returned cfg/pool = %v/%v", gotCfg, gotPool)
			}
			wantOrder := []string{"load-1", tt.openEvent, "recover"}
			if !reflect.DeepEqual(order, wantOrder) {
				t.Fatalf("failed recovery order = %v, want %v", order, wantOrder)
			}
			assertPoolClosed(t, pool)
		})
	}
}
