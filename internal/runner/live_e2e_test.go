//go:build live_e2e

// live_e2e_test.go is the AUTONOMOUS, real-LLM + real-Postgres replacement for the
// Phase-4 (HITL + identity + conversations) human UAT (04-HUMAN-UAT.md). Instead of
// an operator eyeballing the REPL and reading rows by hand, it drives the REAL
// runner.Runner over the REAL openai_compat client against DeepSeek-V4 Flash (via
// OpenRouter) and the REAL conversations/askuser/cachemetrics/identity Stores on a
// live Postgres, then ASSERTS every UAT scenario against DB ground truth — never
// against reply prose alone.
//
// Like the KV-cache harness this is a PAID, OPERATOR-AUTHORIZED gate behind the
// live_e2e build tag so it never runs in default CI/Makefile quality. It is gated on
// BOTH OPENROUTER_API_KEY (the paid live model) AND the Postgres DSN env: with the
// key unset it t.Skips locally (the one legitimate local skip); with POSTGRES_PASSWORD
// / AURA_DB_URL / AURA_DB_MIGRATE_URL unset it t.Skips locally but t.Fatals under $CI
// (no-skip-as-green — a skipped integration tier must never pass green in a pipeline).
//
// The four scenarios mirror 04-HUMAN-UAT.md 1:1:
//
//	SC#1 TestLiveE2E_PauseResume          — live ask_user pause, programmatic resume, wire-valid rehydration
//	SC#2 TestLiveE2E_ThreeSimultaneous    — 3 ask_user in one turn, FIFO priority DESC, CR-02 single assistant turn
//	SC#3 TestLiveE2E_AutoTitleAndCost     — multi-turn auto-title + cumulative USD aggregated from per-turn usage
//	SC#4 TestLiveE2E_SearchSimilarity     — pg_trgm search ordered by similarity DESC over persisted turns
//
// REPRODUCE (stack up: make db-up / docker compose; migrations applied through 0007):
//
//	cp /d/Aura/.env ./.env
//	set -a; . ./.env; set +a
//	export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
//	export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
//	go test -tags live_e2e -run TestLiveE2E -timeout 600s -v ./internal/runner/
package runner

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// liveHarness bundles the live-wired Runner plus the concrete Stores so the
// assertions can read DB ground truth directly (paused_states.resumed_answer,
// conversation_turns wire shape, conversations aggregates) without going through
// the narrow runner interfaces.
type liveHarness struct {
	r     *Runner
	conv  *conversations.Store
	pause *askuser.Store
	pool  *pgxpool.Pool
	cfg   llm.Config
}

// requireLiveEnv skips locally (and t.Fatals under $CI) when the paid key or the
// Postgres DSN env is unset. The key gate is checked first so a local run with no
// key skips cleanly without touching the DB.
func requireLiveEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("live_e2e: OPENROUTER_API_KEY unset — PAID gate, not CI. " +
			"Run: cp /d/Aura/.env ./.env; set -a; . ./.env; set +a; " +
			"go test -tags live_e2e -run TestLiveE2E -timeout 600s -v ./internal/runner/")
	}
}

// envOrSkipLive returns the env var, or skips locally / t.Fatals under $CI when
// unset — the no-skip-as-green helper mirrored from internal/db.envOrSkip (the
// runner package has no such helper, the unit tier runs on fakes).
func envOrSkipLive(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("live_e2e requires %s, but it is unset under CI — "+
				"a skipped integration tier must not pass as green; wire it in the paid CI lane", key)
		}
		t.Skipf("live_e2e requires %s; set it and re-run (e.g. via .env + the composed DSN)", key)
	}
	return v
}

// newLiveHarness migrates the DB through the shipped head, opens an aura_app pool,
// and wires a Runner over the REAL Stores + a REAL openai_compat client. The
// registry mirrors the runner's pause-capable set (text_response + ask_user +
// read_tool_output) so the live model can both pause and terminate a turn.
func newLiveHarness(t *testing.T) *liveHarness {
	t.Helper()
	requireLiveEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkipLive(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkipLive(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkipLive(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := "postgres://aura:" + pwd + "@" + host + ":" + port + "/aura?sslmode=disable"

	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("llm.Load: %v", err)
	}
	client := openai_compat.New(*cfg)

	runDir := t.TempDir()
	convStore := conversations.New(pool, conversations.Config{RunDir: runDir, TurnCapBytes: 65536})
	pauseStore := askuser.New(pool)

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	reg.Register(&tools.ReadToolOutput{})

	r := New(Deps{
		Conv:            convStore,
		Pause:           pauseStore,
		Identity:        identity.New(pool),
		CacheMetrics:    cachemetrics.New(pool),
		ToolInvocations: toolinvocations.New(pool),
		Client:          client,
		Registry:        reg,
		LLM:             *cfg,
		RunDir:          runDir,
		PreviewCap:      2048,
		// StopTimeout must exceed TitleTimeout so Stop reliably JOINS an in-flight
		// auto-title worker (its live LLM call can run up to TitleTimeout under load) —
		// otherwise Stop returns a drain-timeout error AND leaves the worker's HTTP
		// round-trip in flight, which would trip the package's goleak in TestMain.
		TitleTimeout: 30 * time.Second,
		StopTimeout:  45 * time.Second,
	})
	return &liveHarness{r: r, conv: convStore, pause: pauseStore, pool: pool, cfg: *cfg}
}

// newLiveConversation mints a conversation id, creates the row via the Runner (so
// the seeded `local` identity owns it), and registers a cascade-delete cleanup so a
// completed live test leaves no garbage in the shared aura.* tables.
func (h *liveHarness) newLiveConversation(t *testing.T, ctx context.Context) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := h.r.NewConversationWithID(ctx, id); err != nil {
		t.Fatalf("NewConversationWithID: %v", err)
	}
	t.Cleanup(func() {
		// Stop joins the auto-title WaitGroup (bounded) so an in-flight title worker's
		// live HTTP round-trip cannot linger past the test and trip goleak in TestMain
		// (D-A5-01). It also auto-resolves any orphan pending. Best-effort: the row is
		// deleted next regardless.
		_ = h.r.Stop(context.Background(), id)
		// ON DELETE CASCADE removes conversation_turns + paused_states + cache_metrics.
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM aura.conversations WHERE id = $1", id)
	})
	return id
}

// resumedAnswerOf reads the persisted resumed_answer jsonb for a token directly off
// the DB (ground truth, not the reply). Returns the decoded {action, content} and
// whether the row is resolved (resumed_at IS NOT NULL).
func (h *liveHarness) resumedAnswerOf(t *testing.T, ctx context.Context, token string) (askuser.ResumeAnswer, bool) {
	t.Helper()
	var raw []byte
	var resolved bool
	err := db.WithIdentityTxRaw(ctx, h.pool, identityctx.LocalOperatorIdentity, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT resumed_answer, resumed_at IS NOT NULL FROM aura.paused_states WHERE token = $1",
			token,
		).Scan(&raw, &resolved)
	})
	if err != nil {
		t.Fatalf("read resumed_answer for %s: %v", token, err)
	}
	var ans askuser.ResumeAnswer
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &ans); err != nil {
			t.Fatalf("decode resumed_answer for %s: %v", token, err)
		}
	}
	return ans, resolved
}
