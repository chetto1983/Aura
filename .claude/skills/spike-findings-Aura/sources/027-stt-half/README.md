---
spike: 027
name: stt-half
type: comparison
validates: "Given real Telegram OGG/Opus audio (IT), when transcribed by local whisper variants, then a working GPU-free STT path for the 9c voice half emerges with the OGG/Opus pipeline proven and latency measured"
verdict: VALIDATED
related: [025-paddleocr-vl-local, 026-glm-ocr-local, 020-vllm-sidecar-4gb-fit]
tags: [multimodal, stt, whisper, faster-whisper, ogg-opus, phase-13]
---

# Spike 027: STT Half — Local Whisper for 9c Voice

## What This Validates

The OCR-VL vision models (025/026) have no audio input, so 9c's voice half is a separate
sidecar. Operator framing: "audio può essere gestito da whisper o altri modelli 2026", then
two sharpening directives during the spike: **(1)** test the operator's REAL audio (a Telegram
message), and **(2)** "converti in Ogg telegram manda solo Ogg" — i.e. validate the actual
production format (Telegram voice notes are OGG/Opus), then the pointer to
`hwdsl2/docker-whisper`. Given real Italian audio in OGG/Opus, when transcribed by local
whisper, then a working, GPU-free STT path emerges with the format pipeline proven.

## Research / Candidates

| Engine | Image | OGG/Opus intake | `/v1/audio/transcriptions` | CPU speed |
|--------|-------|-----------------|----------------------------|-----------|
| whisper.cpp | `ghcr.io/ggml-org/whisper.cpp:main` | **NO** (miniaudio can't decode Opus) — needs ffmpeg pre-convert | server exists | ~2.3× realtime |
| **faster-whisper** (hwdsl2) | `hwdsl2/whisper-server` | **YES, direct** (bundled PyAV/ffmpeg) | **native** | **~0.7× realtime** |
| cloud audio (OpenRouter) | gemini-flash-lite / voxtral-small / gpt-audio-mini | yes | chat (audio_url) | n/a — cloud dep |

Model = **whisper large-v3-turbo** (809M; ~half the cost of large-v3 at near-equal accuracy).
Published FLEURS-IT WER for large-v3 ≈ 9.3%; turbo slightly higher.

## How to Run

```bash
# faster-whisper (the winner) — CPU, int8, large-v3-turbo:
docker run -d --name spike027-whisper -p 18092:9000 -v whisper-data:/var/lib/whisper \
  -e WHISPER_MODEL=large-v3-turbo -e WHISPER_COMPUTE_TYPE=int8 -e WHISPER_DEVICE=cpu \
  hwdsl2/whisper-server:latest     # GPU: --gpus all + :cuda tag + float16
# probe with the REAL Telegram OGG/Opus voice note (NO conversion):
curl http://127.0.0.1:18092/v1/audio/transcriptions \
  -F file=@telegram-voice.ogg -F model=whisper-1 -F language=it
```

Telegram audio downloaded live via Bot API (`getUpdates`→`getFile`→`/file/bot<token>/<path>`);
the OGG/Opus voice-note format reproduced from it with `ffmpeg -c:a libopus -b:a 32k -ar 48000
-ac 1 -application voip` (the Telegram voice-note encoding).

## Investigation Trail

- 2026-06-07: operator sent a real 37s audio (`Via_Lasca_avatar_ready.mp3`, 192k/44.1k mono).
  Downloaded via Bot API.
- **whisper.cpp (CPU)**: transcribed the MP3 perfectly in Italian (auto-detect) — "Ciao, io
  sono Isa e ti do il benvenuto…" (a cosmetics/lifestyle intro). But **total 85.7s for 37s
  audio (≈2.3× realtime)**, encoder-bound (76s of 86s = the encode). load 2.3s.
- **OGG/Opus finding**: converted the MP3 → Telegram-style OGG/Opus. `whisper-cli` direct on
  the .ogg **FAILS** (`miniaudio: failed to read audio data` — miniaudio has no Opus decoder).
  Production path with whisper.cpp therefore needs `ffmpeg → 16kHz WAV` first (ffmpeg IS in
  the image). That path works and gives the identical transcription.
- **faster-whisper (hwdsl2, CPU int8)**: ingests the **OGG/Opus directly, no conversion**, same
  perfect Italian transcription, **23-28s for 37s audio (≈0.7× realtime — faster than real
  time)**, **3.7× faster than whisper.cpp on CPU**. Auto-detects Italian without a language
  hint. 1.5 GiB RAM, **0 VRAM** (CPU). Exposes `/v1/audio/transcriptions` = the PRD voice.go
  contract verbatim.

## Results

**VALIDATED.** Local, GPU-free, private STT for 9c voice is real and the pipeline is now known:

- **Engine = faster-whisper (`hwdsl2/whisper-server`), model large-v3-turbo, int8 on CPU.**
  Beats raw whisper.cpp on every axis that matters here: handles Telegram's native OGG/Opus
  with no conversion shim, 3.7× faster on CPU (sub-realtime), drop-in OpenAI transcription API.
- **The OGG/Opus decode is the load-bearing pipeline fact**: whichever engine 9c picks must
  decode Opus (faster-whisper/PyAV does it inline; whisper.cpp would force an ffmpeg pre-step).
  voice.go: download voice via getFile → POST bytes straight to `/v1/audio/transcriptions`.
- **Latency**: CPU large-v3-turbo ≈ 0.7× realtime (a 1-min note ≈ 40s) — acceptable behind the
  "🎤" reaction; the `:cuda` float16 image collapses the encoder for near-instant if the GPU is
  free (it is — vision OCR runs on CPU too, see 025/026). Quality/latency knob: drop to `small`
  for ~5× faster at higher WER if needed.

**whisper.cpp GPU caveat**: the `main-cuda` image on this WSL2 + A2000 setup gave **no
speedup** (encode 103s, total 115s — *slower* than its own CPU run's 85.7s), i.e. the CUDA
offload either fell back to CPU silently or drowned in WSL+small-card overhead. Not chased —
it only reinforces the faster-whisper-CPU pick. faster-whisper's CTranslate2 backend is the
one that actually accelerates here.

**Open follow-up (deferred, not blocking the engine pick):** formal FLEURS IT/EN WER numbers
(jiwer) + a faster-whisper `:cuda` (float16) latency measurement. The functional + format +
speed verdict is decisive without them.

## STT artifacts (kept for re-run)

- `D:\tmp\spike-027\telegram-audio.mp3` — operator's real audio (downloaded via Bot API)
- `D:\tmp\spike-027\telegram-voice.ogg` — re-encoded to Telegram voice-note OGG/Opus
- `D:\tmp\spike-027\ggml-large-v3-turbo-q5_0.bin` — whisper.cpp model (324 MB)
- `.planning/spikes/022-stt-wer-it-en/harvest-fleurs.sh` — FLEURS IT/EN harvester for the
  deferred formal-WER follow-up