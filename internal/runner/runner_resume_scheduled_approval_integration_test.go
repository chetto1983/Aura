//go:build db_integration

// Integration cover for the Phase A consolidation contract: a REAL scheduled_task_approval
// pause, resolved through the REAL SubmitAnswer path against Postgres, must both (a) hand the
// caller a deterministic Approved/Rejected directive — never Continue, which is what made a
// channel re-drive a turn that no longer exists — and (b) leave the scheduler task in the state
// the ResumeHook transitioned it to. Unit tests pin classifyResolve in isolation; only this
// tier proves the directive and the durable side-effect agree.
package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schedulerApprovalHook mirrors cmd/aura's newScheduledTaskResumeHook: the owner-scoped
// transition keyed on the AUTHENTICATED pending.ConversationID (never a model-relayed id, WR-02),
// accept → active, decline → cancelled. Duplicated shape rather than imported because the
// production hook lives in the composition root (main), which internal/runner cannot import.
func schedulerApprovalHook(pool *pgxpool.Pool) ResumeHook {
	return func(ctx context.Context, pending askuser.Pending, resp ResponseInput) error {
		var rc struct {
			Type   string `json:"type"`
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(pending.ResumeContext, &rc); err != nil || rc.Type != "scheduled_task_approval" {
			return nil
		}
		status := "active"
		if resp.Action == askuser.ActionDecline {
			status = "cancelled"
		}
		_, err := pool.Exec(ctx, `
			UPDATE aura.scheduler_tasks
			SET status = $3, updated_at = now()
			WHERE id = $1 AND origin_conversation_id = $2 AND status = 'pending_approval'`,
			rc.TaskID, pending.ConversationID, status)
		return err
	}
}

// seedPendingApprovalTask inserts an agent_job awaiting approval on convID and registers its
// cleanup. Minimal columns only — the transition under test reads status + origin only.
func seedPendingApprovalTask(t *testing.T, pool *pgxpool.Pool, convID string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO aura.scheduler_tasks
			(id, kind, schedule_kind, every_minutes, status, origin_conversation_id)
		VALUES ($1, 'agent_job', 'every', 60, 'pending_approval', $2)`, id, convID); err != nil {
		t.Fatalf("seed pending_approval task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.scheduler_tasks WHERE id=$1", id)
	})
	return id
}

func taskStatus(t *testing.T, pool *pgxpool.Pool, taskID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		"SELECT status FROM aura.scheduler_tasks WHERE id=$1", taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	return status
}

// TestSubmitAnswer_ScheduledApproval_DirectiveMatchesTaskTransition_Integration is the Phase A
// end-to-end pin. Both channels now render this directive verbatim, so a Continue here would
// resurface as the 1.7 accept-loop on Telegram and a spurious re-drive in the cockpit.
func TestSubmitAnswer_ScheduledApproval_DirectiveMatchesTaskTransition_Integration(t *testing.T) {
	for _, tc := range []struct {
		name        string
		action      string
		wantOutcome ResolveOutcome
		wantStatus  string
	}{
		{"accept activates", askuser.ActionAccept, OutcomeApproved, "active"},
		{"decline cancels", askuser.ActionDecline, OutcomeRejected, "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := migratedRunnerPool(t)
			r, convStore, _ := newIntegrationRunnerWithResumeHook(
				t, pool, agenttest.NewFakeClient(), schedulerApprovalHook(pool))
			convID := newIntegrationConversation(t, pool, convStore)
			taskID := seedPendingApprovalTask(t, pool, convID)
			ctx := context.Background()

			// Mint through the REAL sweep path (MintApprovalPause), so the pause is byte-shaped
			// exactly like the one the backstop pushes to a channel.
			rc, err := json.Marshal(map[string]string{
				"type": "scheduled_task_approval", "task_id": taskID,
			})
			if err != nil {
				t.Fatalf("marshal resume context: %v", err)
			}
			token, err := r.MintApprovalPause(ctx, convID, "Attivo il task pianificato?", rc)
			if err != nil {
				t.Fatalf("mint approval pause: %v", err)
			}

			directive, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: tc.action})
			if err != nil {
				t.Fatalf("SubmitAnswer: %v", err)
			}
			if directive.Outcome != tc.wantOutcome {
				t.Errorf("outcome = %v, want %v", directive.Outcome, tc.wantOutcome)
			}
			if directive.Remaining != 0 {
				t.Errorf("remaining = %d, want 0 (the only pause was resolved)", directive.Remaining)
			}
			if got := taskStatus(t, pool, taskID); got != tc.wantStatus {
				t.Errorf("task status = %q, want %q", got, tc.wantStatus)
			}
		})
	}
}

// TestSubmitAnswer_ScheduledApproval_ForeignConversationDoesNotTransition_Integration keeps the
// WR-02 owner scoping visible at this tier: the hook keys on the authenticated pause
// conversation, so a task whose origin is a DIFFERENT conversation is untouched — while the
// directive still reports Approved, because the pause itself did resolve.
func TestSubmitAnswer_ScheduledApproval_ForeignConversationDoesNotTransition_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, convStore, _ := newIntegrationRunnerWithResumeHook(
		t, pool, agenttest.NewFakeClient(), schedulerApprovalHook(pool))
	originConv := newIntegrationConversation(t, pool, convStore)
	foreignConv := newIntegrationConversation(t, pool, convStore)
	taskID := seedPendingApprovalTask(t, pool, originConv)
	ctx := context.Background()

	rc, err := json.Marshal(map[string]string{"type": "scheduled_task_approval", "task_id": taskID})
	if err != nil {
		t.Fatalf("marshal resume context: %v", err)
	}
	token, err := r.MintApprovalPause(ctx, foreignConv, "Attivo il task pianificato?", rc)
	if err != nil {
		t.Fatalf("mint approval pause: %v", err)
	}

	if _, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept}); err != nil {
		t.Fatalf("SubmitAnswer: %v", err)
	}
	if got := taskStatus(t, pool, taskID); got != "pending_approval" {
		t.Errorf("task status = %q, want it untouched at pending_approval "+
			"(an approval raised in another conversation must not activate it)", got)
	}
}
