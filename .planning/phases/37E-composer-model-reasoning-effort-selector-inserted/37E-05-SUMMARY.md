---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 05
subsystem: api
tags: [reasoning-effort, capability-detection, openrouter, llamacpp, models-endpoint, ttl-cache, allowlist-clamp, http-roundtripper]

# Dependency graph
requires:
  - phase: 37E-02
    provides: "llm.ReasoningTarget(provider,baseURL) -> {None,OpenRouter,LlamaCpp} classifier + the 7-symbol effort vocabulary incl. ReasoningEffortMax; normalizeModelID (models.go) reused verbatim as the cache key"
provides:
  - "llm.ReasoningCapability struct — one model's allowlist-clamped advertised reasoning surface (SupportedEfforts/DefaultEffort/DefaultEnabled/Mandatory/SupportedParams)"
  - "llm.ModelCapabilityClient (+NewModelCapabilityClient, ReasoningCapabilityFor) — TTL-cached GET /models fetch over an injectable transport, defensive parse, keyed by normalizeModelID; NEVER per-turn"
  - "llm.ReasoningCapabilitySource interface (AllowedEfforts(ctx) -> efforts,default,detected) — the neutral seam plan 06's endpoint + validator read; detected=false => safe floor upstream"
  - "openRouterReasoningCaps + llamaCppReasoningCaps impls (mandatory-aware; provider+ops-contract with best-effort /props narrowing)"
  - "llm.NewReasoningCapabilitySource(cfg,ttl) — boot seam selecting the source by ReasoningTarget (openrouter -> /models, llamacpp -> /props source, else nil)"
  - "Captured daemon-free fixtures: testdata/openrouter_models.json + testdata/llamacpp_props.json"
affects: [37E-06, 37E-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Capability-detection subsystem: a net-new external-dependency vertical in internal/llm (fetch + defensive parse + TTL cache + neutral seam) replacing the hard-coded modelCapabilityTable placebo (D-13/D-12)"
    - "Injectable http.RoundTripper + injectable clock (now func() time.Time) — same-package tests drive cold/warm/expiry cache paths and every parse branch from captured fixtures with ZERO network in CI"
    - "Strict allowlist clamp at a trust boundary: untrusted upstream tokens mapped through {max,xhigh,high,medium,low,none} and dropped otherwise, so a hostile /models can never inject a non-vocab effort into the validator's allowed set"
    - "Boot-selected capability seam: NewReasoningCapabilitySource(cfg,ttl) branches on llm.ReasoningTarget so the wiring (plan 06) picks the source without importing backend internals"

key-files:
  created:
    - internal/llm/model_reasoning_caps.go
    - internal/llm/model_reasoning_caps_test.go
    - internal/llm/llamacpp_caps.go
    - internal/llm/testdata/openrouter_models.json
    - internal/llm/testdata/llamacpp_props.json
  modified: []

key-decisions:
  - "Capability client is a net-new lightweight GET client in internal/llm (NOT in openai_compat, which is the streaming/wire layer) — a bounded http.Client with a whole-call Timeout (safe for a non-stream GET) + DisableKeepAlives (goleak-friendly)"
  - "TTL cache holds the mutex across the fetch — serializes a concurrent cold fetch (no thundering herd) and is -race clean; a failed refresh returns the error (never serves stale) so the seam degrades to detected=false (T-37E-05-AVAIL)"
  - "llama.cpp widens to the full spike-095 graduated set on EXPLICIT Provider==llamacpp alone (OQ-4), detected=true without requiring /props; /props is best-effort narrowing, probed once and cached (local launch config is boot-stable)"
  - "The exact chat_template_caps thinking-flag name is undocumented — the probe scans a candidate alias set and narrows ONLY on an explicit false; absent/unknown/unreachable/malformed keeps the full set (never panics, never over-restricts)"
  - "NewReasoningCapabilitySource added as the boot seam (objective: 'selected by llm.ReasoningTarget') so plan 06's serve root wires one call; unrecognized backend -> nil -> caller shows the safe floor {auto,off}"

patterns-established:
  - "Fixture-backed daemon-free capability tests: injected RoundTripper + testdata/*.json + injected clock — the ≥85% owned-surface floor is met by pure go test, never a container/live tag (CLAUDE.md coverage-gate rule)"
  - "Allowlist-clamp-as-pure-function tested directly (TestClampEfforts) — the security control (T-37E-05-UPSTREAM) is proven in isolation, not only through the integration paths"

requirements-completed: []  # WEBMODEL-01/03 capability FOUNDATION shipped (fetch/parse/cache/seam). NOT marked: they are phase-spanning — the endpoint (37E-06) + dynamic UI (37E-07) + two-stage validator must land before the requirement is user-observable. Mirrors the 37E-01/02 precedent where the terminal plan (37E-07) owns the mark.

# Metrics
duration: ~22min
completed: 2026-07-10
---

# Phase 37E Plan 05: Model Reasoning-Capability Detection Summary

**Net-new capability-detection vertical (D-13/D-12): a TTL-cached, defensively-parsed OpenRouter `/models` client + a llama.cpp `/props` source behind one neutral `ReasoningCapabilitySource` seam selected by `llm.ReasoningTarget` — the active model's advertised reasoning efforts are auto-detected (never the hard-coded placebo), allowlist-clamped against a hostile upstream, and degrade to the safe floor `detected=false` on any failure. Zero network in CI.**

## Performance

- **Duration:** ~22 min
- **Started:** 2026-07-10T22:50:00+02:00 (approx, after the 37E-04 close)
- **Completed:** 2026-07-10T23:13:00+02:00
- **Tasks:** 3 (+1 coverage-hardening test commit)
- **Files modified:** 5 (all created)

## Accomplishments
- **`ModelCapabilityClient`** fetches `GET {BaseURL}/models` over an injectable transport (mirroring the openai_compat Bearer + attribution-header wiring), parses it with a body-size-capped `json.Decoder`, and TTL-caches the result keyed by `normalizeModelID` — cold fetch → warm hit (no 2nd call) → post-expiry re-fetch, proven with an injected clock. It is NEVER per-turn: the validator (plan 06) and endpoint read from memory.
- **Defensive parse + allowlist clamp** (Threat T-37E-05-UPSTREAM): every advertised `supported_efforts` token is mapped through the strict `{max,xhigh,high,medium,low,none}` allowlist and unknowns are dropped, so a hostile/buggy upstream can never inject a non-vocab effort. Malformed/absent JSON, non-2xx, and request-build failures all return cleanly (no panic) → the seam yields `detected=false`.
- **`ReasoningCapabilitySource` neutral seam** + two impls: `openRouterReasoningCaps` (honors `mandatory` → strips `none`/off, surfaces `default_effort`) and `llamaCppReasoningCaps` (explicit `Provider==llamacpp` widens to the full spike-095 graduated set `{none,low,medium,high,xhigh,max}` detected=true; best-effort `/props` narrows to `{none}` only on an explicit thinking-disabled flag).
- **`NewReasoningCapabilitySource(cfg,ttl)`** boot seam selects the source by `llm.ReasoningTarget` — the single call plan 06's `serve` root wires into the agui Server; unrecognized backend → `nil` → caller shows the safe floor `{auto,off}`.
- **Captured daemon-free fixtures** (`openrouter_models.json` covering graduated+hostile-token / mandatory / no-reasoning branches; `llamacpp_props.json` with a `chat_template_caps` thinking flag) so every parse test runs against real-shape bytes with no network.

## Task Commits

Each task was committed atomically (TDD tasks 2 & 3 fold test+impl into one `feat` under the compile-clean pre-commit gate — see TDD Gate Compliance):

1. **Task 1: capture /models + /props fixtures** - `42fe6d2e` (test)
2. **Task 2: ModelCapabilityClient + openRouter reasoning-caps seam** - `936c6f59` (feat)
3. **Task 3: llama.cpp capability source + boot seam selector** - `61c12c6d` (feat)
4. **Coverage hardening: allowlist-clamp + request-build branches** - `19dbf827` (test)

**Plan metadata:** (this docs commit)

## Files Created/Modified
- `internal/llm/model_reasoning_caps.go` (created, 271 LOC) - `ReasoningCapability`, `openRouterModelsResponse` DTO, allowlist clamp, `ModelCapabilityClient` (+ `NewModelCapabilityClient`, `ReasoningCapabilityFor`, TTL cache), `ReasoningCapabilitySource` interface, `openRouterReasoningCaps`.
- `internal/llm/llamacpp_caps.go` (created, 170 LOC) - `llamaCppPropsResponse` DTO, `llamaCppReasoningCaps` (provider+ops-contract, /props narrowing), `NewReasoningCapabilitySource` boot selector.
- `internal/llm/model_reasoning_caps_test.go` (created, 462 LOC) - fixture-backed table tests via injected RoundTripper + clock; the pure-function `TestClampEfforts`.
- `internal/llm/testdata/openrouter_models.json` (created) - 3-model /models fixture (graduated+`turbo` hostile token, `mandatory:true`, no-reasoning).
- `internal/llm/testdata/llamacpp_props.json` (created) - llama-server /props fixture with `chat_template_caps.supports_thinking:false`.

## Decisions Made
- **Capability client lives in `internal/llm`, not `openai_compat`.** The wire package is streaming-only; a plain GET client there would smell. It reuses `cfg` (BaseURL+APIKey+Headers — same trust boundary as `/chat/completions`) and `normalizeModelID`.
- **Mutex held across the fetch.** Simpler than singleflight and equally correct for a boot-warmed, long-TTL cache: it collapses a concurrent cold fetch and keeps the map `-race` clean. A failed refresh returns the error and does NOT report the stale cache as fresh (safe-floor semantics).
- **llama.cpp widens on explicit provider alone (OQ-4).** `/props` cannot confirm the `--jinja`/`--reasoning-budget` launch flags, so the operator setting `provider=llamacpp` IS the assertion of the spike-095 config; `/props` only narrows (never the sole source of `detected`).
- **Candidate-alias thinking-flag scan.** The exact `chat_template_caps` key is undocumented; the code checks `{supports_thinking,thinking,supports_reasoning,reasoning}` and narrows only on explicit `false`. Fixture flag name is `[ASSUMED-pending-live-capture]` (documented in-file).
- **WEBMODEL-01/03 not marked complete** — capability foundation only; the endpoint + UI + validator (Waves 4-5) make them user-observable. Terminal plan 37E-07 owns the mark (37E-01/02 precedent).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added `NewReasoningCapabilitySource` boot selector**
- **Found during:** Task 3 (seam wiring)
- **Issue:** The plan's must_haves/objective require the seam to be "selected by `llm.ReasoningTarget`", and the phase_context states plan 06 calls `SetReasoningCapabilitySource`, but no plan task named the constructor that produces the correct source per backend. Without it, plan 06 would have to re-implement the ReasoningTarget branch at the composition root.
- **Fix:** Added `NewReasoningCapabilitySource(cfg, ttl) ReasoningCapabilitySource` in `llamacpp_caps.go` (it references both impls) — branches on `ReasoningTarget`, returns the openRouter source / llamacpp source / `nil`.
- **Files modified:** internal/llm/llamacpp_caps.go
- **Verification:** `TestNewReasoningCapabilitySource` asserts the three selections (openrouter → `*openRouterReasoningCaps`, llamacpp → `*llamaCppReasoningCaps`, vllm → nil).
- **Committed in:** `61c12c6d` (Task 3 commit)

**2. [Rule 2 - Missing Critical] Direct pure-function coverage of the allowlist clamp + request-build branch**
- **Found during:** Post-task verification (coverage review)
- **Issue:** `clampEfforts` empty-input and all-dropped → nil branches, and the `http.NewRequestWithContext` error path, were unexercised (clampEfforts 77.8%). The clamp is the T-37E-05-UPSTREAM security control; CLAUDE.md mandates testing security logic as a pure function.
- **Fix:** Added `TestClampEfforts` (nil/empty/all-unknown/case+whitespace/mixed-hostile) and `TestCapabilitySourceRequestBuildError` (un-parseable BaseURL → openrouter detected=false, llamacpp keeps full set).
- **Files modified:** internal/llm/model_reasoning_caps_test.go
- **Verification:** clampEfforts 77.8%→100%; internal/llm total 93.1%→94.5%.
- **Committed in:** `19dbf827` (test)

---

**Total deviations:** 2 auto-fixed (both Rule 2 - missing critical). No architectural changes (Rule 4), no auth gates.
**Impact on plan:** Both necessary — the boot selector is the seam the objective demands and the linkage plan 06 needs; the added tests harden the phase's named security control. No scope creep (no new files beyond the plan's `files_modified`; the selector lives in the Task-3 file).

## TDD Gate Compliance

Tasks 2 and 3 are `tdd="true"`. The RED→GREEN cycle was performed and observed for both:
- **Task 2 RED:** `model_reasoning_caps_test.go` authored first; `go test` failed to compile (`undefined: ModelCapabilityClient / NewModelCapabilityClient / openRouterReasoningCaps`). **GREEN:** `model_reasoning_caps.go` implemented → 5 tests pass, `-race` clean (WSL), lint 0.
- **Task 3 RED:** the llama.cpp test rows appended first; `go test` failed to compile (`undefined: llamaCppReasoningCaps / newLlamaCppReasoningCaps / NewReasoningCapabilitySource`). **GREEN:** `llamacpp_caps.go` implemented → both rows + factory pass, `-race` clean.

Each TDD task is a SINGLE atomic `feat` commit (test + impl together) rather than separate `test(...)`/`feat(...)` commits. **Reason:** the repo's lefthook pre-commit gate runs `go vet` + `golangci-lint` on every commit (no `--no-verify` for this sequential run) and rejects a non-compiling RED-only test commit. Committing the RED test in isolation is impossible under that gate, so the "every commit compiles + passes vet/lint" invariant is honored while test-first authoring order is preserved and verified failing-then-passing locally. This is the exact documented handling used by 37E-02 — a justified RED-gate accommodation, not a skip.

## Issues Encountered
- **`-race` needs CGO/gcc, absent on the Windows PATH (`CGO_ENABLED=0`, no gcc).** Resolved by running all `-race` verification in WSL Ubuntu (go1.26.5 linux, gcc 15, repo at `/mnt/d/Repo/Aura`) — CLAUDE.md's documented primary dev environment. Non-race tests, `go vet ./...`, `go build ./...`, and coverage ran natively on Windows; `golangci-lint` ran in WSL.
- **No live fixture capture possible** (no `OPENROUTER_API_KEY`, no network, no live llama-server). Fixtures were constructed to the operator-verified `/models` shape (RESEARCH P2.2 A, verified LIVE 2026-07-10) and the official llama.cpp `/props` README schema, with an in-file `_note` marking the synthetic-for-test values (`turbo` hostile token, `none`-on-mandatory, `supports_thinking:false`) and the `[ASSUMED-pending-live-capture]` flag name. Permitted by the plan (Task 1: "If a live llama-server is unavailable, construct the fixture from the official README schema").
- **`jq` is not installed** (the plan's verify commands use it). Substituted an equivalent `python -c` JSON assertion for fixture validation; the Go parse tests are the authoritative validity check.

## User Setup Required
None - no external service configuration required. The capability client reuses the already-configured OpenRouter key + base URL; no new env vars, deps, or migrations.

## Next Phase Readiness
Wave-4 plan 37E-06 (endpoint + two-stage validator) links against the exact delivered symbols:
- `llm.NewReasoningCapabilitySource(cfg, ttl)` — construct the source at the `serve` composition root; pass to `SetReasoningCapabilitySource`.
- `llm.ReasoningCapabilitySource.AllowedEfforts(ctx) -> (efforts, default, detected)` — the endpoint maps `[]ReasoningEffort` → UI symbols (prepend `auto`, omit `off` when mandatory); Stage-2 validator checks `contains(allowed, effort)` when `detected`, else rejects graduated levels (safe floor).
- On a nil/failed source → `detected=false` → degrade to `{auto,off}` (never 503).

No blockers. No new deps, migrations, or env. Owned-surface coverage 94.5% (floor 85%); each new file <600 LOC; `-race` + `golangci-lint` clean.

## Threat Flags
None. The two trust boundaries (OpenRouter `/models`, llama-server `/props`) are exactly the ones enumerated in the plan's threat register and are mitigated as specified (allowlist clamp + body cap + TTL/degrade). No new network endpoints, auth paths, or schema changes were introduced.

## Self-Check: PASSED

Files (created) verified present on disk:
- internal/llm/model_reasoning_caps.go — FOUND
- internal/llm/model_reasoning_caps_test.go — FOUND
- internal/llm/llamacpp_caps.go — FOUND
- internal/llm/testdata/openrouter_models.json — FOUND
- internal/llm/testdata/llamacpp_props.json — FOUND

Commits verified in git log:
- 42fe6d2e (Task 1) — FOUND
- 936c6f59 (Task 2) — FOUND
- 61c12c6d (Task 3) — FOUND
- 19dbf827 (hardening) — FOUND

---
*Phase: 37E-composer-model-reasoning-effort-selector-inserted*
*Completed: 2026-07-10*
