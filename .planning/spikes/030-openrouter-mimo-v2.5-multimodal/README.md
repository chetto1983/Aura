---
spike: 030
name: openrouter-mimo-v2.5-multimodal
type: comparison
validates: "Given OpenRouter xiaomi/mimo-v2.5 (declared text+audio+image+video), when probed with the spike-024 vision set + real Italian audio, then its fitness as a cloud-vision-tier alternative to minimax-m3 (and its audio leg) is measured live"
verdict: VISION VALIDATED — AUDIO NOT WORKING (via OpenRouter)
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

| Probe | Verdict | Evidence | p50 |
|-------|---------|----------|-----|
| color (3-band PNG) | **PASS** | "red, green, blue" | ~5.0 s |
| ocr-it (Italian table) | **PASS** | "Cuneo: 56.000 · Caraglio: 6.800 · **Mondovì**: 22.000 abitanti" — all 3 cities (accent ✓) + all 3 populations correct, Italian thousand-separators | ~3.3 s |
| audio (37 s IT mp3) | **FAIL (empty)** | reply `""`, only 508 prompt tokens for a 905 KB clip → `input_audio` part accepted without error but almost certainly **dropped** by the OpenRouter endpoint | ~5.8 s |

Cost: **$0.00059** for the whole run (6 vision calls + 1 audio).

## Verdict

- **Vision = VALIDATED and competitive.** Italian OCR with accents and correct numbers, on par
  with minimax-m3 (spike 024) and cheaper. A viable drop-in for the `AURA_VISION_CLOUD=true`
  cloud-vision tier — the `MULTIMODAL_FALLBACK_MODEL` selector already abstracts which cloud
  model serves vision, so adopting mimo-v2.5 is a **config change, no code** (amendment #60 design).
- **Audio = NOT confirmed working via OpenRouter.** Despite the declared `audio` modality, the
  `input_audio` leg returned an empty transcription with a token count too low to be a real audio
  ingest. **Does not change the 9c verdict:** voice-in stays **local faster-whisper** (spike 027),
  which is private, GPU-free, sub-realtime, and proven on Telegram OGG/Opus. Audio-cloud remains
  descoped for Phase 13.
- **Video = untested** (a single chat call can't easily carry video; out of scope here).

**Net:** mimo-v2.5 is a cheap, accurate **second option for the cloud-vision tier** alongside
minimax-m3 — recorded for the operator's product decision, **not a Phase-13 blocker** (the
committed local-default stack — GLM-OCR + faster-whisper + Kokoro — is unchanged).
