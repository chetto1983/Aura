---
name: spike-findings-Aura
description: Implementation blueprint from spike experiments. Requirements, proven patterns, and verified knowledge for building Aura (skills self-extension, sandbox runtime, MCP live servers). Auto-loaded during implementation work.
---

<context>
## Project: Aura

Ground-truth spikes for the tabula-rasa rewrite: Phase-9 MCP infrastructure (mail +
WhatsApp live mounts), Phase-11 Skills (discovery/install architecture — re-litigated
session 3 into skill-driven self-extension with no approval ceremony), and the sandbox
runtime posture (mounts, deps, hardening tiers vs the amendment-#50 full-terminal home).

Spike sessions wrapped: 2026-06-04 (001-002), 2026-06-05 (003-010 session 2,
011-013 session 3).
</context>

<requirements>
## Requirements

Non-negotiable decisions that emerged during spiking (full text + provenance in
`.planning/spikes/MANIFEST.md` Requirements):

- Mail/WhatsApp test sends ONLY to the operator's own accounts; ground truth =
  read-back via the same MCP server. Registration through the managed config, never
  new env vars. Secrets never committed.
- **NO install-approval ceremony for skill installs** (operator directive 2026-06-05):
  Aura self-installs with `npx skills add` via the sandbox terminal, Claude-Code-style.
  Supersedes D-03/D-35 install-prudence. **PRD amendment required before implementation.**
- Discovery + install teaching = find-skills-style **skill content** (messages[1]
  always-block), not bespoke Go tools or system-prompt routing. The skills CLI is the
  transport. (~2,050 LOC become deletable — see references/skills-self-extension.md.)
- ONE security keep: the injection blocklist scans at **Loader level** (self-installed
  bodies never pass the Writer).
- Skill/snippet execution always by interpreter + path, never the exec bit.
- 7a frontmatter parser = real YAML lib + CRLF normalization.
- Dep strategy (bake / on-demand uv / hybrid) is a planner choice; `deps:` frontmatter
  becomes load-bearing if on-demand. Prod-parity egress decision is mandatory —
  Docker Desktop's accidental NAT is never a design input.
- Posture: amendment #50 full-terminal home (rw /skills, --no-token); per-identity
  gating arrives with capability_grants (Slice 1.7), not ceremonies.
</requirements>

<findings_index>
## Feature Areas

| Area | Reference | Key Finding |
|------|-----------|-------------|
| Skills self-extension | references/skills-self-extension.md | Skill-content + npx CLI beats the bespoke tool flow 4/4 vs 2/3 live; full autonomous find→add→use→artifact loop in one turn; delete ~2,050 LOC, add ~50 lines of markdown (nanobot-proven shape) |
| Sandbox runtime | references/sandbox-runtime.md | Compose binds work on Docker Desktop (no sync step needed); uv installs deps in 0.3-3s; hardening tiers (token/egress-proxy/gVisor) are the PROD menu — dev runs #50 full-trust |
| MCP live servers | references/mcp-live-servers.md | mail-mcp mounts clean; whatsapp needs the chetto1983 fork (whatsmeow bump + self-echo patch); bridged tools must flip to Deferred or the manifest degrades |

## Source Files

Original spike harnesses, compose overrides, Dockerfiles, the proven find-skills-aura
SKILL.md, the CONNECT proxy, and bridge-patch.diff are preserved under `sources/`.
</findings_index>

<metadata>
## Processed Spikes

- 001-mail-mcp-live-mount
- 002-whatsapp-mcp-pairing
- 003-skills-sh-search-api
- 004a-install-npx-cli
- 004b-install-native-clone
- 005-skills-ro-mount
- 006-xlsx-skill-dry-run
- 007-uv-on-demand-deps
- 008-sandbox-token-auth
- 009-sandbox-egress-allowlist
- 010-sandbox-gvisor-runsc
- 011-npx-find-noninteractive
- 012a-discovery-skill-driven
- 012b-discovery-tool-driven
- 013-thin-surface-gate-parity
</metadata>
