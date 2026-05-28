# Testing Patterns

**Analysis Date:** 2026-05-28

> **Skeleton state.** The repo has zero `*_test.go` files today (the rewrite is mid-flight). Test discipline is therefore PRESCRIPTIVE, sourced from `/home/user/Aura/prd.md` §Test discipline (line 1393) + §Test discipline rigorosa (line 1447) + §Slice Q&A discipline gate sequences (line 1490). Read those sections in full before writing the first test of any slice.

## Test framework

- **Runner:** Go standard `testing` package (Go 1.23). No alternative framework (no testify-as-default, no ginkgo).
- **Module:** `github.com/chetto1983/aura` (`/home/user/Aura/go.mod`).
- **Required external libs** (added per-slice as needed):
  - `go.uber.org/goleak` — goroutine-leak verification. Mandatory for slices 1, 3, 6, 8, 9, 11, 13 (PRD §Test discipline rigorosa #3).
  - `pgregory.net/rapid` OR `github.com/leanovate/gopter` — property-based testing. Mandatory for slices 3 (swarm backpressure), 4 (PromptBuilder invariants), 8 (AG-UI translator coverage).
  - Go 1.24+ `synctest` (when project moves to 1.24) OR explicit channel sync for deterministic timing. No `time.Sleep` for synchronization.
  - `go-mutesting` (manual, not CI) for mutation-testing spot-check ≥70% killed on slice-critical files.

### Run commands

```bash
go test ./...                                # full unit suite
go test -race ./...                          # mandatory; must be green
go test -tags=db_integration ./...           # integration runner: db_integration
go test -tags=sandbox_integration ./...      # integration runner: sandbox_integration
go test -tags=multimodal_integration ./...   # integration runner: multimodal_integration
go test -tags=onboarding_integration ./...   # integration runner: onboarding_integration
go test -tags=neo4j_integration ./...        # integration runner: neo4j_integration
go test -coverprofile=cover.out ./internal/<package>/
go tool cover -func=cover.out                # check threshold
go test ./internal/<package>/ -run TestXxx_Behavior_When_Condition -v
```

## Test discipline (PRD §1393) — "no asilo nido"

A test that exercises the agent loop against a real model is valid **if and only if the prompt sounds like something a real user would write**. Forbidden in E2E prompts:

- Naming a registry tool (`execute`, `text_response`, `swarm.spawn`, `web_fetch`, …)
- Naming a skill or overlay
- Naming an internal Go function/module
- The word "tool" itself, except in natural meta-questions ("which tool would you use for X?" is fine; "use the execute tool to…" is asilo)

**Why.** A prompt that names the tool by-name only tests the dispatcher — already covered by registry unit tests. A natural prompt tests the whole pipeline: system prompt → manifest visibility → model picks the right tool → tool executes → final reply is sensible. Pipeline breakage (poisoned manifest, system prompt hiding the tool, ambiguous schema description) is only caught by natural prompts.

### Bad vs good — table from PRD §Test discipline

| Test type | Bad (asilo) | Good (real) |
|---|---|---|
| LLM client (Slice 1) | `"say hello using text_response"` | `"ciao, dimmi 2+2 in tre parole"` |
| Sandbox tool (Slice 2) | `"use the execute tool to print 4"` | `"quanto fa 2 alla 64 meno 1?"` |
| Swarm (Slice 3) | `"spawn a worker with goal=foo"` | `"trovami in parallelo PIL Italia 2023, capitale Australia, autore Promessi Sposi"` |
| Cache (Slice 4) | `"trigger ephemeral cache_control on system"` | turn-by-turn REPL on a conversational topic (poem, recipe, translation) |

**Exceptions** (PRD §1425): internal unit tests that do NOT go through a model — SSE fixture parser tests, `Coordinator` unit tests, `PromptBuilder` invariant tests — are NOT bound by this rule. They test the Go primitive directly.

### Assert artifact, not reply

PRD §1430-1445:

- For `execute`: assert the subprocess actually ran (log/event), not just that the reply contains `"4"`.
- For `swarm`: assert `Coordinator.children` has 3 entries, not just that the reply mentions "PIL".
- For parallelism (swarm): `wallClock(3 parallel workers) < 1.5 × wallClock(1 worker)`, otherwise the model serialized.
- For cache (provider side-effect): `usage.prompt_cache_hit_tokens > 0` from turn 2 onward.
- **Failure mode to avoid:** test passes because the reply *contains the expected string* while behind the scenes the tool was never called (model hallucinated). **Mitigation:** hook in `tools.Registry` that logs every `Execute` with `tool_name + args_hash + duration`. Tests assert `(reply matches expected) AND (tool was invoked)`. Never only the first.

## Test discipline rigorosa (PRD §1447) — 10 hard requirements

Every committed `*_test.go` MUST satisfy:

1. **Naming:** `TestXxx_Behavior_When_Condition` (descriptive). Forbidden: `TestFoo1`, `TestFoo2`, `TestSomething`. Examples:
   - `TestLoopTurn_AppendsToolResult_When_ToolCallReturnsString`
   - `TestLoopRunCancel_NoGoroutineLeak`
   - `TestSpawn_ReturnsError_When_DepthExceedsMax`
   - `TestSkillCreate_RollsBackFSMove_When_AuditInsertFails`
2. **Setup/teardown:** no shared state across tests except under `_integration` build tags with DB transactions rolled back. `go test -race ./...` green on every touched package.
3. **Goroutine leak check:** `goleak.VerifyNone(t)` in `TestMain` (package-wide) or `defer goleak.VerifyNone(t)` per goroutine-spawning test. Slices 1, 3, 6, 8, 9, 11, 13 require this explicitly in acceptance.
4. **Realistic fixtures:** `testdata/*.{json,csv,md,sse,sql,html,pdf}` with content extracted from real cases and pseudonymized. Forbidden: `{"foo":"bar"}` placeholders.
5. **Property-based where indicated** — slice-scoped: Slice 3 (swarm bus backpressure), Slice 4 (PromptBuilder invariants), Slice 8 (AG-UI translator coverage of ~25 event types). Use `pgregory.net/rapid` or `github.com/leanovate/gopter`.
6. **Build tags for integration:** `//go:build db_integration`, `//go:build sandbox_integration`, `//go:build multimodal_integration`, `//go:build onboarding_integration`, `//go:build neo4j_integration`. Separate CI runner; no flaky-on-mainstream-CI.
7. **No `time.Sleep` for synchronization.** Use Go 1.24+ `synctest` or channel sync. Wait conditions take an explicit 5s timeout + fail-loud (never infinite).
8. **Coverage threshold per package:** ≥ 75% unit (`go test -cover ./internal/...`), ≥ 60% integration. CI fails below threshold (no silent skip).
9. **Mutation testing spot-check:** one `go-mutesting` invocation per slice on a core file (e.g. `llm_agent.go`, `coordinator.go`, `pipeline.go`). Minimum 70% killed. Run manually, not in CI. Result documented in commit message or issue.
10. **Failure-driven test (TDD reverse):** every bug fixed during implementation gets a regression test reproducing the bug BEFORE the fix lands. The test fails on the broken code, passes on the fix.

### Per-slice concrete examples (PRD table, lines 1464-1481)

| Slice | Test "asilo" to REJECT | Rigorous test EXPECTED |
|---|---|---|
| **1 (LLM client)** | `assert reply == "4"` | Fixture SSE multi-chunk delta-merge + tool-call accumulator + ctx-cancel premature close + `goleak.VerifyNone` |
| **2a (Sandbox stateless)** | `aura exec python "print(2)" → 2` | Subprocess wall-time + memory + stdout truncation 1 MiB + EPERM on `socket()` syscall + seccomp profile load verification |
| **2b (Sandbox session)** | Single session reuse | 3 concurrent sessions, hard-cap enforce, TTL reap deterministic via `synctest`, workspace quota enforce, network allowlist `nft list` iptables verify |
| **3 (Swarm)** | `coordinator.Spawn(2) → 2 children` | Wall-clock parallelism `<` 1.5× single (race-detector enforced), 10 children interactive-paused simultaneously without data race, multi-pause FIFO priority sort verify |
| **4 (KV cache)** | `messages[0] == messages[0]` | `usage.prompt_cache_hit_tokens > 0` from turn 2, byte-exact hash comparison, property-based on manifest ordering |
| **5 (Web tools)** | `web_fetch("google.com") returns html` | SSRF protection (loopback denied, allowlist enforced), redirect-chain max 5, content-type sniffing, robots.txt respect |
| **6 (Scheduler)** | `task.fire after 10s` | `FOR UPDATE SKIP LOCKED` concurrency with 5 workers, crash-recovery `unknown_recovery` row, `ReschedulesOnRecovery` selective re-fire |
| **7c (Skill mutation)** | `skill_create writes file` | Tx rollback on INSERT fail (FS-move reversed), audit row immutable (UPDATE/DELETE rejected via trigger), `approval_source` constraint coherence |
| **8 (AG-UI)** | `event.type == "TEXT_MESSAGE_CONTENT"` | AG-UI Dojo conformance suite full run, property-based on all ~25 event types, backpressure SSE channel cap 64 + drop with `RUN_ERROR` |
| **9b (Telegram)** | `bot.send("hello")` | Throttle 1500ms/500ms/1000ms enforce with `synctest`, 429 exponential backoff up to 30s, golden fixture per AG-UI event type → Telegram message |
| **10 (Onboarding)** | Interview 1 question | `LoopAgent max_iter=8` cap enforce, escalation event terminate, fact extraction recall on conv corpus (precision ≥ 0.7), audit `profile_audit` row with `paused_state_token` |
| **11b (Ingest)** | `ingest.file(pdf) returns ok` | `content_hash` idempotent, mem0 two-phase conflict dedup (95% recall on duplicate entities), entity-type taxonomy coverage 100% |
| **11d (Retrieval)** | `memory.search returns 5 chunks` | Hybrid fusion score correctness vs baseline (BM25-only, vector-only, graph-only), re-ranker quality NDCG@5 ≥ 0.8 on corpus eval |
| **13b (vLLM + LMCache)** | `vllm responds` | KV cache hit ratio > 30% turn 2–5 on long-context (>4K-token prompt), failover offline detection switch within 90s, cost tracker rolling-24h accuracy |

### Smoke tests are allowed (and complementary)

PRD §1483-1486:

- 1–3 smoke tests per slice that run in < 5s, no rigor on edge cases.
- `compile + go vet + go build` always green pre-commit.
- Smoke does NOT replace rigorous tests — it complements them.

## Test file organization

### Location

- **Co-located with code under test:** `internal/<pkg>/<unit>_test.go` next to `internal/<pkg>/<unit>.go`.
- **Integration tests live in the same package** but gated by build tag at the top of the file:

  ```go
  //go:build db_integration

  package conversations
  ```

  Examples expected from PRD file targets:
  - `/home/user/Aura/internal/db/db_test.go` — build tag `db_integration` (Slice 0.5)
  - `/home/user/Aura/internal/knowledge/client_test.go` — build tag `neo4j_integration` (Slice 0.7)
  - `/home/user/Aura/internal/sandbox/docker_test.go` — build tag `sandbox_integration` (Slice 2a)
  - `/home/user/Aura/internal/sandbox/sessions_test.go` — build tag `sandbox_integration` (Slice 2b)
  - `/home/user/Aura/internal/swarm/coordinator_test.go` — Slice 3, goroutine-leak + bus backpressure
  - `/home/user/Aura/internal/llm/prompt_test.go` — Slice 4, 5-turn invariants
  - `/home/user/Aura/internal/llm/openai_compat/client_test.go` — Slice 1, SSE fixtures
  - `/home/user/Aura/internal/identity/store_test.go` — build tag `db_integration` (Slice 1.7)
  - `/home/user/Aura/internal/conversations/store_test.go` — build tag `db_integration` (Slice 1.8)
  - `/home/user/Aura/internal/cron/scheduler_test.go` — build tag `db_integration` (Slice 6)
  - `/home/user/Aura/internal/agui/translator_test.go` — property-based (Slice 8)
  - `/home/user/Aura/internal/skills/installer_test.go` — fixture HTTP + path traversal (Slice 7d)

### Fixtures

- **Location:** `internal/<pkg>/testdata/` (Go's standard, ignored by build).
- **Format-by-domain examples** (PRD §Test discipline rigorosa #4):
  - SSE streams: `testdata/sse/<scenario>.sse` (text-only stream, tool-call multi-chunk delta-merge, error 429 no-retry, premature close ctx-cancel, Anthropic ephemeral `cache_control` passthrough)
  - SQL seed data: `testdata/sql/<scenario>.sql`
  - HTML/PDF: pseudonymized real documents for ingest tests
  - Cypher migrations test fixtures: programmatic Cypher inline in tests (NOT `.cypher` file fixtures — PRD §Slice 0.7 OQ #3 explicitly rejects them as premature optimization)
- **Realistic content mandatory.** No `{"foo":"bar"}`.

## Test structure (pattern)

```go
// internal/swarm/coordinator_test.go (Slice 3, prescriptive)
package swarm

import (
    "context"
    "testing"
    "time"

    "go.uber.org/goleak"
)

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}

func TestSpawn_ReturnsError_When_DepthExceedsMax(t *testing.T) {
    t.Parallel()
    c := NewLiveCoordinator(/* deps */)
    _, err := c.Spawn(context.Background(), Spawn{Depth: MaxSpawnDepth + 1})
    if err == nil || !strings.Contains(err.Error(), "spawn depth exceeded") {
        t.Fatalf("expected spawn-depth error, got %v", err)
    }
}

func TestJoin_Unblocks_When_ChildEmitsEscalateEvent(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    // setup: spawn 1 child, drive it via channel sync (no time.Sleep), assert Join returns the synthesized report
}
```

## Mocking

- **Standard library + interfaces.** No mocking framework as default. Stub via small struct that implements the interface (see the existing `stubClient` in `/home/user/Aura/cmd/aura/main.go:74-90` — canned `<-chan llm.Chunk` for the agent-loop skeleton).
- **HTTP/SSE wire layer:** drive against `testdata/*.sse` fixtures replayed by an in-process reader. Slice 1 acceptance lists the required scenarios explicitly: text-only stream, tool-call multi-chunk delta-merge, 429 no-retry bubble-up, premature close ctx-cancel, Anthropic ephemeral cache_control passthrough.
- **Sandbox sidecar:** integration tests under `//go:build sandbox_integration` hit a real sidecar; skipped silently when unreachable. Unit-level Stub `/home/user/Aura/internal/sandbox/sandbox.go:28-36` returns `"not yet implemented"` and is wired only for the agent-loop smoke.
- **Swarm Coordinator:** `Stub` in `/home/user/Aura/internal/swarm/swarm.go:32-42` used in agent-loop smoke; `LiveCoordinator` is the unit under test in `coordinator_test.go`.
- **Time:** prefer `synctest` (Go 1.24+) when the project upgrades; until then, deterministic channel sync with explicit `select { case <-ch: ... case <-time.After(5*time.Second): t.Fatal("timeout waiting for X") }`.

### What to mock

- External HTTP endpoints (LLM provider, web tools, Telegram API).
- Process boundaries (sandbox sidecar, embedding sidecar).
- Time (channel sync, never `time.Sleep`).

### What NOT to mock

- The Postgres driver (use real Postgres under `db_integration` build tag).
- The Neo4j driver (use real Neo4j under `neo4j_integration` build tag).
- The agent loop itself (unit-test it directly).
- Sentinel errors (`ErrAwaitingUserInput` must be the real type — assert byte-identical sentinel passthrough through `ActionRouter.Dispatch`).

## Coverage

- **Unit:** ≥ 75% per package. Enforced by CI (no silent skip).
- **Integration:** ≥ 60% per package.
- **Mutation testing:** spot-check ≥ 70% killed on slice-critical files (manual, per slice). Documented in commit body.

```bash
go test -coverprofile=cover.out ./internal/<package>/
go tool cover -func=cover.out                    # check function-level threshold
go tool cover -html=cover.out                    # local HTML report
```

## Test types

### Unit

- Scope: one Go package, mocks only at external boundaries (HTTP, process, time).
- Speed: < 5s per package.
- No build tag.

### Integration

- Scope: package + real backing service (Postgres, Neo4j, sandbox sidecar, embedding sidecar, multimodal sidecar).
- Gated by build tag.
- DB transactions rolled back per test (no cross-test pollution).
- Skip-on-no-container documented but the test still exists.

### Property-based (PRD §Test discipline rigorosa #5)

- Scope: invariants that must hold over a generated input space.
- Slice-scoped to 3 (swarm), 4 (KV cache PromptBuilder), 8 (AG-UI translator).
- Use `pgregory.net/rapid` or `github.com/leanovate/gopter`.

### Smoke / E2E

- Scope: agent loop end-to-end against a real model + real tools.
- Prompt MUST be natural (no asilo nido). See §Bad vs good table above.
- Assert artifact (tool was invoked, side-effect observable) AND reply, not reply alone.

## Common patterns

### Async / channel sync

```go
done := make(chan struct{})
go func() {
    // exercise the unit
    close(done)
}()
select {
case <-done:
case <-time.After(5 * time.Second):
    t.Fatal("timeout waiting for completion")
}
```

### Goroutine leak verification

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}

// or per-test:
func TestThingThatSpawnsGoroutines(t *testing.T) {
    defer goleak.VerifyNone(t)
    // ...
}
```

### Cancellation testing

```go
ctx, cancel := context.WithCancel(context.Background())
cancel() // pre-cancel
_, err := unit.Operation(ctx, ...)
if !errors.Is(err, context.Canceled) {
    t.Fatalf("expected context.Canceled, got %v", err)
}
```

### Sentinel error passthrough (ActionRouter contract)

```go
func TestDispatch_PropagatesErrAwaitingUserInput_Unchanged(t *testing.T) {
    sentinel := &ErrAwaitingUserInput{Question: "approve?", Kind: "approval"}
    r := NewActionRouter("task", map[string]ActionHandler{
        "schedule": func(_ context.Context, _ json.RawMessage) (ToolResult, error) {
            return ToolResult{}, sentinel
        },
    })
    _, err := r.Dispatch(ctx, []byte(`{"action":"schedule"}`))
    var got *ErrAwaitingUserInput
    if !errors.As(err, &got) || got != sentinel {
        t.Fatalf("expected byte-identical sentinel, got %v", err)
    }
}
```

### Race detector

`go test -race ./...` is mandatory on every touched package. Critical for Slice 3 (10 children interactive-paused simultaneously), Slice 6 (5 scheduler workers `FOR UPDATE SKIP LOCKED`), Slice 7c (Coordinator.ResumeChild + Spawn + Join sharing `sync.RWMutex` on `children` map — PRD line 1288).

## Gate 3 — Definition of Done checklist for tests

Source: `/home/user/Aura/prd.md` §Slice Q&A discipline Gate 3 (line 1532+):

- [ ] All §Acceptance bullets ticked, each verified by a concrete test.
- [ ] Smoke E2E end-to-end green on a clean state.
- [ ] Integration tests passing under their build tags. Skip-on-no-container documented but the test exists.
- [ ] Regression suite green (`go test ./...` full run, including build tags).
- [ ] Coverage threshold reached (≥ 75% unit, ≥ 60% integration) on the new package. Output from `go tool cover -func=cover.out` documented.
- [ ] Mutation testing spot-check (slice-critical): one core file under `go-mutesting`, score killed ≥ 70%. Documented in commit message.
- [ ] No goroutine leak: `goleak.VerifyNone(t)` green on every goroutine-spawning test.
- [ ] No data race: `go test -race` green on every touched package.
- [ ] No "asilo nido" tests — every E2E test re-read against the §Test discipline rigorosa example table; rewrite if it does not survive scrutiny.

---

*Testing analysis: 2026-05-28*
