//go:build db_integration

package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

type expireRig struct {
	r      *Runner
	pool   *pgxpool.Pool
	pauses *askuser.Store
	conv   string
}

func newExpireRig(t *testing.T) expireRig {
	t.Helper()
	pool := migratedRunnerPool(t)
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	r.messageDrafts = messagedrafts.NewStore(pool)
	return expireRig{r: r, pool: pool, pauses: pauseStore, conv: newIntegrationConversation(t, pool, convStore)}
}

// task inserts a task of kind awaiting approval in the rig's conversation, due at next.
func (x expireRig) task(t *testing.T, kind cron.TaskKind, next time.Time) string {
	t.Helper()
	task, err := cron.New(x.pool).CreateTask(context.Background(), cron.CreateTaskParams{
		Kind: kind, Spec: cron.ScheduleSpec{Kind: cron.KindAt, RunAt: next, TZ: "Europe/Rome"},
		Payload: []byte(`{"goal":"g"}`), NextRunAt: next, NotifyRoute: string(cron.RouteStdout),
		Status: "pending_approval", IdentityID: localIdentityID, OriginConversationID: x.conv,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = x.pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, task.ID)
	})
	return task.ID
}

func (x expireRig) pause(t *testing.T, resume string) string {
	t.Helper()
	token := uuid.NewString()
	if err := x.pauses.Insert(ownerCtx(), askuser.InsertParams{
		Token: token, ConversationID: x.conv, Kind: "approval", Question: "Approve?", Priority: 90,
		ToolCallID: "call-" + token[:8], ResumeContext: json.RawMessage(resume),
	}); err != nil {
		t.Fatalf("insert pause: %v", err)
	}
	return token
}

func (x expireRig) pending(t *testing.T) []askuser.Pending {
	t.Helper()
	pending, err := x.pauses.ListPending(ownerCtx(), x.conv)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	return pending
}

// scheduledDraft parks a due message draft behind its review pause.
func (x expireRig) scheduledDraft(t *testing.T, taskID string, sendAt time.Time) messagedrafts.Draft {
	t.Helper()
	draft, err := x.r.messageDrafts.Create(ownerCtx(), messagedrafts.DraftInput{
		IdentityID: localIdentityID, ConversationID: x.conv, ToolCallID: "call-" + taskID,
		Target:             messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"10000000000","message":"hello"}`),
		ExpiresAt:          time.Now().Add(time.Hour), ScheduledTaskID: taskID, SendAt: sendAt,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	asOwner(t, x.pool, localIdentityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE aura.message_drafts SET send_at = now() - interval '1 second' WHERE id = $1`, draft.ID)
		return err
	})
	t.Cleanup(func() {
		asOwner(t, x.pool, localIdentityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM aura.message_drafts WHERE id = $1`, draft.ID)
			return err
		})
	})
	return draft
}

func TestExpireApprovalPauseClosesTheTasksPause(t *testing.T) {
	t.Run("a scheduled-task approval", func(t *testing.T) {
		x := newExpireRig(t)
		id := x.task(t, cron.KindAgentJob, time.Now().Add(-time.Minute))
		x.pause(t, `{"type":"scheduled_task_approval","task_id":"`+id+`"}`)
		closed, err := x.r.ExpireApprovalPause(ownerCtx(), x.conv, id, "it lapsed")
		if err != nil || closed != cron.PauseClosedNow || len(x.pending(t)) != 0 {
			t.Fatalf("closed %v, %v, pending %d", closed, err, len(x.pending(t)))
		}
	})
	t.Run("a message review", func(t *testing.T) {
		x := newExpireRig(t)
		id := x.task(t, cron.KindMessage, time.Now().Add(time.Hour))
		draft := x.scheduledDraft(t, id, time.Now().Add(time.Hour))
		x.pause(t, `{"type":"message_draft","draft_id":"`+draft.ID+`","task_id":"`+id+`"}`)
		if closed, err := x.r.ExpireApprovalPause(ownerCtx(), x.conv, id, "it lapsed"); err != nil || closed != cron.PauseClosedNow {
			t.Fatalf("closed %v, %v", closed, err)
		}
		if got, _ := x.r.messageDrafts.Get(ownerCtx(), localIdentityID, draft.ID); got.Status != messagedrafts.StatusExpired || len(x.pending(t)) != 0 {
			t.Fatalf("draft %s, pending %d", got.Status, len(x.pending(t)))
		}
	})
	t.Run("no pause, then an accepted one", func(t *testing.T) {
		x := newExpireRig(t)
		id := x.task(t, cron.KindAgentJob, time.Now().Add(-time.Minute))
		if closed, err := x.r.ExpireApprovalPause(ownerCtx(), x.conv, id, "it lapsed"); err != nil || closed != cron.PauseNone {
			t.Fatalf("no pause: %v, %v", closed, err)
		}
		token := x.pause(t, `{"type":"scheduled_task_approval","task_id":"`+id+`"}`)
		if err := x.pauses.MarkResumed(ownerCtx(), token, askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: "yes"}); err != nil {
			t.Fatalf("answer: %v", err)
		}
		if closed, err := x.r.ExpireApprovalPause(ownerCtx(), x.conv, id, "it lapsed"); err != nil || closed != cron.PauseAnswered {
			t.Fatalf("accepted: %v, %v", closed, err)
		}
	})
}

// The lapsed pass's closer and messages, over the runner and the stores.
type runnerCloser struct{ r *Runner }

func (c runnerCloser) CloseLapsedApproval(ctx context.Context, task cron.Task, answer string) (cron.PauseClosed, error) {
	return c.r.ExpireApprovalPause(ctx, task.OriginConversationID, task.ID, answer)
}

type storeMessages struct {
	drafts *messagedrafts.Store
	tasks  *cron.Store
}

func (m storeMessages) ApprovedForTask(ctx context.Context, owner, task string) (bool, error) {
	return m.drafts.ApprovedForTask(ctx, owner, task)
}
func (m storeMessages) ActivateReviewedMessage(ctx context.Context, owner, task string) error {
	return m.tasks.ActivateReviewedMessage(ctx, owner, task)
}
func (m storeMessages) ExpireForTask(ctx context.Context, owner, task string) error {
	return m.drafts.ExpireForTask(ctx, owner, task)
}

// recorderFunc is the conversation recorder a test reads back.
type recorderFunc func(conversationID, text string)

func (f recorderFunc) AppendAssistantTurn(_ context.Context, conversationID, text string) error {
	f(conversationID, text)
	return nil
}

// stdoutNotifier delivers every notice, as the stdout route does.
type stdoutNotifier struct{}

func (stdoutNotifier) Notify(context.Context, cron.NotifyRoute, string) error { return nil }
func (stdoutNotifier) Destination(context.Context, cron.NotifyRoute) (string, error) {
	return "", nil
}

func TestAnUnapprovedMessageIsReportedAndSettles(t *testing.T) {
	x := newExpireRig(t)
	id := x.task(t, cron.KindMessage, time.Now().Add(-time.Minute))
	draft := x.scheduledDraft(t, id, time.Now().Add(time.Hour))
	x.pause(t, `{"type":"message_draft","draft_id":"`+draft.ID+`","task_id":"`+id+`"}`)
	store := cron.New(x.pool)
	var notices []string
	d := cron.NewDispatch(nil, cron.DispatchDeps{
		Store: store, Notifier: stdoutNotifier{},
		ConversationRecorder: recorderFunc(func(_, text string) { notices = append(notices, text) }),
		LapsedPauseCloser:    runnerCloser{r: x.r},
		LapsedMessages:       storeMessages{drafts: x.r.messageDrafts, tasks: store},
	})
	if err := d.ExpireLapsedApprovals(ownerCtx()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	task, err := store.GetTask(context.Background(), id)
	if err == nil && task.Status != "cancelled" {
		t.Fatalf("task = %s, want cancelled or settled away", task.Status)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "was not approved in time and did not run") {
		t.Fatalf("notices = %v", notices)
	}
	got, err := x.r.messageDrafts.Get(ownerCtx(), localIdentityID, draft.ID)
	if err != nil || got.Status != messagedrafts.StatusExpired {
		t.Fatalf("draft = %v, %v; want expired and kept", got.Status, err)
	}
	if len(x.pending(t)) != 0 {
		t.Fatal("the review pause is still pending")
	}
	listed, err := x.r.ListMessageDrafts(ownerCtx(), x.conv)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listed drafts = %d, %v", len(listed), err)
	}
}
