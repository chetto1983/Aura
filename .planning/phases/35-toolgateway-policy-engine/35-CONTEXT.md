# Phase 35: ToolGateway + Policy Engine - Context

**Gathered:** 2026-07-03
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver **one in-process policy decision on every tool call** (`allow`/`deny`/`approve`), recorded durably; **fail-CLOSED for mutating tools** under `single_user_hardened`/`server_production`; a **durable pre-execution reservation + idempotency key** for mutating tools; and a **no-op (fail-open, host-direct) under `dev`/`local_trusted`** so the operator's daily full-host experience is unchanged. Requirements **GATE-01..04** (F-001 gateway, F-006, F-011, F-020).

Concretely: (1) a single `Decide` policy-enforcement point at the existing `runTool → execTool → tool.Execute` seam, **table-driven over `internal/scoring` risk tiers**; (2) a trustworthy `Mutating` classification for the action-multiplexed tools deferred from Phase 34; (3) reuse of the append-only `aura.tool_invocations` ledger as the durable reservation + idempotency substrate (no new table, no migration); (4) the `approve` verdict routed to Aura's already-shipped HITL machinery by responder-presence; (5) confirmation/hardening of the already-shipped command-hook fail-closed default (GATE-02).

This discussion captured only **HOW**, not WHAT — GATE-01..04 are tightly locked in `.planning/REQUIREMENTS.md`. It was **research-backed** (3 parallel advisor-researchers: 2026 industrial patterns online + curated `D:/tmp` repos + the live Aura code, at the user's explicit request) and every decision converged on the **minimal industrial form** — the same "no atomic bombs" discipline as Phase 34, and largely a *continuation* of Phase-34's D-02/D-06/D-07 (reuse the existing durability seam; the conditional/unique write IS the idempotency key; do not add a second durability subsystem).

**Locked upstream (NOT re-opened here):**
- Gateway = **one in-process Go function**, no external process, **no OPA/Rego/Cedar, no RBAC** (research SUMMARY §Pitfall 3 "over-engineering line"; the deep-dive's YAML/Rego suggestion is superseded by the milestone's "table-driven over `internal/scoring`" lock). Latency fear is empirically false (~8.3 ms median; Agent-OS <0.1 ms p99, Bifrost +11 µs are the bar).
- Profile-gated: fail-OPEN/host-direct no-op under `dev`/`local_trusted`; fail-CLOSED for mutating under hardened/production. The 4 profiles + validator shipped in **Phase 33** (`internal/config`).

**It does NOT deliver** (scope fence): the per-identity full-capability **sandbox** routing (SBX-01..05 → **Phase 37**); **multi-user identity isolation / owner-scoped stores / Authula cutover** (MUSR-01..06 → **Phase 36**) — including multi-identity *interactive* approval routing; idempotency/observability **migration 0026** (OBS → **Phase 39**); MCP governance limits (**Phase 38**). See Deferred Ideas.

</domain>

<decisions>
## Implementation Decisions

### D-01 — Durable reservation + idempotency: reuse `tool_invocations`, ZERO migration (GATE-03/04)
- **Decision:** Reuse the existing append-only `aura.tool_invocations` ledger (migration `0011`) as the durable reservation substrate. **No new table, no migration 0026 in this phase.** The policy verdict rides the existing `meta jsonb` (chosen over an additive `ADD COLUMN decision text` — the verdict is needed for replay/forensics, not first-class SQL-queryable governance evidence yet; the ALTER remains a clean drop-in later if that changes).
- **D-01a — the reservation IS the `start` fact, made pre-execution.** For a **mutating** tool, the gateway performs a **synchronous, blocking `start` insert BEFORE `tool.Execute()`**. The `start` row is the reservation; the `end` row (already carrying `status`/`result_preview`/`result_sidecar_path`/`exit_code`) is the recorded outcome. Reservation state is implicit/event-sourced: `start`∧¬`end` = in-flight; `start`∧`end` = committed/aborted.
- **D-01b — idempotency key = the EXISTING UNIQUE index.** `tool_invocations_once_per_phase_idx (conversation_id, request_id, tool_call_id, event_kind)` already encodes the GATE-04 triple. **The one load-bearing change:** promote `InsertToolInvocation` from `:exec` / `ON CONFLICT DO NOTHING` to **`:execrows`** (keep `DO NOTHING`): `rows==1` ⇒ reservation acquired, proceed; `rows==0` ⇒ already held ⇒ SELECT the `end` fact and **replay the recorded outcome instead of re-invoking**. This is byte-for-byte the `:exec→:execrows` move Phase-34 **D-03** already made for `MarkPausedStateResumed`, and the direct continuation of **D-06** ("rows-affected IS the idempotency key").
- **D-01c — the REAL work is reservation-before-side-effect timing.** Today the `start` event is persisted **reactively** through the runner's async event stream (`persistToolInvocationLedger`) — it is **NOT guaranteed committed before `Execute()` runs the side effect**. GATE-03 requires the reservation durable in Postgres **before** the mutating `Execute()`. The gateway adds a synchronous blocking `Store.Insert(start)` on the mutating path; the async event-stream write stays as-is for read-only/observability. This is the core net-new seam, not a subsystem.
- **D-01d — crash-orphan reconciliation is APPEND-only + conservative.** A `start`-without-`end` older than a grace window is reconciled by mirroring F-040's `orphan_scan.go`/`sweeper.go` age-grace pattern. Append-only ⇒ you cannot `UPDATE` the orphan to "aborted"; you **append an `end` fact** with `status=error`/indeterminate. The reconciler must **NOT blindly re-invoke a mutating orphan** (its side effect may already have fired) — mutating orphans are marked indeterminate for the model/operator; only known read-only/idempotent tools re-invoke. **This makes D-02 (trustworthy `Mutating`) a hard prerequisite of D-01.**
- **D-01e — read-only "degrade per policy" writes a decision-fact only** (no reservation, no `start`/`end` side-effect anchor). Keep the reserve→execute→append-outcome machinery on the **mutating path only** — do not tax every read with a durable write. ("Default-deny destructive; everything else allow + log.")

### D-02 — `Mutating` classification for action-multiplexed tools: hybrid floor + de-escalation (GATE-01/03)
- **Decision:** Close the Phase-34-deferred gap with a **hybrid**: (a) set `Mutating=true` on `skill`/`task`/`swarm_spawn` (the fail-closed floor — also fixes the completion-gate D-43 consumer); (b) a small in-process gateway classifier `classify(spec, rawArgs) → scoring.RiskTier` that **de-escalates ONLY explicitly-enumerated read actions** to `Safe`; everything unknown/unparseable **saturates upward to `Risky`**. The default is always mutating; the classifier is a monotone de-escalator. `internal/scoring` is **untouched** (reused verbatim).
- **D-02a — ground-truth arg shapes (from the code):** `skill` carries `action` (enum: `list, info, use, create, update, delete, save_snippet, restore, archive`); `task` carries `action` (`schedule, list, cancel, run_now`) + `kind`/`payload`; **`swarm_spawn` has NO action field** — only `goals []string` ⇒ **flat `Risky`, never de-escalated** (it spawns full-capability worker turns; parity with the AG-016 rule that any `agent_job` saturates upward).
- **D-02b — classifier table (reuses `scoring`):**
  - `skill`: `list/info/use` → `Safe`; `restore/archive/save_snippet` → `Normal`; `create/update/install/delete` → **`scoring.ComputeSkillTier`** (→ Risky/Destructive); anything else/empty/unmarshal-error → **`Risky`**.
  - `task`: `list` → `Safe`; `cancel` → `Normal`; `run_now` → `Risky`; `schedule` → **`scoring.ComputeTaskTier`** then force `≥Risky` when `kind=agent_job` (AG-016 parity); else/empty/error → **`Risky`**.
  - Non-multiplexed (incl. all MCP tools) → coarse map of `spec.Mutating` (`shell_exec`/`fs_write` and MCP `!ReadOnlyHint` already correct).
- **D-02c — landmine:** `scoring.ComputeSkillTier` is **mutation-only** — feeding it `list` returns `Risky` (its `default` branch). Do NOT route reads through it; the read-action allowlist lives in the gateway table. Parse failure / missing action / empty string → `Risky`, never `Safe` (the whole safety argument).
- **D-02d — `tools.Spec` touch:** minimal — the three `Mutating:true` edits only. Do **not** add a `Classify` closure to `Spec` (pushes policy into the LLM-visible descriptor). An **optional** `Multiplexed bool` hint is defensible *only* to power a boot-time guard (see Claude's Discretion). Keep tier-assignment (`classify`) and enforcement (profile via `scoring.GateRecommended`) separated, matching the pure-`scoring` discipline.

### D-03 — `approve` verdict runtime: route by responder-presence (GATE-01)
- **Decision:** The gateway **routes** an `approve` verdict; it builds **no new approval UX**. Every primitive already ships. Behavior:
  1. **Always emit + durably record** the verdict (GATE-01 "no tool executes without a recorded policy decision"), including the *terminal* decision: `approve → executed(operator_id)` or `approve → degraded_deny(reason=no_approver)`.
  2. **Interactive session positively known** (cockpit SSE subscriber attached / live CLI REPL / live Telegram chat, **single principal**) → emit the existing `ErrAwaitingUserInput{Kind: approval}` with a `{"type":"gateway_approval",...}` ResumeContext; reuse the Phase-25 approval center (`GET /api/approvals` + `POST …/resolve` → `Runner.SubmitAnswers` accept/decline/cancel), **persist-before-act**. **The resume MUST re-enter the gateway** so the now-approved call still takes its GATE-03 reservation + GATE-04 idempotency key (else a crash between approve and execute double-applies).
  3. **Headless / no positively-known responder** (cron `agent_job`, swarm child, detached run) → **deny-with-guidance** — the shipped D-25 auto-reject pattern; **never pause in place**. A swarm child **relays `needs_user_input` UP** (as `runChild` already does), never pauses. Bound any model re-ask loop like the existing `maxAutoRejects=8`.
- **D-03a — default is DENY unless an interactive responder is positively known** (responder-presence detection is the crux; a mistaken pause of a headless run hangs it, violating the "never blocks" success criterion).
- **D-03b — multi-identity interactive approval is DEFERRED to Phase 36.** Under `single_user_hardened` there is exactly one principal (the owner) ⇒ interactive reuse is trivially correct. Under `server_production` with multiple identities, "surface the pause to the *right* operator" needs MUSR-01..06 owner-scoping ⇒ scope `approve` to **deny-with-guidance under `server_production` until Phase 36 lands**. Do not honor an interactive Telegram approval as an operator decision under production yet (identity unverified pre-36).
- **D-03c — no model-facing `approve`.** The `approve` verdict is host/policy-side; keep it off the tool schema so the model can't self-approve (parity with skills D-03 "activation is human-only"). Reuse `skillApprovalPriority(tier)` so a security approval isn't buried behind chit-chat in the shared `paused_states` FIFO.

### D-04 — GATE-02 (command-hook fail-closed): verify-only + assert coverage
- **Decision:** GATE-02 is **already satisfied** — `commandHookFailPolicy` (`internal/agent/hooks_command.go`) defaults to `FailClosed`, honors a per-hook JSON `fail_policy`, and rejects a crashed rewrite (AG-030). The requirement's "…*or* require an explicit `AURA_COMMAND_HOOK_FAIL_POLICY`" is an `or` the default already meets. Phase 35 **adds/strengthens tests** proving timeout / crash / non-zero-exit all DENY and cannot silently allow a denied command, and pins coverage. **No new global env knob** (avoid config surface for a requirement already met).

### Claude's Discretion
- Gateway package name/shape (research suggested `internal/gateway` with a `Decide(ctx, principal, toolCall) → Verdict` interface) and where the composition root injects it (serve + cron + telegram + swarm — wherever the runner/agent is built).
- **Both** tool-`Execute` call sites must route through the single PEP: `internal/agent/llm_agent_retry.go:42` (`execTool`) **and** `internal/agent/llm_agent_pause.go:100`. Confirm no third path bypasses it.
- **Replay-fidelity contract** (landmine): the replayed `end` preview is capped + secret-redacted (`RedactForLedger`), and the exact-bytes sidecar (`result_sidecar_path`) may be GC'd by F-040's age-grace sweep. **Default:** replay tolerates a missing sidecar → return preview + a `result expired` marker; do NOT extend sidecar retention to chase verbatim replay. Revisit only if telemetry shows it matters.
- Optional `Multiplexed bool` on `tools.Spec` + a `Registry.Validate`-style **boot-guard** that panics if a tool the gateway cannot classify to a concrete tier still has `Mutating=false` — turns the "new unlisted multiplexed tool silently under-gates" landmine into a loud wiring panic (the codebase's established `Register`-panic idiom). Recommended but not required.
- Exact grace-window constant for D-01d (mirror `tmpTTL`); whether the `start` insert opens its own tx or joins an existing one at the seam; the precise `gateway_approval` ResumeContext wording.
- OTel span / metric on the `Decide` call + overhead assertion (Bifrost +11 µs / Agent-OS <0.1 ms p99 as the bar) — nice-to-have; the observability *pack* is Phase 39.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (authoritative WHAT)
- `.planning/REQUIREMENTS.md` §"ToolGateway, Policy & Ledger (GATE)" — **GATE-01..04** exact text + linked findings.
- `.planning/ROADMAP.md` §"Phase 35: ToolGateway + Policy Engine" — goal + 4 success criteria (esp. SC-4 "no-op under dev/local_trusted").

### Primary research (the 2026 industrial-pattern contract — MUST read)
- `docs/research/senior-dev-agent-hardening-2026-tool-policy-gateway.md` — the whole thing: §"TL;DR for Aura" (5-point plan), §2 Temporal durable-execution (idempotency = stable key + recorded outcome, replay on retry; persist-approval-before-act), §"Promote migration 0011 into a true action ledger", §"Anti-over-engineering checklist". **Note:** its YAML/Rego policy-language suggestion is SUPERSEDED by the milestone's "table-driven over `internal/scoring`" lock.
- `.planning/research/SUMMARY.md` — §"ToolGateway" locks (line 38/46/53/67): one in-process `Decide` at the `runTool → execTool → tool.Execute` seam, table-driven over `internal/scoring`, coexists with HookManager + completion-gate + `tool_invocations` ledger, no tool changes; Pitfall 3 = the over-engineering line.

### Audit findings (the contract — `docs/audit/bug-report.md`)
- **F-001** (ToolGateway = the single PEP resolving "full host" vs "10/10"), **F-006** (command-hook fail-closed — GATE-02), **F-011** (mutating-tool durable pre-execution reservation — GATE-03), **F-020** (idempotency key + durable state machine — GATE-04). Cross-check `docs/audit/industrialization-roadmap.md` (dependency order: ledger → gateway → idempotency).

### Prior-phase context (scope boundary + deferral — this phase closes it)
- `.planning/phases/34-agent-loop-correctness-durable-ledger/34-CONTEXT.md` — **D-02/D-06/D-07** (reuse-the-tx / conditional-write-IS-idempotency-key / reject a second durability subsystem — the precedent D-01 continues); **D-03** (`:exec→:execrows` precedent for D-01b); `<decisions>` D-01a/b note the `skill`/`task`/`swarm_spawn` mutating-classification gap; `<deferred>` "Complete the tool `Mutating` classification … action-aware model → Phase 35" + "Mutating-tool durable ledger reservation (F-011/GATE-03/04) → Phase 35" (exactly this phase).
- `.planning/phases/33-runtime-profiles-config-validation/33-CONTEXT.md` — the 4 runtime profiles + `ValidateProfile` the gateway reads to decide fail-open vs fail-closed.

### Code to extend / mirror (read before writing)
- **The seam (single PEP):** `internal/agent/llm_agent.go` (`runTool` L497 → `execTool` L530), `internal/agent/llm_agent_retry.go` (`execTool` — Execute call site + the "no middleware around tool execution" note), `internal/agent/llm_agent_parallel.go` (`runToolRecovering`, `executeBatch`), `internal/agent/llm_agent_pause.go` (**second** Execute call site, L100), `internal/agent/llm_agent_events.go` (`toolStartEvent`), `internal/agent/tools/spec.go` (`Mutating` flag; the 3 edits).
- **Ledger / reservation:** `internal/db/migrations/0011_tool_invocations.up.sql` (append-only table + UNIQUE idempotency index + anti-mutation triggers), `internal/db/migrations/0016_tool_invocations_allow_cascade.up.sql` (the one FK-cascade exception carved into the append-only invariant — don't re-derive), `internal/db/queries/tool_invocations.sql` (`InsertToolInvocation :exec`/`DO NOTHING` → `:execrows`; add an `end`-fact replay SELECT), `internal/toolinvocations/store.go` (`Insert`, `RedactForLedger`/WR-02, replay-fidelity), `internal/runner/runner_persist.go` (`persistToolInvocationLedger` — today's async write; the synchronous pre-execution reserve is the net-new seam beside it), `internal/conversations/orphan_scan.go` + `sweeper.go` (`tmpTTL` age-grace to mirror for D-01d).
- **Classification:** `internal/scoring/scoring.go` (`RiskTier`, `ComputeSkillTier`, `ComputeTaskTier`, `GateRecommended`, `rank` — reuse verbatim; note unknown-tier→Risky is built in), the multiplexed-tool arg shapes in `internal/agent/tools/skill*.go` / `task*.go` / `swarm_spawn*.go`, `internal/agent/mcptools/bridge.go` (`Mutating = !ReadOnlyHint` — confirms non-multiplexed coverage).
- **Approve / HITL:** `internal/agent/tools/ask_user.go` (`ErrAwaitingUserInput{Kind, ResumeContext}`), `internal/agent/tools/skill_write.go` (Phase-29 risk-tiered pause + `skillApprovalPriority`), `internal/agui/approvals_api.go` (`GET /api/approvals` + `POST …/resolve` → `SubmitAnswers`), `internal/askuser/` + `internal/runner/runner_resume.go` (crash-recoverable resume), `internal/cron/handlers/agentjob.go` (D-25 headless auto-reject, `maxAutoRejects`), `internal/swarm/swarm.go` (`runChild` relay-up), `internal/channels/telegram/agui_subscriber.go` (pause rendering), `internal/config/config_runtimeprofile.go` (profile → gateway mode).
- **GATE-02:** `internal/agent/hooks_command.go` (`commandHookFailPolicy`, `rejectCrashedRewrite`), `internal/agent/hooks.go` (`FailPolicy`/`RegisterWithPolicy`), existing `hooks_command_hardening_test.go` / `hooks_command_policy_internal_test.go` / `hooks_policy_test.go`.

### External precedents (`D:/tmp` + web — inspected during research)
- `D:/tmp/adk-go-study/session/database/service.go` — `applyEvent` single-tx event-append + state-update, no ledger (the reuse-over-new-subsystem precedent). `D:/tmp/codex` git tree — `*RequestApprovalParams/Response` schemas + `assess_command_safety` (default-unsafe, allowlist-safe → the D-02 monotone-de-escalation precedent; note codex only ever "wire a real approval" OR "reject deterministically", never an unwired route → D-03). **Caution:** per Phase-34, `D:/tmp/codex` top-level may be a bare `.git` shell — verify before relying on `codex-rs` subtree.
- Web (2025-2026): Microsoft Agent Governance Toolkit (kernel-style <0.1 ms interception PEP), Temporal / DBOS (durable execution over Postgres — piggyback the checkpoint in the step tx), Bifrost / MCPX (single-binary governed tool layer +11 µs), Arcade / arXiv 2603.20953 (per-action authorization). Full list in the deep-dive doc §Sources.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`aura.tool_invocations` (0011)** — already a durable-execution event log with the exact GATE-04 triple behind a UNIQUE index + append-only/anti-tamper triggers + WR-02 redaction + FK-cascade (0016). Makes D-01 a query change, not a new subsystem.
- **`internal/scoring`** — pure `RiskTier` module with action-aware `ComputeSkillTier`/`ComputeTaskTier`, `GateRecommended`, and `rank` (unknown→Risky). The gateway's whole policy table reuses it untouched.
- **`ask_user` + `paused_states` + Phase-25 approval center + Phase-29 skills pause + D-25 headless auto-reject** — the entire `approve` machinery already ships; D-03 only routes into it.
- **`commandHookFailPolicy`** — GATE-02 fail-closed default already implemented + hardening tests.
- **F-040 `orphan_scan.go`/`sweeper.go` age-grace + `tmpTTL`** — the crash-orphan reconciliation home for D-01d.

### Established Patterns
- **`:exec → :execrows` + `WHERE …`/UNIQUE conditional write IS the idempotency key** (Phase-34 D-03/D-06) — reused verbatim for D-01b.
- **Reuse the one durability seam; reject a second mechanism** (Phase-34 D-02/D-07) — the spine of D-01.
- **Monotone saturate-upward for unknown risk** (`scoring.rank` → Risky; codex `assess_command_safety`) — the safety floor of D-02.
- **Persist-before-act HITL via `ask_user`** — reused for D-03; resume must re-enter the PEP.
- **Deterministic reject-vs-wire-real-approval, never an unwired route** (Phase-34 D-11 / codex safety.rs) — shapes D-03's headless deny-with-guidance.
- CLAUDE.md gates: `go vet/build/test -race` per touched package, coverage ≥85% owned-surface, **mutation ≥70% on gateway/classifier/reservation files** (research §"honest 10/10"), table tests + `goleak` + realistic fixtures, no-skip-as-green, ≤600 LOC/file.

### Integration Points
- New `internal/gateway` `Decide` interface injected at every composition root (serve/cron/telegram/swarm); interposed at `runTool → execTool` **and** `llm_agent_pause.go`.
- The synchronous pre-`Execute` reservation write sits **beside** the existing async `persistToolInvocationLedger` (mutating path only).
- The gateway reads the resolved runtime profile (`internal/config`) to pick fail-open-no-op (dev/local_trusted) vs fail-closed (hardened/production).
- `sqlc` regeneration for `InsertToolInvocation :execrows` + the replay SELECT.

</code_context>

<specifics>
## Specific Ideas

- User explicitly asked (as in Phase 34) to **research 2026 industrial patterns online + inspect the curated `D:/tmp` repos** before deciding — so every decision is corroborated against 2026 practice + the live code, not assumed. The dedicated deep-dive `senior-dev-agent-hardening-2026-tool-policy-gateway.md` already carried most of it; three focused advisor-researchers closed the Aura-specific gaps (classification, approve-routing, migration verdict).
- The standout is D-01's **zero-migration** outcome: like Phase-34's D-07, the "durable ledger" the roadmap/audit imagined is satisfied by *promoting the append-only ledger Aura already writes* (one `:exec→:execrows` change + a synchronous pre-execution insert), not by building a mutable reservation subsystem.
- All four decisions landed on the **first (recommended)** option — clean convergence on the minimal industrial form ([[feedback_no_atomic_bombs_minimal_industrial_shape]]).

</specifics>

<deferred>
## Deferred Ideas

These surfaced from research/audit but belong to LATER phases — do NOT pull into Phase 35:

- **Multi-identity *interactive* approval routing** (surface the pause to the *right* operator under `server_production`) → **Phase 36** (needs MUSR-01..06 owner-scoping + Authula). Until then `approve` under `server_production` degrades to deny-with-guidance.
- **Migration 0026** (idempotency/observability columns, retention) + the OTel **metric** path + `/readyz` + alert/dashboard YAML → **Phase 39** (Idempotency + Observability Pack). D-01 deliberately ships *without* it.
- **Additive `ALTER … ADD COLUMN decision text`** to make the policy verdict first-class SQL-queryable/exportable audit evidence → only if GATE governance later needs it (clean drop-in; the verdict rides `meta jsonb` for now).
- **Sandbox routing** of host shell/fs into a per-identity full-capability container (`SandboxRouter.Resolve`) → **Phase 37** (SBX-01..05); the gateway's decision precedes it in the dependency chain.
- **MCP governance limits** (transport classifier, remote trust, mount timeout, frame cap, teardown, audited CLI writes) → **Phase 38**.
- **Verbatim exact-bytes replay** (extend sidecar retention past F-040 GC) → only if telemetry shows a redacted-preview replay is insufficient; default is "replay tolerates a missing sidecar".
- **Declarative YAML/Rego/Cedar policy language + quorum approval** (Microsoft Agent-OS / research doc §1) → explicitly OUT (SUMMARY anti-features): the Go table over `internal/scoring` is the locked minimal form.

### Reviewed Todos (not folded)
None — `todo.match-phase 35` returned 0 matches.

[No scope-creep ideas surfaced — discussion stayed within phase scope.]

</deferred>

---

*Phase: 35-toolgateway-policy-engine*
*Context gathered: 2026-07-03*
