# Phase 35: ToolGateway + Policy Engine - Research

**Researched:** 2026-07-03
**Domain:** In-process policy-enforcement point (PEP) over the Go agent tool-dispatch loop; durable pre-execution reservation + idempotency over an existing append-only Postgres ledger; HITL approve routing
**Confidence:** HIGH (all cited seams verified on disk at HEAD; domain contract pre-locked in CONTEXT + deep-dive)

> **Scope of this document.** Phase 35's CONTEXT.md is already exhaustively research-backed (3 advisor-researchers + `senior-dev-agent-hardening-2026-tool-policy-gateway.md`; D-01..D-04 LOCKED). This RESEARCH.md does **not** re-derive the domain or re-open decisions. Its net-new value is concentrated in **(A)** the `## Validation Architecture` section (the Nyquist Dimension-8 source, lifted into VALIDATION.md) and **(B)** the `## Code-Verification Findings` — a line-cited reconnaissance of the live seams the CONTEXT cites, surfacing drift and landmines the planner must handle.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (verbatim intent — see 35-CONTEXT.md for full text)

- **D-01 — Durable reservation + idempotency: reuse `aura.tool_invocations` (0011), ZERO migration.** Verdict rides `meta jsonb` (not a new `ADD COLUMN`). D-01a: the reservation IS a synchronous, blocking `start` insert BEFORE `tool.Execute()` on the **mutating path only**; `start`∧¬`end`=in-flight, `start`∧`end`=committed. D-01b: idempotency key = the EXISTING UNIQUE index; the one load-bearing change is `InsertToolInvocation` `:exec`→`:execrows` (keep `DO NOTHING`): `rows==1`⇒reserve acquired, `rows==0`⇒replay the recorded `end` instead of re-invoking. D-01c: the real net-new seam is reservation-before-side-effect timing (today's write is async/observer-side). D-01d: crash-orphan reconciliation is APPEND-only + conservative (append an `end` fact, never UPDATE; NEVER blindly re-invoke a mutating orphan → mark indeterminate). D-01e: read-only "degrade per policy" writes a decision-fact only (no reservation).
- **D-02 — `Mutating` classification: hybrid floor + monotone de-escalation.** (a) `Mutating=true` on `skill`/`task`/`swarm_spawn` (fail-closed floor); (b) an in-process `classify(spec, rawArgs) → scoring.RiskTier` that de-escalates ONLY explicitly-enumerated read actions to `Safe`; everything unknown/unparseable saturates upward to `Risky`. `internal/scoring` untouched. D-02a ground-truth arg shapes. D-02b classifier table. D-02c landmine: `ComputeSkillTier` is mutation-only (returns `Risky` for `list`). D-02d: minimal `tools.Spec` touch (3 `Mutating:true` edits); optional `Multiplexed bool` hint + boot-guard.
- **D-03 — `approve` verdict: route by responder-presence.** Always emit+record the verdict (incl. terminal `executed(operator_id)` / `degraded_deny(no_approver)`). Interactive single-principal → `ErrAwaitingUserInput{Kind:approval}` + `{"type":"gateway_approval",…}` ResumeContext into the Phase-25 approval center; **resume MUST re-enter the gateway** (retake reservation + idempotency key). Headless (cron/swarm/detached) → deny-with-guidance (D-25 auto-reject; swarm child relays `needs_user_input` UP). D-03a: default DENY unless interactive responder positively known. D-03b: multi-identity interactive approval DEFERRED to Phase 36 → `approve` under `server_production` degrades to deny-with-guidance. D-03c: no model-facing `approve`.
- **D-04 — GATE-02 (command-hook fail-closed): verify-only + assert coverage.** Already satisfied; add/strengthen tests; NO new global env knob.

### Claude's Discretion
- Gateway package name/shape (`internal/gateway`, `Decide(ctx, principal, toolCall) → Verdict`) + composition-root injection points.
- Both `tool.Execute` call sites route through the single PEP; confirm no third path.
- Replay-fidelity contract: replayed preview is capped+redacted; sidecar may be GC'd → replay tolerates a missing sidecar (return preview + `result expired`), do NOT extend sidecar retention.
- Optional `Multiplexed bool` + `Registry.Validate`-style boot-guard (panic on unclassifiable mutating tool).
- Grace-window constant for D-01d (mirror `tmpTTL`); own-tx vs join-existing at the seam; exact `gateway_approval` wording.
- OTel span/metric on `Decide` + overhead assertion — nice-to-have; the observability *pack* is Phase 39.

### Deferred Ideas (OUT OF SCOPE — do NOT pull in)
- Multi-identity *interactive* approval routing → Phase 36 (MUSR-01..06 / Authula).
- Migration 0026 (idempotency/observability columns) + OTel metric path + `/readyz` → Phase 39.
- Additive `ALTER … ADD COLUMN decision text` → later, only if GATE governance needs SQL-queryable evidence.
- Sandbox routing (per-identity container) → Phase 37 (SBX-01..05).
- MCP governance limits → Phase 38.
- Verbatim exact-bytes replay (extend sidecar retention) → only if telemetry shows it matters.
- Declarative YAML/Rego/Cedar policy language + quorum approval → explicitly OUT (SUMMARY anti-feature; the Go table over `internal/scoring` is the locked minimal form).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description (REQUIREMENTS.md §GATE) | Research Support |
|----|-------------------------------------|------------------|
| **GATE-01** | Every tool call passes through one in-process policy decision (`allow`/`deny`/`approve`) recorded durably; no tool executes without a recorded decision. Table-driven over `internal/scoring`; fail-OPEN under dev/local_trusted, fail-CLOSED for mutating under hardened/production. *(F-001, F-020)* | Single PEP at `execTool` verified as the universal dispatch chokepoint (§Code-Verification 1); classifier table verified feasible from wire args (§CV 4); profile read from `config.Config.Profile` (§CV 6). |
| **GATE-02** | Command hooks default fail-CLOSED (or explicit `AURA_COMMAND_HOOK_FAIL_POLICY`); timeout/crash/non-zero cannot silently allow a denied command. *(F-006)* | **Already satisfied** — `commandHookFailPolicy` default `FailClosed` + `HookManager.hookFault` denies the turn under FailClosed (§CV 5). Phase 35 = verify + strengthen tests (D-04). |
| **GATE-03** | Mutating tools require a successful durable pre-execution ledger reservation under hardened/production; a failed reservation blocks the mutating tool; read-only tools degrade per policy. *(F-011, F-020)* | Reservation = synchronous `start` insert before `Execute`; the current write is async/best-effort/non-fatal and absent on headless paths (§CV 3). Net-new seam inside `execTool`. |
| **GATE-04** | Mutating tools carry an idempotency key (ConversationID+RequestID+ToolCallID); retries do not double-apply; the durable state machine supports recovery. *(F-020)* | Key = the existing UNIQUE index; `:exec`→`:execrows` change surface verified (§CV 2); replay SELECT is net-new query (§CV 2). |
</phase_requirements>

## Summary

Phase 35 places **one in-process policy decision on every tool call** at the `runTool → execTool → tool.Execute` seam and makes mutating-tool execution **fail-closed + durably reserved + idempotent** under the two strict runtime profiles, while remaining a **host-direct no-op** under `dev`/`local_trusted`. The domain is settled: this is an internal Go refactor that snaps a `Decide` interface into an existing dispatch chokepoint and promotes an already-append-only Postgres ledger into a reservation substrate — **no new external dependency, no new table, no migration**.

On-disk verification confirms the CONTEXT's seam map with high fidelity and surfaces a small number of load-bearing clarifications: (1) there are **exactly two** `tool.Execute` call sites and the second is `ask_user`-only (not a mutating bypass), so `execTool` is a sufficient single PEP; (2) the classifier landmine is real — `scoring.ComputeSkillTier("list")` returns `Risky`; (3) the `:exec→:execrows` change touches exactly one query with one caller; (4) the "reservation timing" problem is deeper than pure timing — the current ledger write is **non-fatal and decoupled from the execute decision, and is entirely absent on the swarm/cron headless paths**; and (5) a genuine landmine the CONTEXT under-states: the reservation key's `conversation_id` is a `uuid NOT NULL REFERENCES aura.conversations(id)`, but swarm children (`<conv>-swarm-w<i>`) and cron `agent_job:<runID>` run with **flat non-UUID sessions** and never write to the ledger today.

**Primary recommendation:** Build `internal/gateway` with a `Decide` interface holding the resolved `config.RuntimeProfile` + the existing `toolinvocations.Store`; inject it into all three `NewLlmAgent` composition roots (runner, swarm, cron); call it inside `execTool` (agent stays DB-free by delegating). Invest verification depth in: the monotone-de-escalation classifier (property + exhaustiveness + anti-`ComputeSkillTier`-for-reads tests), the reserve-before-side-effect gate (idempotency replay + fail-closed-blocks integration tier), and the append-only crash-orphan reconciler (mirror `sweeper.go`). Resolve the headless non-UUID `conversation_id` question (Open Q1) before planning the reservation waves.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Policy decision (allow/deny/approve) | `internal/gateway` (new, in-process) | `internal/agent` (call site) | GATE-01 wants ONE PEP; the agent delegates so it stays DB-free [VERIFIED: llm_agent_pause.go:5-6 "the agent stays DB-free"]. |
| Risk-tier classification | `internal/gateway.classify` (new) | `internal/scoring` (reused verbatim) | D-02: tier-assignment lives in the gateway table; `scoring` stays pure [VERIFIED: internal/scoring/scoring.go]. |
| Durable reservation + idempotency | `internal/toolinvocations` + Postgres | `internal/gateway` (orchestrates) | D-01: reuse the append-only ledger; the store owns the SQL, the gateway owns the reserve/replay decision. |
| Enforcement point (execute gate) | `internal/agent` `execTool` | — | The universal dispatch chokepoint; MCP/swarm-child/cron all funnel through some agent's `execTool` [VERIFIED: §CV 1]. |
| Approve → HITL routing | `internal/agent/tools` `ask_user` + `internal/agui` approvals + `internal/runner` | `internal/cron`/`internal/swarm` (headless) | D-03: reuse shipped primitives; route by responder-presence [VERIFIED: §CV 7]. |
| Command-hook fail-closed (GATE-02) | `internal/agent` `HookManager` + `CommandHook` | — | Already the default; verify-only [VERIFIED: §CV 5]. |
| Profile → fail-open/closed selection | `internal/config` `RuntimeProfile` | `internal/gateway` (consumer) | Profiles shipped in Phase 33; Phase 35 is the first *runtime* consumer [VERIFIED: config_runtimeprofile.go:18-19]. |

## Standard Stack

**No new external packages.** This is an internal refactor over the existing Go single-binary stack. Every dependency the phase needs is already in `go.mod` and in active use:

| Component | Already present | Purpose in this phase |
|-----------|-----------------|-----------------------|
| `github.com/jackc/pgx/v5` + `pgxpool` | yes (ledger, all stores) | the reservation INSERT / replay SELECT |
| `sqlc`-generated `internal/db/sqlc` | yes | regenerate `InsertToolInvocation :execrows` + a new `GetToolInvocationEnd :one` |
| `internal/scoring` | yes (pure risk tiers) | reused verbatim for the classifier's mutating branch |
| `internal/config.RuntimeProfile` | yes (Phase 33) | fail-open vs fail-closed selector |
| `internal/toolinvocations` | yes (append-only ledger + redaction) | reservation substrate |
| `go.uber.org/goleak` | yes (test tiers) | leak-clean reconciler + integration tiers |
| `google/uuid` | yes | request/reservation ids |

### Alternatives Considered
| Instead of | Could Use | Tradeoff — why NOT (locked) |
|------------|-----------|------------------------------|
| Go table over `internal/scoring` | OPA/Rego, Cedar, YAML policy language | Explicitly OUT (SUMMARY anti-feature; deep-dive YAML/Rego suggestion superseded). Over-engineering line. |
| Reuse `tool_invocations` (0011) | New `reservations` table + migration 0026 | D-01 ZERO-migration lock; the append-only ledger already carries the GATE-04 triple behind a UNIQUE index. |
| Synchronous reserve inside `execTool` (agent delegates to gateway) | Make the agent itself DB-aware | Breaks the DB-free-agent invariant; the injected `Decide` interface keeps the agent pure. |

**Installation:** none. `sqlc generate` (build step, not a dependency) after the query edits.

## Package Legitimacy Audit

**N/A — this phase installs no external packages.** All code is internal (`internal/gateway` new; `internal/agent`, `internal/toolinvocations`, `internal/scoring`, `internal/config` extended/reused). The Package Legitimacy Gate is satisfied vacuously (no registry surface to slopcheck).

## Architecture Patterns

### System Architecture Diagram (the single PEP + reservation flow)

```
model tool_call
      │
      ▼
LlmAgent.Run loop ── pauseCalls(ask_user only) ──► detectPause ──► tool.Execute [ask_user]  (control-plane; EXEMPT)
      │                                                                    │
      │  (all non-ask_user calls)                                          └─► ErrAwaitingUserInput ► pause Event
      ▼
dispatch ─► executeBatch ─► runToolRecovering ─► runTool ─┐
                                                          ▼
                                              ┌───────── execTool ─────────┐   ◄── THE SINGLE PEP (net-new Decide here)
                                              │ 1. gateway.Decide(profile, │
                                              │    tool.Spec(), rawArgs,    │
                                              │    key{conv,req,toolcall})  │
                                              │        │                    │
                                              │  dev/local_trusted ─► ALLOW (no-op, host-direct)   ── SC-4
                                              │        │ hardened/production                        │
                                              │  ┌─────┴──────┐                                      │
                                              │  │ read-only  │  mutating                           │
                                              │  │ decision-  │     │                               │
                                              │  │ fact only  │  Reserve(start) ──rows==1──► Execute│──► append end (async, best-effort)
                                              │  │ + ALLOW    │     │  rows==0 ──► replay recorded end (no re-invoke)  ── GATE-04
                                              │  └────────────┘     │  INSERT err ──► DENY, no Execute ── SC-3/GATE-03
                                              │                     │  verdict=approve ──► responder-present? ──► ask_user pause / deny-with-guidance ── D-03
                                              └─────────────────────┴──────┘
                                                          │
                                       (async observer)   ▼
                        runner.persistEvent ─► persistToolInvocationLedger ─► Store.Insert (start/end, best-effort, read-only path)
                                                          │
                        crash-orphan reconciler (mirror sweeper.go): start∧¬end older than grace ─► append end{status=error, indeterminate} ── D-01d
```

### Pattern 1: Single PEP by delegation (agent stays DB-free)
**What:** `execTool` calls an injected `gateway.Decide`; the gateway holds the `Store` + `RuntimeProfile`. The agent never imports a DB package.
**When:** every real tool dispatch (sequential + parallel + swarm-child + cron + MCP-bridged — all funnel through some agent's `runTool→execTool`).
**Why:** preserves the DB-free-agent invariant [VERIFIED: internal/agent/llm_agent_pause.go:5-6] while satisfying GATE-01's "every tool call."

### Pattern 2: Conditional-write IS the idempotency key (reuse Phase-34 D-03/D-06)
**What:** `INSERT … ON CONFLICT DO NOTHING` promoted to `:execrows`; `rows==1`⇒reserve, `rows==0`⇒replay. Byte-for-byte the `MarkPausedStateResumed` move Phase 34 already made.
**Example:** the existing query and its single conflict clause:
```sql
-- internal/db/queries/tool_invocations.sql (VERIFIED on disk)
-- name: InsertToolInvocation :exec        ← change to :execrows
INSERT INTO aura.tool_invocations (…21 cols…) VALUES (…)
ON CONFLICT (conversation_id, request_id, tool_call_id, event_kind) DO NOTHING;   ← the GATE-04 triple + event_kind
```

### Pattern 3: Monotone saturate-upward classifier (D-02)
**What:** default is always mutating; `classify` only *lowers* explicitly-enumerated read actions to `Safe`; unknown/empty/parse-fail → `Risky`.
**Anti-pattern it prevents:** routing a read through `scoring.ComputeSkillTier`, whose `default` branch returns `Risky` [VERIFIED: internal/scoring/scoring.go:126-136] — so `list` would gate. Reads must be allow-listed in the gateway table, never scored.

### Anti-Patterns to Avoid
- **Gating `ask_user` through the gateway** — it is the approval primitive; gating it risks recursion (approve→ask_user→Decide→approve). Exempt it (§CV 1).
- **A second durability subsystem** — reject a new reservations table; reuse the ledger (Phase-34 D-02/D-07 precedent).
- **Blindly re-invoking a mutating orphan** — its side effect may already have fired; mark indeterminate (D-01d).
- **Pushing policy into `tools.Spec`** — no `Classify` closure on the LLM-visible descriptor (D-02d); keep tier-assignment in the gateway.

## Code-Verification Findings

> The reconnaissance the objective requested. Every claim is line-cited at HEAD (2026-07-03). Drift and landmines flagged **⚠**.

### CV-1 — The single PEP seam: exactly TWO `tool.Execute` sites; no third bypass ✅
A whole-tree grep of `\.Execute\(` in `internal/` (non-test) returns **exactly two** hits [VERIFIED]:
- `internal/agent/llm_agent_retry.go:42` — `res, err = tool.Execute(ctx, args)` inside `execTool`. This is the **universal** path: `runTool` (llm_agent.go:530) → `execTool`; and the parallel path `executeBatch → runToolRecovering → runTool → execTool` [VERIFIED: llm_agent_parallel.go:95]. So **one Decide at `execTool` covers sequential + parallel dispatch, and every swarm-child/cron/MCP tool** (they all run through some agent's `execTool`).
- `internal/agent/llm_agent_pause.go:100` — `tool.Execute(ctx, …)` inside `detectPause`, reached **only** for `ask_user`: `pauseCalls` skips every call whose name ≠ `askUserToolName` [VERIFIED: llm_agent_pause.go:59]. A mutating tool can **never** reach this site.

**Finding:** `execTool` is a sufficient single PEP for the fail-closed-mutating guarantee. **Recommendation:** place `Decide` in `execTool` (it already receives `tool` + `mutating` + `args`); **exempt** the `ask_user` pause-detection path. If GATE-01's "*every* tool call recorded" is read literally, `ask_user` warrants a trivial `Safe` decision-fact — but gating it is circular and adds no safety (planner micro-decision; recommend exempt with a one-line rationale). ⚠ There is **no middleware layer around tool execution today** — `execTool` explicitly notes it closes that gap [VERIFIED: llm_agent_retry.go:16-19], so the gateway is the first interposition here.

### CV-2 — `InsertToolInvocation :exec → :execrows`: one query, one caller ✅
[VERIFIED: internal/db/queries/tool_invocations.sql:1,15] The query is `:exec` with `ON CONFLICT (conversation_id, request_id, tool_call_id, event_kind) DO NOTHING`. Only caller is `toolinvocations.Store.Insert` [VERIFIED: store.go:66], which discards the result. **Change surface:**
1. Flip to `:execrows` (keep `DO NOTHING`); sqlc regen makes `InsertToolInvocation` return `(int64, error)`.
2. `Store.Insert` (async caller) updates to ignore/log the count — behavior-preserving.
3. Add a `Reserve`-style method returning `rows==1/0` for the gateway.
4. ⚠ **Net-new query needed:** replay requires fetching a *single* `end` fact by key. `ListToolInvocationsByConversation` [VERIFIED: tool_invocations.sql:17] returns the whole conversation — too coarse. Add `-- name: GetToolInvocationEnd :one … WHERE conversation_id=$1 AND request_id=$2 AND tool_call_id=$3 AND event_kind='end'`.

### CV-3 — Reservation timing: deeper than "timing" ⚠ (D-01c clarified)
[VERIFIED: internal/runner/runner_persist.go:70-97,184-227] The current `start` fact is written by `persistToolInvocationLedger`, invoked from `persistEvent` on the **runner's observer side** when it consumes the agent's `ToolInvocation` start Event. Three properties make it unusable as a GATE-03 reservation, only one of which is "timing":
1. **Non-fatal / best-effort** — an insert failure is logged and the turn *continues* [VERIFIED: runner_persist.go:81-90 "must NOT abort the user-facing turn"]. GATE-03 requires the opposite (a failed reservation **blocks**).
2. **Decoupled from the execute decision** — `dispatch` yields the start Event then runs `executeBatch` regardless of whether the observer persisted it; the agent gets no reservation-acquired signal back [VERIFIED: llm_agent_dispatch.go:94-110].
3. **Absent on headless paths** — swarm `runChild` and cron `agentjob` drain their own event streams and **never call `runner.persistEvent`**, so their tool invocations are not laddered to the ledger at all [VERIFIED: swarm.go:186-199; agentjob.go:79-102].

**Finding:** the net-new seam is a **synchronous, fatal-on-failure `Reserve` INSIDE `execTool`** on the mutating path under a strict profile: `rows==1`→Execute; `rows==0`→replay; INSERT error→return deny, **no Execute**. The existing async observer write stays as-is for the read-only/observability path (D-01e). Because the reserve uses the same UNIQUE key, the later async `start` insert becomes a no-op (`rows==0`) — no double row [VERIFIED: the unique index makes the second insert a `DO NOTHING`].

### CV-4 — Classification ground-truth (D-02a/b/c) ✅ with one drift note
- `skillArgs{Action, Name, Query}` [VERIFIED: skill.go:84-88]; action enum = `list, info, use, create, update, delete, save_snippet, restore, archive` [VERIFIED: skill.go:112]. Matches D-02a exactly. ⚠ **Drift:** D-02b's table maps `install → ComputeSkillTier`, but `install` is **not** in the skill tool's action enum (discovery+install was deleted — amendment #51/D-40 [VERIFIED: skill.go:24-25]). Mapping `install` is harmless-but-dead; the live actions are the 9 listed.
- `taskArgs{Action, …, Kind, Payload, …}` [VERIFIED: task.go:88-100]; action enum = `schedule, list, cancel, run_now` [VERIFIED: task.go:110]; kind enum = `reminder, agent_job, backup_postgres, backup_neo4j` [VERIFIED: task.go:116]. Matches D-02a.
- `swarmSpawnArgs{Goals []string}` — **NO action field** [VERIFIED: swarm_spawn.go:48-50] ⇒ flat `Risky`, never de-escalated. Matches D-02a. (Also `Deferred:true` [VERIFIED: swarm_spawn.go:78].)
- **Landmine CONFIRMED (D-02c):** `ComputeSkillTier` has `default: return Risky` [VERIFIED: scoring.go:133-135] — feeding it `list` returns `Risky`. Reads MUST be allow-listed in the gateway, never scored. `rank(unknown)→rank(Risky)` [VERIFIED: scoring.go:58-65]. `GateRecommended = Risky||Destructive` [VERIFIED: scoring.go:140].
- **`agent_job` force-bump CONFIRMED needed:** `ComputeTaskTier` returns `Normal` for a plain worker `agent_job` (only a destructive-keyword payload or `reasoning` tier bumps it) [VERIFIED: scoring.go:83-121]; the tool itself already force-gates every `agent_job` to `pending_approval` (AG-016) [VERIFIED: task.go:206-213], so the gateway must replicate the `≥Risky` force for `kind=agent_job` — `scoring` alone won't.
- **Existing internal gating (coordination note):** `skill` create/update/delete and `task` schedule already run their own risk-tiered pause/approval internally [VERIFIED: skill.go:177-190; task.go:197-213]. The gateway's classification is **additive** (Mutating floor + reservation + completion-gate), not a re-implementation — do not double-gate.

### CV-5 — GATE-02 already satisfied (D-04) ✅
- `commandHookFailPolicy` default (empty/unset) → `FailClosed`; explicit `fail_open` opt-in; unknown → `FailClosed` + error [VERIFIED: hooks_command.go:167-176].
- Timeout → error [VERIFIED: hooks_command.go:311-313]; non-zero-exit with a parseable non-allow `deny` → honored; crashed `rewrite` → rejected (AG-030) [VERIFIED: hooks_command.go:315-320,329-334].
- The crux — `HookManager.hookFault`: FailClosed returns the error (turn dies); FailOpen contains it (allow) [VERIFIED: hooks.go:119-128]. `BeforeTool` propagates the abort [VERIFIED: hooks.go:186-210], and `dispatch` turns that into an infra error that ends the turn [VERIFIED: llm_agent_dispatch.go:65-66]. **⚠ Note (over-satisfaction):** this fail-closed behavior is **profile-independent** — it denies in *all* profiles, not just hardened/production. SC-2 only requires it under hardened/production, so the default over-satisfies; D-04 correctly says **no new env knob**. Phase 35 = strengthen tests only.

### CV-6 — Profile is on `config.Config`; `Strict()` is insufficient for D-03b ⚠
- `config.Config.Profile RuntimeProfile` holds the resolved profile [VERIFIED: config.go:57,362]. The 4 profiles + `ParseProfile` (total, unknown→dev) + `Strict()` (true for hardened+production) are shipped [VERIFIED: config_runtimeprofile.go]. `RuntimeProfile` explicitly "does NOT itself enforce any runtime capability (Tool Gateway / Phase 35+)" [VERIFIED: config_runtimeprofile.go:18-19] — Phase 35 is the first runtime consumer.
- **⚠ Finding:** `Strict()` lumps `single_user_hardened` and `server_production` together. For **fail-closed-mutating (GATE-03)** that is exactly right (both strict). But **D-03b** needs to distinguish them (`approve` routes interactively under hardened, **deny-with-guidance** under production until Phase 36). So the gateway must consume the **full `RuntimeProfile` enum**, not just `Strict()` — branch on `== ProfileServerProduction` for the approve-degradation rule.

### CV-7 — Approve/HITL routing seams all present (D-03) ✅
- `ErrAwaitingUserInput{Question, Options, Kind, Priority, ToolCallID, ResumeContext json.RawMessage, ProxiedFromChildID, ProxiedToolCallID}` [VERIFIED: ask_user.go:67-83]; `Kind=approval` exists [VERIFIED: ask_user.go:24]; `ResumeContext` is exactly where `{"type":"gateway_approval",…}` rides (skill_approval precedent noted in-code) [VERIFIED: ask_user.go:73-76].
- Approval center: `GET /api/approvals` → `ListPendingAll`; `POST /api/approvals/{token}/resolve` → `Runner.SubmitAnswers` (accept/decline/cancel) [VERIFIED: approvals_api.go:48-49,134].
- Headless cron auto-reject: `maxAutoRejects = 8`, `autoRejectMarker`, inject-and-continue loop [VERIFIED: agentjob.go:23,29,79-100]. Swarm child relays `needs_user_input` UP, never pauses in place [VERIFIED: swarm.go:160,196-199].

### CV-8 — Composition roots + the ⚠ headless non-UUID `conversation_id` landmine
Three `NewLlmAgent` sites need the `Gateway` injected [VERIFIED]:
1. `internal/runner/runner.go:540` (`buildAgent`) — covers CLI + cockpit + Telegram; `SessionID = convID` (a real conversation **UUID**, `session_id==conversation_id` D-26) [VERIFIED: runner.go:546]; `RequestID` minted into `ic.RequestID` [VERIFIED: runner.go:530,556].
2. `internal/swarm/swarm.go:167` (`runChild`) — `SessionID = "<conv>-swarm-w<i>"` **FLAT, non-UUID** [VERIFIED: swarm.go:173].
3. `internal/cron/handlers/handler.go:113` (`newAgentWorker`) — `SessionID = "agent_job:<runID>"` **non-UUID** [VERIFIED: agentjob.go:80 + milestone docs].

**⚠ Landmine (Open Q1):** the ledger's `conversation_id` is `uuid NOT NULL REFERENCES aura.conversations(id)` [VERIFIED: migrations/0011:7], and `Store.toParams` calls `uuidParam("conversation_id", …)` [VERIFIED: store.go:90] — a flat `<conv>-swarm-w2` will **fail to parse / violate the FK**. Swarm/cron mutating tools under hardened/production cannot take a reservation keyed on their own session. The main runner path is fine (UUID). See Open Questions.

**Reservation-key threading gap:** `execTool` has `ctx` (carrying `sessionID`(=convID) + `toolCallID` via `WithToolCallContext` [VERIFIED: result.go:19-31]) but **not** `request_id` (it lives in `ic.RequestID`, agent-loop level, not plumbed to the tool ctx). The gateway needs the full `{conv, req, toolcall}` triple → thread `request_id` into the tool context (extend `WithToolCallContext`) or pass it to `Decide` directly.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Idempotency / exactly-once | A bespoke reservation table + state machine | The existing UNIQUE index + `:execrows` `DO NOTHING` | The GATE-04 triple is already a unique constraint; conditional-write IS the key (Phase-34 D-06). |
| Append-only audit + secret redaction | A new ledger + redactor | `toolinvocations.Store` + `RedactForLedger` (8KiB args / 2KiB preview caps) | Redaction is already the persistence chokepoint [VERIFIED: store.go:142-146; redact.go:26-91]. |
| Risk tiers | A new severity enum | `internal/scoring` (`RiskTier`, `Compute*Tier`, `GateRecommended`, `rank`) | Pure, tested, unknown→Risky built in. |
| HITL approve UX | A new approval engine | `ask_user` + `paused_states` + Phase-25 approval center + D-25 auto-reject | Every primitive ships (§CV 7). |
| Crash-orphan reconciliation lifecycle | A new background worker from scratch | Mirror `conversations.Sweeper` (boot one-shot + interval, goleak-clean Start/Stop) | Proven leak-clean pattern [VERIFIED: sweeper.go]. |
| Command-hook fail-closed | A new fail-policy layer | Existing `FailPolicy`/`hookFault` | Already default fail-closed (§CV 5). |

**Key insight:** the "durable ledger" the roadmap imagined is satisfied by *promoting the append-only ledger Aura already writes* — one `:exec→:execrows` change + a synchronous pre-execution insert — not a new subsystem.

## Runtime State Inventory

> Feature-add phase (not a rename). Included because D-01 makes deliberate persistence-surface choices and the objective flagged the sqlc change surface.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data (schema) | **No new table, no migration** (D-01). Reservation rows land in existing `aura.tool_invocations`; the policy verdict rides existing `meta jsonb`. The `status` CHECK allows only `('ok','error')` [VERIFIED: migrations/0011:22] — see Pitfall 4. | none (schema); code writes new *rows* only |
| Stored data (migration) | **None.** No data migration; existing rows are untouched. | none |
| Live service config | None — no external service config embeds phase state. | none |
| OS-registered state | None. | none |
| Secrets/env vars | No new env knob (D-04 forbids one for GATE-02). Gateway reads existing `AURA_PROFILE` via `config.Config.Profile`. Optional grace-window/OTel knobs are Claude's Discretion. | none required |
| Build artifacts | ⚠ **sqlc regeneration required** — `InsertToolInvocation :execrows` + new `GetToolInvocationEnd :one` change generated `internal/db/sqlc/*.go`. Stale generated code after the query edit will not compile the new signatures. | `sqlc generate` as an explicit task; commit regenerated files |

**Nothing found in a category:** stated explicitly above ("None").

## Common Pitfalls

### Pitfall 1: Routing a read action through `scoring.ComputeSkillTier`
**What goes wrong:** `list`/`info`/`use` classify as `Risky` and gate (or block under fail-closed). **Root cause:** `ComputeSkillTier` is mutation-only; its `default` returns `Risky` [VERIFIED: scoring.go:133-135]. **Avoid:** allow-list read actions in the gateway table; only route `create/update/delete` to `ComputeSkillTier`. **Warning sign:** a `skill list` under hardened returns a deny/approve.

### Pitfall 2: A new multiplexed action silently under-gates
**What goes wrong:** an action added to `skill`/`task` (or a new multiplexed tool) that the classifier table doesn't enumerate falls through. **Avoid:** unknown/empty action → `Risky` (never `Safe`); add the optional `Multiplexed bool` + `Registry.Validate`-style boot-guard panic (D-02d) mirroring the existing `Register` panic idiom [VERIFIED: spec.go:111-117]. **Warning sign:** exhaustiveness test (below) fails.

### Pitfall 3: Treating the reservation as "just a timing fix"
**What goes wrong:** moving the async write earlier but keeping it non-fatal/decoupled — a failed reservation still lets Execute run (GATE-03 unmet). **Avoid:** the reserve must be synchronous, fatal-on-failure, and gate Execute on `rows==1`, inside `execTool` (§CV 3). **Warning sign:** SC-3 integration test (fail-closed-blocks) passes without a code change — it never actually blocked.

### Pitfall 4: Encoding "indeterminate" as a new `status` value
**What goes wrong:** the reconciler appends `end{status='indeterminate'}` → CHECK violation. **Root cause:** `status IN ('ok','error')` [VERIFIED: migrations/0011:22]; and the `end` shape requires `ended_at NOT NULL AND status NOT NULL` [VERIFIED: migrations/0011:32-42]. **Avoid:** append `status='error'` + an `indeterminate` marker in `error` text or `meta jsonb` — no schema change. **Warning sign:** the reconciler integration test errors on insert.

### Pitfall 5: Re-invoking a mutating crash-orphan
**What goes wrong:** the reconciler re-runs a mutating tool whose side effect already fired → double-apply. **Avoid:** mutating orphans → append indeterminate `end`, never re-invoke; only known read-only/idempotent tools may re-invoke (D-01d). This makes D-02 a hard prerequisite of D-01. **Warning sign:** a reconciler test shows a second Execute for a mutating tool.

### Pitfall 6: Expecting verbatim replay bytes
**What goes wrong:** the `rows==0` replay path asserts the original result and fails because the preview is capped (2KiB) + redacted, and the sidecar may be GC'd. **Avoid:** replay returns the recorded (redacted, capped) preview + a `result expired` marker when the sidecar is gone; do NOT extend sidecar retention (Claude's Discretion). **Warning sign:** replay test compares against pre-redaction bytes.

## Validation Architecture

> **This is the phase's primary deliverable — lifted into VALIDATION.md and it drives the planner's Nyquist Dimension-8 test requirements.** `workflow.nyquist_validation` is enabled (config.json). Aura's test discipline (CLAUDE.md): table-driven + `goleak` + `-race` + realistic fixtures; **mutation ≥70% on gateway/classifier/reservation files**; coverage **≥85% owned-surface**; **no-skip-as-green** (integration tiers `t.Fatal` under `$CI`); **≤600 LOC/file**.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + table-driven; `go.uber.org/goleak` (leak gate, already used by `toolinvocations` [VERIFIED: main_test.go]) |
| Unit tag | none (default) |
| Integration tag | `//go:build db_integration` — the existing tier for `toolinvocations`/`runner` [VERIFIED: store_integration_test.go:1] (Postgres only; no `neo4j_integration` needed) |
| Property/fuzz | Go `testing.F` fuzz **or** `gopter`/`rapid` (project has the `property-based-testing` skill) for the monotone-classifier invariants |
| Mutation | `go-mutesting` on WSL (only fork supporting go1.26); container-gated code adds `GOFLAGS=-tags=db_integration` + DSN env (CLAUDE.md) |
| Quick run | `go test -race ./internal/gateway/...` |
| Reservation tier | `go test -tags db_integration -race ./internal/gateway/... ./internal/toolinvocations/...` (live PG stack up) |
| Coverage gate | `make coverage` / `scripts/coverage_gate.sh` (owned-surface ≥85%; `internal/gateway` is owned) |
| Lint | `golangci-lint run` (=0; `dupl` enabled) |

### Phase Requirements → Test Map
| Req / SC | Behavior | Test Type | Automated Command | File (Wave 0 = net-new) |
|----------|----------|-----------|-------------------|--------------------------|
| GATE-01 / SC-1 | Every non-`ask_user` dispatch produces a recorded decision; no tool executes without one | unit + db_integration | `go test -race ./internal/gateway/...` ; `go test -tags db_integration -race ./internal/gateway/...` | ❌ `internal/gateway/decide_test.go`, `…_integration_test.go` |
| GATE-01 (classify) | Read actions de-escalate to `Safe`; unknown/empty/parse-fail → `Risky`; `swarm_spawn`→`Risky` | table unit | `go test -race ./internal/gateway/ -run Classify` | ❌ `internal/gateway/classify_test.go` |
| GATE-01 (classify) | **Property:** for arbitrary garbage args, `classify` never returns `Safe` for a non-enumerated action (saturate-upward invariant) | fuzz/property | `go test -race ./internal/gateway/ -run FuzzClassify` (or `-run Property`) | ❌ `internal/gateway/classify_property_test.go` |
| GATE-01 (classify) | **Exhaustiveness:** every action in the live `skill`(9)/`task`(4) enums maps to an explicit tier (no unexpected default) | table unit | `go test -race ./internal/gateway/ -run Exhaustive` | ❌ `classify_test.go` (drive off the tool schemas) |
| GATE-01 (classify) | **Anti-landmine:** reads do NOT flow through `ComputeSkillTier` (a test that FAILS if they did, since `list`→`Risky`) | unit | `go test -race ./internal/gateway/ -run ReadsNotScored` | ❌ `classify_test.go` |
| GATE-02 / SC-2 | Timeout / crash(non-zero) / non-zero-no-decision all DENY (turn aborts); tool never executes | unit | `go test -race ./internal/agent/ -run 'CommandHook.*(Timeout|Crash|Deny|FailClosed)'` | ⚠ **extend** `hooks_command_hardening_test.go`, `hooks_command_policy_internal_test.go` |
| GATE-02 | Explicit `fail_open` allows (contained) — proves the knob, not silent-allow | unit | `go test -race ./internal/agent/ -run CommandHookFailOpen` | ⚠ extend `hooks_policy_test.go` |
| GATE-03 / SC-3 | Under hardened/production, a mutating tool with a **failed** reservation is BLOCKED (Execute never called), returns deny | db_integration | `go test -tags db_integration -race ./internal/gateway/ -run ReservationFailBlocks` | ❌ `…_integration_test.go` (spy tool counting `Execute`) |
| GATE-03 | Reservation `start` is committed in PG **before** `Execute` runs (order proof) | db_integration | `go test -tags db_integration -race ./internal/gateway/ -run ReserveBeforeExecute` | ❌ same |
| GATE-04 | Duplicate key: 1st `rows==1`→Execute; 2nd `rows==0`→replay recorded `end`, Execute called **exactly once** | db_integration | `go test -tags db_integration -race ./internal/gateway/ -run IdempotentReplay` | ❌ same |
| GATE-04 | Replay tolerates a missing/GC'd sidecar (preview + `result expired`, no error) | db_integration | `go test -tags db_integration -race ./internal/gateway/ -run ReplayMissingSidecar` | ❌ same |
| D-01d | `start`∧¬`end` older than grace → reconciler appends `end{status=error, indeterminate}` (APPEND, not UPDATE); mutating orphan NOT re-invoked; in-grace orphan untouched | db_integration | `go test -tags db_integration -race ./internal/gateway/ -run Reconcile` | ❌ `…reconcile_integration_test.go` + goleak Start/Stop |
| SC-4 | Under `dev`/`local_trusted`, mutating tool runs host-direct, gateway writes **no** reservation row (no-op) | unit | `go test -race ./internal/gateway/ -run DevNoOp` | ❌ `decide_test.go` |
| D-03 | Interactive → `ErrAwaitingUserInput{Kind:approval}` with `gateway_approval` ResumeContext; resume re-enters the gateway (retakes reservation) | unit + db_integration | `go test -race ./internal/gateway/ -run Approve...` | ❌ `approve_test.go` |
| D-03b | `approve` under `server_production` degrades to deny-with-guidance (no interactive pause) | unit | `go test -race ./internal/gateway/ -run ApproveProductionDenies` | ❌ `approve_test.go` |

### Sampling Rate
- **Per task commit:** `go test -race ./internal/gateway/... ./internal/agent/... ./internal/toolinvocations/...` (quick, leak-checked).
- **Per wave merge:** full unit + `db_integration` tier on the live PG stack: `go test -tags db_integration -race ./internal/gateway/... ./internal/toolinvocations/... ./internal/runner/...`.
- **Phase gate:** `make quality-full` (vet+build+file-size+lint+race+vuln+coverage) green + **mutation ≥70%** on the gateway/classifier/reservation files (WSL `go-mutesting`, PASS=killed) + full-matrix coverage **≥85% owned-surface** + CI green + push. No-skip-as-green: the `db_integration` tier `t.Fatal`s under `$CI` when the DSN env is unset — a skipped tier fails the gate.

### Wave 0 Gaps
- [ ] `internal/gateway/` package created (`decide.go`, `classify.go`, `reserve.go`, `reconcile.go` — each ≤600 LOC) with its test files.
- [ ] `internal/gateway/decide_test.go`, `classify_test.go`, `classify_property_test.go`, `approve_test.go` (unit).
- [ ] `internal/gateway/gateway_integration_test.go`, `reconcile_integration_test.go` under `//go:build db_integration` + a `main_test.go` with `goleak.VerifyTestMain` (mirror `toolinvocations/main_test.go`).
- [ ] Shared realistic fixtures: a `Store`-backed reservation over the live PG stack + a spy `tools.Tool` that counts `Execute` calls (for idempotency/fail-closed proofs) + a live-conversation UUID seed (the local identity re-seed gotcha applies — FK 23503 if wiped).
- [ ] **Extend** existing GATE-02 tests (`hooks_command_hardening_test.go`, `hooks_command_policy_internal_test.go`, `hooks_policy_test.go`) with the timeout/crash/non-zero → deny + fail_open → allow matrix.
- [ ] sqlc regen check: a test (or CI grep) asserting `InsertToolInvocation` returns rows and `GetToolInvocationEnd` exists.

## Security Domain

> `security_enforcement` is not disabled in config → treated as enabled. This is *the* security-critical phase (the single PEP resolving "full host" vs "10/10").

### Applicable controls (ASVS-mapped, Go backend)
| ASVS Category | Applies | Standard Control in this phase |
|---------------|---------|--------------------------------|
| V4 Access Control | **yes** | The `Decide` PEP itself — default-deny mutating under strict profiles; deny-by-default for unknown classification. |
| V5 Input Validation | **yes** | The classifier parses **untrusted** model-supplied tool args (`skillArgs`/`taskArgs`); a parse failure must saturate to `Risky`, never `Safe` (D-02c). |
| V7 Error/Logging | **yes** | Append-only ledger + `RedactForLedger` secret redaction at the persistence chokepoint [VERIFIED: redact.go]; the verdict is recorded (GATE-01). |
| V6 Cryptography | no | No new crypto; do not hand-roll any. |
| V2/V3 Auth/Session | no (deferred) | Multi-identity principal resolution → Phase 36 (D-03b). |

### Threat patterns for the gateway (STRIDE)
| Pattern | STRIDE | Mitigation (in-phase) |
|---------|--------|------------------------|
| Model self-approves a gated action | Elevation of Privilege | No model-facing `approve` verdict (D-03c); approve is host/policy-side only. |
| New multiplexed action bypasses gating | Elevation of Privilege | Unknown action → `Risky`; boot-guard panic on unclassifiable mutating tool (D-02d). |
| Crash between approve and execute double-applies | Tampering | Persist-approval-before-act; resume re-enters the gateway to retake the idempotency key (D-03 step 2). |
| Reservation write failure silently allows | Spoofing/Repudiation | Fatal-on-failure reserve blocks Execute (GATE-03); the decision is durably recorded. |
| Secret on a shell command line lands in the un-deletable ledger | Information Disclosure | `RedactForLedger` runs before the durable column [VERIFIED: store.go:142-146]. |
| Ledger tampering / orphan cover-up | Tampering | Append-only triggers reject UPDATE/DELETE/TRUNCATE [VERIFIED: migrations/0011:54-68]; reconciler appends, never mutates (D-01d). |
| Headless run hangs on a mistaken pause | Denial of Service | Responder-presence default DENY; swarm relays up, cron auto-rejects (bounded) (D-03a). |

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Postgres (live stack) | `db_integration` reservation tier + coverage gate | ✓ (CLAUDE.md WSL/Docker stack) | 5432 `aura.*` | none — the tier `t.Fatal`s under `$CI` if unset (by design) |
| `go-mutesting` (WSL fork) | mutation ≥70% on gateway/classifier/reservation files | ✓ (`~/go/bin`, WSL) | go1.26-compatible fork | run untagged subset if DSN unavailable, then re-run gated |
| `sqlc` | regenerate the `:execrows` + replay query | ✓ (build toolchain) | pinned | none |
| `golangci-lint` | lint=0 gate | ✓ (v2.12.2 CI-pinned) | — | — |
| `neo4j` | — | n/a | — | not needed (PG-only phase) |

**Missing dependencies with no fallback:** none. **Note:** re-seed the `local` identity before the `db_integration` tier (FK 23503 if a parallel/coverage run wiped `...001`) — the documented re-seed gotcha applies to any tier inserting `tool_invocations` rows (which FK to `conversations`→`identity`).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `ask_user` should be **exempt** from the gateway (control-plane, non-mutating, approve-target) rather than recorded with a `Safe` decision | CV-1 | Low — if GATE-01 is read literally, add a trivial `Safe` decision-fact; either way no mutating bypass exists. |
| A2 | For headless swarm/cron mutating tools, the durable reservation should key on the **parent conversation UUID** (or be scoped out with a documented rationale) rather than the flat session | CV-8 / Open Q1 | **Medium/High** — determines whether GATE-03 is enforceable on headless paths; must be resolved before reservation waves. |
| A3 | `go-mutesting` can reach ≥70% on the new gateway files with the DSN-gated tier | Validation | Medium — some reserve/replay branches only exercise under `db_integration`; mutation must run with `GOFLAGS=-tags=db_integration`. |
| A4 | `session_id == conversation_id` holds as a parseable UUID wherever a reservation actually runs (main runner path) | CV-8 | Low — verified for `runner.buildAgent`; the risk is confined to the headless paths (A2). |

**If this table looks short:** the domain is pre-locked; these are the residual *implementation* assumptions, not domain uncertainty.

## Open Questions

1. **Headless reservation key (the top blocker).** Swarm children (`<conv>-swarm-w<i>`) and cron `agent_job:<runID>` run with flat non-UUID sessions and never write to `tool_invocations` today (CV-8), yet the gateway is injected there and GATE-01 says swarm tools pass the policy decision.
   - *What we know:* the ledger `conversation_id` is a UUID FK; the main runner path is a real UUID; the async ledger write is absent on headless paths.
   - *What's unclear:* whether GATE-03's **durable reservation** must cover headless mutating tools in Phase 35, and if so, what `conversation_id` it uses.
   - *Recommendation:* the minimal-industrial resolution is to key the reservation on the **originating conversation UUID** (swarm `RunConfig.ConvID`; cron `CreateTaskInput.OriginConversationID` [VERIFIED present: task.go:66-70]) with the child's `request_id`/`tool_call_id`. If that plumbing is out of scope, **scope GATE-03 reservation enforcement to the interactive runner path** for Phase 35 (still emit+record the GATE-01 *decision* on headless paths) and document the deferral — the sandbox that will contain headless capability is itself Phase 37. **Decide in planning before writing the reserve seam.**

2. **`request_id` threading to `execTool`.** The reservation triple needs `request_id`, which is not on the tool ctx (CV-8). Extend `WithToolCallContext` to carry it, or pass the triple to `Decide` from `runTool` (which would need `ic.RequestID` threaded down). Small, but touches a signature — pick the shape in planning.

3. **`ask_user` decision recording (A1).** Exempt vs record-a-Safe-fact. Recommend exempt; confirm the literal reading of GATE-01 with the planner/discuss.

4. **Grace-window constant for D-01d.** Mirror `tmpTTL`(24h) [VERIFIED: orphan_scan.go:21] or a shorter reservation-specific window (a stuck in-flight mutating call for 24h is a long time to show as in-flight). Claude's Discretion — recommend a smaller constant (e.g. 15–60 min) since a reservation orphan is a crash signal, not a scratch file, but keep it a named const mirroring the `sidecarOrphanGrace` idiom.

## Sources

### Primary (HIGH confidence — verified on disk at HEAD 2026-07-03)
- `internal/agent/llm_agent.go`, `llm_agent_retry.go`, `llm_agent_pause.go`, `llm_agent_parallel.go`, `llm_agent_dispatch.go` — the PEP seam + the two `Execute` sites.
- `internal/agent/tools/spec.go`, `skill.go`, `task.go`, `swarm_spawn.go`, `ask_user.go`, `result.go` — Mutating flag, arg shapes, sentinel, tool ctx.
- `internal/scoring/scoring.go` — classification landmine + tiers.
- `internal/toolinvocations/store.go`, `redact.go`; `internal/db/queries/tool_invocations.sql`; `internal/db/migrations/0011_*.up.sql`, `0016_*.up.sql` — ledger + `:exec`/`DO NOTHING` + CHECK constraints + append-only triggers.
- `internal/runner/runner_persist.go`, `runner.go` — async best-effort write; agent construction + request id.
- `internal/agent/hooks.go`, `hooks_command.go` — GATE-02 fail-closed.
- `internal/config/config_runtimeprofile.go`, `config.go` — profiles + `Strict()`.
- `internal/conversations/sweeper.go`, `orphan_scan.go` — age-grace reconciler pattern + `tmpTTL`.
- `internal/swarm/swarm.go`, `internal/cron/handlers/agentjob.go`, `internal/agui/approvals_api.go` — headless roots + approve seams.
- `internal/toolinvocations/main_test.go`, `store_integration_test.go` — the `db_integration` + goleak test harness.

### Secondary (the pre-locked domain contract — read, not reproduced)
- `.planning/phases/35-toolgateway-policy-engine/35-CONTEXT.md` — D-01..D-04, canonical_refs, code_context.
- `docs/research/senior-dev-agent-hardening-2026-tool-policy-gateway.md` — 5-point plan, Temporal durable-execution idempotency, anti-over-engineering checklist (YAML/Rego suggestion SUPERSEDED).
- `.planning/research/SUMMARY.md` §ToolGateway — the one-in-process-Decide locks; Pitfall 3 over-engineering line.
- `.planning/REQUIREMENTS.md` §GATE, `.planning/ROADMAP.md` §Phase 35 — GATE-01..04 + SC-1..4.
- `.planning/phases/34-*/34-CONTEXT.md` — D-02/D-03/D-06/D-07 precedents (reuse-tx, `:exec→:execrows`, conditional-write-is-idempotency-key).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new packages; every dependency verified in-tree.
- Architecture / seams: HIGH — every cited seam verified at file:line; the single-PEP and reservation-timing findings are code-grounded.
- Classification: HIGH — arg shapes + the `ComputeSkillTier("list")→Risky` landmine confirmed on disk.
- Validation Architecture: HIGH — mapped to the existing `db_integration`+goleak harness and Aura's gates.
- Headless reservation key (Open Q1): MEDIUM — the landmine is confirmed; the resolution is a planner decision, not yet locked.

**Research date:** 2026-07-03
**Valid until:** ~2026-08-03 for the domain contract; the code-verification findings are valid until the cited files change (re-verify line numbers if `internal/agent` or `internal/toolinvocations` are refactored before planning).
