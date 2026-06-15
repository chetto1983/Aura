---
phase: 03-llm-client-toolresult
plan: 05
subsystem: cmd-aura-chat-config
tags: [cli, repl, chat, config, otel, smoke, acceptance, coverage]
requires:
  - "internal/agent.LlmAgent + LlmAgentConfig + InvocationContext (Plan 04)"
  - "internal/agent.NewTracerProvider (Plan 04)"
  - "internal/llm/openai_compat.New (Plan 02)"
  - "internal/llm.Config + CostUSD (Plan 01)"
  - "internal/agent/tools.Registry (Plan 03)"
provides:
  - "aura chat — interactive in-memory REPL (token-by-token clean prose, cost footer, two-stage Ctrl+C, OTel flush on exit)"
  - "aura config show/get/set over ~/.aura/llm.json (redacted show; refuses llm.api_key)"
  - "scripts/llm_smoke.sh — manual real-OpenRouter acceptance gate (OPENROUTER_API_KEY-gated, NOT CI)"
affects:
  - "Phase 4+ (chat history is in-memory only this phase; persistence is Phase 4)"
tech-stack:
  patterns:
    - "renderTurn streams prose from chunk + final Events; skips tool_call_id-stamped tool-result Events (clean prose, D-12/D-13)"
    - "signal.NotifyContext two-stage SIGINT (D-10): first abort discards partial assistant msg (D-29), second quits"
    - "per-turn token+USD footer via llm.CostUSD (honest n/a, never $0, D-11)"
    - "manual live smoke as the only real-provider gate; deterministic tier proves all logic (no-skip-as-green)"
key-files:
  created:
    - "cmd/aura/chat.go"
    - "cmd/aura/chat_render.go"
    - "cmd/aura/config.go"
    - "cmd/aura/chat_test.go"
    - "cmd/aura/config_test.go"
    - "cmd/aura/cover_test.go"
    - "cmd/aura/main_test.go"
    - "scripts/llm_smoke.sh"
  modified:
    - "cmd/aura/main.go (chat + config subcommand wiring)"
    - "internal/agent/tracing.go (NewTracerProvider export + silent-drop otlp error handler)"
    - "internal/agent/llm_agent_events.go (tool-result Event tool_call_id marker)"
    - "internal/agent/llm_agent.go (consume() iter.Seq2 stop-propagation fix)"
decisions:
  - "The human-verify checkpoint was satisfied via an operator-authorized live run: the product owner set all env in .env and directed a full E2E run. scripts/llm_smoke.sh passed against live OpenRouter (deepseek-v4-flash:exacto) — clean streamed Italian prose, a non-zero token+USD footer, current_time tool turn, clean exit — verified by visual inspection of the captured output, not just exit status."
  - "Three live-surfaced rendering/observability defects and two latent production bugs were fixed BEFORE closing (see Deviations) — the checkpoint exists precisely to catch the human/integration dimension the deterministic tier cannot."
metrics:
  duration: "~3h (impl + live acceptance + defect fixes + coverage hardening)"
  completed: "2026-05-30"
  tasks: 3
  files: 8
---

# Phase 3 Plan 05: aura chat/config + Live Acceptance Summary

Landed the user-facing surface (D-01 commit 7) and drove the live SPEC Req#11 /
ROADMAP SC#1 acceptance to a genuine PASS: `aura chat` interactive REPL streaming
clean prose from DeepSeek-V4-Flash via OpenRouter with a per-turn token+USD cost
footer, two-stage Ctrl+C, `read_tool_output`/`current_time` builtins, OTel spans;
`aura config show/get/set`; and `scripts/llm_smoke.sh` as the manual real-provider
gate. The human-verify checkpoint was exercised live and is satisfied.

## What Was Built

### Task 1 — REPL + config (commit `4a5f312c`)
- `cmd/aura/chat.go` (230 LOC) + `chat_render.go` (176): the testable `chatLoop`
  core driving `LlmAgent.Run`, token-by-token prose via `renderTurn`, the D-11
  cost footer, two-stage SIGINT via `signal.NotifyContext` (D-10/D-29), OTel
  TracerProvider wired from `AURA_OTEL_EXPORTER` and flushed on exit (Req#13).
- `cmd/aura/config.go` (270): `aura config show/get/set` over `~/.aura/llm.json`,
  redacted `show` (key never printed — T-03-01), `set` refuses `llm.api_key`
  (env-only, D-28), numeric validation.

### Task 2 — manual acceptance gate (commit `51785099`)
- `scripts/llm_smoke.sh` (95): OPENROUTER_API_KEY-gated two-turn scripted-stdin
  acceptance; asserts a streamed reply + a non-zero token+USD footer + no raw
  text_response JSON leak; key-unset → documented skip + exit 0; NOT in CI/Makefile.

### Task 3 — human-verify checkpoint: LIVE ACCEPTANCE PASSED
Operator authorized the live run (all env in `.env`). `bash scripts/llm_smoke.sh`
against live OpenRouter produced clean streamed Italian prose, a `current_time`
tool turn, and footers like `· 1064 tok (1000 in / 64 out) · $0.000125 · 11.2s`
(non-zero tokens, real USD) with no JSON leak and a clean exit. SPEC Req#11 /
ROADMAP SC#1 met.

## Deviations from Plan

The live acceptance surfaced defects the deterministic tier could not (it never
scripted a tool-call→tool-result→text_response sequence). All fixed before close:

### Live-surfaced rendering/observability (commit `c3f78acf`)
1. **Raw tool result leaked into prose** + **2. final answer double-printed** on a
   tool turn — one root cause: the tool-result Event carries its preview in
   `LLMResponse.Content` (no FinishReason), structurally identical to an assistant
   chunk, so `renderTurn` emitted it AND polluted the prose buffer → `flushRemainder`
   diverged and re-emitted. Fix: stamp `tool_call_id` (a real AG-UI correlation id,
   previously discarded) into the tool-result Event's StateDelta; `renderTurn` skips
   it. Regression `TestRenderTurn_ToolResultNotRenderedAsProse`.
3. **OTLP export noise on exit** with no collector — contradicted the locked A2
   silent-drop. Fix: no-op `otel.ErrorHandler` on the otlp path.

### Latent production bugs found during coverage hardening
4. **`consume()` iter.Seq2 stop-propagation panic** (commit `bc49b3cb`, fix in
   Plan-04's `llm_agent.go`): an early-breaking consumer crashed the run-loop
   ("range function continued iteration after... returned false"). consume() now
   returns `stopped`; Run() returns without re-yielding. Regression
   `TestLlmAgent_ConsumeStopMidStream`.
5. **`budgetOrDefault` silent-nil** (commit `3bc32f5c`): a malformed `AURA_LOOP_*`
   env made both NewBudget calls fail, returning a nil budget that nil-panicked the
   run-loop (config.Load doesn't validate AURA_LOOP_*). `runOneTurn` now surfaces a
   clear "budget config" turn error before any model call. Regression
   `TestChat_MalformedBudgetEnv_SurfacesError`.

### Coverage hardening (commits `a6687510`,`54810a17`,`d069690b`,`e325edc0`,`6b0fdf86`,`1ada2263`)
Per operator directive (≥95% Phase-3 surface): added NEW tests only. Result —
internal/llm **100%**, internal/llm/openai_compat **97.6%**, internal/agent Phase-3
files (llm_agent/llm_agent_events/tracing/prompt) **96.8%**, all tools **100%**.
Global owned-surface coverage **85.9% → 90.3%** (≥85% floor). Legacy packages left
at the floor per operator scope decision.

## Authentication Gates
The live smoke requires the operator's `OPENROUTER_API_KEY` (real paid calls) — it
is a manual gate, never CI. Satisfied this session via operator-set `.env`.

## Known Stubs / accepted gaps
- `cmd/aura` `runChat`/`runConfig`/usage branches are `os.Exit` CLI glue — the
  behaviourally-covered category the coverage gate excludes from the owned floor.
- A handful of genuinely-unreachable defensive stmts remain uncovered (canonicalArgs
  marshal-error on plain `any`, mintSpanID crypto/rand panic, stdouttrace.New error,
  Stream json.Marshal error on a plain-field wireRequest) — not test-gamed.

## Verification Evidence
- Live `scripts/llm_smoke.sh` PASS (re-run after the production fixes — no regression).
- `go vet ./...`, `go build ./...` green; `go test` + `BASH_ENV=~/.aura-toolchain.sh
  go test -race` green across internal/agent, internal/llm/..., cmd/aura.
- `golangci-lint run ./internal/agent/... ./internal/llm/... ./cmd/aura/...` → 0 issues.
- Global coverage gate `AURA_COVERAGE_MIN=85 bash scripts/coverage_gate.sh` → 90.3% ok.
- File sizes: chat 230, chat_render 176, config 270, smoke 95 — all ≤600 LOC.

## Self-Check: PASSED
