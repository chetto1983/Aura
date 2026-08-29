//go:build cot_eval

// e2e_test.go is the Phase 10 (CAP-06) LIVE North-Star smoke for the scheduler. It
// drives the REAL agent.LlmAgent over the REAL openai_compat client against
// DeepSeek-V4 with a registry carrying the non-deferred `task` tool wired to the live
// Postgres scheduler store (the same persistence `aura chat`/`aura serve` use). Two
// natural Italian North-Star prompts (NO "cron"/"task"/"schedule" literal — the model
// must pick the `task` tool on its own, the Phase-9 swarm-E2E precedent):
//
//   - Q3 "ricordami di chiamare Monica alle 17:30" → the model schedules an `at`
//     one-shot reminder; the HARD assertion is a row landing in aura.scheduler_tasks
//     with kind=reminder + schedule_kind=at (ground truth, NOT the model's prose).
//   - Q1/Q2 cron agent_job (the operator picks the daily-mail-summary or Cuneo-news
//     prompt + a self-send MCP recipient via env) → the model schedules a cron
//     agent_job; the row lands with kind=agent_job + schedule_kind=cron. The live
//     fire + WhatsApp/mail delivery read-back is the OPERATOR checkpoint (Task 3) — the
//     scaffold here proves the natural-prompt → `task` tool → persisted row leg.
//
// THIS IS A MANUAL, OPERATOR-AUTHORIZED, PAID GATE — NOT A CI JOB. It is gated on
// OPENROUTER_API_KEY (live LLM) + the AURA_DB_URL the scheduler store opens, behind the
// cot_eval build tag so it NEVER runs in normal CI / Makefile quality targets
// (no-skip-as-green: CI gates stay on unit/property/race/goleak — this is the ONE
// legitimate skip, exactly like TestSwarmE2E). With either gate unset it t.Skips.
//
// OPERATOR INVOCATION:
//
//	set -a; . ./.env; set +a                       # OPENROUTER_API_KEY + POSTGRES_PASSWORD
//	export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
//	export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
//	# optional, for the cron agent_job + delivery read-back (mounted MCP, Task 3):
//	#   export AURA_EVAL_SELF_PHONE="39XXXXXXXXXX"   # your OWN number, self-send only (D-19)
//	go test -tags cot_eval -run TestSchedulerNorthStarE2E -timeout 300s -v ./internal/cron/
//
// package cron_test (external): the test imports internal/agent/tools, and tools
// imports internal/cron — an INTERNAL cron test would be a cycle. The external test
// package breaks it (cron_test → {cron, tools}; neither imports cron_test).
package cron_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// e2eStore is the live taskStore the `task` tool drives in the smoke — a minimal
// mirror of cmd/aura's cronTaskStore over the same cron.Store + raw status SQL. It is
// test-local so the smoke needs no cmd/aura import (the production adapter is unit-
// covered by the registry boot tests).
type e2eStore struct {
	pool  *pgxpool.Pool
	store *cron.Store
}

func (s *e2eStore) CreateScheduledTask(ctx context.Context, in tools.CreateTaskInput) (tools.ScheduledTask, error) {
	created, err := s.store.CreateTask(ctx, cron.CreateTaskParams{
		Kind: cron.TaskKind(in.Kind),
		Spec: cron.ScheduleSpec{
			Kind:         cron.ScheduleKind(in.ScheduleKind),
			CronExpr:     in.CronExpr,
			EveryMinutes: in.EveryMinutes,
			RunAt:        in.RunAt,
			TZ:           in.TZ,
		},
		Payload:     in.Payload,
		StepBudget:  in.StepBudget,
		NextRunAt:   in.NextRunAt,
		NotifyRoute: in.NotifyRoute,
	})
	if err != nil {
		return tools.ScheduledTask{}, err
	}
	status := in.Status
	if status == "" {
		status = "active"
	}
	if status != "active" {
		if _, err := s.pool.Exec(ctx,
			`UPDATE aura.scheduler_tasks SET status = $2, updated_at = now() WHERE id = $1`,
			created.ID, status); err != nil {
			return tools.ScheduledTask{}, err
		}
	}
	return tools.ScheduledTask{ID: created.ID, Kind: string(created.Kind), ScheduleKind: string(created.ScheduleKind), Status: status, NextRunAt: created.NextRunAt}, nil
}

func (s *e2eStore) ListScheduledTasks(context.Context) ([]tools.ScheduledTask, error) {
	return nil, nil
}
func (s *e2eStore) CancelScheduledTask(ctx context.Context, id string) error {
	return s.store.CancelTask(ctx, id)
}
func (s *e2eStore) RunScheduledTaskNow(context.Context, string) error  { return nil }
func (s *e2eStore) ApproveScheduledTask(context.Context, string) error { return nil }

// TestSchedulerNorthStarE2E drives the natural-prompt → `task` tool → persisted-row
// leg of the North-Star queries live. The fire + delivery read-back is the operator
// checkpoint (Task 3); the scaffold's HARD gate is the persisted scheduler row.
func TestSchedulerNorthStarE2E(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("scheduler E2E: OPENROUTER_API_KEY unset — MANUAL paid gate, NOT CI. See the file header.")
	}
	appURL := os.Getenv("AURA_DB_URL")
	if appURL == "" {
		t.Skip("scheduler E2E: AURA_DB_URL unset — set it (compose up + derive from POSTGRES_PASSWORD). See the file header.")
	}

	baseCfg, err := llm.Load()
	if err != nil {
		t.Fatalf("llm.Load: %v", err)
	}
	client := openai_compat.New(*baseCfg)

	ctx, cancel := context.WithTimeout(context.Background(), 280*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer pool.Close()
	store := &e2eStore{pool: pool, store: cron.New(pool)}

	cfg := config.LoadDB()
	reg := buildE2ERegistry(store)

	// Q3 — natural Italian reminder, NO "cron"/"task"/"schedule" word in the prompt
	// (asserted below). The model must pick the `task` tool and emit an `at` schedule.
	t.Run("Q3_reminder_at", func(t *testing.T) {
		// "domani alle 9:30" is unambiguously in the future regardless of the wall
		// clock at run time — "oggi alle HH:MM" silently becomes a past (unschedulable)
		// instant when the suite runs after that local time, and the model then
		// (correctly) declines to schedule a past reminder, which is a test-prompt flaw,
		// not a wiring bug (Gate-3 finding, 10-06).
		prompt := "Ricordami in questa conversazione di chiamare Monica domani alle 9:30."
		assertNoSchedulingLiteral(t, prompt)
		before := time.Now().UTC()
		driveTurn(t, ctx, client, *baseCfg, reg, cfg, prompt)
		row := latestTaskSince(t, ctx, pool, before)
		if row.kind != "reminder" || row.scheduleKind != "at" {
			t.Fatalf("Q3: expected a reminder/at row from the natural prompt, got kind=%q schedule_kind=%q", row.kind, row.scheduleKind)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM aura.scheduler_tasks WHERE id = $1", row.id)
		})
		t.Logf("Q3 OK: %s scheduled %s/%s (next=%s)", row.id, row.kind, row.scheduleKind, row.nextRun)
	})

	// Q1/Q2 — natural Italian cron agent_job (daily mail summary / Cuneo news). The
	// HARD scaffold gate is the persisted cron agent_job row; the live fire + delivery
	// read-back is the operator checkpoint (Task 3, recorded in the quality snapshot).
	t.Run("Q1_cron_agent_job", func(t *testing.T) {
		prompt := "Ogni mattina alle 9:30 fammi un riassunto delle mail da evadere e consegnalo in questa conversazione."
		assertNoSchedulingLiteral(t, prompt)
		before := time.Now().UTC()
		driveTurn(t, ctx, client, *baseCfg, reg, cfg, prompt)
		row := latestTaskSince(t, ctx, pool, before)
		if row.kind != "agent_job" || row.scheduleKind != "cron" {
			t.Fatalf("Q1: expected an agent_job/cron row from the natural prompt, got kind=%q schedule_kind=%q", row.kind, row.scheduleKind)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM aura.scheduler_tasks WHERE id = $1", row.id)
		})
		t.Logf("Q1 OK: %s scheduled %s/%s (next=%s) — live fire + delivery read-back is the operator checkpoint (Task 3)", row.id, row.kind, row.scheduleKind, row.nextRun)
	})
}

// buildE2ERegistry registers the non-deferred `task` tool over the live store plus the
// minimal actionable built-ins so reg.Validate stays green. It mirrors the production
// buildBaseRegistry task wiring (the smoke exercises the SAME tool the daemon serves).
func buildE2ERegistry(store *e2eStore) *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(tools.CurrentTime{})
	reg.Register(tools.AskUser{})
	reg.Register(&tools.TaskTool{Store: store})
	return reg
}

// driveTurn runs ONE live LlmAgent turn on the prompt, draining its Event stream so
// the `task` tool's Execute persists the row as a side-effect. It fails on a stream
// error; the row assertion is the caller's ground-truth check.
func driveTurn(t *testing.T, ctx context.Context, client llm.Client, llmCfg llm.Config, reg *tools.Registry, cfg *config.Config, prompt string) {
	t.Helper()
	worker := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     client,
		LLM:        llmCfg,
		Registry:   reg,
		PreviewCap: cfg.ToolPreviewCap,
		RunDir:     t.TempDir(),
		SessionID:  "scheduler-e2e-" + uuid.NewString()[:8],
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	budget, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := agent.InvocationContext{Ctx: ctx, RequestID: uuid.Must(uuid.NewV7()), Budget: budget}
	for ev, err := range worker.Run(ic) {
		if err != nil {
			t.Fatalf("live turn error: %v", err)
		}
		// Surface the model's tool choice + final prose so a failure (the model
		// declining to schedule) is diagnosable from -v output without re-instrumenting.
		if ev != nil && ev.LLMResponse != nil {
			for _, tc := range ev.LLMResponse.ToolCalls {
				t.Logf("tool_call name=%s args=%s", tc.Function.Name, tc.Function.Arguments)
			}
		}
	}
}

// taskRow is the ground-truth projection the smoke asserts on (DB, never the prose).
type taskRow struct {
	id           string
	kind         string
	scheduleKind string
	nextRun      string
}

// latestTaskSince returns the most recent scheduler_tasks row created at/after since,
// failing if none landed (the model did not schedule anything — a smoke failure).
func latestTaskSince(t *testing.T, ctx context.Context, pool *pgxpool.Pool, since time.Time) taskRow {
	t.Helper()
	var r taskRow
	var next *time.Time
	err := pool.QueryRow(ctx, `
		SELECT id, kind, schedule_kind, next_run_at
		FROM aura.scheduler_tasks
		WHERE created_at >= $1
		ORDER BY created_at DESC
		LIMIT 1`, since).Scan(&r.id, &r.kind, &r.scheduleKind, &next)
	if err != nil {
		t.Fatalf("no scheduler_tasks row landed after the natural prompt (model did not schedule): %v", err)
	}
	if next != nil {
		r.nextRun = next.UTC().Format(time.RFC3339)
	}
	return r
}

// assertNoSchedulingLiteral guards the natural-prompt discipline: the prompt must NOT
// leak a scheduling keyword that would spoon-feed the tool choice (Phase-9 precedent —
// the model must infer the `task` tool from intent, not a literal).
func assertNoSchedulingLiteral(t *testing.T, prompt string) {
	t.Helper()
	low := strings.ToLower(prompt)
	for _, banned := range []string{"cron", "task", "schedule", "pianifica", "programma"} {
		if strings.Contains(low, banned) {
			t.Fatalf("natural-prompt discipline: prompt leaks the scheduling literal %q: %q", banned, prompt)
		}
	}
}
