# Multimodal 9c — Vision/OCR + Voice (STT in, TTS out)

Implementation blueprint for Phase 13 Slice 9c. The PRD design ("Gemma 4 multimodal served
by vLLM on the 4GB GPU") was **killed by spike session 6** and replaced with **three small CPU
OpenAI-compat sidecars**, GPU entirely free. Binds to PRD amendment #59.

## Requirements (non-negotiable)

- **vLLM is OUT for 9c on this hardware.** RTX A2000 Laptop 4096 MiB + WSL VM 7 GiB RAM cannot
  host a 2B multimodal model with KV-cache headroom (spike 020). The 4GB GPU stays free; 9c
  runs on CPU.
- **Three sidecars, all OpenAI-compat, all CPU:**
  - **`aura-ocr-vl`** — vision/photo + image-OCR. `llama.cpp` server + **GLM-OCR** (decided 2026-06-07).
  - **`aura-stt`** — voice-in. `faster-whisper` (`hwdsl2/whisper-server`), large-v3-turbo int8.
  - **`aura-tts`** — voice-out. Kokoro-82M (`Kokoro-FastAPI`), voice **`if_sara`** (locked).
- **OGG/Opus is the bidirectional audio contract.** Telegram voice notes are OGG/Opus in BOTH
  directions; never transcode in app code.
- **Aura's voice = Kokoro `if_sara`** (female Italian) — operator on-device verdict, locked.
- **Permissive licenses only** (Aura is a product / DGX bundle): Kokoro Apache-2.0, GLM-OCR /
  PaddleOCR-VL open, faster-whisper MIT. F5-TTS / XTTS-v2 (non-commercial) are excluded.
- **markitdown stays** for `documents.go` (doc→markdown) — distinct from `aura-ocr-vl` (image→text).

## How to Build It

### Sidecar 1 — Vision/OCR (`aura-ocr-vl`, llama.cpp CPU)

**DECIDED (operator 2026-06-07): GLM-OCR.** PaddleOCR-VL-1.5 stays a documented latency-only
fallback (env override) if per-page speed ever becomes the bottleneck. Both IT OCR 7/7, 0 VRAM:

| | **GLM-OCR (DECIDED)** | PaddleOCR-VL-1.5 (fallback) |
|---|---|---|
| Source | `ggml-org/GLM-OCR-GGUF` (Q8 ~950MB + mmproj ~484MB) | `PaddlePaddle/PaddleOCR-VL-1.5-GGUF` (~936MB + mmproj ~882MB) |
| Output | **HTML table structure + accents** (`Mondovì`) | plain text, drops accent (`Mondovi`) |
| Warm latency (CPU) | ~2.95s/page | **~1.06s/page** (3× faster) |
| Best for | document/table fidelity, Slice-11 ingest | fast photo-to-text |

```bash
# CPU (proven). Launch via PowerShell, NOT Git-Bash (MSYS rewrites /models → C:\...).
docker run -d --name aura-ocr-vl -p 8082:8080 -v <models>:/models:ro \
  ghcr.io/ggml-org/llama.cpp:server \
  -m /models/GLM-OCR-Q8_0.gguf --mmproj /models/mmproj-GLM-OCR-Q8_0.gguf \
  --host 0.0.0.0 --port 8080 --temp 0 -c 8192
# probe: POST /v1/chat/completions with content [{type:image_url,image_url:{url:"data:image/png;base64,..."}},{type:text,text:"Leggi la tabella…"}]
```

### Sidecar 2 — STT voice-in (`aura-stt`, faster-whisper CPU)

```bash
docker run -d --name aura-stt -p 9000:9000 -v whisper-data:/var/lib/whisper \
  -e WHISPER_MODEL=large-v3-turbo -e WHISPER_COMPUTE_TYPE=int8 -e WHISPER_DEVICE=cpu \
  hwdsl2/whisper-server:latest
# voice.go: download Telegram voice (getFile) → POST the OGG/Opus bytes straight to
# /v1/audio/transcriptions  (-F file=@voice.ogg -F model=whisper-1 -F language=it OR omit for auto)
```
- Ingests **OGG/Opus directly** (bundled PyAV/ffmpeg). ~0.7× realtime on CPU. Auto-detects Italian.

### Sidecar 3 — TTS voice-out (`aura-tts`, Kokoro CPU) — NEW

```bash
docker run -d --name aura-tts -p 8880:8880 ghcr.io/remsky/kokoro-fastapi-cpu:latest
# tts.go: POST /v1/audio/speech {model:"kokoro", input:<reply>, voice:"if_sara", response_format:"opus"}
#         → the returned bytes are a native Telegram voice note → sendVoice (no transcode)
```
- Italian voices: `if_sara` (female, Aura's voice), `im_nicola` (male). ~0.3× realtime CPU. 24kHz.

### Telegram audio I/O (Bot API)

```bash
# IN: getUpdates → message.voice.file_id → getFile → GET /file/bot<TOKEN>/<file_path>  (OGG/Opus)
# OUT: POST /sendVoice -F chat_id=<id> -F voice=@reply.opus -F caption="<ASCII only>"
# Ground truth = the sendVoice RESPONSE (msg.voice.{duration,mime_type,file_id}); bot-sent
# messages never appear in getUpdates.
```

## What to Avoid

- **Do NOT try vLLM for 9c on a 4GB card.** Weights 2.95GiB + ~0.8GiB CUDA overhead → 0.09GiB
  for KV vs 0.44 needed; dual-residency (STT+vision) impossible; WSL 7GiB RAM thrashes the load.
- **Do NOT feed OGG/Opus to whisper.cpp.** Its miniaudio decoder fails on Opus
  (`failed to read audio data`) — it would force an ffmpeg pre-step. faster-whisper decodes inline.
- **Do NOT launch the docker sidecars from Git-Bash.** MSYS rewrites `/models/...` →
  `C:\Program Files\Git\models\...`. `MSYS_NO_PATHCONV=1` breaks docker resolution itself. Use
  **PowerShell**. (In production compose this is moot.)
- **Do NOT pin `vllm/vllm-openai:latest` or any `cuXY` tag above the host driver ceiling.** Driver
  573.91 caps at CUDA 12.x; `:latest` (cu13) and `:cu129` both get refused by
  nvidia-container-cli. If GPU serving is ever needed, pin `cuXY` ≤ driver. CPU sidesteps it.
- **WSL GPU gotcha** (only if forcing GPU): the CUDA image's `/usr/local/cuda*/compat/libcuda.so*`
  shadows the WSL-injected driver lib → `torch.cuda.is_available()==False` while nvidia-smi works.
  Fix: `rm` the compat stub + `ldconfig` in the entrypoint before exec. (CPU avoids this entirely.)
- **Do NOT put emoji/parens/em-dash in a `sendVoice`/`sendPhoto` caption** — HTTP 400. ASCII captions.
- **Italian-number OCR assertions must normalize digit grouping** before substring match: models
  emit `56.000`/`56 000`, not `56000` (false-negative trap — bit both minimax-m3 and the harness).
- **Voice cloning is descoped** — the Kokoro preset is enough. If ever resumed: Chatterbox-multilingual
  (MIT) via `travisvn/chatterbox-tts-api:latest-cpu` (Docker Hub), upload ref via `POST /v1/voices`.
  F5-TTS / XTTS-v2 are non-commercial → excluded.

## Constraints

- Hardware measured on: RTX A2000 Laptop **4096 MiB**, 32 GB host RAM, **WSL VM 7 GiB**, driver 573.91.
- Latencies are **CPU** numbers (GPU free by design): OCR-VL ~1-3s/page, STT ~0.7× realtime,
  TTS ~0.3× realtime. A 1-min voice note ≈ 40s STT on CPU — acceptable behind a "🎤" reaction;
  drop STT to `small` for ~5× faster at higher WER if needed.
- RAM: ~1.4 (OCR-VL) + ~1.5 (STT) + ~0.5 (TTS) ≈ **3.4 GB**, 0 VRAM.
- Images: `ghcr.io/ggml-org/llama.cpp:server` (188MB CPU), `hwdsl2/whisper-server` (~190MB CPU),
  `ghcr.io/remsky/kokoro-fastapi-cpu` (torch-heavy). All pull the model on first run.
- Cloud vision fallback (optional): `minimax/minimax-m3` via OpenRouter — text+image+video, 1M ctx,
  $0.30/$1.20 per M (~$0.0005/call), Italian OCR ~100% with accents. NO audio.

## Origin

Synthesized from spikes: 020 (vLLM 4GB INVALIDATED), 021 (2026 survey SUPERSEDED), 024
(minimax-m3 cloud vision VALIDATED), 025 (PaddleOCR-VL VALIDATED), 026 (GLM-OCR VALIDATED), 027
(faster-whisper STT VALIDATED), 028 (Kokoro TTS VALIDATED, `if_sara` locked), 029 (cloning DESCOPED),
022 (FLEURS WER harvester, deferred).
Source files in: sources/020-… through sources/029-… (+ `harvest-fleurs.sh` for deferred WER).
Binds to PRD amendment #59.
