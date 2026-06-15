# Phase 2: Agent Cornerstone - Research

**Researched:** 2026-05-29
**Domain:** Go 1.26 agent-runtime substrate — `iter.Seq2` streaming interface, errgroup concurrency, shared-atomic budget tree, deterministic JSON, OTel-compatible trace IDs
**Confidence:** HIGH (decisions pre-locked + over-validated by 5 scouts; this research is implementation-readiness, not decision-derivation)

## Summary

Phase 2 implements the unified `Agent` interface and three workflow agents (Sequential/Loop/Parallel) stolen-not-imported from `google/adk-go`, plus a 3-cap Budget tree with child-inherits-parent's-remaining semantics. All 24 design decisions (D-01–D-24) and 7 amendments (A1–A7) in `02-CONTEXT.md` are **locked** — this research does not re-open them. It maps each locked decision to concrete implementation patterns, surfaces the Go-1.26 concurrency footguns the planner must guard against, and specifies the **Validation Architecture** in full detail so the Nyquist gate has a complete test map.

The single highest-risk surface is **ParallelAgent** (`internal/agent/workflow/parallel.go`): it combines four independently-dangerous Go patterns — `iter.Seq2` range-over-func, `errgroup` cancellation choreography, channel backpressure with ack, and a shared `*atomic.Int32` mutated concurrently. The adk-go `parallelagent/agent.go` source is a near-verbatim template, but Aura **diverges deliberately in two places** (D-05: drain `(nil,nil)` not `ctx.Err()`; D-03: capture `cancel` for escalate). The other diverging surfaces (open interface D-01, exported structs D-02, two-tier dedup D-18, 8-byte SpanID D-16) are lower-risk.

**Primary recommendation:** Plan ParallelAgent as the critical-path task with the most test budget (goleak break-early test D-23, depth-3 shared-counter test SC#3, TOCTOU race test D-11, backpressure test). Steal adk's channel structure verbatim, then apply the two documented divergences as explicit edits. Add `github.com/google/uuid v1.6.0` and `pgregory.net/rapid v1.3.0` as the only two new deps. Budget the files conservatively against the 600-LOC rule — `budget.go` at ~120 estimated LOC plus dedup ring (D-18/D-20) plus soft cap (D-12) plus wallclock (D-13) realistically lands ~200+ LOC and **must split** into `budget.go` + `budget_dedup.go`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Agent interface + InvocationContext | Runtime substrate (`internal/agent`) | — | The cornerstone contract every later phase implements/consumes |
| Event/Actions/LLMResponse shape | Runtime substrate (`internal/agent`) | Phase-12 AG-UI (consumer) | Forward-compat shape owned here; transport mapping is Phase 12 |
| Budget tree (steps/wallclock/dedup) | Runtime substrate (`internal/agent`) | Phase-9 swarm (consumer) | Shared-atomic frontier owned here; swarm adds depth-cap semantics |
| Workflow orchestration (Seq/Loop/Parallel) | Runtime substrate (`internal/agent/workflow`) | — | Pure orchestration; no LLM/tool wiring (Phase 3) |
| Canonical JSON serialization | Shared util (`internal/canonicaljson`) | Phase-4 conv hash, Phase-11 skill hash (consumers) | Determinism primitive reused across phases |
| Trace correlation IDs (UUIDv7/SpanID) | Runtime substrate (`internal/agent`) | Future OTel slice (consumer) | OTel-compatible *shape* only; real OTel import deferred |
| CLI dry-run smoke | Binary entry (`cmd/aura`) | — | Demonstrates SC#2/3/4; consumes `agenttest` mocks |
| Mock agents | Test util (`internal/agent/agenttest`) | Phase-3/9 (consumers) | Reusable; zero mock duplication per D-07 |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `iter` | go1.26.3 | `iter.Seq2[*Event, error]` range-over-func | `[VERIFIED: go version go1.26.3 windows/amd64]` GA since 1.23; the entire interface hinges on it |
| Go stdlib `sync/atomic` | go1.26.3 | `atomic.Int32` shared step counter (D-10/D-11) | `[CITED: pkg.go.dev/sync/atomic]` race-safe by construction; the frontier-budget primitive |
| Go stdlib `crypto/rand` | go1.26.3 | 8-byte SpanID/ParentSpanID (D-16) | `[CITED: w3.org/TR/trace-context]` OTel/W3C SpanID is exactly 8 bytes; matches OTel `RandomIDGenerator` |
| Go stdlib `context` | go1.26.3 | `WithDeadline` wallclock (D-13), cancellation choreography (D-23) | `[CITED: go.dev/blog/context-and-structs]` end-to-end cancellation propagation |
| Go stdlib `crypto/sha256` | go1.26.3 | dedup fingerprint `sha256(name+canonical_args)` (D-18) | stdlib; no dep |
| `golang.org/x/sync/errgroup` | v0.18.0 | ParallelAgent fan-out + `WithContext` cancel (D-03) | `[VERIFIED: go.mod]` ALREADY PRESENT (indirect) — promote to direct, no new dep |
| `github.com/google/uuid` | v1.6.0 | UUIDv7 RequestID/TraceID (D-16) | `[VERIFIED: go list -m -versions → v1.6.0 latest]` **NEW direct dep** (A6 confirmed absent from go.mod/go.sum) |

### Supporting (test-only)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `go.uber.org/goleak` | v1.3.0 | goroutine-leak verification (SC#1, D-23) | `[VERIFIED: go.mod]` ALREADY PRESENT; `VerifyTestMain` in `workflow_test.go` |
| `pgregory.net/rapid` | v1.3.0 | property-based tests (D-21) | `[VERIFIED: go list -m -versions → v1.3.0 latest]` **NEW test dep**; budget invariants + Event JSON round-trip |

### Alternatives Considered
| Instead of | Could Use | Tradeoff | Verdict |
|------------|-----------|----------|---------|
| `github.com/google/uuid` NewV7 | hand-rolled UUIDv7 from `crypto/rand` | ~30 LOC, avoids dep, but must re-implement monotonic counter + RFC-9562 bit-layout correctly | LOCKED to google/uuid (D-16); battle-tested, monotonic, 1 dep is acceptable |
| `pgregory.net/rapid` | `github.com/leanovate/gopter` | gopter is older API; TESTING.md lists both as acceptable | rapid recommended (D-21 names it explicitly; simpler shrinking API) |
| importing `google.golang.org/adk` | stealing the pattern | adk pulls 34 transitive deps (OTel + Gemini + GCP + gRPC) | LOCKED to steal-not-import (SPEC Constraint L104) |

**Installation:**
```bash
go get github.com/google/uuid@v1.6.0
go get pgregory.net/rapid@v1.3.0   # test-only; lands in require block
go mod tidy
# errgroup: no `go get` needed — promote golang.org/x/sync from indirect to direct via use
```

**Version verification (run 2026-05-29):**
- `github.com/google/uuid` → latest **v1.6.0** (published 2024-01-23). `[VERIFIED: go list -m -versions]` `NewV7() (UUID, error)` uses `crypto/rand` by default and **is monotonic within a process** — the v1.6.0 impl tracks the last millisecond timestamp and increments a 12-bit `rand_a` sequence on same-ms collisions per RFC-9562 method 1. `[CITED: pkg.go.dev/github.com/google/uuid + github.com/google/uuid/blob/master/version7.go]`
- `pgregory.net/rapid` → latest **v1.3.0**. `[VERIFIED: go list -m -versions]`
- `golang.org/x/sync` → **v0.18.0** present. `[VERIFIED: go.mod L18]`
- `go.uber.org/goleak` → **v1.3.0** present. `[VERIFIED: go.mod L10]`

## Package Legitimacy Audit

> slopcheck was not available in this environment (Windows, no pip slopcheck). All NEW packages are gated by manual registry verification + provenance below. Both are extremely well-known, long-lived Go packages with millions of downloads — they are **not** hallucination candidates. Planner SHOULD still add a `checkpoint:human-verify` before `go get` per the graceful-degradation rule, though risk here is minimal.

| Package | Registry | Age | Source Repo | Verification | Disposition |
|---------|----------|-----|-------------|--------------|-------------|
| `github.com/google/uuid` | Go module proxy | v1.6.0 published 2024-01-23; project since 2016 | github.com/google/uuid | `go list -m -versions` resolved v1.6.0 as latest; canonical Google-owned module | Approved (new direct dep, A6) |
| `pgregory.net/rapid` | Go module proxy | v1.0.0 → v1.3.0 stable line | github.com/flyingmutant/rapid | `go list -m -versions` resolved v1.3.0; named in Aura TESTING.md + D-21 | Approved (new test dep) |
| `golang.org/x/sync` | Go module proxy (golang.org) | v0.18.0 | go.googlesource.com/sync | already in go.sum (A6) | Approved (promote indirect→direct) |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
                          aura agent dry-run  (cmd/aura/agent.go)
                                   │
                   builds Budget from CLI flags (D-06: flag>env>default)
                                   │
                                   ▼
        ┌──────────────────────────────────────────────────────┐
        │  InvocationContext (single-Run-scoped, D-24)           │
        │  { Ctx, Agent(self), RequestID(uuidv7), SpanID(8B),    │
        │    ParentSpanID*(8B), Branch, Budget* }                │
        └──────────────────────────────────────────────────────┘
                                   │  Run(ctx) → iter.Seq2[*Event, error]
                                   ▼
            ┌──────────────┬───────────────┬──────────────────┐
            │ SequentialAg │   LoopAgent    │  ParallelAgent   │
            │ ctx.WithSub  │ ctx.WithSub    │ Budget.Child()   │
            │ (shares ring)│ (shares ring,  │ (forks ring,     │
            │              │  +.iter-N)     │  shares atomic)  │
            └──────┬───────┴───────┬────────┴────────┬─────────┘
                   │               │                 │ errgroup.WithContext
                   │               │  ConsumeStep()  │   ├─ goroutine→resultsChan→ack
                   │               │  Before/After   │   ├─ goroutine→resultsChan→ack
                   ▼               ▼  (dedup veto)    │   └─ goroutine→resultsChan→ack
            yield Event ◄──────────┴──── escalate ────┤  fan-in: yield serially from
            (each carries RequestID for correlation)  │  iterator frame (D-22 footgun 3)
                   │                                   │  escalate→captured cancel()→
                   ▼                                   │  siblings drain (nil,nil) D-05
            stdout: 1 Event = 1 JSON line             │
            (canonicaljson.Marshal, D-08)             ▼
                                              *atomic.Int32 (shared by pointer
                                               across entire tree, D-10)
                                               total consumed ≤ 25, NOT 25³
```

Trace through SC#2: `dry-run` builds `LoopAgent[InfiniteToolCallAgent]`, Budget steps=25 → each loop iteration calls `ConsumeStep()` (decrement-then-check-then-restore, D-11) → on 26th attempt counter hits <0 → restore + return `(false,"max_steps")` → LoopAgent emits explicit budget-exhausted Event → 26 JSON lines total (25 step + 1 terminal).

### Recommended Project Structure
```
internal/agent/
├── agent.go              # Agent interface + InvocationContext + WithSubAgent/WithContext (~90 LOC)
├── event.go              # Event + Actions + LLMResponse + SetAuthorIfEmpty + JSON marshal (~110 LOC)
├── budget.go             # Budget struct + NewBudgetFromEnv + ConsumeStep + Child + Remaining + wallclock (~180 LOC) ⚠ SPLIT RISK
├── budget_dedup.go       # dedupRing + BeforeToolCall/AfterToolResult two-phase fingerprint + exempt allowlist (~120 LOC)  ← split from budget.go (D-18/19/20)
├── errors.go             # ErrBudgetExhausted sentinel (D-04) (~15 LOC)
├── agenttest/
│   └── mocks.go          # InfiniteToolCallAgent, EmitNThenEscalate, RecordingAgent (~140 LOC) (D-07)
├── workflow/
│   ├── sequential.go     # SequentialAgent (~40 LOC)
│   ├── loop.go           # LoopAgent + budget/dedup termination + .iter-N branch (~90 LOC)
│   ├── parallel.go       # ParallelAgent errgroup+ackChan+escalate-cancel (~120 LOC) ⚠ CRITICAL PATH
│   ├── workflow_test.go  # TestMain goleak + all workflow acceptance tests (~280 LOC) ⚠ SPLIT RISK
│   ├── parallel_test.go  # split: SC#3 depth, escalate-cancel, backpressure, goleak break-early
│   └── loop_test.go      # split: SC#2 budget-exhausted, dedup termination, escalate
├── budget_test.go        # atomic invariants + TOCTOU race + canonical hash + property-based (~250 LOC)
├── event_test.go         # JSON round-trip + nullable + UUIDv7/SpanID correlation (~150 LOC)
internal/canonicaljson/
├── canonicaljson.go      # Marshal(any)([]byte,error): sorted keys + UseNumber + strict-reject (~90 LOC)
└── canonicaljson_test.go # fuzz canon(x)==canon(decode(encode(x))); 1≠1.0 (~120 LOC)
cmd/aura/
├── agent.go              # aura agent dry-run subcommand (~90 LOC)
├── agent_test.go         # flag parsing + request-id auto/verbatim (~80 LOC)
└── main.go               # diff: drop chat/chatOnce/stubClient, add case "agent" (~ -30/+8)
scripts/
└── loop_budget_smoke.sh  # SC#2 fixture: count==26, grep limit_hit:max_steps (~30 LOC)
```

### Pattern 1: Open Agent interface (D-01 — diverges from adk seal)
**What:** No unexported `internal() *agent` sealing method. Five methods exactly.
**When to use:** Always — Phase 3 LlmAgent and Phase 9 swarm implement this directly.
**Example:**
```go
// Source: adk-go agent/agent.go L43-52, with the internal() seal REMOVED (D-01)
type Agent interface {
	Name() string
	Description() string
	Run(InvocationContext) iter.Seq2[*Event, error]
	SubAgents() []Agent
	FindAgent(name string) Agent
}
```
Note adk's `FindAgent` recurses via `FindSubAgent`; Aura collapses to a single `FindAgent` that checks self then recurses into subs (the adk `FindSubAgent` split exists only because of its base-struct embed — Aura's open interface implements `FindAgent` per-type or via a shared helper in `agent.go`).

### Pattern 2: Constructor returns interface, struct is exported (D-02)
**What:** `func NewLoop(name string, maxIter uint, subs ...Agent) Agent` returns the interface; `type LoopAgent struct{...}` is exported for compile-asserts.
**When to use:** All three workflow constructors. Mirrors `context.WithCancel`/`net.Pipe` stdlib factory precedent.
**Example:**
```go
// compile-time assertion in test file (SPEC Acceptance L129):
var _ agent.Agent = (*workflow.LoopAgent)(nil)

func NewLoop(name string, maxIter uint, subs ...Agent) Agent {
	return &LoopAgent{name: name, maxIterations: maxIter, subs: subs}
}
```
**Typed-nil guard (D-02):** never `return (*LoopAgent)(nil)` on an error path — a non-nil interface wrapping a nil pointer is a footgun. Return an explicit `nil` interface.

### Pattern 3: iter.Seq2 yield discipline (D-22 — the 4 footguns)
**What:** Range-over-func has four compiler-unguarded footguns confirmed against the Go iter contract.
**Example (correct LoopAgent body, adapting adk loopagent/agent.go L78-104):**
```go
// Source: google/adk-go agent/workflowagents/loopagent/agent.go (Apache 2.0), Aura adds budget.
func (a *LoopAgent) Run(ic InvocationContext) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		for iter := uint(0); ; iter++ {
			ok, reason := ic.Budget.ConsumeStep()      // D-11 decrement-then-check
			if !ok {
				ev := budgetExhaustedEvent(a.name, reason, ic)  // D-04 Event-only
				yield(ev, nil)                          // terminal — return regardless
				return
			}
			for _, sub := range a.subs {
				subIC := ic.WithSubAgent(sub)           // D-09 shares dedup ring + .iter-N branch (D-15)
				for ev, err := range sub.Run(subIC) {
					if !yield(ev, err) {                 // FOOTGUN 2: guard every yield
						return                            // no bare yield after false
					}
					if ev != nil && ev.Actions.Escalate {
						return
					}
				}
			}
			if a.maxIterations > 0 && iter+1 >= a.maxIterations {
				return
			}
		}
	}
}
```
**The 4 footguns** `[CITED: dev.to/gabrielanhaia range-over-func-4-footguns + pkg.go.dev/iter]`:
1. **Storing the yield closure** → panic "continued iteration after whole loop exit". Never cache `yield`.
2. **Bare `yield` after it returned `false`** → panic "continued iteration after function for loop body returned false". Every yield is `if !yield(...) { return }`.
3. **Calling `yield` from a spawned goroutine** → panic under concurrency. ParallelAgent MUST fan-in to a channel and yield **serially from the iterator's own frame** (D-22).
4. **Silent error drop** — `for v := range seq` (one var) compiles even on `Seq2[T,error]`. Mitigation: always `for ev, err := range`; the dry-run consumer and all workflow agents use two-var form.

### Pattern 4: ParallelAgent errgroup + ack backpressure + escalate-cancel (D-03/D-05/D-23) — CRITICAL
**What:** The single highest-leverage and highest-risk pattern. Stolen near-verbatim from adk `parallelagent/agent.go` L67-164, with two deliberate divergences.
**Example (Aura-adapted skeleton):**
```go
// Source: google/adk-go agent/workflowagents/parallelagent/agent.go (Apache 2.0).
// Aura divergences: (D-03) capture cancel for escalate; (D-05) drain (nil,nil) not ctx.Err().
func (a *ParallelAgent) Run(ic InvocationContext) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		eg, egCtx := errgroup.WithContext(ic.Ctx)   // egCtx cancels on first non-nil error
		egCtx, cancel := context.WithCancel(egCtx)   // D-03: our own cancel for escalate (NOT a fake error)
		defer cancel()                               // D-23: always release
		results := make(chan result)
		done := make(chan bool)

		for _, sub := range a.subs {
			sub := sub
			childIC := ic.Child(egCtx, sub)          // D-09: Budget.Child() forks dedup ring, shares *atomic.Int32
			eg.Go(func() error {
				if egCtx.Err() != nil {              // D-23: Go #61611 spawn-loop guard
					return nil
				}
				return runSub(egCtx, childIC, sub, results, done) // 2-arm selects inside
			})
		}
		go func() { _ = eg.Wait(); close(results) }()

		defer close(done)
		for res := range results {
			cont := yield(res.event, res.err)        // serial yield from iterator frame (footgun 3 avoided)
			if res.ack != nil { close(res.ack) }      // backpressure ack
			if res.event != nil && res.event.Actions.Escalate {
				cancel()                              // D-03: escalate → cancel siblings
			}
			if !cont { return }
		}
	}
}
```
**runSub** (per-child) — every channel op is a 2-arm select; on cancellation **return nil, NOT ctx.Err()** (D-05):
```go
func runSub(ctx context.Context, ic InvocationContext, sub Agent, results chan<- result, done <-chan bool) error {
	for ev, err := range sub.Run(ic) {
		if err != nil { return err }                 // real failures go to error slot
		ack := make(chan struct{})
		select {
		case <-done:        return nil               // D-05: clean drain
		case <-ctx.Done():  return nil               // D-05: NOT ctx.Err() — intentional cancel ≠ noise
		case results <- result{event: ev, ack: ack}:
			select {
			case <-ack:
			case <-done:       return nil
			case <-ctx.Done(): return nil
			}
		}
	}
	return nil
}
```
**Divergences from adk made explicit (planner must apply as edits, not blind copy):**
- adk yields `ctx.Err()` at `parallelagent/agent.go:142` → Aura returns `nil` (D-05).
- adk has no escalate-cancel (its escalate semantics differ) → Aura adds the captured `cancel()` on escalate Event (D-03).

### Pattern 5: Budget tree — two derivations + decrement-then-check-then-restore (D-09/D-10/D-11)
**What:** Single `*atomic.Int32` shared by pointer across the whole tree. Two distinct context derivations, NOT one boolean flag.
**Example:**
```go
// D-11: decrement-then-check-then-restore (prevents TOCTOU over-spend)
func (b *Budget) ConsumeStep() (ok bool, reason string) {
	if time.Now().After(b.deadlineWallclock) {       // D-13 wallclock check first
		return false, "wallclock"
	}
	if n := b.steps.Add(-1); n < 0 {
		b.steps.Add(1)                               // restore — the n concurrent goroutines that overshot all restore
		return false, "max_steps"
	}
	return true, ""
}

// D-09: Sequential/Loop share the SAME dedup ring (cross-iteration repeat detection, SC#5)
func (ic InvocationContext) WithSubAgent(sub Agent) InvocationContext {
	c := ic                                          // copy (D-24: never mutate)
	c.Agent = sub
	// Budget unchanged — same *atomic.Int32, same dedupRing
	return c
}

// D-09: Parallel forks dedup ring, shares atomic counter; D-12 applies per-branch soft cap
func (b *Budget) Child(fanout int) *Budget {
	soft := softCap(b.steps.Load(), fanout)          // D-12: ceil(remaining/fanout), tunable AURA_LOOP_BRANCH_SOFT_FRACTION
	return &Budget{
		steps:             b.steps,                  // SHARED pointer (D-10) — total bound preserved
		deadlineWallclock: b.deadlineWallclock,      // shared
		dedupWindow:       b.dedupWindow,
		dedupRing:         newDedupRing(b.dedupWindow), // DISTINCT ring (no cross-branch false positives)
		branchSoftCap:     soft,
	}
}
```
**Concurrency hazard the planner MUST guard (RESEARCH FINDING):** the D-12 per-branch soft cap introduces a *second* counter (`branchSoftCap`) alongside the shared `*atomic.Int32`. If `branchSoftCap` is itself an `atomic.Int32` decremented per branch, the "borrow from shared pool after siblings complete" semantics (D-12) require coordination logic that is NOT in adk and NOT trivially race-free. **Recommendation:** keep the hard total bound (`b.steps`) as the authoritative gate that always runs; treat `branchSoftCap` as a *soft* advisory that, when exhausted, makes `ConsumeStep` return `(false, "branch_soft_cap")` for that branch only — but the branch can retry against the shared pool. The race-test must cover both the hard counter (D-11 TOCTOU) AND the soft-cap interaction. Flag for the planner: this is the one place where the "~30 LOC" estimate (D-12) is optimistic; budget closer to 50 LOC and a dedicated soft-cap test.

### Pattern 6: Canonical JSON internal serializer (D-08/A3 — NOT RFC-8785)
**What:** Deterministic byte output for hashing. Drop RFC-8785 framing entirely.
**Example:**
```go
// internal/canonicaljson/canonicaljson.go
// Deterministic serializer for Aura-internal hashing. NOT RFC-8785 (no cross-system
// crypto-signature consumer → float canonicalization minefield avoided, A3).
func Marshal(v any) ([]byte, error) {
	// (a) decode with UseNumber so numbers stay literal text, never round-trip through float64
	// (b) sort map keys by Go byte order (one documented order; no cross-impl consumer)
	// (c) strict-reject un-canonicalizable input (NaN, Inf, func, chan) → error, never silent coerce
}
```
**Documented rule:** `1` and `1.0` are DISTINCT (literal preservation via `UseNumber`). The fuzz property asserts `canon(x) == canon(decode(encode(x)))` (idempotent round-trip) and `canon("1") != canon("1.0")`. This serializer feeds the dedup `sha256(name + canonical_json(args))` (D-18) and is reused in Phase 4 (conv hash) + Phase 11 (skill content_hash).

### Pattern 7: Two-tier dedup fingerprint (D-18/A2 — reversed from initial design)
**What:** Primary fingerprint = `sha256(name + canonical_json(args))` fired *before* re-executing (blocks side-effects). Result-preview used ONLY as a progress veto.
**Logic:** `BeforeToolCall(name, canonicalArgs)` checks the primary fingerprint before the tool can re-execute, so repeated side effects can be blocked. `AfterToolResult(name, canonicalArgs, resultPreview)` records the bounded result preview only as a progress veto. args repeat + result UNCHANGED → dedup fires (loop). args repeat + result CHANGED → suppress dedup (progress). Volatile-result tools (timestamps/page-tokens) therefore fail **safe** (look like progress) not **open** (D-18 corrects the original `(name,args,result)`-in-hash which was fail-open). Window=3 (D-20), detect period-1 (A-A-A) and period-2 (A-B-A-B), ring ≥ max(window,4). `AURA_LOOP_DEDUP_EXEMPT_TOOLS` (D-19) allowlists poll-shaped tools.

### Pattern 8: Trace IDs — UUIDv7 16B vs crypto/rand 8B SpanID (D-16/A4)
**What:** `RequestID`/`TraceID` = `uuid.UUID` (UUIDv7, 16 bytes = OTel/W3C TraceID exactly). `SpanID`/`ParentSpanID` = **8 random bytes from `crypto/rand`** (NOT UUIDv7).
**Example:**
```go
import "github.com/google/uuid"
import "crypto/rand"

reqID, _ := uuid.NewV7()              // 16B, monotonic, OTel TraceID-shaped
var spanID [8]byte
_, _ = rand.Read(spanID[:])           // 8B = OTel RandomIDGenerator span shape
```
**Why 8B SpanID matters** `[CITED: w3.org/TR/trace-context]`: W3C/OTel SpanID is 8 bytes. A 16-byte UUIDv7 in the SpanID slot would force lossy truncation when a future OTel slice maps these → silently breaks historical trace correlation. 8 random bytes = exactly OTel's `RandomIDGenerator`, making the "OTel-compatible no-dep" claim genuinely TRUE.

### Anti-Patterns to Avoid
- **Yielding from a goroutine** (footgun 3) — ParallelAgent fan-in is mandatory; never `eg.Go(func(){ yield(...) })`.
- **`ctx.Err()` on intentional cancel** (D-05) — pollutes operator output; drain `(nil,nil)`.
- **Fake sentinel error for escalate** (D-03) — pollutes `errgroup.Wait()`; use captured `cancel()`.
- **Budget exhaustion via error slot** (D-04) — termination is Event-only; the `error` slot carries only real LLM/tool failures.
- **InvocationContext stored on a long-lived struct** (D-24) — single-Run-scoped; `WithContext`/`WithSubAgent` always return a copy.
- **`return (*LoopAgent)(nil)`** (D-02) — typed-nil interface footgun; return explicit nil interface.
- **check-then-decrement on the atomic** (D-11) — TOCTOU over-spend; always decrement-then-check-then-restore.
- **RFC-8785 float canonicalization** (A3) — determinism minefield with no Aura consumer.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| UUIDv7 generation | bit-layout + monotonic counter from scratch | `github.com/google/uuid` v1.6.0 `NewV7` | RFC-9562 bit layout + monotonic same-ms sequencing is easy to get subtly wrong |
| Concurrent fan-out + cancel | raw goroutines + sync.WaitGroup + manual ctx | `errgroup.WithContext` | first-error cancellation + Wait() semantics already correct |
| Goroutine-leak detection | manual stack diffing | `goleak.VerifyTestMain` | the canonical Go tool; already in Phase-1 pattern |
| Property generation/shrinking | random input loops | `pgregory.net/rapid` | automatic shrinking to minimal failing case |
| SpanID randomness | math/rand | `crypto/rand` 8 bytes | OTel correctness + non-predictability |

**Key insight:** The *only* genuinely hand-rolled primitives in Phase 2 are `canonicaljson` (deliberately, A3 — no library matches the rescoped requirements) and the dedup ring (domain-specific). Everything else is stdlib or a battle-tested single-purpose lib.

## Common Pitfalls

### Pitfall 1: ParallelAgent goroutine leak on consumer early-break
**What goes wrong:** Consumer (e.g. dry-run, or a parent workflow) `break`s after the first Event while 3 slow children are still producing → children block forever on `results <- ...` → leak.
**Why it happens:** without the `done` channel + 2-arm selects, the producer has no exit path when the consumer stops draining.
**How to avoid:** `defer close(done)` in the iterator frame (D-23); every channel op in `runSub` is a 2-arm select including `case <-done: return nil`.
**Warning signs:** `goleak.VerifyNone` failure listing goroutines parked on chan send; a "passing" test that hangs 5s then times out.

### Pitfall 2: Depth³ budget explosion (the trap Aura avoids)
**What goes wrong:** Each child gets a *fresh* budget (nanobot `subagent.py:140-183` does this) → depth-3 spawn of fan-3 = 25³ = 15,625 steps.
**Why it happens:** copying the Budget struct by value copies the counter instead of sharing a pointer.
**How to avoid:** `Budget.Child()` shares the **same `*atomic.Int32` by pointer** (D-10). SC#3 test asserts total tree consumption ≤ 25.
**Warning signs:** SC#3 test sees >25 total `ConsumeStep` successes; any per-child `NewBudgetFromEnv()` call inside spawn.

### Pitfall 3: Sub-agent-exposed-as-tool reintroduces depth³ (scout-1 rec #4)
**What goes wrong:** If a future phase exposes a sub-agent as a *tool* (Phase 9 swarm), and that wrapper creates a fresh budget, the shared-counter guarantee is silently broken.
**How to avoid:** dedicated test now (`TestParallelAgent_SubAgentExposedAsTool_SharesCounter`) asserting a tool-wrapped sub-agent threads the shared `*atomic.Int32`. Document the invariant on `Budget.Child`.
**Warning signs:** any `agenttest` mock that wraps a sub-agent and calls `NewBudgetFromEnv`.

### Pitfall 4: TOCTOU over-spend under concurrent ConsumeStep
**What goes wrong:** N goroutines all read counter>0, all decrement → logical over-spend beyond cap.
**How to avoid:** decrement-then-check-then-restore (D-11). Race-test: N goroutines vs counter of 1 → exactly one `ok=true`.
**Warning signs:** `go test -race` clean but the count assertion (`exactly 1 ok`) fails intermittently.

### Pitfall 5: Branch label parsed as hierarchy
**What goes wrong:** code reconstructs the agent tree by splitting `Branch` on `.` → breaks when an agent name contains a dot.
**How to avoid:** Branch is a LABEL only (D-15); hierarchy comes from `SpanID`/`ParentSpanID`. Escape or reserve `.` in agent names.

### Pitfall 6: Fail-open dedup on volatile results
**What goes wrong:** putting result in the fingerprint (`sha256(name,args,result)`) means a tool returning a timestamp never produces a repeating triple → loop never detected.
**How to avoid:** two-tier (D-18) — fingerprint on name+args only; result as progress veto. Fixed in A2.

## Runtime State Inventory

> Phase 2 deletes `internal/agent/loop.go` and `stubClient`. This is a code-only refactor with NO stored data, services, or OS state. Inventory completed per discipline:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — no DB tables, no datastores touched by Phase 2. Budget counter is in-memory only. | None |
| Live service config | None — no external services configured by this phase. | None |
| OS-registered state | None — no scheduled tasks, no daemons. | None |
| Secrets/env vars | NEW env var *names* introduced (read-only): `AURA_LOOP_MAX_STEPS`, `AURA_LOOP_MAX_WALLCLOCK_SEC`, `AURA_LOOP_DEDUP_WINDOW` (SPEC) + A7 vars (`AURA_LOOP_DEDUP_EXEMPT_TOOLS`, `AURA_LOOP_BRANCH_SOFT_FRACTION`, `AURA_LOOP_NODE_TIMEOUT_SEC`, `AURA_LOOP_DEDUP_RESULT_CAP`). Code-introduce only; no existing secret renamed. | Add to `.env.example` + PRD env catalog (A7) |
| Build artifacts | `internal/agent/loop.go` (132 LOC) DELETED + `cmd/aura/main.go` `chatOnce`/`stubClient` removed. No compiled artifact persists (Go rebuilds). `go.mod`/`go.sum` gain `google/uuid` + `pgregory.net/rapid`. | `go mod tidy` after `go get`; verify `aura chat` removed cleanly |

**Verified by:** grep of `internal/agent/loop.go` shows it is a self-contained `Loop` struct with no DB/service callers; `cmd/aura/main.go` is the only consumer (L66-67). README.md references `internal/agent/loop.go` (STRUCTURE.md L24, L335) — **RESEARCH FINDING:** README.md will dangle after deletion; planner should add a README touch to the task list or accept a stale doc reference (low priority).

## Code Examples

### budget-exhausted Event (SC#2 exact shape)
```go
// final Event JSON line for loop_budget_smoke.sh (SPEC L117):
// {"request_id":"...","span_id":"...","author":"<loop_name>","actions":{
//   "escalate":true,
//   "state_delta":{"termination_reason":"budget_exhausted","limit_hit":"max_steps","steps_consumed":25}}}
func budgetExhaustedEvent(loopName, reason string, ic InvocationContext) *Event {
	return &Event{
		RequestID: ic.RequestID,
		SpanID:    ic.SpanID,
		Author:    loopName,                       // D-14: explicit per workflow agent
		Branch:    ic.Branch,
		Actions: Actions{
			Escalate: true,                         // D-04: Event-only signal
			StateDelta: map[string]any{
				"termination_reason": "budget_exhausted",
				"limit_hit":          reason,        // "max_steps" | "wallclock" | "dedup"
				"steps_consumed":     stepsConsumed, // == initial max for max_steps case
			},
		},
		Timestamp: time.Now().UTC(),
	}
}
```

### Event JSON marshal discipline (D-21 round-trip property)
```go
// Event MarshalJSON must use: json.Marshal default key order is struct-field order (stable),
// but for round-trip byte-identity the test uses a decoder with UseNumber and the encoder
// with SetEscapeHTML(false) + canonical time format (RFC3339Nano UTC).
// Property (D-21): decode(encode(ev)) == ev for Aura-internal symmetric encode only.
```

### CLI flag sentinel precedence (D-06)
```go
// flags default to -1 ("unset") → fall through to NewBudgetFromEnv; non-(-1) overrides.
maxSteps := flag.Int("max-steps", -1, "override AURA_LOOP_MAX_STEPS")
b, err := agent.NewBudgetFromEnv()   // fail-fast on malformed env (D-06, Phase-1 boot discipline)
if err != nil { return err }
if *maxSteps != -1 { b.SetMaxSteps(int32(*maxSteps)) }   // CLI > env > default(25)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| adk sealed interface (`internal()`) | open interface | adk itself "in future releases will allow implementing this interface" | Aura D-01 leads upstream direction |
| `(name,args,result)` dedup hash | two-tier name+args + result-veto | scout-2 2026-05-29 (A2) | fixes fail-open on volatile results |
| RFC-8785 canonical JSON | internal deterministic serializer | scout-2 2026-05-29 (A3) | avoids float-canon minefield, no Aura consumer needs cross-system signatures |
| 16-byte UUIDv7 SpanID | 8-byte crypto/rand SpanID | scout-5 2026-05-29 (A4) | makes OTel-compat claim genuinely true |
| fresh-budget-per-child (nanobot) | shared `*atomic.Int32` (Strands/OpenAI parity) | locked D-10 | avoids depth³ explosion |

**Deprecated/outdated:**
- `internal/agent/loop.go` `Loop` struct + `defaultMaxSteps=8` — DELETED in Phase 2; replaced by interface + Budget.
- `cmd/aura/main.go` `chatOnce` + `stubClient` + `case "chat"` — REMOVED; `aura chat` returns in Phase 3 with real LlmAgent.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `NewV7` monotonicity in google/uuid v1.6.0 is sufficient for SC#4 "all lines share same request_id" — note SC#4 only requires *one* request_id reused tree-wide (generated once at root), NOT per-Event monotonic generation. Monotonicity matters only if multiple V7s are minted per run. | Trace IDs | Low — request_id is minted once at dry-run boot and propagated; monotonicity is a bonus, not load-bearing for SC#4 |
| A2 | D-12 soft-cap "~30 LOC" estimate is optimistic; realistic ~50 LOC + dedicated test due to borrow-after-siblings coordination | Pattern 5 | Medium — affects LOC budget + test count; flagged to planner |
| A3 | `budget.go` will exceed 600 LOC if dedup ring + soft cap + wallclock all land in one file; recommend pre-emptive split into `budget.go` + `budget_dedup.go` | File budgeting | Low — split is cheap and CLAUDE.md mandates it on touch anyway |
| A4 | README.md reference to `internal/agent/loop.go` will dangle after deletion | Runtime State Inventory | Low — stale doc, not a build break |

**Note:** No `[ASSUMED]`-tagged *package* claims — both new deps are registry-verified and canonical. All assumptions above are implementation-sizing judgments, not factual claims about external systems.

## Open Questions (RESOLVED)

1. **Soft-cap borrow semantics (D-12) exact algorithm**
   - What we know: hard total bound (`b.steps`) is authoritative and always enforced; soft cap throttles a greedy branch to its fair share.
   - What's unclear: the precise "borrow from shared pool only after siblings complete" coordination — is it a passive soft cap (branch hits its cap → `ConsumeStep` returns false for that branch, retries against shared pool) or active rebalancing? D-12 marks the exact fraction-vs-`ceil(remaining/fanout)` as Claude's Discretion.
   - Recommendation: implement the *passive* soft cap (simpler, race-safe, preserves hard bound); document that active rebalancing (DOVA-style) is deferred (CONTEXT Deferred Ideas). Planner picks the default fraction.

2. **`agenttest` mock placement vs import cycle**
   - What we know: mocks live in `internal/agent/agenttest/` (D-07), reused by Phase 3/9.
   - What's unclear: `agenttest` imports `internal/agent` (for the `Agent` interface + `Event`); `internal/agent` tests import `agenttest` → no cycle (test packages can import sibling helper packages). But `workflow` tests importing `agenttest` which imports `agent` is also fine.
   - Recommendation: confirm `agenttest` imports `agent` (one direction only); never have `agent` import `agenttest` outside `_test.go`. Standard Go test-helper pattern, no cycle.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all (iter.Seq2, build, test -race) | ✓ | go1.26.3 windows/amd64 | — |
| `golang.org/x/sync` | ParallelAgent errgroup | ✓ | v0.18.0 (indirect→direct) | — |
| `go.uber.org/goleak` | SC#1 leak tests | ✓ | v1.3.0 | — |
| `github.com/google/uuid` | SC#4 UUIDv7 | ✗ (NEW) | v1.6.0 to install | hand-roll (NOT recommended, D-16) |
| `pgregory.net/rapid` | D-21 property tests | ✗ (NEW) | v1.3.0 to install | gopter (TESTING.md lists as acceptable alt) |
| `bash` | loop_budget_smoke.sh (SC#2) | ✓ (Git Bash / WSL on Windows) | — | — |
| `golangci-lint` | Gate-3 acceptance L123 | unknown — not verified this session | — | RESEARCH FINDING: planner should add an install/verify step; CONVENTIONS.md L19 says "no golangci-lint config yet" but CLAUDE.md + SPEC L123 require it |

**Missing dependencies with no fallback:** none (both new deps install cleanly via `go get`).
**Missing dependencies with fallback:** `pgregory.net/rapid` → gopter.
**RESEARCH FINDING:** `golangci-lint` is required by SPEC Acceptance L123 and CLAUDE.md (MEMORY: "golangci-lint catches what audit agents miss") but CONVENTIONS.md L19 notes no config exists yet. The planner should either (a) add a `.golangci.yml` + install task, or (b) treat the lint gate as `~/go/bin/golangci-lint run` with default rules. Confirm with user if a config is expected.

## Validation Architecture

> nyquist_validation is enabled (no `.planning/config.json` override found / treated as enabled). This section is the Nyquist Dimension-8 test map.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go.uber.org/goleak` v1.3.0 + `pgregory.net/rapid` v1.3.0 |
| Config file | none (Go convention; no jest/pytest equiv) |
| Quick run command | `go test ./internal/agent/... ./internal/canonicaljson/ ./cmd/aura/` |
| Race run command | `go test -race -count=1 ./internal/agent/...` (SC#1) |
| Coverage command | `go test -coverprofile=cover.out ./internal/agent/... && go tool cover -func=cover.out` |
| Smoke command | `bash scripts/loop_budget_smoke.sh` (SC#2) |
| CLI smoke | `go run ./cmd/aura agent dry-run --request-id auto` (SC#4) |

### Phase Requirements → Test Map

INFRA-03 decomposes into the 4 Success Criteria + 9 supporting acceptance tests. Every row specifies file, test name, the assertion, and the observable signal.

| Req / SC | Behavior | Test Type | File · Test Name | Assertion / Observable Signal | File Exists? |
|----------|----------|-----------|------------------|-------------------------------|-------------|
| **SC#1** | zero goroutine leaks across all workflow tests | unit (leak) | `workflow_test.go` · `TestMain` | `goleak.VerifyTestMain(m)` → `go test -race -count=1 ./internal/agent/...` exit 0 | ❌ Wave 0 |
| **SC#1 / D-23** | consumer breaks early, 3 slow children, no leak | unit (leak) | `parallel_test.go` · `TestParallelAgent_NoGoroutineLeak_When_ConsumerBreaksEarly` | `defer goleak.VerifyNone(t)`; consumer `break`s after 1 event; all child goroutines drain via `done`/`ctx.Done()` | ❌ Wave 0 |
| **SC#2** | loop terminates at exactly max_steps with explicit Event | smoke + unit | `scripts/loop_budget_smoke.sh` + `loop_test.go` · `TestLoopAgent_TerminatesAtMaxSteps_WithExplicitEvent` | smoke: stdout line count `== 26`, `grep '"limit_hit":"max_steps"'` present; unit: final Event `Author==<loop_name>` AND `StateDelta{termination_reason:"budget_exhausted",limit_hit:"max_steps",steps_consumed:25}` | ❌ Wave 0 |
| **SC#3** | depth-3 fan-3 tree (9 leaf) shares one counter | unit | `parallel_test.go` · `TestParallelAgent_DepthChainBudgetShared_NotFresh` | single `*atomic.Int32` from 25; total `ConsumeStep` successes across tree `≤ 25` (NOT 25³); count via a counting mock | ❌ Wave 0 |
| **SC#3 / scout-1 #4** | sub-agent exposed as tool threads shared counter | unit | `parallel_test.go` · `TestParallelAgent_SubAgentExposedAsTool_SharesCounter` | tool-wrapped sub-agent uses the same `*atomic.Int32`; total ≤ 25 | ❌ Wave 0 |
| **SC#4** | UUIDv7 request_id on every Event, all equal | smoke (CLI) + unit | `agent_test.go` · `TestDryRun_RequestIDAuto_IsValidUUIDv7_AndStable` | every stdout line `request_id` matches `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`; all lines identical; `--request-id <fixed>` reproduces verbatim | ❌ Wave 0 |
| **SC#4 / D-16** | 16-byte TraceID vs 8-byte SpanID | unit | `event_test.go` · `TestEvent_TraceID16Bytes_SpanID8Bytes` | `len(RequestID)==16`; SpanID serializes to 16 hex chars (8 bytes); ParentSpanID nil at root | ❌ Wave 0 |
| Req#1 | interface + InvocationContext compile | unit | `agent_test.go` · compile-asserts | `var _ agent.Agent = (*workflow.{Sequential,Loop,Parallel}Agent)(nil)`; `go build ./internal/agent/...` clean | ❌ Wave 0 |
| Req#2 | Event full-shape JSON round-trip | unit | `event_test.go` · `TestEvent_FullShapeMarshalsToJSON_RoundTrips` | `Event → JSON → Event` byte-identical; `LLMResponse` nullable when nil; nested `map[string]any` in StateDelta | ❌ Wave 0 |
| Req#3 / D-11 | atomic decrement no over-spend | unit (race) | `budget_test.go` · `TestBudget_ConsumeStep_AtomicDecrement_NoRace` | 10 goroutines × 100 = exactly 1000 successful decrements under `-race` | ❌ Wave 0 |
| Req#3 / D-11 | TOCTOU exactly-one-winner | unit (race) | `budget_test.go` · `TestBudget_ConsumeStep_ExactlyOneWinner_When_CounterIsOne` | N goroutines vs counter 1 → exactly one `ok==true` | ❌ Wave 0 |
| Req#3 / D-09 | child shares counter | unit | `budget_test.go` · `TestBudget_Child_SharesStepsCounter` | parent consume 5 → child `Remaining()==20` | ❌ Wave 0 |
| Req#3 / D-09 | child distinct dedup ring | unit | `budget_test.go` · `TestBudget_Child_DistinctDedupRing` | parent dedup state invisible to child | ❌ Wave 0 |
| Req#3 / D-08 | canonical hash order-independent | unit | `budget_test.go` · `TestBudget_BeforeToolCall_CanonicalHashOrderIndependent` | caller canonicalizes `{"a":1,"b":2}` and `{"b":2,"a":1}` to the same fingerprint before dedup pre-check | ❌ Wave 0 |
| Req#3 / D-13 | wallclock terminates | unit (synctest) | `budget_test.go` · `TestBudget_Wallclock_TerminatesAtDeadline` | Go 1.26 `synctest` (or fake clock) → `ConsumeStep` returns `(false,"wallclock")` past deadline | ❌ Wave 0 |
| Req#3 / D-18 | two-tier dedup terminates on 3 repeats | unit | `loop_test.go` · `TestLoopAgent_DedupWindow_TerminatesOn3SameToolCalls` | same `sha256(name+canon_args)` ×3, result unchanged → `limit_hit=="dedup"` | ❌ Wave 0 |
| Req#3 / D-18 | result-changed suppresses dedup (progress veto) | unit | `loop_test.go` · `TestLoopAgent_DedupVeto_When_ResultChanges` | same args, changing result → NO dedup (loop continues) | ❌ Wave 0 |
| Req#4 | sequential order + escalate | unit | `sequential_test.go` · `TestSequentialAgent_RunsAllSubsInOrder` / `_PropagatesEscalate` | A→B→C order preserved; B escalate → C not invoked | ❌ Wave 0 |
| Req#6 / D-03 | escalate cancels siblings | unit (leak) | `parallel_test.go` · `TestParallelAgent_EscalateFromAnyCancelsSiblings` | child[1] escalate → child[0]/child[2] receive cancel, drain `(nil,nil)`, `goleak.VerifyNone` | ❌ Wave 0 |
| Req#6 | children share parent budget | unit | `parallel_test.go` · `TestParallelAgent_ChildrenShareParentBudget` | 5 budget, 3 children consume 1 each → remaining 2; 3 more → only 2 succeed | ❌ Wave 0 |
| Req#6 | backpressure ack channel | unit | `parallel_test.go` · `TestParallelAgent_BackpressureAckChannel` | slow consumer → producer waits on ack, no unbounded buffer | ❌ Wave 0 |
| D-21 (PBT) | total consumed ≤ max | property | `budget_test.go` · `TestBudget_Property_TotalConsumedNeverExceedsMax` | `rapid` generates random concurrent consume sequences → invariant holds | ❌ Wave 0 |
| D-21 (PBT) | escalate always yielded before return | property | `loop_test.go` · `TestLoopAgent_Property_EscalateYieldedBeforeReturn` | `rapid` → terminal escalate Event always precedes iterator return | ❌ Wave 0 |
| D-21 (PBT) | Event JSON round-trip byte-identity | property | `event_test.go` · `TestEvent_Property_JSONRoundTripByteIdentical` | `rapid`-generated Events → `canon(decode(encode))==encode` (UseNumber, SetEscapeHTML(false), RFC3339Nano UTC) | ❌ Wave 0 |
| A3 (canon) | canonical idempotent + 1≠1.0 | fuzz | `canonicaljson_test.go` · `FuzzCanonical_RoundTripAndDistinctNumbers` | `canon(x)==canon(decode(encode(x)))`; `canon("1") != canon("1.0")`; strict-reject NaN/Inf | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/<touched-pkg>/` (Gate-2)
- **Per workflow-pkg touch:** `go test -race -count=1 ./internal/agent/...` (SC#1, mandatory)
- **Per wave merge:** full `go test -race ./...` + `bash scripts/loop_budget_smoke.sh` + `go run ./cmd/aura agent dry-run --request-id auto`
- **Phase gate:** coverage ≥85% (CLAUDE.md floor, overrides SPEC ≥75%) across unit tier + `golangci-lint run` clean + mutation spot-check ≥70% on `budget.go`/`budget_dedup.go`

### Coverage Strategy to Hit 85% Floor
The 85% floor (CLAUDE.md) is across the **full tag matrix**. Phase 2 is unit-only (no integration build tags — no DB/Neo4j/sandbox). So the floor is a pure unit number on `internal/agent`, `internal/agent/workflow`, `internal/canonicaljson`, and `cmd/aura` (agent.go portion).
- **Highest-coverage-leverage files:** `budget.go`/`budget_dedup.go` (every branch of ConsumeStep/BeforeToolCall/AfterToolResult/Child/wallclock/soft-cap is directly unit-testable — aim 95%+).
- **Hardest to cover:** ParallelAgent cancel/drain branches — the `case <-done` and `case <-ctx.Done()` arms need the break-early + escalate-cancel tests to exercise them. Budget a test per arm.
- **cmd/aura/agent.go:** flag parsing + request-id paths covered by `agent_test.go`; the iterate-and-print loop covered by capturing stdout. Keep business logic out of `main()` so it's testable.
- **mutation spot-check (≥70%, D-21-adjacent):** run `go-mutesting ./internal/agent/budget.go` manually; the decrement-then-check-then-restore and the `<0` boundary are the mutation-sensitive lines.

### Wave 0 Gaps (all test infra is net-new — Phase 1 deferred all agent tests)
- [ ] `internal/agent/workflow/workflow_test.go` — `TestMain` + `goleak.VerifyTestMain` (SC#1)
- [ ] `internal/agent/workflow/{sequential,loop,parallel}_test.go` — split per-agent (avoid 600-LOC on workflow_test.go)
- [ ] `internal/agent/budget_test.go` — atomic/TOCTOU/canonical/wallclock/PBT
- [ ] `internal/agent/event_test.go` — JSON round-trip + trace-ID shape + PBT
- [ ] `internal/agent/agenttest/mocks.go` — `InfiniteToolCallAgent`, `EmitNThenEscalate`, `RecordingAgent`, counting mock for SC#3 (D-07)
- [ ] `internal/canonicaljson/canonicaljson_test.go` — fuzz target
- [ ] `cmd/aura/agent_test.go` — flag parsing + request-id
- [ ] `scripts/loop_budget_smoke.sh` — SC#2 fixture (Git Bash compatible on Windows; use `wc -l` and `grep`)
- [ ] dep install: `go get github.com/google/uuid@v1.6.0 pgregory.net/rapid@v1.3.0`

## Security Domain

> `security_enforcement` not explicitly `false` (treated as enabled). Phase 2 is a runtime substrate with NO network, NO untrusted input, NO persistence, NO auth — the attack surface is minimal. ASVS categories mostly N/A.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | no auth in this phase (Phase 1.7 identity) |
| V3 Session Management | no | InvocationContext is in-process single-Run scope, not a user session |
| V4 Access Control | no | no access control surface |
| V5 Input Validation | partial | `NewBudgetFromEnv` fail-fast on malformed env (D-06); canonicaljson strict-rejects un-canonicalizable input (D-08) — these ARE input-validation controls |
| V6 Cryptography | partial | `crypto/rand` for SpanID (not `math/rand`); `sha256` for dedup fingerprint — never hand-roll either |
| V11 Business Logic | yes | the Budget tree IS a resource-exhaustion control (DoS prevention) — depth³ avoidance, hard step cap, wallclock deadline |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Resource exhaustion via runaway agent loop | Denial of Service | hard step cap (D-10/D-11) + wallclock deadline (D-13) + dedup loop-guard (D-18) — the entire raison d'être of the Budget tree |
| Goroutine leak under cancellation | Denial of Service (slow) | goleak gate (SC#1) + 2-arm selects + defer cancel (D-23) |
| Predictable SpanID enabling trace forgery (future, once exposed) | Spoofing | `crypto/rand` 8 bytes, not `math/rand` (D-16) |
| Hash collision silently merging distinct tool calls | Tampering | strict-reject + `UseNumber` (no float coercion) so `1`≠`1.0` (D-08) |
| Malformed env crashing at runtime mid-loop | Denial of Service | fail-fast at boot (`NewBudgetFromEnv` returns error), Phase-1 boot discipline (D-06) |

## Sources

### Primary (HIGH confidence)
- `D:/tmp/adk-go-study/agent/agent.go` — Agent interface (L43-52 seal at L51), getAuthorForEvent (L237-243), base-Run (L162-215). Shallow clone of `main`; cite by structure.
- `D:/tmp/adk-go-study/agent/workflowagents/{sequential,loop,parallel}agent/agent.go` — verbatim Run cores; ParallelAgent L67-164 is the steal target.
- `internal/llm/client.go` (read) — `ToolCall.ID` to surface as `ToolCallID` (D-17).
- `internal/agent/loop.go` (read) — the 132-LOC `Loop` to delete.
- `cmd/aura/main.go` (read) — dispatch L27-46, `chatOnce`/`stubClient` L64-94 to remove.
- `internal/db/db_test.go` (read) — Phase-1 `goleak.VerifyTestMain` + CI fail-loud pattern to replicate.
- `go.mod` / `go list -m -versions` — dep verification (Go 1.26.3, uuid absent, x/sync+goleak present, uuid v1.6.0/rapid v1.3.0 latest).
- `.planning/codebase/{STRUCTURE,CONVENTIONS,TESTING}.md` — target layout, naming, test discipline.
- W3C Trace Context — TraceID 16B / SpanID 8B (D-16). `[CITED: w3.org/TR/trace-context]`
- pkg.go.dev/iter + dev.to range-over-func — the 4 footguns (D-22). `[CITED]`
- pkg.go.dev/github.com/google/uuid — NewV7 monotonic + crypto/rand (D-16). `[CITED]`

### Secondary (MEDIUM confidence)
- `D:/tmp/picobot/internal/agent/loop.go:236-281` (read) — minimalist single-maxIterations loop confirming Aura's machinery is swarm-justified.
- `D:/tmp/nanobot/.../subagent.py`, `runner.py` (referenced in CONTEXT) — fresh-per-child depth³ trap + stop_reason enum.
- Strands / OpenAI Agents SDK / LangGraph #1260 (scout-cited in CONTEXT) — shared-counter parity for D-10.

### Tertiary (LOW confidence)
- None requiring validation — all load-bearing claims verified against tool output or official docs.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all deps verified against `go list`/`go.mod`; versions confirmed current.
- Architecture: HIGH — patterns pre-locked (D-01–D-24) + adk source read directly + footguns cross-verified.
- Pitfalls: HIGH — each maps to a locked decision + a concrete test; ParallelAgent risks confirmed against adk source.
- Validation Architecture: HIGH — every SC + acceptance criterion mapped to a named test with assertion; coverage strategy concrete.
- Open items: the D-12 soft-cap algorithm exactness and golangci-lint config presence are the only MEDIUM-confidence gaps, both flagged.

**Research date:** 2026-05-29
**Valid until:** 2026-06-28 (30 days — stable stack; only risk is a uuid/rapid minor bump, non-breaking)

## RESEARCH COMPLETE

**Phase:** 2 - Agent Cornerstone
**Confidence:** HIGH

### Key Findings
- ParallelAgent (`internal/agent/workflow/parallel.go`) is the critical path: four dangerous Go patterns combined; steal adk's channel structure verbatim then apply exactly two documented divergences (D-05 drain `(nil,nil)`, D-03 captured-cancel-for-escalate).
- Two NEW deps only: `github.com/google/uuid@v1.6.0` (verified latest, NewV7 monotonic + crypto/rand) and `pgregory.net/rapid@v1.3.0` (test-only). `golang.org/x/sync`(errgroup) + `goleak` already present (A6 confirmed). Go 1.26.3 verified.
- LOC budget RESEARCH FINDING: `budget.go` will exceed 600 LOC with dedup ring + soft cap + wallclock — split into `budget.go` + `budget_dedup.go` pre-emptively; D-12 soft-cap "~30 LOC" is optimistic (~50 + test). `workflow_test.go` should split into `{sequential,loop,parallel}_test.go`.
- Validation Architecture is fully mapped: 4 SC + 9 supporting + 3 property-based + 1 fuzz = 27 named tests, each with file/assertion/signal. Phase 2 is unit-only (no integration tags), so the 85% floor is a pure unit number — `budget*.go` is the highest-coverage-leverage surface.
- Two flagged gaps for planner/user: (1) D-12 soft-cap exact algorithm — recommend passive soft cap, defer DOVA rebalancing; (2) `golangci-lint` config absent (CONVENTIONS.md L19) but required by SPEC L123 — needs install/config decision. Minor: README.md will dangle on `loop.go` deletion.

### File Created
`.planning/phases/02-agent-cornerstone/02-RESEARCH.md`

### Confidence Assessment
| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | deps verified via go list / go.mod |
| Architecture | HIGH | decisions pre-locked + adk source read directly |
| Pitfalls | HIGH | each maps to locked decision + concrete test |
| Validation Architecture | HIGH | every SC mapped to named test + assertion |

### Open Questions (flagged, non-blocking)
1. D-12 soft-cap exact borrow algorithm (recommend passive soft cap).
2. golangci-lint config presence (SPEC requires; CONVENTIONS says none yet).
3. `agenttest` import direction (standard test-helper pattern, no cycle — confirmed safe).

### Ready for Planning
Research complete. No locked decisions re-opened. Planner can now write executable PLAN.md files with the Validation Architecture as the test backbone.
