# Action Plan — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc` · Cross-references [`bug-report.md`](bug-report.md).
Concrete engineering backlog by priority. Each task: title · description · owner role · expected outcome · acceptance criteria.

---

## Immediate (P0 / P1 — before production)

### A1. Panic firewall in all spawned goroutines — `AG-001` (P0)
- **Description:** Add a `safeGo`-style recover wrapper to every goroutine body in `executeBatch` (`llm_agent_parallel.go:43`), `workflow/parallel.go runSub`, `swarm.runWave`, and the `shell_bg` reaper; convert a recovered panic into a per-call `toolRunResult{Err}` / per-child `{status:failed}` report. Add a Runner-level per-turn recover backstop.
- **Owner:** Runtime/concurrency engineer.
- **Expected outcome:** A panicking tool degrades to a model-visible error; the daemon never dies from one turn.
- **Acceptance:** `-race`+`goleak` test dispatches a panicking fake tool through all three paths; process survives; error surfaces; no goroutine leak.

### A2. Mutex on `dedupRing` — `AG-002` (P1)
- **Description:** Guard `entries`/`results` in `budget_dedup.go` with a `sync.Mutex`.
- **Owner:** Runtime engineer.
- **Expected outcome:** No concurrent-map-write fatal even if a future change shares a ring across goroutines.
- **Acceptance:** `-race` concurrent `Before/AfterToolResult` hammer + multi-tool `dispatch` green.

### A3. MCP resilience — `AG-005` / `AG-006` (P1)
- **Description:** `singleflight` reconnect performed *outside* `s.mu`; reconnect with `context.WithoutCancel(parent)` + a dedicated timeout; exponential backoff + per-server circuit breaker with cooldown. Treat `AURA_MCP_CALL_TIMEOUT_SEC=0` as "default"; resolve+validate timeouts at boot.
- **Owner:** Integrations engineer.
- **Expected outcome:** A flapping/hung MCP server degrades gracefully instead of freezing or thrashing the runtime.
- **Acceptance:** concurrent-call test shows no head-of-line block during a slow reconnect; crash-loop server trips breaker after N; hung server bounded by the default deadline; no leaked call goroutines.

### A4. Hook surface hardening — `AG-003` / `AG-004` (P1)
- **Description:** Default hook `cmd.Env` to a minimal allowlist (no inherited provider/DB secrets); exec the held fd (or require a root-owned path) to close the TOCTOU; validate hook-supplied `Request`/`ToolCall`/`ToolResult` bounds; add a per-hook `fail_open`/`fail_closed` policy (default `fail_open` for non-security hooks); `reasoningtrace.Record` every rewrite/deny; wrap in-process hook calls in `recover()`.
- **Owner:** Platform/security engineer.
- **Expected outcome:** A hook fault or compromise is contained; secrets don't leak to hook subprocesses.
- **Acceptance:** child env has no secret-named vars; a failing hook completes the turn under `fail_open`; an oversized rewrite is rejected; rewrites appear in the audit trail.

### A5. Secret boundary — `AG-010` / `AG-009` (P1)
- **Description:** Add URL/DSN markers (`url,dsn,uri,conn,pwd,cookie,session,jwt`) to `secret.IsSecretEnvKey` + a `scheme://user:pass@` value-scan; add a DSN pattern to the shell output redactor. Stop dumping full `history` to the reasoning trace by default; cap per-field size.
- **Owner:** Security engineer.
- **Expected outcome:** DB password no longer reaches shell children or the model; no plaintext-conversation-at-rest by default.
- **Acceptance:** `IsSecretEnvKey("AURA_DB_URL")==true`; a shell child cannot read the composed DSN; trace does not write verbatim history by default.

### A6. Observability minimum — `AG-012` / `AG-013` / `AG-033` (P1)
- **Description:** `slog` at turn/LLM/tool/hook boundaries (request_id/thread_id keyed); add `turn_total{outcome}`, `llm_call_duration_seconds`, `llm_errors_total{kind}`, `tool_errors_total{tool}`, token + hook + span-export-failure counters (non-default registry); `mintSpanID` falls back to zero ID instead of panicking; boot-log the exporter mode.
- **Owner:** Observability/SRE engineer.
- **Expected outcome:** SLOs + alerting possible; telemetry can't crash the daemon.
- **Acceptance:** each terminal `turnReason` increments a counter; an LLM-latency histogram is scraped; injecting an entropy failure does not panic.

### A7. Capability gate on mutating tools — `AG-007` / `AG-011` (P1)
- **Description:** Consult `capability_grants` (Slice 1.7) at dispatch for `Mutating && Untrusted` MCP tools; reconcile skill self-activation (delete dead `skillParamsSchema`, gate or honestly document + restore the operator alert).
- **Owner:** Security/runtime engineer.
- **Expected outcome:** Per-call authorization for side-effecting and self-modifying actions.
- **Acceptance:** a mutating MCP tool without a grant is refused/confirmed; a self-authored skill stays pending or emits an alert; exactly one skill schema is referenced.

### A8. Reasoning-router fallback policy — `AG-008` (P1)
- **Description:** When the classifier is wired but abstains/errors, short-circuit to a static `ReasoningTierLow` instead of an LLM router round-trip; make the LLM router opt-in; add an embed-sidecar circuit breaker; cap the router timeout far below 8s.
- **Owner:** Runtime engineer.
- **Expected outcome:** No per-turn latency cliff when the embed sidecar is down.
- **Acceptance:** with the embedder erroring, no router LLM call is issued (or one then breaker-open); turn latency unaffected.

---

## Short-term improvements (P2)

- **A9. Config validation at boot** (`AG-036`/`AG-027`): reject `max_steps<1`/`wallclock<1`; resolve MCP timeout once at boot; no hot-path env reads. *(Backend engineer.)*
- **A10. fs size cap** (`AG-014`): `AURA_FS_MAX_READ_BYTES`, stat-then-reject or hard-limit stream; auto-page. *(Tools engineer.)*
- **A11. Active wallclock deadline** (`AG-041`): wire `Budget.WithDeadline` into the run ctx. *(Runtime engineer.)*
- **A12. MCP name-collision + schema-size** (`AG-022`/`AG-023`/`AG-025`): boot-validate namespace uniqueness; cap total schema bytes/property count. *(Integrations engineer.)*
- **A13. Default-untrusted provenance** (`AG-052`): unknown tools + `swarm_spawn` reports treated untrusted. *(Security engineer.)*
- **A14. Background-shell eviction + tree-cycle guard** (`AG-015`/`AG-037`): `SessionEvictor` on `BackgroundShells`; visited-set in `findInTree`. *(Runtime engineer.)*
- **A15. Dedup hardening** (`AG-039`/`AG-040`): prune `results` on eviction; period-3+ cycle detection. *(Runtime engineer.)*
- **A16. agent_job gating** (`AG-016`): gate deferred schedules or surface at fire time. *(Scheduler engineer.)*

## Medium-term architecture work

- **A17. Crash-resume checkpoint** (`AG-042`): incremental snapshot of history+counters keyed by sessionID (Runner). *(Persistence engineer.)*
- **A18. Cache-prefix drift detector** (`AG-031`): compute+compare `PrefixHash` per turn after `BeforeModel`; emit a `prefix_drift` metric. *(Runtime engineer.)*
- **A19. classifier cold-start lock scope** (`AG-032`): build the anchor bank outside `c.mu` / `singleflight`. *(Runtime engineer.)*
- **A20. `/readyz` + dependency breakers** (infra §6/§10): readiness probe; embed + per-MCP breakers. *(SRE.)*

## Long-term industrialization

- **A21. Production container** (prior D-01): non-root, read-only rootfs, resource limits for arbitrary-shell runtime. *(Platform engineer.)*
- **A22. Per-thread in-flight guard** (prior B-03, agui/runner): prevent concurrent-run history corruption. *(Runtime engineer.)*
- **A23. Multi-tenant gate** (security §7): re-rate AG-003/AG-007/AG-011 as P0 + add a real sandbox before any off-operator deployment. *(Security lead.)*
- **A24. Chaos/soak CI tier** (testing §3): panic injection, crash-loop MCP, hung server. *(QA/SRE.)*
- **A25. Dead-code + contract docs** (`AG-044`/`AG-028`/`AG-029`): `deadcode ./...` clean; document load-bearing concurrency contracts. *(Any engineer on touch.)*

---

## Suggested execution order

`A1 → A2 → A3 → A5 → A6` (crash + secret + visibility) → `A4 → A7 → A8 → A9–A16` (gating + hardening) → `A17–A20` (durability + arch) → `A21–A25` (deploy-safe + maintain). A1+A2 are hours-to-a-day each and remove the two crash-class issues — do them first.
