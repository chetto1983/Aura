# Executive Summary — `internal/agent` Production-Readiness Audit

**Date:** 2026-06-15 · **HEAD:** `136325dc` · **Branch:** `master`
**Scope:** `internal/agent` (loop · tools · mcptools · workflow · prompt · hooks · tracing · budget) with verified call-sites in `internal/secret`, `internal/web`, `internal/swarm`, `internal/reasoningtrace`, `internal/db`.

---

## High-level assessment

Aura's agent runtime is a **strong, well-reasoned core wrapped in an under-hardened operational perimeter.** The reasoning loop itself is among the better autonomous-agent loops I have audited: it has a *non-empty terminal contract* (every termination path yields real prose — `text_response` → forced synthesis → a deterministic Italian stub — never an empty answer and never the error slot), *bounded recovery* (every `continue` in the main loop is gated by a per-run counter, so the "203-turn thrash" class of infinite loop is provably prevented), a *shared-atomic budget* that deliberately defeats the `max_steps^depth` swarm fan-out bomb, *KV-cache stable-prefix discipline* (volatile hints are tail-injected to a copy; `messages[0]` is byte-stable), genuinely strong *SSRF hardening* (pinned-IP dial, scheme allowlist, metadata-IP block, per-hop redirect re-validation), and a *crypto-nonce untrusted-output envelope* that is a real prompt-injection mitigation. Test investment is serious: **105 test files / ~19.4k test LOC** against ~12.1k non-test LOC, with `goleak`-guarded `TestMain`, property tests, and a fuzz test.

What holds it back from "industrial production-grade" is **not** the algorithm — it is the things a long-lived daemon needs and a clever loop does not provide on its own: **panic isolation, MCP resilience, hook sandboxing, observability for SLOs, secret hygiene at the process boundary, and crash-resumability.**

## Production-readiness score: **6.5 / 10**

| Dimension | Score | Note |
|---|---|---|
| Loop correctness & determinism | 8.5 | Bounded, well-tested, non-empty terminal contract |
| Tooling safety (within trust model) | 7.5 | Strong SSRF/trust wrapping; secret-env + skill-gate gaps |
| Concurrency & resilience | 5.0 | **No panic isolation**; MCP reconnect livelock; soft wallclock |
| Observability | 4.5 | Spans + counters exist; no slog, no latency/error/cost metrics |
| Security (multi-tenant readiness) | 5.0 | Single-operator model is coherent; hook + capability-grant gaps |
| Testing | 7.5 | Excellent breadth; gaps in panic/race/chaos tiers |
| Operability (deploy/secrets/health) | 5.0 | No structured logs; daemon observability thin |
| **Blended** | **6.5** | Strong core, perimeter not yet production-hardened |

The score is the *blended* figure. The loop in isolation is ~8.5; the operational perimeter (~4.5–5) pulls it down. A focused Phase-0/Phase-1 effort (below) would credibly move this to ~8.

## Findings tally

- **1 P0** — unrecovered panics in spawned goroutines crash the whole multi-channel daemon.
- **12 P1** — dedup race, hook TOCTOU/secret-env/fail-soft, MCP reconnect concurrency, `=0` timeout hang, no capability gate on mutating MCP tools, reasoning-router latency cliff, plaintext trace logging, DSN secret leak, ungated skill self-activation, no SLO metrics, no structured logs.
- **26 P2** · **24 P3** — see `bug-report.md`.

> The 4 deep-readers independently re-derived several findings the prior 2026-06-12 cycle had already logged (skill self-extension gate ≈ B-04, swarm reports unwrapped ≈ B-02, observability ≈ O-01/O-02), which raises confidence in both audits. This cycle adds findings the prior pass did not surface — panic isolation, MCP reconnect concurrency detail, the `=0` timeout hang, the reasoning-router latency cliff, and the DSN secret leak.

## Top 5 risks

1. **Daemon-wide crash from one bad tool call (AG-001, P0).** No `recover()` in any spawned goroutine (verified repo-wide; only `db/tx.go` recovers). In `aura serve`, one panicking tool/child terminates the process for every channel and user.
2. **MCP resilience livelock (AG-005/AG-006, P1).** Reconnect holds the server lock across subprocess spawn + handshake, binds to the failed call's ctx, and has no backoff/circuit-breaker; `AURA_MCP_CALL_TIMEOUT_SEC=0` removes the only hang backstop. A flapping or hung MCP server can freeze or thrash the runtime.
3. **Hook subsystem is an unsafe extension surface (AG-003/AG-004, P1).** Exec TOCTOU, full `os.Environ()` (every secret) handed to hook subprocesses, unvalidated wholesale request rewrites, and any hook fault hard-aborts the turn. Becomes P0 in multi-tenant.
4. **Production blindness (AG-012/AG-013, P1).** No structured logs in the agent core; no latency/error/cost/turn-outcome metrics. You cannot build an SLO or alert from what is exported today.
5. **Self-modification + secret-leak gaps (AG-011/AG-010/AG-007, P1).** The model can auto-activate self-authored skills despite gated-looking docs; the DB DSN leaks into shell children; mutating MCP tools run with no per-call capability gate.

## Recommended immediate actions (next 5)

1. **Add `recover()` to every spawned goroutine** (`executeBatch`, `parallel.runSub`, `swarm.runWave`, `shell_bg` reaper) + a Runner-level backstop; translate panics to per-call/per-child errors. *(AG-001 — highest leverage, ~1 day.)*
2. **Add a `sync.Mutex` to `dedupRing`** to remove the latent concurrent-map-write fatal that `recover()` cannot catch. *(AG-002 — hours.)*
3. **Harden the MCP reconnect path:** single-flight reconnect outside the lock, `context.WithoutCancel` + dedicated timeout, exponential backoff + circuit breaker; treat `AURA_MCP_CALL_TIMEOUT_SEC=0` as "default" and validate at boot. *(AG-005/AG-006 — 2–3 days.)*
4. **Close the secret-boundary holes:** add URL/DSN markers to `secret.IsSecretEnvKey`; stop inheriting `os.Environ()` into command hooks (minimal allowlist). *(AG-010/AG-003 — ~1 day.)*
5. **Add the observability minimum:** `slog` at turn/LLM/tool boundaries + Prometheus `turn_total{outcome}`, `llm_call_duration_seconds`, `*_errors_total`, token counters; never panic in `mintSpanID`. *(AG-012/AG-013 — 2–4 days.)*

After these, re-score; the loop core does not need rework — the work is perimeter hardening. See `industrialization-roadmap.md` for the phased plan and `action-plan.md` for the backlog.
