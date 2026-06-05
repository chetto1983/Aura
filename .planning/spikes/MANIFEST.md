# Spike Manifest

## Idea

Ground-truth probe of the MCP infrastructure Phase 9 (Swarm Minimal) depends on for its live E2E Gate-3 tier: prove the `internal/mcp` + `mcptools` seam works against the two REAL third-party stdio servers chosen during `/gsd-discuss-phase 9` (mail-mcp for email send-to-self, lharries/whatsapp-mcp for WhatsApp send-to-self), before `/gsd-plan-phase 9` plans around them. The seam has never been mounted against a live third-party server (only the calculator recipe + code-sandbox-mcp during Phase 8).

**Discovery at spike start:** a parallel Codex session already shipped most of the wiring the Phase-9 CONTEXT assumed missing — `internal/mcp` stdio client, `~/.aura/mcp/servers.json` managed registry, `aura mcp {install,add,list,doctor,tools,enable,disable,remove}` CLI, boot-level mounting in `buildRegistryWithMCP` (`cmd/aura/main.go:104`), and `calendar_integration`/`whatsapp_integration` test scaffolds whose tool-name assertions match MarimerLLC/calendar-mcp and lharries/whatsapp-mcp. The spikes validate the LIVE behavior; the Mount allowlist (CONTEXT D-20) remains un-built.

**Session 2 (2026-06-05, spikes 003-006):** the D-15 mandated pre-planning spike for Phase 11 (Skills) from `11-CONTEXT.md` — ground-truth the skills.sh installation path end-to-end before `/gsd-plan-phase 11`: search API stability, npx-vs-native-clone install comparison, the ro `/skills` bind mount on the dev Docker Desktop stack (the stack's FIRST bind mount — named volumes only today), and the xlsx North-Star ingredients dry-run (anthropics/skills/xlsx scripts by-path in the sandbox image).

## Requirements

- Mail + WhatsApp sends in tests go ONLY to the user's own mailbox/number; ground truth = read-back via the same MCP server.
- MCP server registration goes through the existing managed config (`aura mcp add`/`install` → `~/.aura/mcp/servers.json`), NOT new `AURA_MCP_*_SERVER` env vars (CONTEXT D-21 corrected by discovery; `AURA_MCP_*_SERVER_JSON` remain test-tier overrides).
- Secrets (SMTP/IMAP credentials) live in the managed config env entries or operator env — never committed.

## Spikes

| # | Name | Type | Validates | Verdict | Tags |
|---|------|------|-----------|---------|------|
| 001 | mail-mcp-live-mount | standard | Given mail-mcp built (npm) + IMAP/SMTP creds, when mounted via the managed config and a send_email→search/fetch round-trip to self runs, then namespaced mail__* tools register and the sent message is read back | VALIDATED ✓ | mcp, mail, mount, phase-9 |
| 002 | whatsapp-mcp-pairing | standard | Given lharries/whatsapp-mcp paired via QR to the user's number, when send_message to self + list_messages, then the message is read back and the existing whatsapp_integration test passes live | VALIDATED ✓ (bridge patch required — see README) | mcp, whatsapp, whatsmeow, phase-9 |
| 005 | skills-ro-mount | standard | Given sandbox-agent compose + a host export dir mounted ro at /skills, when a .py is materialized host-side and exec'd by path via sandbox_exec, then it runs in-container, host edits are live-visible, and in-container writes to /skills are refused | VALIDATED ✓ (symlinks-resolve finding — see README) | skills, sandbox, mount, phase-11 |
| 003 | skills-sh-search-api | standard | Given queries (xlsx, find skills, golang), when GET skills.sh/api/search?q= is fetched from Go, then stable JSON parses and both North-Star targets (anthropics/skills→xlsx, vercel-labs/skills→find-skills) are discoverable | VALIDATED ✓ (lax-decode contract — see README) | skills, catalog, api, phase-11 |
| 004a | install-npx-cli | comparison | Given owner/repo + skill name, when npx --yes skills add --skill <name> --copy -y runs into a scratch dir with sanitized env + timeout, then the skill dir lands hermetically + skills-lock.json computedHash recorded | VALIDATED ✓ (4.5s; lockfile hash not verifiable on Windows) | skills, install, npx, phase-11 |
| 004b | install-native-clone | comparison | Given the same source, when a Go harness shallow-clones + copies the skill dir + computes SHA-256, then the result is content-identical to 004a and the node/npx host dep is droppable | VALIDATED ✓ WINNER (bit-identical, 3× faster, own canonical hash) | skills, install, git, phase-11 |
| 006 | xlsx-skill-dry-run | standard | Given anthropics/skills/xlsx installed via the 004 winner, when its SKILL.md is parsed (frontmatter tolerance) and its bundled scripts run by path in the sandbox image, then openpyxl resolves and a real .xlsx is produced and read back | VALIDATED ✓ (image MUST bake deps — plan-changing; see README) | skills, xlsx, sandbox, north-star, phase-11 |
