---
phase: 03-llm-client-toolresult
plan: 01
subsystem: llm-config
tags: [llm, config, otel, cost, prd-amendment]
requires:
  - "internal/config.Config (Phase 1 root composite)"
  - "internal/agent (Phase 2 — anchor file lands here)"
  - "github.com/joho/godotenv (Phase 1)"
provides:
  - "llm.Config + llm.Load (4-tier load-order chain, fail-fast empty-key)"
  - "llm.ErrMissingAPIKey sentinel"
  - "llm.Price + llm.CostUSD (A3 actual-or-table-or-n/a cost)"
  - "config.Config.LLM + config.Config.Otel{Exporter,Endpoint}"
  - "OTel v1.44.0 unified trace train pinned in go.mod (anchor)"
affects:
  - "internal/config (Load now requires OPENROUTER_API_KEY)"
  - "Plan 02 (consumes llm.Config + CostUSD)"
  - "Plan 04 (consumes the pinned OTel train; replaces the anchor with tracing.go)"
tech-stack:
  added:
    - "go.opentelemetry.io/otel v1.44.0"
    - "go.opentelemetry.io/otel/sdk v1.44.0"
    - "go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.44.0"
    - "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0"
  patterns:
    - "fail-fast vs silent-absorb env load-order (budget.go vs config.go)"
    - "structural secret redaction (no String()/log of the config struct)"
    - "blank-import anchor to pin a module train ahead of first real use"
key-files:
  created:
    - "internal/llm/config.go"
    - "internal/llm/prices.go"
    - "internal/llm/config_test.go"
    - "internal/llm/prices_test.go"
    - "internal/llm/main_test.go"
    - "internal/agent/otel_deps.go"
  modified:
    - "prd.md (§Slice 1 A1-A5 amendments)"
    - ".env.example (Slice 1 LLM + OTel catalog)"
    - "go.mod / go.sum (OTel v1.44.0 train)"
    - "internal/config/config.go"
    - "internal/config/config_test.go"
decisions:
  - "Anchor .go file (otel_deps.go) pins the OTel train; go mod tidy prunes unused modules so go get alone is insufficient — committed with Task 2's .go code, keeping Task 1 PRD-amendment .go-free"
  - "config.Load now fail-fasts on empty OPENROUTER_API_KEY (composes llm.Load); existing config tests updated to set a placeholder key — the contract legitimately changed, not test-gaming"
metrics:
  duration: "~40min"
  completed: "2026-05-30"
  tasks: 2
  files: 13
---

# Phase 3 Plan 01: LLM Config Foundation Summary

Landed the phase's first two D-01 commits: the combined A1-A5 PRD-amendment
(REPL, real OTel exporter, actual+table cost honesty, byte-based read_tool_output,
env catalog) with the verified OTel v1.44.0 trace train pinned, then the typed
`llm.Config` 4-tier load-order chain + A3 price table + `config.Config`
composition. This is the foundation every later Phase-3 plan reads.

## What Was Built

### Task 1 — PRD amendment + OTel pin (commit `6fe78c7f`)
- **A1**: `aura chat` redefined as an interactive multi-turn REPL (in-memory
  history only); smoke + acceptance updated from the single-shot form.
- **A2**: OTel emits to a real env-gated exporter (`AURA_OTEL_EXPORTER` ∈
  {stdout,otlp,none}, default otlp → `localhost:4317` silent-drop), not the
  emit-only no-op provider; 8-byte crypto/rand SpanID minted full-tree this slice.
- **A3**: USD cost = OpenRouter actual `usage.cost` first, static price-table
  fallback; unknown model → `n/a`, never `$0`.
- **A4**: `read_tool_output` offset/limit are BYTES (default ~2048) with footer
  `showing bytes X-Y of Z, next offset Y`; the line-based prose at the file-target
  row and acceptance line both fixed.
- **A5**: env catalog gains `AURA_LLM_TEMPERATURE/MAX_TOKENS/CONNECT_TIMEOUT_SEC`
  and `AURA_OTEL_EXPORTER/ENDPOINT` in the PRD index AND `.env.example`;
  `OPENROUTER_API_KEY` retained as the canonical third-party secret.
- OTel v1.44.0 unified train added via `go get` + `go mod tidy`; `go.sum` carries
  no `-rc/-beta/-alpha` on any otel line. The PRD-amendment commit itself changed
  zero `.go` source files.

### Task 2 — llm.Config + prices + composition (commit `f5f8e343`)
- `internal/llm/config.go`: `Config{Provider,Model,BaseURL,APIKey,
  TotalTimeoutSec,ConnectTimeoutSec,Temperature,MaxTokens,Headers,Prices}` with
  the locked precedence (default < .env < `~/.aura/llm.json` < `AURA_LLM_*`).
  Numeric env knobs fail-fast on a malformed-but-set value; empty APIKey returns
  the `ErrMissingAPIKey` sentinel. Defaults match D-22 exactly (model
  `deepseek/deepseek-v4-flash:exacto`, base `https://openrouter.ai/api/v1`, temp
  0.7, max-tokens 4096, timeouts 120/10, attribution headers HTTP-Referer/X-Title).
- `internal/llm/prices.go`: `Price` + seeded `defaultPrices()` + `CostUSD` with
  the provider-cost-first / table / `n/a` honesty contract (D-18/D-23).
- `internal/config/config.go`: `LLM llm.Config` + `Otel{Exporter,Endpoint}`
  composed in `Load`, surfacing `llm.Load`'s error; DB/Neo4j wiring untouched.
- `internal/agent/otel_deps.go`: blank-import anchor pinning the four OTel modules.
- Tests: load-order precedence, missing-key sentinel, malformed-env fail-fast,
  attribution headers, cost provider/table/n-a paths, goleak harness; config
  tests updated for the new key requirement + LLM/OTel composition assertions.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] OTel anchor file to survive `go mod tidy`**
- **Found during:** Task 1
- **Issue:** The plan's Task 1 assumed `go get` alone keeps the four OTel modules
  pinned with zero `.go` changes. Modern `go mod tidy` prunes modules with no
  source importer, dropping sdk/stdouttrace/otlptracegrpc from go.mod — which
  would fail the truth statement "go.mod pins the OTel v1.44.0 unified trace
  train (otel, sdk, stdouttrace, otlptracegrpc)".
- **Fix:** Added `internal/agent/otel_deps.go`, a blank-import anchor that keeps
  all four at v1.44.0 (direct deps). Committed with Task 2's `.go` code, so the
  Task 1 PRD-amendment commit stays `.go`-free (acceptance: zero `.go` files in
  HEAD). Plan 04's `tracing.go` replaces the anchor with the live TracerProvider.
- **Files modified:** internal/agent/otel_deps.go (Task 2 commit)
- **Commit:** f5f8e343

**2. [Rule 1 - Bug] Existing config tests broke on the new key requirement**
- **Found during:** Task 2
- **Issue:** `config.Load()` now composes `llm.Load()`, which fail-fasts on an
  empty `OPENROUTER_API_KEY`. Six existing `internal/config` tests called `Load()`
  without a key and started failing — a real contract change, not a test bug.
- **Fix:** Extended the shared `clearPostgresEnv` helper to set a placeholder
  `OPENROUTER_API_KEY` (and clear the new `AURA_LLM_*`/`AURA_OTEL_*` knobs), plus
  a new `TestLoad_LLMAndOtelComposed` asserting LLM population + OTel defaults.
  This is the correct adaptation to a legitimately changed contract (CLAUDE.md
  "unless the test itself is broken"), not test-gaming.
- **Files modified:** internal/config/config_test.go
- **Commit:** f5f8e343

### gosec G101 false positive (lint)
- `golangci-lint` flagged `AURA_LLM_MAX_TOKENS` as a hardcoded credential
  (gosec's "TOKENS" heuristic). Added `//nolint:gosec` with the verbatim
  false-positive rationale, matching the existing `envAPIKey`/budget.go convention.

## Authentication Gates
None.

## Known Stubs
None. The seeded price table and the OTel anchor are intentional, documented
foundations consumed by Plan 02 (cost) and Plan 04 (tracing) respectively.

## Verification Evidence
- `go vet ./...` clean; `go build ./...` green with OTel v1.44.0 resolved.
- `go test ./internal/llm/ ./internal/config/` green; `go test -race
  ./internal/llm/` green (goleak harness installed).
- `golangci-lint run ./internal/llm/... ./internal/config/...` → 0 issues.
- `grep -c 'go.opentelemetry.io/otel' go.mod` ≥ 4 at v1.44.0; `go.sum` has no
  `-rc/-beta/-alpha` otel line.
- `.env.example` carries all six new env vars; `OPENROUTER_API_KEY` retained.
- PRD-amendment commit `6fe78c7f`: zero `.go` files changed.
- File sizes: config.go 254, prices.go 42, config.go(config) 154 — all ≤600 LOC.

## Self-Check: PASSED
