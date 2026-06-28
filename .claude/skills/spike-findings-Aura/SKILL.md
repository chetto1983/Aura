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

The 2026-06-08 to 2026-06-28 sessions (7-20) extend the blueprint across the rest of
the build: Phase-15 memory (agent-memory MCP + provenance-safe dedup), large-document
ingestion, Phase-14 onboarding + `Agent.md`, adaptive-reasoning (SHIPPED granite tier
classifier), the unified `semindex` semantic tool_search, the Slice-13 local-LLM fallback
(Gemma-4 MTP + FunctionGemma finetune), Phase-17 packaging (fat box vs gVisor), the
calendar/PIM `aura-pim-mcp` fork, the graph-DB eval (STAY with Neo4j), and the v1.0.0
upload-to-chat RAG hardening (rerank + the three Items + the native document_id pre-filter).

Spike sessions wrapped: 2026-06-04 (001-002), 2026-06-05 (003-010 session 2,
011-013 session 3), 2026-06-06 (014-016 session 4), 2026-06-07 (017-019 session 5,
020-029 + 030 session 6); 2026-06-08 (031 s7, 032-035 s8, 036-039 s9, 040-042 s10),
2026-06-12 (043a-047 s11-12, 048-050 s13, 052-058 s14), 2026-06-14 (059-062 s15),
2026-06-16 (063-066 s16), 2026-06-20 (067-069 s17-18), 2026-06-21 (070-074 s19),
2026-06-28 (075-077 s20).
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

### Sessions 7-20 (the full binding Requirements live in each area's reference file)

The session-7→20 areas each carry their own non-negotiables in `references/<area>.md` →
`## Requirements`. The load-bearing ones to honor everywhere:

- **Memory**: upstream agent-memory semantic dedup is NOT provenance-safe — pass
  `source_id`/`document_id`/`run_id` (fork `aura/provenance-safe-dedup` `c1c2d65`); 16 tools
  stay namespaced `memory__*` + Deferred; `DenyRisk=write` keeps the read subset.
- **Ingestion**: exact 5/50 MiB tiers (`>` not `>=`); every chunk carries the 8 provenance
  keys; sparse-first `searchable` before background dense embeddings.
- **Onboarding**: `Agent.md` is protected user-role `messages[1]`, never a 2nd system message;
  `messages[0]` byte-stable; Windows `MoveFileEx` atomic writes; identity-path traversal guard.
- **Adaptive reasoning**: tier sets `Reasoning.Effort` ONLY — **never `max_tokens`** (truncates
  tool-call JSON); embed via granite `:8081`; learning is async/offline + opt-in.
- **Tool-search**: EMBEDDING-PRIMARY (no RRF, no ANN), one `internal/semindex` core; re-embed
  on MCP mount; keep `web_search`/`web_fetch` un-deferred.
- **Local-LLM**: `llama.cpp` not vLLM on 4 GB; pin `server-cuda` ≥2026-06-11; n-max=2; the
  unified Gemma sidecar does NOT retire the 3-CPU-sidecar 9c stack.
- **FunctionGemma**: finetune mandatory (base unusable); train safetensors; custom
  `<start_function_call>` parser; keep off the selection hot path; **074 eval PENDING**.
- **Packaging**: fat full-power box, **REVERT `ec7fe2f6`**; gVisor `runsc` optional tier
  (native-Linux/arm64, PRD amendment); `sbx` is not the appliance runtime.
- **Calendar/PIM**: fork to `aura-pim-mcp` HTTP sidecar replacing mail-mcp; managed-config
  registration; Google Desktop-app OAuth client; Graph email ~25s read-back lag.
- **Graph-DB**: STAY with Neo4j; embeddings **384d** (pin every vector DDL); AGE rejected,
  ArcadeDB fallback only behind a deeper-PoC gate (pinned post-CVE release).
- **Retrieval/RAG**: rerank GPU-mandatory + fail-soft to RRF; scoped retrieval uses the native
  `document_id` PRE-filter; catalog from `ListForThread` via the cache-safe tail, never
  `messages[0]`/`[1]`; Item-3 eval is gated RAGAS (pinned `ragas==0.2.15`), not CI.
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
| Memory graph (Phase 15) | references/memory-graph.md | `aura-agent-memory-mcp` live at `:8091` (16 deferred `memory__*`); upstream semantic dedup over-merges (0.95-0.997) — fixed ONLY by fork `aura/provenance-safe-dedup` `c1c2d65` needing `source_id`/`document_id`/`run_id` keys; facts read back via `graph_query` only |
| Document ingestion (Phase 15) | references/document-ingestion.md | Page-aware sparse-first lane = searchable in ~1.6s on the 830-pg G220; provenance-scoped chunk identity (8 keys), dense embeddings background; exact 5/50 MiB tiers; PrivateGPT (`@8ac84e3c`) reference-only, never its Celery/S3/Qdrant |
| Onboarding + Agent.md (Phase 14) | references/onboarding-agent-md.md | `Agent.md` = per-identity filesystem profile at protected user-role `messages[1]` (profile-first, then skills), NOT a 2nd system message; `messages[0]` byte-stable; Windows `MoveFileEx` atomic writes; Telegram onboarding = `LoopAgent` escalate-to-finish |
| Adaptive reasoning (Slice 13) | references/adaptive-reasoning.md | SHIPPED granite tier classifier (90/92% @~10ms, `16cb5380`) + async centroid-refresh active-learning replaced the per-turn LLM router; tier sets `Reasoning.Effort` ONLY, **never `max_tokens`** (the 203-turn truncation disaster); AdaptThink≠AutoThink |
| Tool-search + semindex | references/tool-search-semindex.md | Semantic `tool_search` ships EMBEDDING-PRIMARY (granite 384d cosine ~2× BM25, **no RRF, no ANN** to N=115) via ONE ~90-LOC `internal/semindex` Index that also does reasoning classification; re-embed on MCP mount (~7ms/tool) |
| Local-LLM multimodal (Slice 13) | references/local-llm-multimodal.md | One pinned `llama.cpp:server-cuda` (≥2026-06-11) + gemma-4 E2B Q4 + MTP draft (n-max=2) + BF16 mmproj (CPU) does text+image+audio+video in 4 GB (peak 3392 MiB) where vLLM died; does NOT retire the 3-CPU-sidecar 9c stack |
| FunctionGemma local FC (Slice 13) | references/functiongemma-local-fc.md | Base FunctionGemma-270M unusable on Aura tools (~8% top1, ~80% refusal) → Colab LoRA finetune mandatory; train safetensors not -GGUF; custom `<start_function_call>` parser needed (`--jinja` won't parse it); eval 074 **PENDING** operator Colab run |
| Packaging box (Phase 17) | references/packaging-box.md | Box = **fat full-power** container (root, writable, `shell_exec`+MCP parity), NOT a distroless jail → **REVERT audit `ec7fe2f6`**; gVisor `runsc` via `compose.gvisor.yaml` is the only optional appliance isolation tier; Docker Sandboxes `sbx` is not the runtime |
| Calendar / PIM (Phase 9 ext) | references/calendar-pim.md | .NET 10 `calendar-mcp` interops with Aura's Go streamable-HTTP client (29 Deferred `calendar__*`, gate GREEN); consolidate mail+calendar into the **`aura-pim-mcp` HTTP-sidecar fork**; Google needs a Desktop-app OAuth client, Graph email ~25s lag |
| Graph-DB eval | references/graph-db-eval.md | **STAY with Neo4j** (maturity/risk, NOT perf — real-data parity ~1.0×); AGE rejected (9 gaps + GDS Leiden/PageRank blocker), ArcadeDB strongest fallback behind a deeper-PoC gate; embeddings are **384d** (Granite-97m), not 768d |
| Retrieval rerank + RAG (Phase 15/30) | references/retrieval-rerank-rag.md | Rerank worth it (kills TOC/lexical FPs) but **GPU-mandatory** (Qwen3-Reranker-0.6B Q4 @333ms; CPU 23s dead); v1.0.0 RAG Items 1/2/3 + native `document_id` pre-filter all VALIDATED + **IMPLEMENTED 2026-06-28** |

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
- 030-openrouter-mimo-v2.5-multimodal
- 031-phase15-memory-source-audit
- 032-agent-memory-mcp-live-mount
- 033-agent-memory-write-read-ground-truth
- 034-agent-memory-dedup-chaos
- 035-agent-memory-loop-recall
- 036-phase14-agentmd-source-truth
- 037-agentmd-messages1-cache-invariant
- 038-profile-store-atomic-contract
- 039-telegram-onboarding-loopagent-prototype
- 040-adaptive-reasoning-source-truth
- 041-optillm-autothink-runtime-fit
- 042-adaptive-budget-policy-shim
- 043a-aura-large-doc-markitdown
- 043b-privategpt-async-ingest-reference
- 044-memory-ingest-provenance-dedup
- 045-large-doc-retrieval-signal
- 046-telegram-ingest-job-ux
- 047-fast-lane-industrial-pdf-ingest
- 048-gemma4-mtp-gpu-fit
- 049-mtp-speedup-headtohead
- 050-gemma4-mtp-multimodal
- 052-reasoning-tier-embed-classifier
- 053-reasoning-classifier-active-learning
- 054-semantic-toolsearch-vs-bm25
- 055-toolsearch-scaling-cliff
- 056-hybrid-fusion-vs-pure
- 057-toolselection-oracle-signal
- 058-unified-embedding-index
- 059-phase17-box-parity-edge
- 060-phase17-fat-image-base
- 061-phase17-isolation-tier
- 062-docker-sandboxes-sbx-fit
- 063-calendar-mcp-build-launch
- 064-calendar-mcp-http-mount
- 066-calendar-oauth-e2e-google-outlook
- 067-apache-age-pipeline
- 068-arcadedb-pipeline
- 069-arcadedb-vs-neo4j-realdata
- 070-rerank-value-or-overengineered
- 071-fc270m-baseline-and-slot
- 072-fc270m-finetune-toolchain-fit
- 073-fc270m-dataset-from-registry
- 074-fc270m-finetuned-vs-baseline
- 075-image-ocr-searchable-chunks
- 076-ragas-faithfulness-discriminates
- 077-catalog-injection-recall
</metadata>
