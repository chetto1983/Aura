---
phase: 13
slug: channels-telegram-multimodal
status: audited
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-08
last_audit: 2026-06-08
validated_by: gsd-validate-phase 13
---

# Phase 13 - Validation Strategy

Per-phase validation contract for feedback sampling during execution.

Audit result: Phase 13 is Nyquist-compliant. The old Wave-0 draft rows were stale;
all planned automated coverage now exists and the live-only surfaces are either
covered by live component tiers or explicitly recorded as manual/operator evidence.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + package `goleak` `TestMain`; `net/http/httptest` for HTTP/SSE and sidecars; fuzz via `testing.F`; race detector for channel and daemon lifecycle |
| Config file | none for unit tests; Go build tags select live tiers |
| Quick run command | `go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/ -count=1` |
| Phase-13 race command | `go test -race ./internal/channels/telegram/ ./internal/agui/ ./internal/config/ ./cmd/aura/` |
| Full suite command | `go test ./... -count=1` plus live tag matrix when sidecars/operator token are available |
| Live tag tiers | `db_integration`, `telegram_integration`, `multimodal_integration` |

### Test Tiers

- Unit: registry lifecycle, Telegram bot dispatch, commands, HITL, renderer, status pane,
  artifact delivery, setup API/SSE, multimodal sidecar clients, send_file substrate.
- Race/goleak: Telegram package, AG-UI fanout, setup SSE pump, channel registry,
  daemon/channel mount.
- `db_integration`: Telegram onboarding token/account store round trip.
- `telegram_integration`: live Bot-API response assertions for sendPhoto/sendDocument/sendVoice.
- `multimodal_integration`: live STT/OCR/TTS/document sidecar round trips.

---

## Sampling Rate

- After every task commit: package-level `go test` plus `go vet`/build or pre-commit hooks.
- After every plan wave: `go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/`.
- Before phase closure: `go test -race ./internal/channels/telegram/ ./internal/agui/ ./internal/config/ ./cmd/aura/`,
  `go test ./... -count=1`, lint/build hooks, and the documented live E2E tiers.
- Max feedback latency: quick tier remains below 30s on this workstation.

---

## Per-Task Verification Map

| Req | Behavior | Test Type | Automated Command | Evidence Files | Status |
|-----|----------|-----------|-------------------|----------------|--------|
| UX-02 | Channel interface + registry `StartAll`/`StopAll` + `AURA_CHANNEL_*_ENABLED` + error aggregation | unit + goleak | `go test ./internal/channels/` | `internal/channels/registry_test.go`, `internal/channels/main_test.go` | PASS |
| UX-02 | Per-turn fanout consumption maps AG-UI events to status-pane/content/artifact render | unit + race | `go test -race ./internal/channels/telegram/ ./internal/agui/` | `internal/channels/telegram/agui_subscriber_test.go`, `internal/channels/telegram/status_pane_test.go`, `internal/channels/telegram/bot_test.go` | PASS |
| UX-02 | MarkdownV2 escaper: 10K Unicode corpus/fuzz, no parse-entity 400 class; plain fallback | unit + fuzz + mutation | `go test ./internal/channels/telegram/ -run 'Mdv2|Markdown|Fallback'` | `internal/channels/telegram/mdv2_test.go`, `internal/channels/telegram/renderer_test.go` | PASS |
| UX-02 | Tables render to deterministic PNG; table content goes through sendPhoto | golden + integration | `go test ./internal/channels/telegram/ -run 'Table|Photo'` | `internal/channels/telegram/tables_test.go`, `internal/channels/telegram/renderer_table_test.go`, `internal/channels/telegram/integration_test.go` | PASS |
| UX-02 | `send_file` -> artifact event -> sendDocument; >50MB/error paths are explicit | unit + integration | `go test ./internal/agent/... ./internal/agui/... ./internal/channels/telegram/ -run 'SendFile|Artifact'` | `internal/agent/tools/send_file_test.go`, `internal/agent/llm_agent_events_artifact_test.go`, `internal/agui/translator_artifact_test.go`, `internal/channels/telegram/artifact_test.go`, `internal/channels/telegram/agui_subscriber_test.go` | PASS |
| UX-02 | 10 Telegram commands intercepted before LLM; `/cost` == CLI, `/search` == CLI, `/cancel` ctx cancel | unit + live CDP command check | `go test ./internal/channels/telegram/ -run 'Command|Cost|Search|Cancel'` | `internal/channels/telegram/commands_test.go`, `internal/channels/telegram/bot_dispatch_test.go`; live Telegram Web: `/search meteo`, `/cost`, `/cancel` no-turn all replied at 20:16-20:27 on 2026-06-08 | PASS |
| UX-02 | HITL options -> InlineKeyboard, no-options -> ForceReply, callback/reply -> same runner resume | unit | `go test ./internal/channels/telegram/ -run 'HITL|Callback|Reply'` | `internal/channels/telegram/hitl_test.go`, `internal/channels/telegram/bot_dispatch_hitl_test.go`, `internal/channels/telegram/agui_subscriber_test.go` | PASS |
| UX-02 | Bot polling goroutine and async document conversion are goleak-clean | unit + goleak + race | `go test -race ./internal/channels/telegram/` | `internal/channels/telegram/main_test.go`, `internal/channels/telegram/bot_test.go`, `internal/channels/telegram/documents_test.go` | PASS |
| UX-03 | Setup token gate: 401 without token, 200 with token, 401 after onboard complete | unit + httptest | `go test ./internal/setup/ -run 'Gate|Token'` | `internal/setup/server_test.go`, `internal/setup/events_test.go` | PASS |
| UX-03 | `/setup/token` getMe validate; `/setup/onboard-link` deep link; `/setup/status` | unit + httptest | `go test ./internal/setup/ -run 'Handle|Token|OnboardLink|Status'` | `internal/setup/handlers_test.go` | PASS |
| UX-03 | SSE `/setup/events` emits `onboarding_completed`; pump exits cleanly | unit + goleak | `go test ./internal/setup/ -run 'Events|SSE'` | `internal/setup/events_test.go`, `internal/setup/main_test.go` | PASS |
| UX-03 | Onboarding round trip: single-use pending token -> `telegram_accounts`; cleanup expired | `db_integration` | `go test -tags db_integration ./internal/channels/telegram/ ./internal/setup/` | `internal/channels/telegram/store_integration_test.go`, `internal/channels/telegram/onboarding_test.go` | PASS |
| UX-04 | Voice STT OGG/Opus direct, retry/backoff, hard-fail UX; OnVoice drives a turn | unit + live sidecar tier | `go test ./internal/channels/telegram/ -run 'Voice|STT|OnVoice'` | `internal/channels/telegram/voice_test.go`, `internal/channels/telegram/bot_dispatch_media_test.go`, `internal/channels/telegram/multimodal_integration_test.go` | PASS |
| UX-04 | TTS Kokoro opus -> sendVoice, ASCII caption, TTS-out after voice-mode/inbound voice | unit + live sidecar tier | `go test ./internal/channels/telegram/ -run 'TTS|ShouldSpeak'` | `internal/channels/telegram/tts_test.go`, `internal/channels/telegram/agui_subscriber_test.go`, `internal/channels/telegram/multimodal_integration_test.go` | PASS |
| UX-04 | Photo routing: local sidecar vs cloud vision branch; OnPhoto drives a turn | unit + live sidecar tier | `go test ./internal/channels/telegram/ -run 'Photo|Vision|OnPhoto'` | `internal/channels/telegram/photo_test.go`, `internal/channels/telegram/photo_resize_test.go`, `internal/channels/telegram/bot_dispatch_media_test.go`, `internal/channels/telegram/multimodal_integration_test.go` | PASS |
| UX-04 | Document conversion tiers: <=5MB sync, 5-50MB async, >50MB refuse; OnDocument drives turn/refusal | unit + live sidecar tier | `go test ./internal/channels/telegram/ -run 'Document|Convert|Tier|OnDocument'` | `internal/channels/telegram/documents_test.go`, `internal/channels/telegram/bot_dispatch_media_test.go`, `internal/channels/telegram/multimodal_integration_test.go` | PASS |
| UX-02/03/04 | `aura serve` mounts channels Registry, Telegram channel, setup server, and config/deps fail-soft | unit + race | `go test -race ./cmd/aura/ ./internal/config/` | `cmd/aura/serve_test.go`, `cmd/aura/serve_channels.go`, `internal/config/config_channels_test.go` | PASS |

---

## Wave 0 Requirements

- [x] `internal/channels/main_test.go`, `internal/channels/telegram/main_test.go`, and
  `internal/setup/main_test.go` run package tests under `goleak.VerifyTestMain`.
- [x] AG-UI event -> Telegram render coverage exists for status pane, renderer, artifact,
  and pending-pause/HITL rendering.
- [x] MarkdownV2 escaper has a 10K Unicode seed corpus and `FuzzMdv2`.
- [x] Table-PNG deterministic structural goldens cover 4-col and 6-col tables.
- [x] Mock sidecar HTTP servers cover STT, TTS, OCR/vision, and markitdown unit tiers.
- [x] Live integration tiers exist for `telegram_integration` and `multimodal_integration`,
  with no-skip-as-green behavior under CI.
- [x] No extra test framework was installed.

---

## Manual-Only / Operator Evidence

| Behavior | Requirement | Evidence | Status |
|----------|-------------|----------|--------|
| Live setup wizard token gate and onboard-link | UX-03 | Prior Gate-3 E2E: setup-wizard :9081 5/5 in `docs/aura-quality-snapshot.md` | ACCEPTED |
| Live Telegram render surface without MarkdownV2 400 | UX-02 | Prior live Bot-API response tests and mdv2/renderer mutation/fuzz evidence; current live chat accepted command replies | ACCEPTED |
| Live `/search`, `/cost`, `/cancel` command route | UX-02 | Current CDP run against logged-in Telegram Web `Aura_bot`: `/search meteo` -> `Nessun risultato.`, `/cost` -> spend summary, `/cancel` -> no-running-turn confirmation | ACCEPTED |
| Mid-tool `/cancel` abort | UX-02 | Automated command registry/ctx-cancel tests cover the propagation; live no-turn command route validated. A deterministic live long-tool interruption needs an MTProto/userbot harness and remains an optional E2E extension, not a Nyquist gap. | ACCEPTED |
| Voice note, photo, document inbound dispatch | UX-04 | Unit dispatch tests prove OnVoice/OnPhoto/OnDocument -> clients -> turn/refusal; live component tiers prove STT/OCR/TTS/document sidecars. CDP cannot upload Telegram media deterministically in this session. | ACCEPTED |
| HITL button/reply resume | UX-02 | Unit callback/reply tests cover same-runner resume, callback toasts, keyboard disarm, and 64-byte callback_data guard. CDP click on older visible buttons did not produce deterministic Telegram Web callback evidence. | ACCEPTED |

---

## Validation Audit 2026-06-08

| Metric | Count |
|--------|-------|
| Requirements audited | 17 |
| Automated gaps found | 0 |
| Tests generated in this audit | 0 |
| Validation doc drift fixed | 1 |
| Manual/operator rows accepted | 6 |
| Escalated | 0 |

Commands run during this audit:

```bash
go test -race ./internal/channels/telegram/ ./internal/agui/ ./internal/config/ ./cmd/aura/
go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/ -count=1
go test ./... -count=1
git diff --check
```

Additional live evidence collected through CDP on the logged-in Telegram Web tab:

```text
/search meteo -> Nessun risultato.
/cost -> Spesa di oggi: $0.018500 (172488 token in, 0 token out)
/cancel -> Nessun turno in corso da annullare.
```

---

## Validation Sign-Off

- [x] All task behaviors have automated verification or an explicit accepted manual/operator row.
- [x] Sampling continuity preserved; no phase segment has three consecutive tasks without automated verification.
- [x] Wave 0 requirements are complete.
- [x] No watch-mode flags are used in verification commands.
- [x] Quick feedback tier remains below 30s.
- [x] `nyquist_compliant: true` set in frontmatter.

Approval: validated by `$gsd-validate-phase 13` on 2026-06-08.
