# Next phase — channel approval consolidation + scheduler board (findings from 1.7 E2E)

**Date:** 2026-07-24
**Status:** Findings + scope for the next session (turn into a plan via brainstorm → writing-plans)
**Predecessor:** [2026-07-23 on-channel scheduled-task approval](2026-07-23-scheduled-task-approval-on-channel-design.md) (fix-plan 1.7 / PRD Amendment #92) — **shipped** with 5 E2E-hardening fixes (commit `104a284d0`).

## Why this phase exists

1.7 delivered the on-channel scheduled-task approval with a deterministic sweep backstop. Live E2E on the real stack (Telegram + DeepSeek-V4-Flash + the cockpit) surfaced **five defects — all Telegram-only, none on the WebUI** — now fixed. The operator's diagnosis is the thesis of this phase:

> "We must consolidate the channels, we cannot duplicate code — this is why we have bugs in Telegram and not in the WebUI."

The bugs were Telegram-only because **Telegram carries its own duplicate implementation of the ask_user/approval surface**, divergent from the cockpit's:

| Concern | Telegram (native) | WebUI (cockpit) |
|---|---|---|
| Render an ask_user pause | `internal/channels/telegram/hitl.go` (`approvalMarkup`/`choiceMarkup`/ForceReply) + `scheduled_approval.go` (`scheduledApprovalMarkup`) | `web/src/approvals/InlineApprovalCard.tsx` |
| Encode the action | `callback_data` (64-byte cap, telebot `\f<Unique>\|` framing) | HTTP token in the URL |
| Resolve | `hitl.handleCallback` (continuation turn) **and** `onScheduledApprovalCallback` → `resolveScheduled` (no continuation) | `POST /api/approvals/{token}/resolve` + React re-drive |
| Confirm to operator | toast / edited message | card state transition |

Two independent implementations of one concept → each fix had to be made twice, and the class of bug "works on one channel, silently broken on the other" is structural. The only shared floor today is `runner.SubmitAnswer(s)`.

## The five fixes shipped in 1.7 (context for the consolidation)

1. **accept-loop** — the resumed model was never told the resume hook activated the task, so it re-ran `task schedule` on every accept. Fixed in `answerTurn` (shared).
2. **`BUTTON_DATA_INVALID`** — sweep button `\f<Unique>\|<token>\|approve` = 65 > 64 bytes. Telegram-native only.
3. **raw UUID in the prompt** — masked to `kind + first-8`.
4. **duplicate prompt** — the sweep raced the in-turn relay and minted a second pause. Fixed with a grace delay + deliver-only-when-minted (the sweep is now a true backstop).
5. **silent backstop resolve** — the sweep-button press had no continuation turn, so it left only a toast; now rewrites the prompt to a visible outcome.

Fixes 1 and 4 live in shared code (`runner`, `cron`) and survive the consolidation; 2, 3, 5 are patches to the Telegram-native path that the consolidation **deletes**.

## Phase A — private native consolidation (priority)

**Operator constraint (2026-07-24):** **no cloud, no public endpoint — simple and private.** This rules OUT the Telegram Mini App: a `web_app` button opens a URL the Telegram *client* loads over HTTPS, so it inherently needs a reachable HTTPS endpoint (public, or private-over-VPN with a trusted cert) — neither simple nor endpoint-free. So the consolidation is done **in-process**, with no new infra.

**Goal:** kill the per-channel divergence that caused the 1.7 bugs WITHOUT a Mini App, and enrich the native Telegram approval — everything private and in-process.

**Approach — two parts:**
1. **Consolidate the shared resolve/continuation orchestration.** Move the "after a pause resolves, do we drive a continuation turn / what confirmation goes back" decision into ONE place both channels drive (the Telegram callback path and the WebUI `/api/approvals` path funnel through the same runner-level logic). This is what structurally removes the loop/duplicate/behavior-divergence bug class — the actual root cause — with zero new infra. `runner.SubmitAnswer(s)` + `answerTurn` already own most of it; finish pulling the channel-specific continuation decisions into the shared layer so a channel only *renders* and *forwards the action*.
2. **Enrich the native Telegram approval** (the "richer ask_user" the operator wants, no Mini App): a formatted message (kind + bounded goal summary + schedule + risk, secret-safe), a multi-row inline keyboard (Approva / Rifiuta / Dettagli), and `ForceReply` for the free-text/choice `ask_user` kinds. A step up from bare Sì/No, still 100% in-process.

**What stays vs goes:** the Telegram-native rendering STAYS (it's the private surface) but becomes a thin render+forward layer over the shared orchestration; the divergent *behavior* (separate continuation logic, the two callback paths `aura_hitl`/`aura_sappr`) is unified. The 1.7 guards (callback_data on-wire length, id masking, outcome edit) remain — they are correct for a native transport.

**Requirements / unknowns for the brainstorm:**
- Draw the exact seam: what the shared orchestration owns vs what a channel owns (render markup, parse action, forward). Both `aura_hitl` and `aura_sappr` Telegram callbacks should collapse to one path.
- Native rendering of free-text/choice kinds on Telegram (ForceReply + option buttons) at parity with the WebUI card's affordances.
- Bounded goal/summary in the Telegram message (secret-safety: never dump the full payload).

**Acceptance (real-scenario, >9.8):** schedule an `agent_job` from Telegram AND from the cockpit → both resolve through the **same** orchestration (one continuation-decision code path, verified) → the richer native Telegram approval renders (kind + summary + multi-row keyboard + free-text where applicable) → no loop, no duplicate, consistent behavior with the cockpit → all private, no endpoint added.

## Phase B — scheduler board detail + edit

**Goal:** the cockpit scheduler board can **show a task's contents and modify it** (operator: "can not see what is inside, can not modify"). Today it lists name/schedule/status + play/edit/delete but has no detail view and no working edit.

**Scope:**
- Task **detail panel**: render `aura.scheduler_tasks` payload/goal/schedule/next_run/origin/step_budget (secret-safe bounds).
- **Update path**: a new `aura task update` tool + owner-scoped `UPDATE aura.scheduler_tasks` + a cockpit edit form (re-tier + re-approve if the edit changes risk).
- Parity with the documents delete/update gap ([[project_document_delete_update_feature_gap]]).

**Acceptance:** open a scheduled task in the cockpit, see its goal/schedule, edit the goal/next-run, save, and see the change reflected (re-gated to `pending_approval` if the edit raises risk).

## Minor / deferred findings

- **re-nudge dropped** (decision, not a bug): the sweep now delivers once on mint, not once per cadence. Revisit only if a real "remind me again" need appears.
- **`AURA_SCHEDULER_APPROVAL_{REMINDER,GRACE}_SEC`** — now plumbed into `compose.yaml` (were previously unreachable in the container). No action.
- **reasoning-object → None-target wire bug** (llama.cpp ignores it today, latent) — still separate.
- **adaptive reasoning is a no-op on llama.cpp** (`ApplyAdaptiveReasoning` is OpenRouter-gated) — still separate.

## Sequencing

Phase A first (it removes the bug class and delivers the richer Telegram ask_user the operator asked for). Phase B is independent and can interleave. Each gets its own brainstorm → spec → plan → execute cycle.
