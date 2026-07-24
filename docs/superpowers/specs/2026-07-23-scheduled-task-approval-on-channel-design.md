# Scheduled-task approval — real HITL on the origin channel (fix-plan 1.7 revision)

**Date:** 2026-07-23
**Status:** Design — awaiting review
**Supersedes:** the delivery half of fix-plan 1.7 / PRD Amendment #92 (the `DeliverToIdentity` text nudge). The throttle/sweep skeleton (migration 0051, `approval_reminded_at`, cadence env, tick wiring) is **kept and re-purposed**, not reverted.

## Problem

A scheduled task forced to `status='pending_approval'` (every `agent_job` per AG-016, or a risky/destructive-scored task) must be approved by the operator **on the channel they scheduled it from** — Telegram or the WebUI chat — as a real `ask_user` HITL prompt. It must **never** be routed to the cockpit governance scheduler board.

Two gaps against that requirement:

1. **Root cause (BUG-1B).** The primary path relies on the *model* relaying the approval as an `ask_user` pause (`task.go` → `scheduledApprovalRequiredResult`). When the model fails to relay, no approval surface is ever created and the task sits `pending_approval` forever.
2. **The 1.7 backstop diverged.** The reminder sweep re-nudges with **plain text pointing at the cockpit** (`"Approve or cancel it in the Aura cockpit."` via the text-only `DeliverToIdentity` seam) — not a channel-actionable HITL prompt. This is the line the revision removes.

## What already works (and is reused verbatim)

- **`ask_user` HITL renders on both channels already.** Telegram → inline Sì/No (`internal/channels/telegram/hitl.go` `approvalMarkup`); WebUI → in-thread approval (`web/src/approvals/useThreadApprovals.ts`, pull-based via `GET /api/approvals`).
- **The resume hook already activates the task on accept.** `newScheduledTaskResumeHook` (`cmd/aura/serve_adapters.go`) → `ApproveScheduledTaskInConversation(taskID, pending.ConversationID)`, owner-scoped to the authenticated origin conversation (WR-02). **No new approval logic is written.**
- **A sanctioned second writer of `paused_states` exists.** The capability-gated operator-origin governance path mints an `ask_user` pause directly via `askuser.Store.Insert`, entirely outside the agent (T-04-19 / Phase-29 D-13; `internal/agent/llm_agent_pause.go:12-20`, `internal/agui/governance_write_seam.go:88-91`). The scheduled-task mint is the **same pattern**: operator-origin, no model, `identity_id` auto-stamped by the BEFORE-INSERT trigger (36-04) from the conversation owner.

## Design

### Core idea

The **backstop sweep becomes the deterministic guarantor** instead of a text nudge. The model relay stays as the t=0 fast path (it already renders correctly when it fires); the sweep makes it non-load-bearing. Per cadence, for every channel-owned `pending_approval` task, the sweep:

1. **Ensure a `scheduled_task_approval` `ask_user` pause exists** on the task's `origin_conversation_id`.
   - If one already exists (the model relayed it, or a prior sweep minted it) → reuse its token. **Idempotent** — never a second pause for the same task.
   - Else **mint it host-side** via `askuser.Store.Insert` (operator-origin, no model), `Kind="approval"`, `ResumeContext={"type":"scheduled_task_approval","task_id":<id>}`, `identity_id` auto-stamped by the trigger.
2. **Re-surface it on the origin channel:**
   - **WebUI** → no push. The pause is in `paused_states`, so `GET /api/approvals` surfaces it in-thread (pull). Self-serving.
   - **Telegram** → **push** the Sì/No prompt bound to the pause **token** via a new optional `ApprovalDeliverer` channel capability (Telegram is push-based; it cannot pull).
3. **Stamp `approval_reminded_at`** regardless of delivery outcome (existing throttle — a failing channel cannot spam the tick).

On accept/cancel, the operator resolves the **real pause** — Telegram button → `SubmitAnswer(token, accept|cancel)`; WebUI → `POST /api/approvals/{token}/resolve`. Both drive the **same** resume hook. Self-terminating: once the task leaves `pending_approval`, the sweep no longer selects it.

**Decision (approved):** model relay = t=0 fast path (in-turn, unchanged); headless sweep mint = deterministic guarantee within one cadence. We do **not** mint mid-model-turn (no precedent for a second writer inserting on a live conversation; would need to prove it can't perturb the running turn's pause FIFO). If a tighter guarantee is wanted, lower `AURA_SCHEDULER_APPROVAL_REMINDER_SEC`, do not mint mid-turn.

### Seams (import-cycle-safe: `cron` imports neither `askuser` nor `channels`)

`cron` declares two consumer-side interfaces (primitives + the resume-context shape only):

```go
// internal/cron — the ensure-or-mint seam (satisfied by a cmd/aura adapter over askuser.Store).
type ApprovalPauseEnsurer interface {
    EnsureApprovalPause(ctx context.Context, taskID, conversationID, kind string) (token string, err error)
}

// internal/cron — extends the existing ChannelDeliverer wiring with a token-bound approval push.
type ApprovalChannel interface {
    DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (delivered bool, err error)
}
```

- **`channels`** gets a new optional capability mirroring `Deliverer`:
  ```go
  // internal/channels/deliver.go
  type ApprovalDeliverer interface {
      DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (delivered bool, err error)
  }
  ```
  `Registry.DeliverApproval` fans out sorted/tri-state exactly like `DeliverToIdentity`; a channel not implementing it is skipped. Only Telegram implements it → WebUI identities return `(false,nil)` (expected; they pull).

- **`cmd/aura`** wires:
  - an `EnsureApprovalPause` adapter over `askuser.Store` (dedup query + mint), into `cron.Dispatch` deps;
  - `channels.Registry` as `cron`'s `ApprovalChannel`;
  - registers the new Telegram callback (below).

### Ensure-or-mint (dedup)

New `askuser` query: list still-pending pauses for a conversation whose `resume_context->>'type' = 'scheduled_task_approval'` and `resume_context->>'task_id' = $taskID`. Match → reuse token; no match → `Insert` a fresh pause. This is what makes the per-cadence sweep idempotent and what lets a model-relayed pause and the sweep converge on one pause.

### Sweep selection — mint covers WebUI origins too (correction to 1.7)

The shipped `ListDuePendingApprovalReminders` excludes `identity_id IN ('','local')` on the rationale that "cockpit approvals are already visible in the AG-UI panel." That rationale only holds when the model **did** relay the pause. For the root-cause fix the sweep must **mint** for WebUI-origin tasks too, so the selection predicate changes:

- **Include** every `pending_approval` task with a **non-null `origin_conversation_id`** (a conversation to attach the pause to) whose throttle stamp is due — regardless of `identity_id`. WebUI/`local`-origin rows are now selected so their pause gets minted; they surface via the pull `/api/approvals`, and the Telegram push is simply a `(false,nil)` no-op for them.
- **Exclude** rows with an empty `origin_conversation_id` (CLI-origin, no conversation) — a HITL pause cannot be keyed without a conversation. CLI keeps its existing `aura task approve` path (out of scope).

This is a one-line predicate change in the sweep query (`origin_conversation_id IS NOT NULL AND origin_conversation_id <> ''` in place of the `identity_id NOT IN ('','local')` filter). The push step, not the SELECT, is what makes it Telegram-only.

### Telegram render + callback (a *real* ask_user resolution, not a shortcut)

- `DeliverApproval` sends the question text + an inline keyboard under a **new** endpoint `aura_sched_approval`, `callback_data = "<token>|<action>"` (`token` uuid 36 + `|` + `approve|cancel` ≤ 8 → < 64-byte cap). Reuses the existing `deliver.go` identity→account resolver.
- New `onScheduledApprovalCallback`: auth-gate (`requireLinkedCallback`), parse `token|action`, then resolve the **real pause** via `runner.SubmitAnswer(token, {Action})` — the same path in-turn HITL uses. It does **not** drive a continuation Turn (unlike in-turn HITL): an operator-origin approval has no turn to continue; the resume hook fires on `SubmitAnswer` and flips the task. Ack + disarm keyboard.
  - **Sì / Approva** → `ActionAccept` → resume hook → `ApproveScheduledTaskInConversation` → `active`.
  - **No / Rifiuta** → `ActionCancel` → resume hook (extended) → `CancelScheduledTaskInConversation` → `cancelled` (so a declined task stops re-nudging instead of looping).

### Resume-hook extension (one branch)

`newScheduledTaskResumeHook` currently no-ops on non-accept. Extend it: `ActionCancel` on a `scheduled_task_approval` pause → new owner-scoped `CancelScheduledTaskInConversation(taskID, pending.ConversationID)` (`UPDATE ... SET status='cancelled' WHERE id=$1 AND origin_conversation_id=$2 AND status='pending_approval'`). Accept branch unchanged. This makes WebUI-cancel and Telegram-cancel behave identically (both go through `SubmitAnswers` → the hook).

### Reminder text

Delete `approvalReminderText`'s cockpit prose. The Telegram push question is a bounded prompt: task **kind + short id** only, never the payload (unchanged secret-safety bound). Example: `"Scheduled agent_job task a1b2c3d4 needs your approval — approve or reject below."`

## Files touched (all ≤600 LOC, refactor-on-touch)

| File | Change |
|---|---|
| `internal/cron/deliver_approval.go` | sweep = ensure-or-mint + `DeliverApproval` push; drop text nudge |
| `internal/cron/dispatch.go` | add `ApprovalPauseEnsurer` + `ApprovalChannel` deps |
| `internal/channels/deliver.go` | `ApprovalDeliverer` capability |
| `internal/channels/registry.go` | `DeliverApproval` fan-out |
| `internal/channels/telegram/deliver.go` | Telegram `DeliverApproval` (render + push) |
| `internal/channels/telegram/bot_dispatch_callbacks.go` + `bot_dispatch.go` | `onScheduledApprovalCallback` + registration |
| `internal/askuser/store*.go` + `queries/paused_states.sql` | dedup query (`GetPendingApprovalByTask`) |
| `cmd/aura/serve_adapters.go` | `EnsureApprovalPause` adapter; resume-hook cancel branch; `CancelScheduledTaskInConversation` |
| `cmd/aura/serve_dispatch.go` | wire the two new cron deps |
| `prd.md` | rewrite Amendment #92 / fix-plan 1.7 |

No new migration (0051 stands; the dedup query reads existing `paused_states.resume_context`).

## Testing / Definition of Done

- **Unit (daemon-free):** ensure-or-mint idempotency (reuse vs mint), `DeliverApproval` fan-out tri-state, Telegram callback approve/cancel parse + SubmitAnswer, resume-hook cancel branch, reminder text bound (kind+id, no payload).
- **Integration (`db_integration`):** dedup query against real `paused_states` jsonb; task flips to active on accept and cancelled on reject; owner-scoping (foreign conversation cannot approve/cancel).
- **E2E with the real agent (DoD, score >9.8 — real scenario, not smoke):**
  1. From **Telegram**, ask the agent to schedule an `agent_job`; confirm the approval arrives **in Telegram** as real Sì/No; press Sì; confirm the task flips to `active` and fires.
  2. From the **WebUI chat**, same; confirm the approval renders **in-thread** (not the governance board); approve; confirm activation.
  3. **Failure mode:** force the model to *not* relay (or delete the relayed pause); confirm the sweep mints + surfaces the approval on the origin channel within one (lowered) cadence.
  4. Reject path: press No; confirm the task is `cancelled` and stops re-nudging.

## Out of scope (tracked separately)

- Loose ends from the working session: revert `compose.override.yaml` (5s/10s test cadence) before deploy; commit regenerated calm-prism baselines + revert the TEMP `--update-snapshots` step in `ci.yml`; push unpushed commits CI-green.
- Adaptive reasoning is a no-op on llama.cpp (`ApplyAdaptiveReasoning` is OpenRouter-gated) — separate enhancement.
- The reasoning-object → None-target wire bug (llama.cpp ignores it today, latent) — separate.

## Post-E2E hardening (2026-07-24)

Live E2E on the real stack (Telegram, DeepSeek-V4-Flash) surfaced four defects the daemon-free unit tests missed; all fixed on this branch:

1. **Accept-loop (BUG-1B tail).** Accepting the model-relayed `ask_user` pause drove a continuation turn, but the task activation was a silent resume-hook side-effect — the resumed model, never told the task was now active, re-ran `task schedule` and relayed another approval every time the operator tapped Sì. **Fix:** `answerTurn` (runner) now injects an explicit RoleTool result on accept/decline of a `scheduled_task_approval` pause ("task is ACTIVE/CANCELLED — scheduling complete, do NOT re-schedule").

2. **`BUTTON_DATA_INVALID (400)` — the sweep push never delivered.** telebot frames inline callback_data on the wire as `"\f<Unique>|<Data>"`; the 19-char Unique `aura_sched_approval` + a 36-char UUID token + `|approve` = 65 bytes > Telegram's 64-byte cap. **Fix:** shortened the Unique to `aura_sappr` (10) + an on-wire length guard + a markup test that counts the framing. (The in-turn HITL only survived because its `aura_hitl` Unique is 9 chars.)

3. **Raw UUID in the prompt.** The relay question dumped the full 36-char id. **Fix:** masked to `kind + first-8` (`shortScheduledTaskID`); the full id stays in the machine-facing `resume_context`.

4. **Duplicate prompt — the sweep raced the model relay.** The sweep selects `approval_reminded_at IS NULL` tasks on the *next* tick (~30s), so it fired in the few-second window between `task schedule` and the model's `ask_user`, minting a second pause → two prompts. This violated this doc's own "we do **not** mint mid-model-turn" rule. **Fix:** the sweep is now a true backstop — (a) a **grace delay** (`AURA_SCHEDULER_APPROVAL_GRACE_SEC`, default 60s) skips a task until it is older than the grace so the in-turn relay wins the fast path; (b) `EnsureApprovalPause` returns a `minted` flag and the sweep pushes **only when it minted** (the model never relayed) — a reused pause is already on the channel.

5. **Backstop resolve read as "nothing happened".** The sweep-button press resolves via `resolveScheduled`, which (correctly) drives no continuation turn — so unlike the model-relay path's "Fatto!" message, it left only a transient toast + a keyboard clear while the task silently flipped to active. **Fix:** `onScheduledApprovalCallback` now rewrites the prompt in place to a persistent outcome (`✅ approvato` / `❌ rifiutato`), giving the backstop path visible confirmation.

**Behavior change vs the original design:** the sweep no longer re-nudges a pending approval each cadence — it delivers the surface **once** (on mint) and relies on the pause's continued existence for the deterministic guarantee. This removes the duplicate-push vector; the "no silent-forget" property is unchanged (a surface always exists within grace + one tick). `AURA_SCHEDULER_APPROVAL_REMINDER_SEC<=0` still hard-disables the sweep.

**Root cause across all five (operator directive):** the two Telegram approval paths (in-turn HITL `aura_hitl` + sweep `aura_sappr`) are a **per-channel duplication** of the WebUI's `/api/approvals` — which is why these bugs hit Telegram and not the cockpit. The point-fixes make the native path correct enough to ship 1.7's deterministic backstop; the **next phase consolidates** Telegram onto the shared cockpit approval surface (Mini App → `InlineApprovalCard` + `/api/approvals`), deleting the duplicate Telegram-native approval code. See the next-phase spec.
