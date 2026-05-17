# Aura — Production Runbook

> **Note:** This document is a stub. The full Cold Install, Upgrade, Rollback, Restore, and key-rotation procedures ship in Phase-T US-T06. This file is pre-created so that deploy tooling and cross-references can point to a stable path.

## Production Overlay

Aura ships with a `compose.prod.yaml` overlay that tightens the dev defaults for production deployment. Activate it with:

```sh
docker compose -f compose.yaml -f compose.prod.yaml up -d
```

Key differences from the dev `compose.yaml`:
- **Dashboard port**: bound to `127.0.0.1:8080` only (not the LAN-accessible `0.0.0.0:18080`). Reach the dashboard via SSH tunnel: `ssh -L 8080:localhost:8080 <server>`.
- **Restart policy**: all long-running services enforce `restart: unless-stopped`.
- **Healthchecks**: 60 s interval checks added to all sidecars (searxng, garage, qdrant, aura-llama-embed, aura-markitdown).

The overlay is additive — it never modifies `compose.yaml`, so dev and prod share a single source of truth.

---

*Full operational procedures (Cold Install, Upgrade, Rollback, SQLite Restore Drill, Telegram Token Rotation, Mistral Key Rotation, llama-embed Rebuild, Common Failure Modes) will be added in US-T06.*
