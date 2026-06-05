---
spike: 001
name: mail-mcp-live-mount
type: standard
validates: "Given mail-mcp built (npm) + IMAP/SMTP creds in the managed config, when mounted via mcp.Open + mcptools.Mount and a send_email→search_emails round-trip to self runs, then namespaced mail__* tools register and the sent message is read back"
verdict: VALIDATED
related: [002]
tags: [mcp, mail, mount, phase-9]
---

# Spike 001: mail-mcp-live-mount

## What This Validates

Given mail-mcp (martinzarfl, Node/TS, stdio) built at `D:/tmp/mail-mcp` and a `mail` entry in `~/.aura/mcp/servers.json` (creds from `.env` `MAIL_MCP_PASSWORD`, Gmail app password), when this harness opens the server, mounts it into a fresh `tools.Registry`, sends a uniquely-tagged email to self, and polls `search_emails` for the tag — then the namespaced `mail__*` tools register (8.1 contract) and the message is read back via IMAP. This is the exact ground-truth loop Phase 9's live E2E uses, and the **first-ever live mount** of the `internal/mcp` + `mcptools` seam against a real third-party server.

## Research

- mail-mcp: Node 22 / TypeScript, `npm install && npm run build` → `dist/index.js`, stdio default. Config = env vars `SMTP_{HOST,PORT,USER,PASS,FROM}` + `IMAP_{HOST,PORT,...}` (IMAP defaults to SMTP values). 16 tools.
- Pre-spike discovery: the managed-config registry (`~/.aura/mcp/servers.json`), `aura mcp` CLI (incl. `doctor`), and boot mounting (`buildRegistryWithMCP`, `cmd/aura/main.go:104`) already exist (Codex commit `ae11737a`) — the spike rides them instead of new `AURA_MCP_*_SERVER` env vars.

## How to Run

```bash
# one-time: register the server (creds NOT committed; password read from .env)
#   ~/.aura/mcp/servers.json gains a "mail" entry (source: spike-001)
go run ./.planning/spikes/001-mail-mcp-live-mount
```

## What to Expect

Forensic ISO-timestamped log: handshake <1s → 16 tools listed → 16 `mail__*` mounted → `send_email` 250 OK → tag found via `search_emails` within ~90s → bridged `Execute` preview → `SUMMARY VALIDATED`, exit 0.

## Investigation Trail

1. **Run 1:** handshake 755ms ✓, 16 tools ✓, mount all namespaced `mail__*` ✓ (64-byte cap respected), manifest renders ✓, `send_email` → `250 2.0.0 OK` (real Gmail delivery, auth from `.env` app password worked first try). **Read-back FAILED**: harness guessed `search_emails {criteria:{subject}}` — actual schema is `{query (required), mailbox?, limit?}` (Zod validation error came back clean through the seam, itself a good sign: tool-level errors propagate as structured text, not transport failures).
2. **Run 2** (args fixed to `{query: tag}`): full green — read-back on poll attempt 1 (~11s after send), bridged `Execute(mail__search_emails)` through `WithToolCallContext`/`ToolResult` returned a 465-byte preview containing the tag, untruncated.

## Results

**VALIDATED ✓** — every link of the chain proven live:

| Link | Evidence |
|---|---|
| stdio handshake (`mcp.Open`) vs real TS MCP SDK server | 487-755ms, stable across runs |
| `ListTools` | 16/16 advertised, names match source (`src/index.ts:976-1364`) |
| `mcptools.Mount` 8.1 namespacing | all 16 `mail__*`, ≤64 bytes, alphabetical manifest |
| SMTP send (side effect) | 2 real emails delivered to self, `250 2.0.0 OK` + messageId |
| IMAP read-back (E2E ground-truth loop) | tag found ~11s post-send, 1 poll attempt |
| Bridged `Execute` path (what the agent calls) | 465B preview, truncated=false, ToolResult contract held |
| Tool-error propagation | Zod validation error surfaced as structured tool error (model-self-correctable) |

**Surprises / findings for Phase 9:**

1. **`bridge.go:88` mounts `Deferred: false`** — 16 non-deferred mail tools (+ ~10 WhatsApp next) land in EVERY manifest, marching straight into the 30-50-tool degradation threshold 8.1 was built to defend. Phase 9 must flip bridged tools to `Deferred: true` (or make it configurable) alongside the D-20 allowlist.
2. **Footgun census confirms D-20**: server advertises `delete_mailbox`, `move_message`, `create_mailbox` — an unfiltered mount hands destructive mailbox ops to swarm workers. Allowlist v1: `send_email, fetch_emails, search_emails, get_thread`.
3. **`search_emails` contract**: `{query, mailbox?, limit?}` — the E2E scenario prompts/assertions must use `query`.
4. Read-back latency budget: ~11-15s send→searchable on Gmail; the E2E poll loop needs ≥30s headroom, 90s is comfortable.
5. `.golangci.yml` now excludes `.planning/` (spike harnesses are not production code; keeps the module-wide lint-0 gate intact).
