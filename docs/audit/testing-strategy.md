# Testing Strategy

## Current Coverage Assessment

Aura has meaningful test foundations:

- Loop budget and retry behavior have dedicated code paths and tests in the agent packages.
- Parallel tool execution has bounded worker and panic-recovery logic.
- Shell/filesystem guardrail tests exist, including the targeted skills-dir fence.
- MCP timeout, schema capping, and trust-related paths have coverage.
- Resume single-answer atomicity has a regression test.
- Makefile test targets use a filtered Go package list.
- Frontend package scripts include lint/typecheck/format, Vitest coverage, Playwright E2E, and Stryker mutation.
- CI includes CodeQL, integration lanes, web E2E, coverage gates, and multiple smoke/integration tiers.

Observed gaps:

- Batch pause/resume atomicity is not tested at the same level as single-answer resume.
- Terminal `text_response` with mutating siblings needs regression coverage.
- Shell/filesystem production policy needs tests once profiles are added.
- Sidecar read path validation needs negative tests.
- CI package discovery is inconsistent with local `scripts/go_packages.sh`.
- Some smoke/chaos scripts exist, but agent safety/load/chaos/profile validation is not yet one mandatory release gate.
- Some CI/release supply-chain references should be pinned more strictly.

## Proposed Test Pyramid

### Unit Tests

Focus:

- Tool policy decisions.
- Path normalization and fencing.
- Shell destructive-pattern parsing.
- Hook fail policy.
- Sidecar path reconstruction and validation.
- Config profile validation.
- Terminal/sibling dispatch splitting.
- Atomic filesystem writes.

Required new unit tests:

1. `.env.example` copied behavior preserves destructive shell defaults.
2. `text_response` plus mutating sibling is rejected.
3. Batch resume cannot inject answers unless all pauses are claimed.
4. Sidecar read rejects paths outside computed sidecar root.
5. `fs_write` uses atomic write semantics.
6. Command hook omitted policy resolves to fail-closed in production.
7. Remote MCP empty trust is blocked.
8. Production config rejects default object-store secrets.
9. Mixed URL+command MCP config is rejected and does not launch stdio.
10. AG-UI conversation and approval APIs enforce identity scoping.
11. Empty MCP trust body is rejected.
12. Mutating panic preserves mutating classification.
13. Background shell poll/kill requires matching session/actor.

### Integration Tests

Focus:

- End-to-end agent loop with tool calls.
- Pause/resume through runner and HTTP surfaces.
- MCP server lifecycle and timeout behavior.
- AG-UI auth/capability gates.
- Database and sidecar persistence recovery.

Required new integration tests:

1. Outside-workspace `send_file` approval either succeeds through a real hook or returns a clear unsupported error.
2. Listener port conflict causes process failure or readiness false.
3. Mutating tool ledger failure blocks production-mode execution.
4. Background shell job TTL expires and records status.
5. Conversation reload after crash preserves durable ordering and rejects corrupted sidecar paths.
6. Conversation delete evicts all session-scoped in-memory tool state.
7. Scheduler SIGTERM drains in-flight jobs until deadline.
8. MCP mount timeout, frame overflow, and close timeout are deterministic.

### Contract Tests

Focus:

- Tool schemas and result normalization.
- MCP trust classes and config import behavior.
- API auth/capability behavior.
- Audit ledger shape.

Required new contract tests:

1. Tool descriptor contract: mutating/deferred/read-only flags are present and correct.
2. MCP remote trust contract: no remote server is runnable without explicit trust.
3. Audit ledger contract: mutating invocation has durable started/succeeded/failed states.
4. Web API contract: production profile denies unauthenticated mutation routes.
5. Web API identity contract: authenticated principals cannot access other principals' conversations or approvals.
6. MCP governance contract: trust writes require explicit class, reason, transport compatibility, and audit row.

### Golden And Regression Tests

Focus:

- Agent-loop determinism and safety.
- Prompt-injection handling.
- Conversation replay.

Required scenarios:

1. Prompt-injected file read request is denied in production profile.
2. Prompt-injected shell destructive command requires approval or is denied.
3. Completion-gate rejection does not allow sibling side effects.
4. LLM stream timeout returns normalized error and preserves recoverable state.
5. Tool output truncation can be expanded through `read_tool_output` without path escape.

### Load And Chaos Tests

Focus:

- Concurrent conversations.
- MCP timeout storm.
- Background shell job pressure.
- Database outage during pause/resume.
- Object-store outage during artifact persistence.

Required scenarios:

1. 100 concurrent short agent runs with tool calls.
2. Burst of MCP calls where 30 percent time out.
3. DB restart during pause/resume.
4. Process kill during large filesystem write.
5. Scheduler and Web UI running while AG-UI listener failure is injected.

## Suggested CI Checks

1. `gofmt`, `go vet`, `staticcheck` if available.
2. Go tests using `scripts/go_packages.sh`, including race tests for core packages.
3. Frontend lint/build/tests with deterministic dependency install.
4. `govulncheck` on filtered packages.
5. Config validation test for `server_production`.
6. Security regression suite for shell/filesystem/MCP policy.
7. Generated docs/audit JSON validation where audit files are maintained.
8. SBOM and license scan for release artifacts.

## Reference-Inspired Evaluation Taxonomy

`D:\tmp\agent-infra-sandbox\evaluation\README.md` organizes evaluations by capability class. Aura should adopt similar categories:

- `basic`: simple file/tool calls
- `shell`: foreground/background shell, cancellation, destructive approval
- `filesystem`: read/write/edit, path fences, atomicity
- `mcp`: stdio and remote HTTP trust, schema caps, timeout
- `memory`: provenance, stale state, recovery
- `pause_resume`: single and batch human-in-the-loop flows
- `error`: failed tools, retries, timeout, crash recovery
- `workflow`: multi-tool tasks and adversarial prompt injection
- `production`: profile validation, ledger durability, readiness, observability
