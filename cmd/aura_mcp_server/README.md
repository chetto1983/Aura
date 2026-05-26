# aura_mcp_server - Claude Code to Aura bridge

Stdio JSON-RPC 2.0 MCP server that exposes a running Aura instance to any MCP
host such as Claude Code or Cursor.

## What You Get

55 tools wired to Aura's HTTP API. `tools/list` is the source of truth, grouped
as:

| Group | Tools |
|---|---|
| Chat | `aura_chat`, `aura_chat_answer` |
| Wiki | `aura_wiki_search`, `aura_wiki_read`, `aura_wiki_pages`, `aura_wiki_graph`, `aura_wiki_godnodes`, `aura_wiki_path`, `aura_wiki_index_rebuild`, `aura_wiki_reindex`, `aura_wiki_log_append` |
| Sources | `aura_source_list`, `aura_source_get`, `aura_source_read`, `aura_source_ocr`, `aura_source_raw`, `aura_source_derived`, `aura_source_upload`, `aura_source_ingest`, `aura_source_reocr`, `aura_source_delete` |
| Files | `aura_file_tree`, `aura_file_read`, `aura_file_write`, `aura_file_delete`, `aura_file_delete_many`, `aura_file_mkdir`, `aura_file_rename` |
| Tasks | `aura_tasks_list`, `aura_task_get`, `aura_task_upsert`, `aura_task_cancel`, `aura_task_delete` |
| Skills | `aura_skills_list`, `aura_skill_get`, `aura_skills_catalog`, `aura_skill_install`, `aura_skill_delete` |
| MCP | `aura_mcp_servers`, `aura_mcp_status`, `aura_mcp_providers`, setup helpers, provider probe/mail helpers, and `aura_mcp_invoke` |
| Direct tool registry | `aura_tool_registry`, `aura_tool_call` |
| Maintenance | `aura_health`, `aura_tool_compact`, `aura_compact_reindex`, `aura_tool_attempts` |

## Build

```powershell
go build -o aura-mcp-server.exe ./cmd/aura_mcp_server
```

Produces a self-contained binary with no runtime dependencies.

## Mint A Bearer Token

The MCP server authenticates against Aura's HTTP API using a bearer token.
Mint one via Telegram: send `/login` to your Aura bot. It replies with a token
in a private DM.

For development, you can also insert a SHA-256 hash directly into `api_tokens`
via SQLite; see memory `reference_aura_token_bootstrap_db_insert`.

## Register In Claude Code

Edit `~/.claude/mcp.json`:

```json
{
  "mcpServers": {
    "aura": {
      "command": "D:/Aura/aura-mcp-server.exe",
      "args": ["--api", "http://localhost:18080"],
      "env": {
        "AURA_TOKEN": "<paste the bearer token here>"
      }
    }
  }
}
```

Restart Claude Code. From the next session, deferred tools `mcp__aura__*`
appear in the system reminder. Call `ToolSearch(query="select:mcp__aura__wiki_search")`
to load a schema, then invoke.

## Smoke Test Offline

```powershell
$tokens = @'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
'@
$env:AURA_TOKEN = "fake"
$tokens | .\aura-mcp-server.exe
```

Expect `initialize` plus `tools/list` with 55 entries. No Aura instance is
needed for this protocol check.

## Smoke Test With Aura

```powershell
$env:AURA_TOKEN = "<real token>"
$req = @'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aura_health","arguments":{}}}
'@
$req | .\aura-mcp-server.exe --api http://localhost:18080
```

Expect Aura's `/api/health` JSON in the second response's `content[0].text`.

## Wire Format

- Transport: stdio, newline-delimited JSON-RPC 2.0
- Protocol version: `2024-11-05`
- Buffer: up to 16 MB per frame
- HTTP timeout: 30 s default, override with `--timeout`

## Limits

- This MCP server is an operator/test bridge over Aura's bearer-authenticated
  HTTP API. It exposes both reads and mutations that the API already owns.
- Destructive/admin tools are named explicitly (`delete`, `reindex`, `install`,
  `setup_*`) and still rely on Aura's server-side auth, admin flags, validation,
  and capability checks.
- Native LLM tool actions can be tested through `aura_tool_call`, which calls
  Aura's live registry-level `/api/tools/call` endpoint.

## Adding More Tools

The base tools live in `builtinTools()` in `main.go`; expanded API-backed tools
live in `api_backed_tools.go` with handlers in `api_backed_handlers.go`.
Handlers call `auraClient.do(method, path, query, body)` for JSON endpoints or
`doRaw` for multipart/raw-byte endpoints.
