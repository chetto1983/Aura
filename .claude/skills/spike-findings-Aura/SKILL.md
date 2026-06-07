---
name: spike-findings-Aura
description: Implementation blueprint from spike experiments. Requirements, proven patterns, and verified knowledge for building Aura (skills self-extension, sandbox runtime, MCP live servers, AG-UI gateway, Telegram channel). Auto-loaded during implementation work.
---

<context>
## Project: Aura

Ground-truth spikes for the tabula-rasa rewrite: Phase-9 MCP infrastructure (mail +
WhatsApp live mounts), Phase-11 Skills (discovery/install architecture — re-litigated
session 3 into skill-driven self-extension with no approval ceremony), the sandbox
runtime posture (mounts, deps, hardening tiers vs the amendment-#50 full-terminal home),
the Phase-12 AG-UI gateway (official Go SDK pin, event surface, SSE round-trip), the
Phase-13 Telegram channel (telebot pin, MarkdownV2 discipline, table rendering
head-to-head, artifact file delivery), and the Phase-13 Slice-9c multimodal engine
(vLLM-4GB killed → three local CPU sidecars: OCR-VL + faster-whisper STT + Kokoro TTS).

Spike sessions wrapped: 2026-06-04 (001-002), 2026-06-05 (003-010 session 2,
011-013 session 3), 2026-06-06 (014-016 session 4), 2026-06-07 (017-019 session 5,
020-029 session 6).
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
- **AG-UI SDK pin = pseudo-version** `v0.0.0-20260514093510-e9e910b230b9` (amendment #6's
  40-hex CI grep gate is structurally unsatisfiable — amend to a pseudo-version grep).
  **PRD amendment required before Phase-12 implementation** (4 fixes — see
  references/agui-gateway.md Requirements).
- Resume contract = protocol-native `RunAgentInput.Resume []ResumeEntry`, superseding the
  PRD's RoleTool-answers design; `outcome.type ∈ {success, interrupt}`; REASONING_* is
  canonical (THINKING_* deprecated); translator must skip empty deltas + guarantee IDs.
- **telebot.v4 pin = tag `v4.0.0-beta.9`** (repo is tagged now — amendment #5's SHA-pin
  premise stale; CI gate = literal version grep). **PRD amendment required before Phase-13.**
- **Tables on Telegram = PNG primary** (operator on-device verdict, common + stress case):
  markdown tables → gridded PNG via x/image + gofont/gomono 2x → `sendPhoto`; pre-block
  is the zero-dep fallback. mdv2.go escaper must be entity-aware (whole-string destroys
  intended formatting; one naked reserved char = whole send 400s).
- **The Telegram channel MUST deliver file artifacts** (operator directive 2026-06-07):
  `sendDocument` with path + filename only (Telegram detects MIME); wire
  `$AURA_RUN_DIR`//workspace artifacts to the chat.
- **9c multimodal = three local CPU sidecars, vLLM OUT** (spike session 6, PRD amendment #59):
  vLLM cannot host a 2B multimodal on the 4GB A2000 (KV starvation + WSL 7GiB RAM). Replace the
  single Gemma sidecar with `aura-ocr-vl` (llama.cpp + **GLM-OCR**, decided 2026-06-07; PaddleOCR-VL fallback),
  `aura-stt` (faster-whisper `hwdsl2/whisper-server`, voice-in), `aura-tts` (Kokoro-82M, voice-out).
  All CPU, GPU free, permissive licenses. **PRD amendment #59 committed.**
- **Aura speaks** (operator directive "facciamo parlare Aura"): TTS voice-out leg added to 9c;
  **voice = Kokoro `if_sara`** (female Italian), locked on-device. Voice cloning descoped.
- **OGG/Opus is the channel audio contract both ways** — faster-whisper decodes it inline
  (whisper.cpp can't); Kokoro `response_format: opus` → `sendVoice` with no transcode.
</requirements>

<findings_index>
## Feature Areas

| Area | Reference | Key Finding |
|------|-----------|-------------|
| Skills self-extension | references/skills-self-extension.md | Skill-content + npx CLI beats the bespoke tool flow 4/4 vs 2/3 live; full autonomous find→add→use→artifact loop in one turn; delete ~2,050 LOC, add ~50 lines of markdown (nanobot-proven shape) |
| Sandbox runtime | references/sandbox-runtime.md | Compose binds work on Docker Desktop (no sync step needed); uv installs deps in 0.3-3s; hardening tiers (token/egress-proxy/gVisor) are the PROD menu — dev runs #50 full-trust |
| MCP live servers | references/mcp-live-servers.md | mail-mcp mounts clean; whatsapp needs the chetto1983 fork (whatsmeow bump + self-echo patch); bridged tools must flip to Deferred or the manifest degrades |
| AG-UI gateway | references/agui-gateway.md | SDK pin = pseudo-version (amendment-#6 grep gate unsatisfiable as written); 21/21 PRD events exist incl. native REASONING_*; resume contract is protocol-native `resume[]`; ~60-LOC pure iter.Seq2 translator + SDK SSEWriter round-trips the PRD smoke verbatim at 35-40ms loopback |
| Telegram channel | references/telegram-channel.md | telebot pin is a TAG now (v4.0.0-beta.9); tables render to PNG (pure Go x/image, 5-21ms, on-device WINNER over pre-block and key-value); sendDocument round-trips xlsx/pdf/docx/csv with exact MIME; send responses are the read-back ground truth |
| Multimodal 9c | references/multimodal-9c.md | vLLM-4GB INVALIDATED → three local CPU OpenAI-compat sidecars: `aura-ocr-vl` (llama.cpp + **GLM-OCR**, decided; PaddleOCR-VL fallback; IT OCR 7/7), `aura-stt` (faster-whisper, OGG/Opus direct, 0.7× RT), `aura-tts` (Kokoro `if_sara`, opus voice note, 0.3× RT). GPU free, permissive licenses. OGG/Opus bidirectional. PRD amendment #59 |

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
- 014-agui-sdk-module-pin
- 015-agui-event-surface
- 016-agui-sse-roundtrip
- 017-telebot-v4-sha-pin-live-send
- 018a-table-pre-block
- 018b-table-as-image
- 018c-table-restructured
- 019-artifact-file-delivery
- 020-vllm-sidecar-4gb-fit
- 021-survey-2026-shortlist
- 022-stt-wer-it-en
- 024-openrouter-minimax-m3-vision
- 025-paddleocr-vl-local
- 026-glm-ocr-local
- 027-stt-half
- 028-kokoro-tts
- 029-voice-cloning
</metadata>
