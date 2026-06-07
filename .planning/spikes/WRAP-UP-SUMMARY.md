# Spike Wrap-Up Summary

**Date:** 2026-06-07 (sessions 1-3 wrapped 2026-06-05, session 4 2026-06-06, session 5 2026-06-07)
**Spikes processed:** 23
**Feature areas:** skills-self-extension, sandbox-runtime, mcp-live-servers, agui-gateway, telegram-channel
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
| 014 | agui-sdk-module-pin | standard | VALIDATED ✓ (amendment-#6 CI gate unsatisfiable as written) | agui-gateway |
| 015 | agui-event-surface | standard | VALIDATED ✓ (21/21 events; 4 amendments) | agui-gateway |
| 016 | agui-sse-roundtrip | standard | VALIDATED ✓ (13/13 PRD order, 35-40ms loopback) | agui-gateway |
| 017 | telebot-v4-sha-pin-live-send | standard | VALIDATED ✓ (pin is a TAG now — amendment #5 stale) | telegram-channel |
| 018a | table-pre-block | comparison | VALIDATED ✓ (loser; zero-dep fallback, no wrap ≤56 chars) | telegram-channel |
| 018b | table-as-image | comparison | VALIDATED ✓ WINNER (T2 + T3 on-device) | telegram-channel |
| 018c | table-restructured | comparison | VALIDATED ✓ (loser; key\|value 2-col cards only) | telegram-channel |
| 019 | artifact-file-delivery | standard | VALIDATED ✓ (4/4 MIME exact, all open on-device) | telegram-channel |

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
- **AG-UI gateway (session 4, pre-Phase-12)**: the official Go SDK at pin
  `v0.0.0-20260514093510-e9e910b230b9` covers 21/21 PRD-required events (REASONING_*
  native, #33 served; THINKING_* deprecated) and ships `RunAgentInput` with dual-case
  unmarshal + a protocol-native `resume[]` contract that supersedes the PRD's
  RoleTool-answers design (maps 1:1 to Slice-1.5 PausedState incl. HITL cancel).
  The −100-LOC iter.Seq2 translator design round-trips live over the SDK SSEWriter
  (13/13 PRD order, `event:`+`id:`+`data:` framing, 35-40ms loopback floor) with
  zero changes to internal/agent (D-17 holds). Amendment-#6's 40-hex CI grep gate
  is structurally unsatisfiable — pseudo-version grep instead.

- **Telegram channel (session 5, pre-Phase-13)**: telebot.v4 pin is a **tag** now
  (`v4.0.0-beta.9` — amendment #5's SHA-pin premise stale); Pitfall #18 verified strict
  (one naked reserved char = whole send 400s → mdv2.go must be entity-aware); **tables
  render to PNG** (pure Go x/image + gofont/gomono 2x, 5-21ms, ~150 LOC) — operator
  on-device WINNER over pre-block and key-value on both the common and the stress case;
  `sendDocument` round-trips xlsx/pdf/docx/csv byte-identical with exact MIME detection
  (operator requirement: the channel MUST deliver file artifacts). Bot-API send
  responses are the read-back ground truth (bot messages never hit getUpdates).

## Pending follow-through

- **PRD amendment** (supersedes D-03, D-13 gate surface, D-35 install-prudence,
  parts of #49; aligns with #50) BEFORE implementing the deletion/rewire slice.
- **PRD amendment for Phase 12** (4 fixes: #6 gate regex → pseudo-version; outcome
  literals {success, interrupt}; resume contract → protocol-native `resume[]`;
  event-count language) BEFORE `/gsd-plan-phase 12`.
- **PRD amendment for Phase 13** (3 items: #5 refresh → tag pin `v4.0.0-beta.9`;
  table-rendering policy PNG-primary in renderer.go; artifact-delivery requirement
  via `sendDocument`) BEFORE `/gsd-plan-phase 13`.
- Binding decisions recorded in `.planning/spikes/MANIFEST.md` Requirements.
