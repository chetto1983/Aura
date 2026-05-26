package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

func TestGeneratePromptHealthReportUsesMetricsAndRedactsPayloads(t *testing.T) {
	t.Setenv("AURA_PROBE_DB_DOCKER", "0")
	db := testutil.OpenTestDB(t, migrations.Run)
	env := &Env{DB: db, DBPath: ":memory:"}
	now := time.Now().UTC()
	ts := now.Format(time.RFC3339)

	_, err := db.ExecContext(context.Background(), `
INSERT INTO runs (id, channel, status, started_at, updated_at)
VALUES ('run-probe-health', 'web', 'completed', ?, ?);

INSERT INTO conversations
  (channel, chat_id, user_id, turn_index, role, content, llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out, created_at)
VALUES
  ('web', 4242, 7, 1, 'user', 'SECRET_CONVERSATION_PAYLOAD', 0, 0, 0, 0, 0, ?),
  ('web', 4242, 7, 2, 'assistant', 'SECRET_ASSISTANT_PAYLOAD', 2, 3, 2100, 12000, 800, ?),
  ('telegram', 5151, 7, 3, 'assistant', 'SECOND_SECRET_PAYLOAD', 1, 0, 900, 4000, 300, ?);

INSERT INTO conversation_compactions
  (run_id, chat_id, turn_index, tokens_before, tokens_after, threshold_tokens, focus_preview, elapsed_ms, created_at)
VALUES
  ('run-probe-health', 4242, 2, 50000, 18000, 35000, 'SECRET_COMPACTION_PAYLOAD', 90, ?);

INSERT INTO tool_attempts
  (id, run_id, tool_name, tool_kind, outcome, class, reason, args_hash, arg_keys_json, error_redacted, elapsed_ms, started_at, ended_at)
VALUES
  ('attempt-probe-health', 'run-probe-health', 'search', 'native', 'recoverable', 'validation', 'SECRET_TOOL_REASON', 'hash-only', '["action"]', 'redacted', 120, ?, ?);

INSERT INTO compact_memory_documents
  (id, kind, title, body, status, updated_at)
VALUES
  ('mem-probe-health', 'user_memory', 'SECRET_MEMORY_TITLE', 'SECRET_MEMORY_BODY', 'active', ?);
`, ts, ts, ts, ts, ts, ts, ts, ts, ts)
	if err != nil {
		t.Fatalf("seed prompt health: %v", err)
	}

	report, err := generatePromptHealthReport(env, now)
	if err != nil {
		t.Fatalf("generatePromptHealthReport: %v", err)
	}
	if !report.DB.ViewsPresent {
		t.Fatal("ViewsPresent = false, want true after migrations.Run")
	}
	if report.DB.Rollup.AssistantTurns != 2 {
		t.Fatalf("AssistantTurns = %d, want 2", report.DB.Rollup.AssistantTurns)
	}
	if report.DB.Percentiles.PromptTokensP50 != 4000 || report.DB.Percentiles.PromptTokensP95 != 12000 {
		t.Fatalf("percentiles = %+v, want p50=4000 p95=12000", report.DB.Percentiles)
	}
	if report.DB.Rollup.Compactions != 1 {
		t.Fatalf("Compactions = %d, want 1", report.DB.Rollup.Compactions)
	}
	if len(report.DB.ToolOutcomes) == 0 || report.DB.ToolOutcomes[0].ToolName != "search" {
		t.Fatalf("ToolOutcomes = %+v, want search bucket", report.DB.ToolOutcomes)
	}
	if len(report.DB.Outliers) == 0 || report.DB.Outliers[0].ChatHash == "" {
		t.Fatalf("Outliers missing redacted chat hash: %+v", report.DB.Outliers)
	}
	for _, row := range report.DB.Outliers {
		if row.ChatHash == "4242" || row.ChatHash == "5151" {
			t.Fatalf("outlier ChatHash leaked raw chat id: %+v", row)
		}
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	body := string(raw)
	for _, forbidden := range []string{
		"SECRET_CONVERSATION_PAYLOAD",
		"SECRET_ASSISTANT_PAYLOAD",
		"SECOND_SECRET_PAYLOAD",
		"SECRET_COMPACTION_PAYLOAD",
		"SECRET_TOOL_REASON",
		"SECRET_MEMORY_TITLE",
		"SECRET_MEMORY_BODY",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("prompt health report leaked %q:\n%s", forbidden, body)
		}
	}
}
