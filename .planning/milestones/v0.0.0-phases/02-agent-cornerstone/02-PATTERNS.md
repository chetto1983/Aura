# Phase 2: Agent Cornerstone - Pattern Map

**Mapped:** 2026-05-29
**Files analyzed:** 18 (create) + 3 (modify) + 1 (delete)
**Analogs found:** 18 / 18 (every new file has a live-repo analog; adk-go is the external pattern source, NOT a repo analog)

> **Scope note for the planner:** Phase 2 is mostly *net-new* substrate (no prior `Agent`/`Event`/`Budget` exists). So "analog" here means **the live-repo file whose conventions the new file must replicate** (package-doc style, error wrapping, interface/factory idiom, JSON tagging, test scaffolding) — not a file doing the same job. The *behavioral* pattern source is `D:/tmp/adk-go-study/...` (stolen-not-imported, Apache 2.0), already mapped exhaustively in `02-RESEARCH.md` §Architecture Patterns. This document layers the **Aura house-style** on top of those behavioral patterns. Where RESEARCH gives the algorithm, PATTERNS gives the local convention to copy.

## File Classification

| New/Modified File | Role | Data Flow | Closest Repo Analog | Match Quality |
|-------------------|------|-----------|---------------------|---------------|
| `internal/agent/agent.go` (new) | interface + context type | request-response (streaming) | `internal/agent/tools/spec.go` (interface + doc-comment idiom); `internal/llm/client.go` (value-type shape) | role-match |
| `internal/agent/event.go` (new) | model / DTO | transform (struct↔JSON) | `internal/llm/client.go` (struct + json tags + nullable pointers) | exact |
| `internal/agent/budget.go` (new) | service (resource governor) | event-driven (atomic counter) | `internal/db/config.go` (Config struct) + `internal/db/migrate.go` (fail-fast sentinel) | role-match |
| `internal/agent/budget_dedup.go` (new, split per RESEARCH #1) | utility (ring + fingerprint) | transform | `internal/agent/tools/manifest.go` (stable-order helper); `internal/canonicaljson` (sibling) | partial |
| `internal/agent/errors.go` (new) | sentinel errors | — | `internal/db/migrate.go` L28-29 (`errMissing*` const + sentinel idiom) | exact |
| `internal/agent/workflow/sequential.go` (new) | controller (orchestrator) | request-response (streaming) | `internal/agent/loop.go` (the loop being replaced; concrete iteration) | role-match |
| `internal/agent/workflow/loop.go` (new) | controller (orchestrator) | event-driven (loop+budget) | `internal/agent/loop.go` (MaxSteps loop, the thing the interface generalizes) | exact (replaces it) |
| `internal/agent/workflow/parallel.go` (new) ⚠ CRITICAL | controller (concurrent orchestrator) | pub-sub (errgroup fan-out) | none in repo — adk-go is the template; `internal/db/db_test.go` goleak discipline applies | no-repo-analog |
| `internal/agent/agenttest/mocks.go` (new) | test util | event-driven | `internal/agent/tools/text_response.go` (minimal interface impl idiom) | role-match |
| `internal/canonicaljson/canonicaljson.go` (new) | utility (serializer) | transform | `internal/agent/tools/manifest.go` (sort-for-stability rationale) | partial |
| `cmd/aura/agent.go` (new) | route (subcommand) | request-response (CLI→stdout) | `cmd/aura/db.go` (`runDB` dispatcher + flag/exit idiom) | exact |
| `cmd/aura/main.go` (modify) | route dispatcher | — | self (L27-46 switch; remove `chat`/`chatOnce`/`stubClient`) | self |
| `internal/agent/workflow/workflow_test.go` (new) | test (TestMain) | — | `internal/db/db_test.go` L26-28 (`goleak.VerifyTestMain`) | exact |
| `internal/agent/workflow/{sequential,loop,parallel}_test.go` (new) | test | — | `internal/db/db_test.go` (table-driven + race); `internal/config/config_test.go` (t.Setenv) | exact |
| `internal/agent/budget_test.go` (new) | test (race/property) | — | `internal/db/db_test.go` (race discipline) | role-match |
| `internal/agent/event_test.go` (new) | test (round-trip/property) | — | `internal/config/config_test.go` (table-driven assertions) | role-match |
| `internal/canonicaljson/canonicaljson_test.go` (new) | test (fuzz) | — | `internal/config/config_test.go` (assertion style) | partial |
| `cmd/aura/agent_test.go` (new) | test (CLI) | — | `internal/config/config_test.go` (t.Setenv + table) | role-match |
| `scripts/loop_budget_smoke.sh` (new) | smoke script | — | `scripts/neo4j_smoke.sh` (set -euo pipefail; delegate-to-Go idiom) | role-match |
| `.env.example` (modify) | config | — | self (append SPEC 3 + A7 vars) | self |
| `internal/agent/loop.go` (DELETE) | — | — | — | — |

## Pattern Assignments

### `internal/agent/agent.go` (interface + context type)

**Behavioral source:** adk-go `agent/agent.go` L43-52 (interface, seal removed per D-01) + RESEARCH Pattern 1/2/5. **House-style analog:** `internal/agent/tools/spec.go`.

**Package-doc + interface idiom to copy** (`tools/spec.go:1-33`): a multi-line `// Package agent ...` doc stating ownership + the design rule, then the interface declared right after imports with a one-line doc per method's contract. Replicate this exact shape — package doc explains *why the interface is open* (D-01), then:
```go
// Agent is the cornerstone contract every later phase implements or consumes.
// Open by design (no unexported seal) — Phase 3 LlmAgent and Phase 9 swarm
// implement this directly. Pattern derivato da google/adk-go v1.4.0
// agent/agent.go (Apache 2.0); the internal() seal at L51 is intentionally removed.
type Agent interface {
	Name() string
	Description() string
	Run(InvocationContext) iter.Seq2[*Event, error]
	SubAgents() []Agent
	FindAgent(name string) Agent
}
```

**InvocationContext field-not-embed idiom (D-24):** mirror `db.Config` (`db/config.go:17-25`) — a plain struct with per-field doc comments. `Ctx context.Context` is a **named field, never embedded**. Add the doc comment verbatim from D-24: "InvocationContext is single-Run-scoped; never store on a long-lived struct, never cache, never share across invocations." `WithContext`/`WithSubAgent` return a **copy** (RESEARCH Pattern 5 shows `c := ic; c.Agent = sub; return c`).

**No analog in repo for `iter.Seq2`** — it is net-new to this codebase. Follow RESEARCH Pattern 3 (the 4 footguns) verbatim; no existing file demonstrates range-over-func.

---

### `internal/agent/event.go` (model / DTO)

**Analog:** `internal/llm/client.go` (exact — struct-with-json-tags DTO, the canonical shape file in this repo).

**Struct + json-tag idiom to copy** (`llm/client.go:24-61`): note the `omitempty` on optional fields, pointer-for-nullable convention, and the **per-field doc comment explaining when each field populates** (e.g. `// ToolCalls populates only for assistant messages`). Replicate exactly for `Event`/`Actions`/`LLMResponse`:
```go
// from llm/client.go:24-30 — copy this tagging discipline
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
```

**Reuse `llm.ToolCall` directly (D-17, `code_context` L132):** `Event.LLMResponse.ToolCalls` is `[]llm.ToolCall`; surface its existing `ID` field (`llm/client.go:35`) as the AG-UI `ToolCallID`. Do NOT redefine a ToolCall type — import `internal/llm`.

**Nullable convention:** `llm/client.go:57-61` uses `*ToolCall` for "exactly one of". Apply to `LLMResponse *LLMResponse` on `Event` (nil when the Event is not an LLM turn) and `ParentSpanID` nil at root (RESEARCH test `TestEvent_TraceID16Bytes_SpanID8Bytes`).

**Trace IDs (D-16):** `RequestID`/`TraceID` = `uuid.UUID` (16B, `uuid.NewV7()`); `SpanID`/`ParentSpanID` = `[8]byte` from `crypto/rand` — NOT uuid. No repo analog (uuid is a new dep, A6); follow RESEARCH Pattern 8.

---

### `internal/agent/budget.go` (service — resource governor)

**Analog:** `internal/db/config.go` (Config-struct + doc idiom) + `internal/db/migrate.go` (fail-fast sentinel) + RESEARCH Pattern 5.

**Fail-fast env parsing (D-06) — DIVERGE from `config.go`:** the repo has two env idioms. `config/config.go:114-124` `envIntDefault` **silently absorbs parse errors** (returns fallback). D-06 requires the *opposite* — `NewBudgetFromEnv` must **fail-fast** on malformed env (`AURA_LOOP_MAX_STEPS=abc` → error). Copy the **`migrate.go` sentinel-string idiom** instead:
```go
// from db/migrate.go:28-29 — the load-bearing-error-string idiom; tests assert verbatim
const errMissingMigrateURL = "AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17"
...
if migrateURL == "" {
	return 0, errors.New(errMissingMigrateURL)
}
```
For Budget: define exact error strings (e.g. `AURA_LOOP_MAX_STEPS=%q: not an integer`) as consts/wrapped errors, return them from `NewBudgetFromEnv() (*Budget, error)`. This matches the Phase-1 boot fail-fast discipline (CONTEXT D-06 ref).

**Config-struct doc idiom (`db/config.go:17-25`):** per-field doc comment with the default in-line (`// default 10`). Apply to the Budget struct fields (`steps *atomic.Int32`, `deadlineWallclock time.Time`, `dedupWindow int`, `branchSoftCap int`).

**Error wrapping convention (repo-wide):** `fmt.Errorf("<verb> <noun>: %w", err)` — see `db/db.go:35,54,58` and `loop.go:62` (`llm stream step %d: %w`). Every error in budget.go follows this.

**ConsumeStep / Child / softCap:** no repo analog (net-new atomic frontier). Follow RESEARCH Pattern 5 + the FINDING note (soft-cap ~50 LOC, passive cap).

---

### `internal/agent/budget_dedup.go` (utility — ring + two-tier fingerprint)

**Analog:** `internal/agent/tools/manifest.go` (stable-ordering-for-determinism rationale) + sibling `internal/canonicaljson`.

**Why this file is split out (RESEARCH #1, A3):** `budget.go` + dedup ring + soft cap + wallclock > 600 LOC → CLAUDE.md "no god class". Split is pre-emptive, same package.

**Determinism-comment idiom to copy** (`manifest.go:19-21`): manifest.go documents *why* ordering is stable ("any reshuffle invalidates the provider-side prompt cache"). Mirror this — document *why* the fingerprint is `sha256(name + canonicaljson.Marshal(args))` and *why* result is veto-only (D-18 fail-safe). Consumes `internal/canonicaljson.Marshal`.

**Two-tier fingerprint + ring:** no repo analog. Follow RESEARCH Pattern 7 (window=3, period-1 + period-2, ring ≥ max(window,4), `AURA_LOOP_DEDUP_EXEMPT_TOOLS` allowlist), but implement it as a two-phase API: `BeforeToolCall(name, canonicalArgs)` blocks repeats before side effects, and `AfterToolResult(name, canonicalArgs, resultPreview)` records the bounded result preview as a progress veto. Do not design a single method that needs both pre-execution blocking and post-execution result data at once.

---

### `internal/agent/errors.go` (sentinel errors)

**Analog:** `internal/db/migrate.go:28-29` (exact — `const errMissing... = "..."` + `errors.New`).

Export `var ErrBudgetExhausted = errors.New("...")` (D-04, for Phase 3/9 programmatic consumers). Note the divergence: db's sentinel is an unexported const-string; here D-04 wants an **exported `error` var** for `errors.Is` checks. Use `errors.New` at package scope.

---

### `internal/agent/workflow/sequential.go` + `loop.go` (controllers)

**Behavioral source:** adk-go `sequentialagent/agent.go` L79-90 and `loopagent/agent.go` L75-105 (RESEARCH Pattern 3 has the verbatim Aura-adapted LoopAgent body). **House-style analog:** `internal/agent/loop.go` (the file being deleted — its loop/error idiom carries over).

**Factory-returns-interface idiom (D-02):** no exact repo precedent, but `tools.NewRegistry()` (`spec.go:42`) shows the constructor convention (`func NewX() *X`). Diverge to return the *interface*: `func NewLoop(name string, maxIter uint, subs ...Agent) Agent`. **Typed-nil guard (D-02):** never `return (*LoopAgent)(nil)`.

**Loop body to replace:** old `loop.go:53-103` is the `for step := 0; step < MaxSteps` concrete loop. The new LoopAgent generalizes it into `iter.Seq2` with `Budget.ConsumeStep()` replacing the bare `step` counter. Carry over the error-wrap style (`loop.go:62`).

**Author explicit (D-14):** each workflow agent sets `Author` explicitly on emitted Events (no base-struct embed in Aura's open interface). Branch dot-join + `.iter-<N>` per D-15.

**Attribution comment (CONTEXT `<specifics>`):** add verbatim:
`// Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/loopagent/agent.go (Apache 2.0). Adattato per Aura con SC#2 budget exhaustion + SC#3 child-inherits-remaining + SC#4 UUIDv7 OTel-compat.`

---

### `internal/agent/workflow/parallel.go` (CRITICAL — concurrent orchestrator)

**No repo analog** — this is the highest-risk net-new surface. Behavioral template is adk-go `parallelagent/agent.go` L67-164, **stolen near-verbatim** with exactly two documented divergences (D-03 captured-cancel-for-escalate; D-05 drain `(nil,nil)` not `ctx.Err()`). RESEARCH Pattern 4 contains the full Aura-adapted skeleton (`Run` + `runSub`) — the planner should hand executors that skeleton directly.

**Repo discipline that DOES apply:** `golang.org/x/sync/errgroup` is in go.mod (indirect → promote to direct). Goleak discipline from `db_test.go` (below) is mandatory here (D-23). Error-wrap style repo-wide. Every channel op is a 2-arm `select` with `case <-ctx.Done(): return nil` (D-05). `defer cancel()` + `defer close(done)`.

---

### `internal/agent/agenttest/mocks.go` (test util — D-07)

**Analog:** `internal/agent/tools/text_response.go` (minimal-interface-impl idiom).

Mocks (`InfiniteToolCallAgent`, `EmitNThenEscalate`, `RecordingAgent`, counting mock for SC#3) implement the `Agent` interface. Copy the `text_response.go` shape: tiny struct, methods returning canned values:
```go
// text_response.go:12-18 — the minimal-impl idiom to mirror per mock
type TextResponse struct{}
func (TextResponse) Spec() Spec { ... }
```
**Import direction (RESEARCH OQ#2):** `agenttest` imports `internal/agent` (one direction); `agent` never imports `agenttest` outside `_test.go`. Standard test-helper pattern, no cycle.

---

### `internal/canonicaljson/canonicaljson.go` (utility — serializer)

**Analog:** `internal/agent/tools/manifest.go` (sort-for-determinism rationale). Behavioral spec: RESEARCH Pattern 6 (A3 — NOT RFC-8785).

**Package-doc must state the rescope (A3):** "Deterministic serializer for Aura-internal hashing. NOT RFC-8785 (no cross-system crypto-signature consumer → float-canonicalization minefield avoided)." `Marshal(v any) ([]byte, error)`: sort map keys by Go byte order; `json.Decoder.UseNumber()` so numbers stay literal text (`1` ≠ `1.0`); strict-reject NaN/Inf/func/chan (error, never silent coerce). Error-wrap style repo-wide. No `internal/agent` import (consumed by it, not the reverse).

---

### `cmd/aura/agent.go` (route — `aura agent dry-run`)

**Analog:** `cmd/aura/db.go` (exact — the subcommand-handler idiom).

**`runX(args []string)` dispatcher idiom to copy** (`db.go:19-44`): sub-subcommand switch, `fmt.Fprintln(os.Stderr, "usage: ...")` + `os.Exit(1)` on bad args, load config, print human status, exit non-zero on error:
```go
// db.go:19-29 — copy this handler entry shape for runAgent
func runDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}")
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err != nil { fmt.Fprintln(os.Stderr, "config load:", err); os.Exit(1) }
	...
}
```
**Flag sentinel precedence (D-06):** flags default `-1` ("unset") → fall through to `NewBudgetFromEnv()`; non-`-1` overrides. RESEARCH §Code Examples "CLI flag sentinel precedence" has the exact pattern. **Keep logic out of `main()`** so `agent_test.go` can capture stdout (RESEARCH coverage strategy). One Event = one JSON line via `canonicaljson.Marshal`. Builds a mock `LoopAgent[InfiniteToolCallAgent]` from `agenttest`.

---

### `cmd/aura/main.go` (modify — dispatcher)

**Self-analog** (`main.go:22-46`). Remove `case "chat"` + `chatOnce` (L30-35, 64-73) + `stubClient` (L75-94). Add `case "agent": runAgent(os.Args[2:])`. Update the package doc-comment subcommand list (L1-9) and `usage()` (L48-50). Mirror the existing `case "db": runDB(os.Args[2:])` wiring exactly. Drop the now-unused `internal/agent` + `internal/llm` imports if `chatOnce`/`stubClient` were their only consumers (verify with `go build`).

---

## Shared Patterns

### Test scaffolding — goleak TestMain
**Source:** `internal/db/db_test.go:26-28`
**Apply to:** `internal/agent/workflow/workflow_test.go` (SC#1), and any package with goroutine-spawning tests.
```go
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```
Per-test leak check (D-23): `defer goleak.VerifyNone(t)` in the ParallelAgent break-early test. Run with `go test -race -count=1 ./internal/agent/...`.

### Test scaffolding — env-driven table tests with t.Setenv
**Source:** `internal/config/config_test.go:10-50`
**Apply to:** `cmd/aura/agent_test.go`, `budget_test.go` (NewBudgetFromEnv cases), `event_test.go`.
Copy the `clearEnv(t)` helper + `t.Setenv` baseline-reset idiom so env-dependent tests are hermetic. Table-driven assertions with `t.Errorf("field: want %q, got %q", ...)`.

### Error wrapping
**Source:** `internal/db/db.go:35,54,58`, `internal/agent/loop.go:62`
**Apply to:** every error path in all new `.go` files.
```go
return ..., fmt.Errorf("<verb> <noun>: %w", err)   // wrap with %w, lower-case verb-noun prefix
```
Load-bearing literal-error-string + sentinel idiom (`db/migrate.go:28-29`) applies to `errors.go` (`ErrBudgetExhausted`) and `NewBudgetFromEnv` fail-fast messages (D-06) — tests assert these verbatim, so do not paraphrase.

### Determinism rationale comment
**Source:** `internal/agent/tools/manifest.go:19-21`
**Apply to:** `canonicaljson.go`, `budget_dedup.go`.
Document *why* output is deterministically ordered (cache stability / hash stability), not just *that* it is sorted — matches the existing house comment style that cites the consequence of getting it wrong.

### Smoke-script idiom
**Source:** `scripts/neo4j_smoke.sh:11` + `scripts/check-file-size.sh`
**Apply to:** `scripts/loop_budget_smoke.sh`.
`#!/usr/bin/env bash` + `set -euo pipefail`. Prefer delegating heavy logic to a Go harness over fragile bash; for SC#2 use `wc -l` (assert `== 26`) and `grep '"limit_hit":"max_steps"'` against `aura agent dry-run --max-steps 25` stdout. Git-Bash compatible (Windows host).

### Config struct + per-field doc + in-line default
**Source:** `internal/db/config.go:17-25`
**Apply to:** the `Budget` struct, `InvocationContext` struct.
Plain struct, one doc-comment per field stating the default in-line (`// default 25`).

## No Analog Found

Files whose **behavior** has no live-repo precedent — planner directs executors to the adk-go template + RESEARCH patterns (house-style still applies from the Shared Patterns above):

| File | Role | Data Flow | Reason | Use Instead |
|------|------|-----------|--------|-------------|
| `internal/agent/workflow/parallel.go` | concurrent orchestrator | pub-sub | No errgroup/channel-fan-out code exists in repo | adk `parallelagent/agent.go` L67-164 + RESEARCH Pattern 4 (full skeleton) |
| `internal/agent/budget.go` (ConsumeStep/Child) | atomic governor | event-driven | No `atomic.Int32` frontier-budget exists | RESEARCH Pattern 5 + D-09/10/11/12/13 |
| `internal/agent/event.go` (iter.Seq2 / trace IDs) | DTO | transform | No `iter.Seq2`, no uuid, no crypto/rand SpanID in repo (all net-new; uuid is new dep A6) | RESEARCH Pattern 3 + 8 |
| `internal/canonicaljson/canonicaljson.go` | serializer | transform | Deliberately hand-rolled (A3 — no library matches rescoped reqs) | RESEARCH Pattern 6 |

## Metadata

**Analog search scope:** `internal/agent/`, `internal/agent/tools/`, `internal/llm/`, `internal/db/`, `internal/config/`, `cmd/aura/`, `scripts/`
**Files scanned (read in full or targeted):** `internal/agent/loop.go`, `internal/llm/client.go`, `cmd/aura/main.go`, `cmd/aura/db.go`, `internal/agent/tools/{spec,manifest,search,text_response}.go`, `internal/db/{db_test,config,migrate}.go`, `internal/config/{config,config_test}.go`, `scripts/neo4j_smoke.sh`, `go.mod`
**Behavioral pattern source (external, not a repo analog):** `D:/tmp/adk-go-study/agent/...` (mapped in 02-RESEARCH.md §Architecture Patterns 1-8)
**Pattern extraction date:** 2026-05-29
