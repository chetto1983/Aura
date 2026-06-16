# Aura image vendored sources

> The standalone `mail-mcp` recipe was retired. The calendar PIM sidecar (forked
> calendar-mcp → `chetto1983/aura-pim-mcp`, a separate compose service published to
> GHCR) now provides email (get/search/send) over MCP, so no mail bin is vendored
> into this image. See `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md`.

## Recipe sources (not vendored)

- `recipe:calculator` → `chetto1983/calculator-mcp-server` @ `46a1e66709bc387e8c223f15ec25fb5ae3a1af08` (own fork, pinned; uvx warm-cached at build).
- `recipe:whatsapp` → forked `chetto1983/whatsapp-mcp` @ `aura/cockpit-connect` (whatsmeow bridge + FastMCP HTTP front + cockpit management REST), a separate compose service published to GHCR (`ghcr.io/chetto1983/whatsapp-mcp:sidecar`). No longer vendored into this image; the old `docker/whatsapp/` local build was retired with the GHCR fork.
