# Testing Strategy

## Current Coverage Assessment

The codebase has meaningful tests around budgets, context compaction, runner persistence, skill governance, MCP behavior, provenance wrapping, shell approval helpers, and live runner scenarios. Scoped verification for this audit passed:

```text
go test ./internal/agent/...
```

Important integration tiers exist but are build-tagged or environment-gated, including live LLM/Postgres tests in `internal/runner/live_e2e_test.go` and DB integration tests in `internal/conversations` and `internal/skills`.

## Gaps

- No default CI evidence for production policy profiles.
- No continuous crash-injection tests around mutating tool execution.
- Limited chaos testing for DB, MCP, LLM provider, and filesystem sidecar failures.
- No load tests for concurrent turns, background shell caps, MCP saturation, or sidecar growth.
- Memory behavior is tested with scripted fake-client tool calls, not enforced runtime behavior.
- No full security regression suite for prompt injection plus dangerous tool access.

## Proposed Test Pyramid

## Unit Tests

Add or strengthen:

- `ExecutionPolicy` decisions: allow, prompt, deny.
- Workspace path resolution and absolute-path denial.
- Atomic `FSWrite` behavior.
- Terminal exclusivity in `llm_agent_dispatch`.
- Background shell owner/TTL enforcement.
- MCP local mutability overrides.
- Reasoning trace production guard.

## Integration Tests

Add:

- Runner crash recovery across tool start, side effect, and result persistence.
- Sidecar spill, retention, and encrypted-at-rest path.
- Skill create/update gate matrix by capability profile.
- Tool ledger outbox replay after DB outage.
- Scheduler task approval and identity propagation.

## End-To-End Tests

Add:

- Remote/web session with read-only profile cannot use shell/FS/skill-write tools.
- Local trusted profile can request break-glass approval and execute a command.
- Prompt-injected web page cannot cause host file read or shell execution.
- Mixed `text_response` plus mutating tool is rejected and self-corrected.

## Load And Soak Tests

Add:

- N concurrent conversations with bounded tool batches.
- Background shell lifecycle under cap and TTL.
- MCP server reconnect/backoff under repeated failures.
- Sidecar directory growth and sweeper behavior.
- LLM provider rate-limit/backoff behavior.

## Chaos Tests

Add:

- Kill process after tool start before tool result.
- Drop DB connection during ledger insert.
- Fill sidecar filesystem.
- Fail entropy source for nonce generation.
- Kill MCP server mid-call and during reconnect.
- Simulate LLM stream EOF mid-response.

## Security Tests

Add:

- Prompt injection in web/file/MCP output attempting to call dangerous tools.
- Path traversal and absolute path policy tests.
- Shell policy bypass cases: script wrapper, interpreter command, encoded command, env indirection.
- Secret redaction tests for ledger, trace, shell output, and sidecars.
- Skill self-extension abuse tests.

## Suggested CI Checks

- `go test ./...`
- `go test -race ./internal/agent/... ./internal/runner/... ./internal/conversations/...`
- Build-tagged DB integration lane with mandatory DSN in CI.
- Build-tagged live smoke lane with paid-provider guard.
- Fuzz/property lane for JSON schema, canonical args, path validation, and context compaction.
- Static analysis: `go vet`, `govulncheck`, `gosec` or equivalent, CodeQL if available.
- Migration check and rollback check for database changes.

