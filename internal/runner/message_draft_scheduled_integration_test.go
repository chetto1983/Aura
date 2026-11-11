//go:build db_integration

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// fakeMessageTasks records which reviewed message tasks the runner activated or cancelled.
type fakeMessageTasks struct {
	activated, cancelled []string
	activateErr          error
}

func (f *fakeMessageTasks) ActivateReviewedMessage(_ context.Context, ownerID, taskID string) error {
	if f.activateErr != nil {
		return f.activateErr
	}
	f.activated = append(f.activated, ownerID+"/"+taskID)
	return nil
}

func (f *fakeMessageTasks) CancelReviewedMessage(_ context.Context, ownerID, taskID string) error {
	f.cancelled = append(f.cancelled, ownerID+"/"+taskID)
	return nil
}

// trustedSendSpy is the mounted WhatsApp send; a scheduled review must never run it.
type trustedSendSpy struct{ executions int }

func (s *trustedSendSpy) Spec() tools.Spec {
	return tools.Spec{Name: "whatsapp__send_message", TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_message", Mutating: true}
}

func (s *trustedSendSpy) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	s.executions++
	return tools.ToolResult{Preview: "sent"}, nil
}

type scheduledReview struct {
	r      *Runner
	tasks  *fakeMessageTasks
	spy    *trustedSendSpy
	pool   *pgxpool.Pool
	conv   string
	taskID string
	draft  messagedrafts.Draft
}

// newScheduledReview parks one scheduled WhatsApp draft behind its review pause, as the agent's
// host review leaves it.
func newScheduledReview(t *testing.T) scheduledReview {
	t.Helper()
	pool := migratedRunnerPool(t)
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	r.messageDrafts = messagedrafts.NewStore(pool)
	tasks := &fakeMessageTasks{}
	r.messageTasks = tasks
	spy := &trustedSendSpy{}
	r.registry.Register(spy)
	conv := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()
	sendAt := time.Now().Add(time.Hour)
	task, err := cron.New(pool).CreateTask(ctx, cron.CreateTaskParams{
		Kind: cron.KindMessage, Spec: cron.ScheduleSpec{Kind: cron.KindAt, RunAt: sendAt, TZ: "UTC"},
		Payload: []byte(`{}`), NextRunAt: sendAt, NotifyRoute: string(cron.RouteNone),
		Status: "pending_approval", IdentityID: localIdentityID,
	})
	if err != nil {
		t.Fatalf("seed message task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, task.ID)
	})
	draft, err := r.messageDrafts.Create(ctx, messagedrafts.DraftInput{
		IdentityID: localIdentityID, ConversationID: conv, ToolCallID: "call-scheduled",
		Target:             messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"10000000000","message":"Original"}`),
		ExpiresAt:          sendAt.Add(tools.ScheduledMessageGrace),
		ScheduledTaskID:    task.ID, SendAt: sendAt,
	})
	if err != nil {
		t.Fatalf("create scheduled draft: %v", err)
	}
	t.Cleanup(func() {
		asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM aura.message_drafts WHERE id = $1`, draft.ID)
			return err
		})
	})
	if err := pauseStore.Insert(ctx, askuser.InsertParams{
		Token: uuid.NewString(), ConversationID: conv, Kind: "approval",
		Question: "Review this scheduled message before it is sent", Priority: 90, ToolCallID: "call-scheduled",
		ResumeContext: json.RawMessage(`{"type":"message_draft","draft_id":"` + draft.ID + `","task_id":"` + task.ID + `"}`),
	}); err != nil {
		t.Fatalf("persist review pause: %v", err)
	}
	return scheduledReview{r: r, tasks: tasks, spy: spy, pool: pool, conv: conv, taskID: task.ID, draft: draft}
}

func (s scheduledReview) answer(t *testing.T) string {
	t.Helper()
	var answer string
	asOwner(t, s.pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT content FROM aura.conversation_turns WHERE conversation_id = $1 AND role = 'tool'`, s.conv).Scan(&answer)
	})
	return answer
}

func TestApprovingAScheduledDraftActivatesItsTaskAndSendsNothing(t *testing.T) {
	s := newScheduledReview(t)
	res, err := s.r.ResolveMessageDraft(ownerCtx(), s.draft.ID, "send", json.RawMessage(`{"message":"Edited"}`))
	if err != nil || res.Status != messagedrafts.StatusApproved || res.Directive.Outcome != OutcomeContinue {
		t.Fatalf("resolve = %+v, %v", res, err)
	}
	if len(s.tasks.activated) != 1 || s.tasks.activated[0] != localIdentityID+"/"+s.taskID {
		t.Fatalf("activated %v", s.tasks.activated)
	}
	if s.spy.executions != 0 {
		t.Fatalf("the review sent the message %d times", s.spy.executions)
	}
	if got := s.answer(t); !strings.HasPrefix(got, "scheduled message approved: it will be sent at ") {
		t.Fatalf("answer = %q", got)
	}
}

func TestDecliningAScheduledDraftCancelsItsTask(t *testing.T) {
	s := newScheduledReview(t)
	res, err := s.r.ResolveMessageDraft(ownerCtx(), s.draft.ID, "decline", nil)
	if err != nil || res.Status != messagedrafts.StatusDeclined {
		t.Fatalf("resolve = %+v, %v", res, err)
	}
	if len(s.tasks.cancelled) != 1 || s.tasks.cancelled[0] != localIdentityID+"/"+s.taskID {
		t.Fatalf("cancelled %v", s.tasks.cancelled)
	}
	want := "the operator declined the scheduled message; nothing will be sent and task " + s.taskID + " is cancelled"
	if got := s.answer(t); got != want {
		t.Fatalf("answer = %q, want %q", got, want)
	}
}

func TestAFailedActivationIsRetriedFromTheCard(t *testing.T) {
	s := newScheduledReview(t)
	s.tasks.activateErr = errors.New("database unavailable")
	if _, err := s.r.ResolveMessageDraft(ownerCtx(), s.draft.ID, "send", nil); err == nil {
		t.Fatal("a failed activation was reported as approved")
	}
	if got, _ := s.r.messageDrafts.Get(ownerCtx(), localIdentityID, s.draft.ID); got.Status != messagedrafts.StatusApproved {
		t.Fatalf("draft = %q, want approved", got.Status)
	}
	s.tasks.activateErr = nil
	res, err := s.r.ResolveMessageDraft(ownerCtx(), s.draft.ID, "send", nil)
	if err != nil || res.Status != messagedrafts.StatusApproved || len(s.tasks.activated) != 1 {
		t.Fatalf("retry = %+v, %v; activated %v", res, err, s.tasks.activated)
	}
}
