# Phase 13 — Deferred Items (out-of-scope discoveries)

Logged per the executor SCOPE BOUNDARY rule: pre-existing lint findings in files
NOT modified by the current plan. Do NOT fix these in the plan that discovered
them — they belong to the owning plan / a hygiene sweep.

## Discovered during 13-04 (golangci-lint on internal/channels/telegram)

Owner: plan 13-03 (these files were authored there; 13-04 did not touch them).

| File | Line | Linter | Finding |
|------|------|--------|---------|
| internal/channels/telegram/tables.go | 137 | errcheck | `bodyFace.Close()` return value not checked (`defer bodyFace.Close()`) |
| internal/channels/telegram/tables.go | 142 | errcheck | `headFace.Close()` return value not checked (`defer headFace.Close()`) |
| internal/channels/telegram/mdv2.go | 81 | staticcheck QF1002 | could use tagged switch on `c` |
| internal/channels/telegram/mdv2_test.go | 126 | staticcheck QF1002 | could use tagged switch on `c` |
| internal/channels/telegram/mdv2_test.go | 142 | staticcheck QF1002 | could use tagged switch on `c` |

Note: these did not fail CI at 13-03 close (the project lint gate may run a
different ruleset / these may be advisory). Surfaced here so the phase-close
hygiene sweep or the renderer plan (13-05, which consumes tables.go/mdv2.go) can
fold the fixes on-touch. 13-04's own new files (registry.go, channel.go,
telegram/config.go, config.go additions) are golangci-lint-clean
(`--new-from-rev=HEAD` → 0 issues).

## Discovered during 13-09 Gate-3 live E2E (compose sidecar images vs the validated spikes)

Owner: plan 13-08/13-09 (the 9c sidecar compose wiring).

| Sidecar | compose default | Issue | Disposition |
|---------|-----------------|-------|-------------|
| aura-stt | `fedirz/faster-whisper-server:latest-cpu` + `WHISPER__MODEL=large-v3-turbo` | HTTP 500 `Invalid model size 'large-v3-turbo'` — that image only supports up to large-v3 | **FIXED** (38870e40) → spike-027's `hwdsl2/whisper-server` + `WHISPER_MODEL` on :9000 (turbo native, OGG/Opus direct). E2E TTS→STT now green. |
| markitdown | `ghcr.io/microsoft/markitdown:latest` | registry-denied — no such public image; markitdown ships no official HTTP `/convert` server image | **DEFERRED** — the documents.go `/convert` leg (UX-04 doc conversion) is NOT in the `multimodal_integration` tier (STT/OCR/TTS only). Needs an operator-provided or purpose-built markitdown-server image, or a documents.go pivot to a different converter. Fail-soft at runtime (an unreachable sidecar just errors the document path). Not blocking the Phase-13 Gate-3 sign-off. |

Note: the GLM-OCR sidecar (`ghcr.io/ggml-org/llama.cpp:server` `--hf-repo …glm-ocr-GGUF`) works as-is — llama.cpp auto-pulled the mmproj and the live OCR describe leg passed (4.5s). No change needed there despite spike 026 using explicit local `-m`/`--mmproj`.
