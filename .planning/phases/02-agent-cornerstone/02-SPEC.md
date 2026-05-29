# Phase 2: Agent Cornerstone — Specification

**Created:** 2026-05-29
**Ambiguity score:** 0.105 (gate: ≤ 0.20)
**Requirements:** 8 locked

## Goal

Sostituire `internal/agent/loop.go` (Phase 1 skeleton, `Loop` struct concreta) con la **`Agent` interface unificata + workflow agents stolen-not-imported da `google/adk-go` v1.4.0**: pattern interface `Run(InvocationContext) iter.Seq2[*Event, error]` + 3 workflow agents built-in (Sequential/Loop/Parallel) + Budget tree con 3-cap contract (`AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`, `AURA_LOOP_DEDUP_WINDOW=3`) e child-inherits-parent's-remaining via shared atomic counter. Cornerstone sostanziale: ogni Phase successiva (3 LLM, 4 HITL/Conv/Id, 5 Sandbox-2a, 6 KV cache, 7 Web, 8 Sandbox-2b, 9 Swarm, 10 Scheduler, 11 Skills, 12 AG-UI, 13 Channels, 14 Onboarding, 15 Memory) implementa o consuma questa interface.

## Background

**Current state (post-Phase 1):**

- `internal/agent/loop.go` (132 LOC) — `Loop` struct concreta: `Model + Messages []llm.Message + Tools *tools.Registry + Client llm.Client + MaxSteps int`. Metodi `NewLoop(client, model, systemPrompt, reg) *Loop`, `Turn(ctx, userText) (string, error)`, `runTool(ctx, ToolCall) (string, error)`, `toolDefs() []llm.ToolDef`. `MaxSteps=8` hardcoded const `defaultMaxSteps`. NO interface, NO iter.Seq2, NO Event, NO budget tree, NO request_id.
- `internal/agent/tools/` — `Registry` + `spec.go` (Tool interface) + `manifest.go` (Render entries) + `search.go` (tool_search) + `text_response.go` (terminal tool). DROP-IN COMPATIBILE col nuovo design.
- `internal/llm/client.go` (78 LOC) — `Client` interface + `Message` + `ToolCall` + `Chunk` + `Request` + `ToolDef`. Già stream-friendly via `<-chan Chunk`. DROP-IN COMPATIBILE.
- Test coverage: ZERO (Phase 1 deliberately deferred test discipline a Phase 2 cornerstone).

**Gap to INFRA-03 target:**

9 nuovi concetti mancanti — Agent interface, iter.Seq2 streaming, Event/Actions struct, InvocationContext, Sequential/Loop/Parallel agents, Budget tree shared-atomic, child-inherits-remaining semantics, UUIDv7 trace_id propagation (OTel-compatible no-dep), dedup canonical hash window. Sostituzione (non aggiunta) di `Loop`: il `Loop` skeleton attuale viene **eliminato in Phase 2**, il `LlmAgent` reale (LLM client integration con tools dispatch) arriva in Phase 3 — il `aura chat` binary in Phase 2 espone solo `aura agent dry-run` (mock fixture per SC#2/3/4) finché Phase 3 non lo wires al LLM client.

**Reference:** [adk-go v1.4.0 agent/agent.go imports](https://raw.githubusercontent.com/google/adk-go/main/agent/agent.go) + [agent/workflowagents/loopagent/agent.go](https://raw.githubusercontent.com/google/adk-go/main/agent/workflowagents/loopagent/agent.go) pattern (Apache 2.0). Importare `google.golang.org/adk` significherebbe pull transitive di 34 deps (OTel + Gemini SDK + GCP + GORM + gRPC + 20 altri) — inaccettabile per Aura stack minimal. **Stealing pattern, NOT importing**, confermato 2026-05-29 dopo verifica imports adk-go transitive chain.

## Requirements

1. **Agent interface + InvocationContext**: Interface base unificata che tutte le fasi successive implementano.
   - Current: NESSUNA interface in `internal/agent/` — `Loop` è struct concreta accoppiata a `tools.Registry` + `llm.Client`.
   - Target: `internal/agent/agent.go` definisce `type Agent interface { Name() string; Description() string; Run(InvocationContext) iter.Seq2[*Event, error]; SubAgents() []Agent; FindAgent(name string) Agent }`. `InvocationContext` carries: `Ctx context.Context`, `Agent Agent` (self-reference per workflow), `RequestID uuid.UUID` (UUIDv7), `SpanID uuid.UUID` (UUIDv7, distinct per span), `ParentSpanID *uuid.UUID` (nil per root), `Branch string` (hierarchical path "root.iter-2.worker-3"), `Budget *Budget` (shared tree). Extension points (`SessionStore`, `LLMClient`, `Tools`) sono campo wire-noop in Phase 2 — riempiti in Phase 3/4. ~80 LOC totali.
   - Acceptance: `go build ./internal/agent/...` verde; `go vet` clean; il package esporta esattamente i type sopra; compile-time assert `var _ agent.Agent = (*workflow.LoopAgent)(nil)` in `loop_test.go`.

2. **Event + Actions full shape (AG-UI forward-compat)**: Type unico per tutti i runtime/LLM-emitted Event con field forward-compat per Phase 12 AG-UI gateway senza refactor.
   - Current: NESSUN Event type. Il `Loop.Turn` ritorna `(string, error)` finale.
   - Target: `internal/agent/event.go` definisce `type Event struct { RequestID uuid.UUID; SpanID uuid.UUID; ParentSpanID *uuid.UUID; Author string; Branch string; LLMResponse *LLMResponse; Actions Actions; Timestamp time.Time }`. `LLMResponse struct { Content string; ToolCalls []ToolCall; FinishReason string }`. `Actions struct { Escalate bool; StateDelta map[string]any; ArtifactDelta map[string]any }`. Field `Author` = `<workflow_agent_name>` per workflow-runtime-emitted (es. `"InterviewLoop"`), `"user"` per user-input events, agent name per LLM events (adk-go pure pattern, NON constant globale). ~70 LOC.
   - Acceptance: smoke test `TestEvent_FullShapeMarshalsToJSON` round-trip `Event` → JSON → `Event` byte-identical; field `LLMResponse` nullable correctly when nil; `Actions.StateDelta`/`ArtifactDelta` map type accepts nested `map[string]any`.

3. **Budget tree shared atomic**: 3-cap budget contract con shared atomic counter propagato per reference parent→child (SC#3 enforcement).
   - Current: `defaultMaxSteps = 8` const in `loop.go` (hardcoded, no env override, no wallclock, no dedup).
   - Target: `internal/agent/budget.go` definisce `type Budget struct { steps *atomic.Int32; deadlineWallclock time.Time; dedupWindow int; dedupRing *dedupRing }`. `NewBudgetFromEnv()` legge `AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`, `AURA_LOOP_DEDUP_WINDOW=3`. `(*Budget).ConsumeStep() (ok bool, reason string)` atomic decrement, returns `(false, "max_steps")` se contatore ≤ 0, `(false, "wallclock")` se `time.Now() > deadlineWallclock`. `(*Budget).RecordToolCall(name string, argsCanonicalJSON []byte) (dedup bool)` hash via `sha256(name + canonical_json_sorted_keys(args))`, sliding window ring buffer size `dedupWindow=3`, return `true` se hash detected `dedupWindow` consecutive times. `(*Budget).Child() *Budget` ritorna copia con stesso `*atomic.Int32` (shared), stesso `deadlineWallclock`, **distinct** `dedupRing` (child ha proprio dedup state per evitare false positive cross-branch). ~120 LOC.
   - Acceptance: `TestBudget_ConsumeStep_AtomicDecrement_NoRace` (10 goroutines × 100 consume = exactly 1000 decrements via `go test -race`); `TestBudget_Child_SharesStepsCounter` (parent consume 5 → child sees `Remaining()=20`); `TestBudget_Child_DistinctDedupRing` (parent dedup state non visibile a child); `TestBudget_RecordToolCall_CanonicalHashOrderIndependent` (`{"a":1,"b":2}` == `{"b":2,"a":1}` hash identical); `TestBudget_Wallclock_TerminatesAt300Sec` (synctest-based).

4. **Sequential workflow agent**: Esegue sub-agents una volta in ordine, propaga escalate upward (~30 LOC).
   - Current: NON ESISTE.
   - Target: `internal/agent/workflow/sequential.go` `type sequentialAgent struct { name string; subs []Agent }`. `Run(ctx) iter.Seq2[*Event, error]` itera `subs` chiamando `sub.Run(ctx.WithSubAgent(sub))` per ciascuno, yield ogni Event upward, return early on `event.Actions.Escalate=true`. `NewSequential(name string, subs ...Agent) Agent` constructor.
   - Acceptance: `TestSequentialAgent_RunsAllSubsInOrder` (3 subs A→B→C emit events sequenzialmente, order preserved); `TestSequentialAgent_PropagatesEscalate` (sub B emit `Escalate=true` → sub C NOT invoked, parent yields B's event then returns).

5. **Loop workflow agent (SC#2)**: Esegue sub-agents N volte o fino a escalate/budget-exhausted, emette explicit termination Event.
   - Current: NON ESISTE.
   - Target: `internal/agent/workflow/loop.go` `type loopAgent struct { name string; maxIterations uint; subs []Agent }`. `Run(ctx) iter.Seq2[*Event, error]` ripete sub-agents fino a (a) `maxIterations` raggiunte, (b) `event.Actions.Escalate=true` emesso da sub, OR (c) `ctx.Budget.ConsumeStep()` ritorna `false`. Su (c) emette **explicit budget-exhausted Event**: `Event{Author: <loop_agent_name>, Actions: Actions{Escalate: true, StateDelta: {"termination_reason": "budget_exhausted", "limit_hit": reason, "steps_consumed": <N>}}}`. ~50 LOC inclusa dedup ring check via `ctx.Budget.RecordToolCall(...)`. `NewLoop(name string, maxIter uint, subs ...Agent) Agent` constructor.
   - Acceptance: `TestLoopAgent_StopsAtMaxIterations` (`maxIter=3`, 3 emit then return); `TestLoopAgent_EscalatePropagation` (sub at iter 2 emit Escalate=true → return after iter 2); **SC#2** `TestLoopAgent_TerminatesAtMaxSteps_WithExplicitEvent` (mock sub emits same tool call forever, `Budget.maxSteps=25` → loop terminates at exactly 25 step consumption, final Event has `Author=<loop_name>` AND `Actions.StateDelta["termination_reason"]="budget_exhausted"` AND `Actions.StateDelta["limit_hit"]="max_steps"` AND `Actions.StateDelta["steps_consumed"]=25`); `TestLoopAgent_DedupWindow_TerminatesOn3SameToolCalls` (same `sha256(name+canonical_json)` 3 times → loop terminates with `limit_hit="dedup"`).

6. **Parallel workflow agent (SC#3)**: Esegue sub-agents concurrent, errgroup + ackChan backpressure, escalate da any child cancella siblings via ctx.
   - Current: NON ESISTE.
   - Target: `internal/agent/workflow/parallel.go` `type parallelAgent struct { name string; subs []Agent }`. `Run(ctx) iter.Seq2[*Event, error]` spawn `len(subs)` goroutines via `errgroup.WithContext(ctx.Ctx)`, ognuna chiama `sub.Run(childCtx)` con `childCtx = ctx.WithSubAgent(sub)` (stesso `Budget *atomic.Int32` shared). Events yielded via shared `resultsChan` con `ackChan` per backpressure (synchronous: consumer must ack before producer continues). Escalate da any child → `errGroup` cancel via egCtx → siblings ricevono ctx.Done() → drain pulito. ~80 LOC.
   - Acceptance: `TestParallelAgent_ChildrenShareParentBudget` (parent has 5 step budget, spawns 3 children each ConsumeStep() 1 time → total decremented = 3, remaining = 2; spawns 3 more concurrent → only 2 succeed, 1 gets `ok=false`); **SC#3** `TestParallelAgent_DepthChainBudgetShared_NotFresh` (depth 3 spawn chain: Root spawns 3 ParallelAgent each spawning 3 → 9 leaf agents, all share single `*atomic.Int32` starting at 25, totale step consumed across tree ≤ 25 (NOT 25³=15625)); `TestParallelAgent_EscalateFromAnyCancelsSiblings` (3 children, child[1] Escalate=true → child[0]+child[2] ricevono ctx canceled, no goroutine leak via `goleak.VerifyNone`); `TestParallelAgent_BackpressureAckChannel` (consumer slow → producer waits, no buffer growth).

7. **`aura agent dry-run` CLI smoke (SC#4)**: Sub-command Cobra che esegue un mock Agent (`scripts/loop_budget_smoke.sh` fixture compatible) per dimostrare SC#2/3/4 working e UUIDv7 request_id correlation OTel-compatible.
   - Current: `aura chat <msg>` esiste ma usa `stubClient` + `Loop` struct (rimosso in Phase 2). NESSUN `aura agent` sub-command.
   - Target: `cmd/aura/agent.go` aggiunge sub-command `aura agent dry-run [--request-id <uuid|auto>] [--max-steps <N>] [--max-wallclock-sec <N>] [--dedup-window <N>]`. Implementazione: costruisce mock `LoopAgent[InfiniteToolCallAgent]` (mock sub emits same tool_call forever), wires `Budget` da CLI flags (override env), itera `agent.Run(InvocationContext)` consumando `iter.Seq2[*Event, error]`, stampa ogni Event come JSON line su stdout (one event per line). Exit code 0 su termination naturale, 1 su error. `request-id=auto` → UUIDv7 generato boot-time; `request-id=<uuid>` → usato verbatim per smoke reproducibility. ~80 LOC nuovo + ~20 LOC modifiche `cmd/aura/main.go` per dispatch.
   - Acceptance: **SC#4** `aura agent dry-run --request-id auto` → stdout contiene N Event JSON lines, OGNI riga ha campo `request_id` valida UUIDv7 (`urn:uuid:01...`-style, version=7), tutti uguali tra loro (parent shared). `aura agent dry-run --request-id 0192f000-0000-7000-8000-000000000001` → stdout Event lines ALL hanno quel request_id verbatim. `aura agent dry-run --max-steps 25` (default) → termina dopo exactly 25 step + emette budget-exhausted Event. Smoke `scripts/loop_budget_smoke.sh` esegue il comando, conta Event JSON lines, asserisce `count==26` (25 step events + 1 budget-exhausted), grep `"limit_hit":"max_steps"` presente.

8. **Goroutine leak + race detector mandate (SC#1)**: Tutti i workflow agent tests usano `goleak.VerifyNone(t)` + `go test -race` clean.
   - Current: ZERO test in `internal/agent/`, ZERO goleak coverage.
   - Target: `internal/agent/workflow/workflow_test.go` ha `func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }`. Ogni test che spawn goroutines (`TestParallelAgent_*`) usa `defer goleak.VerifyNone(t)` come safety net. `go test -race -count=1 ./internal/agent/...` esce 0. CI job `unit-test` (race detector) gira sempre su Phase 2 code.
   - Acceptance: **SC#1** `go test -race -count=1 ./internal/agent/...` → exit code 0, no race warnings, no goroutine leak reports.

## Boundaries

**In scope:**
- `internal/agent/agent.go` — `Agent` interface + `InvocationContext` struct
- `internal/agent/event.go` — `Event` + `Actions` + `LLMResponse` types (full shape, AG-UI forward-compat)
- `internal/agent/budget.go` — `Budget` shared-atomic counter + 3-env-var contract + dedup ring buffer + canonical hash
- `internal/agent/workflow/sequential.go` — `SequentialAgent` (~30 LOC)
- `internal/agent/workflow/loop.go` — `LoopAgent` con dedup window check + budget-exhausted Event (~50 LOC)
- `internal/agent/workflow/parallel.go` — `ParallelAgent` con errgroup + ackChan backpressure (~80 LOC)
- `internal/agent/workflow/workflow_test.go` — table-driven test con goleak (SC#1) + 11 acceptance tests above
- `internal/agent/budget_test.go` — atomic invariants + race detector + canonical hash + sliding window
- `internal/agent/event_test.go` — JSON round-trip + nullable field
- `cmd/aura/agent.go` — `aura agent dry-run` sub-command (SC#4)
- `cmd/aura/main.go` (diff ~-15 / +10) — rimozione dispatch `case "chat"` legacy, aggiunta `case "agent"` dispatch
- `cmd/aura/agent_test.go` — CLI flag parsing test
- `scripts/loop_budget_smoke.sh` — fixture script SC#2 (mock sub emits same tool_call, asserisce 26 Event lines + grep budget_exhausted)
- Eliminazione: `internal/agent/loop.go` (132 LOC `Loop` struct concreta + `chatOnce` flow associato in `cmd/aura/main.go:64-95`)
- Eliminazione: `stubClient` in `cmd/aura/main.go:78-94` (non più usato senza `Loop`)
- Migration commit: `slice 0.9: agent runtime abstraction (interface + workflow agents + budget tree)` con `Loop` deletion atomico
- ENV vars nuove: `AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`, `AURA_LOOP_DEDUP_WINDOW=3` documentate in `.env.example`

**Out of scope** (NOT in Phase 2):
- `LlmAgent` implementation (real LLM client + tools dispatch) — quella è **Phase 3** (CORE-01). Phase 2 produce solo interface + workflow + mock dry-run.
- AG-UI gateway / Event mapping → SSE — quella è **Phase 12** (UX-01). Phase 2 produce Event full-shape forward-compat ma NON il transport mapping.
- Tools `Registry` integration con Agent — `tools.Registry` resta accessible via `LlmAgent` in Phase 3, Phase 2 non lo wires.
- Conversation persistence + multi-thread — quella è **Phase 4** (CORE-04). Phase 2 NON tocca `InvocationContext.SessionStore` se non come field placeholder.
- OTel dep transitive (`go.opentelemetry.io/otel/trace`) — Phase 2 produce **OTel-compatible shape** (trace_id + span_id + parent_span_id) **WITHOUT** importing OTel package. Drop-in compat per OTel integration Phase futura, no transitive dep oggi.
- `aura chat` user-facing CLI ripristino — `aura chat <msg>` con stubClient era Phase 1 demo, Phase 2 lo elimina. `aura chat` torna in **Phase 3** con LlmAgent wired.
- Swarm coordinator + DM-by-ID + tier-mapped models — quella è **Phase 9** (CAP-03), riusa `ParallelAgent` di Phase 2 ma aggiunge semantic Aura (cap `MAX_SPAWN_DEPTH=2`).
- `ask_user` pause/resume — quello è **Phase 4** (CORE-02), Phase 2 non implementa `Actions.Escalate` resume protocol oltre yield + return.
- Skill instruction-based / code snippets — quella è **Phase 11** (CAP-07, CAP-08).
- Memory ingest + GraphRAG — quella è **Phase 15** (UX-06/07/08/09).

## Constraints

- **Go 1.25+** mandatory per `iter.Seq2` (PRD amendment #1 sealed; Aura compose dev uses Go 1.26.3 verified `go version` 2026-05-29).
- **Pattern stolen-not-imported da adk-go v1.4.0** confirmed: NESSUN `import "google.golang.org/adk/..."` in any Aura file. Attribuzione via commento `// Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/loopagent/agent.go (Apache 2.0). Adattato per Aura con SC#2 budget exhaustion + SC#3 child-inherits-remaining + SC#4 UUIDv7 OTel-compat.`
- **Cap 600 LOC/file** enforced via `scripts/check-file-size.sh` (Phase 1 infra reuse). Stima file targets: budget.go ~120, parallel.go ~80, agent.go ~80, event.go ~70, loop.go ~50, sequential.go ~30, agent_dry_run.go ~80, test files cap each ~250.
- **goleak mandatory** (PRD amendment #15) — `TestMain` + `goleak.VerifyTestMain(m)` in `workflow_test.go`. Pattern Phase 1 (`internal/db/db_test.go:26-28`) replicato.
- **Race detector clean** — `go test -race -count=1 ./internal/agent/...` exit 0. CI `unit-test` job already runs `go test -race -count=1 ./...` (Phase 1 infra).
- **Coverage threshold** ≥75% unit (PRD §Test discipline rigorosa #8) sul package `internal/agent/`. Verificato via `go test -cover ./internal/agent/...` ≥ 0.75.
- **Mutation testing spot-check** ≥70% killed (PRD §Test discipline rigorosa #9) su `budget.go` (file più critico per SC#3 atomic invariants). Skill `mutation-testing` (trailofbits) consultabile.
- **Property-based testing** raccomandato (PRD §Test discipline rigorosa #5 per slice 3/4/8 esplicito, **opzionale ma consigliato per Slice 0.9** date le 2 invariants property-checkable: (a) "totale step consumed ≤ max_steps initial" e (b) "escalate event sempre yielded prima del return"). Library: `pgregory.net/rapid` o `gopter`. Skill `property-based-testing` (trailofbits) consultabile.
- **No native OTel dep** — `go.mod` di Phase 2 NON include `go.opentelemetry.io/otel/trace`. UUIDv7 generato via `github.com/google/uuid` v1.6.0 (light, transitive-clean: il package è già pull-in chain da migrate v4 Phase 1, verified `go mod graph | grep google/uuid`).
- **Canonical JSON sorted keys** per dedup hash — implementazione handrolled in `budget.go` (no deps), riusabile in Phase 4 conversation_turn hash, Phase 11 skill content_hash. Pattern RFC 8785-like (sort keys lex, no whitespace, no `null` field omitted).

## Acceptance Criteria

- [ ] **SC#1**: `go test -race -count=1 ./internal/agent/...` exits 0 with `goleak.VerifyTestMain(m)` in `workflow_test.go` — zero goroutine leak across all workflow agent tests.
- [ ] **SC#2**: `bash scripts/loop_budget_smoke.sh` exits 0; stdout contains 26 Event JSON lines (25 step events + 1 budget-exhausted); the final Event has `"author":"<loop_name>"` AND `"actions":{"escalate":true,"state_delta":{"termination_reason":"budget_exhausted","limit_hit":"max_steps","steps_consumed":25}}`.
- [ ] **SC#3**: `TestParallelAgent_DepthChainBudgetShared_NotFresh` passes — depth-3 spawn chain (Root spawns 3 ParallelAgent, each spawning 3 = 9 leaf agents) sharing single `*atomic.Int32` starting at `AURA_LOOP_MAX_STEPS=25` consumes ≤ 25 steps total across tree (NOT 25³=15625).
- [ ] **SC#4**: `aura agent dry-run --request-id auto` emits N Event JSON lines on stdout, every line has field `"request_id"` matching valid UUIDv7 regex `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, all lines share same `request_id` value. `aura agent dry-run --request-id 0192f000-0000-7000-8000-000000000001` reproduces verbatim across runs.
- [ ] `go vet ./internal/agent/... ./cmd/aura/...` clean.
- [ ] `go build ./...` clean.
- [ ] `bash scripts/check-file-size.sh` clean (no file >600 LOC).
- [ ] `golangci-lint run ./internal/agent/... ./cmd/aura/...` clean.
- [ ] `go test -cover ./internal/agent/...` reports ≥ 0.75 statement coverage on `internal/agent/` package.
- [ ] `internal/agent/loop.go` (Phase 1 132-LOC `Loop` struct) deleted from working tree; `git log --oneline -- internal/agent/loop.go` shows deletion commit in Phase 2 atomic.
- [ ] `cmd/aura/main.go` no longer dispatches `case "chat"` to `chatOnce`; dispatches `case "agent"` to `runAgent` (new); `stubClient` type deleted.
- [ ] `.env.example` updated with `AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`, `AURA_LOOP_DEDUP_WINDOW=3` documented.
- [ ] go.mod adds `github.com/google/uuid` (likely indirect → direct promotion; already in chain via `golang-migrate v4` transitive, verified).
- [ ] Compile-time interface assertions: `var _ agent.Agent = (*workflow.SequentialAgent)(nil)`, `(*workflow.LoopAgent)(nil)`, `(*workflow.ParallelAgent)(nil)` present in test files.
- [ ] Phase 2 atomic commit message matches `slice 0.9: agent runtime abstraction (interface + workflow + budget tree)` with Co-Authored-By trailer per CLAUDE.md.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                                |
|--------------------|-------|------|--------|---------------------------------------------------------------------|
| Goal Clarity       | 0.95  | 0.75 | ✓      | INFRA-03 + 4 SC ROADMAP + 4 HOW decisions locked Round 2.            |
| Boundary Clarity   | 0.85  | 0.70 | ✓      | Loop eliminato in Phase 2 lock + CLI dry-run in-scope lock + Event full-shape lock + 11 out-of-scope items enumerati. |
| Constraint Clarity | 0.85  | 0.65 | ✓      | Canonical hash lock + OTel-compat no-dep lock + cap 600 LOC + goleak/race + coverage ≥75% + mutation ≥70%. |
| Acceptance Criteria| 0.90  | 0.70 | ✓      | 4 SC + 9 supporting acceptance criteria + Event Author pattern lock = test asserzioni concrete machine-checkable. |
| **Ambiguity**      | 0.105 | ≤0.20| ✓      | Gate passed comfortable margin (0.20 - 0.105 = 0.095 headroom).      |

Status: ✓ = met minimum.

## Interview Log

| Round | Perspective | Question summary | Decision locked |
|-------|-------------|------------------|-----------------|
| 0     | (auto)      | First assessment vs INFRA-03 + ROADMAP SC | Goal/Constraint/Acceptance ≥ min; Boundary 0.65 below min (Loop fate, CLI scope, Event shape ambigui). |
| 1     | Researcher  | Loop skeleton fate durante Phase 2? | **Eliminato in Phase 2** (cleaner break, LlmAgent ex novo in Phase 3). |
| 1     | Researcher  | `aura agent dry-run` CLI smoke incluso? | **Sì incluso** (~80 LOC, dimostra SC#2/3/4 working). |
| 1     | Researcher  | Event shape full vs minimal? | **Full shape per AG-UI forward-compat** (~70 LOC event.go, zero refactor Phase 12). |
| 2     | Simplifier  | Budget propagation algo? | **Shared atomic counter** (single `*atomic.Int32` propagato per reference parent→child, ~30 LOC, race-safe natural per ParallelAgent). |
| 2     | Simplifier  | Dedup hash function? | **`sha256(name + canonical_json_sorted_keys(args))`** (researched: Claude Code #4277 hash-based, Aura threshold=3 più aggressivo di CC default 5 per single-user fail-fast). |
| 2     | Simplifier  | Event Author pattern? | **`Author = <workflow_agent_name>`** (adk-go v1.4.0 pure pattern, e.g. `"InterviewLoop"`; dettagli granulari in `Actions.StateDelta["termination_reason"]`; più informativo di constant globale `"system"`/`"agent_runtime"`). |
| 2     | Simplifier  | UUIDv7 propagation pattern? | **OTel-compatible shape (trace_id + span_id + parent_span_id), NO transitive OTel dep** (researched: 2026 OTel best practice, Datadog confirmation, drop-in compat per Phase futura OTel integration, ~20 LOC extra). |

Total interview rounds: 2 (max allowed 6). Gate met after Round 1 (Boundary 0.65→0.85). Round 2 elective to lock HOW decisions and reduce discuss-phase scope.

---

*Phase: 02-agent-cornerstone*
*Spec created: 2026-05-29*
*Next step: `/gsd-discuss-phase 2` — implementation decisions (file layout details, constructor signatures, error sentinel choices, env-default-vs-override precedence, test fixture organization).*
