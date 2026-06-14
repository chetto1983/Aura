# WhatsApp MCP Vendor Provenance

- Source: `D:\tmp\whatsapp-mcp`
- Upstream fork commit: `7d6a06dcdce1f01dfb24f60e1030d5efba9f3b88`
- Applied patch: `bridge-patch.diff` from `.planning/spikes/002-whatsapp-mcp-pairing`
- Vendored source archive: `whatsapp-mcp-src.tar.gz`

The Docker image builds the CGO whatsmeow bridge from vendored Go source and runs
the vendored Python FastMCP front as a Streamable HTTP MCP server. The unpacked
third-party source is kept inside the Docker build stages, so repo-wide Go
quality gates do not lint or line-count upstream files.

The named Compose volume mounted at `/app/whatsapp-bridge/store` preserves
`whatsapp.db` and `messages.db` across `docker compose down && docker compose up`.
