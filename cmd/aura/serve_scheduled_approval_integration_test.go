//go:build db_integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
)

// The scheduled-task hook answers only a scheduled_task_approval; a message's review is its card.
func TestTheScheduledTaskHookNeverActivatesAMessageTask(t *testing.T) {
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		msg := "scheduled task hook integration needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)"
		if os.Getenv("CI") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	ctx := context.Background()
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	owner := uuid.Must(uuid.NewV7()).String()
	id := insertOwnedTask(t, pool, owner, "message")
	conversation := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET status = 'pending_approval', origin_conversation_id = $2 WHERE id = $1`, id, conversation); err != nil {
		t.Fatalf("park the task: %v", err)
	}
	hook := newScheduledTaskResumeHook(&cronTaskStore{pool: pool, store: cron.New(pool)})
	pending := askuser.Pending{Kind: tools.KindApproval, ConversationID: conversation,
		ResumeContext: json.RawMessage(`{"type":"scheduled_task_approval","task_id":"` + id + `"}`)}
	if err := hook(identityctx.WithIdentityID(ctx, owner), pending, runner.ResponseInput{Action: askuser.ActionAccept}); err == nil {
		t.Fatal("the hook activated a message task")
	}
	if status := schedulerTaskStatus(t, pool, id); status != "pending_approval" {
		t.Fatalf("message task is %s after the hook", status)
	}
}

// Decision 35: the hook reads the row and accepts only a question the host wrote for it: the
// in-turn one naming what will run, or the sweep's bounded one.
func TestTheHookRefusesAQuestionTheHostDidNotWrite(t *testing.T) {
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		msg := "scheduled task hook integration needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)"
		if os.Getenv("CI") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	ctx := context.Background()
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	owner := uuid.Must(uuid.NewV7()).String()
	hook := newScheduledTaskResumeHook(&cronTaskStore{pool: pool, store: cron.New(pool)})
	park := func() (id, conversation, named string) {
		id = insertOwnedTask(t, pool, owner, "agent_job")
		conversation = uuid.Must(uuid.NewV7()).String()
		var scheduleKind, tz string
		var next time.Time
		if err := pool.QueryRow(ctx, `UPDATE aura.scheduler_tasks SET status = 'pending_approval', origin_conversation_id = $2,
payload = '{"goal":"Riassumi la posta"}' WHERE id = $1 RETURNING schedule_kind, tz, next_run_at`, id, conversation).Scan(&scheduleKind, &tz, &next); err != nil {
			t.Fatalf("park the task: %v", err)
		}
		return id, conversation, tools.ApprovalQuestion("agent_job", id, scheduleKind, "", 0, tz, next, json.RawMessage(`{"goal":"Riassumi la posta"}`))
	}
	accept := func(id, conversation, question string) error {
		return hook(identityctx.WithIdentityID(ctx, owner), askuser.Pending{
			Kind: tools.KindApproval, ConversationID: conversation, Question: question,
			ResumeContext: json.RawMessage(`{"type":"scheduled_task_approval","task_id":"` + id + `"}`),
		}, runner.ResponseInput{Action: askuser.ActionAccept})
	}

	id, conversation, _ := park()
	if err := accept(id, conversation, "Confermi il promemoria?"); err == nil {
		t.Fatal("a foreign question approved the job")
	}
	if status := schedulerTaskStatus(t, pool, id); status != "pending_approval" {
		t.Fatalf("job is %s after a foreign question", status)
	}
	id, conversation, named := park()
	if err := accept(id, conversation, named); err != nil || schedulerTaskStatus(t, pool, id) != "active" {
		t.Fatalf("the named question: %v, status %s", err, schedulerTaskStatus(t, pool, id))
	}
	id, conversation, _ = park()
	if err := accept(id, conversation, mintedApprovalQuestion("agent_job", id)); err != nil || schedulerTaskStatus(t, pool, id) != "active" {
		t.Fatalf("the sweep's question: %v, status %s", err, schedulerTaskStatus(t, pool, id))
	}
}
