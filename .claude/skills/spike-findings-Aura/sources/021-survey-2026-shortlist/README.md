---
spike: 021
name: survey-2026-shortlist
type: standard
validates: "Given a web survey of 2026 multimodal models (STT+vision) + Ollama host probing, when ≤4GB candidates are pulled and probed (vision + audio support, VRAM fit, GPU/CPU split), then a measured shortlist with quick quality signal emerges"
verdict: SUPERSEDED
related: [020-vllm-sidecar-4gb-fit, 025-paddleocr-vl-local, 026-glm-ocr-local]
tags: [multimodal, survey, ollama, phase-13]
---

# Spike 021: 2026 Multimodal Survey + Shortlist via Ollama Host Probe

## What This Validates

Given a web survey of the 2026 multimodal market (STT + vision, ≤4GB VRAM) plus Ollama host
probing (D-15: Ollama 0.30.6 already installed — the zero-setup probe channel), when each
candidate is pulled and probed for vision quality signal, audio acceptance, VRAM fit and
GPU/CPU offload split, then a measured shortlist emerges that decides what enters spikes
022 (WER) and 023 (vision quality).

## Research

Survey checked 2026-06-07 (web — the PRD's Gemma-4-only view is stale, per 13-CONTEXT
"valutiamo altri modelli multimodali del 2026"):

### Vision/omni candidates (Ollama probe set)

| Candidate | Params | Ollama tag | Size | Modalities | Why |
|-----------|--------|-----------|------|------------|-----|
| Gemma 4 E2B | ~5B raw (2B eff.) | `gemma4:e2b` | ~3GB? | text+image+**audio** | PRD-family omni, smaller sibling |
| Gemma 4 E4B | ~7.5B raw (4B eff.) | `gemma4:e4b` | ~5GB? | text+image+**audio** | THE PRD baseline model |
| Qwen3-VL 2B | 2B | `qwen3-vl:2b` | 1.9GB | text+image | vLLM-parity candidate (020 probe A is its FP8 twin) |
| Qwen3-VL 4B | 4B | `qwen3-vl:4b` | 3.3GB | text+image | quality step-up, borderline fit |
| MiniCPM-V 4.6 | 1.3B | `openbmb/minicpm-v4.6` | ~1GB | text+image | [OCR-strong tiny](https://github.com/openbmb/MiniCPM-V) (OCRBench/DocVQA ≈ 7-8B class) |
| MiniCPM-o 4.5 | 8B | `openbmb/minicpm-o4.5` | ~5GB? | text+image+**speech** | omni stretch goal — expect partial offload |

### STT candidates (engine-aware)

| Candidate | Params | Engine | IT WER (published) | Notes |
|-----------|--------|--------|--------------------|-------|
| whisper-large-v3-turbo | 809M | **vLLM** `/v1/audio/transcriptions` | v3=9.26% FLEURS IT, turbo slightly worse | 020 probe B; the PRD voice.go contract verbatim |
| Gemma 4 E2B/E4B audio | 5-7.5B | Ollama / llama.cpp | unpublished | THE baseline question (#21325: GGUF has the audio encoder; server exposure unverified) |
| [Parakeet-TDT-0.6B-v3](https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3) | 600M | NeMo/ONNX — **NOT vLLM** | ~9.7% avg 24-lang (FLEURS 11.5) | 25 EU langs incl. IT; beats whisper-turbo on speed even on CPU; engine mismatch is a finding for the verdict |
| Canary-1B-v2 | 1B | NeMo — NOT vLLM | 8.1% avg | quality ref point |
| Voxtral Mini Transcribe V2 | 4B | vLLM | ~5.9% avg FLEURS | **excluded**: bf16 ~9.5GB, no ≤4GB quant shipped |

Published-WER triangulation: our measured FLEURS-IT/EN numbers in 022 should land near the
published figures — a big gap flags a harness bug, not a model property.

### Probe method

- Vision: `/api/chat` with the 3-band PNG (shared with 020) → colors named + latency ×3 warm.
- A second vision probe with REAL content (photo with text) for a quality signal beyond color recall.
- Audio: `ollama run <tag> "<prompt>" <wav>` CLI acceptance probe (API audio field undocumented).
  Probe clips = Windows SAPI TTS WAVs with KNOWN text (zero downloads, real speech):
  `probe-en.wav` "hello, tell me two plus two" + `probe-it.wav` "ciao, dimmi due più due".
  Soft transcription sanity check; real WER is spike 022.
- Fit: `ollama ps` GPU/CPU split (THE fit signal — "100% GPU" vs "48%/52%") + `nvidia-smi` VRAM.
- Unload (`ollama stop`) between candidates; pulls timed and sized.

**Sequencing constraint:** Ollama probes need the GPU free — run AFTER 020's vLLM phases are
down.

## How to Run

```bash
cd /d/Aura
# one-time: synth the TTS probe clips
powershell -File .planning/spikes/021-survey-2026-shortlist/make-probe-clips.ps1
# all vLLM spike containers must be DOWN (GPU free)
docker compose -f .planning/spikes/020-vllm-sidecar-4gb-fit/compose.spike020.yaml -p spike020 down
go run ./.planning/spikes/021-survey-2026-shortlist 2>&1 | tee /d/tmp/spike-021-run.log
```

## What to Expect

Per candidate: pull size/time, vision color assertion, text-in-photo signal, audio
accepted/rejected (+ transcript when accepted), warm latency p50, `ollama ps` split,
VRAM peak. Final `[SHORTLIST]` block ranks the entrants for 022/023.

## Observability

Forensic log on stdout (tee'd to `/d/tmp/spike-021-run.log` — leashed-run convention),
ISO-timestamped `[CATEGORY]` lines, `[SUMMARY]` verdict, exit 0 = shortlist produced.

## Investigation Trail

- 2026-06-07: survey research done (web): Gemma 4 on Ollama has e2b/e4b tags with audio
  capability flag; qwen3-vl:2b=1.9GB/4b=3.3GB; MiniCPM-V 4.6 (1.3B) + MiniCPM-o 4.5 (8B omni)
  official openbmb namespace; Parakeet/Canary are the NeMo-engine ASR wave — IT-capable,
  fast, but not vLLM-servable (engine finding for the verdict).
- 2026-06-07: harness design — Ollama CLI audio acceptance probe + SAPI TTS known-text clips.

## Results

PENDING
