//go:build db_integration

package conversations

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestDelete_CascadesPauseReferencedBySkillAudit proves a hard conversation delete
// can remove ephemeral paused_states while the append-only skill audit keeps the
// recorded pause token as historical evidence. The audit row must not keep a live
// FK back to the pause row; otherwise real threads with pending approvals 500 on
// DELETE /api/conversations/{id}.
func TestDelete_CascadesPauseReferencedBySkillAudit(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	tok := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.paused_states (token, conversation_id, kind, question, priority, tool_call_id)
		 VALUES ($1, $2, 'approval', 'approve skill?', 0, 'tc')`, tok, convID); err != nil {
		t.Fatalf("insert paused_state: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.skill_audit (
			actor_id, skill_name, action, content_hash, approval_source,
			paused_state_token, gate_recommended, gate_taken
		) VALUES (
			'user', 'delete-cascade-regression', 'activate', 'sha256:test',
			'ask_user', $1, true, true
		)`, tok); err != nil {
		t.Fatalf("insert skill_audit: %v", err)
	}

	if err := s.Delete(ctx, convID); err != nil {
		t.Fatalf("Delete with audit-referenced pause: %v", err)
	}

	var pausedCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM aura.paused_states WHERE token=$1", tok).Scan(&pausedCount); err != nil {
		t.Fatalf("count paused: %v", err)
	}
	if pausedCount != 0 {
		t.Fatalf("paused_state must cascade-delete, got %d rows", pausedCount)
	}

	var auditCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM aura.skill_audit WHERE paused_state_token=$1", tok).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("skill_audit must retain the historical token, got %d rows", auditCount)
	}
}
