---
focus: concerns
generated: 2026-05-10
last_mapped_commit: 6b6fa8245e19b9f49fb48e39a28a424cdfcda03f
---

# Concerns — Aura Codebase

## Architectural Observations

### Monolithic `internal/` Package
The entire application lives in `internal/` spread across 41 packages. There are no clearly separated bounded contexts or domains — packages import each other liberally. This is not necessarily a problem for a solo-developer project, but it makes reasoning about dependency boundaries harder as the codebase grows.

### Single SQLite Database
All state (conversations, wiki, settings, scheduler jobs, sources, memory) lives in one SQLite database accessed through `internal/db/db.go`. This is pragmatically correct for a local-first agent, but:

- **Contention risk**: The scheduler, API server, agent runner, and Telegram bot all access the same DB. SQLite handles concurrent readers well, but writes serialize. Under heavy agent workloads with concurrent tool calls, write contention could become visible.
- **No read replicas**: If the dashboard becomes read-heavy, there's no read-path scaling option.
- **Playwright serialized**: `workers: 1` in Playwright config is explicitly because "dashboard reads shared SQLite." E2E tests can't run in parallel.

### Docker Compose Footprint
The production `compose.yaml` runs 11 services. Several are heavy (Ollama with GPU, Qdrant vector DB). For a solo developer's agent, this is a substantial operational footprint. The `compose.image.yaml` variant exists, suggesting awareness of this.

### Debug Commands Proliferation
21 `cmd/debug_*` CLIs exist, each testing a specific subsystem. These are effectively ad-hoc integration tests that aren't run in CI. If the subsystem changes, the corresponding debug CLI may silently bit-rot.

## Testing & Quality

### No Coverage Thresholds
1,307 test functions across 156 test files but no `-cover` enforcement in CI or Makefile. Coverage is unknown. Critical paths (agent loop, tool execution, LLM retry logic, Telegram streaming) may have variable coverage.

### Limited Parallel Testing
- Go tests don't use `t.Parallel()` widely
- Playwright E2E runs with `workers: 1`, `fullyParallel: false`
- Test suite runtime grows linearly with test count

### Hand-Rolled Fakes
No mocking framework means fakes must be manually kept in sync with interfaces. Adding a method to an interface requires updating all fake implementations. Manageable now but becomes friction as interfaces grow.

### Playwright Dependency on Live Instance
E2E tests require a running Aura instance with a valid bearer token (`AURA_E2E_TOKEN`). There's no `compose.yaml`-based E2E setup that launches a self-contained test environment. The token must be manually minted via Telegram's `request_dashboard_token` tool.

## Operational Concerns

### Workspace Isolation
The sandbox system has three modes (`process`, `pyodide`, `pyodide_container`) with platform-specific skill sandboxing (`internal/skill/sandbox_linux.go`, `sandbox_other.go`). On non-Linux platforms, sandbox options are reduced. Security characteristics vary by deployment platform.

### LLM Provider Coupling
`internal/llm/` supports OpenAI-compatible and Ollama backends. Tool definitions and system prompts may assume capabilities (function calling, streaming, structured output) that vary across providers. Model-specific behaviour differences could surface as agent bugs rather than LLM configuration issues.

### Data Directory Growth
`data/` directory accumulates SQLite WAL files, logs, npm caches, pip caches, and matplotlib caches. There's no automated pruning mechanism except what the scheduler's maintenance job handles. Long-running instances will grow `data/` unbounded.

### Single Binary + Embedded Dashboard
The React dashboard is built and embedded into the Go binary via `embed` (`internal/api/dist/`). Elegant for distribution but means dashboard updates require a full binary rebuild and restart. Hot-reload during dashboard development requires separate `npm run dev` proxying.

## Security Considerations

### Bearer Token Auth
Authentication uses static bearer tokens stored in `internal/auth/store.go`. No token rotation, no OAuth, no session expiry. The Telegram allowlist (`internal/telegram/access.go`) controls who can interact via bot, but the dashboard/web API relies solely on bearer tokens.

### Sandbox Escape Surface
Code execution sandbox is a critical security boundary with platform-specific implementations (Linux vs other). Process-based sandboxing (`SANDBOX_RUNTIME_MODE: "process"` in compose) relies on OS-level isolation. On non-Linux platforms, sandboxing is reduced.

### MCP Server Access
MCP servers run as subprocesses with stdio/SSE transport. `internal/mcppolicy/policy.go` enforces access policies, but MCP servers can execute arbitrary code. Policy enforcement is a critical security checkpoint.

### Secrets in Files
`data/secrets/` contains `garage.toml` (S3 credentials) and `aura.env`. File-based secrets are less secure than Docker secrets or a vault, but appropriate for a solo-developer local deployment.

## Performance Notes

### Vector Search Dual Backend
The system supports both Qdrant (remote) and chromem-go (embedded) for vector search. Switching between them requires reindexing. `cmd/debug_qdrant/main_test.go:110` explicitly tests for "stale Qdrant index" detection, indicating this is a known operational concern.

### Embedding Cache
`internal/search/embed_cache.go` — SQLite-backed embedding cache to avoid re-embedding the same text. Cache invalidation on model change is not automated.

### Conversation Context Growth
Long conversations grow context linearly. The summarizer (`internal/conversation/summarizer/`) mitigates this, but summarization quality depends on the LLM. If summarization misses key details, the agent effectively "forgets" context that may still be relevant.

## No Detected TODOs or FIXMEs
A grep across all Go and TypeScript source files found zero `TODO`, `FIXME`, `HACK`, `XXX`, or `BUG` comments. This suggests either exceptional discipline or that concerns aren't being annotated in code. The debug CLIs serve as living documentation of past problems, but there are no code-level markers for future work or known fragility.
