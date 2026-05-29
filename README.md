# Aura

Self-hosted agent runtime. Tabula-rasa rewrite (2026-05-27).

## Scope

Four concentric components, nothing else:

1. **Agent loop** — streaming LLM, deferred-tool dispatch, bounded iterations.
2. **KV cache** — provider-aware prompt caching (DeepSeek auto, Anthropic ephemeral). Stable-prefix discipline; zero `messages[0]` mutation.
3. **Sandbox** — Python + shell execution in container-isolated workers (seccomp, ulimit, net-deny by default).
4. **Swarm** — parallel agents in a controlled loop with peer-to-peer talk (tier model, MAX_SPAWN_DEPTH=3, shared bus + DM-by-ID).

Persistence: Neo4j via `mcp-neo4j-cypher` (MCP stdio). No native Go adapter — the model talks to Neo4j through MCP exclusively.

## What's deliberately not here

- Telegram bot (optional plugin binary, separate repo concern)
- Web dashboard
- Wiki .md filesystem with git tracking
- FTS5 / Qdrant / in-memory graph index
- OCR / markitdown / whisper ingestion (will return as Neo4j-MCP-mediated tools when needed)
- Setup wizard, tray icon, dashboard auth

History of the prior implementation is preserved at git tag `pre-rewrite-2026-05-27` (pushed to origin).

## Status

Bootstrap. See `cmd/aura/main.go` and `internal/agent/` (Agent interface + workflow agents + Budget tree).
