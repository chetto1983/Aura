// task subcommand dispatcher for `aura task {schedule|list|cancel|run_now|approve|runs|doctor}`
// (D-14 full triad CLI parity + D-17 doctor verb). Lives in package main alongside
// cmd/aura/main.go's switch case "task". It mirrors runWeb/runWebDoctor: a hand-parsed
// switch (no cobra — repo convention), config.LoadDB so no OPENROUTER_API_KEY is needed,
// human-readable output, sysexits codes. It makes EVERY scheduler grammar path
// operator-testable without an LLM (SC#1 + the chaos/smoke scripts).
//
// The schedule path uses the shipped cron.ParseSchedule (gronx.IsValid before persist,
// T-10-14) + DST-safe cron.NextRunAt, and scoring.ComputeTaskTier to route a
// destructive payload to pending_approval (D-27). Persistence is raw parameterized SQL
// over the pool (the status-aware INSERT + approve/runs/doctor aggregates that the
// 10-02 store does not yet expose) — never string-concatenated, T-10-01.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const taskUsage = "usage: aura task {schedule|list|cancel|run_now|approve|runs|doctor}"

func runTask(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, taskUsage)
		os.Exit(exitUsage)
	}
	cfg := config.LoadDB()
	ctx := context.Background()

	switch args[0] {
	case "schedule":
		taskSchedule(ctx, cfg, args[1:])
	case "list":
		taskList(ctx, cfg)
	case "cancel":
		taskCancel(ctx, cfg, args[1:])
	case "run_now":
		taskRunNow(ctx, cfg, args[1:])
	case "approve":
		taskApprove(ctx, cfg, args[1:])
	case "runs":
		taskRuns(ctx, cfg, args[1:])
	case "doctor":
		taskDoctor(ctx, cfg)
	default:
		fmt.Fprintf(os.Stderr, "aura task: unknown subcommand %q\n%s\n", args[0], taskUsage)
		os.Exit(exitUsage)
	}
}

// openTaskPool opens the runtime (aura_app) pool the task CLI operates against.
func openTaskPool(ctx context.Context, cfg *config.Config) *pgxpool.Pool {
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task:", err)
		os.Exit(exitInfra)
	}
	return pool
}

// taskSchedule parses the full at|every|cron triad (D-14) plus --max-steps
// (amendment #19), validates via cron.ParseSchedule, computes the first fire and the
// risk tier, and inserts the row as active or pending_approval (D-27).
func taskSchedule(ctx context.Context, cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("task schedule", flag.ExitOnError)
	cronExpr := fs.String("cron", "", "cron expression (5 fields), e.g. '30 9 * * 1-5'")
	at := fs.String("at", "", "one-shot RFC-3339 instant, e.g. 2030-01-01T09:30:00Z")
	every := fs.Int("every", 0, "recurring interval in minutes (minimum 5)")
	maxSteps := fs.Int("max-steps", 0, "agent step budget (kind=agent_job)")
	kind := fs.String("kind", "", "reminder|agent_job|backup_postgres|backup_neo4j")
	argsJSON := fs.String("args", "", "JSON payload, e.g. '{\"text\":\"...\"}'")
	notify := fs.String("notify", "", "whatsapp|email|stdout")
	tz := fs.String("tz", defaultSchedulerTZ(), "IANA timezone for a cron schedule")
	_ = fs.Parse(args)

	if *kind == "" {
		fmt.Fprintln(os.Stderr, "aura task schedule: --kind is required")
		os.Exit(exitUsage)
	}
	scheduleKind, runAt := triadToSpec(*cronExpr, *at, *every)
	payload := payloadOrEmpty(*argsJSON)

	spec, err := cron.ParseSchedule(scheduleKind, *cronExpr, *every, runAt, *tz)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task schedule:", err)
		os.Exit(exitUsage)
	}
	next, err := cron.FirstFire(spec, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task schedule:", err)
		os.Exit(exitUsage)
	}

	tier := scoring.ComputeTaskTier(scoring.TaskArgs{Kind: *kind, ScheduleKind: scheduleKind, Payload: payload})
	status := "active"
	if scoring.GateRecommended(tier) {
		status = "pending_approval"
	}

	pool := openTaskPool(ctx, cfg)
	defer pool.Close()

	id := uuid.Must(uuid.NewV7()).String()
	_, err = pool.Exec(ctx, `
		INSERT INTO aura.scheduler_tasks
			(id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
			 step_budget, status, next_run_at, notify_route)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		id, *kind, scheduleKind,
		nullableText(spec.CronExpr), nullableInt(spec.EveryMinutes), nullableTime(spec.RunAt),
		spec.TZ, payload, nullableInt(*maxSteps), status, nullableTime(next), nullableText(*notify))
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task schedule:", err)
		os.Exit(exitInfra)
	}

	fmt.Printf("scheduled %s (kind=%s, schedule=%s, risk=%s, status=%s)\n", id, *kind, scheduleKind, tier, status)
	if status == "pending_approval" {
		fmt.Printf("awaiting approval — run: aura task approve %s\n", id)
	} else if !next.IsZero() {
		fmt.Printf("next run: %s\n", next.UTC().Format(time.RFC3339))
	}
}

// triadToSpec resolves exactly one of --cron/--at/--every to a schedule_kind. An
// over/under-specified triad is left for cron.ParseSchedule to reject with a clear
// message (a missing kind yields an empty schedule_kind → ErrInvalidScheduleKind).
func triadToSpec(cronExpr, at string, every int) (string, time.Time) {
	switch {
	case cronExpr != "":
		return "cron", time.Time{}
	case at != "":
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aura task schedule: --at must be RFC-3339: %v\n", err)
			os.Exit(exitUsage)
		}
		return "at", t.UTC()
	case every > 0:
		return "every", time.Time{}
	default:
		fmt.Fprintln(os.Stderr, "aura task schedule: exactly one of --cron/--at/--every is required")
		os.Exit(exitUsage)
		return "", time.Time{}
	}
}

// taskList renders active + pending_approval tasks with next_run_at, flagging the
// awaiting-approval rows (D-14/D-17 Claude's discretion format).
func taskList(ctx context.Context, cfg *config.Config) {
	pool := openTaskPool(ctx, cfg)
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT id, kind, schedule_kind, status, next_run_at
		FROM aura.scheduler_tasks
		WHERE status IN ('active', 'pending_approval')
		ORDER BY next_run_at ASC NULLS LAST, id ASC`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task list:", err)
		os.Exit(exitInfra)
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tKIND\tSCHEDULE\tSTATUS\tNEXT_RUN")
	n := 0
	for rows.Next() {
		var id, kind, schedKind, status string
		var next *time.Time
		if err := rows.Scan(&id, &kind, &schedKind, &status, &next); err != nil {
			fmt.Fprintln(os.Stderr, "aura task list:", err)
			os.Exit(exitInfra)
		}
		flag := status
		if status == "pending_approval" {
			flag = "pending_approval [awaiting approval]"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, kind, schedKind, flag, fmtTimePtr(next))
		n++
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "aura task list:", err)
		os.Exit(exitInfra)
	}
	_ = w.Flush()
	if n == 0 {
		fmt.Println("no scheduled tasks")
	}
}

func taskCancel(ctx context.Context, cfg *config.Config, args []string) {
	id := requireID("cancel", args)
	pool := openTaskPool(ctx, cfg)
	defer pool.Close()
	if err := cron.New(pool).CancelTask(ctx, id); err != nil {
		fmt.Fprintln(os.Stderr, "aura task cancel:", err)
		os.Exit(exitInfra)
	}
	fmt.Printf("cancelled %s\n", id)
}

// taskRunNow flips an active task's next_run_at to now() so the next tick claims it
// immediately. A pending_approval task is refused (D-13: approval is the only path
// out of pending_approval; run_now must not bypass it).
func taskRunNow(ctx context.Context, cfg *config.Config, args []string) {
	id := requireID("run_now", args)
	pool := openTaskPool(ctx, cfg)
	defer pool.Close()

	tag, err := pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET next_run_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'active'`, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task run_now:", err)
		os.Exit(exitInfra)
	}
	if tag.RowsAffected() == 0 {
		fmt.Fprintf(os.Stderr, "aura task run_now: %s is not an active task (pending_approval or cancelled tasks cannot be run)\n", id)
		os.Exit(exitUsage)
	}
	fmt.Printf("queued %s to run on the next tick\n", id)
}

// taskApprove is the only transition out of pending_approval (T-10-13). It flips
// status to active so the gated task can fire.
func taskApprove(ctx context.Context, cfg *config.Config, args []string) {
	id := requireID("approve", args)
	pool := openTaskPool(ctx, cfg)
	defer pool.Close()

	tag, err := pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET status = 'active', updated_at = now()
		WHERE id = $1 AND status = 'pending_approval'`, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task approve:", err)
		os.Exit(exitInfra)
	}
	if tag.RowsAffected() == 0 {
		fmt.Fprintf(os.Stderr, "aura task approve: %s is not awaiting approval\n", id)
		os.Exit(exitUsage)
	}
	fmt.Printf("approved %s (now active)\n", id)
}

// taskRuns lists the agent_job_runs audit ledger, optionally filtered by --task.
func taskRuns(ctx context.Context, cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("task runs", flag.ExitOnError)
	taskFilter := fs.String("task", "", "filter by task id")
	limit := fs.Int("limit", 20, "max rows")
	_ = fs.Parse(args)

	pool := openTaskPool(ctx, cfg)
	defer pool.Close()

	var rows pgx.Rows
	var err error
	if *taskFilter != "" {
		rows, err = pool.Query(ctx, `
			SELECT id, task_id, status, started_at, completed_at, summary, last_error
			FROM aura.agent_job_runs WHERE task_id = $1
			ORDER BY started_at DESC LIMIT $2`, *taskFilter, *limit)
	} else {
		rows, err = pool.Query(ctx, `
			SELECT id, task_id, status, started_at, completed_at, summary, last_error
			FROM aura.agent_job_runs
			ORDER BY started_at DESC LIMIT $1`, *limit)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura task runs:", err)
		os.Exit(exitInfra)
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "RUN_ID\tTASK_ID\tSTATUS\tSTARTED\tCOMPLETED")
	n := 0
	for rows.Next() {
		var id, taskID, status string
		var started time.Time
		var completed *time.Time
		var summary, lastErr *string
		if err := rows.Scan(&id, &taskID, &status, &started, &completed, &summary, &lastErr); err != nil {
			fmt.Fprintln(os.Stderr, "aura task runs:", err)
			os.Exit(exitInfra)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, taskID, status, started.UTC().Format(time.RFC3339), fmtTimePtr(completed))
		n++
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "aura task runs:", err)
		os.Exit(exitInfra)
	}
	_ = w.Flush()
	if n == 0 {
		fmt.Println("no runs recorded")
	}
}

// taskDoctor reports scheduler health against PG (D-17): the freshest tick proxy
// (max next_run_at vs now), due-task count, in-flight runs, and the staleness of the
// oldest running run's heartbeat. config.LoadDB() means no OPENROUTER_API_KEY is
// needed. Exit 0 on a healthy read, 71 on an infra fault.
func taskDoctor(ctx context.Context, cfg *config.Config) {
	pool := openTaskPool(ctx, cfg)
	defer pool.Close()

	var activeCount, pendingCount, dueCount int
	if err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'active'),
			count(*) FILTER (WHERE status = 'pending_approval'),
			count(*) FILTER (WHERE status = 'active' AND next_run_at <= now())
		FROM aura.scheduler_tasks`).Scan(&activeCount, &pendingCount, &dueCount); err != nil {
		fmt.Fprintln(os.Stderr, "aura task doctor:", err)
		os.Exit(exitInfra)
	}

	var inFlight int
	var oldestHeartbeat *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT count(*), min(last_heartbeat_at)
		FROM aura.agent_job_runs WHERE status = 'running'`).Scan(&inFlight, &oldestHeartbeat); err != nil {
		fmt.Fprintln(os.Stderr, "aura task doctor:", err)
		os.Exit(exitInfra)
	}

	var nextRun *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT min(next_run_at) FROM aura.scheduler_tasks WHERE status = 'active'`).Scan(&nextRun); err != nil {
		fmt.Fprintln(os.Stderr, "aura task doctor:", err)
		os.Exit(exitInfra)
	}

	fmt.Printf("active tasks:        %d\n", activeCount)
	fmt.Printf("pending approval:    %d\n", pendingCount)
	fmt.Printf("due now:             %d\n", dueCount)
	fmt.Printf("next run at:         %s\n", fmtTimePtr(nextRun))
	fmt.Printf("in-flight runs:      %d\n", inFlight)
	if oldestHeartbeat != nil {
		fmt.Printf("oldest heartbeat:    %s (%s ago)\n", oldestHeartbeat.UTC().Format(time.RFC3339), time.Since(*oldestHeartbeat).Round(time.Second))
	} else {
		fmt.Printf("oldest heartbeat:    — (no running jobs)\n")
	}
	fmt.Println("status: OK")
}

// --- helpers ---

func requireID(action string, args []string) string {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintf(os.Stderr, "usage: aura task %s <id>\n", action)
		os.Exit(exitUsage)
	}
	if _, err := uuid.Parse(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "aura task %s: %q is not a valid task id\n", action, args[0])
		os.Exit(exitUsage)
	}
	return args[0]
}

func payloadOrEmpty(s string) []byte {
	if s == "" {
		return []byte("{}")
	}
	if !json.Valid([]byte(s)) {
		fmt.Fprintln(os.Stderr, "aura task schedule: --args must be valid JSON")
		os.Exit(exitUsage)
	}
	return []byte(s)
}

func defaultSchedulerTZ() string {
	if tz := os.Getenv("AURA_SCHEDULER_TZ"); tz != "" {
		return tz
	}
	return "Europe/Rome"
}

func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableInt(n int) *int {
	if n <= 0 {
		return nil
	}
	return &n
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func fmtTimePtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}
