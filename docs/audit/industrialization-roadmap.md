# Industrialization Roadmap — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`

Step-by-step plan to take the agent runtime from "strong prototype with a verified loop core" to production-grade. **The loop core is kept intact** — work is perimeter hardening, ordered by risk-reduction-per-effort. Effort: **S** (≤1 day) · **M** (2–4 days) · **L** (1–2 weeks). Each phase lists priority, effort, impact, dependencies, and acceptance criteria.

---

## Phase 0 — Stabilization (crash & corruption firewall)

> Goal: the daemon cannot be killed or corrupted by one bad turn. Highest leverage; do first.

| Item | Finding | Effort | Impact |
|---|---|---|---|
| `recover()` in every spawned goroutine + Runner backstop | AG-001 (P0) | S–M | Eliminates daemon-wide crash from one panicking tool/child |
| Mutex on `dedupRing` | AG-002 (P1) | S | Removes latent concurrent-map-write fatal |
| Validate `max_steps>0`/`wallclock>0` at `NewBudget`; `=0` MCP timeout → default | AG-036/AG-006 | S | Config typos can't silently disable/hang the runtime |
| Cycle/visited guard in `findInTree` | AG-037 (P2) | S | No stack-overflow on a malformed agent tree |

- **Dependencies:** none. Each is local and independently shippable.
- **Acceptance:** a panicking fake tool through `executeBatch`/`parallel`/`swarm` returns a per-call/per-child error with the process alive, under `-race`+`goleak`; `-race` dedup hammer test green; `NewBudget(0)` errors; cyclic tree bounded.

## Phase 1 — Observability & reliability

> Goal: production is no longer blind; transient dependency faults degrade instead of cascading.

| Item | Finding | Effort | Impact |
|---|---|---|---|
| `slog` structured logs at turn/LLM/tool/hook boundaries (request_id/thread_id keyed) | AG-013 (P1) | M | Greppable production diagnostics |
| Metric set: `turn_total{outcome}`, `llm_call_duration_seconds`, `*_errors_total`, tokens, hooks, span-export-failures; non-default registry | AG-012/AG-057 (P1) | M | SLOs + alerting become possible |
| `mintSpanID` never panics; tracer boot-log; confirm daemon boots tracer | AG-033/AG-056/AG-013 | S | Telemetry can't crash the daemon; silent-drop visible |
| MCP resilience: single-flight reconnect off-lock + `WithoutCancel` + backoff + circuit breaker | AG-005 (P1) | M–L | No reconnect livelock / head-of-line freeze |
| Embed-sidecar breaker + reasoning-router fallback policy (static tier, opt-in router) | AG-008 (P1) | M | No per-turn latency cliff when sidecar degrades |
| Active wallclock deadline ctx wired into the run | AG-041 (P2) | S | Hard wall-time bound, not step-boundary soft gate |

- **Dependencies:** Phase 0 (panic firewall) should land first so chaos tests are meaningful.
- **Acceptance:** every terminal `turnReason` increments a labeled counter; an LLM-latency histogram is scraped; a crash-loop MCP server trips backoff+breaker; an embed outage adds no extra LLM round-trip; a Grafana dashboard shows turn outcome + latency + tool-error rate.

## Phase 2 — Architecture hardening

> Goal: tighten the boundaries the loop crosses — durability, provenance, config.

| Item | Finding | Effort | Impact |
|---|---|---|---|
| Default-untrusted provenance for unknown tools; propagate untrusted through `swarm_spawn` reports | AG-052 (prior B-02) | S–M | Closes the indirect-injection laundering path |
| Boot-time config resolution + validation (no hot-path env reads) | AG-027 (P2) | M | A malformed env can't make every MCP call loop-fatal mid-run |
| MCP schema total-size/property-count cap; reconnect Mutating-flip warning | AG-025/AG-024 (P2) | S–M | Hostile/buggy server can't bloat the manifest or silently flip mutation |
| Crash-resume checkpoint of history+counters keyed by sessionID (Runner) | AG-042 (P2) | L | Long autonomous runs survive a crash |
| Generalize dedup to period-3+ cycles; prune `results` map on eviction | AG-040/AG-039 (P2) | S | Loop guard catches longer thrash; no unbounded map growth |

- **Dependencies:** Phase 1 observability (so checkpoint/restore is observable).
- **Acceptance:** a swarm child's malicious web content is enveloped in the parent prompt; config errors fail loud at boot; a kill-9 mid-run resumes from the last checkpoint; a 3-tool oscillation is caught before budget exhaustion.

## Phase 3 — Security hardening

> Goal: capability gating + secret boundary; prepare for beyond-single-operator.

| Item | Finding | Effort | Impact |
|---|---|---|---|
| Wire `capability_grants` into dispatch for `Mutating && Untrusted` MCP tools | AG-007 (P1) | M | Per-call authorization, not server-boundary-binary |
| Reconcile skill self-activation: delete dead schema, gate or honestly document + alert | AG-011 (P1) | S–M | No silent self-modification / injection persistence |
| Command-hook sandbox: minimal env (no inherited secrets), exec-by-fd, validate rewrites, audit log | AG-003 (P1) | M | Hook surface no longer leaks secrets / TOCTOU-exploitable |
| DSN-aware `IsSecretEnvKey` + value-scan redaction | AG-010/AG-047 (P1/P3) | S | DB password no longer reaches shell children / model |
| Gate deferred `agent_job` schedules (or surface at fire time) | AG-016 (P2) | S | Deferred-execution privilege surface gated |
| Reasoning-trace PII handling: don't dump full history by default; cap fields | AG-009 (P1) | S | No plaintext-conversation-at-rest by default |

- **Dependencies:** `capability_grants` (Slice 1.7) substrate exists; this wires it in.
- **Acceptance:** a mutating MCP tool without a grant is refused/confirmed; a self-authored skill stays pending or emits an alert; hook children have no secret env; `IsSecretEnvKey("AURA_DB_URL")==true`.

## Phase 4 — Scalability & production operations

> Goal: safe to deploy beyond the operator's own machine.

| Item | Finding | Effort | Impact |
|---|---|---|---|
| Production container (non-root, read-only rootfs, resource limits) | prior D-01 | M | Bounds the blast radius of arbitrary `shell_exec` |
| `/readyz` checking provider/embed/MCP/Neo4j reachability | infra §6 | S–M | Real readiness vs liveness |
| Per-thread in-flight guard (concurrent runs on one thread) | prior B-03 (agui/runner) | M | No history corruption / budget double-spend |
| Rate-limit / backpressure on outbound LLM+tool beyond per-turn budget | infra §7 | M | Cost + provider-quota protection |
| Multi-tenant re-rating gate: re-classify AG-003/AG-007/AG-011 as P0; add sandbox | security §7 | L | Gate before any non-single-operator deployment |

- **Dependencies:** Phases 0–3.
- **Acceptance:** the daemon runs in a non-root container with resource limits; `/readyz` flips on a dependency outage; two concurrent turns on one thread don't interleave history.

## Phase 5 — Long-term maintainability

> Goal: reduce the convention-load-bearing surface and dead code.

| Item | Finding | Effort | Impact |
|---|---|---|---|
| Remove dead code (`skillParamsSchema`, `openManagedServer` if unref) | AG-044/AG-028 | S | DEEP-REFACTOR-ON-TOUCH compliance |
| Document load-bearing concurrency contracts (registry atomic-swap, swarm-ctx read-only, dedup serial) | AG-029/AG-062/AG-002 | S | Future edits don't silently break invariants |
| Unify glob semantics (`fs_grep` vs `fs_glob`); atomic file writes | AG-046/AG-045 | S | Fewer model-facing surprises |
| Chaos/soak test tier in CI (panic, crash-loop MCP, hung server) | testing §3 | M | Failure modes regression-locked |
| Multilingual reasoning seeds / document Italian-corpus assumption | AG-055 | S | Correct routing for non-Italian operators |

- **Dependencies:** prior phases land the fixes these document/test.
- **Acceptance:** `deadcode ./...` clean; concurrency contracts commented at the boundary; chaos lane green nightly.

---

## Sequencing summary

```
Phase 0 (days)  → Phase 1 (1-2 wk) → Phase 2 (1-2 wk) → Phase 3 (1 wk) → Phase 4 (2-3 wk) → Phase 5 (ongoing)
crash firewall    observe+resilient   harden+durable     gate+secrets     deploy-safe        maintain
```

After Phase 0+1 the blended readiness score moves from **6.5 → ~7.5**; after Phase 3, **~8.5**; Phase 4 is the gate for multi-tenant/off-operator deployment.
