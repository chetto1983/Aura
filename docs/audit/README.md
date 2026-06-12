# Aura `internal/agent` — Production-Readiness Audit

**Audited path:** `d:\Aura\internal\agent` (+ adjacent `internal/runner`, `internal/conversations`, `internal/askuser`, `internal/llm`, `internal/mcp`, `internal/agui`, `internal/config`, `cmd/aura`).
**Branch:** `tabula-rasa` · **Latest cycle:** 2026-06-12 (re-audit-2) · **Prior cycles:** 2026-06-10, 2026-06-11.

> **Verified status as of 2026-06-13 → [`reconciliation-2026-06-13.md`](reconciliation-2026-06-13.md).** The P1 gate (B-01..B-04, O-01, O-02, D-01) was **CLOSED + tested** by commit `ec7fe2f6` after this audit was written. Current verified state: **score ~7.5 · 0 P1 open · 10 CLOSED / 6 PARTIAL / 22 OPEN / 2 TRACKED.** The tables below are the 2026-06-12 snapshot; the reconciliation note and `risk-register.md` carry the live status.

This is a fresh, evidence-based re-audit that *verifies the prior remediation claims against the current code* rather than re-deriving from scratch. Every finding carries a `file:line`, an impact statement, and a recommended fix. Where a prior "CLOSED" does not hold, it is flagged as over-credited with evidence.

---

## TL;DR

The agent **core loop is genuinely production-grade** (≈8.5/10 in isolation) and the 2026-06-10/11 P0/P1 pass was real engineering — most closures verify. The **blended production-readiness score is 6.5/10**, held down by the operational perimeter:

- **0 P0** — both prior P0s (R-01 untrusted-output envelope, R-02 MCP hang) verify closed for the direct paths.
- **8 P1**: one crash-durability correctness hazard (mutating tools re-execute after an intra-turn crash), one trust-boundary hole (swarm reports re-enter the parent prompt unwrapped), one data-integrity bug (no per-thread in-flight guard), one security/contract regression (self-extension gate open with stale schema + lost alert), and a cluster of operational blockers (no tracer in the daemon, no structured logging, no metrics, no production container).
- **20 P2 / 12 P3**: context-compaction correctness, resume atomicity, breaker lifetime, secret-env divergence, disk growth, CI gaps, test-apex gaps.

If you read only one document, read [`executive-summary.md`](executive-summary.md). If you read the 2026-06-10 audit and want only what changed, read [`re-audit-2026-06-12.md`](re-audit-2026-06-12.md).

---

## How to read these reports

| File | What it is | Read it when |
|---|---|---|
| [`executive-summary.md`](executive-summary.md) | Score, top risks, immediate actions | You want the 2-minute verdict |
| [`re-audit-2026-06-12.md`](re-audit-2026-06-12.md) | This cycle's delta vs prior: verified / over-credited / new | You already read the 06-10 audit |
| [`architecture-review.md`](architecture-review.md) | Current design, loop analysis, weaknesses, target shape | You want to understand the system |
| [`bug-report.md`](bug-report.md) | Every correctness/security finding, full format | You're fixing things |
| [`security-audit.md`](security-audit.md) | Prompt-injection surface, subprocess/file/secret boundaries | Threat-model review |
| [`infrastructure-audit.md`](infrastructure-audit.md) | Logging, metrics, tracing, health, config, deployment, CI | Ops/SRE readiness |
| [`testing-strategy.md`](testing-strategy.md) | Coverage reality, missing tiers, the test pyramid | Test planning |
| [`industrialization-roadmap.md`](industrialization-roadmap.md) | Phased plan (0–5) with effort/impact/acceptance | Sequencing the work |
| [`action-plan.md`](action-plan.md) | Concrete backlog by priority | Sprint planning |
| [`risk-register.md`](risk-register.md) | All risks with severity/probability/impact/status | Risk tracking |
| [`target-architecture.md`](target-architecture.md) | Proposed industrial-grade design | Long-horizon design |
| [`proposed-patches.md`](proposed-patches.md) | Patch-style change recommendations (not applied) | Implementing fixes |
| [`audit-index.json`](audit-index.json) | Machine-readable summary | Tooling/dashboards |

**Severity scale.** P0 = critical production blocker / data loss / security breach / unsafe execution. P1 = serious reliability/correctness/security issue to fix before production. P2 = important maintainability/observability/architecture issue. P3 = improvement/cleanup/future hardening.

**Finding IDs.** This cycle uses `B-##` (bugs/correctness/security), `O-##` (observability/ops), `D-##` (deployment), `M-##` (memory/context). Prior-cycle `R-##` IDs are preserved in [`risk-register.md`](risk-register.md) with updated status so the audit trail stays continuous.

---

## Most important findings (2026-06-12)

| # | Severity | Finding | Files |
|---|---|---|---|
| B-01 | **P1** | Mutating tool side effects **re-execute** after an intra-turn crash — the host write happens before the result turn is persisted; reload drops the dangling call; the model re-issues it (R-05 only half-closed) | `llm_agent.go:376-388`, `runner_persist.go`, `store_helpers.go:232-267` |
| B-02 | **P1** | Swarm child reports re-enter the **parent** prompt with **no** untrusted envelope → indirect prompt-injection laundering across the trust boundary | `internal/swarm/runner_adapter.go:54`, `trust.go:14` |
| B-03 | **P1** | No per-thread in-flight guard: concurrent runs on one thread interleave history appends → conversation corruption + budget double-spend | `agui/server.go:139-190`, `runner.go:211-236` |
| B-04 | **P1** | Self-extension gate **open** for `always:false` skills, while the tool schema + comments still claim human approval and the operator alert no longer fires (R-09 regressed by P5 policy) | `skills/writer.go:94-148`, `tools/skill.go:99-112` |
| O-01 | **P1** | `aura serve` (the production daemon) **never boots the tracer**, and the agent core has **zero structured logging** → production runs blind | `cmd/aura/serve.go`, `internal/agent/*.go` |
| O-02 | **P1** | No latency/error/cost metrics, no Prometheus (only 4 expvar counters) → no SLOs, no alerting | `internal/agent/metrics.go` |
| D-01 | **P1** | No production container (non-root, read-only, resource-bounded) for a runtime that executes arbitrary `shell_exec` | repo root (no `Dockerfile`), `compose.yaml` |
| M-01 | **P2** | L1 microcompact rewrites `ask_user` answers (and small tool results) to a **dead** `read_tool_output` pointer → user answers silently destroyed after ~10 rounds (R-28) | `conversations/context.go:208-229` |

See [`bug-report.md`](bug-report.md) for the full severity-ordered list (0 P0, 8 P1, 20 P2, 12 P3).

---

## What is done well (do not regress)

Verified strong, and to be preserved as-is:

- **Budget core** — TOCTOU-safe shared-atomic step counter (decrement-then-check-then-restore, `budget.go:225-236`), fan-out-proof, fail-fast env parsing, injectable clock.
- **`iter.Seq2` discipline** — yield-after-false guards and drain-to-close are airtight; `ParallelAgent` + `executeBatch` have no constructible deadlock or leak (`-race` green, goleak across 4 packages).
- **Reliability primitives are real** — the circuit breaker is **checked via `Allow()` before every stream open** (`llm_agent_stream_retry.go:25`), not merely fed failures; per-call (`TotalTimeoutSec`) and per-tool (`NodeTimeout`) deadlines are wired end-to-end and the wallclock cancels in-flight work (`Budget.WithDeadline` is now genuinely called — `runner.go`).
- **Untrusted-output envelope** — NFKC + `html.EscapeString` + crypto/rand nonce on all 8 direct feeders, both error and success paths (`trust.go`); a forged `</tool_output>` is escaped, the nonce is unguessable.
- **Path/process safety** — strict allowlist sidecar id grammar before `filepath.Join` (no traversal/Windows-ADS), `0o600` perms; `send_file` double-`EvalSymlinks` fence; MCP reconnect-**no-replay**; process-group kill on both OSes with `WaitDelay`.
- **Test discipline** — an excellent tool-call wire-contract test (one `tool_result` per `tool_call`), real `pgregory.net/rapid` property tests, exhaustive dedup coverage.

## Method

Six parallel deep-readers covered loop+reliability, tools+security, memory+persistence, infra+observability, testing+quality, and reference comparison (`D:\tmp\{codex,nanobot,picobot,adk-go-study,agent-memory}`). Every prior "CLOSED" was re-checked against the working tree; two are over-credited (R-05, R-09). Two contested reference-comparison claims (sequential dispatch, missing breaker check) were **falsified by direct code reads** and discarded.

## Historical records (kept, not regenerated)

`19-LIVE-SIGNOFF-2026-06-10.md`, `deep-correctness-audit-2026-06-10.md`, `p0-validation-2026-06-10.md`, `p1-validation-2026-06-10.md`, `p2-boundary-lifecycle-validation-2026-06-11.md`, `e2e-closure-2026-06-11.md`, `trust-boundary-info-2026-06-10.md` are point-in-time evidence from prior cycles, preserved as-is.
