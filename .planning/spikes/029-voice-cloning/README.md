---
spike: 029
name: voice-cloning
type: standard
validates: "Given a permissive-license zero-shot voice-cloning TTS, when cloning a reference voice into Italian, then Aura could speak in an arbitrary cloned voice — DESCOPED before execution (operator chose the Kokoro preset voice)"
verdict: DESCOPED
related: [028-kokoro-tts]
tags: [multimodal, tts, voice-cloning, descoped, phase-13]
---

# Spike 029: Voice Cloning (Descoped)

## What This Would Have Validated

Operator directive "aggiungiamo anche il clone della voce da audio" — clone an arbitrary voice
(the operator's "Isa" avatar clip) into Italian so Aura could speak in a cloned voice, distinct
from Kokoro's fixed preset voices (028).

## Outcome: DESCOPED before execution

Immediately after hearing the Kokoro voices (028), the operator locked `if_sara` as Aura's
voice ("la voce femminile di kokoro è perfetta chiudiamo qui") — so **cloning is not needed**.
The capability was descoped on the spot; the chatterbox container was pulling and never ran a
clone. Documented here so the validated approach is on the shelf if cloning is ever wanted.

## Research (kept for the future)

Permissive-license (commercial-OK, required for the Aura/DGX product) zero-shot cloning, 2026:

| Model | License | Italian | Serving |
|-------|---------|---------|---------|
| **Chatterbox-multilingual** (Resemble) | **MIT** (code+weights) | ✅ (22 langs via `chatterbox-tts-api`) | `travisvn/chatterbox-tts-api:latest-cpu` (Docker Hub, 1.2GB), OpenAI-compat `/v1/audio/speech` :4123, clone via `POST /v1/voices -F voice_file=@ref.wav -F name=`; ~10s ref |
| **CosyVoice2-0.5B** (Alibaba) | **Apache-2.0** | ✅ (9 langs) | repo FastAPI / community dockers |
| **Fish Speech / OpenAudio** | Apache-2.0 | partial | repo server |
| **OmniVoice** (k2-fsa) | Apache-2.0 | ✅ (600+ langs), 40× RT | newer |
| ~~F5-TTS / XTTS-v2~~ | **non-commercial** | ✅ | **excluded for the product** |

Chosen-if-resumed: **Chatterbox-multilingual via `travisvn/chatterbox-tts-api`** (MIT, OpenAI-compat
sidecar mirroring 027/028, Italian, ~10s reference). A 15s mono 24kHz reference was already cut
from the operator's clip at `D:\tmp\spike-029\isa-ref.wav` — drop-in for a future run:
`curl -X POST :18094/v1/voices -F voice_file=@isa-ref.wav -F name=isa`, then
`POST :18094/v1/audio/speech {input, voice:"isa", language:"it"}`.

## Results

DESCOPED — not executed. Aura's TTS = Kokoro `if_sara` (spike 028). No cloning in 9c scope.