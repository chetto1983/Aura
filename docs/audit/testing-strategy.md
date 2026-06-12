# Testing Strategy — Aura `internal/agent` (2026-06-12)

## 1. Coverage reality

Measured live 2026-06-12 (`go test ./internal/agent/... -count=1 -cover`; `-race` green across all 6 packages):

| Package | Prior claim | Measured | Tier | Notes |
|---|---|---|---|---|
| `internal/agent` | 93.3% | **92.5%** | unit-only | In the coverage-gate floor |
| `internal/agent/tools` | 87.5% | **86.1%** | unit-only | In the floor (R-17 closed — no longer excluded) |
| `internal/agent/workflow` | 92.4% | **88.8%** | unit-only | In the floor |
| `internal/agent/mcptools` | 83.4% | **82.7%** | unit-only | In the floor |
| `internal/agent/prompt` | 91.9% | **92.0%** | unit-only | In the floor |
| `internal/agent/agenttest` | — | **42.6%** | unit-only | Test-helper package; **dilutes the floor** (T-04) |

The per-package numbers are **honest**. Critically, `internal/agent` and **all** its subpackages have **zero integration-tagged tests** (`grep "//go:build"` finds only `live_finalize`, `windows`, `!windows`) — so for this package, unit-only *is* the full matrix. There is no skip-as-green risk here because there is nothing tier-gated to skip.

### The 93% vs 77.2% "contradiction" — reconciled

They measure different denominators in different environments; both are real, not in conflict:

- **93% is `internal/agent`-scoped, unit-only, env-independent** — and for this package that's the whole matrix.
- **77.2%** (from `p2-boundary-lifecycle-validation-2026-06-11.md`) is the **repo-wide owned-surface gate run with the container stack DOWN**. On that Windows-bash invocation `$CI` was unset, so the integration tiers in `internal/db`/`cron`/`conversations` **skipped** (locally `envOrSkip` skips; under `$CI` it `t.Fatal`s), collapsing those packages to their unit-only floors (~20-34%) and dragging the aggregate to 77.2%.
- **With the stack up, the same gate reads ~85.9%** (`docs/aura-quality-snapshot.md`) and **passes**. CI's knowledge + skills jobs run it with the stack and env present.

The agent package's own coverage is not implicated in the 77.2% miss. The repo-wide gate needs a stack-up run (or the `$CI` env) to report green — an operational caveat, not an agent-package gap.

---

## 2. Test quality assessment

### Genuinely strong (preserve)

- **Tool-call wire contract:** `llm_agent_wire_validity_test.go:11` `assertHistoryToolCallsAnswered` enforces "one `tool_result` per `tool_call`" across the dedup-recovery and completion-gate-veto scenarios. This is exactly the contract test a production agent loop needs, and it exists.
- **Property-based testing is real:** 5 files use `pgregory.net/rapid` — budget total-consumed-never-exceeds-max, Event JSON round-trip byte-identity, BM25, result paging, loop properties.
- **goleak** via `VerifyTestMain` in `agent`, `tools`, `mcptools`, `workflow`.
- **Exhaustive dedup coverage** (22 cases: ring eviction, exempt-tools, ping-pong, result-change suppression).
- **Tests assert artifacts, not `r.Reply`:** the `r.Reply`-only anti-pattern appears 0× in agent tests; assertions target history/requests/budget/filesystem. Mocks carry compile-time interface assertions.

### Gaps (the apex of the pyramid is missing)

| ID | Gap | Evidence | Severity |
|---|---|---|---|
| T-01 | **Zero fuzz, zero benchmarks** in the agent runtime | `grep "func Fuzz"`/`func Benchmark` → 0 | P2 |
| T-01 | **No documented mutation score** for any agent-core file | snapshot documents mutation only for telegram/skills/web/agui — none for `budget*.go`, `llm_agent_completion.go`, tools | P2 |
| — | **No provider-500-storm / chaos test** | `FakeClient` injects only single-turn / single-mid-stream errors; no N-consecutive-5xx or alternating storm | P2 |
| — | **No crash/fault-injection test** for B-01 (re-execution), M-02 (duplicate resume) | grep "R-27"/"R-28"/"crash" → none traceable | P2 |
| T-02 | `foldToASCII` 23.5% covered (filename folding, primary channel) | `send_file.go:193` | P3 |
| T-03 | Deferred-tool `Spec()` constructors at 0% | `fs_*`/`search.go` | P3 |

---

## 3. Proposed test pyramid

Current shape: a **wide, disciplined unit/property base** (~460 funcs, 73 files) with **no apex** (fault-injection, load) and **no fuzz sublayer**. Target:

```
            ┌───────────────────────────┐
            │  E2E / live (gated)        │  cot_eval, live_e2e, live_finalize — keep, add crash-recovery E2E
            ├───────────────────────────┤
            │  Chaos / fault-injection   │  ← NEW: crash mid-turn, provider-500 storm, MCP hang, SIGTERM drain
            ├───────────────────────────┤
            │  Integration (tagged)      │  ← agent pkg has NONE today; add a tiny tier for runner↔agent persistence
            ├───────────────────────────┤
            │  Property + Fuzz           │  property: strong; FUZZ: ← NEW (tool-args, MCP description framing)
            ├───────────────────────────┤
            │  Unit (table-driven)       │  strong, ~86-93%
            └───────────────────────────┘
```

---

## 4. Top missing regression tests (each tied to a finding)

| Priority | Test | Closes |
|---|---|---|
| 1 | `TestRegression_B01_CrashMidTurnNoReExecution` — persist the tool-call turn, kill before the result commit, reload, assert the mutating call is NOT silently dropped (a recovery marker appears, not a re-execution) | B-01 (R-05) |
| 2 | `TestRegression_M02_NoDuplicateResume` — two resume signals for one pause → exactly one answer turn, second returns `ErrPauseNotFound` | M-02 (R-27) |
| 3 | `TestRegression_M01_AskUserSurvivesL1Eviction` — an `ask_user` answer older than `evictAfter` survives verbatim, not a dead `read_tool_output` pointer | M-01 (R-28) |
| 4 | `TestContext_SmallWindowDoesNotDisableProtection` — `ContextWindow:32000` over-cap history → error/compaction, never raw unprotected history | M-03 (R-29) |
| 5 | `TestLlmAgent_SurvivesProvider500Storm` — N consecutive 5xx across turns → clean surfaced error + budget intact + breaker open across turns | B-05, retry |
| 6 | `TestRunner_ConcurrentRunsOneThreadSerialize` — two `Turn` on one thread → 409 or provable serialization (race-detector) | B-03 |
| 7 | `FuzzToolArgsUnmarshal` + `FuzzMCPDescriptionFraming` | T-01, B-15 |
| 8 | `BenchmarkBudget_BeforeToolCall` / `BenchmarkDedupRing_Push` + recorded mutation score for `budget*.go`, `llm_agent_completion.go` | T-01 |

---

## 5. Suggested CI checks (delta to today)

- **Add a `windows-latest` lane** (build + vet + `internal/agent/tools` units; race if feasible) — the OS-specific kill path ships untested (O-07).
- **Wire a mutation spot-check** for the agent-core critical files into the phase gate, recorded in a `VALIDATION.md` Manual-Only table per CLAUDE.md (currently undocumented for the core).
- **Exclude `agenttest/`** from the coverage-gate denominator (it's test infrastructure, like `sqlc`) so the floor reflects real owned surface (T-04).
- **Add the crash/storm chaos tests above** to the standard unit job (they use the in-memory `FakeClient`, no stack required).
- **Keep** the existing `-race ./...`, the coverage gate (run with stack + `$CI`), and the live-gated tiers (`cot_eval`, `live_e2e`) exactly as-is — those are good.

---

## 6. Maintainability (testability-adjacent)

- **`shell_exec.go` is 598/600 LOC** (B-11) — split before the next touch forces it past the gate.
- **Three divergent `secretEnvKey` impls** (B-09) — a single shared helper would be one test instead of three drifting ones.
- **No fragile package-global state** in the agent core (`todo`/`shell_bg` state is instance-scoped struct fields with mutexes; `swarm_context` uses a typed ctx key with an `ok`-bool, never panics) — verified clean, do not regress.
