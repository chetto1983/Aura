//go:build db_integration

package runner

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/prompt"
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

// The background teacher's label reaches the real row through the caller-identity
// transaction, so the worker's detached context must still carry the turn's identity: under
// RLS a context without it would match no row, and the write would succeed doing nothing.
func TestTeacherLabelLandsOnTheRealUserRow(t *testing.T) {
	pool := migratedRunnerPool(t)
	teacher := &routerTeacher{answer: `{"tier":"none"}`,
		turns: agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "Ecco il riassunto.")))}
	r, convStore, _ := newIntegrationRunner(t, pool, teacher)
	r.runtime.Replace(r.runtime.Snapshot().Client, mandatoryRoute())
	r.classifier = prompt.NewReasoningClassifier(&agenttest.UniformEmbedder{})
	r.turnDecisions = convStore
	convID := newIntegrationConversation(t, pool, convStore)

	if _, err := drain(r.Turn(ownerCtx(), convID, new("riassumi il rapporto trimestrale"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if err := r.Stop(ownerCtx(), convID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	var effort, requested, source string
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ownerCtx(),
			`SELECT reasoning_effort, reasoning_effort_requested, reasoning_effort_source
			   FROM aura.conversation_turns WHERE conversation_id = $1 AND role = 'user'`,
			convID).Scan(&effort, &requested, &source)
	})
	// Seeds decided high and sent it. The label stores the teacher's none before the clamp,
	// as the column defines it; on this route a reuse would send it as low.
	if effort != "high" || requested != "none" || source != agent.EffortSourceTeacher {
		t.Fatalf("user row: effort %q requested %q source %q; want high sent, labelled none by the teacher", effort, requested, source)
	}
}
