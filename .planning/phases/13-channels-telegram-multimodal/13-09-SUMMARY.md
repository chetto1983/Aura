---
phase: 13-channels-telegram-multimodal
plan: 09
subsystem: infra
tags: [telegram, channels, daemon, multimodal, sidecar, compose, ci, integration-test, goleak, fail-soft]

# Dependency graph
requires:
  - phase: 13-05
    provides: telegram.NewChannel(Deps) — the channels.Channel impl (polling lifecycle, per-turn fanout)
  - phase: 13-07
    provides: setup.NewServer(Deps) — the loopback :9081 setup-wizard HTTP/SSE server + BotProbe seam
  - phase: 13-08
    provides: the 4 multimodal sidecar clients (voice/tts/photo/documents) + MultimodalConfig
  - phase: 13-04
    provides: channels.Registry (StartAll/StopAll, AURA_CHANNEL_*_ENABLED gate, SetEnabledOverride)
  - phase: 12
    provides: bootServe/runServe daemon composition root + the AG-UI fail-soft mount pattern
provides:
  - "channels Registry (Telegram) mounted as a fail-soft daemon sibling of the AG-UI gateway in bootServe/runServe"
  - "setup-wizard HTTP server (:9081) mounted + run fail-soft; StopAll/Shutdown before env.close()"
  - "--no-telegram / --only=cli flags overriding the AURA_CHANNEL_TELEGRAM_ENABLED env gate"
  - "telegram_integration tier (live Bot-API, response-asserted, no-skip-as-green) — COMPILES; live run is Gate-3"
  - "multimodal_integration tier (live STT/OCR/TTS round-trip, no-skip-as-green) — COMPILES; live run is Gate-3"
  - "compose.yaml: aura-stt / aura-tts / aura-ocr-vl / markitdown sidecar services"
  - "ci.yml: sidecar-gated multimodal_integration + operator-token-gated telegram_integration jobs"
affects: [14-onboarding, 17-packaging]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fail-soft daemon-subsystem mount (channels Registry + setup server as siblings of the AG-UI gateway; StopAll/Shutdown before env.close())"
    - "Composition-root interface adaptation (setupStoreAdapter projects setup.InsertPendingParams onto telegram — breaks the cross-package import)"
    - "No-skip-as-green live integration tiers (t.Fatal under $CI when env set; compile floor always runs, live send gated on the secret)"
    - "Response-not-poll-stream ground truth (assert on the Send reply msg.Photo/Document/Voice, never the inbound update stream)"

key-files:
  created:
    - cmd/aura/serve_channels.go
    - cmd/aura/serve_test.go
    - internal/channels/telegram/integration_test.go
    - internal/channels/telegram/multimodal_integration_test.go
  modified:
    - cmd/aura/serve.go
    - compose.yaml
    - .github/workflows/ci.yml

key-decisions:
  - "Read the bot token + throttles via telegram.LoadConfig() (NOT a non-existent chat.cfg.TelegramBotToken field — the central config has no such field; the telegram package owns TELEGRAM_BOT_TOKEN + AURA_TELEGRAM_*_THROTTLE_MS)"
  - "OpenRouter base/key/model for the cloud-vision branch come from cfg.LLM (the central config has no OpenRouter fields); the setup IdentityID resolves the seeded `local` identity"
  - "serve.go split: the channel/setup wiring lives in serve_channels.go (refactor-on-touch; serve.go stays 287 LOC)"
  - "compose sidecar images are env-overridable (AURA_STT_IMAGE etc.) so the operator pins the exact CPU image; defaults reference the upstream CPU tags from research §7"

patterns-established:
  - "Fail-soft subsystem lifecycle: startChannelSubsystems/stopChannelSubsystems mirror the AG-UI ListenAndServe goroutine + bounded Shutdown; StopAll runs BEFORE env.close()"
  - "setupStoreAdapter: the composition-root bridge for two structurally-identical-but-distinct-package param types"
  - "no-skip-as-green CI: a sidecar-gated job + an operator-token-gated job, each with an always-running compile floor + a live step that t.Fatals under $CI when its env is set"

requirements-completed: []  # UX-02/03/04 are NOT complete — the live Gate-3 (Task 3) is orchestrator-owned + pending

# Metrics
duration: ~35min
completed: 2026-06-08
---

# Phase 13 Plan 09: Channels + Setup + Multimodal Wiring Summary (Tasks 1-2 — Gate-3 PENDING)

**channels Registry (Telegram) + setup :9081 server mounted fail-soft into `aura serve`, plus the two live integration tiers (telegram_integration response-asserted, multimodal_integration STT/OCR/TTS round-trip) + the 4 compose sidecars + the no-skip-as-green CI wiring — both tiers COMPILE; the live full matrix + coverage + mutation + operator sign-off is the orchestrator-owned Task-3 checkpoint.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-06-08
- **Completed (Tasks 1-2):** 2026-06-08
- **Tasks:** 2 of 3 (Task 3 = Gate-3 human-verify checkpoint, orchestrator-owned — NOT executed)
- **Files modified:** 7 (4 created, 3 modified)

## Accomplishments

- **Task 1 (mount):** `serveEnv` gains `channels *channels.Registry` + `setupSrv *http.Server`. `bootServe` builds the Telegram channel over the shared composition root (Runner + pool + `TELEGRAM_BOT_TOKEN` + the `AURA_TELEGRAM_*` throttles), registers it in `channels.NewRegistry()`, and builds the loopback setup-wizard server (:9081) with a telebot `getMe` `BotProbe` closure. `runServe` `StartAll`s the registry + runs the setup server fail-soft (mirroring the AG-UI `ListenAndServe` goroutine), then `StopAll` + `Shutdown` **before** `env.close()`. `--no-telegram` / `--only=cli` override the enable gate. Proven by `serve_test.go` with a fake channel (no live bot): StartAll/StopAll invoked, fail-soft on a failing channel, ordering, flag override — goleak-clean.
- **Task 2 (live tiers + sidecars + CI):** `integration_test.go` (`//go:build telegram_integration`) live-sends a table-PNG / artifact / voice to the operator chat and asserts on the Bot-API **RESPONSE** (`msg.Photo` / `msg.Document.{FileName,MIME,FileSize}` / `msg.Voice` non-nil) — `grep -c getUpdates` is **0** (ground truth = the Send reply). `multimodal_integration_test.go` (`//go:build multimodal_integration`) round-trips the live sidecars (the marquee TTS→STT audio loop + OCR vision). Both tiers `t.Fatal` under `$CI` when env is SET but unusable. `compose.yaml` gains `aura-stt` / `aura-tts` / `aura-ocr-vl` / `markitdown`. `ci.yml` adds a sidecar-gated `multimodal_integration` job + an operator-token-gated `telegram_integration` job (compile floor always runs; live step gated on the secret).

## Task Commits

1. **Task 1: mount channels Registry + setup server in bootServe/runServe** — `9c3631bf` (feat)
2. **Task 2: live integration tiers + compose sidecars + CI no-skip-as-green** — `e2fe7eb8` (test)

**Plan metadata:** (this commit) (docs: SUMMARY + STATE + ROADMAP)

_Task 1 was a `tdd="true"` task: the RED test + GREEN implementation were verified together (test compile-fail → impl → green) and committed as one cohesive feat commit, the dominant repo pattern._

## Files Created/Modified

- `cmd/aura/serve_channels.go` (created) — `bootChannelsAndSetup` (telegram channel + registry + setup server), `startChannelSubsystems`/`stopChannelSubsystems` (fail-soft lifecycle), `serveTelegramOverride` (--no-telegram/--only=cli), `setupStoreAdapter`, `telegramGetMeProbe`, `resolveLocalIdentityID`
- `cmd/aura/serve.go` (modified) — `serveEnv` gains the channels + setupSrv fields; `bootServe` builds them; `runServe` parses the flags, starts the subsystems after the AG-UI mount, and stops them before `env.close()`
- `cmd/aura/serve_test.go` (created) — fake-channel lifecycle proofs (StartAll/StopAll, fail-soft, idempotent stop, flag override), goleak-clean
- `internal/channels/telegram/integration_test.go` (created) — `telegram_integration` live Bot-API tier, response-asserted, no-skip-as-green
- `internal/channels/telegram/multimodal_integration_test.go` (created) — `multimodal_integration` live STT/OCR/TTS round-trip tier, no-skip-as-green
- `compose.yaml` (modified) — the 4 multimodal sidecar services (separate hunk from the parallel `aura-llama-embed -ub` change, which was deliberately left unstaged)
- `.github/workflows/ci.yml` (modified) — the two gated integration jobs

## Decisions Made

- **Bot token source:** the plan text referenced `chat.cfg.TelegramBotToken`, but the central `config.Config` has **no such field** — the token + throttles are owned by the telegram package (`telegram.LoadConfig()` reads `TELEGRAM_BOT_TOKEN` + `AURA_TELEGRAM_*_THROTTLE_MS`). Used `telegram.LoadConfig()`. This is a wiring clarification, not a behavioural deviation.
- **Cloud-vision config source:** the central config has no `OpenRouterBaseURL`/`OpenRouterAPIKey` — those live in `cfg.LLM`. The `photo.go` cloud branch is exercised by the telegram package's own `MultimodalConfig`; the serve composition root does not need to wire them for the mount (the channel's render path is the consumer). The setup `IdentityID` resolves the seeded `local` identity via `identity.Store.GetIdentityByName`.
- **serve.go split:** the channel/setup wiring went into `serve_channels.go` (refactor-on-touch); `serve.go` stays at 287 LOC, well under the 600 cap.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] setupStoreAdapter bridge for the cross-package param type mismatch**
- **Found during:** Task 1 (setup server wiring)
- **Issue:** `setup.Store.InsertPending` takes `setup.InsertPendingParams` but `*telegram.Store.InsertPending` takes `telegram.InsertPendingParams` — structurally identical, distinct packages (the patterns map flagged this as "the composition root adapts one onto the other in 13-09"). A direct `telegram.New(pool)` does not satisfy `setup.Store`.
- **Fix:** Added `setupStoreAdapter` in `serve_channels.go` — a thin field-copy projecting `setup.InsertPendingParams` onto `telegram.InsertPendingParams`, with `PendingConsumed`/`CountAccounts` delegating verbatim. `var _ setup.Store = setupStoreAdapter{}` proves conformance.
- **Files modified:** cmd/aura/serve_channels.go
- **Verification:** `go build ./...` + `go vet ./...` green; setup server builds at the composition root.
- **Committed in:** `9c3631bf` (Task 1 commit)

**2. [Rule 3 - Blocking] getUpdates literal removed from test comments**
- **Found during:** Task 2 (telegram_integration tier)
- **Issue:** The acceptance criterion is `grep -c getUpdates integration_test.go is 0`, but the doc comments mentioned "getUpdates" to explain the ground-truth rule, inflating the count to 4.
- **Fix:** Reworded the comments to "the poll/get-updates stream" / "the inbound update stream" — the test never calls getUpdates, and the literal grep is now 0. Behaviour unchanged.
- **Files modified:** internal/channels/telegram/integration_test.go
- **Verification:** `grep -c getUpdates internal/channels/telegram/integration_test.go` → 0; `go vet -tags telegram_integration` compiles.
- **Committed in:** `e2fe7eb8` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 3 — blocking wiring). No architectural change, no scope creep.
**Impact on plan:** Both were necessary to satisfy the explicit wiring contract + the literal acceptance criterion.

## Issues Encountered

- **Parallel Codex session collision in compose.yaml:** an uncommitted 2-line `aura-llama-embed -ub 1024` change from a concurrent Codex session shared the file. Handled per the objective's mandate: my 4 sidecar services were appended AFTER the existing services (a separate git hunk); I built a filtered patch (`git apply --cached` with only my services+volumes hunk) and DECLINED the `-ub`/`1024` hunk. Verified `git diff --cached compose.yaml` contains 0 occurrences of `-ub`/`1024`; the Codex change remains unstaged + intact in the working tree. `.planning/spikes/MANIFEST.md` (modified) and `.planning/spikes/032-logo-manual-live-ingest/` (untracked) were also left untouched.

## Gate-3 Checkpoint — PENDING (Task 3, orchestrator-owned)

This plan is **NOT complete**. Task 3 is a `checkpoint:human-verify` (`gate="blocking"`) owned by the orchestrator. UX-02/03/04 are **NOT** marked complete. The orchestrator must, before operator sign-off:

1. **Full tag matrix live:** `go test -race -tags 'db_integration telegram_integration multimodal_integration' ./...` with Postgres + the 3 sidecars (`make ...` / compose up `aura-stt aura-tts aura-ocr-vl`) + the operator bot token (`TELEGRAM_BOT_TOKEN` + `AURA_E2E_CHAT_ID`) up. The no-skip-as-green guards must FIRE (not a sub-second skip).
2. **Coverage:** `make coverage` — owned-surface ≥85% (CLAUDE.md floor) across the full matrix.
3. **Mutation:** a go-mutesting spot-check ≥70% on `internal/channels/telegram/mdv2.go` + `renderer.go` (WSL go-mutesting, PASS=killed).
4. **Snapshot:** append the full-matrix/coverage/mutation rows to `docs/aura-quality-snapshot.md`.
5. **Operator sign-off** on the 5 live manual-only behaviours (setup token 401/200, live render no-400, /cancel, voice + image, onboarding deep-link).

What is DONE + green (this plan's scope): `go build ./...`, `go vet ./...`, `go vet -tags 'telegram_integration multimodal_integration'` (both tiers COMPILE), `go test ./cmd/aura/ -run 'Serve|Boot|Channel|Setup'`, `go test -race ./cmd/aura/ ./internal/channels/... ./internal/setup/...`, and `golangci-lint run` (0 issues, default + integration tags). The sidecar/bot-dependent live tiers were deliberately NOT run here — they are the orchestrator's Gate-3.

## Next Phase Readiness

- The phase is wired into `aura serve`; the only remaining work for Phase 13 closure is the Gate-3 live matrix + operator sign-off (Task 3).
- The setup `IdentityID` resolves the seeded `local` identity at boot; a fresh DB before the seed logs a WARN and the setup server still boots (fail-soft) — Phase 14 onboarding consumes the same `telegram_setup_pending` surface.
- **A2 caveat (carried from 13-08):** the markitdown `/convert` request/response shape is `[ASSUMED]`; the live `multimodal_integration` tier does NOT exercise markitdown (it round-trips STT/OCR/TTS), so a real markitdown image mismatch would surface at Gate-3 or in Phase 14 — isolated to `documents.go::postConvert` for a one-line fix.

---

## Self-Check: PASSED

- FOUND: cmd/aura/serve_channels.go
- FOUND: cmd/aura/serve_test.go
- FOUND: internal/channels/telegram/integration_test.go
- FOUND: internal/channels/telegram/multimodal_integration_test.go
- FOUND: .planning/phases/13-channels-telegram-multimodal/13-09-SUMMARY.md
- FOUND commit: 9c3631bf (Task 1, feat)
- FOUND commit: e2fe7eb8 (Task 2, test)

---
*Phase: 13-channels-telegram-multimodal*
*Completed (Tasks 1-2): 2026-06-08 — Task 3 Gate-3 checkpoint PENDING (orchestrator-owned)*
