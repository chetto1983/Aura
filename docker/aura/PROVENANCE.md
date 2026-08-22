# Aura image vendored sources

> The standalone `mail-mcp` recipe was retired. The calendar PIM sidecar (forked
> calendar-mcp → `chetto1983/aura-pim-mcp`, a separate compose service published to
> GHCR) now provides email (get/search/send) over MCP, so no mail bin is vendored
> into this image. See `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md`.

## Recipe sources (not vendored)

- `recipe:calculator` → `chetto1983/calculator-mcp-server` @ `46a1e66709bc387e8c223f15ec25fb5ae3a1af08` (own fork, pinned; uvx warm-cached at build).
- `recipe:calendar` → forked `chetto1983/aura-pim-mcp` @ `aura/pim-sidecar` (calendar-mcp 1.4.1, Blazor UI stripped, tool surface trimmed 29→14), a separate compose service pinned by commit to GHCR (`ghcr.io/chetto1983/aura-pim-mcp:10383276961828bc19f34a9372ba2c64a14e2b62`). Only one workflow writes that fork's `:sidecar`, but the tag moved on 2026-08-22, so the pin is the raw commit sha its publish workflow tags alongside.
- `recipe:whatsapp` → forked `chetto1983/whatsapp-mcp` @ `main` (whatsmeow bridge + MCP-2.0.0 HTTP front + cockpit management REST), a separate compose service pinned by commit to GHCR (`ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345`). The `aura/cockpit-connect` branch is retired — fused into `main` on 2026-07-01 — and `:sidecar` is contested by two publish workflows, so the pin is a `sha-` tag. No longer vendored into this image; the old `docker/whatsapp/` local build was retired with the GHCR fork.
