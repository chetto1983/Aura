# Phase-MM Synthesis — 9 stories, 3 waves

Synthesizes the three parallel planning reports (architecture, audio, image)
into one Ralph-ready story list. Source reports:

- `docs/phase-mm-architecture-plan-2026-05-17.md` — substrate-level changes
- `docs/phase-mm-audio-plan-2026-05-17.md` — audio IN (whisper.cpp) + audio OUT (Piper)
- `docs/phase-mm-image-plan-2026-05-17.md` — image IN (OpenRouter Sonnet vision) + image OUT (Replicate Flux)

## Execution order

**Wave 1 — Substrate (foundation, blocking)**

These 3 stories MUST land before any modality work. They extend
`chat.InboundMessage`, `llm.Message`, and source.Kind enums without breaking
the text-only path (additive only).

| ID | Title | Files | Estimate |
| --- | --- | --- | --- |
| US-MM-ARCH01 | Populate `InboundMessage.Attachments` from Telegram voice/photo/document | `internal/channels/telegram/inbound.go`, `internal/chat/hub.go` | 3-4h |
| US-MM-ARCH02 | Add `MultipartContent []ContentPart` to `llm.Message` with dual serializer in openai.go | `internal/llm/client.go`, `internal/llm/openai.go` | 3-4h |
| US-MM-ARCH03 | Extend source.Kind with KindAudio + KindImage (already reserved) + format detection | `internal/storage/sources/store/`, `internal/storage/sources/formats.go` | 2-3h |

**Wave 2 — Audio + Image landing (parallel after Wave 1)**

Once the substrate accepts attachments + multipart, these stories can land
in parallel.

| ID | Title | Files | Estimate |
| --- | --- | --- | --- |
| US-MM-AUDIO01 | Voice message ingestion: Telegram voice → source store with KindAudio + handler mirroring `documents.go` | `internal/telegram/voice_handler.go` (new), `setup.go` wiring | 4-5h |
| US-MM-AUDIO02 | whisper.cpp sidecar + `transcribe_audio` tool (default base.en CPU, optional Groq API fallback) | `internal/storage/sources/whisper/` (new), `internal/agent/tools/registry/transcribe_audio.go` (new) | 4-5h |
| US-MM-IMAGE01 | Photo upload: Telegram photo → source store with KindImage + inline vision pass via multipart message to LLM | `internal/telegram/photos.go` (new), `invocation_builder.go` wiring | 4-5h |
| US-MM-IMAGE02 | `generate_image` tool (Replicate Flux.1-schnell default, capability-gated) | `internal/storage/sources/replicate/` (new), `internal/agent/tools/registry/generate_image.go` (new) | 3-4h |

**Wave 3 — Output modalities + E2E (after Wave 2)**

| ID | Title | Files | Estimate |
| --- | --- | --- | --- |
| US-MM-AUDIO03 | TTS output via Piper local + operator-toggle (default OFF, `audio_enable_tts=true` enables it; never auto-triggered) | `internal/storage/sources/piper/` (new), `outbound.go` integration | 4-5h |
| US-MM-E2E01 | End-to-end multimodal probe: voice → transcribe → agent turn → vision photo → describe → reply (text or voice if TTS enabled) | `cmd/probe_multimodal/main.go` (new), fixture audio + image | 3-4h |

**Total: 9 stories, ~30-40h of focused work, ~4-5 sessions.** Matches the
prd.md §7.4 estimate of ~4 sessions for Phase-MM.

## Decisions locked-in by the planning

**Default models (operator can override via env vars):**
- Audio IN: whisper.cpp base.en CPU (5-8s/30s, $0)
- Audio OUT: Piper en_US-amy-low (2-3s/30s, $0)
- Image IN: anthropic/claude-sonnet-4-6 via OpenRouter ($0.003/img, 2-3s)
- Image OUT: Replicate Flux.1-schnell ($0.003/img, 4-6s)

**Mini-PC budget invariants preserved:**
- whisper.cpp uses ≤4 threads (per `feedback_minipc_cpu_budget`)
- Piper is CPU-only and fast (~2s)
- Vision + generation go to API by default (no GPU on mini-PC)
- Local Qwen2.5-VL + local Flux remain as operator opt-in for privacy/cost

**Substrate vs plugin boundary:**
- Substrate (Wave 1): channel adapter attachments, llm multipart, source.Kind
- Plugin (Phase-U future): which model wired (whisper local vs Groq; Sonnet vs Qwen-VL; Flux vs DALL-E vs Imagen)

**Backward compatibility:**
- All Wave 1 changes are additive — no breaking changes to text-only path
- Existing tests stay green without modification
- Operators using only text-mode see zero difference

## Tool surface

Three new LLM-callable tools:

- `transcribe_audio(source_id, language?)` — read-only, takes a source_id from a voice message OR a manually uploaded audio file, returns transcript. Read-only so no capability gate needed beyond `tool.execute.transcribe_audio`.
- `generate_image(prompt, style?, size?)` — write-side (creates a new source). Capability-gated under `tool.execute.generate_image` with rate limit per the existing budget tracker.
- `synthesize_speech(text)` — write-side (creates a new outbound media payload). Capability-gated AND globally disabled by default (operator must set `audio_enable_tts=true`). LLM never auto-triggers — agent only calls when explicitly asked.

## Open questions surfaced (defer to operator before Wave 1 kickoff)

From audio plan:
1. Whisper model: base.en (5-8s) vs medium.en (2-3s, ~2GB RAM)?
2. Voice message frequency expected? Affects SSD storage retention policy
3. Piper voice preference (default amy-low)
4. Transcription language (en-only vs auto-detect)?

From image plan:
1. Vision model: Sonnet 4.6 (best quality) vs Sonnet 4.6 Haiku-tier (cheaper, lower quality)?
2. Generation provider: Replicate Flux (cheap, fast) vs DALL-E 3 (expensive, best)?
3. Local fallback (Qwen2.5-VL + Stable Diffusion) priority: must-have or opt-in?
4. Image rate limit: max generations per chat per hour?

From architecture plan:
1. Inline vs background ingest for audio/image (both planned — confirm default)?
2. Web channel parity: audio in via dashboard upload too, or Telegram-only first?
3. Attachment privacy in run_events archive (log SourceID + filename only, suppress transcript content)?

These questions don't block Wave 1 (substrate) but should be answered before
Wave 2 (modality stories) so the defaults are right.

## Ralph kickoff plan

When Phase-FIX closes and Phase-MM is ready to start:

1. Archive Phase-Z (already done) → Phase-FIX (Ralph) → archive Phase-FIX
2. Open `scripts/ralph/prd.json` with Wave 1 (3 ARCH stories) — Ralph ships them
3. Open follow-up queue with Wave 2 (4 stories: 2 audio + 2 image) — Ralph parallel
4. Final queue Wave 3 (2 stories: TTS + E2E probe)

Or one big queue of 9 stories with MAX_ITER=12 letting Ralph blast through —
the dependency ordering is documented in the priority numbers.

---

Produced 2026-05-17 evening from 3 parallel research subagents. Each report
remains the source of truth for its dimension; this synthesis is the
operator-facing summary.
