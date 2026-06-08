---
phase: 13-channels-telegram-multimodal
plan: 08
subsystem: api
tags: [telegram, multimodal, stt, tts, vision, ocr, markitdown, faster-whisper, kokoro, glm-ocr, openai-compat, httptest, goleak]

# Dependency graph
requires:
  - phase: 13-03
    provides: llm.SupportsVision/SupportsAudio model-capability flags (gates the photo.go cloud route)
  - phase: 13-05
    provides: bot.go + the botSender seam + asciiCaption helper + the per-turn handler the multimodal pre-processors feed
  - phase: 13-04
    provides: central config AURA_VISION_CLOUD + MULTIMODAL_*/STT_*/TTS_* knobs (projected into telegram.MultimodalConfig)
provides:
  - "voice.go — faster-whisper STT client: OGG/Opus direct multipart upload (no ffmpeg), 2-retry exp backoff, hard-fail UX (IT copy + 😵 reaction); transcript → text user message"
  - "tts.go — Kokoro TTS client: opus voice note via sendVoice (ASCII-safe caption), ShouldSpeak(voice_mode OR echo) trigger, VoiceModePref stub (no explicit send_voice tool, OQ2)"
  - "photo.go — vision routing: ONE config-only AURA_VISION_CLOUD branch (default false=local GLM-OCR sidecar; true=OpenRouter, SupportsVision-gated primary-vs-fallback model)"
  - "documents.go — tiered markitdown /convert: ≤5MB sync / 5-50MB async (WaitGroup-drained, goleak-clean) / >50MB refuse"
  - "sidecar.go — shared thin OpenAI-compat HTTP-client ctor (ctx-timeout dialer, DisableKeepAlives) + telegram-local MultimodalConfig"
affects: [13-09]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Thin OpenAI-compat sidecar HTTP client (ctx-bound timeout, DisableKeepAlives, secret-only-on-header) mirrored from internal/llm/openai_compat — zero Go ML"
    - "Single config-only runtime branch for a mode switch (photo.go AURA_VISION_CLOUD, Pitfall 6 / #60) — no per-mode duplicate handler"
    - "WaitGroup-tracked async goroutine drained by Stop (documents.go 5-50MB tier) — goleak-clean"
    - "telegram-local config projection (MultimodalConfig) to keep the package free of an internal/config import"

key-files:
  created:
    - internal/channels/telegram/sidecar.go
    - internal/channels/telegram/voice.go
    - internal/channels/telegram/tts.go
    - internal/channels/telegram/photo.go
    - internal/channels/telegram/documents.go
    - internal/channels/telegram/voice_test.go
    - internal/channels/telegram/tts_test.go
    - internal/channels/telegram/photo_test.go
    - internal/channels/telegram/documents_test.go
  modified: []

key-decisions:
  - "voice.go HardFail targets the inbound MESSAGE (tele.Editable), not the voice media — *tele.Voice is not Editable; the 😵 reaction is applied to the message"
  - "tts.go omits the model field (Kokoro is single-model, voice is selected by the `voice` field) rather than misusing the vision model id"
  - "photo.go isolates the single AURA_VISION_CLOUD branch in a route() helper returning (baseURL, apiKey, model) — Describe is the one public entry; SupportsVision gates primary-vs-fallback model in the cloud arm"
  - "documents.go async tier detaches from the request ctx via context.WithoutCancel so the convert outlives the inbound handler; Stop drains via WaitGroup+sync.Once"
  - "MultimodalConfig is a telegram-local projection of the central config knobs (composition root wires it in 13-09) — keeps the package internal/config-free, matching config.go"

patterns-established:
  - "Sidecar HTTP-client ctor: ctx-timeout on the dialer (no http.Client.Timeout), DisableKeepAlives for goleak-order-independence, typed *sidecarStatusError for status classification (never a string match)"
  - "Mode-switch = single .env-driven runtime branch, zero code dup (photo.go)"
  - "Async media goroutine = WaitGroup + sync.Once Stop drain, ctx detached via WithoutCancel"

requirements-completed: [UX-04]

# Metrics
duration: 10min
completed: 2026-06-08
---

# Phase 13 Plan 08: Multimodal 9c Sidecar Clients Summary

**Four thin OpenAI-compat sidecar HTTP clients (zero Go ML) for the Telegram channel: faster-whisper STT (OGG/Opus direct, no ffmpeg), Kokoro TTS (opus→sendVoice), GLM-OCR/cloud vision on a single AURA_VISION_CLOUD branch (SupportsVision-gated), and tiered markitdown document conversion.**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-06-08T09:54:37Z
- **Completed:** 2026-06-08T10:03:57Z
- **Tasks:** 2 (both TDD: RED→GREEN)
- **Files modified:** 9 created (5 source + 4 test)

## Accomplishments
- **voice.go (STT):** downloads the OGG/Opus voice note via a `botFiler` seam and POSTs the bytes DIRECTLY (`mime/multipart`) to `STT_BASE_URL/audio/transcriptions` — faster-whisper decodes Opus inline, NO ffmpeg pre-step (spike 027). 2-retry exp backoff (1s/2s default, ctx-honoring sleep); a persistent failure returns an error and `HardFail` applies the 😵 reaction; the IT copy "❌ Trascrizione non disponibile." is the user message the handler sends. The transcript becomes a text user message fed to `runner.Turn` by the handler (13-09).
- **tts.go (TTS):** POSTs reply text to `TTS_BASE_URL/audio/speech` (`response_format=opus`, Kokoro `if_sara`) and replies via `sendVoice` with an ASCII-sanitized caption (Pitfall 4, reuses `asciiCaption`). Trigger = `ShouldSpeak(voice_mode OR inbound-was-voice)` — NO explicit `send_voice` tool this phase (OQ2); `VoiceModePref` is a stub returning false until Slice 10/Phase 14.
- **photo.go (vision):** ONE public entry (`Describe`) with ONE config-only branch isolated in `route()` — `if cfg.VisionCloud { OpenRouter } else { aura-ocr-vl }` (Pitfall 6 / #60, zero code dup). The UNCHANGED default (`AURA_VISION_CLOUD=false`) POSTs a base64 `image_url` to the local GLM-OCR sidecar `/chat/completions`; `true` routes to OpenRouter using the PRIMARY model when `llm.SupportsVision(model)` is true, else `MULTIMODAL_FALLBACK_MODEL` (minimax-m3) so an image is never sent to a non-vision model (T-13-08-VisionMisroute). The switch is `.env`-only — no default is flipped.
- **documents.go (markitdown):** tiered `/convert` (T-13-08-SidecarDoS) — ≤5MB SYNC (return markdown), 5-50MB ASYNC on a WaitGroup-tracked goroutine that `Stop` drains (goleak-clean, Pitfall 5; ctx detached via `WithoutCancel`), >50MB REFUSED with an IT user-facing message and NO sidecar call. The `/convert` request shape is isolated to `postConvert` (A2: ASSUMED endpoint → one-line change if the real image differs).
- **sidecar.go:** the shared OpenAI-compat HTTP-client ctor (mirrors `internal/llm/openai_compat`: ctx-timeout dialer, DisableKeepAlives) + the telegram-local `MultimodalConfig` projection, reused by all four clients.

## Task Commits

Each task was committed atomically:

1. **Task 1: voice.go (STT OGG/Opus direct) + tts.go (Kokoro opus→sendVoice)** — `e029140c` (feat, TDD RED+GREEN)
2. **Task 2: photo.go (single AURA_VISION_CLOUD branch) + documents.go (tiered markitdown)** — `8136aa6c` (feat, TDD RED+GREEN)

_Note: each TDD task was written test-first (confirmed RED compile-failure) then implemented to GREEN; the two halves landed in one atomic feat commit per task._

## Files Created/Modified
- `internal/channels/telegram/sidecar.go` — shared sidecar HTTP-client ctor + MultimodalConfig (124 LOC)
- `internal/channels/telegram/voice.go` — STT multipart client, OGG/Opus direct, 2-retry, hard-fail UX (192 LOC)
- `internal/channels/telegram/tts.go` — TTS opus→sendVoice, ShouldSpeak trigger, VoiceModePref stub (122 LOC)
- `internal/channels/telegram/photo.go` — vision routing, single AURA_VISION_CLOUD branch, SupportsVision-gated (144 LOC)
- `internal/channels/telegram/documents.go` — tiered markitdown /convert, async WaitGroup drain (164 LOC)
- `internal/channels/telegram/voice_test.go` / `tts_test.go` / `photo_test.go` / `documents_test.go` — httptest mock-sidecar unit tests

## Decisions Made
- `voice.go HardFail` reacts on the inbound MESSAGE (`tele.Editable`), not the voice media (`*tele.Voice` is not `Editable`).
- `tts.go` omits the model field (Kokoro selects the voice via the `voice` field).
- `photo.go` keeps `Describe` as the single public entry and isolates the one `if cfg.VisionCloud` decision in `route()`.
- `documents.go` async tier detaches the request ctx (`context.WithoutCancel`) so the convert survives the handler return; `Stop` drains via WaitGroup + `sync.Once`.

## Deviations from Plan

None — plan executed exactly as written. (Two intra-task test-mechanics fixes during GREEN, documented under Issues Encountered, were test-double corrections, not plan deviations.)

## Issues Encountered
- **`*tele.Voice` is not `tele.Editable`** (build error during Task 1 GREEN): the React API needs the message as the `Editable`. Changed `HardFail` to take a `tele.Editable` (the inbound message) and updated the test to pass a `*tele.Message`. Correct domain modelling — a reaction targets a message, not media.
- **httptest connection reset on the >5MB async upload** (test failure during Task 2 GREEN): the mock `/convert` handler responded before draining the 5MB+ request body, resetting the connection mid-upload (`wsasend: forcibly closed`). Fixed by draining the request body in the test handler (`io.Copy(io.Discard, r.Body)`) — mirrors a real markitdown sidecar which reads the multipart file fully before responding. Test-double correctness, not a production-code change.

## Known Stubs
- `VoiceModePref(convID) bool` returns the default `false` (Slice 10 preferences are not shipped — Phase 14). This is INTENTIONAL and documented in the function doc-comment: the TTS trigger still fires on the echo path (inbound-was-voice) today, so the stub does not block UX-04; Phase 14 wires it to the real preference store. Tracked, not blocking the plan goal.

## Verification

- `go test ./internal/channels/telegram/ -run 'Voice|STT|TTS'` — green (STT OGG-direct multipart + 2-retry hard-fail; TTS opus→sendVoice non-nil msg.Voice + ASCII caption).
- `go test ./internal/channels/telegram/ -run 'Photo|Vision'` — green (false→local-hit/cloud-not; true-vision→cloud primary model; true-non-vision→cloud fallback model; sidecar-5xx error path).
- `go test ./internal/channels/telegram/ -run Document` — green (sync markdown / async callback delivery drained by Stop / >50MB refuse no-hit / sync-5xx error / Stop idempotent).
- `go vet ./...` + `go build ./...` clean; `go test -race ./internal/channels/telegram/` green (2.6s); `golangci-lint run ./internal/channels/telegram/...` = 0 issues; `go vet -tags multimodal_integration` clean. All files ≤600 LOC.

## Threat Flags

None — no security surface outside the plan's `<threat_model>`. All four mitigations applied: HTTP timeout 30s + >50MB refuse (T-SidecarDoS), WaitGroup-drained async goroutine (T-AsyncLeak), SupportsVision-gated cloud route (T-VisionMisroute), ASCII-sanitized voice caption (T-CaptionInject).

## Next Phase Readiness
- The four media clients are standalone units with clean seams (`botFiler`/`botReactor`/`botSender`, `MultimodalConfig`, `OnAsyncResult`). Plan 13-09 wires them into the Telegram media handlers (OnVoice/OnPhoto/OnDocument), populates `MultimodalConfig` from the central config at the composition root, mounts the channels Registry + setup server in serve.go, and runs the live `multimodal_integration`/`telegram_integration` tiers + Gate-3 sign-off.
- No blockers. The live sidecar tier is deliberately delegated to 13-09 (unit tier uses httptest mocks only).

## Self-Check: PASSED

All 9 source/test files created and present on disk; both task commits (`e029140c`, `8136aa6c`) exist in git history.

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
