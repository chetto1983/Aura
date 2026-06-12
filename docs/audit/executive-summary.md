# Executive Summary — Aura `internal/agent` Production-Readiness Audit

**Date:** 2026-06-12 (re-audit-2) · **Branch:** `tabula-rasa` · **Scope:** `internal/agent` + the runner/conversations/llm/mcp/agui/config packages that compose it.

> Supersedes the 2026-06-10 executive summary (score 5.5). This cycle re-verifies the 06-10/06-11 remediation against current code: most P0/P1 closures hold; two are over-credited; several new operational and durability gaps surface.

---

## Verdict

**Production-readiness score: 6.5 / 10.**

Aura's agent runtime is a tale of two systems. The **core execution loop is genuinely production-grade** — among the cleanest agent loops this auditor has reviewed: a TOCTOU-safe shared-atomic budget, a race-clean `iter.Seq2` stream contract with airtight yield-after-false discipline, a real circuit breaker checked *before* each call, end-to-end bounded timeouts, a two-tier dedup ring with a progress veto, and an untrusted-output provenance envelope (NFKC + HTML-escape + crypto/rand nonce). In isolation the loop scores ~8.5/10, and the prior P0/P1 pass was **real engineering** — the majority of its closures verify.

The score is held to 6.5 by the **production perimeter**, which this re-audit finds materially weaker than the prior cycle credited. Three classes of gap dominate:

1. **Crash durability is half-built.** Per-event journaling closed the *observability* of intra-turn state, but **not** the hazard it was credited with: a mutating tool (`fs_write`, `shell_exec`, `swarm_spawn`) commits its host side effect *before* the result turn is persisted, and the crash-recovery repair *drops* the dangling tool call — so the model re-issues it on resume. The in-memory dedup ring is reborn each turn and gives zero cross-turn protection. R-05 is over-credited as CLOSED.

2. **Production is operationally blind.** The long-lived `aura serve` daemon — the real production surface (scheduler + AG-UI + Telegram) — **never boots the OTel tracer**, so the one span the loop emits is dev-REPL-only. The agent core has **no structured logging** (only debug-gated `reasoningtrace`), and metrics are four monotonic expvar counters with **no latency histograms, no error rates, no cost, and no Prometheus endpoint**. You cannot operate, alert on, or debug this in production today.

3. **The trust and concurrency perimeter has two real holes.** Swarm child reports re-enter the *parent* prompt with **no untrusted envelope** (indirect prompt-injection laundering), and there is **no per-thread in-flight guard** — two concurrent runs on one conversation interleave history appends and corrupt it. Separately, the self-extension gate was deliberately opened for `always:false` skills (P5 policy) while the tool schema, doc comments, and operator alert were left advertising the *old* gated behavior — a misleading contract, not just a policy choice.

None of this requires a rewrite. Every fix is localized. Closing the eight P1 items lands the system at a defensible 7.5–8.

---

## Findings at a glance

| Severity | Count | Character |
|---|---|---|
| **P0** | 0 | Both prior P0s (untrusted-output envelope, MCP hang) verify closed for the direct paths |
| **P1** | 8 | 1 crash-durability hazard, 1 trust-boundary hole, 1 data-integrity bug, 1 security/contract regression, 4 operational blockers (tracer, logs, metrics, container) |
| **P2** | 20 | Context-compaction correctness, resume atomicity, breaker lifetime, secret-env divergence, disk growth, CI gap, test apex |
| **P3** | 12 | Dormant token-zeroing, cosmetic stream replay, span coverage, registry overwrite, coverage hygiene |

**Dimension scores:** loop correctness **9**, reliability **7**, tool execution **7**, maintainability **8**, testing **7**, security **6**, memory/context **6**, crash durability **4**, observability **3**, deployment/ops **3**.

---

## Top 5 critical risks

1. **Duplicated mutating side effects on crash recovery (B-01, P1).** A deploy/OOM/panic between a tool's execution and the persistence of its result turn causes the model to re-run the *same* mutating call on resume — a file written twice, a shell command run twice, a swarm re-fanned. Silent. `llm_agent.go:376-388`, `store_helpers.go:232-267`.

2. **Prompt-injection laundering via swarm reports (B-02, P1).** A swarm worker that fetches attacker-controlled content can synthesize a summary that re-enters the *parent* prompt unwrapped (no `<tool_output trust="untrusted">`), bypassing the boundary R-01 built for direct tools. `internal/swarm/runner_adapter.go:54`, `trust.go:14`.

3. **Conversation corruption under concurrent runs (B-03, P1).** Two `POST /agent/run` (or AG-UI + Telegram) on one thread both load and mutate history with no serialization → interleaved appends, double-spent budget, wire-invalid history. `agui/server.go:139`, `runner.go:211`.

4. **Operationally blind production (O-01/O-02, P1).** No tracer in the daemon, no structured logs, no latency/error/cost metrics, no Prometheus, PG-only `/healthz`. A repeat of the very P0 MCP-hang class would be undetectable. `serve.go`, `metrics.go`, `tracing.go`.

5. **Self-extension gate open with a lying contract (B-04, P1).** The model can write and immediately use an `always:false` skill in-conversation, ungated, with no operator alert — while the tool schema still tells the model (and any auditor) that changes require human approval. `skills/writer.go:94-148`, `tools/skill.go:99-112`.

---

## Recommended immediate actions (next 5)

1. **Close the crash re-execution hazard (B-01).** Write a tool-intent row inside the same transaction that persists the assistant `tool_calls` turn *before* execution; on reload, surface an unmatched intent as a synthetic "result unknown — verify before re-running" instead of silently dropping it. *Effort M.*

2. **Wrap swarm reports (B-02).** One line: set `Provenance{Source:"swarm", Trust:TrustUntrusted}` on the `swarm_spawn` ToolResult (or add `swarm_spawn` to `untrustedToolNames`). The envelope path already keys off provenance. *Effort S.*

3. **Add a per-thread in-flight guard (B-03).** A `sync.Map[threadID]*mutex` (or singleflight) in `Runner`, shared by AG-UI and Telegram; reject or serialize a second concurrent run on the same thread. *Effort M.*

4. **Make production observable (O-01/O-02).** Boot the tracer in `serve`, install a default `slog` JSON handler with `request_id`/`thread_id` correlation, and add a Prometheus `/metrics` endpoint with turn/tool/LLM latency histograms + error and in-flight gauges. *Effort S/M.*

5. **Fix the self-extension contract (B-04).** Update the `skill` tool schema + doc comments to state the true auto-activate policy, and fire the operator alert/audit row on the ungated path too. *Effort S.*

A fully sequenced plan is in [`industrialization-roadmap.md`](industrialization-roadmap.md); the concrete backlog is in [`action-plan.md`](action-plan.md); patch-level guidance is in [`proposed-patches.md`](proposed-patches.md).
