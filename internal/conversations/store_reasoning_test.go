//go:build db_integration

// Integration proofs for the amendment #91 (fix-plan 1.12) display-only reasoning
// persistence. Requires a running Postgres with the migrations applied through 0050
// (reasoning columns):
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via (against a THROWAWAY DB — db_integration mutations are NOT isolated per-run,
// so NEVER the live `aura` DB):
//
//	go test -tags db_integration -race -run TestTurnReasoning ./internal/conversations -count=1
//
// Reuses the store_test.go harness (envOrSkip / migratedPool / newStore /
// newConversation / localID) + the package goleak TestMain. No-skip-as-green:
// envOrSkip t.Fatals under $CI when the DSN is unset.
package conversations

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestTurnReasoning_PersistedForDisplayNeverInHistory is the amendment #91 pinned
// regression (point 2) plus the display round-trip (points 1/3): a persisted
// reasoning-bearing assistant turn (a) surfaces through the display-only
// ListTurnReasoning read with its duration, (b) stores NULL (not empty string) for
// reasoning-less answer turns, and (c) NEVER leaks into the LoadHistory rebuild —
// the marshaled messages array (exactly what wireRequest embeds as `messages`)
// carries zero reasoning content.
func TestTurnReasoning_PersistedForDisplayNeverInHistory(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	const cot = "PERSISTED-COT-MARKER: the user wants a haiku; count syllables first"
	var askCall llm.ToolCall
	askCall.ID = "call-1"
	askCall.Type = "function"
	askCall.Function.Name = "web_search"
	askCall.Function.Arguments = `{"query":"haiku rules"}`
	toolCalls, err := json.Marshal([]llm.ToolCall{askCall})
	if err != nil {
		t.Fatalf("marshal tool_calls: %v", err)
	}

	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "write a haiku"},
		// A tool-call assistant turn: NOT answer-shaped, must not appear in the
		// display read (it can never carry reasoning).
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, ToolCalls: toolCalls},
		{ConversationID: convID, Seq: 4, Role: llm.RoleTool, Content: "5-7-5", ToolCallID: "call-1"},
		{ConversationID: convID, Seq: 5, Role: llm.RoleAssistant, Content: "autumn moonlight…",
			Reasoning: cot, ReasoningDurationMS: 1500},
		{ConversationID: convID, Seq: 6, Role: llm.RoleUser, Content: "another"},
		{ConversationID: convID, Seq: 7, Role: llm.RoleAssistant, Content: "winter stillness…"}, // no reasoning → NULL
	}
	for _, p := range turns {
		if err := s.AppendTurn(ctx, p); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", p.Seq, err)
		}
	}

	// (a) Display-only read: one row per answer-shaped assistant turn, seq order,
	// reasoning + duration on the bearing row, empty on the NULL row.
	rows, err := s.ListTurnReasoning(ctx, convID)
	if err != nil {
		t.Fatalf("ListTurnReasoning: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListTurnReasoning returned %d rows, want 2 (answer-shaped turns only): %+v", len(rows), rows)
	}
	if rows[0].Seq != 5 || rows[0].Reasoning != cot || rows[0].DurationMS != 1500 {
		t.Errorf("row[0] = %+v, want seq 5 with the CoT + 1500ms", rows[0])
	}
	if rows[1].Seq != 7 || rows[1].Reasoning != "" || rows[1].DurationMS != 0 {
		t.Errorf("row[1] = %+v, want seq 7 with no reasoning (NULL degrade)", rows[1])
	}

	// (b) NULL mapping: "" / 0 persist as SQL NULL, never empty-string/0 values.
	var reasoningNull, durationNull bool
	if err := pool.QueryRow(ctx,
		"SELECT reasoning IS NULL, reasoning_duration_ms IS NULL FROM aura.conversation_turns WHERE conversation_id = $1 AND seq = 7",
		convID,
	).Scan(&reasoningNull, &durationNull); err != nil {
		t.Fatalf("null-mapping probe: %v", err)
	}
	if !reasoningNull || !durationNull {
		t.Errorf("seq 7 reasoning/duration NULL = %v/%v, want true/true", reasoningNull, durationNull)
	}

	// (c) Amendment #91 point 2 PIN: the history rebuild is structurally reasoning-
	// free. Marshal the rebuilt messages — the exact `messages` array wireRequest
	// embeds — and assert zero reasoning content, by marker AND by key.
	hist, err := s.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	raw, err := json.Marshal(hist)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if strings.Contains(string(raw), cot) {
		t.Fatalf("LoadHistory leaked the persisted CoT into the wire messages: %s", raw)
	}
	if strings.Contains(strings.ToLower(string(raw)), "reasoning") {
		t.Fatalf("LoadHistory wire messages carry a reasoning key: %s", raw)
	}
}
