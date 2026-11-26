# Aura image vendored sources

> The standalone `mail-mcp` recipe was retired. The calendar PIM sidecar (forked
> calendar-mcp → `chetto1983/aura-pim-mcp`, a separate compose service published to
> GHCR) now provides email (get/search/send) over MCP, so no mail bin is vendored
> into this image. See `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md`.

## Recipe sources (not vendored)

- `recipe:calculator` → `chetto1983/calculator-mcp-server` @ `46a1e66709bc387e8c223f15ec25fb5ae3a1af08` (own fork, pinned; uvx warm-cached at build).
- `recipe:calendar` → forked `chetto1983/aura-pim-mcp` @ `main`, last synced with upstream calendar-mcp 1.9.0. The fork's `AURA-FORK.md` lists each difference from upstream. It is a separate compose service.
  - **Surface.** One MCP tool per operation: upstream's 29 tool classes. Aura mounts them as `calendar__<tool>`. Each tool has only its own parameters and its own `required` list. `send_email`, `create_event` and `create_contact` require `accountId`. The one multiplexed `calendar` action tool of 2026-08-22 is gone (`docs/superpowers/specs/2026-11-23-pim-tool-split-design.md`, shipped in `442c2058d`). The split was checked on the lab VM on 2026-11-25 at fork `8fbd799`. The current pin, which adds `accountId` as required on `create_event` and `create_contact`, reached the VM through the updater on 2026-11-26.
  - **Pin.** Compose pins the image by the full fork commit sha, the tag its publish workflow writes on every push to `main` (`ghcr.io/chetto1983/aura-pim-mcp:bcab36e2a4f0c18ead2aed4205485c22b3d530f5`). The pull policy defaults to `missing`.
  - **What moves the pin.** A push to the fork reaches an appliance only through an Aura release that moves this pin. The updater retires `AURA_PIM_MCP_IMAGE` from appliance `.env` files. A developer points at a local build with `AURA_PIM_MCP_DEV_IMAGE`.
- `recipe:whatsapp` → forked `chetto1983/whatsapp-mcp` @ `main` (whatsmeow bridge + MCP-2.0.0 HTTP front + cockpit management REST), a separate compose service pinned by commit to GHCR (`ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345`). The `aura/cockpit-connect` branch is retired — fused into `main` on 2026-07-01 — and `:sidecar` is contested by two publish workflows, so the pin is a `sha-` tag. No longer vendored into this image; the old `docker/whatsapp/` local build was retired with the GHCR fork.
