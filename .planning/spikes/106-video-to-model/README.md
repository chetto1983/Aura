---
spike: 106
idea: studio-media-editing
name: video-to-model
type: standard
validates: "Given an 8 s H.264 clip with burned-in per-frame timestamps, when it is sent as native video to llama.cpp b10951 and to an OpenRouter video model, then each backend's capability key and wire part are measured rather than guessed, and the answer proves the frames were decoded"
verdict: VALIDATED
related: [105]
tags: [llm, video, llama.cpp, openrouter, multimodal, capabilities, wire-format]
---

# Spike 106: A video that actually reaches the model

## What This Validates

The plan `docs/superpowers/plans/2026-09-19-video-to-model.md` wants a video attachment to travel
as native bytes "when, and only when, the selected LLM declares video input". Two things were
unknown and could not be guessed: **which `/props` key llama.cpp uses to declare video**, and
**whether `input_video` on `server-b10951` decodes a real clip or answers "video input is not
supported"**. The OpenRouter half was measured by the controller in the same session and is
folded in here so the two backends are comparable: same file, same question.

## How to Run

```bash
docker run -d --name aura-video-probe -p 18099:8080 -v aura-video-probe-cache:/root/.cache \
  ghcr.io/ggml-org/llama.cpp:server-b10951 \
  -hf ggml-org/Qwen3-VL-2B-Instruct-GGUF --host 0.0.0.0 --port 8080 -c 16384
until curl -sf http://localhost:18099/health >/dev/null; do sleep 5; done   # ~11 min, 2.1 GB
curl -s http://localhost:18099/props | python3 -c \
  'import json,sys; print(json.dumps(json.load(sys.stdin)["modalities"]))'
bash probe.sh ../105-studio-media-editing/media/clip-h264-silent.mp4       # ~12 min on CPU
docker rm -f aura-video-probe          # the named cache volume is kept on purpose
```

`probe.sh` builds the body with `python3` rather than `jq`: Git Bash on this host has no `jq`, and
a 3.7 MB base64 payload belongs in a file, not in a shell variable. Override the port with
`PROBE_PORT`. Artifacts land in `out/` (gitignored).

The clip is spike 105's `media/clip-h264-silent.mp4`, re-probed here with the probe container's
own `ffprobe` 6.1.1: **1280x720, H.264, 24 fps, 192 frames, 8.000 s, 2 799 062 B, no audio
track**, with a `hh:mm:ss.mmm fN` stamp burned into every frame. OpenRouter was given the same
file.

## Investigation Trail

1. **The model exists.** `ggml-org/Qwen3-VL-2B-Instruct-GGUF` answers on the HF API, so the gemma
   fallback named in the brief was not needed for step 1.
2. **`-hf` pulls the projector by itself.** Two blobs downloaded in parallel into the named
   volume: `Qwen3-VL-2B-Instruct-Q8_0.gguf` and `mmproj-Qwen3-VL-2B-Instruct-Q8_0.gguf`. Note the
   quant: `-hf` without a `:quant` suffix resolved to **Q8_0**, not Q4_K_M. Total **2.1 GB in
   10 min 56 s** (11:53:47 to 12:04:43 UTC, ~190 MB/min); the wait is logged minute by minute in
   `out/download-timeline.log`. Server: CPU only, `n_threads = 6`, `n_ctx_slot = 16384`, 4 slots,
   `build_info b10951-093a2f86c`.
3. **The load warns about a knob this probe did not exercise:** *"Qwen-VL models require at
   minimum 1024 image tokens to function correctly on grounding tasks ... try adding
   `--image-min-tokens 1024`"* (llama.cpp issue 16842). The probe ran without it and still read
   the stamps.
4. **`/props` answers `video` literally** — the whole object, verbatim:
   ```json
   {"vision": true, "video": true, "audio": false}
   ```
   Beside it, `"model_ftype": "Q8_0"` and a `media_marker` placeholder token. `vision` and `video`
   are separate booleans, so an image-only model stays distinguishable from a video one.
5. **`input_video` is accepted, with no `data:` prefix.** The part that worked is
   `{"type":"input_video","input_video":{"data":"<base64>"}}` — raw base64, *not* a data URI
   (OpenRouter's shape is the opposite). The error string the binary carries, *"video input is not
   supported"*, never appeared; `docker logs` shows the slot ingesting the clip instead.
6. **The clip cost 15 720 prompt tokens** and the server logged `truncated = 0` — it fits in the
   16 384 context with 4% to spare. An 8 s clip therefore nearly fills a 16k window: context, not
   capability, is the real llama.cpp limit.
7. **Timing, cold** (`out/response-run1.json`, `cache_n: 0`): prompt 15 720 tokens in **571.3 s**
   (27.5 tok/s), 200 completion tokens in **158.7 s** (1.25 tok/s) — **730.0 s, about 12 min 10 s**
   end to end. A CPU-only number on a 2B Q8_0 model: it measures this host, not the format.
8. **The answer proves the frames were decoded.** Asked for the timestamp burned into the first
   and the last frame, the model returned `00:00:00.083 f2` and `00:00:07.833 f188`. At 24 fps
   f2 = 0.0833 s and f188 = 7.8333 s, so both stamps are real frames of this clip, each correctly
   paired with its frame number. `finish_reason` was `length`: the 200-token budget went into
   prose, which is a probe artifact, not a failure.
9. **The prompt cache holds the decoded video, and the answer reproduces.** A byte-identical
   repeat reused `n_past = 15719` and re-evaluated a single token, skipping all 571 s of prompt
   processing: **70.1 s** wall, `cache_n 15719`, 65 completion tokens, `finish_reason: stop`, and
   the same two stamps — *"the video begins at 00:00:00.083 and ends at 00:00:07.833"*. It adds
   "This is a 7.75-second video", which is the correct subtraction of the two stamps it read and
   0.25 s short of the container's 8.000 s, because the sampler never showed it f0 or f191.
10. **OpenRouter** (measured by the controller, same clip, 2026-09-20): `z-ai/glm-5.3-flash` via
    CoreWeave, `POST /api/v1/chat/completions`, part
    `{"type":"video_url","video_url":{"url":"data:video/mp4;base64,<whole file>"}}`, body
    3 732 457 B. Run 1 (`max_tokens` 200): HTTP 200 in **11.49 s**, 19 272 prompt tokens,
    **$0.002 960 892**, `finish_reason: length` — the budget went to reasoning, so `content` was
    null, but the reasoning text quoted the burned-in timestamps and the frame counter, which
    proves the frames were decoded rather than guessed. Run 2 (`max_tokens` 900): **22.43 s**,
    9 668 prompt tokens, **$0.001 821 303**, `finish_reason: stop`, content exactly
    `first=00:00:00.500` / `last=00:00:07.500`, which is right for this clip.
    `usage.prompt_tokens_details.video_tokens` reads **0** on both runs: the video is billed inside
    `prompt_tokens`.
11. **The gemma fallback was not run.** The brief reserves `ggml-org/gemma-4-E4B-it-GGUF` for the
    case where Qwen refused the clip or answered nothing useful. Qwen did neither — it accepted the
    video and read two different frames correctly, twice — so the 5.2 GB pull (Q4_0 4.59 GB +
    mmproj 0.56 GB) was not spent. llama.cpp's video support is therefore measured on one model
    family, which is exactly what the verdict below claims and no more.

## Results

**VALIDATED — both backends take native video, each with its own part.**

- **The `/props` key is `video`.** `{"vision": true, "video": true, "audio": false}`, measured on
  b10951 with Qwen3-VL-2B loaded. `llamaCppModalities` already passes unknown keys through
  `clampInputModalities` and renames only `vision` to `image`, so **a key named `video` reaches the
  `video` modality with no code change**. Task 4 is a *pinning* task: add the fake-`/props` test
  case, add no rename.
- **`input_video` works on b10951.** `{"type":"input_video","input_video":{"data":"<base64>"}}`,
  **raw base64 with no `data:` prefix**. The 8 s clip decoded to 15 720 prompt tokens,
  `truncated = 0`, and the model read timestamps burned into two different frames correctly, in
  730 s cold and 70 s against a warm prompt cache.
- **OpenRouter works with the data-URI part.** `{"type":"video_url","video_url":{"url":
  "data:video/mp4;base64,…"}}` on `z-ai/glm-5.3-flash`, answer correct, $0.0018–0.0030 for this
  8 s clip, 11–22 s.
- **Decision for Task 3: implement both branches.** `ReasoningTargetOpenRouter` writes `video_url`
  with the data URI; `ReasoningTargetLlamaCpp` writes `input_video` with bare base64;
  `ReasoningTargetOllama` and `ReasoningTargetNone` write no video part. The test table already in
  Task 3 of the plan matches what was measured and needs no edit.

**What this does NOT show:**

- Nothing about **GPU**. Every llama.cpp number here is CPU-only on 6 threads with a 2B Q8_0 model.
  A GPU build changes the time, not the wire shape and not the capability key.
- Nothing about **other llama.cpp video models**: only Qwen3-VL-2B was loaded, gemma-4-E4B was not
  pulled (trail 11), so "b10951 decodes video" rests on one model family.
- Nothing about **overflow**: this clip fit at 96% of a 16k context. How llama.cpp behaves when a
  decoded video exceeds `n_ctx`, and which frame-sampling flags bound it, was not measured — and at
  15 720 tokens for 8 s that is the next experiment worth running.
- Nothing about **audio** (the clip is silent; `audio: false` here), nor about **streaming**,
  payload ceilings, or a second OpenRouter provider — one model, one provider, two runs.
- The `--image-min-tokens 1024` warning of trail 3 was **not** exercised; grounding accuracy under
  that flag is unmeasured.
- Neither backend was asked for a **wrong** answer: there is no negative control here proving a
  model without video would refuse, only the capability key that is supposed to keep it from being
  asked.
