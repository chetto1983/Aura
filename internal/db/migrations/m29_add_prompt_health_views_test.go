package migrations

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestPromptHealthViewsExposeRedactedOperationalMetrics(t *testing.T) {
	db := openTestDB(t)
	if err := Run(context.Background(), db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, view := range []string{
		"prompt_health_turns_24h",
		"prompt_health_rollup_24h",
		"prompt_health_tool_outcomes_24h",
		"prompt_health_memory_kinds",
		"prompt_health_run_events_24h",
	} {
		assertViewExists(t, db, view)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(context.Background(), `
INSERT INTO runs (id, channel, status, started_at, updated_at)
VALUES ('run-prompt-health', 'web', 'completed', ?, ?);

INSERT INTO conversations
  (channel, chat_id, user_id, turn_index, role, content, llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at)
VALUES
  ('web', 101, 201, 1, 'user', 'PRIVATE USER CONTENT', 0, 0, 0, 0, 0, ?),
  ('web', 101, 201, 2, 'assistant', 'PRIVATE ASSISTANT CONTENT', 2, 3, 1400, 42000, 900, ?);

INSERT INTO conversation_compactions
  (run_id, chat_id, turn_index, iteration, messages_before, messages_after, tokens_before, tokens_after, threshold_tokens, cumulative_tokens, focus_preview, elapsed_ms, created_at)
VALUES
  ('run-prompt-health', 101, 2, 1, 20, 8, 50000, 18000, 40000, 50000, 'PRIVATE FOCUS', 120, ?);

INSERT INTO tool_attempts
  (id, run_id, tool_name, tool_kind, outcome, class, reason, args_hash, arg_keys_json, error_redacted, elapsed_ms, started_at, ended_at)
VALUES
  ('attempt-prompt-health', 'run-prompt-health', 'search', 'native', 'recoverable', 'validation', 'bad field', 'hash-only', '["action"]', 'redacted', 80, ?, ?);

INSERT INTO compact_memory_documents
  (id, kind, title, body, status, updated_at)
VALUES
  ('mem-prompt-health', 'user_memory', 'private title', 'PRIVATE MEMORY BODY', 'active', ?);

INSERT INTO run_events
  (id, run_id, seq, type, actor_id, payload_json, created_at)
VALUES
  ('event-prompt-health', 'run-prompt-health', 1, 'tool_start', 'actor:test', '{"tool":"search"}', ?);
`, now, now, now, now, now, now, now, now, now, now)
	if err != nil {
		t.Fatalf("seed prompt health rows: %v", err)
	}

	assertScalar(t, db, `SELECT assistant_turns FROM prompt_health_rollup_24h`, int64(1))
	assertScalar(t, db, `SELECT max_prompt_tokens FROM prompt_health_rollup_24h`, int64(42000))
	assertScalar(t, db, `SELECT compactions FROM prompt_health_rollup_24h`, int64(1))
	assertScalar(t, db, `SELECT count FROM prompt_health_tool_outcomes_24h WHERE tool_name = 'search' AND outcome = 'recoverable'`, int64(1))
	assertScalar(t, db, `SELECT count FROM prompt_health_memory_kinds WHERE kind = 'user_memory' AND status = 'active'`, int64(1))
	assertScalar(t, db, `SELECT count FROM prompt_health_run_events_24h WHERE type = 'tool_start'`, int64(1))
}

func assertViewExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'view' AND name = ?`, name).Scan(&got); err != nil {
		t.Fatalf("view %s missing: %v", name, err)
	}
}
