# Phase 22: bug fix — Specification

**Created:** 2026-06-15
**Ambiguity score:** 0.12 (gate: ≤ 0.20)
**Requirements:** 12 locked

## Goal

Remediate every finding in the 2026-06-15 `internal/agent` production-readiness audit (1 P0 + 12 P1 + 26 P2 + 24 P3 = 63 actionable; `docs/audit/`) to the project Gate-3 done-bar — each fix lands with its named regression test, the ≥85% owned-surface coverage floor holds, and full CI is green — moving the blended production-readiness score from **6.5 → ≥8.0**, **except** the three multi-tenant-only security slices explicitly deferred below.

## Background

All 22 v1 requirements are complete; this is a **remediation phase**, not a feature. The fresh audit at HEAD `136325dc` (`docs/audit/bug-report.md`, `action-plan.md`, `proposed-patches.md`) found the `internal/agent` core loop verified-correct but the operational perimeter under-hardened. Verified current state (grounded in the audit read):

- **No `recover()` in any spawned goroutine** across `internal/agent` + `internal/swarm` (repo-wide grep: only `internal/db/tx.go:28` recovers). `executeBatch`, `workflow/parallel.runSub`, `swarm.runWave`, and the `shell_bg` reaper all run unguarded → one panicking tool crashes the whole multi-channel `serve` daemon (AG-001, P0).
- `dedupRing` (`budget_dedup.go`) is mutated lock-free, race-free only by an unenforced cross-file convention adjacent to the concurrent `executeBatch` (AG-002).
- MCP reconnect (`mcptools/bridge_reconnect.go`) holds the server lock across subprocess spawn+handshake, binds to the failed call's ctx, has no backoff/circuit-breaker; `AURA_MCP_CALL_TIMEOUT_SEC=0` disables the per-call timeout (AG-005/006).
- `secret.IsSecretEnvKey` substring denylist misses DSN-shaped env vars → `AURA_DB_URL` password reaches `shell_exec` children (AG-010). Command hooks inherit the full `os.Environ()` (AG-003). The reasoning trace logs full history/PII (AG-009).
- `metrics.go` exports ~5 counters — no turn-outcome/LLM-latency/error/token/hook metrics; agent core has no `slog`; `mintSpanID` panics on entropy failure (AG-012/013/033).
- Reasoning-router LLM fallback adds a synchronous ≤8s round-trip per turn when the embed sidecar degrades (AG-008).
- The hook subsystem (just landed in Phase 21 / EXT-01) has no fail-soft policy — any hook fault aborts the turn (AG-004).
- Assorted loop/budget/workflow/tool P2/P3 hardening (AG-014..AG-064): fs size cap, budget validation, cycle guard in `findInTree`, dedup growth/period-3, MCP name collisions, default-untrusted provenance, dead-code removal, atomic writes, etc.

The audit's `bug-report.md` is the authoritative finding list; `proposed-patches.md` (PP-1..PP-10) gives the change approach per major finding; `testing-strategy.md` §4 names the required regression tests.

## Requirements

1. **Goroutine panic firewall (P0, AG-001)**: No spawned goroutine can crash the process.
   - Current: zero `recover()` in any `internal/agent`/`internal/swarm` goroutine; a panicking tool kills `aura serve` for all channels.
   - Target: every goroutine body in `executeBatch`, `workflow/parallel.runSub`, `swarm.runWave`, and the `shell_bg` reaper recovers and converts the panic to a per-call `toolRunResult{Err}` / per-child `{status:failed}` report; a Runner-level per-turn recover backstop exists.
   - Acceptance: a `-race`+`goleak` test dispatches a deliberately-panicking fake tool through all three concurrent paths; the process survives, the panic surfaces as a model-visible error, no goroutine leaks.

2. **Dedup-ring data-race elimination (P1, AG-002)**: The dedup ring is safe under concurrency by construction, not convention.
   - Current: `dedupRing.entries`/`results` mutated without a lock; concurrent-map-write is a Go fatal `recover()` cannot catch.
   - Target: a `sync.Mutex` guards all `dedupRing` mutations.
   - Acceptance: a `-race` test hammering concurrent `BeforeToolCall`/`AfterToolResult` + a multi-tool `dispatch` with >1 parallel tools passes clean.

3. **MCP reconnect resilience (P1, AG-005/006/024/025/027 + AG-007 flip-warn slice)**: A flapping/hung MCP server degrades gracefully.
   - Current: reconnect holds `s.mu` across spawn+handshake+list; reuses the failed call's ctx; no backoff/breaker; `=0` timeout → unbounded hang + held mutex; malformed timeout env is loop-fatal per call.
   - Target: single-flight reconnect performed outside the lock with `context.WithoutCancel` + a dedicated timeout; exponential backoff + per-server circuit breaker; `AURA_MCP_CALL_TIMEOUT_SEC=0` means "default" (explicit `-1` for infinite); timeout resolved+validated once at mount/boot; `slog.Warn` on a reconnect that flips a tool's `Mutating` flag; after-send transport failures marked non-retryable.
   - Acceptance: concurrent calls during a slow reconnect show no head-of-line stall beyond a bound; a crash-loop fake server trips the breaker after N failures; a hung server with timeout `0` is bounded by the default deadline with no leaked goroutines.

4. **Secret boundary (P1, AG-010/009/047 + AG-003 minimal-env slice)**: Credentials do not leak to shell children, hook subprocesses, or the trace by default.
   - Current: `IsSecretEnvKey` misses `*_URL/_DSN/_URI/_CONN/_PWD`; command hooks inherit full `os.Environ()`; reasoning trace dumps full history/PII with name-only redaction.
   - Target: DSN/URL markers + a `scheme://user:pass@` value-scan in `IsSecretEnvKey`; a DSN pattern in the shell output redactor; command hook `cmd.Env` defaults to a minimal allowlist (no inherited provider/DB secrets); the reasoning trace no longer writes verbatim `history` by default and caps per-field size.
   - Acceptance: `IsSecretEnvKey("AURA_DB_URL")==true`; a `shell_exec` child cannot read the composed DSN; the output redactor masks `postgres://u:p@h`; a hook child env contains no secret-named vars; with the trace on, full `history` is not written verbatim by default.

5. **Observability surface (P1, AG-012/013/033/056/057)**: Production is no longer blind and telemetry cannot crash the daemon.
   - Current: ~5 counters; no LLM-latency/turn-outcome/error/token/hook metrics; no `slog`; `mintSpanID` panics on `crypto/rand` failure; OTLP silently drops with no boot log; promauto duplicate-registration risk.
   - Target: `slog` at turn/LLM/tool/hook boundaries (request_id/thread_id keyed); `aura_agent_turn_total{outcome}`, `llm_call_duration_seconds`, `llm_errors_total{kind}`, `tool_errors_total{tool}`, token + hook + `span_export_failures_total` + `panic_total{site}` metrics on a non-default registry; `mintSpanID` falls back to a zero ID; a boot log states the exporter mode + endpoint.
   - Acceptance: each terminal `turnReason` increments its labeled counter; an LLM-latency histogram is scrapeable; injecting an entropy failure does not panic; a metric-contract test asserts the outcome→counter mapping.

6. **Reasoning-router fallback policy (P1, AG-008)**: An embed-sidecar outage adds no per-turn latency cliff.
   - Current: any classifier miss/abstain/error falls through to a synchronous LLM router round-trip (≤8s) every turn.
   - Target: a wired-but-abstaining classifier short-circuits to a static `ReasoningTierLow`; the LLM router is opt-in; an embed-sidecar circuit breaker stops repeated router calls; the router timeout is capped well below 8s.
   - Acceptance: with the embedder forced to error, no router LLM call is issued (or one then breaker-open) and turn latency is unaffected.

7. **Hook reliability (P1/P2/P3, AG-004/030/058/054)**: A hook fault is contained, not turn-fatal.
   - Current: any hook error/timeout/unknown-decision aborts the turn; a non-zero exit with a parseable decision silently swallows the run error; no `recover()` around in-process hooks; bare hook command names resolved via `$PATH`.
   - Target: a per-hook `fail_open`/`fail_closed` policy (default `fail_open` for non-security hooks) routing failures to log+metric+allow; `rewrite` requires exit 0; in-process hook calls wrapped in `recover()`; hook commands require absolute paths.
   - Acceptance: a failing/timed-out/panicking hook completes the turn under `fail_open` and aborts with a clear reason under `fail_closed`; a non-zero-exit `rewrite` is rejected.

8. **Default-untrusted provenance (P1-in-deployment, AG-052)**: Unknown-tool and swarm-child output cannot launder prompt injection.
   - Current: `trust.go untrustedSource` defaults unknown tools to trusted; `swarm_spawn` child reports (embedding children's web/fs output) are not enveloped.
   - Target: default unknown-tool output to untrusted (explicit allowlist for genuinely-safe built-ins); `swarm_spawn` child reports carry `TrustUntrusted` provenance so the parent wraps them.
   - Acceptance: a swarm child's malicious web content is wrapped in the nonce `<tool_output trust="untrusted">` envelope in the parent prompt; a built-in safe tool stays trusted.

9. **Loop / budget / workflow correctness (P2, AG-035/036/037/038/039/040/041/043)**: The reasoning substrate is bounded and validated.
   - Current: `maxIterations==0` bounded only by budget/no-progress; `max_steps`/`wallclock` not validated `>0` (`=0` silently disables the runtime); `findInTree` has no cycle guard; swarm reserve is best-effort TOCTOU; dedup `results` grows unbounded + misses period-3 cycles; wallclock is a soft step-boundary gate (`WithDeadline` unwired); parallel result-closer leak edge unproven.
   - Target: a default iteration ceiling for `maxIterations==0`; `NewBudget` rejects `maxSteps<1`/`wallclock<1`; `findInTree` carries a visited-set/depth bound; `Budget.WithDeadline` threaded into the run ctx (confirm composition-root wiring at `cmd/aura/agent.go`); dedup `results` pruned on eviction + period-3+ cycle detection; the closer-goroutine no-leak property proven by a `goleak` stress test.
   - Acceptance: `NewBudget(0)`/negative errors at construction; a cyclic agent tree does not stack-overflow; a 3-tool oscillation is caught before budget exhaustion; total wall-time is bounded; the parallel `goleak` stress test passes.

10. **Tools hardening (P2/P3, AG-014/015/016/017/018/019/020/045/046)**: Tool execution is memory-safe, evictable, and consistent.
    - Current: no fs read/write/edit size cap (OOM risk); `BackgroundShells` is not a `SessionEvictor` (buffer leak); `agent_job` gated only by keywords; fragile bg-snapshot bookkeeping; unvalidated model `cwd`; `send_file` fence off when root empty; stale MCP embeddings; non-atomic writes; inconsistent glob semantics.
    - Target: `AURA_FS_MAX_READ_BYTES` cap with stat-then-reject/auto-page; `BackgroundShells` implements `SessionEvictor` + prunes on poll/kill; `agent_job` schedules gated or surfaced at fire time; cwd validated; `send_file` fails closed in non-CLI when root empty; per-tool description-hash forces ranker rebuild on MCP reconnect; atomic temp-file+rename writes; unified glob semantics (or documented).
    - Acceptance: a file over the cap returns a clean error (no OOM); a finished bg shell's buffer is reclaimed on session end; the named per-finding tests in `testing-strategy.md` §4 pass.

11. **Skill self-extension reconcile, single-operator slice (P1, AG-011/044/051)**: The skill tool's docs match its behavior; no dead code.
    - Current: gated-looking comments/spec contradict the ungated live auto-activation path; an unused duplicate `skillParamsSchema` const drifts; `skill_write.go:186` can return the `ask_user` pause sentinel (dormant), contradicting the "only ask_user pauses" invariant.
    - Target: delete the dead `skillParamsSchema`; reconcile comments/spec with the actual behavior; restore the operator alert (or document the ungated trust boundary honestly); reconcile the pause-sentinel path with the invariant.
    - Acceptance: exactly one skill schema is referenced; `deadcode` is clean for the const; a test asserts the documented activation behavior matches code; `TestAskUserOnlyPauseConstraint` still holds.

12. **Cross-cutting done-bar + finding ledger (constraint, applies to all of 1–11)**: Every in-scope finding is closed to Gate-3 and nothing is silently dropped.
    - Current: 63 findings open; no per-finding test/coverage guarantee.
    - Target: every `AG-###` finding in `docs/audit/bug-report.md` (P0–P3) is either (a) fixed with its named regression test, or (b) explicitly documented as accepted/no-fix with rationale in the phase close-out (e.g. AG-021 advisory gate, AG-048 symlink, AG-049 port, AG-053), or (c) confirmed-then-routed for the four NEEDS-CONFIRMATION items (AG-028 dead code, AG-034 ledger redaction [other package], AG-041 `WithDeadline` wiring [cmd/aura], AG-043 leak edge); the ≥85% owned-surface coverage floor holds; `go vet`+`build`+`-race`+`golangci-lint`+`govulncheck` are green.
    - Acceptance: a finding-coverage ledger maps every `AG-###` to {fixed+test | accepted+rationale | confirmed+routed}; `make coverage` ≥85%; CI green; mutation spot-check ≥70% on the newly-touched critical files (`llm_agent_parallel.go`, `budget_dedup.go`, `mcptools/bridge_reconnect.go`).

## Boundaries

**In scope:**
- All 63 actionable `internal/agent` findings (AG-001..AG-064) from `docs/audit/bug-report.md`, P0 through P3.
- The single-operator-relevant slices of the three multi-tenant-flavored security findings (hook minimal-env + fail-soft, DSN secret stripping, skill dead-schema/honest-docs/alert, `slog.Warn` on MCP Mutating-flip).
- Per-finding regression tests (`testing-strategy.md` §4: panic firewall, dedup race, MCP resilience, secret-boundary, tree-cycle, budget validation, router-fallback, cache-prefix drift) + the ≥85% coverage floor + green CI.
- A finding-coverage ledger recording the disposition of every `AG-###`.
- Confirmation of the four NEEDS-CONFIRMATION items, landing the `internal/agent`-side change where the fix lives there.

**Out of scope (deferred to a future security/hardening phase or a different package):**
- **AG-007 full `capability_grants` per-call wiring** — multi-tenant gate; the trust model is single-operator today. *(Only the `slog.Warn`-on-flip slice lands here.)*
- **AG-003 hook exec-by-fd TOCTOU close** — requires an attacker who can already write the host hook path (already-compromised in the single-operator model). *(Only minimal-env + fail-soft land here.)*
- **AG-011 full multi-tenant skill-activation gating** — *(only the dead-schema/honest-docs/alert slice lands here.)*
- **Prior-cycle findings in other packages** — B-01 (runner crash-reexec, `internal/runner`), B-03 (per-thread in-flight guard, `internal/agui`/`runner`), M-01 (microcompact, `internal/conversations`) — different packages, not `internal/agent`.
- **Production container (non-root, read-only rootfs), `/readyz`, per-thread in-flight guard, multi-tenant re-rating, dependency breakers beyond the MCP/embed ones above** — OPS/deployment scope (prior D-01), a separate phase.
- **AG-034 ledger redaction** if it proves to live entirely in the persistence layer — confirm, then route (the `event.go` shape change, if any, lands here; the DB projection does not).

## Constraints

- **Done-bar:** every fix ships with its named regression test; ≥85% owned-surface coverage floor (project hard floor, overrides PRD 75/60); `go vet`+`go build`+`go test -race`+`golangci-lint`(+`dupl`)+`govulncheck` green; no `t.Skip`-as-green; mutation spot-check ≥70% on the touched critical files.
- **No behavior regressions:** the verified-correct loop properties must be preserved — non-empty terminal contract, bounded recovery counters, KV-cache stable-prefix (`cache_invariant_audit.sh` still green), shared-atomic budget, SSRF hardening, nonce untrusted-output envelope.
- **Execution order (planning hint, risk-first waves):** W1 crash (R1, R2) → W2 secrets+observability (R4, R5) → W3 MCP+reliability (R3, R6, R9) → W4 remaining P2 tools/hooks/provenance (R7, R8, R10, R11) → W5 P3 cleanup + ledger (R12). `/gsd-plan-phase` owns final wave composition.
- **File-size discipline:** no file >600 LOC; deep-refactor-on-touch (dead-code removal, dupl-folding) in the same commit.
- **Commit discipline:** atomic commits, ideally one finding (or one tight finding-cluster) per commit with the `AG-###` ref in the message.

## Acceptance Criteria

- [ ] A `-race`+`goleak` test proves a panicking tool through `executeBatch`/`parallel.Run`/`swarm.runWave` does not crash the process (R1).
- [ ] `dedupRing` mutations are mutex-guarded; a `-race` concurrent hammer test passes (R2).
- [ ] MCP reconnect is single-flight off-lock with backoff+breaker; a crash-loop fake server trips the breaker; a hung server with `AURA_MCP_CALL_TIMEOUT_SEC=0` is bounded by the default deadline with no goroutine leak (R3).
- [ ] `IsSecretEnvKey("AURA_DB_URL")==true`; a `shell_exec` child cannot read the composed DSN; hook child env has no secret-named vars; trace does not write verbatim `history` by default (R4).
- [ ] Each terminal `turnReason` increments a labeled Prometheus counter; an LLM-latency histogram exists; `mintSpanID` does not panic on an injected entropy failure (R5).
- [ ] With the embedder forced to error, no per-turn LLM router round-trip is issued (R6).
- [ ] A failing hook completes the turn under `fail_open` and aborts cleanly under `fail_closed` (R7).
- [ ] A swarm child's untrusted output is wrapped in the nonce envelope in the parent prompt; unknown-tool output defaults to untrusted (R8).
- [ ] `NewBudget(0)`/negative errors at construction; a cyclic agent tree does not stack-overflow; total wall-time is actively bounded (R9).
- [ ] A file over `AURA_FS_MAX_READ_BYTES` returns a clean error (no OOM); a finished background shell's buffer is reclaimed on session end (R10).
- [ ] Exactly one skill schema is referenced; `deadcode` clean; documented skill activation behavior matches code (R11).
- [ ] A finding-coverage ledger maps every `AG-###` to fixed+test / accepted+rationale / confirmed+routed; the named deferrals are recorded (R12).
- [ ] `make coverage` ≥ 85% owned-surface; `go vet`+`build`+`-race`+`golangci-lint`+`govulncheck` green; `cache_invariant_audit.sh` green (R12 / no-regression).
- [ ] Mutation spot-check ≥70% killed on `llm_agent_parallel.go`, `budget_dedup.go`, `mcptools/bridge_reconnect.go` (R12).

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Remediate audit findings to Gate-3; score 6.5→≥8.0           |
| Boundary Clarity   | 0.90  | 0.70 | ✓      | In = 63 findings; explicit multi-tenant + other-pkg carve-outs |
| Constraint Clarity | 0.85  | 0.65 | ✓      | Done-bar + risk-first waves + no-regression locked            |
| Acceptance Criteria| 0.85  | 0.70 | ✓      | Per-finding from audit + named tests + finding ledger         |
| **Ambiguity**      | 0.12  | ≤0.20| ✓      |                                                              |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption). No dimensions below minimum.

## Interview Log

| Round | Perspective     | Question summary                          | Decision locked                                                        |
|-------|-----------------|-------------------------------------------|------------------------------------------------------------------------|
| 1     | Boundary Keeper | Which findings does Phase 22 cover?       | Everything (all 63 P0–P3 internal/agent findings)                      |
| 1     | Acceptance      | Done-bar per fix?                         | Fix + named regression test + ≥85% coverage + green CI (Gate-3 DoD)    |
| 1     | Boundary Keeper | How deep on multi-tenant security P1s?    | Single-operator parts now; defer capability_grants/exec-by-fd/full skill gate |
| 2     | Simplifier      | One phase or split given 63 at strict bar?| One phase, wave-executed                                               |
| 2     | Seed Closer     | No-fix + NEEDS-CONFIRMATION handling?     | Investigate, then fix-or-document; confirm NEEDS-CONFIRMATION first    |
| 2     | Seed Closer     | Lock execution wave order?                | Risk-first: crash → secrets+obs → MCP+reliability → P2 → P3            |

---

*Phase: 22-bug-fix*
*Spec created: 2026-06-15*
*Source audit: docs/audit/ (cycle 2026-06-15, HEAD 136325dc) — bug-report.md, action-plan.md, proposed-patches.md, testing-strategy.md*
*Next step: /gsd-discuss-phase 22 — implementation decisions (how to build what's specified above)*
