---
spike: 030
name: openrouter-mimo-v2.5-multimodal
type: comparison
validates: "Given OpenRouter xiaomi/mimo-v2.5 (declared text+audio+image+video), when probed with the spike-024 vision set + real Italian audio + a real YouTube short, then its fitness as a cloud-vision-tier alternative to minimax-m3 and the reality of its audio/video legs are measured live"
verdict: VISION + VIDEO VALIDATED — STANDALONE AUDIO NOT WORKING (via OpenRouter)
related: [024-openrouter-minimax-m3-vision, 027-stt-half, 021-survey-2026-shortlist]
tags: [multimodal, vision, ocr, audio, openrouter, cloud-vision-tier, phase-13]
---

# Spike 030: OpenRouter xiaomi/mimo-v2.5 Multimodal Probe

## Why

Operator directive 2026-06-08: *"I find one more model can read image audio and video,
do a small test — https://openrouter.ai/xiaomi/mimo-v2.5"*. MiMo-V2.5 is a candidate for
the **9c cloud-vision tier** (`AURA_VISION_CLOUD=true`, currently `minimax/minimax-m3`,
amendment #59/5b). Mirrors spike 024 probe-for-probe so the vision result is apples-to-apples.

## Model facts (OpenRouter models API)

| field | value |
|-------|-------|
| id | `xiaomi/mimo-v2.5` (note: `-pro` and `-flash` siblings are **text-only**) |
| input modalities | `text, audio, image, video` (declared) |
| output | `text` |
| context | 1,048,576 |
| pricing | **$0.14/M prompt · $0.28/M completion** (cheaper than minimax-m3) |

## How to run

```bash
set -a; source <(tr -d '\r' < .env); set +a
go run -tags spike_multimodal ./.planning/spikes/030-openrouter-mimo-v2.5-multimodal
```

Assets staged to `D:\tmp\spike-030\` (volatile, not committed): `ocr-table.png` (PIL-rendered
Italian city table = same content as spike 024), `audio.mp3` (the real 37 s Italian voice note
from spike 027), optional `photo.jpg`. Probes auto-skip if their asset is absent.

## Results (live, 2026-06-08)

| Probe | Verdict | Evidence | latency |
|-------|---------|----------|---------|
| color (3-band PNG) | **PASS** | "red, green, blue" | ~5 s |
| ocr-it (Italian table) | **PASS** | "Cuneo: 56.000 · Caraglio: 6.800 · **Mondovì**: 22.000 abitanti" — all 3 cities (accent ✓) + all 3 populations correct (Italian thousand-separators) | ~3.3 s |
| **video** (1:53 YouTube short, `video_url` data URL) | **PASS** | 27,095 prompt tokens ingested; accurate **timestamped** IT description — transcribed the spoken IT commentary, read on-screen text ("36.000 stars", feature list), narrated UI actions (drag-drop, Deep Research demo). Needs `max_tokens ≥ ~3000` (reasoning model eats the budget first). | ~41 s |
| audio (37 s IT mp3, standalone `input_audio`) | **FAIL** | empty reply, **no usage block**, 146 s hang even with `max_tokens=4000` + `reasoning.exclude` — the standalone audio path is broken/dropped. *(But the audio track **inside** the video probe WAS transcribed.)* | 146 s |

### Content-part contract (learned live)
- **Image:** `image_url` data URL — only `bmp/gif/png/jpeg/webp` (a video mime → HTTP 400 "Param Incorrect: invalid image format").
- **Video:** `video_url` data URL (`data:video/mp4;base64,…`). This is the *only* video shape — `image_url` rejects it.
- **Audio:** `input_audio` `{data, format: mp3|wav}` — accepted without error but does **not** transcribe.
- **Reasoning-budget gotcha:** mimo-v2.5 is a reasoning model; with a small `max_tokens` the visible `content` comes back empty because reasoning consumed the whole completion budget. Set a generous `max_tokens` (and `reasoning.exclude` if you only want the answer).

Cost: **< $0.01 total** across all probes (vision ~$0.0002/call; full-video understanding ~$0.005/call at 27k input tokens).

> Meta-note: the test short is itself about **Odysseus** — a self-hosted, open-source, *agentic* AI platform (Chat, Agents, Tools, MCP, Compare, Deep Research, email assistant, memory, skills, local-privacy). That is squarely Aura's product space; useful competitive signal independent of the model test.

## Verdict

- **Vision = VALIDATED and competitive.** Italian OCR with accents and correct numbers, on par
  with minimax-m3 (spike 024) and cheaper. A viable drop-in for the `AURA_VISION_CLOUD=true`
  cloud-vision tier — the `MULTIMODAL_FALLBACK_MODEL` selector already abstracts which cloud
  model serves vision, so adopting mimo-v2.5 is a **config change, no code** (amendment #60 design).
- **Video = VALIDATED.** `video_url` data URL works end-to-end: full frame-sampled understanding
  plus transcription of the in-video audio track, accurate and timestamped, in Italian. A genuinely
  multimodal cloud option if a future tier ever needs video understanding (e.g. a user forwarding a
  Telegram video). Not in Phase-13 scope, but a real capability signal.
- **Standalone audio = NOT working via OpenRouter.** The `input_audio` leg hangs (146 s) and returns
  empty even with a generous budget — the audio path is effectively dead on this endpoint.
  **Does not change the 9c verdict:** voice-in stays **local faster-whisper** (spike 027) — private,
  GPU-free, sub-realtime, proven on Telegram OGG/Opus. Audio-cloud remains descoped for Phase 13.

**Net:** mimo-v2.5 is a cheap, accurate **second option for the cloud-vision tier** (and a working
*video*-understanding option) alongside minimax-m3 — recorded for the operator's product decision,
**not a Phase-13 blocker** (the committed local-default stack — GLM-OCR + faster-whisper + Kokoro —
is unchanged; adopting mimo for cloud vision is a `MULTIMODAL_FALLBACK_MODEL` config change).
