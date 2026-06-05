# Spike Wrap-Up Summary

**Date:** 2026-06-05
**Spikes processed:** 15
**Feature areas:** skills-self-extension, sandbox-runtime, mcp-live-servers
**Skill output:** `./.claude/skills/spike-findings-Aura/`

## Processed Spikes

| # | Name | Type | Verdict | Feature Area |
|---|------|------|---------|--------------|
| 001 | mail-mcp-live-mount | standard | VALIDATED ✓ | mcp-live-servers |
| 002 | whatsapp-mcp-pairing | standard | VALIDATED ✓ (fork patch required) | mcp-live-servers |
| 003 | skills-sh-search-api | standard | VALIDATED ✓ (client superseded by CLI transport) | skills-self-extension |
| 004a | install-npx-cli | comparison | VALIDATED ✓ | skills-self-extension |
| 004b | install-native-clone | comparison | VALIDATED ✓ (WINNER of 004; moot under no-ceremony directive) | skills-self-extension |
| 005 | skills-ro-mount | standard | VALIDATED ✓ (ro inverted by amendment #50 → rw) | sandbox-runtime |
| 006 | xlsx-skill-dry-run | standard | VALIDATED ✓ (egressless premise corrected by 007) | sandbox-runtime |
| 007 | uv-on-demand-deps | standard | VALIDATED ✓ | sandbox-runtime |
| 008 | sandbox-token-auth | standard | VALIDATED ✓ (superseded on dev by #50 --no-token; prod menu) | sandbox-runtime |
| 009 | sandbox-egress-allowlist | standard | PARTIAL ⚠ (advisory on Docker Desktop; enforced on native Linux) | sandbox-runtime |
| 010 | sandbox-gvisor-runsc | standard | PARTIAL ⚠ (workload OK under runsc; native-Linux/CI tier only) | sandbox-runtime |
| 011 | npx-find-noninteractive | standard | VALIDATED ✓ | skills-self-extension |
| 012a | discovery-skill-driven | comparison | VALIDATED ✓ WINNER | skills-self-extension |
| 012b | discovery-tool-driven | comparison | VALIDATED ✓ baseline (dead-gate root cause found) | skills-self-extension |
| 013 | thin-surface-gate-parity | standard | VALIDATED ✓ | skills-self-extension |

## Key Findings

- **Skills architecture decided by live evidence**: skill-content + `npx skills` CLI
  via the sandbox terminal completes the full autonomous self-extension loop
  (find → add → use → artifact, one turn, 212s, DeepSeek-V4) and beats the shipped
  tool-driven flow (4/4 vs 2/3). Operator directive removed the install-approval
  ceremony (Claude-Code parity). ~2,050 LOC become deletable; ~50 lines of skill
  markdown + a Loader-level blocklist scan replace them. nanobot
  (242-LOC loader + clawhub skill) is the production prior art.
- **Production bug found**: the agent's pause machinery is name-gated to ask_user —
  `skill action=install`'s gate sentinel collapsed to `error: awaiting user input`,
  making the D-13 red-flag gate dead code and root-causing the E2E FAIL + the
  6-iteration amendment-#49 churn. Dies with the deletion.
- **Eval harness blind spot**: turnCapture records tool names only — the D-35
  catalog→ask_user→install→sandbox_exec hard floor was structurally unsatisfiable.
  Replacement gate = artifact + self-install evidence (012a harness pattern).
- **Sandbox**: compose binds work on Docker Desktop (immediate visibility); uv makes
  on-demand deps viable (0.3-3s); token/egress-proxy/gVisor are the prod hardening
  menu — dev runs amendment-#50 full-trust; never infer egress posture from Docker
  Desktop's accidental NAT.
- **MCP**: both Phase-9 live servers mount through the existing seam; whatsapp needs
  the chetto1983 fork (whatsmeow bump + self-echo persistence patch); bridged tools
  must flip to Deferred before both servers mount (manifest degradation threshold).

## Pending follow-through

- **PRD amendment** (supersedes D-03, D-13 gate surface, D-35 install-prudence,
  parts of #49; aligns with #50) BEFORE implementing the deletion/rewire slice.
- Binding decisions recorded in `.planning/spikes/MANIFEST.md` Requirements.
