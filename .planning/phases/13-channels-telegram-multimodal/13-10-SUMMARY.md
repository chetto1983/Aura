---
phase: 13-channels-telegram-multimodal
plan: 10
subsystem: telegram
tags: [telegram, inbound-dispatch, commands, hitl, multimodal, artifact, tts, validation]

# Dependency graph
requires:
  - phase: 13-05
    provides: Telegram channel core, per-turn AG-UI fanout, status pane, content renderer
  - phase: 13-06
    provides: commands, HITL, artifact delivery, onboarding command surface
  - phase: 13-08
    provides: voice/photo/document/TTS multimodal clients
  - phase: 13-09
    provides: serve.go mount, live component E2E tiers, sidecar compose, CI jobs
provides:
  - "Telegram inbound dispatch wired for commands, voice, photo, document, HITL callbacks/replies, status-pane cancel, artifacts, and TTS-out"
  - "MultimodalConfig, command deps, HITL resume seam, artifact and TTS wiring through telegram.Deps"
  - "13-VALIDATION.md audited and marked Nyquist-compliant"
affects: [14-onboarding, 17-packaging]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Inbound dispatch before LLM: commands and HITL are intercepted before ordinary turns"
    - "Subscribe-before-Run x3: status, content, artifact consumers are subscribed before the AG-UI fanout starts"
    - "Inline callback hygiene: answerCallbackQuery toast, 64-byte callback_data guard, stale keyboard disarm"
    - "Telegram-sized final rendering: finalized long answers split into multiple messages instead of truncating"

key-files:
  created:
    - docs/telegram-ux-best-practices.md
    - internal/channels/telegram/bot_dispatch_media_test.go
  modified:
    - internal/channels/telegram/bot.go
    - internal/channels/telegram/bot_dispatch.go
    - internal/channels/telegram/agui_subscriber.go
    - internal/channels/telegram/commands.go
    - internal/channels/telegram/hitl.go
    - internal/channels/telegram/renderer.go
    - internal/channels/telegram/status_pane.go
    - cmd/aura/serve_channels.go
    - internal/config/config.go
    - .planning/phases/13-channels-telegram-multimodal/13-VALIDATION.md

key-decisions:
  - "Live Telegram command checks were completed through the already-open Telegram Web CDP session: /search, /cost, and no-turn /cancel all reached the bot and returned bot replies."
  - "Media upload and old inline-button activation were not forced through raw CDP; those are covered by unit dispatch tests plus the live component tiers because Telegram Web does not expose a reliable deterministic CDP-only media/callback harness here."
  - "Native MTProto/userbot inbound automation is an optional future E2E extension, not a remaining Nyquist validation gap."

requirements-completed: [UX-02, UX-03, UX-04]

# Metrics
duration: continued closeout on 2026-06-08
completed: 2026-06-08
---

# Phase 13 Plan 10: Telegram Inbound Dispatch + UX Closeout Summary

Plan 10 closed the integration gap left by 13-09: the channel now reaches the
features that had already been built in earlier plans. Commands, HITL, voice,
photo, document conversion, artifact delivery, status-pane cancel, and TTS-out
are reachable through the Telegram channel instead of existing only as isolated
components.

## Accomplishments

- `telegram.Deps` carries the multimodal config, command backends, HITL resume
  seam, artifact/TTS wiring, and serve composition roots populate those deps.
- `bot_dispatch.go` registers and routes `OnText`, `OnVoice`, `OnPhoto`,
  `OnDocument`, `OnCallback`, callback fallback, `OnReply`, search pagination
  callbacks, and status-pane cancel callbacks.
- Commands intercept before LLM dispatch; `/cost` reuses the CLI cost path,
  `/search` reuses the conversation search path and now paginates long results,
  `/cancel` uses the in-flight turn cancel registry.
- HITL callbacks answer immediately with short Telegram toasts, clear resolved
  keyboards, and keep choice callback payloads below Telegram's 64-byte limit.
- `handleTurn` subscribes status/content/artifact channels before fanout `Run`,
  sends artifacts as documents, and gates post-render TTS through `ShouldSpeak`.
- The renderer splits finalized long answers into Telegram-sized chunks instead
  of silently dropping the tail.
- The status pane now has a one-tap cancel button while active and collapses
  successful tool lists on completion.
- `docs/telegram-ux-best-practices.md` is closed: all actionable backlog items
  are done; upstream-gated/native-streaming and HTML/entity migration ideas are
  explicitly deferred, not pending.

## Verification

Automated verification run during closeout:

```bash
go test -race ./internal/channels/telegram/ ./internal/agui/ ./internal/config/ ./cmd/aura/
go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/ -count=1
go test ./... -count=1
git diff --check
```

Git hooks also passed on the pushed implementation commit:

```text
pre-commit: gofmt, vet, file-size
pre-push: lint, build
```

Live/CDP checks against the logged-in Telegram Web `Aura_bot` chat:

```text
/search meteo -> Nessun risultato.
/cost -> Spesa di oggi: $0.018500 (172488 token in, 0 token out)
/cancel -> Nessun turno in corso da annullare.
```

The media and HITL inbound surfaces are covered by handler-level tests and the
existing live component tiers:

- `TestOnVoiceRoutesToTranscribe`
- `TestOnPhotoRoutesToDescribe`
- `TestOnDocumentSyncRoutesToConvert`
- `TestOnCallbackToastsAndDisarmsPromptKeyboard`
- `TestOnReplyForceReplyAnswerResumes`
- `TestHandleTurnArtifactReachesSendDocument`
- `TestHandleTurnTTSOutSpeaksWhenInboundVoice`
- `telegram_integration` live Bot-API sendPhoto/sendDocument/sendVoice tier
- `multimodal_integration` live STT/OCR/TTS/document sidecar tier

## Gate Decision

Phase 13 is closed as Nyquist-compliant. Direct live Telegram inbound command
coverage was collected via CDP; deterministic media upload and old-button
callback driving through raw CDP were accepted via automated dispatch tests plus
the live component tiers. A future MTProto/userbot harness may extend the live
inbound E2E, but it is not a remaining phase-13 validation gap.

## Follow-Up

- Phase 14 can build on the Telegram onboarding/setup surface.
- Optional: add an MTProto/userbot inbound E2E leg for voice/photo/document/HITL
  if a reusable authenticated harness becomes available.
