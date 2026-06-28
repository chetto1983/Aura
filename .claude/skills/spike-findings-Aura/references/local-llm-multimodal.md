# local llm multimodal

Slice-13 local-LLM fallback engine + the unified-multimodal question for Phase-13/9c.
Synthesizes the Gemma-4 MTP-on-GPU lane (048/049/050) and the adjacent cloud-multimodal
probe (030). Engine = **llama.cpp** (vLLM is dead on this hardware — spike 020).

## Requirements

Hard constraints a future build MUST honor (from MANIFEST Session-6 PIVOT lines 117-126,
binding for `/gsd-plan-phase 13` 9c, plus the Session-13 spike-derived non-negotiables).

- **Engine = llama.cpp, NEVER vLLM for 9c/local-LLM on 4 GB.** vLLM is INVALIDATED on
  this hardware (spike 020: weights 2.95 GiB + 0.8 GiB CUDA overhead → 0.09 GiB KV vs
  0.44 needed; WSL 7 GiB RAM starves the load; dual-residency impossible — operator
  killed the path). llama.cpp succeeds where vLLM died.
- **CUDA image must carry the June-2026 Gemma-4 MTP merges; pin it, never `latest`.**
  Gemma-4 MTP merged into llama.cpp **2026-06-07** (PR #23398); compact E2B/E4B
  assistant support **2026-06-08** (PR #24282). Builds older than that **cannot load
  arch `gemma4-assistant`**. Proven image: `ghcr.io/ggml-org/llama.cpp:server-cuda`
  created **2026-06-11T07:16Z**, digest
  `sha256:e502860c8aa147e74e7cf42568fa2a8407c578dd291c1b231f698a55dd83fef6`
  (`system_fingerprint: b9592-ac4cddeb0`). Per Session-6: pin a `cuXY` image matching
  the host driver ceiling.
- **The committed Phase-13/9c local-default stack is UNCHANGED by these spikes.**
  It stays three OpenAI-compat CPU sidecars: **OCR-VL = GLM-OCR** (DECISO operatore
  2026-06-07; PaddleOCR-VL = latency-only fallback) on llama.cpp `/v1/chat/completions`;
  **STT = faster-whisper** (`hwdsl2/whisper-server`, `/v1/audio/transcriptions`);
  **TTS = Kokoro-FastAPI** voice `if_sara` (`/v1/audio/speech`). OGG/Opus is the channel
  audio contract both ways. The unified Gemma-4 GPU sidecar (050) is a Slice-13 local-LLM
  *addition / optional collapse*, NOT a replacement; it **does NOT retire GLM-OCR for
  document-grade OCR** (Gemma-4 vision is 3/6 terms vs GLM-OCR's 7/7).
- **Best model that FITS 4 GB = gemma-4 E2B QAT UD-Q4_K_XL + its mtp draft, full offload.**
  E2B QAT UD-Q4_K_XL = 2.44 GiB weights, mtp draft = 56.5 MiB. **E4B Q4 (4.21 GiB) is
  over-ceiling** — partial-offload only, out of scope for the 4 GB fit. Do not attempt
  full E4B offload on the A2000.
- **MTP ships with `--spec-type draft-mtp --spec-draft-n-max 2`, never n-max=4.**
  n-max=2 never loses and wins big on English/code; n-max=4 is counterproductive
  (0.75×). Capacity-plan on the **~64 tok/s Italian baseline**, NOT unsloth's 1.4-2.2×
  marketing (Italian draft acceptance is ~32-38% → ~1.0×).
- **Vision lane has two mandatory serving knobs**: `chat_template_kwargs:{enable_thinking:false}`
  (else runaway `reasoning_content` → empty content) and `repeat_penalty≈1.3` /
  `repeat_last_n:128` (else greedy repetition loop on dense text). The **mmproj must be
  BF16 and stay on CPU** (`--no-mmproj-offload`) to preserve the 4 GB fit. Gemma-4 is a
  thinking model — any client must budget `max_tokens` past the reasoning phase.
- **Cloud-vision tier: minimax/minimax-m3 stays the committed default** (amendment #59/5b).
  mimo-v2.5 is a config-only alternative via `MULTIMODAL_FALLBACK_MODEL` /
  `AURA_VISION_CLOUD=true` — **no code change**. Standalone cloud audio (OpenRouter
  `input_audio`) is dead — voice-in stays local faster-whisper.

## How to Build It

These spikes are **probes, not shipped code** — none landed in a package. They prove the
serving recipe for the future Slice-13 local-LLM-fallback sidecar.

### Model assets (unsloth/gemma-4-E2B-it-qat-GGUF)

Staged to `D:\tmp\spike-048-models` in the spikes:
- `gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf` — 2,620,368,960 B (~2.44 GiB), the text weights.
- `mtp-gemma-4-E2B-it.gguf` — 59,234,176 B (~56.5 MiB), the MTP draft head wired via
  `--model-draft` (Gemma-4 ships the MTP head as a separate small GGUF in the same repo).
- `mmproj-BF16.gguf` — 986,833,728 B (~941 MiB), the **single** vision+audio projector
  (1411 tensors, both encoders). **BF16 only** — F16/Q8_0 cause repetitions
  (HF unsloth/gemma-4-E2B-it-GGUF discussion #1). Download gotcha: the mmproj truncated
  at 767 MiB (curl exit 255); `curl -C -` resume to the exact 986,833,728 B fixed it.

### Serve — text-only GPU lane (spike 048, VALIDATED)

PowerShell ONLY — Git-Bash/MSYS mangles `/models` → `C:/Program Files/Git/models/...`.

```powershell
docker run -d --name spike048 --gpus all -p 8095:8080 `
  -v D:\tmp\spike-048-models:/models:ro `
  ghcr.io/ggml-org/llama.cpp:server-cuda `
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
  --model-draft /models/mtp-gemma-4-E2B-it.gguf `
  --spec-type draft-mtp --spec-draft-n-max 2 `
  -ngl 99 --spec-draft-ngl 99 -c 4096 --temp 0 --host 0.0.0.0 --port 8080
```

- `-ngl 99 --spec-draft-ngl 99` = full GPU offload of both weights and draft head.
- This build defaults to `-fit on` ("fitting params to device memory"): without explicit
  `-ngl` it auto-redistributes layers to CPU instead of OOMing — a graceful 4 GB fallback.
  Passing `-ngl 99` overrides it (`n_gpu_layers already set by user to 99, abort`).
- Full **128K context fits in 4 GB** (`-c 131072` → 3451 MiB total). KV growth is
  sub-linear via Gemma's sliding-window attention (5:1 local:global): 32K→128K (4×) added
  only ~600 MiB. The "KV grows linearly" assumption is WRONG for this arch. `-c 4096` →
  3705 MiB, `-c 32768` → 2849 MiB.
- Boot is fast: 2.5 GiB of weights healthy in ~17-25 s; harness uses a 240 s `/health`
  deadline.

### Client contract (OpenAI-compat `/v1/chat/completions`)

Gemma-4 is a **thinking model**: `reasoning_content` consumes `max_tokens` before any
visible `content`. First 048 run failed with empty content at `max_tokens:160`
(`finish_reason:length`, ~1.6k reasoning chars). Raise budget OR disable thinking.

```go
// text lane — budget past the thinking phase
payload := map[string]any{
    "model":       "gemma-4-e2b",
    "messages":    []map[string]string{{"role": "user", "content": prompt}},
    "max_tokens":  1024,   // MUST leave headroom past reasoning_content
    "temperature": 0,
}
```

Native `/completion` exposes `timings.predicted_per_second` plus `draft_n` /
`draft_n_accepted` — the ground-truth speculative-decoding counters. Use these (not chat
token counts) for any speed/acceptance measurement.

### Serve — unified multimodal sidecar (spike 050, VALIDATED)

Add the BF16 projector on CPU; everything else identical. One 4 GB GPU does text + image
+ audio + frame-sequence video, MTP still active.

```powershell
docker run -d --name spike050 --gpus all -p 8095:8080 `
  -v D:\tmp\spike-048-models:/models:ro `
  ghcr.io/ggml-org/llama.cpp:server-cuda `
  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
  --mmproj /models/mmproj-BF16.gguf --no-mmproj-offload `
  --model-draft /models/mtp-gemma-4-E2B-it.gguf `
  --spec-type draft-mtp --spec-draft-n-max 2 `
  -ngl 99 --spec-draft-ngl 99 -c 8192 --temp 0 --host 0.0.0.0 --port 8080
```

Vision/OCR request — **both knobs are non-negotiable**:

```go
payload := map[string]any{
    "model":                "gemma-4-e2b",
    "messages":             []map[string]any{{"role": "user", "content": content}},
    "max_tokens":           768,
    "temperature":          0,
    "repeat_penalty":       1.3,    // else greedy repetition ("Modbus RTU RS 485" x90)
    "repeat_last_n":        128,
    "chat_template_kwargs": map[string]any{"enable_thinking": false}, // else empty content
}
// content parts:
//   image: {"type":"image_url","image_url":{"url":"data:image/png;base64,"+b64}}
//   audio: {"type":"input_audio","input_audio":{"data":b64,"format":"wav"}}   // cap 30 s
//   video: multiple image_url parts in ONE turn (no native decoder — sample frames client-side)
```

- Audio is the **standout**: verbatim Italian incl. proper nouns (Torino, Cuneo, Novara),
  142 tok/s, draft 19/22 — faster than the dedicated faster-whisper path. Audio needs
  neither vision knob but tolerates both. **Audio capped at 30 s** (Gemma USM Conformer).
- Audio asset prep gotcha (Kokoro): Kokoro emits a streaming WAV with placeholder
  `nframes = 2^31-1` (reports dur 89478 s) — rewrite a canonical 44-byte RIFF/WAVE header
  over the PCM (real 7.0 s, 24 kHz mono) or Gemma's audio decoder chokes.
- Video = client-side frame sampling: llama.cpp mtmd has **no native video decoder**.
  Real video ingest needs an ffmpeg frame extractor upstream. Two-frame turn is understood
  temporally ("Il primo fotogramma… il secondo…").
- Image prompt-processing is the latency cost (~6 s for 297 tokens on the CPU projector);
  generation stays fast. Offloading the projector to GPU (+1.2 GiB) would speed encode but
  **breaks the 4 GB fit** — CPU encode is the right trade.

### MTP serving decision (spike 049, PARTIAL)

Ship `--spec-draft-n-max 2`. It never loses at n=2 and wins big on EN/code. Sweep proved
n=2 is the optimum; n=4 is harmful. VRAM cost of MTP ≈ 56.5 MiB draft + tiny KV (negligible
— the 048 fit holds with MTP active).

### Cloud-vision tier (spike 030 — config-only, no code)

`xiaomi/mimo-v2.5` on OpenRouter is a cheaper alternative to the committed
`minimax/minimax-m3` for the `AURA_VISION_CLOUD=true` tier. The `MULTIMODAL_FALLBACK_MODEL`
selector already abstracts which cloud model serves vision → adopting mimo is a config
change. Live content-part contract (learned 2026-06-08):
- **Image:** `image_url` data URL, only `bmp/gif/png/jpeg/webp` (a video mime → HTTP 400
  "invalid image format").
- **Video:** `video_url` data URL (`data:video/mp4;base64,…`) — the ONLY video shape;
  `image_url` rejects it. Works end-to-end incl. in-video audio transcription, timestamped.
- **Audio (standalone):** `input_audio {data, format: mp3|wav}` — accepted without error
  but does **not** transcribe (146 s hang, empty reply, no usage block). Dead leg.
- **Reasoning-budget gotcha:** mimo-v2.5 is a reasoning model — small `max_tokens` →
  empty `content`. Set `max_tokens ≥ ~3000` for video; add `reasoning.exclude` if you only
  want the answer.

## What to Avoid

- **vLLM on 4 GB (spike 020 INVALIDATED).** Do not re-litigate. KV-cache arithmetic wall +
  WSL RAM starvation + impossible dual-residency. The whole 9c "served with vLLM" premise
  is DEAD. Use llama.cpp.
- **`server-cuda:latest` or any image built before 2026-06-08.** Cannot load arch
  `gemma4-assistant`; the `--spec-type … draft-mtp` flag surface won't exist. Verify with
  `--help` (look for `draft-mtp`, `--spec-draft-n-max`, `--spec-draft-model`,
  `--spec-draft-ngl`) before pulling models.
- **E4B Q4 full-offload on 4 GB.** 4.21 GiB weights alone exceed the card before any
  KV/CUDA context. 8 GB is the floor for E4B / GPU-mmproj / heavy concurrency — but NOT
  for E2B context length (full 128K fits in 4 GB).
- **`--spec-draft-n-max 4` (or higher).** Net-negative (0.75× vs baseline): every rejected
  draft token wastes verify compute, and low Italian acceptance (~32-38%) makes over-
  drafting counterproductive. n=2 only.
- **Pricing/capacity-planning on unsloth's 1.4-2.2× MTP headline.** Real global gain on
  this hardware is 1.20× p50 (EN 1.36-1.49× @71-73% accept; IT ~1.0× @32-38%). For an
  Italian-primary assistant plan on the ~64 tok/s baseline.
- **Byte-comparing greedy MTP-on vs MTP-off outputs.** Speculative verification batches
  change CUDA numerics enough to flip near-tie argmax: 5/6 greedy outputs DIVERGE at temp 0
  (common prefixes 5-578 chars). MTP is lossless at the distribution level, NOT bit-exact.
  Any eval harness comparing configs must compare **quality, not bytes**.
- **Vision lane without `enable_thinking:false`.** Gemma-4 E2B runs away in
  `reasoning_content` on dense OCR (7029 reasoning chars, `finish=length`, empty content);
  raising max_tokens just produces *more* thinking.
- **Vision lane without `repeat_penalty`.** With thinking off, the tiny model falls into a
  greedy repetition loop at temp 0 ("Modbus RTU RS 485" ×90). Both knobs together = clean
  structured OCR, `finish=stop`.
- **mmproj F16 or Q8_0.** Cause repetitions — BF16 is the recommended/only good quant.
- **mmproj on GPU.** +1.2 GiB breaks the 4 GB fit. Keep `--no-mmproj-offload` (CPU encode).
- **Retiring GLM-OCR with the unified Gemma sidecar.** Gemma-4 vision is "glance at an
  image" quality (3/6 ground-truth terms, garbles detail/dimensions) — well below the
  dedicated OCR sidecars (025 PaddleOCR-VL / 026 GLM-OCR both 7/7). Keep GLM-OCR for
  document-grade OCR.
- **Cloud standalone audio via OpenRouter** (mimo-v2.5 or otherwise). The `input_audio`
  leg hangs and returns empty. Voice-in stays local faster-whisper (spike 027). Audio-cloud
  is descoped for Phase-13.
- **Launching docker from Git-Bash for these models.** MSYS rewrites `/models/...` →
  `C:/Program Files/Git/models/...` (the `-c` sweep died on exactly this — that error is
  the artifact, not a VRAM wall). Use PowerShell. (Windows-Go also resolves `/d/tmp` to
  `\d\tmp` not `D:\tmp` — hardcode `D:\...` in source paths.)

## Constraints

- **Hardware:** RTX A2000 Laptop, **4096 MiB** total VRAM, driver 573.91, 32 GB RAM,
  Windows desktop holding ~1.16-1.2 GiB residency (dual-residency is the real constraint).
- **VRAM measured peaks (desktop included):** text lane 3705/4096 MiB (`-c 4096`); full
  128K context 3451 MiB; unified multimodal (mmproj on CPU) **3392/4096 MiB**. No OOM, no
  partial-offload fallback needed.
- **Throughput:** baseline gen p50 **63.5** / p95 65.3 tok/s (MTP off). MTP n=2 p50 76.5 /
  p95 87.4 (1.20×). Per-prompt at n=2: en-reason 87.4 tok/s 1.49× @73%, en-code 86.3 1.36×
  @71%, it-explain 64.1 1.02% @38%, it-story 65.8 0.99× @32%. Audio 142 tok/s; vision OCR
  61 tok/s (10.6 s incl. CPU encode); video 10.9 s / 2 frames.
- **Model sizes (bytes):** E2B QAT UD-Q4_K_XL = 2,620,368,960 (~2.44 GiB);
  mtp-gemma-4-E2B-it = 59,234,176 (~56.5 MiB); mmproj-BF16 = 986,833,728 (~941 MiB);
  E4B QAT UD-Q4_K_XL = 4,215,693,760 (~4.21 GiB, over ceiling).
- **Image pin:** `ghcr.io/ggml-org/llama.cpp:server-cuda` @
  `sha256:e502860c8aa147e74e7cf42568fa2a8407c578dd291c1b231f698a55dd83fef6`, created
  2026-06-11T07:16Z, `system_fingerprint: b9592-ac4cddeb0`.
- **MTP merge timeline:** PR #23398 (MTP, 2026-06-07), PR #24282 (E2B/E4B assistant,
  2026-06-08). Audio: PR #21421 (USM Conformer in mtmd). Older builds reject
  `gemma4-assistant`.
- **Audio cap = 30 s** (Gemma USM Conformer). No native video decoder in llama.cpp mtmd.
- **Ports (spike-local):** llama.cpp host `8095`→container `8080`; Kokoro `8096`→`8880`.
- **Cloud mimo-v2.5:** id `xiaomi/mimo-v2.5` (`-pro`/`-flash` siblings are **text-only**);
  context 1,048,576; pricing **$0.14/M prompt · $0.28/M completion** (cheaper than
  minimax-m3); endpoint `https://openrouter.ai/api/v1/chat/completions`; total probe cost
  < $0.01 (vision ~$0.0002/call, full-video ~$0.005/call @27k input tokens). Env:
  `OPENROUTER_API_KEY`. minimax-m3 committed default (amendment #59/5b);
  `AURA_VISION_CLOUD` / `MULTIMODAL_FALLBACK_MODEL` select the cloud model.
- **License/serving caveats (Session-6):** WSL libcuda-compat-stub shadow needs the
  `rm + ldconfig` entrypoint fix for GPU; CPU serving sidesteps GPU caveats entirely and is
  fast enough for the dedicated 0.9B OCR-VL sidecar.

## Origin

Synthesized from spikes: **048, 049, 050, 030**. Source files in:
`sources/048-gemma4-mtp-gpu-fit/`, `sources/049-mtp-speedup-headtohead/`,
`sources/050-gemma4-mtp-multimodal/`, `sources/030-openrouter-mimo-v2.5-multimodal/`
(each = README.md + main.go probe harness). Authoritative requirements: MANIFEST
Session-6 PIVOT (binding for `/gsd-plan-phase 13` 9c) + Session-13 narrative.

Verdicts:
- **048 gemma4-mtp-gpu-fit:** VALIDATED (E2B+MTP full-offload fits 4 GB, peak 3705 MiB;
  full 128K context fits; thinking-model trap).
- **049 mtp-speedup-headtohead:** PARTIAL (n=2 optimal 1.20× global; EN 1.36-1.49×, IT
  ~1.0×; n=4 counterproductive 0.75×; greedy not bit-exact across spec configs).
- **050 gemma4-mtp-multimodal:** VALIDATED (one 4 GB GPU does text+image+audio+frame-seq
  video, peak 3392 MiB; audio excellent, vision below GLM-OCR; two mandatory vision knobs).
- **030 openrouter-mimo-v2.5-multimodal:** VISION + VIDEO VALIDATED, STANDALONE AUDIO NOT
  WORKING via OpenRouter (config-only cloud-vision alternative; not a Phase-13 blocker).
- Context dead-end carried forward: **020 vllm-sidecar-4gb-fit** INVALIDATED (vLLM is OUT).
