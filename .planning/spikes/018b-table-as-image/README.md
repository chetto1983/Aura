---
spike: 018b
name: table-as-image
type: comparison
validates: "Given the same markdown table, when rendered to PNG in pure Go and sent via sendPhoto, then it is crisp and readable on the operator's device, with latency/LOC/dependency cost measured"
verdict: VALIDATED WINNER
related: [017-telebot-v4-sha-pin-live-send, 018a-table-pre-block, 018c-table-restructured]
tags: [telegram, tables, png, x-image, phase-13]
---

# Spike 018b: Table → PNG Image (pure Go)

## What This Validates

Given the same T1/T2/T3 markdown tables as 018a, when rendered to a gridded PNG with
`golang.org/x/image` (opentype + `gofont/gomono`/`gomonobold`, 28px = 14pt@2x) and sent
both as `sendPhoto` and `sendDocument`, then the result is crisp and readable on the
operator's device — and the render cost (latency, bytes, deps) is measured.

## Research

- Pure-Go stack, no CGO, no external font files: `x/image/font/opentype` +
  `gofont/gomono` (regular cells) + `gomonobold` (header). `x/image` was already an
  *indirect* dep of Aura's module (2019 pseudo-version) — upgraded to v0.41.0 for the spike.
- Telegram re-encodes photos (JPEG); mitigation probed = 2x font scale. `sendDocument`
  preserves the original PNG losslessly as comparison arm.
- Rejected alternatives: fogleman/gg (extra dep + freetype, no gain for grids),
  headless-browser HTML render (a nuclear bomb for a table).

## How to Run

```bash
set -a; source <(tr -d '\r' < .env); set +a
go run -tags spike_telegram ./.planning/spikes/018b-table-as-image
```

PNGs also land in `D:/tmp/spike-018b-*.png` for host-side inspection.

## What to Expect

Per table: `[RENDER]` WxH/bytes/ms, then a photo + document pair delivered; the photo
read-back logs Telegram's served dimensions vs original (re-encode probe); exit 0.

## Investigation Trail

1. First run green (msg 3423-3428): T1 520×313 @5ms/22KB, T2 720×366 @10ms/30KB,
   T3 1923×366 @21ms/67KB.
2. Telegram served photos at **original dimensions** (no downscale at these sizes) but
   re-encoded: served byte size *larger* than the source PNG (e.g. T3 67KB→121KB) —
   high-quality re-encode, crispness preserved at 2x scale.
3. Human checkpoint on the operator's phone: **winner on both T2 (common case) and T3
   (wide stress case)** over pre-block and key-value variants. Artifacts opened fine.

## Results

**VALIDATED — comparison WINNER (T2 + T3).**

- **Phase-13 decision input**: markdown tables in LLM replies render to PNG and go out as
  `sendPhoto`; pre-block (018a) is the no-dep fallback when rendering fails.
- Cost is negligible: 5-21ms render, pure Go, the dep is `golang.org/x/image` (already
  indirect, promoted to direct in-phase) + embedded Go fonts. No file-system fonts, no CGO.
- 2x scale (28px) survives Telegram's photo re-encode legibly; `sendDocument` arm not
  needed for tables (photo crispness sufficed on-device).
- Renderer budget for the phase: ~150 LOC (`parse + measure + draw + encode`), unicode
  measured per-rune via font metrics (the `—` em-dash cell rendered correctly).
