# MCP Live Servers (mail + WhatsApp, Phase-9 E2E ground truth)

## Requirements

- Mail + WhatsApp test sends go ONLY to the operator's own mailbox/number; ground
  truth = read-back via the same MCP server with a unique `AURA-SPIKE/E2E-<unix>` tag.
- Server registration goes through the managed config (`~/.aura/mcp/servers.json`,
  `aura mcp add`/`install`) — NOT new env vars. Secrets in managed-config env entries
  or operator env, never committed.

## How to Build It

**mail (martinzarfl/mail-mcp, Node 22 stdio):** `npm install && npm run build` →
`dist/index.js`; env `SMTP_{HOST,PORT,USER,PASS,FROM}` (+IMAP defaults from SMTP).
Mounts clean through `mcp.Open` + `mcptools.Mount` — 16 `mail__*` tools, handshake
<1s, Gmail app-password auth works. Read-back: `search_emails` takes
`{query (required), mailbox?, limit?}` — NOT a criteria object. Tag found ~11s
post-send, poll 5s/90s deadline.

**whatsapp (lharries/whatsapp-mcp = Go whatsmeow bridge + Python/uv MCP server):**
everything lives in WSL; Aura spawns it as
`ServerConfig{Command:"wsl", Args:["-e","bash","-lc","cd … && uv run main.py"]}` —
stdio pipes through wsl.exe with ZERO Aura changes. **Use the maintained fork
`chetto1983/whatsapp-mcp` (commit 6de1dcd)** — upstream is stale: whatsmeow must
track latest (servers 405 old clients), 5 context.Context call-site fixes, and the
Aura REST-send persistence patch (whatsmeow does NOT echo self-sent messages; the
bridge must store its own sends — `bridge-patch.diff`). QR pairing once; session
persists in `store/whatsapp.db`.

E2E operational pre-reqs: the bridge is a long-lived third process (REST :8080) —
bring-up + health probe (GET /api/send → 405 = alive) before scenarios.

## What to Avoid

- **Bridged tools mount `Deferred: false`** (`bridge.go:88` finding): 16 mail + ~10
  whatsapp non-deferred tools march straight into the 30-50-tool manifest degradation
  threshold. Flip bridged mounts to deferred (or make it per-server config) + the
  D-20 allowlist before mounting both servers.
- **LID vs phone JID duality**: WhatsApp self-chat rows split across
  `<phone>@s.whatsapp.net` (bridge-sent) and `<lid>@lid` (phone-sent) — assertions
  must target the right JID per direction or query both.
- **`pkill -f <pattern>` suicide**: never when the invoking shell's command line
  contains the pattern (it kills itself, exit 15) — identify by `/proc/<pid>/fd/1`.
- Stale half-paired whatsmeow stores break re-pairing — wipe `store/` and re-QR.

## Constraints

- whatsmeow: pin-and-refresh discipline (server-enforced 405 on old clients), QR
  rotates ~20-60s during pairing.
- Tool-level errors (e.g. Zod validation) propagate as structured text through the
  seam — model-self-correctable, not transport failures.

## Origin

Synthesized from spikes: 001, 002
Source files: sources/001-mail-mcp-live-mount/, sources/002-whatsapp-mcp-pairing/
(incl. bridge-patch.diff)
