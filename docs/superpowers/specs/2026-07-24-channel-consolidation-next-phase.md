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

## Phase A — channel approval consolidation (priority)

**Goal:** one approval implementation, both channels. Telegram renders the cockpit approval card and resolves through the **same** `/api/approvals`; the duplicate Telegram-native approval rendering/callback/resolve code is removed.

**Approach (decided during 1.7 — "Approach A / Mini App"):**
- A Telegram **Mini App** (Web App): an inline `web_app` button opens the cockpit approval surface inside Telegram's in-app browser.
- Reuse `web/src/approvals/InlineApprovalCard.tsx` + `GET /api/approvals` + `POST /api/approvals/{token}/resolve` verbatim — no callback_data, no 64-byte cap, no second continuation path.
- Build the Mini App shell with **@telegram-apps/telegram-ui** (https://github.com/telegram-mini-apps-dev/TelegramUI — native-look React kit).
- **Auth:** validate the signed `initData` blob (HMAC of the bot token) on the backend; map `initData.user.id` → identity via the existing `aura.telegram_accounts.telegram_user_id` binding. No separate login.
- **Delete** (or reduce to "launch the Mini App"): `scheduled_approval.go`'s native markup/callback, and the approval branch of `hitl.go`. The sweep's `ApprovalDeliverer` becomes "send a `web_app` launch button" instead of an inline Sì/No.

**Requirements / unknowns to resolve in the brainstorm:**
- A **public HTTPS URL** for the Mini App (Telegram fetches it). Caddy vhost vs the Cloudflare tunnel (ephemeral trycloudflare is not stable enough) — decide the hosting.
- `initData` validation lib/impl in Go; replay/expiry handling.
- Fallback when the Mini App can't open (old client / no HTTPS): keep a minimal native accept/reject? Or hard-require the Mini App.
- Free-text and choice `ask_user` kinds (not just approval) — the Mini App should cover them too, finishing the "ask_user like the WebUI" ask.

**Acceptance (real-scenario, >9.8):** schedule an `agent_job` from Telegram → the approval opens the **cockpit card** in Telegram → approve/reject/free-text resolves via `/api/approvals` → task flips → **the Telegram-native approval code is gone** and there is exactly one resolve path exercised by both channels.

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
