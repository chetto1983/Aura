#!/usr/bin/env bash
# Installs the probe's pinned packages and stages what the probe pages fetch: the fixtures, the
# VAD models and the ONNX runtime files, all served from the probe's own origin.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/.local/bin:$PATH"
npm install --no-audit --no-fund
npx playwright install chromium
mkdir -p public/fixtures public/vad public/ort out
cp ../../web/e2e/fixtures/video-studio/clip-a.mp4 public/fixtures/
cp ../../web/e2e/fixtures/video-studio/audio/*.wav ../../web/e2e/fixtures/video-studio/audio/*.json public/fixtures/ 2>/dev/null || true
cp node_modules/@ricky0123/vad-web/dist/silero_vad_legacy.onnx node_modules/@ricky0123/vad-web/dist/silero_vad_v5.onnx public/vad/
cp node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.wasm node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.mjs public/ort/
