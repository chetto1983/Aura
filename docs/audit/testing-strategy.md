# Testing Strategy — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`

## 1. Current coverage reality

The agent package is **well-tested by volume and breadth** — this is a genuine strength, not a gap.

| Signal | Value | Evidence |
|---|---|---|
| Test files | 105 | `find internal/agent -name '*_test.go'` |
| Test LOC | ~19,399 | vs ~12,115 non-test LOC (1.6× ratio) |
| Goroutine-leak detection | ✅ | `goleak` in `main_test.go` (TestMain), `mcptools/main_test.go`, finalize tests |
| Property-based tests | ✅ | `workflow/loop_property_test.go`, `budget_dedup_test.go` |
| Fuzz tests | ✅ | `agent_fuzz_test.go`, `tools/result_test.go`, `tools/bm25_test.go` |
| Contract tests | ✅ | `workflow/workflow_contract_test.go`, `llm_agent_wire_validity_test.go` |
| Internal/white-box tests | ✅ | `*_internal_test.go` for finalize/completion/pause |
| Integration tier | ⚠️ partial | `//go:build memory_integration` (agent + mcptools); DB/Neo4j tiers live in other packages |
| Live/E2E | ⚠️ gated | `cot_eval` (OPENROUTER_API_KEY), `reasoning_classifier_live_test.go`, `live_finalize_test.go` — not in default CI |

Coverage is reported at ~90% owned-surface (project `make coverage`, ≥85% floor). The loop's hard invariants are well-locked: `TestFinalizeOutsideBudget`, `TestAskUserOnlyPauseConstraint`, the truncation/recovery counter tests, wire-validity, and the cache-prefix hash test all exist.

## 2. The gap is not *quantity* — it is *failure-mode coverage*

The findings in `bug-report.md` cluster in areas the existing tests do **not** exercise:

| Gap | Finding | Missing test |
|---|---|---|
| **Panic isolation** | AG-001 | No test dispatches a *panicking* tool through `executeBatch`/`parallel.Run`/`swarm.runWave` and asserts the process survives. This is the single highest-value missing test. |
| **Concurrent dedup-ring race** | AG-002 | No `-race` test fans `>1` parallel tools through `dispatch` hammering the ring. |
| **MCP reconnect under concurrency** | AG-005 | No test asserts concurrent calls don't head-of-line-block during a slow reconnect, or that a crash-looping server trips backoff/breaker. |
| **`=0` timeout hang** | AG-006 | No test asserts a hung MCP server is bounded when the timeout env is `0`. |
| **Hook fail-soft** | AG-004 | No test asserts a failing/timed-out/panicking hook degrades vs aborts the turn. |
| **Reasoning-router fallback latency** | AG-008 | No test asserts that an embed-sidecar outage does **not** trigger an LLM router round-trip every turn. |
| **Secret-env boundary** | AG-010 | No test asserts `AURA_DB_URL` is stripped from shell children / `IsSecretEnvKey("AURA_DB_URL")`. |
| **Cycle in agent tree** | AG-037 | No test asserts `findInTree` survives a cyclic/diamond tree. |
| **Budget validation** | AG-036 | No test asserts `max_steps=0`/negative is rejected at construction. |
| **Active wallclock** | AG-041 | No test asserts total wall-time is bounded (vs step-boundary soft gate). |

## 3. Proposed test pyramid

```
            ╱ live/E2E (gated)   — cot_eval, live_finalize, classifier live; keep nightly
          ╱  chaos/soak          — NEW: panic injection, crash-loop MCP, hung server, OOM-large-file
        ╱    integration         — memory_integration + NEW: MCP reconnect, hook subprocess, swarm fan-out -race
      ╱      property/fuzz        — loop, dedup, event, bm25, args (HAVE; extend to budget validation + name collision)
    ╱        unit (white+black)   — HAVE strong base; add panic/race/secret-env/cycle/timeout cases
```

The base is already strong; the pyramid is **missing its chaos/soak tier entirely** and has integration gaps around the concurrency/resilience findings.

## 4. Critical regression tests to add (in priority order)

1. **Panic firewall** (AG-001): table of panicking fake tools → `executeBatch`, `parallel.Run`, `swarm.runWave` all return a per-call/per-child error, process survives, under `-race` + `goleak`.
2. **Dedup-ring race** (AG-002): `-race` concurrent `BeforeToolCall`/`AfterToolResult` + a full multi-tool `dispatch` with `>1` parallel tools.
3. **MCP resilience** (AG-005/AG-006): fake server that (a) reconnects slowly → no head-of-line block, (b) crash-loops → backoff + breaker-open, (c) hangs with timeout `0` → bounded by default deadline, no goroutine leak.
4. **Hook fail-soft** (AG-004): hook that errors/times-out/panics → `fail_open` completes the turn, `fail_closed` aborts with a clear reason; assert secret-named env vars are absent from the child.
5. **Secret boundary** (AG-010): `IsSecretEnvKey("AURA_DB_URL")==true`; a shell child cannot read the composed DSN.
6. **Tree-cycle + budget validation** (AG-037/AG-036): cyclic tree → bounded (no stack overflow); `max_steps∈{0,-1}` → construction error.
7. **Reasoning-router fallback** (AG-008): embedder forced to error → static tier, no router LLM call (or one then breaker-open).
8. **Cache-prefix drift** (AG-031): a `BeforeModel` hook rewrite that changes `messages[0]` → assert detected/metric emitted.

## 5. Suggested CI checks

- **Keep:** `go vet`, `go build`, `golangci-lint` (+`dupl`), `go test -race`, `govulncheck`, coverage gate (≥85% owned), `goleak` in TestMain. The "no-skip-as-green" discipline (integration tiers `t.Fatal` under `$CI` when env unset) is excellent — keep it.
- **Add:** a `-race`-mandatory lane for the new concurrency tests (panic firewall, dedup race, MCP reconnect, swarm fan-out). Windows + Linux lanes already exist (prior O-07).
- **Add:** a nightly chaos lane (panic injection, crash-loop MCP, hung server) — these are deterministic enough to run in CI with fakes, not just live.
- **Add:** mutation spot-check (`go-mutesting`, project standard ≥70% killed) on the newly-touched critical files (`llm_agent_parallel.go`, `budget_dedup.go`, `mcptools/bridge_reconnect.go`) after the fixes land.
- **Add:** a metric-contract test (AG-012) asserting each terminal `turnReason` maps to a labeled counter once the metrics are added.

## 6. Bottom line

Testing is the project's **second-strongest** area after the loop core. The action is not "write more tests broadly" — it is to add the **specific failure-mode tests** (panic, race, resilience, secret-boundary) that would have caught the P0/P1 findings, and to stand up the missing **chaos/soak tier**. Each fix in `action-plan.md` ships with its regression test named above.
