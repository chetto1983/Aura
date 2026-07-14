package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5/pgxpool"
)

func rolloutSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestCompactionRolloutComposition(t *testing.T) {
	s := rolloutSource(t, "chat_boot.go")
	if !strings.Contains(s, "NewCompactionRolloutStore(pool)") || !strings.Contains(s, "NewCompactionRolloutController") {
		t.Fatal("durable rollout composition missing")
	}
}
func TestCompactionRolloutPersistedReaderInjection(t *testing.T) {
	s := rolloutSource(t, "chat_boot.go")
	if !strings.Contains(s, "CompactionEffective: rolloutReader") || !strings.Contains(s, "compactionEffective: rolloutReader") {
		t.Fatal("persisted reader injection missing")
	}
}
func TestCompactionRolloutEvaluatorLifecycle(t *testing.T) {
	s := rolloutSource(t, "serve.go")
	if !strings.Contains(s, "compactionRollout.Run(signalCtx") {
		t.Fatal("signal-scoped evaluator lifecycle missing")
	}
}
func TestCompactionMemoryComposition(t *testing.T) {
	s := rolloutSource(t, "serve.go")
	if !strings.Contains(s, "SetCompactionMemoryStore(memory.NewStore(chat.pool))") {
		t.Fatal("IC-10 compaction memory store is not wired into the live control plane")
	}
}
func TestCompactionRolloutCancellation(t *testing.T) {
	s := rolloutSource(t, "serve.go")
	if !strings.Contains(s, "errors.Is(err, context.Canceled)") {
		t.Fatal("evaluator cancellation handling missing")
	}
}
func TestCompactionRolloutStartupFailsClosed(t *testing.T) {
	s := rolloutSource(t, "chat_boot.go")
	if !strings.Contains(s, "compaction rollout durable preflight") || !strings.Contains(s, "rolloutReader.Read(ctx)") {
		t.Fatal("durable preflight missing")
	}
}

// TestCompactionRolloutDisabledBootstrapSeed pins the fresh-DB crash-loop fix: a
// disabled-default rollout row must be seeded BEFORE the fail-closed Read preflight
// (and before the serve.go evaluator loop starts), or every boot against a freshly
// migrated database hard-fails with "no rows in result set" forever.
func TestCompactionRolloutDisabledBootstrapSeed(t *testing.T) {
	s := rolloutSource(t, "chat_boot.go")
	seedIdx := strings.Index(s, "rolloutStore.EnsureDisabledDefault(ctx")
	readIdx := strings.Index(s, "rolloutReader.Read(ctx)")
	if seedIdx < 0 {
		t.Fatal("disabled-default bootstrap seed missing")
	}
	if readIdx < 0 || seedIdx > readIdx {
		t.Fatal("bootstrap seed must run before the durable preflight Read")
	}
}

// chat_boot_test.go pins QUAL-04b: no boot error path leaks a pool or an MCP
// subprocess. The real leak was the CommandHookManagerFromEnv failure path, which
// returned after the pool + registry were built with neither pool.Close nor an
// mcpClosers drain; the fix is a deferred close-on-error guard in assembleChatEnv.
// db.Open eagerly Pings, so the concrete boot cannot be driven past pool-open
// without a live Postgres — these tests use a NON-pinging pgxpool (pgxpool.New never
// connects with MinConns=0) plus an injectable pool opener, keeping the whole suite
// a fast, PG-free unit tier (verify: go test -race ./cmd/aura/ -run Boot).

// poolCloseSpy is a minimal interface{ Close() } stand-in so releaseBootResources'
// close-on-error contract (drain closers, then close the pool) is asserted without a
// live pool. It records its close into the shared order slice.
type poolCloseSpy struct{ order *[]string }

func (p poolCloseSpy) Close() { *p.order = append(*p.order, "pool") }

// validBootConfig is the minimal dev-profile config that PASSES cfg.Validate: a
// non-empty DB DSN + Neo4j password (the two all-tier required secrets) and a
// loopback web bind. RunDir is left empty so ScanOrphans is a no-op (no pool query).
func validBootConfig() *config.Config {
	return &config.Config{
		Profile:  config.ProfileDev,
		DB:       db.Config{URL: "postgres://u:p@127.0.0.1:1/aura"},
		Neo4j:    knowledge.Config{Password: "graph-secret"},
		AGUIBind: "127.0.0.1:9080",
		LLM:      llm.Config{Model: "test/model"},
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
	var order []string
	closers := []func() error{
		func() error { order = append(order, "closer-a"); return nil },
		func() error { order = append(order, "closer-b"); return nil },
	}
	releaseBootResources(poolCloseSpy{order: &order}, closers)

	if len(order) != 3 {
		t.Fatalf("release fired %d actions, want 3 (2 closers + pool): %v", len(order), order)
	}
	if order[len(order)-1] != "pool" {
		t.Fatalf("pool must close AFTER the closers drain, got %v", order)
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

	cfg, gotPool, err := resolveConfigAndPool(ctx, loadConfig, opener)
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
