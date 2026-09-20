# Live verification — a video reaches the model only when the model declares video input

Measured 2026-09-20 on the running appliance at `https://localhost`, through the cockpit.
Plan: [`docs/superpowers/plans/2026-09-19-video-to-model.md`](../plans/2026-09-19-video-to-model.md).
Measurement the plan rests on: [`.planning/spikes/106-video-to-model/README.md`](../../../.planning/spikes/106-video-to-model/README.md).

| | |
|---|---|
| Revision before the update | `781f1b200` (Plan A; no video path at all) |
| Revision the cases ran on | **`f68711c8d`** (`002ad2da4` for cases 1–2, then `f68711c8d` after the llama.cpp fix below) |
| Operator's primary model, before and after | `openrouter` / `https://openrouter.ai/api/v1` / **`deepseek/deepseek-v4.1-flash`** — noted before anything was routed, restored at the end, re-read from `aura.settings` to confirm |

## What was sent

`.planning/spikes/105-studio-media-editing/media/clip-h264-silent.mp4` — 1280x720 H.264,
24 fps, 192 frames, 8.000 s, 2 799 062 B, silent, with an `hh:mm:ss.mmm fN` timestamp
burned into every frame. Uploaded through the cockpit's real presign → objectstore →
finalize path; accepted as `modality: "video"`, `size_bytes: 2799062`.

One prompt for every case: read the stamp burned into the top-left of the **first** and
the **last** frame and answer `first=<stamp> last=<stamp>`, or exactly `NO FRAMES` if the
frames themselves are not visible. A model that cannot see pixels cannot invent these
numbers, and a model that can see them names frames its own sampler chose.

## Cases

| # | Routed model | Declares video? | Prompt tokens | Answer | Verdict |
|---|---|---|---|---|---|
| 1 | `z-ai/glm-5.3-flash` (OpenRouter) | yes (`text, image, video`) | 26 917 | `first=00:00:00.500 last=00:00:07.500` | **PASS** |
| 2 | `deepseek/deepseek-v4.1-flash` (OpenRouter) | no (`text, image`) | 18 119 | `NO FRAMES` | **PASS** |
| 3 | `ggml-org/Qwen3-VL-2B-Instruct-GGUF` (llama.cpp `server-cuda-b10951`) | yes (`/props` → `video: true`) | 33 317 | `first=00:00:00.083 last=00:00:07.833` | **PASS**, after a fix |
| 4 | Telegram video note | — | — | — | **NOT EXERCISED** |

**Case 1 — the video-capable cloud model read the clip.** The stamps are the same pair
spike 106 got from this model through a bare `video_url` request, so the frames were
decoded rather than guessed. Cost $0.0024 for the turn.

**Case 2 — the text-only model got the reference and said so.** Same clip, same prompt,
same upload path, only the routed model changed: no error, no refusal, just a model
telling the truth about what it was given. The quantitative witness is the size of the
prompt — **26 917 − 18 119 = 8 798 tokens** of difference. That delta is the clip.

**Case 3 — the local model read the clip, and the server log proves it.** 33 317 −
17 626 = **15 691 tokens** of clip, against the **15 720** spike 106 measured for this
exact file, and llama-server's prompt rate fell from 4 728 tok/s on the text-only attempt
to **1 386 tok/s**, which is the multimodal encoder working. `truncated = 0`. The stamps
(`f2` and `f188` at 24 fps) are a *different* pair from case 1's, because each backend
samples its own frames — both are real frames of this clip.

**Case 4 — not exercised, and not approximated.** A linked chat exists
(`aura.telegram_accounts` holds one row, linked 2026-09-11, channel running). But the path
under test is inbound — a video note travelling from a human's Telegram client to the bot
— and sending that means acting as the linked human. The Bot API only goes the other way,
and a bot-sent video would exercise none of the projection. What covers this path for now
is `internal/channels/telegram/bot_dispatch_media_projection_test.go`, which drives the
Telegram projection for image and video over one table and keeps audio out, through the
same `assets.NativeMedia` admission the cockpit uses. That is a unit-level guarantee, not
a live one.

## The bug this verification found

Case 3 failed on its first run: 17 626 text-only tokens, `NO FRAMES`, and a server log
with no media ingest — against a local server whose `/props` plainly said `video: true`.

`internal/llm/llamacpp_caps.go` typed `default_generation_settings.params` as
`map[string]float64`, but a live llama-server puts `"ignore_eos": false`, `"samplers": [...]`,
`"chat_format": "Content-only"` and more in that same object. `encoding/json` therefore
rejected the **whole** `/props` document over the first boolean:

```
llm: decode /props: json: cannot unmarshal bool into Go struct field
llamaCppPropsResponse.default_generation_settings.params.ignore_eos of type float64
```

The `modalities` object beside it had already parsed correctly and was discarded with the
error, so `probedContentCaps` recorded `detected=false` and **no llama.cpp backend could
receive image or video bytes at all**. The field predates this plan (it is the 2026-09-03
sampling discovery); what the plan did was give the failure a visible consequence. Fixed
in `f68711c8d`, test-first: `params` decodes as `any` and is narrowed by `numbersOnly`,
which skips non-numeric members instead of discarding the set — the rule
`parseOllamaSamplingParameters` already documents for its own source. The `/props` fixture
gained the real mixed block its own note always claimed it pinned, which is why the
existing tests had been green against a shape no server emits.

## What this does NOT prove

- **Nothing about duration or size limits.** One 8 s, 2.7 MB clip, on every case. No
  per-model duration ceiling was probed, no large file, no long video. At 15 720 tokens
  for 8 seconds on llama.cpp, a clip a few times longer stops fitting a modest context
  before anything else goes wrong — untested here.
- **Nothing about the wire bytes as read off the wire.** No proxy sat between Aura and
  OpenRouter, because that would have meant handling the operator's key. Case 1 proves the
  part was *accepted and decoded* by a model that only accepts
  `{"type":"video_url","video_url":{"url":"data:..."}}`; the byte-exact shape Aura emits is
  pinned by `internal/llm/openai_compat/request_video_test.go`, not by this run. Case 3's
  shape is witnessed at the receiving end by llama-server's own log.
- **Two models, one provider each.** `z-ai/glm-5.3-flash` on OpenRouter and Qwen3-VL-2B on
  llama.cpp b10951. Nothing here says how another OpenRouter video model, another provider
  routing the same model, or another llama.cpp VL family behaves.
- **Nothing about Ollama or an unknown backend** beyond the code's default: they declare no
  video, so they are never sent any. Not exercised live.
- **Nothing about Telegram, live** — see case 4.
- **Nothing about audio**, which by design never becomes native media; the clip is silent.
- **Nothing about concurrency, streaming partial failures, or retries** — one turn per
  case, each on a fresh thread.
- **Case 2's token baseline is a shape, not a constant.** The two turns ran on separate
  threads, so 18 119 is the same *kind* of prompt, not the same bytes; the ~8.8k delta
  supports an order-of-magnitude conclusion, not a token-exact one.
