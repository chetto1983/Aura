# Spike Manifest

## Idea

Ground-truth probe of the MCP infrastructure Phase 9 (Swarm Minimal) depends on for its live E2E Gate-3 tier: prove the `internal/mcp` + `mcptools` seam works against the two REAL third-party stdio servers chosen during `/gsd-discuss-phase 9` (mail-mcp for email send-to-self, lharries/whatsapp-mcp for WhatsApp send-to-self), before `/gsd-plan-phase 9` plans around them. The seam has never been mounted against a live third-party server (only the calculator recipe + code-sandbox-mcp during Phase 8).

**Discovery at spike start:** a parallel Codex session already shipped most of the wiring the Phase-9 CONTEXT assumed missing — `internal/mcp` stdio client, `~/.aura/mcp/servers.json` managed registry, `aura mcp {install,add,list,doctor,tools,enable,disable,remove}` CLI, boot-level mounting in `buildRegistryWithMCP` (`cmd/aura/main.go:104`), and `calendar_integration`/`whatsapp_integration` test scaffolds whose tool-name assertions match MarimerLLC/calendar-mcp and lharries/whatsapp-mcp. The spikes validate the LIVE behavior; the Mount allowlist (CONTEXT D-20) remains un-built.

## Requirements

- Mail + WhatsApp sends in tests go ONLY to the user's own mailbox/number; ground truth = read-back via the same MCP server.
- MCP server registration goes through the existing managed config (`aura mcp add`/`install` → `~/.aura/mcp/servers.json`), NOT new `AURA_MCP_*_SERVER` env vars (CONTEXT D-21 corrected by discovery; `AURA_MCP_*_SERVER_JSON` remain test-tier overrides).
- Secrets (SMTP/IMAP credentials) live in the managed config env entries or operator env — never committed.

## Spikes

| # | Name | Type | Validates | Verdict | Tags |
|---|------|------|-----------|---------|------|
| 001 | mail-mcp-live-mount | standard | Given mail-mcp built (npm) + IMAP/SMTP creds, when mounted via the managed config and a send_email→search/fetch round-trip to self runs, then namespaced mail__* tools register and the sent message is read back | VALIDATED ✓ | mcp, mail, mount, phase-9 |
| 002 | whatsapp-mcp-pairing | standard | Given lharries/whatsapp-mcp paired via QR to the user's number, when send_message to self + list_messages, then the message is read back and the existing whatsapp_integration test passes live | PENDING | mcp, whatsapp, whatsmeow, phase-9 |
