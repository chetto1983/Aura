# aura_mcp_server — Claude Code ↔ Aura bridge

Stdio JSON-RPC 2.0 MCP server that exposes a curated subset of a running Aura
instance to any MCP host (Claude Code, Cursor, etc.).

## What you get

10 tools wired to Aura's HTTP API:

| Tool | What it does |
|---|---|
| `aura_wiki_search` | Hybrid FTS+vector search across the wiki |
| `aura_wiki_read` | Read a wiki page by slug (full markdown) |
| `aura_wiki_pages` | List wiki pages with optional category filter |
| `aura_source_list` | List sources (PDFs, audio memos, etc.) |
| `aura_source_read` | Read a source's OCR/markdown content |
| `aura_chat` | Send a chat message and get the reply |
| `aura_tasks_list` | List scheduled tasks |
| `aura_skills_list` | List installed skills |
| `aura_mcp_servers` | List Aura's own configured MCP servers |
| `aura_health` | Health + version + module status |

## Build

```powershell
go build -o aura-mcp-server.exe ./cmd/aura_mcp_server
```

Produces a ~9 MB self-contained binary (no runtime deps).

## Mint a bearer token

The MCP server authenticates against Aura's HTTP API using a bearer token.
Mint one via Telegram: send `/login` to your Aura bot — it replies with a token
in a private DM. Copy the plaintext token.

(Alternative: insert a SHA-256 hash directly into `api_tokens` via SQLite for
dev — see memory `reference_aura_token_bootstrap_db_insert`.)

## Register in Claude Code

Edit `~/.claude/mcp.json` (create if missing):

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
to load the schema, then invoke.

## Smoke test (offline — just protocol)

```powershell
$tokens = @'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
'@
$env:AURA_TOKEN = "fake"
$tokens | .\aura-mcp-server.exe
```

Expect `initialize` reply + `tools/list` with 10 entries. No Aura instance
needed for this protocol check.

## Smoke test (with running Aura)

```powershell
$env:AURA_TOKEN = "<real token>"
$req = @'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aura_health","arguments":{}}}
'@
$req | .\aura-mcp-server.exe --api http://localhost:18080
```

Expect Aura's `/api/health` JSON in the second response's `content[0].text`.

## Wire format

- Transport: stdio, newline-delimited JSON-RPC 2.0
- Protocol version: `2024-11-05` (current MCP stable)
- Buffer: up to 16 MB per frame (large tool results from `aura_source_read` fit)
- HTTP timeout: 30 s default, override with `--timeout`

## Limits

- Read-only by design except `aura_chat` (which writes to conversations + may
  trigger tools server-side via Aura's normal agent loop)
- No mutation tools: source upload, wiki write, task scheduling, settings
  changes — use Aura's dashboard or Telegram for those (intentional safety
  surface; the MCP host is a co-pilot, not an admin)

## Adding more tools

Each tool is one entry in `builtinTools()` in `main.go` — schema + handler
function. Handlers call `auraClient.do(method, path, query, body)` and return
formatted text. ~30 LOC per tool. Add new ones as Aura's API grows.
