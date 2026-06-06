---
phase: 12
slug: ag-ui-gateway
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-06
---

# Phase 12 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + rapid (property-based) + goleak |
| **Config file** | none — standard `go test`; tagged tiers per repo convention |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/agui/ ./internal/llm/openai_compat/ ./internal/agent/` |
| **Full suite command** | `go test -race ./... && go test -tags db_integration -race -p 1 ./internal/agui/ ./internal/conversations/` (DSNs from POSTGRES_PASSWORD per repo convention) |
| **Estimated runtime** | ~30s quick / ~120s full (stack up) |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test -race ./internal/agui/ ./internal/llm/openai_compat/ ./internal/agent/`
- **After every plan wave:** Run full suite command (incl. db_integration tier with stack up)
- **Before `/gsd-verify-work`:** Full suite green + live smoke (aura serve + curl SSE incl. REASONING_*) + boundary gate + pin grep
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

*Pre-locked by the planner — one row per task across all six plans (12-01…12-06; 12-05/12-06 = reasoning data-plane per amendment #57). Status flips green at execution time; `File Exists` flips when the artifact lands (Wave 0 markers stay ❌ until the Wave-0 artifacts ship).*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| SC-4 (pin) | 12-01 | 1 | UX-01 | T-12-02, T-12-SC | immutable dep pin | ci-gate | `grep -cF 'v0.0.0-20260514093510-e9e910b230b9' go.mod` → exactly `1` | ❌ W0 | ⬜ pending |
| SC-2 (boundary) | 12-01 | 1 | UX-01 | T-12-01 | one-way agent→agui import closure | ci-gate | `bash scripts/agui_boundary_check.sh` (go list -deps; exit 1 on violation) | ❌ W0 | ⬜ pending |
| Golden fixtures | 12-01 | 1 | UX-01 | — | 21 verified wire shapes seeded (incl. 5 REASONING_*) | static | `grep -q RUN_STARTED internal/agui/testdata/golden-events.json` (+ build green) | ❌ W0 | ⬜ pending |
| Translator state machine | 12-01 | 1 | UX-01 | T-12-03, T-12-04 | valid AG-UI seq; skip empty deltas; non-empty ids; sorted StateDelta; tool-result≠TEXT_MESSAGE | unit/property | `go test -race -run TestTranslatorProperty ./internal/agui/` (rapid + golden) | ❌ W0 | ⬜ pending |
| Reasoning wire dual-field | 12-05 | 1 | UX-01 | T-12-18 | wireChunk.Delta accept-both reasoning/reasoning_content → SAME emitted Chunk{Reasoning}; immediate token-per-token; no leak into content | unit | `go test -race -run TestParseSSEReasoning ./internal/llm/openai_compat/` (two golden fixtures → identical events, #33) | ⬜ | ⬜ pending |
| Reasoning Chunk + Event | 12-05 | 1 | UX-01 | T-12-17 | llm.Chunk.Reasoning + agent.LLMResponse.Reasoning additive; reasoningChunkEvent mirror; consume case; round-trip symmetric; stream-only (no content leak) | unit | `go test -race -run 'TestEventRoundTrip|TestConsumeReasoning' ./internal/agent/` | ⬜ | ⬜ pending |
| Translator REASONING lifecycle | 12-06 | 2 | UX-01 | T-12-20 | REASONING_START/MESSAGE_START/CONTENT*/MESSAGE_END/END; rsn- messageId; coalesced; interleave-before-TEXT; clean close on interruption; Validate() passes | unit/property | `go test -race -run TestTranslatorProperty ./internal/agui/` (reasoning invariants + 5 REASONING golden shapes) | ⬜ | ⬜ pending |
| CLI live 💭 reasoning render | 12-06 | 2 | UX-01 | T-12-19 | renderRunnerTurn streams dim 💭 reasoning live; stream-only (not in answer/prose buffer) | unit | `go test -race -run TestRenderRunnerTurn ./cmd/aura/` + `grep -c '💭' cmd/aura/chat_render.go` ≥1 | ⬜ | ⬜ pending |
| Fanout (drop-on-full) | 12-02 | 2 | UX-01 | T-12-05 | cap-64 buffered, drop+WARN, never block producer | unit | `go test -race -run TestFanout ./internal/agui/` (goleak) | ⬜ | ⬜ pending |
| Fanout/client leak | 12-02 | 2 | UX-01 | T-12-06 | sole-sender-closes; ctx.Done arm; exit on cancel + source end | unit | `go test -race ./internal/agui/` (goleak TestMain; NumGoroutine baseline) | ⬜ | ⬜ pending |
| Client subscriber seam | 12-02 | 2 | UX-01 | T-12-07 | transport-free in-proc subscriber; SDK aliases shield call sites | unit | `go test -race -run TestClientSubscriberRoundTrip ./internal/agui/` | ⬜ | ⬜ pending |
| SC-1 (server SSE) | 12-03 | 3 | UX-01 | T-12-09, T-12-12 | loopback bind; MaxBytesReader; cap-64 SSE pump; RUN_STARTED…RUN_FINISHED | integration | `go test -tags db_integration -race -p 1 ./internal/agui/` (httptest + Postgres) + live `scripts/agui_smoke.sh` | ⬜ | ⬜ pending |
| SC-3 (GET snapshot) | 12-03 | 3 | UX-01 | T-12-11 | thread 404 on unknown id; MESSAGES_SNAPSHOT JSON matches persisted turns | integration | `go test -tags db_integration -race -p 1 -run TestThreadMessages ./internal/agui/` | ⬜ | ⬜ pending |
| Server unit tier (404/400/disconnect) | 12-03 | 3 | UX-01 | T-12-12 | 404/400 guards; over-cap body → 400; disconnect → pump exits | unit | `go test -race -run TestServer ./internal/agui/` (in-memory fakes, goleak) | ⬜ | ⬜ pending |
| RUN_ERROR DSN redaction | 12-03 | 3 | UX-01 | T-12-10 | sanitizeErr strips DSN/key/path before the wire | unit | `go test -race -run TestServerRunErrorRedaction ./internal/agui/` (synthetic DSN → no `secret`) | ⬜ | ⬜ pending |
| AURA_AGUI_* config | 12-03 | 3 | UX-01 | T-12-08, T-12-13 | loopback default; restrictive CORS; buffer-cap env | unit | `grep -c 'AURA_AGUI_BIND' internal/config/config.go` ≥1 + `grep -c 'AURA_AGUI_BUFFER_CAP' internal/config/config.go` ≥1 + `go test -race ./internal/config/` | ⬜ | ⬜ pending |
| serve.go daemon mount | 12-03 | 3 | UX-01 | T-12-08, T-12-09 | http.Server mounted on scheduler daemon; graceful Shutdown; no --bind flag | unit | `grep -c 'agui.NewServer' cmd/aura/serve.go` ==1 + `grep -c 'Shutdown' cmd/aura/serve.go` ≥1 + `go build ./...` | ⬜ | ⬜ pending |
| Smoke + CI tier + coverage/mutation | 12-04 | 4 | UX-01 | T-12-14, T-12-15 | no-skip-as-green; FRAME ground truth not secrets; REASONING_* on live leg | smoke/ci-gate | `go test -tags db_integration -race -count=1 -p 1 ./internal/agui/...` (autonomous) + `bash scripts/agui_smoke.sh` (operator) | ❌ W0 | ⬜ pending |
| Operator live Gate-3 | 12-04 | 4 | UX-01 | — | live OpenRouter SSE round-trip + REASONING_* lifecycle + live 💭 render + GET (no CoT persisted) + 404 + graceful shutdown | human-verify | `bash scripts/agui_smoke.sh` (live, OPENROUTER_API_KEY set) + operator sign-off | ⬜ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@e9e910b230b9329c905e31ca024b4114dedf7918` + transitive-sums `go get .../pkg/core/events@v0.0.0-20260514093510-e9e910b230b9` (spike 014 two-step)
- [ ] `internal/agui/testdata/` seeded from `.claude/skills/spike-findings-Aura/sources/015-agui-event-surface/golden-events.json` (21 golden wire shapes, incl. the 5 REASONING_* shapes asserted by Plan 12-06)
- [ ] `internal/agui/translator_test.go` — property-based, covers UX-01 translator obligations (seed from golden fixtures)
- [ ] Boundary-gate script + CI wiring (SC-2) — exists before any `internal/agui` code lands
- [ ] Pin-grep CI gate (SC-4)

> Wave-0 reconciliation (Blocker 2): the strict Wave-0 test artifact is `translator_test.go` (+ the boundary script + golden fixtures). `server_test.go` is NOT a Wave-0 artifact — it is authored in Plan 03 (Wave 3) alongside `server.go`, since it cannot exist before `NewServer`/`Mux` do. The RESEARCH §Wave 0 Gaps mention of `server_test.go` is a forward reference, reconciled here (RESEARCH.md not edited).

> Reasoning data-plane note (amendment #57): the llm+agent reasoning data-plane (Plan 12-05) is a NEW Wave-1 plan with ZERO file overlap with `internal/agui` — it runs in parallel with 12-01. The translator REASONING lifecycle + CLI render (Plan 12-06, Wave 2) depend on BOTH 12-01 (translator.go/types.go) and 12-05 (LLMResponse.Reasoning), so they land after both — compile-order safe. The 5 REASONING golden shapes are seeded by 12-01 (Wave 1) and asserted by 12-06 (Wave 2). Reasoning is STREAM-ONLY: never persisted, no semantic parse, byte-per-byte (#33 invariant) — the no-leak-into-content invariant is gated in BOTH 12-05 (consume loop / accumulate) and 12-06 (chat_render answer buffer).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Mutation spot-check ≥70% on translator.go | Gate 3 DoD | go-mutesting runs on WSL, manual per repo convention | WSL: `PATH=~/go/bin:$PATH go-mutesting ./internal/agui/translator.go` — PASS=killed, score=killed/total, autopsy survivors before chasing score; the REASONING lifecycle branch (Plan 12-06) is part of this surface |
| Live operator smoke (SC-1/SC-3 by hand) | UX-01 | Operator-observable per ROADMAP wording | `./aura serve` then curl per SC-1/SC-3 rows (or `bash scripts/agui_smoke.sh` live); inspect SSE frames visually (≥1 body print) |
| Live REASONING_* + 💭 render (amendment #57) | UX-01 | Reasoning streaming is operator-observable; needs a reasoning-capable live model (OpenRouter-gated) | live `./aura serve` + curl → confirm `event: REASONING_START`…`REASONING_END` interleave BEFORE the first `TEXT_MESSAGE_START`, reasoning NOT mixed into the answer, NOT persisted in the GET snapshot; `./aura chat` → confirm live dim 💭 render before the answer |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter (map pre-locked by the planner)
- [ ] `wave_0_complete: true` (flips at execution time once Wave-0 artifacts land)

**Approval:** pending (map pre-locked; Status cells flip during execution)
