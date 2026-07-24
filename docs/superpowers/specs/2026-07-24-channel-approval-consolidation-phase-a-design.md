# Phase A — private native channel-approval consolidation (design)

**Date:** 2026-07-24
**Status:** Design locked (brainstorm complete) — ready for `writing-plans`.
**Governing index:** [consolidated-fix-plan-2026-07-20.md](../../audit/consolidated-fix-plan-2026-07-20.md) — Phase A is the operator-directed consolidation that succeeds the shipped **Wave 1.7** (approval relay-liveness).
**Predecessors:** [2026-07-24 channel-consolidation next-phase findings](2026-07-24-channel-consolidation-next-phase.md) · [2026-07-23 on-channel scheduled-task approval](2026-07-23-scheduled-task-approval-on-channel-design.md) (1.7, shipped).
**Operator constraint (2026-07-24):** no cloud, no public endpoint — simple, private, in-process. Rules OUT the Telegram Mini App. Zero new infra.

---

## 1. Problem

1.7 shipped the on-channel scheduled-task approval with a deterministic sweep backstop. Live E2E surfaced **five defects, all Telegram-only, none on the WebUI**. Root cause (operator's thesis): **Telegram carries a duplicate implementation of the ask_user/approval surface**, divergent from the cockpit's.

The **input** is already normalized — every channel resolves through `runner.SubmitAnswer(s)(token, {action, content})`. The divergence is in the **output decision** ("after a pause resolves, drive a continuation turn or render a deterministic outcome?"), reimplemented three times:

| Path | Resolve entry | Continuation behavior |
|---|---|---|
| TG in-turn HITL (`aura_hitl`) | `hitl.handleCallback`/`submit` | `remaining==0 && !cancel` → drives `startTurn` |
| TG scheduled backstop (`aura_sappr`) | `onScheduledApprovalCallback` → `resolveScheduled` | **no** continuation; bespoke Italian outcome edit |
| WebUI (`/api/approvals/{token}/resolve`) | `handleResolveApproval` | server drives nothing; React infers re-drive from the action verb |

Two Telegram callback endpoints + a three-way-duplicated decision = the structural "works on one channel, silently broken on the other" bug class. Fixes 2, 3, 5 of the 1.7 five were patches to the Telegram-native duplicate this phase **deletes**; fixes 1 and 4 live in shared code (`runner`, `cron`) and survive.

---

## 2. Research grounding

Method mirrors the AG-016 redesign: industrial patterns from `D:\tmp` curated runtimes + web. Both streams converge.

**One shared principle across the two serious analogs:** *one resolver returns a typed resolution; every transport only renders it — never re-implements it.*

- **codex** (Rust): TUI keypress, app-server, the automated guardian reviewer, and an MCP-delegate parent all normalize their answer into one typed envelope keyed by id (`Op::ExecApproval{id, decision}`) → one resolver (`notify_approval`). Nothing downstream branches on who/what answered. Governance-approve (`ReviewDecision`) and free-text-answer (`RequestUserInputResponse`) are *separate typed channels* sharing one resolve mechanism.
- **ADK-go** (Google — closest analog): the human answer is one envelope (`FunctionResponse{id, confirmed}`) → one resolver (`RequestConfirmationRequestProcessor`); console, web, and A2A converge there. It **re-executes the approved action deterministically — it does not ask the model to re-decide** — separating "the gated side-effect" from "continue the conversation." Pending state lives on the persisted task, so *answered-later-with-no-live-turn* is native.
- **Out-of-band approval literature:** "`needs_approval` works for in-session decisions where the user is present, while out-of-band approvals require notification, timeout handling, and an audit record" — and **"'approved' isn't a chat message you parse; it's structured data you store."**

**The classifier this implies** (a pure function of the pause's nature — *not* the delivery channel, *not* an in-turn/backstop flag):

> **Does resolving the pause leave the model more work to do in this turn?**
> - clarification / choice / shell-or-gateway approval → **yes** (model must proceed / run the now-approved action) → **continue the turn** (codex resumes in place; ADK drives a fresh turn — both continue).
> - **`scheduled_task_approval`** → **no**; the `ResumeHook` (`status → active`, owner-scoped on the origin conversation = the "action-hash" guard) *is* the whole intent → **deterministic outcome, no model turn**. This is the "structured data, not a chat message" case, and it is exactly why the 1.7 continuation-driven relay could loop — driving a model turn gave the model a chance to re-decide and re-schedule.

The pause's resume-context type is **already read by the runner** (`scheduledApprovalAnswer`, `newScheduledTaskResumeHook`). So the decision has a natural home with no new inputs.

Sources: LangGraph interrupts; OpenAI Agents SDK human-in-the-loop; Truto / nNode async-approval; and the `D:\tmp` survey of adk-go-study, codex, go-swarm, nanobot, picobot, openhuman, elysia.

---

## 3. Architecture — one runner-owned `ResolveDirective`

`runner.SubmitAnswer` stops returning a bare `remaining int` and returns a typed directive computed **once**. Each channel switches on it and renders in its own idiom (the Go/TS boundary means rendering can't be shared — but the *decision* now is).

```go
// runner package
type ResolveOutcome int

const (
    OutcomeContinue   ResolveOutcome = iota // in-session: the channel drives its own continuation render
    OutcomeApproved                          // scheduled gate approved: render the deterministic "approved" confirmation
    OutcomeRejected                          // scheduled gate rejected: render the deterministic "rejected" confirmation
    OutcomePending                           // remaining>0: render the next FIFO pause, nothing else
    OutcomeTerminated                        // cancel: runner already auto-resolved; nothing to render
)

type ResolveDirective struct {
    Outcome   ResolveOutcome
    Remaining int
}

func (r *Runner) SubmitAnswer(ctx context.Context, token string, resp ResponseInput) (ResolveDirective, error)
```

**Why an enum, not a pre-rendered string:** the confirmation text is user-facing and localized (Telegram → Italian; the cockpit → react-i18next en+it). The runner emits a semantic *code*, honoring both the English-only-prompts rule (the runner never emits user prose) and the WebUI i18n. Each channel maps `OutcomeApproved`/`OutcomeRejected` to its own localized string.

**Classifier** (`classifyResolve(pending, resp, remaining)`, pure, runner-side):

| Condition | Directive |
|---|---|
| `resp.Action == cancel` | `{OutcomeTerminated, 0}` |
| `remaining > 0` | `{OutcomePending, remaining}` |
| pause is `scheduled_task_approval`, `accept` | `{OutcomeApproved, 0}` |
| pause is `scheduled_task_approval`, `decline` | `{OutcomeRejected, 0}` |
| else (clarification / choice / gateway approval) | `{OutcomeContinue, 0}` |

The runner still injects the English model-facing RoleTool marker (`scheduledApprovalAnswer`, "Operator APPROVED … do NOT reschedule") — that is the model's *future-context* record and is orthogonal to the user-facing directive; both now derive from the same single scheduled-approval detection helper (folds today's three duplicated short-id helpers into one).

`SubmitAnswers` (batch) keeps returning `(int, error)` — its only non-test caller is the AG-UI POST `/agent/run` `Resume[]` path, which resumes-and-continues inline and never needs a directive. The WebUI resolve endpoint (single-entry today) switches to `SubmitAnswer`.

---

## 4. Component changes

### 4a. Runner (`internal/runner`)
- `SubmitAnswer` returns `ResolveDirective`; add `classifyResolve` + fold the scheduled-approval detection (currently duplicated in `scheduledApprovalAnswer`) into one helper that yields both the model marker and the outcome code.
- `cancelConversation` returns `{OutcomeTerminated, 0}`.
- Watch the 600-LOC cap on `runner_resume.go` — split the classifier into `runner_resume_directive.go` if needed.

### 4b. Telegram (`internal/channels/telegram`) — collapse two callbacks into one
- **Delete** `scheduled_approval.go` in full (`aura_sappr`, `onScheduledApprovalCallback`, `resolveScheduled`, `scheduledApprovalMarkup`, `scheduledApprovalResolvedText`, the bespoke short-id) + `scheduled_approval_test.go`.
- **One** callback endpoint (keep `aura_hitl`) handles every pause. After `SubmitAnswer`, switch on the directive:
  - `OutcomeContinue` → `startTurn(...)` (existing in-turn fanout).
  - `OutcomeApproved`/`OutcomeRejected` → edit the message to the localized deterministic confirmation + disarm (the surviving behavior of the 1.7 outcome edit, now directive-driven and used by *every* delivery path).
  - `OutcomePending` → `promptPendingPause` (next FIFO).
  - `OutcomeTerminated` → disarm keyboard.
- **Unified markup + correct on-wire guard:** one `callbackData` builder that counts the telebot `\f<Unique>|` framing against the 64-byte cap (the correct 1.7 fix from `scheduled_approval.go`, applied to the single path — today `hitl.go`'s guard checks Data alone, which is looser than the real budget). Keep id masking.
- The cron sweep's `DeliverApproval` renders through the **same** markup builder as the in-turn relay.
- If `hitl.go` approaches 600 LOC after absorbing the richer rendering, split rendering into `hitl_render.go`.

### 4c. Telegram — enrich the native approval (no Mini App)
- **Formatted message:** kind + **bounded** goal summary + schedule + risk — secret-safe (reuse the server sanitizer; never dump the payload; bound length).
- **Multi-row inline keyboard:** Approva / Rifiuta / **Dettagli**. `Dettagli` edits the message to reveal the bounded detail (goal / schedule / next-run), still sanitized.
- **ForceReply** for the free-text/choice `ask_user` kinds (parity with the cockpit card's textarea + option buttons); choice options keep the index-in-callback-data trick under the on-wire guard.

### 4d. WebUI (Option B — endpoint returns the directive)
- `POST /api/approvals/{token}/resolve` returns **200 + `{outcome, remaining}`** (JSON projection of the directive) instead of 204. It calls `SubmitAnswer` (single) instead of `SubmitAnswers` with a one-entry map.
- `useResolveApproval` parses the body; `InlineApprovalCard` maps the server `outcome` onto its `CardState` (`approved`→answered/success, `rejected`→declined, `terminated`→cancelled) and re-drives the thread **only** on `continue` — replacing today's client-side inference from the action verb. Both channels now consume the same directive.
- i18n: reuse existing `approval.card.*` keys; add outcome-confirmation keys in en + it if not already present. Rebuild `internal/webui/dist` via the docker webbuild stage (Linux node — the web-dist-freshness rule).

---

## 5. Data flow (all three paths, one directive)

```
                       ┌─────────────────────────────────────────┐
  TG in-turn tap ─┐    │ runner.SubmitAnswer(token, {action})     │
  TG backstop tap ┼──▶ │   claim pause + append answer turn (tx)  │
  WebUI resolve  ─┘    │   applyResumeHook  (deterministic side-  │
  CLI repl       ─┘    │     effect: task status→active, etc.)    │
                       │   classifyResolve → ResolveDirective     │
                       └───────────────────┬─────────────────────┘
                                           ▼
        Continue → channel drives its own continuation render (startTurn / SSE re-drive / REPL loop)
        Approved/Rejected → channel renders its localized deterministic confirmation (no model turn)
        Pending → channel renders the next FIFO pause
        Terminated → channel disarms
```

A `scheduled_task_approval` resolves **identically** whether tapped in-turn, tapped from the sweep backstop hours later, resolved in the cockpit, or answered in the CLI — because the directive is a pure function of the pause, not the transport.

---

## 6. What gets deleted (dedup ledger)

- `internal/channels/telegram/scheduled_approval.go` + `_test.go` (the whole native duplicate).
- The bespoke Telegram outcome strings + the second callback endpoint `aura_sappr`.
- Three short-task-id helpers (`scheduledApprovalAnswer` inline, `serve_scheduled_approval.shortTaskID`, `telegram.shortTelegramTaskID`) → one.
- The looser Data-only 64-byte guard in `hitl.go` (superseded by the framing-inclusive guard).
- WebUI client-side action-verb → state inference (superseded by the server outcome).

---

## 7. Error handling
- `SubmitAnswer` errors surface as today: Telegram sends the submit-error notice (pause stays open, user retries); WebUI returns the sanitized HTTP error; REPL prints it.
- `ErrPauseNotFound` (unknown / already-resolved token) → Telegram toasts "già gestito" + disarms; WebUI 404. Unchanged.
- WebUI owner-gate (`GetByTokenForIdentity` → 403/404 split) is unchanged and runs before `SubmitAnswer`.
- Best-effort renders (message edit, keyboard disarm, continuation send) never wedge the resolve — the pause is already resolved in the DB.

---

## 8. Testing strategy
- **Runner unit (table-driven):** `classifyResolve` for every (kind × action × remaining) → expected directive; `scheduled_task_approval` accept→Approved / decline→Rejected; ordinary approval/choice/clarification→Continue; remaining>0→Pending; cancel→Terminated. Mutation spot-check ≥70% on the classifier.
- **Telegram unit:** the single callback dispatching each directive branch; unified markup builder (multi-row keyboard, `Dettagli`, ForceReply, on-wire length guard *counting the framing*); the deleted `scheduled_approval_test.go` cases folded in. Daemon-free (no live bot) so they count toward the coverage floor.
- **WebUI vitest:** card renders each server `outcome`; `useResolveApproval` parses the 200 body; re-drive only on `continue`; Stryker ≥70%, coverage ≥85% (web gates).
- **Integration (`db_integration`):** resolve a real `scheduled_task_approval` pause end-to-end → task row transitions + directive `Approved`/`Rejected`; ordinary pause → `Continue` + remaining semantics.
- **Live E2E (>9.8, real stack):** schedule an `agent_job` from Telegram **and** from the cockpit → both resolve through the one directive → richer native Telegram renders (kind + summary + multi-row + free-text where applicable) → **no loop, no duplicate**, behavior identical to the cockpit → all private, no endpoint added. Force the sweep backstop (delete the model pause) → sweep re-mints + delivers via the unified builder → resolves identically.
- Standing gates: 85% coverage floor on the `db_integration neo4j_integration` matrix (verify the stricter Skills-gate `db_integration`-only number), quality-snapshot re-attestation at phase close, no-skip-as-green.

---

## 9. Migrations & env
- **None.** The classifier reads existing `paused_states.resume_context`; no schema change, no new `AURA_*` knob, no new infra. The 1.7 throttle column and `AURA_SCHEDULER_APPROVAL_{REMINDER,GRACE}_SEC` are untouched.

## 10. Caller migration (blast radius)
- `SubmitAnswer` return type change: **6 production sites** — Telegram `hitl.go` (×3, collapses to fewer under the single callback), WebUI `approvals_api.go`, CLI `chat_repl.go`. The AG-UI `server_run.go` / `server_run_detach.go` use `SubmitAnswers` (batch, unchanged). Plus the runner/cmd test suite (~30 sites) read `directive.Remaining` — mechanical.

## 11. Risks & mitigations
- **In-turn UX change:** a scheduled-approval tap in a live chat no longer produces a model "Fatto!" turn — it shows the deterministic confirmation. Research-backed and *intended* (removes the reschedule-loop surface); called out for operator awareness. Ordinary clarification/approval continuation is unchanged.
- **Signature ripple:** contained by keeping `SubmitAnswers` batch stable and switching WebUI to the single `SubmitAnswer`; the test churn is mechanical.
- **LOC caps:** `runner_resume.go` and `hitl.go` may need on-touch splits (`runner_resume_directive.go`, `hitl_render.go`).
- **web-dist freshness:** rebuild `dist` on Linux node (docker webbuild) or the freshness gate fails.

## 12. Acceptance criteria
1. One runner `ResolveDirective`; `classifyResolve` is the single continuation/outcome decision. (unit + mutation)
2. One Telegram callback endpoint; `scheduled_approval.go` deleted. (grep-clean + unit)
3. Richer native Telegram approval: formatted secret-safe message + Approva/Rifiuta/Dettagli + ForceReply. (unit + live)
4. WebUI resolve returns the directive; the card renders it, re-drives only on `continue`. (vitest + live)
5. A `scheduled_task_approval` resolves identically across TG-in-turn / TG-backstop / cockpit / CLI. (integration + live E2E)
6. No loop, no duplicate; all private, no endpoint added; ≥9.8 real-scenario. Gates green.

## 13. Out of scope
- **Phase B** (independent): cockpit scheduler board detail panel + `aura task update` + edit form. Own brainstorm → spec → plan.
- Mini App / any public or VPN HTTPS endpoint (operator-excluded).
- The deferred minor findings from the next-phase doc (re-nudge cadence, reasoning-object wire bug, adaptive-reasoning no-op on llama.cpp).
