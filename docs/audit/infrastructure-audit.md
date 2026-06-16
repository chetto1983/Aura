# Infrastructure Audit

## Current State

Aura has important infrastructure pieces in place:

- Structured logging through `slog`.
- Prometheus and expvar counters/histograms in `internal/agent/metrics.go`.
- OpenTelemetry span creation and exporter setup in `internal/agent/tracing.go`.
- Budget-based wallclock and step limits in `internal/agent/budget.go`.
- Conversation persistence and context compaction in `internal/conversations`.
- Tool invocation ledger in `internal/toolinvocations`, called from `internal/runner/runner_persist.go`.
- Web SSRF guards in `internal/web`.

These are useful foundations. The missing piece is a production operations envelope that makes dependency state, permissions, durability, and isolation explicit.

## Missing Capabilities

## Configuration Management

Findings:

- Budget env parsing mostly fails fast, but `AURA_LOOP_MAX_PARALLEL_TOOLS` silently falls back to the default in `internal/agent/llm_agent_parallel.go` lines 89-99.
- Execution permissions are not centralized. Tool behavior is determined by composition-root registration and individual tool code.

Recommendations:

- Add a typed runtime config object for agent execution policy.
- Validate all env/config values at boot.
- Expose an effective configuration endpoint or diagnostic command with secrets redacted.

## Secrets Handling

Findings:

- Shell output redacts known secret-shaped environment values.
- Reasoning trace full mode can persist plaintext prompts/history when `AURA_REASONING_TRACE=full`.
- Sidecar files store large outputs and conversation content as plaintext `0600` files.

Recommendations:

- Default production full-trace to disabled.
- Add encryption-at-rest for sidecars or make sidecar storage pluggable.
- Add retention and secure cleanup for run directories.

## Logging, Metrics, And Tracing

Findings:

- Metrics exist for budget steps, tool dispatch, LLM stream opens/retries, turn outcomes, errors, hooks, tokens, cost, and tracing failures.
- Tool ledger failure is logged and non-fatal.
- Trace exporter failures are counted/logged, but dependency readiness is not centralized.

Recommendations:

- Add SLO-oriented dashboards: turn success rate, tool error rate, tool latency, LLM latency, budget terminations, retry rate, crash recovery count, sidecar spill rate.
- Add alerts for ledger-degraded, full-trace-enabled, shell background job count near cap, repeated tool panics, and MCP breaker open.
- Add trace attributes for capability profile, identity class, tool risk tier, and approval decision.

## Health Checks

Missing production health checks:

- Database connectivity and migration version.
- LLM provider connectivity and rate-limit state.
- MCP server status and breaker state.
- Embedder/semantic search sidecar.
- Scheduler/cron worker status.
- Background shell supervisor status.
- Sidecar filesystem writability and capacity.
- OTel exporter connectivity.

Recommendation:

- Implement `/healthz` for process liveness and `/readyz` for dependency readiness.
- Separate optional degraded dependencies from required dependencies in a machine-readable health payload.

## Queues And Backpressure

Findings:

- Tool execution has a bounded worker pool per batch.
- Background shells have a running-count cap.
- The tool ledger is best-effort and does not queue failed events.

Recommendations:

- Add global and per-identity turn concurrency limits.
- Add durable outbox for tool ledger and outbound notifications.
- Add rate limits per identity/channel/tool class.
- Add queue depth metrics and rejection reasons.

## Persistence And Checkpointing

Findings:

- Runner persists turn events and repairs malformed tool-call pairs on load.
- Crash after mutating tool execution can still leave "previous result unknown" instead of a durable committed tool result.

Recommendations:

- Add durable tool transaction records with idempotency keys.
- Require mutating tools to declare idempotency/compensation behavior.
- Persist tool start before execution and result/commit atomically with the recovery record.

## Runtime Isolation

Findings:

- Shell and native filesystem tools run with full process privileges.
- Skill auto-activation assumes the container/host is the trust boundary.

Recommendations:

- Add sandbox profiles: `read_only`, `workspace_write`, `network_disabled`, `full_host_break_glass`.
- Use OS-level isolation where possible: Windows restricted tokens/WFP, Linux namespaces/Landlock/bubblewrap, containerized tool runners.
- Enforce network egress policy for shell, MCP, and web tools.

## Deployment Assumptions

Known:

- Aura runs in a container.

UNKNOWN:

- Whether the intended production deployment is single-user local desktop, remote web service, or multi-tenant SaaS.
- Whether the container is disposable and hardened.
- Whether the container runs privileged, as root, with host networking, with broad host mounts, or with the Docker socket mounted.
- Whether secrets are injected into the container environment or filesystem.
- Whether all users are trusted operators.

This matters because several current decisions are acceptable only for a trusted local single-operator product.
