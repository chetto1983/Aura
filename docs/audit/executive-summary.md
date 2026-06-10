# Executive Summary — Aura `internal/agent` Production-Readiness Audit

**Date:** 2026-06-10 · **Scope:** `d:\Aura\internal\agent` (~9k non-test LOC, ~13.6k test LOC) · **Branch:** `tabula-rasa` @ `0ab722e5`

---

## High-level assessment

Aura's agent runtime is **a well-engineered core wrapped in an under-hardened perimeter.** The central ReAct loop (`LlmAgent`), the budget system, the dedup ring, the `iter.Seq2` streaming contract, and the KV-cache prefix discipline are genuinely strong — race-clean, principled, and backed by 406 test functions including property-based and goroutine-leak tests. Two independent auditors (loop correctness, memory) confirmed there is **no infinite-loop or cache-poisoning path inside `LlmAgent` itself**, and the parallel-agent choreography survived adversarial attempts to construct a deadlock.

The problems are at the **seams and the boundary**, and they are serious:

1. **The trust boundary is unmarked.** Untrusted content (web pages, MCP results, file contents, shell stdout) re-enters the model's prompt with no provenance tag. Combined with full-host `shell_exec`, full-environment secret inheritance, and ungated auto-activating skill self-extension, a single prompt-injection vector reaches host code execution, secret exfiltration, and a persistent self-installed backdoor. This is the dominant risk class.

2. **The reliability controls are partly inert.** The headline "300s wallclock DoS cap" only refuses *new* steps — the code that would actually cancel in-flight work (`Budget.WithDeadline`, `NodeTimeout`) is **dead** (zero production callers). MCP tool calls have no per-call timeout and hold a mutex across a blocking pipe read, so one hung server permanently wedges every later turn. There is no retry on provider 429/5xx, no circuit breaker, no metrics, and no health endpoint.

3. **Durability is lossy in ways that cause real data harm.** Intra-turn tool work is never persisted, so a pause/resume or crash mid-turn loses all completed rounds and **re-executes side-effecting tools**. A crash in a narrow window between two pause-writes permanently bricks a conversation with a wire-invalid history (provider 400 on every future turn).

4. **A few sharp implementation bugs.** `fs_edit` with an empty `old_string` silently corrupts any host file; subprocess output is buffered unbounded in RAM (OOM on the shared host); the model can approve its own gated destructive scheduled task.

None of these is a reason to redesign the core. They are a focused, mostly small-fix backlog standing between a strong prototype and a production-safe system.

## Production readiness score

# 5.5 / 10

**Rationale.** The engineering quality of the core is high (would be 8+ in isolation), but "production-grade" is gated by the perimeter: 2 P0s (one a system-wide hang, one a security-architectural blast radius), 15 P1s including data-corruption (`fs_edit`), data-loss (intra-turn persistence), conversation-bricking (orphan tool_result), and accidental-OOM (subprocess buffering), plus the complete absence of operational observability (metrics, health). A system that can corrupt a host file on a common model mistake, broadcast all secrets to a child process, and hang forever on an unresponsive sidecar is not yet shippable as an industrial product — but every one of those is a bounded, well-localized fix. After Phase 0 + Phase 1 of the roadmap, a re-score of **7.5–8** is realistic.

| Dimension | Score | Note |
|---|---|---|
| Loop correctness | 8/10 | Excellent; gaps only at composition seams |
| Tool execution | 5/10 | `fs_edit` corruption, unbounded buffers, self-approve gate |
| Memory/context | 6/10 | Cache discipline strong; persistence lossy |
| Reliability | 4/10 | Dead cancellation code, no retry/breaker, MCP hang |
| Observability | 2/10 | No metrics, no health, traces dropped by default |
| Security | 3/10 | Unmarked trust boundary; full-env to children |
| Testing | 7/10 | Strong techniques; coverage-gate gap, no Windows lane |
| Maintainability | 8/10 | Clean ≤600 LOC files, typed contracts, documented decisions |

## Top risks

1. **Prompt-injection → host RCE + secret exfiltration** (P0). Unmarked untrusted tool output + full-host shell + full-env children. The keystone risk.
2. **MCP hang wedges the daemon** (P0). No per-call timeout, mutex held across a blocking read; one unresponsive server is a permanent silent hang and a shutdown deadlock.
3. **Data corruption via `fs_edit` empty `old_string`** (P1). A common model mistake destroys host files with no backup.
4. **Wallclock cannot cancel in-flight work** (P1). The DoS cap is inert; a hung tool runs unbounded.
5. **Intra-turn persistence loss → duplicated side effects** (P1). Pause/resume or crash re-runs mutating tools the model already executed.
6. **Conversation-bricking orphan tool_result** (P1). A crash window leaves a wire-invalid history that 400s on every future turn.
7. **Secrets broadcast to every child process** (P1). `os.Environ()` to shells and MCP servers; model-facing output unredacted.
8. **Auto-activating skill self-extension** (P1). Injection can install a persistent, reboot-surviving standing-instruction backdoor.
9. **No retry/breaker on provider errors** (P1). One 429 kills a turn; an outage hammers a dead provider.
10. **Operationally blind** (P1). No metrics, no `/healthz` — the P0 hang is undetectable in production.

## Recommended immediate actions (Phase 0 — Stabilization)

These are the highest impact-per-effort fixes; all are localized and low-risk. Estimated total: **~1–1.5 engineer-weeks.**

1. **Reject empty `old_string` in `fs_edit`** (1 line + test). Stops file corruption today.
2. **Add a per-call MCP timeout + ctx-aware read** (bridge + transport). Stops the daemon-wide hang.
3. **Wire `Budget.WithDeadline` into the runner + clamp `shell_exec timeout_ms`.** Makes the wallclock real.
4. **Cap subprocess output buffers (ring/tail) + free background buffers.** Stops accidental OOM.
5. **Filter the child environment (strip secret-shaped vars) for `shell_exec` and MCP launch.** Stops ambient secret broadcast.
6. **Remove `approve` from the model-facing `task` enum.** Closes the self-approval bypass.
7. **Add `GET /healthz` + basic counters.** Makes the system observable enough to operate.
8. **Remove `/internal/agent/tools/` from the coverage-gate exclusion.** Re-enables the floor on the riskiest package.

Then Phase 1 (observability + reliability: retry/backoff/breaker, slog, parallel-tool cap, intra-turn persistence) and Phase 3 (security: provenance tagging, skill always-gate, send_file fence). See [`industrialization-roadmap.md`](industrialization-roadmap.md) and [`action-plan.md`](action-plan.md).
