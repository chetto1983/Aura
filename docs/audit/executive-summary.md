# Executive Summary

## Production Readiness Score

Score: 5.5 / 10

The core loop is promising and has several serious safeguards, and running Aura in a container improves the execution boundary compared with a raw host process. The production boundary is still effectively "trusted local operator inside a powerful runtime container." That is not yet an industrial-grade boundary unless the container is hardened and paired with enforced execution policy. Before production exposure, Aura needs capability profiles, container/workspace-aware filesystem and shell policy, durable tool idempotency/checkpointing, stricter skill self-extension governance, and operational hardening.

## Strengths

- Bounded loop with default max steps and wallclock budget in `internal/agent/budget.go`.
- LLM stream-open retries and circuit-breaker behavior in `internal/agent/llm_agent_stream_retry.go`.
- Tool-call deduplication and stable result hashing in `internal/agent/budget_dedup.go`.
- Untrusted tool-output framing by default in `internal/agent/trust.go`.
- SSRF-resistant web fetch flow in `internal/web/ssrf.go` and `internal/web/fetcher.go`.
- Event persistence and repair of malformed tool-call history in `internal/runner/runner_persist.go` and `internal/conversations/store_helpers.go`.
- OpenTelemetry and Prometheus/expvar metrics exist in `internal/agent/tracing.go` and `internal/agent/metrics.go`.

## Top Risks

1. Unrestricted shell and filesystem access can turn prompt injection or model error into arbitrary compromise of the Aura container, mounted volumes, injected secrets, and reachable networks.
2. Model-authored skill create/update auto-activation allows the agent to modify its own future behavior without human approval.
3. Detached background shells can outlive the originating turn and lack session ownership enforcement.
4. Mixed final response plus side-effecting tool calls can mutate host state while appearing to finalize a turn.
5. Crash recovery after side-effecting tools is not a full idempotent transaction boundary.

## Finding Counts

- P0: 0
- P1: 6
- P2: 9
- P3: 5
- Total: 20

## Recommended Immediate Actions

1. Add an execution capability profile that defaults remote/server contexts to read-only tools inside the container.
2. Disable model-authored skill auto-activation outside a disposable local sandbox.
3. Make filesystem writes atomic and add tests for crash-safe overwrite behavior.
4. Add TTL, ownership, and per-session authorization to background shell jobs.
5. Enforce terminal-tool exclusivity: `text_response` must not share a batch with mutating tools.
