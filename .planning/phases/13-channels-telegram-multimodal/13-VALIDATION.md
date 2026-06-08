---
phase: 13
slug: channels-telegram-multimodal
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-08
---

# Phase 13 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `13-RESEARCH.md` §Validation Architecture. Per-task rows are filled
> by the planner (task IDs assigned at plan time); the requirement→tier map below is binding.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `goleak` (mandatory, amendment #15) + `net/http/httptest` for HTTP/SSE; property/fuzz via `testing.F`; `testing/synctest` for throttle timing |
| **Config file** | none (go test); build tags select tiers |
| **Quick run command** | `go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/` |
| **Full suite command** | `go test -race -tags 'db_integration telegram_integration multimodal_integration' ./...` then `make coverage` (owned-surface floor ≥85%) |
| **Estimated runtime** | unit ~tens of s; full matrix minutes (sidecars + live bot) |

### Test tiers (build tags)
- **unit** (no tag, always in CI): escaper fuzz, table-PNG golden, renderer golden fixtures per AG-UI event, command dispatch (no LLM), registry lifecycle, `photo.go` env-branch with mock sidecar, setup token-gate (httptest), fanout consumption with synthetic event stream.
- **`db_integration`**: onboarding token round-trip (INSERT `telegram_accounts`, consume pending, cleanup expired), Store CRUD against real PG.
- **`telegram_integration`** (NEW — live bot, operator token only): `sendPhoto`/`sendDocument`/`sendVoice` round-trip asserting on the Bot-API **response** (`msg.Photo` / `msg.Document.{FileName,MIME,FileSize}` / `msg.Voice` non-nil) — ground truth = response, never `getUpdates` (spike 017/019). `t.Fatal` under `$CI` when token env set but unusable (no-skip-as-green).
- **`multimodal_integration`** (NEW — requires the 3 sidecars up): voice-note IT round-trip (STT), photo IT OCR recall, `sendVoice` (TTS). Skipped without containers; `t.Fatal` under `$CI` if env set but sidecars unreachable.

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/` (CLAUDE.md post-edit gate)
- **After every plan wave:** `go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/` + `golangci-lint run`
- **Before `/gsd-verify-work`:** full tag matrix live + `make coverage` ≥85% owned-surface + mutation spot-check ≥70% on the escaper + renderer critical files
- **Max feedback latency:** quick tier < 30 s

---

## Per-Task Verification Map

> Task IDs (`13-NN-MM`) are assigned by the planner. Each row below is a requirement-level
> contract the planner must attach to at least one task's `<acceptance_criteria>`.

| Req | Behavior | Threat Ref | Secure Behavior | Test Type | Automated Command | File | Status |
|-----|----------|-----------|-----------------|-----------|-------------------|------|--------|
| UX-02 | Channel interface + registry StartAll/StopAll + `AURA_CHANNEL_*_ENABLED` + error aggregation | — | per-channel enable gate | unit + goleak | `go test ./internal/channels/` | ❌ W0 | ⬜ |
| UX-02 | Per-turn fanout consumption maps AG-UI events → status-pane-B render | — | N/A | unit (golden per event, incl. microcompact pointer) | `go test ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-02 | MarkdownV2 escaper: 10K Unicode fuzz, no `400 can't parse entities`; pre-fence pipes/dashes unescaped | T-MdV2 | entity-aware escape + plain-text fallback | fuzz (`testing.F`) | `go test -run Fuzz -fuzz FuzzMdv2 -fuzztime 30s ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-02 | tables.go 4-col/6-col → deterministic PNG (dims+grid); pre-block fallback ≤56 char/row | — | N/A | golden + `telegram_integration` sendPhoto | `go test ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-02 | `send_file` → artifact event → sendDocument (xlsx/pdf, MIME exact, >50MB error) | T-Artifact | error not silent truncate | unit (emit) + `telegram_integration` | `go test -tags telegram_integration ./...` | ❌ W0 | ⬜ |
| UX-02 | 10 commands bot-intercept, no LLM; `/cost`==CLI, `/search`==CLI (cross-slice invariant) | — | N/A | unit | `go test ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-02 | HITL: options→InlineKeyboard, none→ForceReply, callback→resume; throttle via synctest | — | N/A | unit (`testing/synctest`) | `go test ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-02 | bot polling goroutine goleak-clean | — | N/A | unit goleak | `go test ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-03 | Setup token gate: 401 without / 200 with / 401 after onboard-complete | T-SetupToken | mandatory token middleware on all `/setup/*` | unit (httptest) | `go test ./internal/setup/` | ❌ W0 | ⬜ |
| UX-03 | `/setup/token` getMe validate; `/setup/onboard-link` `{deep_link,qr_svg}`; `/setup/status` | T-BotTokenLeak | never log token | unit (httptest + telebot Offline) | `go test ./internal/setup/` | ❌ W0 | ⬜ |
| UX-03 | SSE `/setup/events` poll-2s emits `onboarding_completed`; pump goleak-clean | — | N/A | unit (httptest SSE) + goleak | `go test ./internal/setup/` | ❌ W0 | ⬜ |
| UX-03 | onboarding round-trip INSERT `telegram_accounts` + cleanup expired (single-use, 1h TTL) | T-TokenReplay | `consumed_at` single-use | `db_integration` | `go test -tags db_integration ./internal/channels/telegram/ ./internal/setup/` | ❌ W0 | ⬜ |
| UX-04 | voice.go STT OGG/Opus direct, 2-retry, hard-fail UX | T-SidecarDoS | HTTP timeout 30s | unit (mock) + `multimodal_integration` | `go test -tags multimodal_integration ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-04 | tts.go Kokoro opus→sendVoice, ASCII caption | — | N/A | unit (mock) + `multimodal_integration` (msg.Voice non-nil) | `go test -tags multimodal_integration ./...` | ❌ W0 | ⬜ |
| UX-04 | photo.go `AURA_VISION_CLOUD`: false→sidecar mock called, true→cloud called (no sidecar) | — | single runtime branch | unit (both branches, mock) | `go test ./internal/channels/telegram/` | ❌ W0 | ⬜ |
| UX-04 | documents.go tiered ≤5MB sync / 5-50MB async / >50MB refuse; async goleak-clean | T-SidecarDoS | refuse >50MB | unit (mock markitdown) + goleak | `go test ./internal/channels/telegram/` | ❌ W0 | ⬜ |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/channels/main_test.go` + `internal/channels/telegram/main_test.go` + `internal/setup/main_test.go` — `TestMain` with `goleak.VerifyNone` (amendment #15)
- [ ] Golden fixtures dir — one AG-UI-event→Telegram-message fixture per event type (incl. microcompact pointer case, PRD acceptance)
- [ ] Escaper 10K-Unicode seed corpus + `FuzzMdv2`
- [ ] Table-PNG golden bytes (4-col, 6-col) — deterministic dims+grid
- [ ] Mock sidecar HTTP servers (httptest) for STT / TTS / OCR-VL / markitdown unit tiers
- [ ] CI job additions: `telegram_integration` (operator-token-gated, optional) + `multimodal_integration` (sidecar-gated) — both `t.Fatal`-under-`$CI`-when-env-set per no-skip-as-green
- [ ] No new framework install (Go stdlib + goleak already in tree)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live Telegram turn render (status pane B, no `400 can't parse entities`) | UX-02 | Needs a real bot + human read of formatting fidelity | Operator sends a message; observe status pane + content reply render correctly |
| `/cancel` mid-tool aborts + Aura confirms | UX-02 | Needs a long-running tool + ctx-cancel propagation observed live | Operator runs a long tool, sends `/cancel`, observes immediate abort |
| Voice note → transcribe → (optional) voice reply | UX-04 | Needs `aura-stt`/`aura-tts` sidecars + a real OGG/Opus voice note | Operator sends an IT voice note; observe transcription + optional `sendVoice` reply |
| Image + caption → description / OCR | UX-04 | Needs `aura-ocr-vl` (or `AURA_VISION_CLOUD=true`) + visual judgement | Operator sends an image "what's in this picture?"; observe description |
| Terminal ASCII QR onboarding deep-link | UX-03 | QR scan is a physical action | Operator scans the ASCII QR / clicks `t.me/<bot>?start=<token>`, observes onboarding completion |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (quick tier)
- [ ] `nyquist_compliant: true` set in frontmatter (after Wave 0 complete)

**Approval:** pending
