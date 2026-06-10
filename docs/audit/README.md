# Aura `internal/agent` — Production-Readiness Audit

**Audit date:** 2026-06-10
**Audited path:** `d:\Aura\internal\agent` (133 files, ~9,061 non-test LOC + ~13,600 test LOC)
**Branch / commit:** `tabula-rasa` @ `0ab722e5`
**Reference material:** `D:\tmp` (codex, nanobot, picobot, adk-go-study — curated, non-production)
**Method:** seven parallel deep-dive audits (loop correctness, tool execution, memory/context, infrastructure/reliability, security, testing, reference comparison), each reading every in-scope file; high-severity claims re-verified by the lead auditor against source.

---

## What this audit is

A strict, evidence-based production-readiness review of Aura's agent runtime, conducted as if the repository is intended to become a real industrial system. Every finding cites a file, a location (function + line), the concrete failure scenario, a recommended fix, and a confidence level (`CONFIRMED` = traced in code; `NEEDS CONFIRMATION` = suspected, with the missing evidence named).

It is **not** a rewrite proposal. The core ReAct loop is genuinely well-engineered (see "What is done well" below); the findings concentrate at the **edges** — composition seams, untrusted-input handling, subprocess resource bounds, persistence durability, and the missing operational surface (metrics, health, timeouts that actually cancel).

## How to read the reports

Read in this order depending on your role:

| If you are… | Start with |
|---|---|
| A decision-maker / lead | [`executive-summary.md`](executive-summary.md) — score, top risks, immediate actions |
| An engineer about to fix things | [`action-plan.md`](action-plan.md) + [`proposed-patches.md`](proposed-patches.md) |
| Reviewing the architecture | [`architecture-review.md`](architecture-review.md) + [`target-architecture.md`](target-architecture.md) |
| Triaging specific bugs | [`bug-report.md`](bug-report.md) — every finding, severity-ordered |
| Planning the work | [`industrialization-roadmap.md`](industrialization-roadmap.md) — phased plan with acceptance criteria |
| Owning ops / SRE | [`infrastructure-audit.md`](infrastructure-audit.md) |
| Owning security | [`security-audit.md`](security-audit.md) |
| Owning QA | [`testing-strategy.md`](testing-strategy.md) |
| Tracking risk | [`risk-register.md`](risk-register.md) |
| Tooling / automation | [`audit-index.json`](audit-index.json) — machine-readable summary |

## Document index

1. [`README.md`](README.md) — this file
2. [`executive-summary.md`](executive-summary.md) — assessment, readiness score, top risks, immediate actions
3. [`architecture-review.md`](architecture-review.md) — current design, loop analysis, weaknesses, target shape
4. [`bug-report.md`](bug-report.md) — every finding with evidence (the master list)
5. [`infrastructure-audit.md`](infrastructure-audit.md) — observability, config, reliability surfaces
6. [`security-audit.md`](security-audit.md) — prompt injection, secrets, trust boundaries
7. [`testing-strategy.md`](testing-strategy.md) — coverage assessment, gaps, proposed test pyramid
8. [`industrialization-roadmap.md`](industrialization-roadmap.md) — phased plan (Phase 0–5)
9. [`action-plan.md`](action-plan.md) — concrete engineering backlog
10. [`risk-register.md`](risk-register.md) — risk table
11. [`target-architecture.md`](target-architecture.md) — proposed industrial-grade design
12. [`proposed-patches.md`](proposed-patches.md) — patch-style recommendations per major issue
13. [`audit-index.json`](audit-index.json) — machine-readable summary

## Summary of the most important findings

| # | Severity | Finding | Files |
|---|---|---|---|
| F-01 | **P0** | MCP stdio tool calls have no per-call timeout and hold a mutex across a blocking pipe read — one hung server wedges every later turn touching it, and deadlocks graceful shutdown | `internal/mcp/client.go`, `internal/agent/mcptools/bridge_reconnect.go` |
| F-02 | **P0** | Untrusted tool output (web pages, MCP results, file contents, shell stdout) re-enters the prompt with no provenance/trust marking — prompt injection reaches full-host `shell_exec`, secret-laden child env, and persistent self-modification | `internal/agent/llm_agent.go:361` + all tool result feeders |
| F-03 | **P1** | `Budget.WithDeadline`/`NodeTimeout` are dead code — the 300s wallclock cap only refuses *new* steps; in-flight LLM/tool calls run unbounded | `internal/agent/budget.go:322-328` |
| F-04 | **P1** | Intra-turn tool work is never persisted — pause/resume or crash mid-turn loses all completed rounds and re-runs side-effecting tools | `internal/runner/runner_persist.go`, `internal/agent/llm_agent.go:282,361` |
| F-05 | **P1** | `shell_exec` and MCP subprocesses inherit the full process env (all secrets); model-facing output is never redacted | `shell_exec.go:393`, `mcp/client.go:83` |
| F-06 | **P1** | `fs_edit` with empty `old_string` corrupts any host file (interleaves `new_string` between every rune) | `internal/agent/tools/fs_edit.go:52,72` |
| F-07 | **P1** | Unbounded in-memory buffering of subprocess output → OOM on the shared host | `shell_exec.go:192-247`, `shell_bg.go:55-60` |
| F-08 | **P1** | No retry/backoff on provider 429/5xx; parsed `Retry-After` is discarded; no circuit breaker | `llm_agent_stream_retry.go:57-93` |

See [`bug-report.md`](bug-report.md) for the complete, severity-ordered list (2 P0, 15 P1, 31 P2, 24 P3).

## What is done well (do not regress these)

The audit found genuinely strong engineering that should be preserved as-is:

- **`iter.Seq2` discipline** — yield-after-false guards and drain-to-close are airtight; the `ParallelAgent` choreography has no constructible deadlock or goroutine leak (`-race` green).
- **Budget core** — TOCTOU-safe shared-atomic step counter, fan-out-proof tree, fail-fast env parsing, injectable clock, no process-global mutation.
- **Two-tier dedup ring** — fail-safe by design (args-only fingerprint pre-execution, result preview as progress veto), window-independent period-2 detection.
- **Cache-prefix hygiene** — `messages[0]` is a byte-stable constant; volatile hints ride a trailing copy; the historical "six poisoning sites" class is structurally eliminated and CI-replayed over 20 turns.
- **SSRF hardening** (`internal/web`) — dial-time IP pinning, fail-closed on any blocked DNS record, redirect revalidation, scheme/MIME/size allowlists. Best-in-class.
- **Process-group kill on both OSes**, append-only redacted audit ledger, namespaced collision-refusing MCP mounts, deterministic tool manifest ordering, bounded recovery counters (no infinite-loop path inside `LlmAgent`).
