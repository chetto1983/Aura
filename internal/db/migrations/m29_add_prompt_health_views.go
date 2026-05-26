package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addPromptHealthViews(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
CREATE VIEW IF NOT EXISTS prompt_health_turns_24h AS
SELECT
  id,
  channel,
  chat_id,
  user_id,
  turn_index,
  role,
  created_at,
  llm_calls,
  tool_calls_count,
  elapsed_ms,
  tokens_in,
  tokens_out
FROM conversations
WHERE datetime(created_at) >= datetime('now', '-24 hours');

CREATE VIEW IF NOT EXISTS prompt_health_rollup_24h AS
SELECT
  COUNT(*) AS turn_rows,
  COUNT(DISTINCT chat_id) AS distinct_chats,
  SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END) AS assistant_turns,
  COALESCE(SUM(llm_calls), 0) AS llm_calls,
  COALESCE(SUM(tool_calls_count), 0) AS tool_calls,
  COALESCE(MAX(llm_calls), 0) AS max_llm_calls,
  COALESCE(MAX(tool_calls_count), 0) AS max_tool_calls,
  COALESCE(MAX(tokens_in), 0) AS max_prompt_tokens,
  COALESCE(SUM(tokens_in), 0) AS prompt_tokens,
  COALESCE(SUM(tokens_out), 0) AS completion_tokens,
  COALESCE(MAX(elapsed_ms), 0) AS max_elapsed_ms,
  COALESCE(SUM(elapsed_ms), 0) AS elapsed_ms,
  (SELECT COUNT(*)
     FROM conversation_compactions
    WHERE datetime(created_at) >= datetime('now', '-24 hours')) AS compactions,
  COALESCE((SELECT MAX(tokens_before)
     FROM conversation_compactions
    WHERE datetime(created_at) >= datetime('now', '-24 hours')), 0) AS max_compaction_tokens_before,
  COALESCE((SELECT MAX(tokens_after)
     FROM conversation_compactions
    WHERE datetime(created_at) >= datetime('now', '-24 hours')), 0) AS max_compaction_tokens_after
FROM prompt_health_turns_24h;

CREATE VIEW IF NOT EXISTS prompt_health_tool_outcomes_24h AS
SELECT
  tool_name,
  outcome,
  class,
  COUNT(*) AS count,
  COALESCE(MAX(elapsed_ms), 0) AS max_elapsed_ms,
  COALESCE(SUM(elapsed_ms), 0) AS elapsed_ms,
  MAX(ended_at) AS last_seen
FROM tool_attempts
WHERE datetime(ended_at) >= datetime('now', '-24 hours')
GROUP BY tool_name, outcome, class;

CREATE VIEW IF NOT EXISTS prompt_health_memory_kinds AS
SELECT
  kind,
  status,
  COUNT(*) AS count,
  MAX(updated_at) AS last_updated
FROM compact_memory_documents
GROUP BY kind, status;

CREATE VIEW IF NOT EXISTS prompt_health_run_events_24h AS
SELECT
  type,
  run_origin,
  COUNT(*) AS count,
  MAX(created_at) AS last_seen
FROM run_events
WHERE datetime(created_at) >= datetime('now', '-24 hours')
GROUP BY type, run_origin;
`); err != nil {
		return fmt.Errorf("migrations: add prompt health views: %w", err)
	}
	return nil
}
