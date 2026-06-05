---
spike: 002
name: whatsapp-mcp-pairing
type: standard
validates: "Given lharries/whatsapp-mcp paired via QR to the user's number, when send_message to self + list_messages, then the message is read back via MCP"
verdict: VALIDATED
related: [001]
tags: [mcp, whatsapp, whatsmeow, phase-9]
---

# Spike 002: whatsapp-mcp-pairing

## What This Validates

Given lharries/whatsapp-mcp — Go whatsmeow bridge (WSL, CGO/sqlite, REST :8080) + Python/uv MCP server (stdio spawned through `wsl.exe` from the Windows host) — paired via QR to the user's personal number, when `send_message` to SELF runs via the mounted MCP and `list_messages` polls for the tag, then the message is read back. The WhatsApp half of Phase 9's live E2E ground truth, and the second live exercise of the `mcptools` seam (including the Windows↔WSL stdio topology).

## Research

- Architecture: `whatsapp-bridge/` (Go, whatsmeow + mattn/go-sqlite3 → **CGO**, QR-only pairing via qrterminal, REST `:8080`, stores `store/messages.db`) + `whatsapp-mcp-server/` (Python, uv, MCP stdio; reads the bridge's SQLite + calls its REST for sends; 12 tools).
- Windows host has no CGO toolchain on PATH and no uv → **everything runs in WSL** (primary dev env); Aura spawns the MCP server as `wsl -e bash -lc '… uv run main.py'` — stdio pipes through wsl.exe transparently.
- The existing `internal/mcp/whatsapp_integration_test.go` tool-name assertions (`search_contacts, list_messages, list_chats, send_message`) match this server — it was already the intended bridge.

## How to Run

```bash
# bridge (WSL, once paired the session persists in store/whatsapp.db):
wsl -e bash -lc 'cd ~/whatsapp-mcp/whatsapp-bridge && ./whatsapp-bridge'
# full round-trip:
go run ./.planning/spikes/002-whatsapp-mcp-pairing -self 39XXXXXXXXXX
# read-only validation of a phone-sent message:
go run ./.planning/spikes/002-whatsapp-mcp-pairing -read-chat <jid> -expect <text>
```

## What to Expect

Handshake (via wsl.exe) <1s → 12 tools → `whatsapp__*` mount → `send_message` success → tag found via `list_messages` on attempt 1 → `SUMMARY VALIDATED`, exit 0. A real WhatsApp message lands in the user's self-chat.

## Investigation Trail

1. **Upstream repo is stale**: first run → `Client outdated (405)` — the pinned whatsmeow (2025-03-18) is rejected by WhatsApp servers. Bumped to `@latest` (v0.0.0-**20260603**… — released the day before this spike).
2. **API drift**: new whatsmeow requires `context.Context` in 5 call sites (`Download`, `sqlstore.New`, `GetFirstDevice`, `GetGroupInfo`, `GetContact`) — patched with `context.Background()`.
3. **Stale half-paired store** from the 405 attempt broke re-pairing → wiped `store/`, fresh QR, user scanned, `Successfully authenticated`.
4. **Process hygiene footgun**: `pkill -f whatsapp-bridge` matched the invoking `bash -lc` command line itself (path contains the pattern) and killed its own shell (exit 15). Two concurrent bridge instances (mine + user's) briefly shared the SQLite store — killed by stdout fd inspection (`/proc/<pid>/fd/1`).
5. **Send works, read-back fails**: `send_message` → `success:true` + message delivered to phone, but `list_messages` saw nothing — **whatsmeow does not echo self-sent messages as events**, so the bridge never stored them.
6. **LID discovery**: a message sent FROM THE PHONE to self landed in the store under the user's **`@lid` JID** (`…@lid`), not `phone@s.whatsapp.net` — WhatsApp's new linked-identity addressing splits the "self chat" across two JIDs depending on originating device. Read half via MCP `list_messages(chat_jid=<lid>)` VALIDATED.
7. **Bridge patch** (`bridge-patch.diff`, in this dir): after a successful REST `/api/send` with text, persist the message into `messages.db` (`StoreChat` + `StoreMessage`, `is_from_me=true`, `aura-local-<nanos>` id) — whatsmeow can't echo it, so the bridge records its own side effect. Rebuilt, bridge re-authenticated from the persisted session (no QR needed).
8. **Full round-trip green**: send → read-back on poll attempt 1, <1s end-to-end.

## Results

**VALIDATED ✓** (with the bridge patch applied — `bridge-patch.diff` is part of the verdict):

| Link | Evidence |
|---|---|
| stdio handshake THROUGH wsl.exe (Windows host ↔ WSL MCP server) | 382-1067ms across runs |
| 12 tools advertised, mount namespaced `whatsapp__*` ≤64B | all runs |
| QR pairing + session persistence | re-auth after restart with no QR |
| `send_message` to self (real side effect) | `success:true`, messages delivered to phone (user-confirmed) |
| Read-back of phone-sent messages | "Test invio" found via MCP `list_messages` on `@lid` chat |
| Read-back of bridge-sent messages | tag found attempt 1 **after the patch** |

**Findings for Phase 9 (E2E design impact):**

1. **The bridge needs Aura's patch** to provide automated ground truth for agent-sent messages (no whatsmeow self-echo). `bridge-patch.diff` = whatsmeow bump + 5 context fixes + REST-send persistence. **Patch maintained in the user's fork: <https://github.com/chetto1983/whatsapp-mcp> (commit `6de1dcd`)** — the canonical source for Phase 9, mirroring the `recipe:calculator` → chetto1983 fork pattern; Phase 9 adds `mail`/`whatsapp` recipes to `cmd/aura/mcp.go` pointing at it.
2. **LID vs phone JID duality**: self-chat is `<phone>@s.whatsapp.net` for bridge-sent rows, `<lid>@lid` for phone-sent rows. E2E assertions must target the right JID per direction (or query both).
3. **WSL topology validated**: `ServerConfig{Command:"wsl", Args:[…uv run main.py]}` in the managed config works unchanged through `mcp.Open` — no Aura code changes needed for WSL-resident servers.
4. **Operational pre-req for E2E**: the bridge is a long-lived third process (REST :8080) — the E2E tier needs a bring-up step + health check (REST 405 on GET /api/send = alive) before scenarios run.
5. Upstream staleness risk: whatsmeow must track latest (server-enforced 405 on old clients) — pin-and-refresh discipline, not pin-and-forget.
