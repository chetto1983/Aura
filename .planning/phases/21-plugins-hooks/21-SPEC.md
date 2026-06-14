# Phase 21: Plugins — Hooks (Slice EXT-1) — Specification

**Created:** 2026-06-14
**Ambiguity score:** 0.11 (gate: ≤ 0.20)
**Requirements:** 6 locked

> **Derivation note (2026-06-14):** requirements are derived directly from the approved design contract `docs/superpowers/specs/2026-06-14-aura-plugins-unified-extension-design.md` + the file:line seam map of the same date + research `docs/research/2026-06-14-aura-plugin-architecture-research.md`, and ratified by PRD **amendment #63** (capability `EXT-01`). Ambiguity was already ≤ 0.20 from that prior work, so SPEC.md was generated directly (the spec-phase "requirements already clear" path) rather than via a fresh 6-round interview. The three cross-cutting decisions are LOCKED: command/manifest `aura plugins`/`aura.plugin.json`; hook authoring = both in-process Go + out-of-process command programs; providers/channels deferred.

## Goal

Aura's agent loop gains a registrable in-process **`HookManager`** fired at five fixed points of `LlmAgent.Run`, letting first-party Go hooks and trust-gated out-of-process command programs **observe, rewrite, or veto** model calls and tool calls — with byte-identical loop behavior when zero hooks are registered.

## Background

Aura is the first slice (EXT-1) of the `EXT-01` unified-extension capability (PRD amendment #63). The architecture is settled: tools/skills already have clean in-process seams (`tools.Registry` + `mcptools.bridge`; gated skills `Writer`/`Loader`), and the one missing surface is **hooks** — there is no registrable interception layer today.

Current reality (ground-truthed 2026-06-14 via the seam map):

- The loop is `internal/agent/llm_agent.go`, `LlmAgent.Run` (`:178-382`), an `iter.Seq2[*Event, error]` emitter. `LlmAgent` is configured via `LlmAgentConfig` (fields like `Classifier`, `Breaker`) at the composition root `cmd/aura/main.go`.
- There is **no generic hook/middleware/plugin layer** — but the loop already runs **five hardcoded, single-purpose interceptors** at exactly the points a hook layer would attach, proving the seams are reachable and the semantics are precedented:
  - completion-critic **veto** of termination — `gateCompletion` `:348`/`:480`
  - dedup **veto** before a tool batch — `BeforeToolCall` `:419`
  - tool-result **rewrite** (preview/spillover) before history — `:559`/`:564`
  - reasoning router mutating the next request — `:247`
  - tool-result provenance/trust stamping — `spec.go:60`
- Governance gates the new layer must compose with (never bypass): budget `ConsumeStep` `:215` (before model), dedup `:419` (before tool), `ask_user` pause `:362` (before dispatch), and the append-only `tool_invocations` audit written out-of-band in the Runner (`internal/runner/runner_persist.go:219`).
- `capability_grants` exists but is **unwired** (`internal/identity/store.go`, `'*'` seeded) — it is the gate for EXT-3 (self-install), NOT this slice.
- The KV-cache invariant (`scripts/cache_invariant_audit.sh`) asserts SHA-256(`messages[0]`) constant across a 20-turn replay; any hook touching prompt assembly must preserve it.

The primary deliverable that does NOT exist yet: a `HookManager` type + `Hook` interface, the five fired insertion points, a trust-gated command-program hook runner, and the governance-composition wiring (audit re-emit on rewrite).

## Requirements

1. **HookManager + Hook contract**: A registrable in-process hook layer injected into the agent.
   - Current: No hook/middleware type exists; `LlmAgentConfig` has no hook field.
   - Target: A `HookManager` (registry of ordered `Hook`s) injected via `LlmAgentConfig` (parallel to `Classifier`/`Breaker`), plus a `Hook` interface exposing the five lifecycle methods. A nil/empty manager is a valid no-op.
   - Acceptance: `LlmAgent` constructed with a nil `HookManager` runs unchanged; a registered hook's methods are invoked at the right points (unit test with a recording hook).

2. **Five insertion points with first-non-nil-wins semantics**: Hooks fire at the five seam-map points and the first non-nil result short-circuits.
   - Current: The five interceptors are hardcoded and non-extensible.
   - Target: `OnTurnStart` (`:187`), `BeforeModel(ctx, *llm.Request)` (`:266`, may substitute a response / early-exit), `BeforeTool(ctx, *llm.ToolCall)` (`:419`→`:444`, may rewrite args or veto with a synthetic `tools.ToolResult`), `AfterTool(ctx, call, *tools.ToolResult)` (`:450`, may rewrite the result before history), `OnTurnEnd` (`:192`). ADK "first non-nil result wins, early-exit" ordering across multiple registered hooks.
   - Acceptance: With two hooks on one event, the first non-nil result wins and the second is not consulted; a `BeforeModel` hook returning a response short-circuits the model call; a `BeforeTool` hook returning a result vetoes execution — all proven by unit tests.

3. **Two authoring modes (in-process Go + trust-gated command program)**: Hooks can be compiled Go or external programs.
   - Current: Neither exists.
   - Target: (a) an in-process Go `Hook` registered at the composition root; (b) an out-of-process **command-program** hook runner (Codex shape) that passes event JSON on stdin and reads an `allow`/`deny`/`rewrite` decision from stdout-JSON/exit-code, behind a **trust-hash gate** (a command whose recorded hash does not match is NOT executed) with a bounded timeout. Command hooks are restricted to observe/approve-deny + bounded rewrite; they never run on a sub-millisecond mutating path beyond the defined points.
   - Acceptance: An in-process hook and a command-program hook each fire and can allow/deny/rewrite; a command whose hash does not match the trust record is refused (not executed) with a clear diagnostic; a command exceeding its timeout is killed and treated as `deny`/error per policy.

4. **Governance composition + audit consistency**: Hooks compose with existing gates and keep the audit trail truthful.
   - Current: budget/dedup/ask_user gates fire with no hook awareness; `tool_invocations` records the Event-emitted args.
   - Target: Hooks run alongside (never bypass) budget `ConsumeStep`, dedup `BeforeToolCall`, and the `ask_user` pause. A `BeforeTool` arg-rewrite MUST re-emit the ToolInvocation Event so `tool_invocations` records the **rewritten** args, not the originals.
   - Acceptance: With a `BeforeTool` rewrite hook active, the `tool_invocations` ledger row for that call contains the rewritten args (integration test); budget and dedup gates still fire (a dedup-vetoed call + a budget-exhausted run behave as today with hooks present).

5. **KV-cache invariant preserved**: The hook layer does not destabilize the cacheable prefix.
   - Current: `scripts/cache_invariant_audit.sh` passes (SHA-256(`messages[0]`) constant over 20-turn replay).
   - Target: No hook code path mutates `messages[0]`; any prompt-affecting hook output is confined to the mutable region or a history copy (mirroring the existing `<budget>` tail-injection pattern).
   - Acceptance: `scripts/cache_invariant_audit.sh` passes unchanged with the hook layer present and with a no-op hook registered; SHA-256(`messages[0]`) constant across the replay.

6. **Zero-hook no-op safety (no regression)**: Default behavior is byte-identical to today.
   - Current: The loop produces today's outputs.
   - Target: With no hooks registered, `LlmAgent.Run` emits the identical Event sequence for a fixture run (no extra allocations on the hot path beyond a nil check).
   - Acceptance: A golden/fixture replay with zero hooks yields the same final Event and the same `tool_invocations` rows as the pre-change baseline; `goleak` + `-race` clean.

## Boundaries

**In scope:**
- `HookManager` + `Hook` interface (Go) injected via `LlmAgentConfig`.
- The five fired insertion points in `internal/agent/llm_agent.go` with first-non-nil-wins/early-exit.
- In-process Go hook authoring + out-of-process command-program hook runner with trust-hash gate + timeout.
- Governance composition (budget/dedup/ask_user) + `tool_invocations` re-emit on `BeforeTool` rewrite.
- Composition-root registration wiring + cache-invariant preservation.
- Unit + integration + mutation tests; goleak/race clean; coverage ≥ 85% owned surface.

**Out of scope:**
- `aura.plugin.json` manifest + `aura plugins {add,list,…}` installer — that is **Slice EXT-2** (Phase 22).
- Self-install loop + wiring `capability_grants` — that is **Slice EXT-3** (Phase 23).
- Provider and channel manifest extension — deferred per amendment #63 (providers stay `AURA_LLM_MODEL` config; channels remain a native-Go surface).
- Hot-adding hooks to a *live* agent's registry — runtime additions ride the per-turn fresh-`LlmAgent` rebuild; no live mutation of an immutable registry.
- A hook marketplace / distribution / discovery — no marketplace in v1.
- Migration 0016 `plugins_audit` — lands with EXT-2 (the installer is what it audits); this slice reuses the existing `tool_invocations` ledger only.

## Constraints

- **KV-cache invariant is non-negotiable**: no hook path mutates `messages[0]`; `scripts/cache_invariant_audit.sh` must stay green (cross-slice CI gate from Phase 6 onward).
- **Mutating hooks are in-process only**: the sub-millisecond rewrite/veto path is Go; command-program hooks (process round-trip) are restricted to observe/approve-deny + bounded rewrite at the defined points.
- **Command hooks are default-deny**: executed only behind a trust-hash match, with a bounded timeout; an unmatched/timed-out hook never silently mutates the loop.
- **No governance bypass**: hooks compose with budget/dedup/ask_user; the `tool_invocations` audit reflects rewritten args.
- **Quality gates (project standard)**: owned-surface coverage ≥ 85% across the tag matrix; `goleak` + `-race` clean; mutation spot-check ≥ 70% on the hook-dispatch file; `golangci-lint` = 0; no file > 600 LOC.

## Acceptance Criteria

- [ ] `LlmAgent` with a nil `HookManager` emits a byte-identical Event sequence and identical `tool_invocations` rows vs. the pre-change baseline on a fixture replay.
- [ ] A registered in-process `BeforeTool` hook can (a) veto a call with a synthetic result and (b) rewrite args; the `tool_invocations` row records the **rewritten** args.
- [ ] First-non-nil-wins: two hooks on one event → first non-nil result wins, second not consulted (unit test).
- [ ] A `BeforeModel` hook can short-circuit with a synthesized response; budget `ConsumeStep` fired before the hook.
- [ ] An out-of-process command-program hook receives event JSON on stdin and applies allow/deny/rewrite; a hash-mismatched command hook is refused (not executed); a timed-out hook is killed and treated as deny/error.
- [ ] dedup veto + budget exhaustion behave as today with hooks present (composition regression test).
- [ ] `scripts/cache_invariant_audit.sh` passes with the hook layer + a no-op hook registered (SHA-256(`messages[0]`) constant).
- [ ] `goleak` + `-race` clean; owned-surface coverage ≥ 85%; mutation ≥ 70% on the hook-dispatch file; `golangci-lint` = 0.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.92  | 0.75 | ✓      | Outcome + 5 fixed insertion points + no-op invariant         |
| Boundary Clarity   | 0.95  | 0.70 | ✓      | EXT-2/EXT-3/providers/channels explicitly excluded           |
| Constraint Clarity | 0.85  | 0.65 | ✓      | KV-cache, in-process-mutating, default-deny, coverage gates  |
| Acceptance Criteria| 0.85  | 0.70 | ✓      | 8 pass/fail checks, all falsifiable                          |
| **Ambiguity**      | 0.11  | ≤0.20| ✓      | Derived from prior design contract + seam map                |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective     | Question summary                          | Decision locked                                                        |
|-------|-----------------|-------------------------------------------|-----------------------------------------------------------------------|
| —     | Researcher      | What hook seam exists today?              | None registrable; 5 hardcoded interceptors at the target points        |
| —     | Simplifier      | Irreducible core of "easy extend"?        | Hooks are the only missing surface; tools/skills already seamed        |
| —     | Boundary Keeper | What is NOT this slice?                   | Manifest/installer (EXT-2), self-install (EXT-3), providers/channels   |
| —     | Failure Analyst | What breaks if hooks are wrong?           | Audit desync on rewrite → re-emit Event; KV-cache mutation → invariant |
| —     | Seed Closer     | Authoring model + command-hook safety?    | Both modes; command hooks default-deny behind trust-hash + timeout     |

(Rounds derived from the 2026-06-14 brainstorming → research → design flow + the three locked operator decisions, not a fresh interactive interview; ambiguity was already ≤ 0.20.)

---

*Phase: 21-plugins-hooks*
*Spec created: 2026-06-14*
*Next step: /gsd-discuss-phase 21 — implementation decisions (HookManager type shape, command-hook protocol, audit re-emit mechanics)*
