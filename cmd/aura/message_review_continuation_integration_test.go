//go:build db_integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestApprovedMessageSurvivesAutomaticContinuation(t *testing.T) {
	pool := mcpAuditMigratedPool(t)
	owner, thread := uuid.NewString(), uuid.NewString()
	ctx := identityctx.WithIdentityID(t.Context(), owner)
	if _, err := pool.Exec(ctx, "INSERT INTO aura.identities (id,name,kind) VALUES ($1,$2,'user')", owner, "message-guard-"+owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id=$1", owner); err != nil {
			t.Errorf("clean up test identity: %v", err)
		}
	})
	if err := conversations.InitEncoder(); err != nil {
		t.Fatal(err)
	}
	conv := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 1024})
	if _, err := conv.Create(ctx, conversations.CreateParams{ID: thread, IdentityID: owner, Model: "test", Metadata: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err := conv.SetTitleIfNull(ctx, thread, "Message review"); err != nil {
		t.Fatal(err)
	}
	pauses := askuser.New(pool)
	drafts := messagedrafts.NewStore(pool)
	tasks := cron.New(pool)
	sendAt := time.Now().Add(time.Hour)
	callID := "schedule-reviewed-message"
	task, err := tasks.CreateTask(ctx, cron.CreateTaskParams{
		Kind: cron.KindMessage, Spec: cron.ScheduleSpec{Kind: cron.KindAt, RunAt: sendAt, TZ: "UTC"},
		Payload: []byte("{}"), NextRunAt: sendAt, NotifyRoute: string(cron.RouteNone), Status: "pending_approval",
		IdentityID: owner, OriginConversationID: thread, OriginToolCallID: callID,
	})
	if err != nil {
		t.Fatal(err)
	}
	unrelated := insertOwnedTask(t, pool, owner, "reminder")
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM aura.scheduler_tasks WHERE id=$1", task.ID); err != nil {
			t.Errorf("clean up scheduled message: %v", err)
		}
	})
	draft, err := drafts.Create(ctx, messagedrafts.DraftInput{
		IdentityID: owner, ConversationID: thread, ToolCallID: callID,
		Target: messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, RegisteredToolName: "whatsapp__send_message",
		OriginalArgs: json.RawMessage(`{"recipient":"1000000000","message":"A test message"}`),
		ExpiresAt:    sendAt.Add(tools.ScheduledMessageGrace), ScheduledTaskID: task.ID, SendAt: sendAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	resumeContext, _ := json.Marshal(map[string]string{"type": "message_draft", "draft_id": draft.ID, "task_id": task.ID})
	assistant, _ := json.Marshal([]llm.ToolCall{agenttest.MakeToolCall(callID, "ask_user", `{"question":"Review the scheduled message"}`)})
	if err := conv.AppendTurn(ctx, conversations.AppendTurnParams{ConversationID: thread, Role: llm.RoleUser, Content: "Send this message in one hour."}); err != nil {
		t.Fatal(err)
	}
	if err := conv.AppendTurn(ctx, conversations.AppendTurnParams{ConversationID: thread, Role: llm.RoleAssistant, ToolCalls: assistant}); err != nil {
		t.Fatal(err)
	}
	if err := pauses.Insert(ctx, askuser.InsertParams{Token: uuid.NewString(), ConversationID: thread, ToolCallID: callID, Kind: "approval", Question: "Review the scheduled message", ResumeContext: resumeContext}); err != nil {
		t.Fatal(err)
	}
	inbox := steertest.New(steer.Config{})
	newRun := func(turns ...agenttest.FakeTurn) *runner.Runner {
		reg := tools.NewRegistry()
		reg.Register(&tools.TaskTool{Store: newCronTaskStore(pool, conv)})
		reg.Register(&tools.ToolSearch{Registry: reg})
		reg.Register(tools.TextResponse{})
		discover := agenttest.ToolCallTurn(agenttest.MakeToolCall(uuid.NewString(), "tool_search", `{"query":"select:task"}`))
		turns = append([]agenttest.FakeTurn{discover}, turns...)
		return runner.New(runner.Deps{
			Conv: conv, Pause: pauses, MessageDrafts: drafts, MessageTasks: tasks, Steer: inbox,
			ResumeCommitter: runner.NewPoolResumeCommitter(pool, conv, pauses),
			Registry:        reg, Identity: memIdentityStore{}, CacheMetrics: memCacheMetricStore{}, ToolInvocations: memToolInvocationStore{},
			Client: agenttest.NewFakeClient(turns...),
			LLM:    llm.Config{Model: "test", ContextWindow: 1000000, MaxOutputTokens: 32768, TotalTimeoutSec: 120},
		})
	}
	result, err := newRun().ResolveMessageDraft(ctx, draft.ID, "send", nil)
	if err != nil || result.Status != messagedrafts.StatusApproved || result.Directive.Outcome != runner.OutcomeContinue {
		t.Fatalf("approve = %+v, %v", result, err)
	}
	if status := schedulerTaskStatus(t, pool, task.ID); status != "active" {
		t.Fatalf("after approval: %s", status)
	}
	// The committed answer's transaction timestamp distinguishes it from a later
	// model response that reuses the same tool_call_id on a sibling branch.
	var reviewLeaf int
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT t.seq FROM aura.conversation_turns t JOIN aura.paused_states p
			ON t.conversation_id=p.conversation_id AND t.tool_call_id=p.tool_call_id
			WHERE t.conversation_id=$1 AND t.role='tool' AND p.tool_call_id=$2 AND t.created_at=p.resumed_at`,
			thread, callID).Scan(&reviewLeaf)
	}); err != nil {
		t.Fatalf("review answer and pause were not stamped together: %v", err)
	}
	catalog := func(count int) string {
		items := make([]assets.Asset, count)
		indexed := map[string]bool{}
		for i := range items {
			id := fmt.Sprintf("document-%d", i+1)
			items[i] = assets.Asset{ID: id, DocumentID: id, FileName: "manual.pdf", Summary: strings.Repeat("x", 100)}
			indexed[id] = true
		}
		return assets.WithContextBlocks("", assets.BuildKnowledgeCatalog(items, nil, func([]string) map[string]bool { return indexed }))
	}
	for _, tc := range []struct {
		name, content string
		attachments   []string
		protected     bool
	}{
		{"catalog only", catalog(1), nil, true},
		{"ordinary mention", "I read the knowledge catalog. Cancel the message.", nil, false},
		{"typed text after envelope", catalog(1) + "Cancel the message.", nil, false},
		{"altered header", strings.Replace(catalog(1), "call document_search", "don't call document_search", 1), nil, false},
		{"nonsequential document rows", strings.Replace(catalog(2), "- [2]", "- [1]", 1), nil, false},
		{"attached file", catalog(1), []string{uuid.NewString()}, false},
		{"spilled real text", strings.Repeat("This is my request. ", 100), nil, false},
		{"spilled catalog only", catalog(8), nil, true},
	} {
		seq, err := conv.AppendTurnSeq(ctx, conversations.AppendTurnParams{
			ConversationID: thread, Role: llm.RoleUser, Content: tc.content, AttachmentIDs: tc.attachments, ParentSeq: &reviewLeaf,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := conv.MessageTaskAcceptedOnBranch(ctx, owner, thread, task.ID, seq); err != nil || got != tc.protected {
			t.Fatalf("%s: protected=%t, want %t, err=%v", tc.name, got, tc.protected, err)
		}
	}
	head, err := conv.LatestTurnSeq(ctx, thread)
	if err != nil {
		t.Fatal(err)
	}
	if accepted, err := conv.MessageTaskAcceptedOnBranch(ctx, uuid.NewString(), thread, task.ID, head); err != nil || accepted {
		t.Fatalf("foreign owner reached review through catalog: %t, %v", accepted, err)
	}
	call := func(name, action, id string) agenttest.FakeTurn {
		return agenttest.ToolCallTurn(agenttest.MakeToolCall(name, "task", fmt.Sprintf(`{"action":%q,"task_id":%q}`, action, id)))
	}
	done := agenttest.TextChunks("stop", "Done.")
	if err := inbox.Push(thread, steer.SourceWorker, "cancel this scheduled message"); err != nil {
		t.Fatal(err)
	}
	resumed := newRun(call("list", "list", ""), call("pause", "pause", task.ID),
		call("cancel", "cancel", strings.ToUpper(task.ID)), call("run-early", "run_now", task.ID),
		call("resume", "resume", task.ID), call("other", "cancel", unrelated), done)
	interleaved := false
	for ev, err := range resumed.Turn(ctx, thread, nil) {
		if err != nil {
			t.Fatal(err)
		}
		if ev != nil && !interleaved {
			// A separate request writes a genuine user message on another branch
			// after this model loaded its history. This run never saw that message.
			if _, err := conv.ForkBranch(ctx, thread, reviewLeaf, llm.RoleUser, "Different branch"); err != nil {
				t.Fatal(err)
			}
			interleaved = true
		}
	}
	if !interleaved {
		t.Fatal("automatic continuation emitted no events")
	}
	got, err := drafts.Get(ctx, owner, draft.ID)
	if err != nil || got.Status != messagedrafts.StatusApproved || schedulerTaskStatus(t, pool, task.ID) != "active" {
		t.Fatalf("automatic continuation undid approval: draft=%s task=%s err=%v", got.Status, schedulerTaskStatus(t, pool, task.ID), err)
	}
	if status := schedulerTaskStatus(t, pool, unrelated); status != "cancelled" {
		t.Fatalf("unrelated task was blocked: %s", status)
	}
	assertToolAnswer := func(callID, want string) {
		t.Helper()
		var answer string
		err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT content FROM aura.conversation_turns WHERE conversation_id=$1 AND role='tool' AND tool_call_id=$2`, thread, callID).Scan(&answer)
		})
		if err != nil || !strings.Contains(answer, want) {
			t.Fatalf("tool response %s = %q, %v; want %q", callID, answer, err, want)
		}
	}
	assertToolAnswer("list", task.ID)
	for _, guarded := range []string{"pause", "cancel", "run-early", "resume"} {
		assertToolAnswer(guarded, "new user request")
	}
	// A real operator steer delivered to this run releases the guard. The tool
	// reaches the adapter, which applies its usual kind-specific pause policy.
	if err := inbox.Push(thread, "cockpit", "Pause the scheduled message"); err != nil {
		t.Fatal(err)
	}
	withSteer := newRun(call("check", "list", ""), call("user-steer", "pause", task.ID), done)
	if _, err := drainForPause(withSteer.TurnBranch(ctx, thread, reviewLeaf)); err != nil {
		t.Fatal(err)
	}
	assertToolAnswer("user-steer", "cannot be paused")
	if _, err := drainForPause(newRun(call("explicit", "cancel", task.ID), done).Turn(ctx, thread, new("Cancel the message I just scheduled."))); err != nil {
		t.Fatal(err)
	}
	got, err = drafts.Get(ctx, owner, draft.ID)
	if err != nil || got.Status != messagedrafts.StatusDeclined || schedulerTaskStatus(t, pool, task.ID) != "cancelled" {
		t.Fatalf("explicit cancellation: draft=%s task=%s err=%v", got.Status, schedulerTaskStatus(t, pool, task.ID), err)
	}
	// A fresh role=user boundary ends protection for subsequent empty continuations, too.
	latest, err := conv.LatestTurnSeq(ctx, thread)
	if err != nil {
		t.Fatal(err)
	}
	if accepted, err := conv.MessageTaskAcceptedOnBranch(ctx, owner, thread, task.ID, latest); err != nil || accepted {
		t.Fatalf("new user turn did not release guard: %t, %v", accepted, err)
	}
	if accepted, err := conv.MessageTaskAcceptedOnBranch(ctx, owner, thread, task.ID, reviewLeaf); err != nil || !accepted {
		t.Fatalf("the branch with the review lost its guard: %t, %v", accepted, err)
	}
	parent := reviewLeaf - 1
	copyLeaf, err := conv.AppendTurnSeq(ctx, conversations.AppendTurnParams{
		ConversationID: thread, Role: llm.RoleTool, ToolCallID: callID, Content: "scheduled message approved", ParentSeq: &parent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted, err := conv.MessageTaskAcceptedOnBranch(ctx, owner, thread, task.ID, copyLeaf); err != nil || accepted {
		t.Fatalf("reused tool call ID on sibling was mistaken for a review: %t, %v", accepted, err)
	}
}
