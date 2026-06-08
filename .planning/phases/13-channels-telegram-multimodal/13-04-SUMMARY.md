---
phase: 13-channels-telegram-multimodal
plan: 04
subsystem: infra
tags: [channels, registry, telegram, config, env, fail-soft, errors-join, multimodal]

# Dependency graph
requires:
  - phase: 13-channels-telegram-multimodal (13-01)
    provides: internal/channels + internal/channels/telegram packages (goleak harnesses, Store, deps anchor)
provides:
  - "Channel interface (narrow 4-method lifecycle contract; Start takes ctx only — NO fanout subscriber)"
  - "Registry: map-backed, per-channel AURA_CHANNEL_<NAME>_ENABLED gate (default true), errors.Join fail-soft StartAll/StopAll, flag override predicate"
  - "telegram.Config + LoadConfig (TELEGRAM_BOT_TOKEN upstream naming + AURA_TELEGRAM_* throttles)"
  - "central config.Config Phase-13 knobs: AURA_SETUP_BIND (:9081 loopback), AURA_SETUP_TOKEN, AURA_VISION_CLOUD, MULTIMODAL_*/STT_*/TTS_* sidecar URLs+models"
affects: [13-05 telegram bot.go/renderer, 13-06 artifact consumer, 13-07 setup wizard, 13-08 multimodal clients, 13-09 serve.go registry mount]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fail-soft daemon-subsystem registry: errors.Join aggregation, one channel's Start failure never aborts siblings (mirrors serve.go agui http log-but-never-exit)"
    - "Per-subsystem config ownership: telegram owns its bot-token+throttle Config in-package; central config.Config owns the cross-subsystem setup/vision/sidecar knobs"

key-files:
  created:
    - internal/channels/channel.go
    - internal/channels/registry.go
    - internal/channels/registry_test.go
    - internal/channels/telegram/config.go
    - internal/channels/telegram/config_test.go
    - internal/config/config_channels_test.go
  modified:
    - internal/config/config.go
    - internal/config/config_test.go

key-decisions:
  - "Start(ctx) takes NO fanout subscriber — fanout is per-turn inside the channel (research §1, overrides the PRD Start(ctx,sub) sketch)"
  - "Registry tracks started channels so StopAll only stops what StartAll started; StopAll idempotent"
  - "Enable resolution: flag override predicate wins (ok=true), else AURA_CHANNEL_<NAME>_ENABLED env gate (default true, silent fallback)"
  - "telegram throttles read via a local envIntDefault copy to keep the telegram package free of an internal/config import"
  - "AURA_SETUP_BIND defaults 127.0.0.1:9081 — a loopback port DISTINCT from the AG-UI :9080 (T-13-04-SetupExposure compensating control)"

patterns-established:
  - "Fail-soft registry: StartAll logs+aggregates per-channel Start errors via errors.Join, continues siblings, returns the joined error without aborting"
  - "Enable gate: AURA_CHANNEL_<upper(Name)>_ENABLED default-true silent fallback + flag override predicate seam for serve.go --no-telegram/--only=cli (13-09)"

requirements-completed: [UX-02]

# Metrics
duration: ~17min
completed: 2026-06-08
---

# Phase 13 Plan 04: Channels Framework + Phase-13 Env Surface Summary

**Narrow Channel interface + fail-soft errors.Join Registry with a per-channel AURA_CHANNEL_<NAME>_ENABLED gate, plus the Telegram channel config and the central setup/vision/sidecar env knobs every later Wave plan mounts into.**

## Performance

- **Duration:** ~17 min
- **Started:** 2026-06-08T10:33:00Z
- **Completed:** 2026-06-08T10:42:00Z
- **Tasks:** 2 (both TDD)
- **Files modified:** 8 (6 created, 2 modified)

## Accomplishments
- `Channel` interface: the narrow 4-method lifecycle contract (Name/Start/Stop/IsHealthy) with `Start(ctx)` taking NO fanout subscriber — fanout is per-turn inside the channel (research §1 critical correction, supersedes the PRD `Start(ctx, sub)` sketch).
- `Registry`: map-backed (NewRegistry/Register) with `StartAll`/`StopAll` that read `AURA_CHANNEL_<NAME>_ENABLED` (default true), aggregate failures with `errors.Join`, and never abort siblings on one failure (fail-soft, mirrors serve.go's agui-http "log but never exit"). StopAll only stops what StartAll started and is idempotent. A flag-override predicate seam (`SetEnabledOverride`) backs serve.go's future `--no-telegram`/`--only=cli` (13-09).
- `telegram.Config` + `LoadConfig`: `TELEGRAM_BOT_TOKEN` (upstream/third-party naming) + the three `AURA_TELEGRAM_*` throttles (1500/500/1000ms silent-fallback defaults).
- Central `config.Config` Phase-13 surface: `AURA_SETUP_BIND` (loopback `:9081`, distinct from AG-UI `:9080`), `AURA_SETUP_TOKEN` (empty → boot-generated 13-07), `AURA_VISION_CLOUD` (false=local GLM-OCR), and the `MULTIMODAL_*`/`STT_*`/`TTS_*` sidecar URL+model knobs (upstream naming; fallbacks minimax-m3/if_sara/opus).

## Task Commits

Each task committed atomically (TDD red→green folded into one feature commit per task; the interface is a pure contract and the registry RED/GREEN were authored in sequence):

1. **Task 1: Channel interface + Registry (fail-soft, enable-gated)** - `76cc566a` (feat)
2. **Task 2: telegram config.go + central AURA_* env additions** - `923c229a` (feat)

**Plan metadata:** [final docs commit — see git log]

## Files Created/Modified
- `internal/channels/channel.go` - the narrow 4-method `Channel` interface (subscriber-free Start)
- `internal/channels/registry.go` - map-backed Registry, AURA_CHANNEL_*_ENABLED gate, errors.Join fail-soft StartAll/StopAll, override predicate
- `internal/channels/registry_test.go` - fake Channel double: enable-gate, fail-soft aggregation, StopAll-only-stops-started, idempotency, override on/off
- `internal/channels/telegram/config.go` - telegram Config + LoadConfig (bot token + throttles)
- `internal/channels/telegram/config_test.go` - telegram config defaults/overrides/malformed-fallback
- `internal/config/config.go` - added Phase-13 setup/vision/sidecar fields + their env loads
- `internal/config/config_test.go` - extended clearPostgresEnv with the new keys (Phase-13 test split out)
- `internal/config/config_channels_test.go` - Phase-13 config defaults+overrides + separate-port assertion

## Decisions Made
- `Start(ctx)` subscriber-free (research §1) — the channel holds the `*runner.Runner` and builds a fresh Fanout per turn; the registry never sees fanout.
- telegram package keeps a local `envIntDefault` copy rather than importing `internal/config` — keeps the channel config cohesive and import-light (matches the config.go package doc that per-subsystem configs live in their owning packages).
- Sidecar base URLs (MULTIMODAL/STT/TTS) default empty — they stay empty until the operator wires the compose sidecars; only the model/voice/format knobs carry fallbacks. This is intentional config, NOT a stub (the consumers in 13-08 fail-closed on an empty URL).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Split config_test.go to satisfy the 600-LOC file-size hook**
- **Found during:** Task 2 (central AURA_* env additions)
- **Issue:** Adding `TestPhase13ConfigDefaultsAndOverrides` pushed `internal/config/config_test.go` to 645 LOC; the pre-commit `file-size` hook (CLAUDE.md NO GOD CLASS / refactor-on-touch) rejected the commit.
- **Fix:** Extracted the Phase-13 test verbatim into a new `internal/config/config_channels_test.go` (81 LOC); `config_test.go` returned to 568 LOC.
- **Files modified:** internal/config/config_test.go, internal/config/config_channels_test.go
- **Verification:** Hook green on re-commit; `go test ./internal/config/` passes; both files < 600 LOC.
- **Committed in:** `923c229a` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** The split is a mechanical refactor-on-touch mandated by the project's file-size gate; no behavior change, no scope creep.

## Issues Encountered
None beyond the file-size split above.

## Deferred Issues
Pre-existing golangci-lint findings in `internal/channels/telegram/tables.go` (errcheck on the two `*Face.Close()` defers) and `mdv2.go`/`mdv2_test.go` (staticcheck QF1002 tagged-switch suggestions) were surfaced by the lint run but are OUT OF SCOPE — those files were authored by plan 13-03, not touched here. Logged to `.planning/phases/13-channels-telegram-multimodal/deferred-items.md` for the 13-05 renderer plan (which consumes them) or a hygiene sweep. This plan's own new files are golangci-lint-clean (`--new-from-rev=HEAD` → 0 issues).

## User Setup Required
None - no external service configuration required this plan. The new env knobs all carry safe defaults (loopback binds, local-sidecar vision, empty sidecar URLs that fail-closed at call time).

## Next Phase Readiness
- The `Channel` interface + `Registry` are the stable mount point for 13-05/13-06 (Telegram bot.go implements Channel; serve.go in 13-09 builds the registry, calls StartAll/StopAll).
- The Phase-13 env surface (setup bind/token, vision branch, sidecar URLs) is loaded into `config.Config` for 13-07 (setup wizard reads SetupBind/SetupToken) and 13-08 (multimodal clients read the sidecar URLs + VisionCloud branch).
- No blockers. Unit + `-race` tiers are the authoritative gate for this pure-config/lifecycle plan and are green.

## Self-Check: PASSED

All 7 created files exist on disk; both task commits (`76cc566a`, `923c229a`) are present in git history.

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
