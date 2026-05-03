package swarm

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS swarm_runs (
  id           TEXT PRIMARY KEY,
  goal         TEXT NOT NULL,
  status       TEXT NOT NULL,
  created_by   TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  completed_at TEXT,
  last_error   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS swarm_tasks (
  id             TEXT PRIMARY KEY,
  run_id         TEXT NOT NULL,
  parent_id      TEXT NOT NULL DEFAULT '',
  role           TEXT NOT NULL,
  subject        TEXT NOT NULL,
  prompt         TEXT NOT NULL,
  tool_allowlist TEXT NOT NULL DEFAULT '[]',
  status         TEXT NOT NULL,
  depth          INTEGER NOT NULL DEFAULT 0,
  attempts       INTEGER NOT NULL DEFAULT 0,
  blocked_by     TEXT NOT NULL DEFAULT '[]',
  result         TEXT NOT NULL DEFAULT '',
  tool_calls     INTEGER NOT NULL DEFAULT 0,
  llm_calls      INTEGER NOT NULL DEFAULT 0,
  tokens_prompt  INTEGER NOT NULL DEFAULT 0,
  tokens_completion INTEGER NOT NULL DEFAULT 0,
  tokens_total   INTEGER NOT NULL DEFAULT 0,
  elapsed_ms     INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL,
  started_at     TEXT,
  completed_at   TEXT,
  last_error     TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(run_id) REFERENCES swarm_runs(id)
);

CREATE INDEX IF NOT EXISTS idx_swarm_tasks_run ON swarm_tasks(run_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_status ON swarm_tasks(status, created_at);
`

type Store struct {
	db    *sql.DB
	owned bool
	now   func() time.Time
	newID func(prefix string) (string, error)
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open swarm db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping swarm db: %w", err)
	}
	s := newStore(db, true)
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func NewStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("swarm store: db required")
	}
	s := newStore(db, false)
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func newStore(db *sql.DB, owned bool) *Store {
	db.SetMaxOpenConns(1)
	return &Store{
		db:    db,
		owned: owned,
		now: func() time.Time {
			return time.Now().UTC()
		},
		newID: randomID,
	}
}

func (s *Store) Close() error {
	if !s.owned {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("swarm migrate busy_timeout: %w", err)
	}
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("swarm migrate: %w", err)
	}
	if err := addSwarmTaskMetricColumns(s.db); err != nil {
		return fmt.Errorf("swarm migrate metric columns: %w", err)
	}
	return nil
}

func addSwarmTaskMetricColumns(db *sql.DB) error {
	cols, err := tableColumns(db, "swarm_tasks")
	if err != nil {
		return err
	}
	for _, col := range []string{"tokens_prompt", "tokens_completion", "tokens_total"} {
		if cols[col] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE swarm_tasks ADD COLUMN ` + col + ` INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func (s *Store) CreateRun(ctx context.Context, goal, createdBy string) (*Run, error) {
	id, err := s.newID("swarm")
	if err != nil {
		return nil, err
	}
	now := s.now()
	run := &Run{
		ID:        id,
		Goal:      goal,
		Status:    RunPending,
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO swarm_runs (id, goal, status, created_by, created_at, updated_at, last_error)
VALUES (?, ?, ?, ?, ?, ?, '')`,
		run.ID, run.Goal, string(run.Status), run.CreatedBy, formatTime(run.CreatedAt), formatTime(run.UpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("create swarm run: %w", err)
	}
	return run, nil
}

func (s *Store) MarkRunRunning(ctx context.Context, id string) error {
	return s.updateRun(ctx, id, RunRunning, nil, "")
}

func (s *Store) CompleteRun(ctx context.Context, id string) error {
	now := s.now()
	return s.updateRun(ctx, id, RunCompleted, &now, "")
}

func (s *Store) FailRun(ctx context.Context, id string, errText string) error {
	now := s.now()
	return s.updateRun(ctx, id, RunFailed, &now, errText)
}

func (s *Store) updateRun(ctx context.Context, id string, status RunStatus, completedAt *time.Time, errText string) error {
	now := s.now()
	completed := nullableTime(completedAt)
	res, err := s.db.ExecContext(ctx, `
UPDATE swarm_runs
SET status = ?, updated_at = ?, completed_at = ?, last_error = ?
WHERE id = ?`,
		string(status), formatTime(now), completed, errText, id)
	if err != nil {
		return fmt.Errorf("update swarm run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("swarm run %s not found", id)
	}
	return nil
}

func (s *Store) GetRun(ctx context.Context, id string) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, goal, status, created_by, created_at, updated_at, completed_at, last_error
FROM swarm_runs WHERE id = ?`, id)
	return scanRun(row)
}

func (s *Store) CreateTask(ctx context.Context, runID string, a Assignment) (*Task, error) {
	id, err := s.newID("task")
	if err != nil {
		return nil, err
	}
	now := s.now()
	task := &Task{
		ID:            id,
		RunID:         runID,
		ParentID:      a.ParentID,
		Role:          a.Role,
		Subject:       a.Subject,
		Prompt:        a.Prompt,
		ToolAllowlist: cleanList(a.ToolAllowlist),
		Status:        TaskPending,
		Depth:         a.Depth,
		CreatedAt:     now,
	}
	allow, err := json.Marshal(task.ToolAllowlist)
	if err != nil {
		return nil, fmt.Errorf("marshal tool allowlist: %w", err)
	}
	blocked, err := json.Marshal(task.BlockedBy)
	if err != nil {
		return nil, fmt.Errorf("marshal blocked_by: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO swarm_tasks (
  id, run_id, parent_id, role, subject, prompt, tool_allowlist, status,
  depth, attempts, blocked_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		task.ID, task.RunID, task.ParentID, task.Role, task.Subject, task.Prompt, string(allow),
		string(task.Status), task.Depth, string(blocked), formatTime(task.CreatedAt))
	if err != nil {
		return nil, fmt.Errorf("create swarm task: %w", err)
	}
	return task, nil
}

func (s *Store) MarkTaskRunning(ctx context.Context, id string) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `
UPDATE swarm_tasks
SET status = ?, attempts = attempts + 1, started_at = ?, last_error = ''
WHERE id = ?`,
		string(TaskRunning), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("mark swarm task running: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("swarm task %s not found", id)
	}
	return nil
}

func (s *Store) CompleteTask(ctx context.Context, id string, result agent.Result) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `
UPDATE swarm_tasks
SET status = ?, result = ?, tool_calls = ?, llm_calls = ?,
    tokens_prompt = ?, tokens_completion = ?, tokens_total = ?, elapsed_ms = ?,
    completed_at = ?, last_error = ''
WHERE id = ?`,
		string(TaskCompleted), result.Content, result.ToolCalls, result.LLMCalls,
		result.Tokens.PromptTokens, result.Tokens.CompletionTokens, result.Tokens.TotalTokens,
		result.Elapsed.Milliseconds(), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("complete swarm task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("swarm task %s not found", id)
	}
	return nil
}

func (s *Store) FailTask(ctx context.Context, id string, errText string) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `
UPDATE swarm_tasks
SET status = ?, completed_at = ?, last_error = ?
WHERE id = ?`,
		string(TaskFailed), formatTime(now), errText, id)
	if err != nil {
		return fmt.Errorf("fail swarm task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("swarm task %s not found", id)
	}
	return nil
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, run_id, parent_id, role, subject, prompt, tool_allowlist, status,
       depth, attempts, blocked_by, result, tool_calls, llm_calls,
       tokens_prompt, tokens_completion, tokens_total, elapsed_ms,
       created_at, started_at, completed_at, last_error
FROM swarm_tasks WHERE id = ?`, id)
	return scanTask(row)
}

func (s *Store) ListTasks(ctx context.Context, runID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, run_id, parent_id, role, subject, prompt, tool_allowlist, status,
       depth, attempts, blocked_by, result, tool_calls, llm_calls,
       tokens_prompt, tokens_completion, tokens_total, elapsed_ms,
       created_at, started_at, completed_at, last_error
FROM swarm_tasks
WHERE run_id = ?
ORDER BY created_at ASC, id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list swarm tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, rows.Err()
}

func randomID(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func parseNullTime(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	t, err := parseTime(raw.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(row rowScanner) (*Run, error) {
	var run Run
	var status string
	var createdAt, updatedAt string
	var completed sql.NullString
	if err := row.Scan(&run.ID, &run.Goal, &status, &run.CreatedBy, &createdAt, &updatedAt, &completed, &run.LastError); err != nil {
		return nil, err
	}
	run.Status = RunStatus(status)
	var err error
	run.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	run.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	run.CompletedAt, err = parseNullTime(completed)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func scanTask(row rowScanner) (*Task, error) {
	var task Task
	var status string
	var allowlist, blockedBy string
	var createdAt string
	var startedAt, completedAt sql.NullString
	if err := row.Scan(
		&task.ID, &task.RunID, &task.ParentID, &task.Role, &task.Subject, &task.Prompt,
		&allowlist, &status, &task.Depth, &task.Attempts, &blockedBy, &task.Result,
		&task.ToolCalls, &task.LLMCalls, &task.TokensPrompt, &task.TokensCompletion,
		&task.TokensTotal, &task.ElapsedMS, &createdAt, &startedAt,
		&completedAt, &task.LastError,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(allowlist), &task.ToolAllowlist); err != nil {
		return nil, fmt.Errorf("unmarshal tool_allowlist for %s: %w", task.ID, err)
	}
	if err := json.Unmarshal([]byte(blockedBy), &task.BlockedBy); err != nil {
		return nil, fmt.Errorf("unmarshal blocked_by for %s: %w", task.ID, err)
	}
	task.Status = TaskStatus(status)
	var err error
	task.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	task.StartedAt, err = parseNullTime(startedAt)
	if err != nil {
		return nil, err
	}
	task.CompletedAt, err = parseNullTime(completedAt)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func cleanList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
