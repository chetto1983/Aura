//go:build db_integration

package runner

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5"
)

func TestTurnWritesTheDecisionToTheRealUserRow(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	r, convStore, _ := newIntegrationRunner(t, pool, client)
	r.turnDecisions = convStore
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := WithReasoningOverride(ownerCtx(), llm.ReasoningEffortHigh)

	const text = "riscrivi questa funzione in Go con i test"
	if _, err := drain(r.Turn(ctx, convID, new(text))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	var (
		rows                                  int
		role, content, requested, source, key string
		effort                                *string
	)
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ownerCtx(),
			`SELECT count(*) FROM aura.conversation_turns WHERE conversation_id = $1 AND reasoning_effort_source IS NOT NULL`,
			convID).Scan(&rows); err != nil {
			return err
		}
		return tx.QueryRow(ownerCtx(),
			`SELECT role, content, reasoning_effort, reasoning_effort_requested, reasoning_effort_source, recall_context_key
			   FROM aura.conversation_turns WHERE conversation_id = $1 AND reasoning_effort_source IS NOT NULL`,
			convID).Scan(&role, &content, &effort, &requested, &source, &key)
	})
	// The integration runner's route is not a reasoning target: the composer's choice is
	// recorded as requested, and nothing was sent, so the applied effort stays NULL.
	if rows != 1 || role != "user" || content != text || effort != nil || requested != "high" ||
		source != agent.EffortSourceUser || key != agent.TurnContextKey(nil, "") {
		t.Fatalf("rows %d: role %q content %q effort %v requested %q source %q key %q", rows, role, content, effort, requested, source, key)
	}
}
