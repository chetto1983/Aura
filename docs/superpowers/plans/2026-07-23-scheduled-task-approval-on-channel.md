# Scheduled-task Approval on the Origin Channel — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A scheduled task forced to `pending_approval` surfaces as a real `ask_user` HITL prompt on the channel it was scheduled from (Telegram Sì/No, WebUI in-thread) — never the cockpit board — and the backstop sweep deterministically ensures-or-mints that pause so it never depends on the model relaying.

**Architecture:** The per-tick sweep (a) ensures a wire-valid `scheduled_task_approval` `ask_user` pause exists on the task's origin conversation — reusing a model-relayed one, else minting one via a new `Runner.MintApprovalPause` that writes the synthetic assistant `ask_user` turn + pause in one `CommitPause` tx (wire-valid by construction); and (b) pushes a token-bound Sì/No prompt to Telegram via a new optional `ApprovalDeliverer` channel capability (WebUI needs no push — it pulls via `/api/approvals`). Accept/reject resolve the real pause through the existing `SubmitAnswer` → resume-hook bridge; the hook gains a decline→cancel branch.

**Tech Stack:** Go 1.25+, pgx/pgxpool, sqlc, telebot.v4, `internal/askuser`, `internal/runner`, `internal/cron`, `internal/channels`.

## Global Constraints

- **Import-cycle:** `internal/cron` imports NEITHER `internal/channels` NOR `internal/askuser`/`internal/agent/tools`. All new cron dependencies are consumer-side interfaces declared in `cron`. `internal/channels` does not import `cron`.
- **File size ≤600 LOC**; refactor-on-touch (dead-code removal + dupl-fold + comments-updated in the same commit).
- **No new migration.** Migration `0051` (`scheduler_tasks.approval_reminded_at`) already shipped. The dedup reads existing `paused_states.resume_context`.
- **Reject uses `ActionDecline`, NEVER `ActionCancel`.** `SubmitAnswer(ActionCancel)` short-circuits to `cancelConversation` (aborts the whole conversation) BEFORE `applyResumeHook` (`internal/runner/runner_resume.go:81`). Decline reaches the hook.
- **Bounded prompt text:** task **kind + short id (first 8 chars)** only. NEVER the payload.
- **Post-edit gate (every Go edit):** `go vet ./...`, `go build ./...`, `go test ./internal/<pkg>/`, `go test -race ./internal/<pkg>/`.
- **Coverage floor 85%** across the full tag matrix; daemon-gated code needs daemon-free unit tests.
- Env knob `AURA_SCHEDULER_APPROVAL_REMINDER_SEC` (default 3600, `<=0` disables) — already exists, unchanged.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/runner/runner_mint_approval.go` (create) | `Runner.MintApprovalPause` — synthesize assistant `ask_user` turn + pause via `CommitPause`, return token |
| `internal/runner/runner_mint_approval_test.go` (create) | unit + integration tests for the mint |
| `cmd/aura/serve_adapters.go` (modify) | `EnsureApprovalPause` adapter (dedup + mint); `CancelScheduledTaskInConversation`; resume-hook decline branch |
| `cmd/aura/serve_dispatch.go` (modify) | wire the two new cron deps |
| `internal/channels/deliver.go` (modify) | `ApprovalDeliverer` optional capability |
| `internal/channels/registry.go` (modify) | `Registry.DeliverApproval` fan-out |
| `internal/channels/telegram/deliver.go` (modify) | `Telegram.DeliverApproval` render+push |
| `internal/channels/telegram/scheduled_approval.go` (create) | `onScheduledApprovalCallback` + markup + parse |
| `internal/channels/telegram/bot_dispatch.go` (modify) | register the `aura_sched_approval` callback |
| `internal/cron/deliver_approval.go` (modify) | sweep = ensure-or-mint + `DeliverApproval`; new deps; drop text nudge |
| `internal/cron/dispatch.go` (modify) | `ApprovalPauseEnsurer` + `ApprovalChannel` dep fields |
| `internal/db/queries/scheduler_tasks.sql` (modify) | selection predicate: drop local-exclusion, require `origin_conversation_id` |
| `internal/db/sqlc/*` (regen) | `sqlc generate` |

---

## Task 1: Runner.MintApprovalPause (wire-valid operator-origin pause)

**Files:**
- Create: `internal/runner/runner_mint_approval.go`
- Test: `internal/runner/runner_mint_approval_test.go`

**Interfaces:**
- Consumes: `r.resumeCommitter.CommitPause(ctx, assistantTurn conversations.AppendTurnParams, pauses []askuser.InsertParams) error` (existing, `interfaces.go:121`); `r.pause.ListPending(ctx, convID) ([]askuser.Pending, error)`.
- Produces: `func (r *Runner) MintApprovalPause(ctx context.Context, convID, question string, resumeContext json.RawMessage) (token string, err error)`.

The method builds a synthetic assistant `ask_user` tool_call turn (so the later `RoleTool` answer is wire-valid) + one `askuser.InsertParams` pause, and commits both in one `CommitPause` tx. `token` and `toolCallID` are fresh UUIDs. The assistant turn's tool_call name is `ask_user`, arguments mirror the tool's shape `{"question":...,"kind":"approval","resume_context":...}`.

- [ ] **Step 1: Write the failing test** (`runner_mint_approval_test.go`)

```go
package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
)

func TestMintApprovalPause_WritesWireValidPauseAndReturnsToken(t *testing.T) {
	fakePause := newFakePauseStore() // reuse the package fake (fakes_test.go); add if absent
	fakeCommitter := newRecordingCommitter()
	r := &Runner{pause: fakePause, resumeCommitter: fakeCommitter}

	rc := json.RawMessage(`{"type":"scheduled_task_approval","task_id":"abc123"}`)
	token, err := r.MintApprovalPause(context.Background(), testConvID, "Approve scheduled task?", rc)
	if err != nil {
		t.Fatalf("MintApprovalPause: %v", err)
	}
	if token == "" {
		t.Fatal("want a non-empty token")
	}
	// Exactly one CommitPause call: one assistant ask_user turn + one pause whose
	// ToolCallID matches the assistant turn's tool_call id (wire-validity), and whose
	// ResumeContext round-trips the scheduled_task_approval payload.
	if len(fakeCommitter.pauses) != 1 {
		t.Fatalf("want 1 pause committed, got %d", len(fakeCommitter.pauses))
	}
	p := fakeCommitter.pauses[0]
	if p.Kind != "approval" || p.Token != token || p.ConversationID != testConvID {
		t.Fatalf("pause fields wrong: %+v", p)
	}
	var got struct{ Type, TaskID string }
	_ = json.Unmarshal(p.ResumeContext, &got)
	if got.Type != "scheduled_task_approval" || got.TaskID != "abc123" {
		t.Fatalf("resume_context not preserved: %s", p.ResumeContext)
	}
	if p.ToolCallID == "" || fakeCommitter.assistantTurn.ToolCallID != "" {
		// assistant turn carries the tool_call in its ToolCalls, pause.ToolCallID references it
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/runner/ -run TestMintApprovalPause -v`
Expected: FAIL — `MintApprovalPause` undefined (and any missing fake methods).

- [ ] **Step 3: Implement `MintApprovalPause`**

```go
package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// MintApprovalPause creates a WIRE-VALID operator-origin approval pause on convID: it
// writes the synthetic assistant ask_user tool_call turn AND the paused_states row in one
// CommitPause tx, so the pause is indistinguishable from a model-relayed ask_user pause and
// its later RoleTool answer (on accept/decline) references a real assistant tool_call
// (no orphan tool message). It is the deterministic host-side creator the scheduler
// approval-reminder sweep uses when the model never relayed the pause (Amendment #92
// revised). Kind is always "approval"; resumeContext is the caller's machine-readable
// payload (e.g. {"type":"scheduled_task_approval","task_id":...}). Returns the pause token.
func (r *Runner) MintApprovalPause(ctx context.Context, convID, question string, resumeContext json.RawMessage) (string, error) {
	token := uuid.NewString()
	toolCallID := uuid.NewString()
	args, err := json.Marshal(map[string]any{
		"question":       question,
		"kind":           "approval",
		"resume_context": resumeContext,
	})
	if err != nil {
		return "", fmt.Errorf("mint approval pause: marshal args: %w", err)
	}
	assistant := conversations.AppendTurnParams{
		ConversationID: convID,
		Role:           llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:   toolCallID,
			Type: "function",
			Function: llm.ToolCallFunction{Name: "ask_user", Arguments: string(args)},
		}},
	}
	pause := askuser.InsertParams{
		Token:          token,
		ConversationID: convID,
		Kind:           "approval",
		Question:       question,
		ResumeContext:  resumeContext,
		ToolCallID:     toolCallID,
	}
	if err := r.resumeCommitter.CommitPause(ctx, assistant, []askuser.InsertParams{pause}); err != nil {
		return "", fmt.Errorf("mint approval pause: %w", err)
	}
	return token, nil
}
```

> NOTE: verify `conversations.AppendTurnParams` has a `ToolCalls []llm.ToolCall` field and `llm.ToolCallFunction`'s exact field names before compiling; adapt to the real shapes (read `internal/conversations/store.go` + `internal/llm`). If `AppendTurnParams` carries tool calls differently (e.g. serialized), match how `CommitPause`/`persistPause` builds the assistant turn today.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/runner/ -run TestMintApprovalPause -v` → PASS

- [ ] **Step 5: Integration test (db_integration)** — mint against a real pool, then `ListPending` returns it, `GetByToken` round-trips ToolCallID, and a `SubmitAnswer(token, accept)` injects a wire-valid RoleTool (history has the matching assistant tool_call). Reuse `internal/runner` integration harness.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/runner_mint_approval.go internal/runner/runner_mint_approval_test.go
git commit -m "feat(runner): MintApprovalPause — wire-valid operator-origin approval pause"
```

---

## Task 2: Owner-scoped cancel + resume-hook decline branch + EnsureApprovalPause adapter

**Files:**
- Modify: `cmd/aura/serve_adapters.go`

**Interfaces:**
- Consumes: `Runner.MintApprovalPause` (Task 1); `askuser.Store.ListPending`; the existing `cronTaskStore.pool`.
- Produces:
  - `func (s *cronTaskStore) CancelScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error`
  - `EnsureApprovalPause(ctx context.Context, taskID, conversationID, kind string) (token string, err error)` on a new `approvalPauseEnsurer` adapter type wrapping `*askuser.Store` + `*runner.Runner`.
  - extended `newScheduledTaskResumeHook` handling `ActionDecline`.

- [ ] **Step 1: Failing test — cancel branch of the resume hook**

```go
func TestScheduledTaskResumeHook_DeclineCancelsTask(t *testing.T) {
	fake := &fakeScheduledApprover{} // extend with CancelScheduledTaskInConversation record
	hook := newScheduledTaskResumeHook(fake)
	pending := askuser.Pending{
		Kind: tools.KindApproval, ConversationID: "conv-1",
		ResumeContext: rawJSON(t, map[string]string{"type": "scheduled_task_approval", "task_id": "task-1"}),
	}
	if err := hook(context.Background(), pending, runner.ResponseInput{Action: askuser.ActionDecline}); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if fake.cancelledTask != "task-1" || fake.cancelledConv != "conv-1" {
		t.Fatalf("want cancel(task-1, conv-1), got (%q,%q)", fake.cancelledTask, fake.cancelledConv)
	}
}
```

- [ ] **Step 2: Run → FAIL** (`newScheduledTaskResumeHook` ignores decline; fake lacks the method).

- [ ] **Step 3: Implement**

Extend the `scheduledTaskApprover` seam and the hook:

```go
type scheduledTaskApprover interface {
	ApproveScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error
	CancelScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error
}

// in newScheduledTaskResumeHook, after decoding rc (type + task_id):
switch resp.Action {
case askuser.ActionAccept:
	return store.ApproveScheduledTaskInConversation(ctx, rc.TaskID, pending.ConversationID)
case askuser.ActionDecline:
	return store.CancelScheduledTaskInConversation(ctx, rc.TaskID, pending.ConversationID)
default:
	return nil // ActionCancel never reaches here (short-circuited in SubmitAnswer); fail-closed
}
```

Guard the early-return so only `Kind==approval` + `rc.Type=="scheduled_task_approval"` + non-empty `TaskID` proceed (mirror the existing accept-only guard, but let decline through).

`CancelScheduledTaskInConversation` (owner-scoped):

```go
func (s *cronTaskStore) CancelScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET status = 'cancelled', updated_at = now()
		WHERE id = $1 AND origin_conversation_id = $2 AND status = 'pending_approval'`, taskID, conversationID)
	if err != nil {
		return fmt.Errorf("cancel scheduled task %s: %w", taskID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled task %s is not awaiting approval in conversation %s", taskID, conversationID)
	}
	return nil
}
```

- [ ] **Step 4: EnsureApprovalPause adapter + test**

```go
// approvalPauseEnsurer satisfies cron.ApprovalPauseEnsurer: it reuses a model-relayed
// scheduled_task_approval pause on the origin conversation (dedup), else mints a wire-valid
// one via Runner.MintApprovalPause. Idempotent across sweep cadences.
type approvalPauseEnsurer struct {
	pauses *askuser.Store
	runner *runner.Runner
}

func (e approvalPauseEnsurer) EnsureApprovalPause(ctx context.Context, taskID, conversationID, kind string) (string, error) {
	pending, err := e.pauses.ListPending(ctx, conversationID)
	if err != nil {
		return "", fmt.Errorf("ensure approval pause: list pending: %w", err)
	}
	for _, p := range pending {
		if p.Kind != askuser.KindApproval /* or tools.KindApproval */ || len(p.ResumeContext) == 0 {
			continue
		}
		var rc struct{ Type, TaskID string }
		if json.Unmarshal(p.ResumeContext, &rc) == nil && rc.Type == "scheduled_task_approval" && rc.TaskID == taskID {
			return p.Token, nil // reuse — no duplicate pause
		}
	}
	question := fmt.Sprintf("Scheduled %s task %s needs your approval. Approve or reject it below.", kind, shortID(taskID))
	rc, _ := json.Marshal(map[string]string{"type": "scheduled_task_approval", "task_id": taskID})
	return e.runner.MintApprovalPause(ctx, conversationID, question, rc)
}
```

Test both branches (reuse existing pending; mint when none) with a fake pause store + a runner stub exposing `MintApprovalPause` behavior (or a small interface `approvalMinter` so the adapter takes an interface, not `*runner.Runner`, for unit-testability).

> Prefer declaring `approvalMinter interface { MintApprovalPause(...) (string,error) }` in cmd/aura so the adapter is unit-testable without a real Runner.

- [ ] **Step 5: Run** `go test ./cmd/aura/ -run 'ScheduledTaskResumeHook|EnsureApprovalPause' -v` → PASS; `go build ./...`.

- [ ] **Step 6: Commit**

```bash
git commit -am "feat(cmd/aura): owner-scoped scheduled-task cancel + decline resume branch + EnsureApprovalPause"
```

---

## Task 3: channels.ApprovalDeliverer capability + Registry fan-out

**Files:**
- Modify: `internal/channels/deliver.go`, `internal/channels/registry.go`

**Interfaces:**
- Produces: `type ApprovalDeliverer interface { DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (delivered bool, err error) }`; `func (r *Registry) DeliverApproval(ctx, identityID, token, taskID, kind string) (bool, error)`.

- [ ] **Step 1: Failing test** (`registry_test.go`) — a channel implementing `ApprovalDeliverer` receives the push; a channel not implementing it is skipped; tri-state (delivered / not-mine / owns-but-failed) mirrors `DeliverToIdentity`. Model the `fakeApprovalDeliverer` on the existing `fakeDeliverer`.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement** `ApprovalDeliverer` in `deliver.go` and `DeliverApproval` in `registry.go` — copy the `DeliverToIdentity` fan-out verbatim (sorted names, snapshot-under-lock, first-delivers-wins, `(false,err)` stops siblings), asserting `snap[n].(ApprovalDeliverer)`.

- [ ] **Step 4: Run → PASS;** `go vet ./... && go test -race ./internal/channels/`.

- [ ] **Step 5: Commit** `feat(channels): ApprovalDeliverer capability + Registry.DeliverApproval fan-out`.

---

## Task 4: Telegram DeliverApproval render/push + scheduled-approval callback

**Files:**
- Modify: `internal/channels/telegram/deliver.go` (add `DeliverApproval`)
- Create: `internal/channels/telegram/scheduled_approval.go` (markup, parse, `onScheduledApprovalCallback`)
- Modify: `internal/channels/telegram/bot_dispatch.go` (register `aura_sched_approval`)

**Interfaces:**
- Consumes: `t.deliverSender()`, `t.accountResolver()` (existing, `deliver.go`); `resumeRunner.SubmitAnswer` (existing hitl seam); `requireLinkedCallback` (existing auth gate).
- Produces: `func (t *Telegram) DeliverApproval(ctx, identityID, token, taskID, kind string) (bool, error)`; `scheduledApprovalMarkup(token) *tele.ReplyMarkup`; `onScheduledApprovalCallback(daemonCtx) tele.HandlerFunc`.

Constants: `const schedApprovalUnique = "aura_sched_approval"`; `callback_data = token + "|" + action` where action ∈ {`approve`,`reject`} (uuid 36 + 1 + ≤7 < 64).

- [ ] **Step 1: Failing test** — `DeliverApproval` resolves identity→account and sends text + a 2-button inline keyboard (Sì → `token|approve`, No → `token|reject`); tri-state matches `Deliver`. `onScheduledApprovalCallback` on `token|approve` calls `SubmitAnswer(token, {Action:accept})` and does NOT drive a continuation Turn; on `token|reject` calls `SubmitAnswer(token, {Action:decline})`; unlinked callback is rejected. Use the existing telegram test doubles (`deliverBot`, `deliverResolver`, a fake `resumeRunner`).

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.** `DeliverApproval` mirrors `Deliver` (same nil-guards + resolver + tri-state) but `Send`s with `&tele.SendOptions{ReplyMarkup: scheduledApprovalMarkup(token)}`. Markup:

```go
func scheduledApprovalMarkup(token string) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{{
		{Unique: schedApprovalUnique, Text: "Sì", Data: token + "|approve"},
		{Unique: schedApprovalUnique, Text: "No", Data: token + "|reject"},
	}}
	return mk
}
```

Callback handler: auth-gate, parse `token|action`, map `approve→askuser.ActionAccept` / `reject→askuser.ActionDecline`, `t.dispatch.hitl.runner.SubmitAnswer(ctx, token, runner.ResponseInput{Action})` (add a thin `hitl.resolveScheduled(ctx, token, action) error` that calls SubmitAnswer and ignores remaining/never calls `h.resume`), `c.Respond` an ack, disarm the keyboard. A resolve error → toast "already handled".

> The scheduled-approval callback must NOT drive a continuation Turn (unlike in-turn HITL): an operator-origin approval has no live turn; the resume hook fires inside `SubmitAnswer`.

- [ ] **Step 4: Register** in `bot_dispatch.go`: `bot.Handle(&tele.InlineButton{Unique: schedApprovalUnique}, t.onScheduledApprovalCallback(daemonCtx))`.

- [ ] **Step 5: `var _ channels.ApprovalDeliverer = (*Telegram)(nil)`** compile-time assertion in `deliver.go`.

- [ ] **Step 6: Run → PASS;** `go vet ./... && go test -race ./internal/channels/telegram/`.

- [ ] **Step 7: Commit** `feat(telegram): DeliverApproval render + aura_sched_approval callback (real ask_user resolve)`.

---

## Task 5: cron sweep — ensure-or-mint + push; drop text nudge; predicate change

**Files:**
- Modify: `internal/cron/dispatch.go` (deps), `internal/cron/deliver_approval.go` (sweep), `internal/db/queries/scheduler_tasks.sql` (predicate), then `sqlc generate`.

**Interfaces:**
- Produces on `DispatchDeps`: `ApprovalPauseEnsurer ApprovalPauseEnsurer`, `ApprovalChannel ApprovalChannel`.
- cron-side seams:
```go
type ApprovalPauseEnsurer interface {
	EnsureApprovalPause(ctx context.Context, taskID, conversationID, kind string) (token string, err error)
}
type ApprovalChannel interface {
	DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (delivered bool, err error)
}
```

- [ ] **Step 1: Failing test** (`deliver_approval_test.go`, daemon-free) — rewrite the suite: for each due row the sweep calls `EnsureApprovalPause(taskID, originConvID, kind)` then `DeliverApproval(identityID, token, taskID, kind)`, and stamps `MarkApprovalReminded` REGARDLESS of push outcome; a nil ensurer/channel → no-op; kill switch (`interval<=0`) → no query; a WebUI-origin row (identity `local`) still gets `EnsureApprovalPause` called even though `DeliverApproval` returns `(false,nil)`. Use fakes for the two new seams.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement the sweep body:**

```go
func (d *Dispatch) sweepApprovalReminders(ctx context.Context) error {
	interval := approvalReminderInterval()
	if interval <= 0 || d.deps.ApprovalReminderStore == nil ||
		d.deps.ApprovalPauseEnsurer == nil || d.deps.ApprovalChannel == nil {
		return nil
	}
	cutoff := time.Now().UTC().Add(-interval)
	rows, err := d.deps.ApprovalReminderStore.ListDueApprovalReminders(ctx, cutoff, approvalReminderSweepLimit)
	if err != nil {
		return err
	}
	for _, task := range rows {
		token, eerr := d.deps.ApprovalPauseEnsurer.EnsureApprovalPause(ctx, task.ID, task.OriginConversationID, task.Kind)
		if eerr != nil {
			slog.Warn("approval reminder: ensure pause failed (retry next cadence)", "task", task.ID, "err", eerr)
			// still stamp so a permanently-failing ensure cannot spam the tick
		} else {
			delivered, derr := d.deps.ApprovalChannel.DeliverApproval(ctx, task.IdentityID, token, task.ID, task.Kind)
			switch {
			case derr != nil:
				slog.Warn("approval reminder delivery failed (retry next cadence)", "task", task.ID, "err", derr)
			case !delivered:
				slog.Debug("approval reminder: no push channel owns identity (WebUI pulls)", "task", task.ID, "identity", task.IdentityID)
			}
		}
		if merr := d.deps.ApprovalReminderStore.MarkApprovalReminded(ctx, task.ID); merr != nil {
			slog.Warn("mark approval reminded", "task", task.ID, "err", merr)
		}
	}
	return nil
}
```

Delete `approvalReminderText` and the `ChannelDeliverer` usage for approvals. Confirm `Task` exposes `OriginConversationID` (it does, `store.go:94`).

- [ ] **Step 4: Predicate change** in `scheduler_tasks.sql` `ListDuePendingApprovalReminders`: replace `AND identity_id NOT IN ('', 'local')` with `AND origin_conversation_id IS NOT NULL AND origin_conversation_id <> ''`. Run `sqlc generate` (or the repo's `make sqlc`); update the daemon-free fake DBTX row set if the projected columns changed (they do not — same projection).

- [ ] **Step 5: Integration test (db_integration)** — a `pending_approval` task with a `local` identity but a non-null `origin_conversation_id` IS selected; a task with empty `origin_conversation_id` is NOT; throttle cutoff + stamp behavior unchanged.

- [ ] **Step 6: Run** `go test -race ./internal/cron/` (+ `-tags db_integration` with the stack up) → PASS.

- [ ] **Step 7: Commit** `feat(cron): approval sweep ensures+mints on-channel HITL pause; drop cockpit text nudge`.

---

## Task 6: Composition-root wiring

**Files:**
- Modify: `cmd/aura/serve_dispatch.go`

- [ ] **Step 1:** wire `DispatchDeps.ApprovalPauseEnsurer = approvalPauseEnsurer{pauses: <askuser.Store>, runner: <*runner.Runner>}` and `DispatchDeps.ApprovalChannel = <*channels.Registry>` (the same registry already passed as `ChannelDeliverer`). Nil-safe: if the runner or askuser store isn't wired in a given root, leave the ensurer nil → the sweep no-ops (kill-switch parity).
- [ ] **Step 2:** `go build ./... && go vet ./...`.
- [ ] **Step 3:** existing `cmd/aura` dispatch wiring tests still green; add one asserting both new deps are non-nil in `aura serve` boot.
- [ ] **Step 4: Commit** `feat(cmd/aura): wire approval-pause ensurer + approval channel into the scheduler dispatch`.

---

## Task 7: Full local verification + E2E

- [ ] **Step 1:** `make quality` (WSL) — vet + lint + deadcode + race + vuln + build.
- [ ] **Step 2:** `bash scripts/coverage_docker.sh` (stack up) — owned-surface ≥85%; add daemon-free unit tests for any new gated branch that dropped a package below floor.
- [ ] **Step 3: E2E with the real agent (DoD, score >9.8 — real scenario, not smoke):**
  1. Telegram: ask the agent to schedule an `agent_job`; approval arrives IN Telegram as real Sì/No; press Sì; task flips `active` and fires. Press No on a second one; task `cancelled`.
  2. WebUI chat: same; approval renders in-thread (not the governance board); approve; activation.
  3. Failure mode: delete the model-relayed pause (or use a model that doesn't relay); confirm the sweep mints + surfaces the approval on the origin channel within one lowered cadence.
  4. Drive via the real running stack per `docs/` E2E recipe + memory `cockpit-e2e-idempotency-key`; mine the agent-memory graph for recorded bugs.
- [ ] **Step 4:** quality snapshot re-attestation for touched CI-gate rows; PRD already amended.

---

## Loose ends (separate commits, before the phase-close push)

- [ ] Revert `compose.override.yaml` (5s tick / 10s cadence test knobs) — delete the file or reset to real cadence.
- [ ] calm-prism: commit regenerated baselines; revert the TEMP `--update-snapshots` step in `.github/workflows/ci.yml`.
- [ ] Re-check `git log origin/master..HEAD`; push the branch CI-green; verify all CI jobs pass (`gh run watch`).

## Self-review notes

- **Spec coverage:** ensure-or-mint (Task 1+2), Telegram push+callback (Task 4), WebUI pull (no code — already works via `/api/approvals`), predicate/WebUi-mint correction (Task 5 Step 4/5), reject→cancel via decline (Task 2), reminder-text removal (Task 5 Step 3), PRD amendment (done pre-plan). Covered.
- **`ActionCancel` trap:** encoded as a Global Constraint and in Task 2/Task 4 (reject = decline). Covered.
- **Type consistency:** `EnsureApprovalPause`/`DeliverApproval`/`MintApprovalPause` signatures identical across producer/consumer tasks. `scheduledTaskApprover` extended in Task 2 is the same interface `newScheduledTaskResumeHook` consumes.
- **Verify-before-compile flags:** the exact shapes of `conversations.AppendTurnParams` tool-call carriage (Task 1) and `askuser.KindApproval` vs `tools.KindApproval` constant location (Task 2) MUST be read from source during execution — noted inline.
