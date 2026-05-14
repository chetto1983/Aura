# internal/tools

The `tools` package is the **LLM-facing API surface** of Aura. Every tool the
agent loop can call — built-in or dynamic MCP-bridged — lives here. The
package owns:

- The `Tool` interface, the `Registry` that dispatches calls by name, and the
  optional vector index that ranks tools by relevance to the live turn.
- The 25+ built-in tools (memory, web, sources, scheduler, files, wiki,
  workspace, sandbox, auth).
- Adapters that surface MCP-server tools as ordinary `Tool` instances
  (`MCPTool`, name-mangled as `mcp_<server>_<tool>`).
- LLM-contract helpers: structured `ToolDefinition` with curated examples,
  per-category gating, error-class logging for CLAUDE.md value-leak compliance.

## Boundaries

| In | Out |
|---|---|
| `agentloop.Loop` → `Registry.Execute(ctx, name, args)` | Tool result string + error |
| `internal/mcp.Client` (advertised tools) | `MCPTool` wrappers |
| `internal/source.*`, `internal/wiki.*`, `internal/scheduler.*`, … | Operate on persistent state |
| `internal/sandbox.Manager` | `execute_code` / `execute_shell` |

The package depends on most of the rest of Aura's domain packages but is
**not** imported by them — the registry is constructed at startup in
`cmd/aura` and handed to the agent loop.

## Public API

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any        // JSON Schema fragment
    Execute(ctx context.Context, args map[string]any) (string, error)
}

reg := tools.NewRegistry(logger)
reg.Register(tools.NewSearchMemoryTool(...))
result, err := reg.Execute(ctx, "search_memory", args)
defs := reg.Definitions()              // for the LLM tool catalog
```

Tools may also implement `ToolDefinitionProvider` to ship curated examples,
and `CategorizedTool` / `MultiCategorizedTool` to opt into category gating.
`WithCategory(tool, "category")` wraps any tool without losing its
`Definition()` (forwarding is explicit — see [registry.go](registry.go)).

## Files

| File | Purpose |
|---|---|
| `registry.go` | `Tool` interface, `Registry`, category constants, redact-aware `argKeys`, default 5-min tool-exec timeout. |
| `definition.go` | `ToolDefinition`, `ToolDefinitionProvider`, `definitionForTool`. |
| `examples.go` | Tool-call example fixtures merged into descriptions. |
| `error.go` | `FormatToolError` + `classifyToolError` for low-cardinality logging. |
| `args.go` | `stringArg`, `stringSliceArg`, `cleanStrings` — boundary-trimming. |
| `context.go` | Per-call context keys: user ID, active-turn allowlist. |
| `web_common.go` | `requiredString`, `intArg`, `truncateForToolContext` (UTF-8 safe), `formatFetchResult`, `formatSearchResults`. |
| `searxng.go` | `web_search` against a SearXNG instance. |
| `direct_fetch.go` | `web_fetch` with SSRF-blocking dialer, env-driven loopback override, http.DetectContentType sniff. |
| `memory_search.go` | `search_memory` — hybrid wiki + compact-memory query, recency-weighted. |
| `tool_search.go` | Internal tool-discovery (used by the agent loop's tool ranking). |
| `source.go` | Shared formatters and `readBoundedFile` for source tools. |
| `source_store.go` | `store_source` (kind=text/url, validates absolute URLs). |
| `source_ocr.go` | `ocr_source` — Mistral OCR with 64 MiB PDF cap and write rollback on metadata failure. |
| `source_read.go` | `read_source` — modes: metadata, ocr, excerpt. |
| `source_list.go` | `list_sources` + `lint_sources`. |
| `source_delete.go` | `delete_source` (memoryindex first, then files). |
| `scheduler.go` | `schedule_task` / `list_tasks` / `cancel_task` / `run_task_now`. 5-min minimum interval. |
| `files.go` | `DocumentSender`, caption sanitizer, `stringifyCell`. |
| `files_xlsx.go` | `create_xlsx`. |
| `files_docx.go` | `create_docx`. |
| `files_pdf.go` | `create_pdf`. |
| `files_blocks.go` | Shared `blockShape` + `parseBlockShapes` used by docx/pdf. |
| `wiki.go` | `write_wiki_page` — server-managed schema/prompt versions. |
| `workspace_files.go` | `read_file`, `write_file`, `apply_patch`, `list_files`, `search_files`. |
| `workspace_validation.go` | Server-managed file denylist + wiki/skill validation. |
| `exec.go` | `execute_code` / `execute_shell` + internal manifest orchestration (parallel, per-call timeout, escalation blocklist). |
| `auth.go` | `request_dashboard_token` — privileged; blocked from internal manifests. |
| `ingest.go` | `ingest_source` LLM-facing wrapper. |
| `mcp.go` | `MCPTool` adapter exposing one MCP-server tool as a `Tool`. |
| `tool_registry.go` | Filesystem-backed user-authored tools (`*.py` under tools dir). |
| `tool_mgmt.go` | LLM tools that manage other LLM tools (read/save). |
| `tool_definitions.go` | Built-in `ToolDefinition` fixtures. |
| `registry_search.go` | FTS-mode tool ranking. |
| `registry_search_vector.go` | Qdrant-backed tool ranking (lock-free Search snapshot). |

## Conventions

### LLM contract

- **Names** are stable identifiers (`web_fetch`, `search_memory`, …). They
  appear in tool calls, telemetry, and the agent loop's allowlist. Renaming
  one is a breaking change for any prompt that references it by name.
- **Descriptions** are read every turn by the LLM. Keep them imperative,
  spell out failure modes, and never lie about safety (see F-002: the
  `allow_network` parameter was removed because it advertised an isolation
  that didn't exist).
- **Parameters** are JSON Schema fragments. `enum`, `minimum`, `maximum`,
  `required` are honored by some providers — set them so a strict
  provider can reject malformed calls early.
- **Examples** in `ToolDefinition.Examples` are merged into the prompt and
  cost tokens; keep them concrete and short.

### Security policy (CLAUDE.md alignment)

- **Tool argument values must never be logged.** Only names + argument keys.
  `argKeys` further redacts credential-shaped key names (password, secret,
  token, api_key, auth, bearer, session_id, cookie) to `<redacted>`.
- **Tool errors must not leak values.** The registry logs an `error_class`
  enum (timeout, not_found, validation, …) instead of the raw message.
  Individual tools should keep value-bearing detail out of the wrapped
  error chain when feasible.
- **Web fetches block private/loopback/link-local IPs by default.**
  Override with `AURA_WEB_FETCH_ALLOW_LOOPBACK=1` (dev/tests) or
  `AURA_WEB_FETCH_ALLOW_HOSTS=host1,host2` (operator allowlist).
- **Path traversal is the workspace layer's responsibility.**
  `workspace.Root` enforces the sandbox; tool wrappers add a denylist for
  server-managed files (`wiki/index.md`, `wiki/log.md`, `wiki/SCHEMA.md`).
- **The "sandbox" runtime is not network-isolated.** The process runtime
  shares Aura's container namespace. Tool descriptions are honest about it.
- **`execute_code` internal manifest** has an explicit blocklist
  (execute_code, execute_shell, request_dashboard_token, delete_source,
  forget_memory) plus the active-turn allowlist; both must permit a call.

### Concurrency

- Tools are called in parallel from the agent loop within a single turn.
  Stateless tools need no special handling. Stateful ones must guard
  shared state with a mutex (and never hold one across HTTP — see
  `ToolVectorIndex.Search` for the snapshot pattern).
- `Registry.Execute` imposes a default 5-minute deadline when the caller
  did not attach one. Tools that have stricter needs (web_fetch 30s,
  execute_code 1-300s) set their own first; this is defense-in-depth for
  the worst case where a tool ignores ctx entirely.

### File-size cap

CLAUDE.md caps per-file LOC at 600. Two files were over (files.go, source.go)
and have been split (F-046, F-047). Three trend toward the cap and are
candidates for a future proactive split (F-048):

| File | LOC | Note |
|---|---|---|
| `memory_search.go` | 569 | Score helpers + render helpers split candidate. |
| `scheduler.go` | 540 | Schedule write tool vs. read tools split candidate. |
| `exec.go` | 480 | Code vs. shell vs. internal-manifest split candidate. |

## Testing

All tests use Go's standard `*_test.go` convention and live next to the file
they test. The package has ~5500 LOC of tests covering registry behaviour,
each individual tool, the workspace guard, the parallel manifest, and the
SSRF dialer.

```powershell
go test ./internal/tools/...
go test -race ./internal/tools/...
go test -run TestWorkspaceFileToolsBlockServerManagedWikiFiles ./internal/tools/
```

## Audit history

- 2026-05-11 — full 6-pillar audit: 3 CRITICAL, 14 HIGH, 21 MEDIUM, 10 LOW.
  Findings + fix commits cross-referenced in [`.planning/audits/tools-2026-05-11/REVIEW.md`](../../.planning/audits/tools-2026-05-11/REVIEW.md).
  Deferred: F-043 (per-tool structured logging — requires constructor
  refactor across every tool) and F-048 (proactive splits of three
  files trending toward the LOC cap).
