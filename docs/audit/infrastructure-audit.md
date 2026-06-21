# Infrastructure Audit

## Summary

Aura has a credible local development infrastructure: Compose services, Postgres migrations, Neo4j, Garage/S3-compatible storage, a Makefile, scripts, health endpoints, and Web UI auth gates. The production gap is that the same defaults and runtime assumptions are too permissive for industrial operation.

## Configuration Management

Confirmed:

- Configuration is loaded through `internal/config/config.go`, including `.env` support through `godotenv.Load`.
- Some validation exists for database URL and Neo4j password.
- Integer and boolean env helpers in `internal/config/config_env.go` silently fall back to defaults on malformed values.

Risks:

- Malformed env values can be ignored silently.
- Local-trusted and production-like profiles are not separated.
- `.env.example` contains a shell guardrail override that disables default destructive shell patterns when copied.
- Static object-store/Garage credentials are accepted by default.

Recommendations:

1. Add explicit runtime profiles: `dev`, `local_trusted`, `single_user_hardened`, `server_production`.
2. Add a `aura doctor --production` or `aura config validate --profile server_production` command.
3. Treat malformed env values as production errors.
4. Fail production validation for default secrets, permissive CORS, unfenced shell/filesystem tools, disabled destructive shell patterns, missing auth secret, and non-readiness healthchecks.

## Secrets

Confirmed:

- `internal/config/config.go` defines static object-store defaults.
- `compose.yaml` and `scripts/garage_bootstrap.sh` use fixed development credentials.
- MCP process environment filtering avoids passing obvious secret env variables into managed subprocesses (`internal/mcp/client.go`).

Risks:

- Development credentials can escape into production.
- There is no verified secret-rotation flow for object-store credentials or Garage RPC secrets.

Recommendations:

1. Generate local development secrets on first bootstrap.
2. Require secret values from a secret manager or explicitly supplied environment in production.
3. Add startup logs that identify secret source class without printing secret values.
4. Add tests that reject known default credentials in production profile.

## Logging, Metrics, And Tracing

Confirmed:

- AG-UI registers `/metrics`, `/debug/vars`, `/healthz`, and `/readyz` routes in `internal/agui/server.go`.
- Reasoning trace has redaction behavior and optional full mode in `internal/reasoningtrace/reasoningtrace.go`.
- Tool invocation store redacts and caps argument/result previews in `internal/toolinvocations/store.go`.

Gaps:

- No production dashboard/alert bundle was found.
- Mutating tool ledger writes are best-effort.
- Full reasoning trace mode needs retention, encryption, and production warnings.

Recommendations:

1. Emit structured log events with request ID, conversation ID, run ID, tool ID, invocation ID, actor, runtime profile, policy decision, and error class.
2. Add OpenTelemetry spans for LLM calls, tool execution, MCP roundtrips, pause/resume, persistence, and scheduler work.
3. Add metrics and alerts for:
   - loop step exhaustion
   - wallclock budget exhaustion
   - tool timeout and retry rate
   - mutating tool ledger failure
   - shell/MCP denial and approval rate
   - pause/resume failures
   - background job age/count
   - HTTP listener and readiness state
   - DB migration and connection health

## Health Checks

Confirmed:

- HTTP health/readiness routes exist.
- Compose healthcheck runs `aura version`.

Risks:

- Container health can be green while the served API is down.
- Listener failures can be logged while the broader daemon continues.

Recommendations:

1. Make listener failure fatal for the serving process, or make readiness false and let orchestration restart it.
2. Change Compose healthcheck to call `/readyz`.
3. Ensure readiness includes database connectivity, migration state, scheduler readiness where relevant, and listener state.

## Deployment And Runtime Isolation

Confirmed:

- Shell and filesystem tools run with host authority.
- MCP stdio servers are spawned as local processes with environment filtering and command trust checks in managed paths.
- Background shell jobs have max-running caps and shutdown handling.

Gaps:

- No mandatory sandbox for shell/filesystem/MCP tools.
- No production egress policy.
- No per-tool CPU/memory/process limits visible in the audited tool layer.
- Background jobs have no mandatory TTL.

Recommendations:

1. Add sandbox backends: direct host for dev, workspace chroot/container for hardened mode, and no-host-shell by default in server production.
2. Add network egress allowlists for shell and MCP processes.
3. Add per-tool resource limits and TTL.
4. Add policy-managed background jobs with owner/session scoping.

Subagent update:

- MCP mount/open should have a per-server timeout.
- Stdio MCP frames need a maximum size before parse.
- MCP close should be bounded and terminate process trees.
- Docker MCP network allowlists are advisory unless enforced by a proxy/firewall.
- Background shell jobs need unguessable IDs, owner/session binding, TTL, and explicit delete/evict behavior.

## Persistence, Queues, And Recovery

Confirmed:

- Conversation content can spill to sidecars.
- Ask-user pause/resume is persisted.
- Tool invocation records can be persisted with preview capping.
- Scheduler and Compose infrastructure exist.

Gaps:

- Sidecar reads trust stored paths.
- Batch resume is not atomic-first.
- Mutating side effects are not transactionally tied to ledger persistence.
- Retention and cleanup are not first-class operational flows.

Recommendations:

1. Use append-only event logs for conversation state.
2. Reconstruct sidecar paths from IDs rather than trusting DB path text.
3. Use idempotency keys and durable invocation state for mutating tools.
4. Add retention policies for sidecars, reasoning traces, tool outputs, and object-store artifacts.
5. Add backup/restore tests with recovery time and recovery point objectives.

Subagent update:

- `AURA_RUN_DIR` should be normalized to an absolute path or rejected when relative.
- Conversation delete/clear flows should go through runner lifecycle eviction before persistence deletion.
- Unreferenced sidecars inside live conversation directories need reconciliation after crash windows.
- Scheduler shutdown should separate stop-admission from in-flight job drain.
- systemd stop budgets should exceed the longest configured backup handler duration, or backups should be shortened and atomically promoted.

## Dependency Management And CI

Confirmed:

- `scripts/go_packages.sh` exists to filter `web/node_modules`.
- `Makefile` uses the filtered package list.
- CI workflow still uses raw `./...` in several Go steps.
- The repository also has substantial frontend and integration quality gates, including Vitest, Playwright, Stryker, CodeQL, coverage gates, and smoke/integration scripts.

Recommendations:

1. Use the filtered package list in all CI Go commands.
2. Add CI lint that rejects raw `./...` where it can traverse local dependency artifacts.
3. Add `govulncheck` and dependency license/SBOM outputs against the same package list.
4. Add release artifacts with checksums and provenance.
5. Pin third-party workflow actions and tool installs to immutable SHAs or exact versions.
