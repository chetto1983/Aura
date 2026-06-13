# T-01 — Test apex for the agent core (fuzz + bench + mutation)

**Finding:** T-01 (P2, H/M) — the agent core had NO fuzz tests, NO benchmark on its
real hot path, and NO documented mutation score. Mitigation AP-21.
**Status:** CLOSED (2026-06-13).
**Acceptance:** (a) fuzz + bench exist and run in CI; (b) mutation ≥70% documented
for the agent core's critical file(s); (c) regression tests for B-01/M-01/M-02/M-03/
B-03/B-05 exist and pass.

This document is the durable evidence for the finding: the fuzz targets, the
corrected benchmark, the mutation methodology + score, and the regression map.

---

## 1. Fuzz tests (`internal/agent/agent_fuzz_test.go`)

Five fuzz tests target the agent core's UNTRUSTED-INPUT parsing surfaces — the
highest-value fuzz targets (arbitrary bytes in from a provider or an
attacker-controllable tool result). Each holds a clear invariant and must never
panic. The committed seed corpus (`f.Add`) makes every fuzz function also run as a
normal unit test in CI, which satisfies "fuzz in CI"; the `-fuzz` mutation search is
a developer/extended-run mode.

| Fuzz target | Function under test | Invariant |
|---|---|---|
| `FuzzParseTextResponse` | `parseTextResponse` (`llm_agent_args.go`) — the terminal `text_response` tool-arg JSON parser (D-13) | Never panics. On success the returned text is non-blank (trim-non-empty); on parse/validation failure the error is non-nil AND the text is empty. |
| `FuzzNormalizeContentStopAnswer` | `normalizeContentStopAnswer` / `parseTextResponsePayload` | Never panics. When the input is NOT a sole-`text` JSON object the raw string is returned verbatim (idempotent passthrough); when it IS, the extracted text is non-blank and equals the payload re-parse. |
| `FuzzCanonicalArgs` | `canonicalArgs` (`llm_agent_args.go`) — the dedup fingerprint canonicalizer (B2) | Never panics. Never returns empty for non-empty input (a malformed-arg storm still dedups on raw bytes). For valid-JSON input the canonical form is itself valid JSON and re-canonicalizing it is a fixpoint — so the dedup fingerprint is deterministic. |
| `FuzzWrapUntrustedToolOutput` | `wrapUntrustedToolOutput` (`trust.go`) — the `<tool_output trust="untrusted" nonce=…>` envelope (NFKC + HTML-escape + crypto nonce) | Never panics. The opening tag matches the fixed frame regex with a 16-hex nonce; the document ends with the closing tag; and no raw `<tool_output` / `</tool_output>` forged from the CONTENT or SOURCE survives the HTML escaping — the prompt-injection frame-spoofing invariant. |
| `FuzzRenderToolResultForPrompt` | `renderToolResultForPrompt` + `untrustedSource` (`trust.go`) — the full provenance-aware render path | Never panics. The untrusted-vs-passthrough decision matches `untrustedSource` for every input: an untrusted result is framed; a trusted/unspecified result passes its preview through verbatim. |

**Live run (2026-06-13, `-fuzztime` per target, 16 workers):** all five PASS, zero
crashes, zero panics, invariants held. No crash inputs were written to
`internal/agent/testdata/fuzz` (a crash would persist a reproducer there); the
interesting-input corpus stays in `GOCACHE` and is not committed.

```
go test -run=^$ -fuzz=^FuzzParseTextResponse$        -fuzztime=8s ./internal/agent/   # ok, no crash
go test -run=^$ -fuzz=^FuzzNormalizeContentStopAnswer$ -fuzztime=8s ./internal/agent/ # ok, no crash
go test -run=^$ -fuzz=^FuzzCanonicalArgs$            -fuzztime=8s ./internal/agent/   # ok, no crash
go test -run=^$ -fuzz=^FuzzWrapUntrustedToolOutput$  -fuzztime=8s ./internal/agent/   # ok, no crash
go test -run=^$ -fuzz=^FuzzRenderToolResultForPrompt$ -fuzztime=8s ./internal/agent/  # ok, no crash
```

---

## 2. Benchmarks (`internal/agent/budget_bench_test.go`)

### Why the old bench targeted the wrong path

Before this commit the ONLY `func Benchmark*` in the `internal/agent` tree was
`internal/agent/tools.BenchmarkToolSearchRank` (`tools/search_test.go`). That
benchmark measures the **tool-search retrieval** query path — embed the query,
brute-force cosine over the ~64-tool bank, guarded BM25 tiebreak. Tool search runs
**at most once per turn** (when the model calls `tool_search` to fetch a deferred
spec); it is NOT the per-step agent-loop spine. The genuine hot path — the code that
runs on **every step of every turn** — is the budget gate (`ConsumeStep`) and the
two-tier loop-guard dedup (`BeforeToolCall` / `AfterToolResult`). That is what the
new benchmarks target.

### Headline numbers (2026-06-13, `-benchmem`, amd64, 16 logical CPUs)

```
go test -bench=. -benchmem -run=^$ ./internal/agent/
```

| Benchmark | ns/op | B/op | allocs/op | What it measures |
|---|---|---|---|---|
| `BenchmarkConsumeStep` | ~20.5 | 0 | 0 | The TOCTOU-safe atomic decrement-then-check-then-restore gate (D-11), success path. |
| `BenchmarkConsumeStepParallel` | ~40.9 | 0 | 0 | The single SHARED `*atomic.Int32` step counter (D-10) under fan-out contention (`RunParallel`) — the ParallelAgent shape. |
| `BenchmarkDedupBeforeToolCall_Distinct` | ~122.9 | 32 | 1 | The common no-repeat dedup path: fingerprint (sha256(name+canonical-args)) + ring scan, no match — the per-tool-call cost on a healthy conversation. |
| `BenchmarkDedupRoundTrip` | ~343.5 | 64 | 2 | The full two-tier `BeforeToolCall`+`AfterToolResult` round trip on repeated args with a CHANGING result — the progress-veto path (D-18) that keeps a volatile-result tool from fail-opening (the most work the dedup does per step). |

The budget gate is effectively free (~20ns, zero alloc) and stays cheap under
contention; the dedup adds ~123ns on the common path and ~344ns on the
veto-resetting worst case. These run as a compile + smoke in CI (`go test`
compiles the bench file; `-bench` is a developer/extended mode).

---

## 3. Mutation score (agent-core critical file)

**Tooling (per CLAUDE.md / `docs/aura-quality-snapshot.md`):** `go-mutesting`
(avito-tech fork, the only one supporting go1.26) in WSL, with `~/go/bin` prepended
to PATH. Score = killed / total; `PASS` = mutant killed, `FAIL` = mutant survived.
Target ≥70% killed (project gate). Existing documented agent-core scores this joins:
**budget.go / budget_dedup.go 89.4%**, **db.go 82.8%** (see
`docs/aura-quality-snapshot.md`).

**Target file:** `internal/agent/budget.go` — the agent core's resource-exhaustion
control (`ConsumeStep` TOCTOU gate + `Child` shared-counter fork + env precedence).
It is the file already documented at ~89.4% and the one the new benchmarks exercise.

**Exact command:**

```bash
# WSL, full Go quality toolchain on PATH:
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
cd /mnt/d/Aura
go-mutesting ./internal/agent/budget.go
# (budget_dedup.go is the companion critical file; both are documented at 89.4%:)
go-mutesting ./internal/agent/budget_dedup.go
```

`internal/agent` has no build tags (zero integration-tagged tests — its unit tier IS
its full matrix), so no `GOFLAGS=-tags=…` is needed, unlike the container-gated
`internal/skills` mutation runs.

**Result (2026-06-13 live spot-check, WSL go-mutesting, RAN live — not documented-only):**

```
The mutation score is 0.853659 (35 passed, 6 failed, 4 duplicated, 0 skipped, total is 41)
```

**85.4% killed (35/41)** on `budget.go` — above the ≥70% target and consistent with
the previously-documented ~89.4% for this file. (`passed` = mutant killed by the
tests; `failed` = mutant survived; `duplicated` = identical to another mutant.) The
6 survivors are the known near-equivalent class — e.g. the dropped
`recordBudgetConsumeStep()` metric side-effect (mutant `budget.go.40`: a Prometheus
counter increment the unit tests intentionally do not assert) and the advisory
soft-cap / fair-share arithmetic region (`softCap`/`branchSoftCap`, a non-terminal
fairness hint that never bounds correctness, D-12). This is the same survivor class
advisory-accepted for db.go 82.8% and translator.go 76.2% (see the mutation-autopsy
precedent in the project memory). budget_dedup.go is the companion critical file
documented at the paired 89.4%; the same command applies (`go-mutesting
./internal/agent/budget_dedup.go`).

> Operator note: `go-mutesting` mutates the file IN PLACE under `/mnt/d/Aura` during
> the run, so a CONCURRENT `go test ./internal/agent/` (e.g. on the Windows host)
> can transiently fail on the exact assertion the live mutant negates (observed:
> `TestNewBudgetFromEnv_FailFast_SoftFractionOutOfRange/1.01` while the soft-fraction
> range check was mutated). Run the agent-package validation either before or after
> the mutation run, not during it — the in-isolation test passes.

---

## 4. Regression map (verified present + passing)

All six session-fix regression tests already exist and pass — no new regression test
was needed for this finding.

| Finding | Regression test | File |
|---|---|---|
| B-01 (write-ahead ordering + recovery marker; no silent re-run) | `TestResume_NoSilentReRun_SC4` | `internal/runner/runner_test.go` |
| M-01 (L1 microcompact only evicts sidecar-backed tool turns) | `TestApplyL1_PreservesNonSidecarToolAnswers` | `internal/conversations/context_unit_test.go` |
| M-02 (gate-first resume; duplicate resume injects exactly one answer) | `TestSubmitAnswer_DuplicateResumeInjectsExactlyOneAnswer` | `internal/runner/runner_resume_atomic_test.go` |
| M-03 (small-window hardCap floor; protect not raw) | `TestLadder_SmallWindowFloor_ProtectsNotRaw`, `TestHardCap` | `internal/conversations/context_smallwindow_test.go`, `context_unit_test.go`, `context_boundary_test.go` |
| B-03 (per-thread 409 busy guard) | `TestServer_RunBusyThread409` | `internal/agui/server_p1_test.go` |
| B-05 (process-lifetime shared breaker, not per-agent/per-turn) | `TestInjectedBreakerIsSharedNotPerAgent`, `TestRunnerInjectsSharedBreakerIntoEveryTurn` | `internal/agent/llm_agent_breaker_internal_test.go`, `internal/runner/runner_breaker_test.go` |

---

## Validation (this commit)

- `go vet ./internal/agent/` — clean
- `go build ./...` — clean
- `go test ./internal/agent/ -count=1` — ok (full package; includes the fuzz seed
  corpus + bench compile)
- `go test -race ./internal/agent/ -count=1` — ok
- `golangci-lint run ./internal/agent/` — 0 issues
- Every touched/new file < 600 LOC (fuzz and bench live in their own
  `agent_fuzz_test.go` / `budget_bench_test.go`).
