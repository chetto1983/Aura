# Channel-Approval Consolidation (Phase A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lift the "after a pause resolves, continue the turn or render a deterministic outcome" decision into ONE runner-owned `ResolveDirective`, collapse Telegram's two approval callbacks into one, enrich the native Telegram approval, and have the WebUI resolve endpoint consume the same directive.

**Architecture:** `runner.SubmitAnswer` returns a typed `ResolveDirective` computed by a single `classifyResolve` (pure function of the pause's nature). Every channel renders the directive in its own idiom and forwards the action only — the Go/TS boundary means rendering can't be shared, but the *decision* now is. `scheduled_task_approval` → deterministic outcome (its ResumeHook is the whole intent); everything else → continue the turn.

**Tech Stack:** Go 1.26 (runner, telegram/telebot.v4, agui, cron), TypeScript/React + Vitest (cockpit `web/`), Postgres (no schema change).

**Design source:** [docs/superpowers/specs/2026-07-24-channel-approval-consolidation-phase-a-design.md](../specs/2026-07-24-channel-approval-consolidation-phase-a-design.md). **Governing index:** [docs/audit/consolidated-fix-plan-2026-07-20.md](../../audit/consolidated-fix-plan-2026-07-20.md) (succeeds Wave 1.7).

## Global Constraints

- **No new infra:** no migration, no new `AURA_*` env var, no public/VPN endpoint. Verbatim from spec §9.
- **File LOC cap ≤600** (CLAUDE.md): split on touch (`runner_resume_directive.go`, `hitl_render.go`) if a touched file crosses it.
- **All prompt/model-facing text in English; all user-facing channel text localized** (Telegram → Italian; cockpit → react-i18next en+it). The runner emits a semantic outcome CODE, never user prose.
- **Coverage floor ≥85%** on the `db_integration neo4j_integration` matrix (verify the stricter Skills-gate `db_integration`-only number); daemon-free unit tests for any DB/daemon-gated code.
- **Post-edit gate (every Go edit):** `go vet ./...` + `go build ./...` + `go test ./internal/<pkg>/` + `go test -race ./internal/<pkg>/`. Run Go in WSL; web gates (vitest/tsc/eslint/prettier) on Windows.
- **Commit discipline:** atomic, imperative subject + why-body + `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. Stage explicit paths only (a parallel `internal/adaptive/` spike is live — never `git add -A`).
- **Parallel-spike caveat:** the tree may not `vet` clean due to the adaptive spike's WIP. If a pre-commit `vet` hook blocks a change that is green in isolation, STOP and ask the operator before `--no-verify`.

---

## File Structure

**Runner (`internal/runner/`)**
- `runner_resume_directive.go` *(new)* — `ResolveOutcome`, `ResolveDirective`, `classifyResolve`, and the single scheduled-approval detector (folds today's duplicated detection).
- `runner_resume.go` *(modify)* — `SubmitAnswer` returns `ResolveDirective`; `cancelConversation` returns it; `scheduledApprovalAnswer` calls the shared detector.
- `runner.go` *(modify, only if needed)* — no API beyond `SubmitAnswer`; `SubmitAnswers` unchanged.

**Telegram (`internal/channels/telegram/`)**
- `scheduled_approval.go` + `scheduled_approval_test.go` *(DELETE)* — the whole `aura_sappr` duplicate.
- `bot_dispatch.go` *(modify)* — remove the `schedApprovalUnique` registration; `onCallback` becomes the sole approval callback and dispatches on the directive.
- `hitl.go` *(modify)* — `handleCallbackResult` returns the directive; unified `callbackData` builder with the framing-inclusive 64-byte guard; richer markup.
- `hitl_render.go` *(new, if hitl.go crosses 600)* — `approvalMarkup`/`choiceMarkup`/`detailsMarkup` + the formatted-message builder.
- `deliver.go` *(modify)* — `DeliverApproval` renders through the unified builder; drop `scheduledApprovalText`/`scheduledApprovalMarkup`.

**AG-UI (`internal/agui/`)**
- `approvals_api.go` *(modify)* — resolve returns `200 + {outcome, remaining}` via `SubmitAnswer` (was `204` via `SubmitAnswers`).

**Cockpit (`web/`)**
- `web/src/approvals/useApprovals.ts` *(modify)* — `postResolve` returns the directive; `ResolveDirective` type.
- `web/src/approvals/InlineApprovalCard.tsx` *(modify)* — map server outcome → `CardState`; re-drive only on `continue`.
- `web/src/approvals/__tests__/InlineApprovalCard.test.tsx` + `ThreadApprovalCards.test.tsx` *(modify)* — assert outcome-driven state.

**CLI (`cmd/aura/`)**
- `chat_repl.go` *(modify)* — read `directive.Remaining`; optionally render the outcome in the REPL.

---

## Task 1: Runner `ResolveDirective` + `classifyResolve` (pure, no signature change yet)

**Files:**
- Create: `internal/runner/runner_resume_directive.go`
- Test: `internal/runner/runner_resume_directive_test.go`

**Interfaces:**
- Produces: `type ResolveOutcome int`; consts `OutcomeContinue, OutcomeApproved, OutcomeRejected, OutcomePending, OutcomeTerminated`; `type ResolveDirective struct { Outcome ResolveOutcome; Remaining int }`; `func classifyResolve(pending askuser.Pending, action string, remaining int) ResolveDirective`; `func isScheduledTaskApproval(pending askuser.Pending) bool`.

- [ ] **Step 1: Write the failing test** — `internal/runner/runner_resume_directive_test.go`

```go
package runner

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
)

func schedApprovalPending(taskID string) askuser.Pending {
	rc, _ := json.Marshal(map[string]string{"type": "scheduled_task_approval", "task_id": taskID})
	return askuser.Pending{Kind: tools.KindApproval, ResumeContext: rc}
}

func TestClassifyResolve(t *testing.T) {
	ordinary := askuser.Pending{Kind: "clarification"}
	gateway := askuser.Pending{Kind: tools.KindApproval} // shell/gateway approval: no scheduled resume ctx
	sched := schedApprovalPending("11112222-3333-4444-5555-666677778888")

	cases := []struct {
		name      string
		pending   askuser.Pending
		action    string
		remaining int
		want      ResolveDirective
	}{
		{"cancel wins", sched, askuser.ActionCancel, 0, ResolveDirective{OutcomeTerminated, 0}},
		{"remaining wins over continue", ordinary, askuser.ActionAccept, 2, ResolveDirective{OutcomePending, 2}},
		{"sched accept -> approved", sched, askuser.ActionAccept, 0, ResolveDirective{OutcomeApproved, 0}},
		{"sched decline -> rejected", sched, askuser.ActionDecline, 0, ResolveDirective{OutcomeRejected, 0}},
		{"ordinary accept -> continue", ordinary, askuser.ActionAccept, 0, ResolveDirective{OutcomeContinue, 0}},
		{"gateway approval -> continue", gateway, askuser.ActionAccept, 0, ResolveDirective{OutcomeContinue, 0}},
		{"sched with remaining still pending", sched, askuser.ActionAccept, 1, ResolveDirective{OutcomePending, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyResolve(c.pending, c.action, c.remaining); got != c.want {
				t.Fatalf("classifyResolve = %+v, want %+v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — WSL: `go test ./internal/runner/ -run TestClassifyResolve` → FAIL (`classifyResolve` undefined).

- [ ] **Step 3: Write the implementation** — `internal/runner/runner_resume_directive.go`

```go
package runner

import (
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
)

// ResolveOutcome is the runner-owned decision about what a channel does after a pause
// resolves. It is a semantic CODE (never user prose): the runner is not locale-aware, so
// each channel maps Approved/Rejected to its own localized confirmation.
type ResolveOutcome int

const (
	OutcomeContinue   ResolveOutcome = iota // in-session: the channel drives its own continuation render
	OutcomeApproved                         // scheduled gate approved: render the deterministic "approved" confirmation
	OutcomeRejected                         // scheduled gate rejected: render the deterministic "rejected" confirmation
	OutcomePending                          // remaining>0: render the next FIFO pause, nothing else
	OutcomeTerminated                       // cancel: the runner already auto-resolved; nothing to render
)

// ResolveDirective is the single output of resolving one pause. Both Telegram and the
// cockpit render it; neither re-derives the continue-vs-outcome decision (codex/ADK
// "one resolver, transports only render it").
type ResolveDirective struct {
	Outcome   ResolveOutcome
	Remaining int
}

// classifyResolve is the ONE continuation/outcome decision. Pure function of the pause's
// nature + the action + the remaining count — no channel/transport input.
//
//	cancel                              -> Terminated (runner already aborted the turn)
//	remaining>0                         -> Pending (more FIFO pauses to answer first)
//	scheduled_task_approval, accept     -> Approved (ResumeHook activated the task; nothing more to do)
//	scheduled_task_approval, decline    -> Rejected (ResumeHook cancelled the task)
//	otherwise (clarification/choice/gateway approval) -> Continue (the model has more work this turn)
func classifyResolve(pending askuser.Pending, action string, remaining int) ResolveDirective {
	if action == askuser.ActionCancel {
		return ResolveDirective{Outcome: OutcomeTerminated}
	}
	if remaining > 0 {
		return ResolveDirective{Outcome: OutcomePending, Remaining: remaining}
	}
	if isScheduledTaskApproval(pending) {
		if action == askuser.ActionDecline {
			return ResolveDirective{Outcome: OutcomeRejected}
		}
		return ResolveDirective{Outcome: OutcomeApproved}
	}
	return ResolveDirective{Outcome: OutcomeContinue}
}

// isScheduledTaskApproval reports whether a pause is the operator governance gate whose
// ResumeHook (activate/cancel) IS the whole intent — the single detector that supersedes
// the ad-hoc decode duplicated in scheduledApprovalAnswer + the deleted telegram helpers.
func isScheduledTaskApproval(pending askuser.Pending) bool {
	if pending.Kind != tools.KindApproval || len(pending.ResumeContext) == 0 {
		return false
	}
	var rc struct {
		Type   string `json:"type"`
		TaskID string `json:"task_id"`
	}
	return json.Unmarshal(pending.ResumeContext, &rc) == nil &&
		rc.Type == "scheduled_task_approval" && rc.TaskID != ""
}
```

- [ ] **Step 4: Refactor `scheduledApprovalAnswer` to use the shared detector** — in `internal/runner/runner_resume.go`, replace the inline decode in `scheduledApprovalAnswer` (lines ~188-203) so it early-returns `if !isScheduledTaskApproval(pending) { return "" }` and then decodes only the `TaskID` for the short id. Keep the English marker text unchanged.

- [ ] **Step 5: Run tests to verify they pass** — WSL: `go test ./internal/runner/ -run 'TestClassifyResolve|Scheduled'` → PASS. Then `go build ./... && go vet ./...` (expect only the parallel-spike vet noise, not runner).

- [ ] **Step 6: Commit**

```bash
git add internal/runner/runner_resume_directive.go internal/runner/runner_resume_directive_test.go internal/runner/runner_resume.go
git commit -m "feat(runner): add ResolveDirective + classifyResolve continuation decision

One pure function owns the continue-vs-deterministic-outcome decision; folds the
duplicated scheduled_task_approval detection into isScheduledTaskApproval.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: `SubmitAnswer` returns `ResolveDirective` (mechanical caller migration, build-green)

**Files:**
- Modify: `internal/runner/runner_resume.go` (`SubmitAnswer`, `cancelConversation`)
- Modify callers: `internal/channels/telegram/hitl.go`, `internal/agui/approvals_api.go`, `cmd/aura/chat_repl.go`
- Modify tests: the runner/cmd test callers listed in the design blast-radius

**Interfaces:**
- Consumes: `classifyResolve` (Task 1).
- Produces: `func (r *Runner) SubmitAnswer(ctx, token string, resp ResponseInput) (ResolveDirective, error)`. `SubmitAnswers` KEEPS `(int, error)`.

> Behavior is IDENTICAL after this task — every caller just reads `directive.Remaining` where it read the old int. The new Outcome is *consumed* in Tasks 4/6/7. This task only keeps the build green under the new signature.

- [ ] **Step 1: Change `SubmitAnswer`** — `internal/runner/runner_resume.go`

```go
func (r *Runner) SubmitAnswer(ctx context.Context, token string, resp ResponseInput) (_ ResolveDirective, err error) {
	ctx, end := resumeBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	pending, err := r.pause.GetByToken(ctx, token)
	if err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer: %w", err)
	}
	if resp.Action == askuser.ActionCancel {
		return r.cancelConversation(ctx, pending.ConversationID)
	}
	claim := ResumeClaim{Token: token, Answer: toResumeAnswer(resp), Turn: r.answerTurn(pending, resp)}
	if err := r.resumeCommitter.CommitResume(ctx, claim); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer: %w", err)
	}
	if err := r.applyResumeHook(ctx, pending, resp); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer: %w", err)
	}
	remaining, err := r.remainingPending(ctx, pending.ConversationID)
	if err != nil {
		return ResolveDirective{}, err
	}
	return classifyResolve(pending, resp.Action, remaining), nil
}
```

- [ ] **Step 2: Change `cancelConversation` to return a directive** — same file:

```go
func (r *Runner) cancelConversation(ctx context.Context, conversationID string) (ResolveDirective, error) {
	if err := r.injectCancelledAnswers(ctx, conversationID); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer (cancel): %w", err)
	}
	if err := r.pause.AutoResolveForConversation(ctx, conversationID); err != nil {
		return ResolveDirective{}, fmt.Errorf("submit answer (cancel): %w", err)
	}
	remaining, err := r.remainingPending(ctx, conversationID)
	if err != nil {
		return ResolveDirective{}, err
	}
	return ResolveDirective{Outcome: OutcomeTerminated, Remaining: remaining}, nil
}
```

- [ ] **Step 3: Migrate `hitl.go` call sites (compile-only for now)** — in `internal/channels/telegram/hitl.go`, the three `SubmitAnswer` calls (lines ~130, 193, 227) change from `remaining, err :=` to `directive, err :=` and read `directive.Remaining` where `remaining` was used (Task 4 rewrites the dispatch to use `directive.Outcome`). For line 227 (`resolveScheduled`) leave a temporary `_ = directive` — Task 3 deletes this method.

- [ ] **Step 4: Migrate `approvals_api.go` (compile-only)** — `internal/agui/approvals_api.go:157`: this becomes `SubmitAnswer` in Task 6; for now change `s.run.SubmitAnswers(...)` to keep compiling by reading the tuple as before (it still returns `(int, error)` — unchanged). No edit needed here yet.

- [ ] **Step 5: Migrate `chat_repl.go`** — `cmd/aura/chat_repl.go:166`: `remaining, err := d.run.SubmitAnswer(...)` → `directive, err := d.run.SubmitAnswer(...)` and use `directive.Remaining`.

- [ ] **Step 6: Migrate runner + cmd tests** — mechanically update every `remaining, err := r.SubmitAnswer(` / `_, err := r.SubmitAnswer(` in `internal/runner/*_test.go` and `cmd/aura/*_test.go` (see design §10 list) to read `directive.Remaining`. `SubmitAnswers` test callers are UNCHANGED.

- [ ] **Step 7: Build + full runner tests (incl. race)** — WSL:

```
go build ./... 2>&1 | grep -v adaptive   # spike noise filtered
go test -race ./internal/runner/ ./cmd/aura/ -run 'Resume|SubmitAnswer|Pause|Cancel'
```
Expected: PASS (behavior unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/runner/runner_resume.go internal/channels/telegram/hitl.go cmd/aura/chat_repl.go internal/runner/*_test.go cmd/aura/*_test.go
git commit -m "refactor(runner): SubmitAnswer returns ResolveDirective (behavior unchanged)

Mechanical signature migration; callers read directive.Remaining. SubmitAnswers
(batch, AG-UI run-resume) keeps returning remaining. Outcome consumed next.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Collapse the two Telegram callbacks into one (delete `aura_sappr`)

**Files:**
- Delete: `internal/channels/telegram/scheduled_approval.go`, `internal/channels/telegram/scheduled_approval_test.go`
- Modify: `internal/channels/telegram/bot_dispatch.go` (registration), `internal/channels/telegram/deliver.go` (`DeliverApproval`), `internal/channels/telegram/hitl.go` (unified `callbackData` guard; delete `resolveScheduled`)

**Interfaces:**
- Consumes: the single `onCallback` handler + `hitl.handleCallbackResult`.
- Produces: `DeliverApproval` renders via `approvalMarkup(token)` (the same builder the in-turn relay uses).

- [ ] **Step 1: Write the failing test** — the sweep push must render through the unified `aura_hitl` builder, not `aura_sappr`. In `internal/channels/telegram/deliver_test.go` add:

```go
func TestDeliverApprovalUsesUnifiedCallback(t *testing.T) {
	rec := &recordingSender{} // existing test double that captures Send options
	tg := newDeliverTestTelegram(t, rec)
	ok, err := tg.DeliverApproval(context.Background(), testIdentity, testToken, testTaskID, "agent_job")
	if err != nil || !ok {
		t.Fatalf("DeliverApproval = (%v,%v)", ok, err)
	}
	btn := rec.lastMarkup().InlineKeyboard[0][0]
	if btn.Unique != callbackUnique { // aura_hitl, NOT aura_sappr
		t.Fatalf("approval button Unique = %q, want %q", btn.Unique, callbackUnique)
	}
	// on-wire budget: \f + Unique + | + Data must fit 64
	if wire := len("\f") + len(btn.Unique) + len("|") + len(btn.Data); wire > 64 {
		t.Fatalf("on-wire callback_data = %d bytes > 64", wire)
	}
}
```
(Reuse the existing `deliver_test.go` doubles; mirror `scheduled_approval_test.go`'s recorder setup, which you are about to delete.)

- [ ] **Step 2: Run to verify it fails** — WSL: `go test ./internal/channels/telegram/ -run TestDeliverApprovalUsesUnifiedCallback` → FAIL (still `schedApprovalUnique`).

- [ ] **Step 3: Add the framing-inclusive guard to the unified builder** — in `internal/channels/telegram/hitl.go` replace the Data-only guard so it counts the telebot frame (the correct 1.7 fix, lifted from the deleted `scheduled_approval.go`):

```go
// telebot frames the wire callback_data as "\f<Unique>|<Data>" and Telegram caps THAT at
// 64 bytes — so the budget is checked WITH the framing, not on Data alone.
const callbackTelebotFrameBytes = len("\f") + len(callbackUnique) + len(callbackSep)

func callbackData(token, action, value string) string {
	data := strings.Join([]string{token, action, value}, callbackSep)
	if callbackTelebotFrameBytes+len(data) > callbackDataMaxBytes {
		panic("telegram callback_data exceeds Telegram's 64-byte on-wire cap")
	}
	return data
}
```
(Delete the old `callbackDataMaxBytes` = 64 comment about "Data alone".)

- [ ] **Step 4: Point `DeliverApproval` at the unified builder** — in `internal/channels/telegram/deliver.go` line ~92-93, replace the `scheduledApprovalText`/`scheduledApprovalMarkup` render with the unified message + `approvalMarkup`. Use the pause's own bounded question if available; the sweep passes only `taskID, kind`, so build the bounded header here:

```go
text := approvalPromptText(taskID, kind) // formatted header helper (Task 5); bounded, secret-safe
if _, err := sender.Send(tele.ChatID(acct.TelegramUserID), text,
	&tele.SendOptions{ReplyMarkup: approvalMarkup(token)}); err != nil {
	return false, fmt.Errorf("telegram deliver approval: send to %d: %w", acct.TelegramUserID, err)
}
```
(For this task, `approvalPromptText` may be a thin `"🔔 Task "+kind+" "+shortID+" richiede la tua approvazione."`; Task 5 enriches it. `shortID` via a single shared short-id helper — reuse `hitl`'s.)

- [ ] **Step 5: Delete the duplicate + its registration + `resolveScheduled`**
  - Delete files `scheduled_approval.go` + `scheduled_approval_test.go`.
  - `internal/channels/telegram/bot_dispatch.go`: remove line ~101 `bot.Handle(&tele.InlineButton{Unique: schedApprovalUnique}, t.onScheduledApprovalCallback(daemonCtx))`.
  - `internal/channels/telegram/hitl.go`: delete `resolveScheduled` (lines ~221-229) — no caller remains.

- [ ] **Step 6: Run tests + build** — WSL:

```
go build ./internal/channels/telegram/ 2>&1 | grep -v adaptive
go test -race ./internal/channels/telegram/ -run 'Deliver|Callback|Hitl|Approval'
```
Expected: PASS; no reference to `schedApprovalUnique`/`onScheduledApprovalCallback` remains (`grep -rn schedApprovalUnique internal/` empty).

- [ ] **Step 7: Commit**

```bash
git add internal/channels/telegram/
git rm internal/channels/telegram/scheduled_approval.go internal/channels/telegram/scheduled_approval_test.go
git commit -m "refactor(telegram): collapse aura_sappr into the single aura_hitl callback

Delete the scheduled-approval native duplicate; the sweep push now renders
through the unified builder with the framing-inclusive 64-byte on-wire guard.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Telegram consumes the directive Outcome

**Files:**
- Modify: `internal/channels/telegram/hitl.go` (`handleCallbackResult`, `callbackOutcome`), `internal/channels/telegram/bot_dispatch.go` (`onCallback`)
- Test: `internal/channels/telegram/hitl_test.go`, `internal/channels/telegram/bot_dispatch_hitl_test.go`

**Interfaces:**
- Consumes: `runner.ResolveDirective`, `runner.OutcomeApproved/Rejected/Continue/Pending/Terminated`.
- Produces: `callbackOutcome` gains `outcome runner.ResolveOutcome`; `onCallback` edits the message to the localized deterministic confirmation on Approved/Rejected, disarms on Terminated, renders next on Pending, drives `startTurn` on Continue.

- [ ] **Step 1: Write the failing test** — a scheduled-approval accept must leave a deterministic confirmation, NOT drive a continuation turn. In `internal/channels/telegram/hitl_test.go`:

```go
func TestHandleCallbackScheduledApprovalRendersOutcomeNoResume(t *testing.T) {
	fake := &fakeResumeRunner{directive: runner.ResolveDirective{Outcome: runner.OutcomeApproved}}
	var resumed bool
	h := newHitl(fake, func(context.Context, string) { resumed = true })
	out := h.handleCallbackResult(context.Background(),
		callbackData(schedToken, askuser.ActionAccept, "yes"), convID, nil)
	if resumed {
		t.Fatal("scheduled approval must NOT drive a continuation turn")
	}
	if out.outcome != runner.OutcomeApproved {
		t.Fatalf("outcome = %v, want Approved", out.outcome)
	}
}
```
(Extend the existing `fakeResumeRunner` to return a `ResolveDirective` from `SubmitAnswer`.)

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/channels/telegram/ -run TestHandleCallbackScheduledApprovalRendersOutcomeNoResume` → FAIL.

- [ ] **Step 3: Thread the directive through `handleCallbackResult`** — `internal/channels/telegram/hitl.go`. `resumeRunner.SubmitAnswer` now returns `(runner.ResolveDirective, error)`. Rewrite the tail of `handleCallbackResult`:

```go
type callbackOutcome struct {
	action    string
	content   string
	submitted bool
	resumed   bool
	outcome   runner.ResolveOutcome
}

// ... after a successful SubmitAnswer returning `directive`:
	out.submitted = true
	out.outcome = directive.Outcome
	if afterSubmit != nil {
		afterSubmit(out)
	}
	if directive.Outcome == runner.OutcomeContinue {
		if h.resume != nil {
			h.resume(ctx, convID)
			out.resumed = true
		}
	}
	return out
```
Continuation fires ONLY on `OutcomeContinue`. Approved/Rejected/Pending/Terminated do not resume (the caller renders them). Apply the same directive read to `submit` (used by ForceReply) so a scheduled approval answered by text also renders the outcome.

- [ ] **Step 4: Render the outcome in `onCallback`** — `internal/channels/telegram/bot_dispatch.go`, after `handleCallbackResult`:

```go
switch out.outcome {
case runner.OutcomeApproved, runner.OutcomeRejected:
	t.editApprovalOutcome(c.Bot(), cb.Message, out.outcome) // localized IT confirmation + drop keyboard
case runner.OutcomePending:
	t.promptPendingPause(daemonCtx, t.sender(c), chatID)
default: // Continue (already resumed) / Terminated (auto-resolved) → nothing extra
}
```

And the localized renderer (channel-owned Italian, replacing the deleted `scheduledApprovalResolvedText`):

```go
func (t *Telegram) editApprovalOutcome(bot tele.API, msg *tele.Message, outcome runner.ResolveOutcome) {
	if bot == nil || msg == nil {
		return
	}
	text := "✅ Task pianificato approvato — è attivo e partirà all'orario previsto."
	if outcome == runner.OutcomeRejected {
		text = "❌ Task pianificato rifiutato — annullato."
	}
	if _, err := bot.Edit(msg, text, &tele.ReplyMarkup{}); err != nil {
		slog.Warn("telegram approval: outcome edit failed", "err", err)
		t.disarmCallbackKeyboard(bot, msg)
	}
}
```

- [ ] **Step 5: Run tests (incl. race)** — WSL: `go test -race ./internal/channels/telegram/ -run 'Callback|Hitl|Approval|Outcome'` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/channels/telegram/hitl.go internal/channels/telegram/bot_dispatch.go internal/channels/telegram/hitl_test.go internal/channels/telegram/bot_dispatch_hitl_test.go
git commit -m "feat(telegram): render the ResolveDirective outcome on the single callback

scheduled_task_approval -> deterministic IT confirmation, no continuation turn;
ordinary HITL -> continuation. One path, no in-turn/backstop branch.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: Enrich the native Telegram approval (multi-row keyboard + Dettagli + ForceReply)

**Files:**
- Modify/Create: `internal/channels/telegram/hitl.go` (or `hitl_render.go` if >600 LOC) — `approvalMarkup`, `choiceMarkup`, `approvalPromptText`, `detailsMarkup` + a `Dettagli` handler.
- Modify: `internal/channels/telegram/bot_dispatch.go` — route the `Dettagli` action in `onCallback`.
- Test: `internal/channels/telegram/hitl_test.go`

**Interfaces:**
- Produces: `func approvalMarkup(token string) *tele.ReplyMarkup` (multi-row: Approva/Rifiuta on row 1, Dettagli on row 2); a new `ActionDetails = "details"` local const parsed by `parseCallback`; `func approvalPromptText(taskID, kind string) string` (bounded, secret-safe).

- [ ] **Step 1: Write the failing tests** — `internal/channels/telegram/hitl_test.go`

```go
func TestApprovalMarkupMultiRow(t *testing.T) {
	mk := approvalMarkup("11112222-3333-4444-5555-666677778888")
	if len(mk.InlineKeyboard) != 2 {
		t.Fatalf("want 2 rows (Approva/Rifiuta, Dettagli), got %d", len(mk.InlineKeyboard))
	}
	if len(mk.InlineKeyboard[0]) != 2 || mk.InlineKeyboard[1][0].Text != "Dettagli" {
		t.Fatalf("unexpected keyboard shape: %+v", mk.InlineKeyboard)
	}
	for _, row := range mk.InlineKeyboard { // on-wire guard holds for every button
		for _, b := range row {
			if wire := 2 + len(b.Unique) + len(b.Data); wire > 64 {
				t.Fatalf("button %q on-wire %d > 64", b.Text, wire)
			}
		}
	}
}

func TestDetailsCallbackIsNonResolving(t *testing.T) {
	// A Dettagli tap must NOT call SubmitAnswer (it only reveals detail).
	fake := &fakeResumeRunner{}
	h := newHitl(fake, nil)
	out := h.handleCallbackResult(context.Background(),
		callbackData(schedToken, actionDetails, ""), convID, nil)
	if fake.submitCalls != 0 || out.submitted {
		t.Fatal("details must not resolve the pause")
	}
}
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/channels/telegram/ -run 'ApprovalMarkupMultiRow|DetailsCallback'` → FAIL.

- [ ] **Step 3: Multi-row markup + details action** — in `hitl.go`/`hitl_render.go`:

```go
const actionDetails = "details" // channel-local: reveal bounded detail, does NOT resolve

func approvalMarkup(token string) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{
		{
			{Unique: callbackUnique, Text: "Approva", Data: callbackData(token, askuser.ActionAccept, "yes")},
			{Unique: callbackUnique, Text: "Rifiuta", Data: callbackData(token, askuser.ActionDecline, "")},
		},
		{
			{Unique: callbackUnique, Text: "Dettagli", Data: callbackData(token, actionDetails, "")},
		},
	}
	return mk
}
```

- [ ] **Step 4: Guard the details action in resolve + handle it in `onCallback`**
  - In `handleCallbackResult`, short-circuit BEFORE `SubmitAnswer` when `action == actionDetails`: `return callbackOutcome{action: actionDetails}`.
  - In `bot_dispatch.go onCallback`, when `action == actionDetails`, edit the message to reveal the pause's full bounded `Question` (from `PendingFor`) and keep the keyboard armed:

```go
if action == actionDetails {
	t.revealApprovalDetails(daemonCtx, c, chatID) // edits msg to the bounded full Question, keyboard intact
	_ = c.Respond(&tele.CallbackResponse{Text: "Dettagli"})
	return nil
}
```
`revealApprovalDetails` reads `t.deps.Resume.PendingFor`, finds the token's pause, and edits the message to its `Question` (already server-sanitized/bounded) — a pure render, no new DB read beyond the existing pending read.

- [ ] **Step 5: Formatted prompt text + ForceReply parity** — `approvalPromptText(taskID, kind)` returns a formatted multi-line bounded header (kind + short id, secret-safe). Verify `choiceMarkup` already renders one button per option + a cancel, and the `default` (clarification) branch already sends `ForceReply: true` (it does — `hitl.prompt`). No change needed for ForceReply beyond confirming the choice/clarification kinds still route there for scheduled + ordinary pauses.

- [ ] **Step 6: Run tests (incl. race)** — WSL: `go test -race ./internal/channels/telegram/ -run 'Approval|Details|Choice|Hitl'` → PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/channels/telegram/
git commit -m "feat(telegram): richer native approval (Approva/Rifiuta/Dettagli + bounded detail)

Multi-row keyboard, a non-resolving Dettagli reveal of the bounded question,
formatted secret-safe prompt. No Mini App, in-process.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: WebUI resolve endpoint returns the directive (Go)

**Files:**
- Modify: `internal/agui/approvals_api.go` (`handleResolveApproval`, `resolveBody`, a new `resolveResponse`)
- Test: `internal/agui/approvals_api_unit_test.go`

**Interfaces:**
- Consumes: `runner.SubmitAnswer` (single) + `runner.ResolveDirective`.
- Produces: `POST /api/approvals/{token}/resolve` → `200` + `{"outcome":"continue|approved|rejected|pending|terminated","remaining":N}`.

- [ ] **Step 1: Write the failing test** — `internal/agui/approvals_api_unit_test.go`

```go
func TestResolveApprovalReturnsDirective(t *testing.T) {
	srv := newApprovalsTestServer(t, /* run stub returning OutcomeApproved */)
	rr := httptest.NewRecorder()
	body := `{"action":"accept"}`
	req := httptest.NewRequest(http.MethodPost, "/api/approvals/"+validUUID+"/resolve", strings.NewReader(body))
	srv.handleResolveApproval(rr, withScopedIdentity(req))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got struct {
		Outcome   string `json:"outcome"`
		Remaining int    `json:"remaining"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Outcome != "approved" {
		t.Fatalf("outcome = %q, want approved", got.Outcome)
	}
}
```
(Extend the existing `run` test stub so `SubmitAnswer` returns a chosen `ResolveDirective`.)

- [ ] **Step 2: Run to verify it fails** — WSL: `go test ./internal/agui/ -run TestResolveApprovalReturnsDirective` → FAIL (still 204 / uses `SubmitAnswers`).

- [ ] **Step 3: Switch to `SubmitAnswer` + project the directive** — `internal/agui/approvals_api.go`, replace the `SubmitAnswers` block (lines ~154-165):

```go
directive, err := s.run.SubmitAnswer(r.Context(), token, runner.ResponseInput{Action: action, Content: body.Content})
if err != nil {
	if errors.Is(err, askuser.ErrPauseNotFound) {
		http.Error(w, "approval not found or already resolved", http.StatusNotFound)
		return
	}
	http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
	return
}
writeJSON(w, resolveResponse{Outcome: outcomeString(directive.Outcome), Remaining: directive.Remaining})
```

Add the projection:

```go
type resolveResponse struct {
	Outcome   string `json:"outcome"`
	Remaining int    `json:"remaining"`
}

func outcomeString(o runner.ResolveOutcome) string {
	switch o {
	case runner.OutcomeApproved:
		return "approved"
	case runner.OutcomeRejected:
		return "rejected"
	case runner.OutcomePending:
		return "pending"
	case runner.OutcomeTerminated:
		return "terminated"
	default:
		return "continue"
	}
}
```
(`writeJSON` already sets 200 + JSON. Update the handler docstring: 204 → 200.)

- [ ] **Step 4: Run tests (incl. race)** — WSL: `go test -race ./internal/agui/ -run 'Resolve|Approval'` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agui/approvals_api.go internal/agui/approvals_api_unit_test.go
git commit -m "feat(agui): resolve endpoint returns the ResolveDirective (200 JSON)

Switch to SubmitAnswer (single) and project {outcome, remaining} so the cockpit
consumes the same decision as Telegram instead of inferring re-drive client-side.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: WebUI card consumes the directive (TypeScript)

**Files:**
- Modify: `web/src/approvals/useApprovals.ts` (`postResolve`, `ResolveDirective` type)
- Modify: `web/src/approvals/InlineApprovalCard.tsx` (`submit` onSuccess maps outcome→state; re-drive only on `continue`)
- Test: `web/src/approvals/__tests__/InlineApprovalCard.test.tsx`

**Interfaces:**
- Consumes: `{outcome, remaining}` from Task 6.
- Produces: `postResolve` resolves to `ResolveDirective`; the card sets `CardState` from `outcome`.

- [ ] **Step 1: Write the failing test** — `web/src/approvals/__tests__/InlineApprovalCard.test.tsx`

```tsx
it('renders the approved chip and does not re-drive on a scheduled approval', async () => {
  server.use(
    http.post('/api/approvals/:token/resolve', () =>
      HttpResponse.json({ outcome: 'approved', remaining: 0 }),
    ),
  );
  const onResolved = vi.fn();
  render(<InlineApprovalCard approval={schedApproval} onResolved={onResolved} />);
  await userEvent.click(screen.getByRole('button', { name: /answer|conferma|approva/i }));
  expect(await screen.findByText(/answered|approvat/i)).toBeInTheDocument();
  expect(onResolved).toHaveBeenCalled();
});
```

- [ ] **Step 2: Run to verify it fails** — Windows Git Bash: `cd web && ./node_modules/.bin/vitest run src/approvals/__tests__/InlineApprovalCard.test.tsx` → FAIL.

- [ ] **Step 3: `postResolve` returns the directive** — `web/src/approvals/useApprovals.ts`:

```ts
export type ResolveOutcome = 'continue' | 'approved' | 'rejected' | 'pending' | 'terminated';
export interface ResolveDirective {
  readonly outcome: ResolveOutcome;
  readonly remaining: number;
}

async function postResolve(vars: ResolveVars): Promise<ResolveDirective> {
  const body =
    vars.action === 'accept' ? { action: vars.action, content: vars.content ?? '' } : { action: vars.action };
  const res = await fetch(`/api/approvals/${encodeURIComponent(vars.token)}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`HTTP ${String(res.status)}`);
  const parsed: unknown = await res.json();
  return parsed as ResolveDirective;
}
```
(`useResolveApproval`'s `onSuccess` still invalidates the query; its `mutate` result now carries the directive.)

- [ ] **Step 4: Card maps outcome → state, re-drives only on continue** — `web/src/approvals/InlineApprovalCard.tsx` `submit`:

```tsx
resolve.mutate(payload, {
  onSuccess: (directive) => {
    switch (directive.outcome) {
      case 'approved':
        setState('answered');
        break;
      case 'rejected':
        setState('declined');
        break;
      case 'terminated':
        setState('cancelled');
        break;
      default: // 'continue' | 'pending'
        setState(action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled');
    }
    void onResolved?.(attempt); // the thread gate re-drives; only 'continue' has more model work
  },
  onError: () => { void onResolutionFailed?.(attempt); },
});
```
(The existing `onResolved`→thread-gate path already handles re-drive for a live thread; a scheduled `approved`/`rejected` sets a terminal chip and the gate no-ops because the run is not streaming. Confirm `useThreadApprovals` does not force a re-drive on a terminal outcome; if it does, gate it on `directive.outcome === 'continue'`.)

- [ ] **Step 5: Run web gates** — Windows Git Bash, in `web/`:

```
./node_modules/.bin/vitest run src/approvals
./node_modules/.bin/tsc --noEmit
./node_modules/.bin/eslint src/approvals
./node_modules/.bin/prettier --check src/approvals
```
Expected: PASS.

- [ ] **Step 6: Commit** (do NOT rebuild dist yet — Task 8 rebuilds once)

```bash
git add web/src/approvals/
git commit -m "feat(web): approval card consumes the resolve directive

Map server {outcome} -> CardState; re-drive only on 'continue'. Both channels
now render the same runner decision.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: Rebuild the cockpit bundle (dist freshness)

**Files:**
- Modify: `internal/webui/dist/**` (generated)

- [ ] **Step 1: Rebuild dist on Linux node** (Windows Vite re-hashes every chunk — use the docker webbuild stage):

```
docker compose build aura   # runs the webbuild stage (Linux node-24) that emits internal/webui/dist
```
Or the project's documented web-dist target if one exists (`make web-dist` / the CI webbuild). Confirm the diff is only `internal/webui/dist/assets/*`.

- [ ] **Step 2: Verify freshness locally** — run the web-dist-freshness check the pre-push/CI uses; it must report the committed dist matches the source.

- [ ] **Step 3: Commit**

```bash
git add internal/webui/dist
git commit -m "build(web): rebuild cockpit bundle for approval directive card

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: Integration test — scheduled approval resolves identically end-to-end

**Files:**
- Test: `internal/runner/runner_resume_test.go` (or a new `internal/runner/scheduled_approval_directive_integration_test.go`, `//go:build db_integration`)

- [ ] **Step 1: Write the integration test** — a real `scheduled_task_approval` pause resolved via `SubmitAnswer` returns `OutcomeApproved` and the ResumeHook fired (task active):

```go
//go:build db_integration

func TestScheduledApprovalResolvesToApprovedAndActivates(t *testing.T) {
	r, store := newRunnerWithScheduledApprover(t) // wires newScheduledTaskResumeHook over a real task store
	convID, taskID := seedPendingApprovalTask(t, store)
	token := mintScheduledApprovalPause(t, r, convID, taskID)

	directive, err := r.SubmitAnswer(context.Background(), token, ResponseInput{Action: askuser.ActionAccept})
	if err != nil {
		t.Fatalf("SubmitAnswer: %v", err)
	}
	if directive.Outcome != OutcomeApproved {
		t.Fatalf("outcome = %v, want Approved", directive.Outcome)
	}
	if got := taskStatus(t, store, taskID); got != "active" {
		t.Fatalf("task status = %q, want active", got)
	}
}
```
(Mirror the existing `runner_resume_single_atomic_integration_test.go` harness + `serve_scheduled_approval.go` wiring for the approver stub.)

- [ ] **Step 2: Run it against the disposable DB** — WSL, stack up:

```
GOFLAGS=-tags=db_integration go test ./internal/runner/ -run TestScheduledApprovalResolvesToApprovedAndActivates -count=1
```
Expected: PASS. Add the decline→Rejected→cancelled twin.

- [ ] **Step 3: Commit**

```bash
git add internal/runner/
git commit -m "test(runner): scheduled approval resolves to Approved + activates (db_integration)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 10: Gates, mutation, live E2E, phase close

- [ ] **Step 1: Full local matrix (WSL, stack up)** — the stricter Skills-gate number:

```
bash scripts/coverage_docker.sh        # disposable aura_cov DB; owned-surface floor >=85%
```
Fix any package that dropped below 85% with daemon-free unit tests (esp. the deleted `scheduled_approval.go` coverage now folded into `hitl`/directive tests).

- [ ] **Step 2: Mutation spot-check on the classifier** — WSL:

```
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
go-mutesting ./internal/runner/runner_resume_directive.go   # expect >=70% killed
```
Record the score in the phase VALIDATION note.

- [ ] **Step 3: Quality snapshot re-attestation** — for every `docs/aura-quality-snapshot.md` row whose CI-gate-path glob matches a changed file, bump `Last measured` to today + prepend a re-attestation note. Verify:

```
AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" \
AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" \
bash scripts/quality_snapshot_gate.sh   # must print: ok: ... checked N row(s)
```

- [ ] **Step 4: Live E2E (>9.8, real stack — Telegram + DeepSeek-V4-Flash + cockpit)**
  1. `docker compose up -d aura` (restores `AURA_SCHEDULER_APPROVAL_GRACE_SEC=60`).
  2. Schedule an `agent_job` from Telegram → ONE richer prompt (kind + summary + Approva/Rifiuta/Dettagli) → tap Dettagli (reveals bounded detail, keyboard intact) → tap Approva → deterministic "✅ approvato", task `active`, **no loop, no duplicate, no BUTTON_DATA_INVALID**.
  3. Schedule an `agent_job` from the cockpit → resolve in the card → `approved` chip, no client re-drive; identical behavior.
  4. Force the backstop (delete the model pause) → sweep re-mints + pushes via the unified builder → resolve → identical `approved` outcome.
  5. An ordinary clarification ask_user on BOTH channels still continues the turn.
  Score each dimension; target >9.8.

- [ ] **Step 5: Push + CI green + merge** — per CLAUDE.md phase-close discipline:

```
git push
# watch: CI + Skills (db_integration coverage) + CodeQL + Web-E2E all green
```
Merge to `master` if on a branch; re-run post-merge verification. Update the design doc status → shipped; write a session handoff.

- [ ] **Step 6: Update memories** — mark `project_telegram_miniapp_richer_askuser` Phase A shipped; note the ResolveDirective seam as the consolidation mechanism.

---

## Self-Review

**Spec coverage (design §):**
- §3 ResolveDirective + classifyResolve → Task 1. ✅
- §3 SubmitAnswer signature + SubmitAnswers unchanged → Task 2. ✅
- §4b collapse callbacks + delete scheduled_approval.go + framing guard → Task 3. ✅
- §4b directive dispatch (Continue/Approved/Rejected/Pending/Terminated) → Task 4. ✅
- §4c enrichment (multi-row, Dettagli, formatted, ForceReply) → Task 5. ✅
- §4d WebUI Option B (Go endpoint) → Task 6; (React card/hook) → Task 7; dist → Task 8. ✅
- §8 testing (unit/integration/vitest/mutation/live) → Tasks 1,3-7,9,10. ✅
- §9 no migration/env → Global Constraints + no such step. ✅
- §11 behavioral change (in-turn scheduled approval no "Fatto!") → Task 4 (deterministic outcome) + Task 10 live check. ✅

**Placeholder scan:** every code step carries real code; test steps carry real assertions; the one deliberately-thin helper (`approvalPromptText` in Task 3) is enriched in Task 5, noted inline. No TBD/TODO.

**Type consistency:** `ResolveOutcome`/`ResolveDirective`/`classifyResolve`/`isScheduledTaskApproval` defined in Task 1; consumed with the same names/fields in Tasks 2,4,6. `outcomeString` (Go) ↔ `ResolveOutcome` (TS union) use the same five string values (`continue|approved|rejected|pending|terminated`). `actionDetails="details"` defined + parsed in Task 5. `callbackUnique`/`callbackData`/`callbackTelebotFrameBytes` consistent across Tasks 3 and 5.

**Open item flagged for execution:** Task 7 Step 4 — confirm `useThreadApprovals`/`ThreadApprovalCards` does not force a re-drive on a terminal outcome; gate on `outcome === 'continue'` if it does. This is a read-and-verify, not a placeholder.
