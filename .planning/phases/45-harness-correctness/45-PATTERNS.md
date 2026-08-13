# Phase 45: Harness correctness - Pattern Map

**Mapped:** 2026-08-13
**Files analyzed:** 8 modified + 1 new source file + 2 amendment docs + ~7 test files
**Analogs found:** 8 / 8 source files have a direct in-repo analog (this phase touches only
existing packages; several "analogs" ARE the file itself, since most of it is additive to code
already read cover-to-cover by 45-RESEARCH.md's Findings 1-6). No external-library analog needed
anywhere in this phase.

This phase is unusual for a pattern map: 45-RESEARCH.md already did full-file reads of every
touched file and cites exact line numbers. This document does not re-derive those facts — it
re-packages them as "what to copy from where," adds the LOC/god-class accounting CLAUDE.md
requires, and answers the five deep-dive analogs the planner explicitly needs.

---

## Deep-Dive Analogs (mandatory reading — the five the planner asked for)

### 1. The `resultExpiredMarker` seam, in full — `replayedMarker` must mirror it exactly

**File:** `internal/gateway/reserve.go` (305 LOC)

**The marker constant** (lines 24-28):
```go
// resultExpiredMarker is appended to a replayed preview when the recorded end's sidecar
// has been GC'd (F-040). Replay tolerates the missing sidecar — it returns the capped +
// redacted preview plus this marker, never an error, and never extends sidecar retention
// to chase the verbatim bytes (Pitfall 6).
const resultExpiredMarker = "\n\n[result expired: full output no longer retained]"
```

**Where it is injected — the ONLY production site today** (lines 285-305, `replayResult`, Layer A
only):
```go
func replayResult(end *toolinvocations.Event) tools.ToolResult {
	if end == nil {
		return tools.ToolResult{Preview: "[reservation held: the tool did NOT run and no result was recorded]"}
	}
	preview := end.ResultPreview
	fullPath := end.ResultSidecarPath
	if fullPath != "" {
		if _, statErr := os.Stat(fullPath); statErr != nil {
			preview += resultExpiredMarker
			fullPath = ""
		}
	}
	return tools.ToolResult{
		Preview: preview, FullPath: fullPath, Bytes: end.ResultBytes, Truncated: end.ResultTruncated,
	}
}
```
This return value is handed straight back by `execTool` (`internal/agent/llm_agent_retry.go:141-143`,
`if verdict.Replay != nil { return *verdict.Replay, nil }`) as the literal `RoleTool` message
content — no intermediate transform. `replayResult` is the ONLY caller-of-record; it is invoked
from `reserve()` (`reserve.go:248`) on the rows==0 branch.

**The Layer B twin that has NO marker today** — `decodeOperationReplay` (lines 95-113):
```go
func decodeOperationReplay(replay *idempotency.ReplayResult) (tools.ToolResult, error) {
	if replay == nil {
		return tools.ToolResult{}, errors.New("missing replay")
	}
	if len(replay.Body) == 0 && replay.Preview == "" && replay.SidecarRef == "" {
		return tools.ToolResult{}, errors.New("expired replay")
	}
	var result tools.ToolResult
	if len(replay.Body) != 0 {
		if err := json.Unmarshal(replay.Body, &result); err != nil {
			return tools.ToolResult{}, errors.New("invalid replay")
		}
	} else {
		result.Preview = replay.Preview
		result.FullPath = replay.SidecarRef
		result.Bytes = len(result.Preview)
	}
	return result, nil
}
```
D-10 needs `replayedMarker` appended here too. **This is genuinely new code, not a mirror** — the
"mirrors resultExpiredMarker" framing in CONTEXT.md is correct only for the marker STRING itself,
not for a pre-existing append site in this function.

**REUSABLE CODE flag:** two call sites (`replayResult` for Layer A, `decodeOperationReplay` for
Layer B) need the identical "append `replayedMarker` to `.Preview`" step. Do not duplicate the
append logic — extract one small helper, e.g. `markReplayed(result tools.ToolResult) tools.ToolResult`
that both call, so the marker string and the append rule live in exactly one place (the same
`reserve.go` file both already live in).

**OTel span attribute — the gap 45-RESEARCH.md's Open Question 1 flags, resolved by grep:**
Neither `replayResult` nor `decodeOperationReplay` takes a `context.Context`/span today — no
`span.SetAttributes` call exists anywhere in `reserve.go`. The span that IS in scope at the point
the model-visible result is finalized is the `tool.execute` span, started/ended in
`internal/agent/llm_agent_tool.go:171-204` (`runTool`):
```go
// internal/agent/llm_agent_tool.go:171,187,202-204
spanCtx, span := startToolSpan(ctx, call.Function.Name, run.Mutating)
toolCtx := tools.WithToolCallContext(spanCtx, a.sessionID, call.ID, a.runDir, a.previewCap)
// ...
res, err := a.execTool(toolCtx, tool, run.Mutating, json.RawMessage(call.Function.Arguments))
run.EndedAt = time.Now().UTC()
// ...
run.Result = res
run.Preview = renderToolResultForPrompt(call.Function.Name, res)
endToolSpan(span, "")
return run
```
`span` is exactly the `tool.execute` span (`internal/agent/tracing.go:181-198`, `startToolSpan`/
`endToolSpan` — the established pattern for stamping this span: `span.SetAttributes(attribute.X(...))`
before `.End()`, see `tracing.go:183-186, 193-196`). **Recommended shape:** since `runTool` already
has `res` (the returned `tools.ToolResult`, which after D-10 carries the `replayedMarker` substring
when replayed) AND the `verdict`/layer information is NOT visible this high up the call chain today
(`execTool` swallows it), the cleanest non-invasive path is for `execTool`
(`internal/agent/llm_agent_retry.go:72-186`) to derive the two attribute values right where it
already reads `verdict.Replay`/`verdict.OperationDecision` (lines 92, 141-143) and set them on the
span it receives via `ctx` — OR return them alongside `tools.ToolResult` for `runTool` to attribute
after `endToolSpan`. Both satisfy D-10; CONTEXT.md leaves the exact plumbing to Claude's Discretion
by extension (RESEARCH.md Open Question 1), so pick whichever needs the smaller signature change
and note the choice in the plan.

**Layer discrimination (`reservation` vs `operation`) — already computable without new plumbing:**
`internal/gateway/decide.go:76-83` shows `Decide` already tags the verdict:
```go
operationVerdict, proceed := g.beginOperation(ctx, spec, rawArgs, key, tier)
if !proceed {
	return operationVerdict, nil   // Layer B replay: OperationDecision == idempotency.DecisionReplay
}
verdict, err := g.reserve(ctx, spec, rawArgs, key, tier, operatorID)
verdict.OperationDecision = operationVerdict.OperationDecision  // DecisionAcquired here
return verdict, err
```
So: `verdict.OperationDecision == idempotency.DecisionReplay` → Layer B (`"operation"`); else
`verdict.Replay != nil` (set by `reserve()`'s rows==0 branch) → Layer A (`"reservation"`). No new
field is needed on `Verdict` to know which layer produced a given replay.

**Existing test to extend (unit tier, mirrors this seam exactly):**
`internal/gateway/reserve_test.go` — `TestReplayResultMissingSidecar` (lines 98-112) is the direct
analog for a new `TestReplayedMarkerAppendedOnBothLayers`-style test:
```go
// internal/gateway/reserve_test.go:100-111
func TestReplayResultMissingSidecar(t *testing.T) {
	res := replayResult(&toolinvocations.Event{
		ResultPreview:     "partial preview",
		ResultSidecarPath: "/nonexistent/definitely-gc-ed.result",
		ResultTruncated:   true,
	})
	if res.FullPath != "" { t.Fatalf(...) }
	if !strings.Contains(res.Preview, "partial preview") || !strings.Contains(res.Preview, "result expired") {
		t.Fatalf(...)
	}
}
```

---

### 2. The `ReplayPolicy` type and every branch over it — how the codebase adds an enum value

**File:** `internal/agent/tools/spec.go` (221 LOC), lines 72-83 (confirmed exact against CONTEXT.md
and RESEARCH.md):
```go
// ReplayPolicy is the finite way a completed mutation can be returned safely.
type ReplayPolicy string

// The replay policy, idempotency scopes, and argument normalizer a mutating tool
// declares in its Spec. ReplayToolResult is the only safe replay today: a repeated
// call returns the recorded ToolResult instead of re-running the mutation.
const (
	ReplayToolResult             ReplayPolicy      = "tool_result"
	OperationScopeAgent          idempotency.Scope = idempotency.ScopeAgentTool
	OperationScopeMCP            idempotency.Scope = idempotency.ScopeMCPTool
	OperationNormalizerCanonical                   = "canonical_tool_args_v1"
)
```

**Every place `ReplayPolicy` is compared against anything — the complete surface, confirmed by
RESEARCH.md's grep:**
- `internal/gateway/reserve.go:43` — the ONLY comparison in the codebase:
  `spec.ReplayPolicy != tools.ReplayToolResult` → deny. **D-07 leaves this exact line unchanged.**
- `internal/agent/tools/spec.go:88` — `OperationFingerprint`'s completeness guard:
  `if !spec.Mutating || spec.OperationScope == "" || spec.OperationNormalizer == "" || spec.ReplayPolicy == "" { return ..., errors.New("tool operation metadata is incomplete") }`
  (an emptiness check, not a value-switch).

**There is no multi-case `switch` over `ReplayPolicy` anywhere — by design.** `ReplayPolicy` is a
single-value enum with one equality guard, not a registry. This is directly evidenced by D-07/D-08:
`ReplayReissueExecutes` was never built, and the guard above is the entire "vocabulary." **The
planner must NOT add a switch statement or a second constant** — D-09's new boot-guard
(§Pattern: fail-loud boot check, below) is the only new branching this phase adds near `ReplayPolicy`,
and it checks for EMPTINESS, not for a specific value.

**What an actual multi-case switch over a sibling enum in the SAME file family looks like** (for
when the planner needs the general "add a case" idiom elsewhere in this phase, e.g. nothing here
requires it, but D-01's neighbouring code has one) —
`internal/agent/idempotency_operation.go:22-27`:
```go
switch parent.Key.Scope {
case idempotency.ScopeHTTPMutation, idempotency.ScopeCLICommand,
	idempotency.ScopeSchedulerRun, idempotency.ScopeApproval:
default:
	return nil, errUnsupportedParentOperation
}
```
This is the codebase's idiom for "enumerate the known-good cases, fail closed on default" — the
same shape D-04's fail-closed round-ordinal check should follow (see §5 below / Pattern 1).

---

### 3. Child-operation-key construction path + threading the round ordinal across the `ic.Ctx`/`spanCtx` boundary

**Files:** `internal/agent/idempotency_operation.go` (56 LOC, full file already quoted in
RESEARCH.md Finding 2 and reproduced below), `internal/gateway/reserve.go` (the consumer side).

**The full key-derivation function today** (this IS the file D-01/D-04 edit — not an analog, the
target):
```go
// internal/agent/idempotency_operation.go, full file
package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
)

var errUnsupportedParentOperation = errors.New("unsupported parent operation scope")

func deriveToolOperationContext(ctx context.Context, spec tools.Spec, args json.RawMessage) (context.Context, error) {
	parent, ok := idempotency.OperationFromContext(ctx)
	if !ok || parent.Key.Scope == spec.OperationScope {
		return ctx, nil
	}
	switch parent.Key.Scope {
	case idempotency.ScopeHTTPMutation, idempotency.ScopeCLICommand,
		idempotency.ScopeSchedulerRun, idempotency.ScopeApproval:
	default:
		return nil, errUnsupportedParentOperation
	}
	toolFingerprint, err := tools.OperationFingerprint(spec, args)
	if err != nil {
		return nil, err
	}
	childKeyFingerprint, err := idempotency.FingerprintTyped(struct {
		Version           string            `json:"version"`
		ParentScope       idempotency.Scope `json:"parent_scope"`
		ParentKey         string            `json:"parent_key"`
		ParentFingerprint string            `json:"parent_fingerprint"`
		ToolScope         idempotency.Scope `json:"tool_scope"`
		ToolFingerprint   string            `json:"tool_fingerprint"`
	}{
		Version: "tool-child-v1", ParentScope: parent.Key.Scope, ParentKey: parent.Key.Key,
		ParentFingerprint: idempotency.FingerprintHex(parent.Fingerprint), ToolScope: spec.OperationScope,
		ToolFingerprint: idempotency.FingerprintHex(toolFingerprint),
	})
	if err != nil {
		return nil, err
	}
	return idempotency.WithOperation(ctx, idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: parent.Key.IdentityID, Scope: spec.OperationScope,
			Key: "child:" + idempotency.FingerprintHex(childKeyFingerprint),
		},
		Fingerprint: toolFingerprint, Correlation: idempotency.FingerprintHex(parent.Fingerprint),
	})
}
```
D-01 adds `RoundOrdinal uint32` to the anonymous struct; D-04 adds `round, ok :=
modelRoundFromContext(ctx); if !ok { return nil, errMissingModelRound }` before the struct is built
— same shape as the `switch`/`default` block six lines above it.

**The round-ordinal source** — `internal/agent/model_round.go` (30 LOC, full file):
```go
package agent

import (
	"context"

	"github.com/google/uuid"
)

type modelRound struct {
	requestID uuid.UUID
	ordinal   uint32
}

type modelRoundOrdinal uint32

func (ordinal *modelRoundOrdinal) next(requestID uuid.UUID) modelRound {
	(*ordinal)++
	return modelRound{requestID: requestID, ordinal: uint32(*ordinal)}
}

type modelRoundContextKey struct{}

func withModelRound(ctx context.Context, round modelRound) context.Context {
	return context.WithValue(ctx, modelRoundContextKey{}, round)
}

func modelRoundFromContext(ctx context.Context) (modelRound, bool) {
	round, ok := ctx.Value(modelRoundContextKey{}).(modelRound)
	return round, ok
}
```

**THE closest existing example of threading a per-round-ish value across exactly this
`ic.Ctx`/dispatch boundary — already done ONCE in this very function, for `RequestID`:**
`internal/agent/llm_agent.go:208-213` (inside `Run`, BEFORE the loop, i.e. the same
"re-point `ic.Ctx` once, before the machinery that needs it runs" shape D-03 must copy):
```go
turnCtx, turnSpan := startTurnSpan(ic.Ctx, requestID, a.sessionID)
// Thread the per-turn request_id onto the ctx so execTool can build the gateway
// ReservationKey (conversation + request + tool_call) without a signature change on
// the dispatch chain (Open Q2 — the smaller signature touch).
turnCtx = tools.WithRequestID(turnCtx, requestID)
ic = ic.WithContext(turnCtx)
```
`tools.WithRequestID`/`tools.RequestIDFromContext` (`internal/agent/tools/result.go:63,68`) is the
SAME `context.WithValue`-behind-an-unexported-key idiom `model_round.go` already uses. **This is
the exact precedent for D-03**: the comment even states the identical rationale ("without a
signature change on the dispatch chain") that D-03's own reasoning uses. D-03's line goes at the
same call depth, just once per ROUND instead of once per TURN — inside the `for` loop, immediately
before `a.dispatch(ic, ...)` at `llm_agent.go:547`:
```go
// llm_agent.go:546-547 — the exact insertion point (current code):
a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls})
done, infraErr := a.dispatch(ic, spanID, parentSpanID, requestID, calls, &turnU, yield)
```
D-03's addition is one line inserted between those two: `ic = ic.WithContext(withModelRound(ic.Ctx, modelRound))`
(exact identifier names at implementer's discretion) — mirroring line 213's `ic =
ic.WithContext(turnCtx)` precisely, using the SAME `WithContext` copy-semantics method
(`internal/agent/agent.go:74-80`, confirmed to return a copy, never mutate the receiver).

**Confirms the plumbing gap D-03 exists to close** — `internal/agent/llm_agent_dispatch.go:14,109`:
```go
func (a *LlmAgent) dispatch(ic InvocationContext, ...) (done bool, infraErr error) {
	...
	runs := a.executeBatch(ic.Ctx, ic.Budget, batch, startedAt)  // ic.Ctx, NOT spanCtx
```
`spanCtx` (which today carries the round via `withModelRound(spanCtx, modelRound)` at
`llm_agent.go:355`) is a local variable inside `Run`'s loop body that is never assigned onto
`ic.Ctx` — that assignment is exactly D-03's job, mirroring the `turnCtx`/`ic.WithContext` pattern
already used once per turn at line 213.

---

### 4. The supersede/validity-window UPDATE + `fact_key`/`factIdentity` neighbours, and the exact-match-beside-broad-match shape

**Files:** `internal/arcadedb/memory.go` (495 LOC), `internal/arcadedb/memory_provenance.go`
(338 LOC, unchanged by this phase — read-only neighbour).

**The broad-match UPDATE that exists today** — `memory.go:166-178`:
```go
// closeSupersededStatement ends the validity window of every still-valid fact
// with the same subject and predicate. It does not delete: the row stays
// queryable through `as_of`.
//
// The object is deliberately NOT in the WHERE clause -- the object is the thing
// that changed, so matching on it would mean the statement could never fire.
const closeSupersededStatement = "UPDATE " + factEdgeType + " SET valid_to = :valid_to, " +
	"expired_at = :expired_at, fact_key = NULL WHERE predicate = :predicate AND expired_at IS NULL " +
	"AND (valid_to IS NULL OR valid_to > :valid_to) " +
	"AND fact_key <> :fact_key AND outV().name = :subject_name"
```
Invoked from `UpsertFact` (`memory.go:231-243`) with `fact_key: factKey` bound to the NEW fact's
own key (self-exclusion, not target disambiguation):
```go
if fact.Supersedes {
	rows, err := c.Command(ctx, closeSupersededStatement, map[string]any{
		"valid_to": validFrom.UTC().Format(time.RFC3339), "expired_at": now.UTC().Format(time.RFC3339),
		"predicate": fact.Predicate, "subject_name": fact.Subject, "fact_key": factKey,
	})
	...
	written.Superseded = countUpdated(rows)
}
```

**D-15's new exact-match variant to add BESIDE it** (this is the "closest existing example of an
exact-match single-row UPDATE beside a broad-match one" — there is no third pre-existing example in
this file; the broad-match statement above IS the base the new statement is derived from by
swapping the WHERE clause):
```sql
-- NEW (D-15), same SET clause, WHERE narrowed to one edge by fact_key:
UPDATE FACT SET valid_to = :valid_to, expired_at = :expired_at, fact_key = NULL
WHERE fact_key = :target_fact_key AND expired_at IS NULL
```
Follow the SAME const-string-with-doc-comment convention as `closeSupersededStatement` (a
package-level `const` just above the function that uses it, doc comment explaining the WHY not just
the WHAT — see the "object deliberately NOT in the WHERE" comment above as the house style).

**`fact_key`'s producer** — `memory_provenance.go:206-223`:
```go
func factIdentity(fact Fact) string {
	hash := sha256.New()
	var size [8]byte
	for _, field := range []string{fact.Subject, fact.Predicate, fact.Object, fact.Statement} {
		value := []byte(strings.TrimSpace(field))
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func activeFactKey(key string, validTo, now time.Time) any {
	if !validTo.IsZero() && !validTo.After(now) {
		return nil
	}
	return key
}
```
`factIdentity` is deterministic and content-derived (no randomness); `activeFactKey` NULLs it at
CREATE time for an already-expired fact, mirroring what `closeSupersededStatement`'s own `fact_key
= NULL` SET clause does at CLOSE time — both paths independently null the key, confirmed
consistent by RESEARCH.md Finding 4.

**`FactHit`/`MemorySearchHit` — the two structs D-15 must add a `FactKey`/`fact_key` field to,
plus their two SQL projections that currently omit it:**
```go
// internal/arcadedb/memory.go:273-284 — no FactKey field today
type FactHit struct {
	Statement string `json:"statement"` Predicate string `json:"predicate"`
	Subject string `json:"subject"` SubjectKind string `json:"subject_kind,omitempty"`
	Object string `json:"object"` ObjectKind string `json:"object_kind,omitempty"`
	ValidFrom string `json:"valid_from"` ValidTo string `json:"valid_to,omitempty"`
	Sources []FactSource `json:"sources"`
}
```
Both `searchFactsStatement` (`memory.go:290-294`) and `factsAboutStatement` (`memory.go:352-355`)
project `statement, predicate, valid_from, valid_to, sources, outV().name AS subject, ...` — neither
selects `fact_key`; both need it added to the `SELECT` list and `factHitFromRow` (`memory.go:425-437`)
needs the corresponding `FactKey: rowString(row, "fact_key")` line. The MCP-facing twin
(`cmd/arcadedb-mcp/tool_memory.go:114-124`, `MemorySearchHit`) and its converter `toHits`
(`tool_memory.go:300-320`) need the same field threaded through.

**REUSABLE CODE flag:** `factHitFromRow` already centralizes the row→struct mapping for BOTH
`SearchFacts` and `FactsAbout` (both call it) — adding `FactKey` there is one change point, not
two. Keep it that way; do not inline a second row-mapper.

---

### 5. Test analogs per tier

| Tier | Closest existing test to extend/pattern-match | File | Notes |
|------|------------------------------------------------|------|-------|
| **Unit (untagged)** — idempotency key + fail-closed | none exists yet for this file (`idempotency_operation_test.go` is `MISSING`) | new: `internal/agent/idempotency_operation_test.go` | Pattern-match `internal/agent/tools/spec_test.go`'s style for `OperationFingerprint`, or simplest: table-driven `deriveToolOperationContext` calls with/without `modelRoundFromContext` set, asserting the derived key CHANGES across rounds and STAYS THE SAME on the same round (fingerprint equality on `idempotency.OperationFromContext(ctx).Key.Key`). |
| **Unit (untagged)** — replay marker | `internal/gateway/reserve_test.go` | 148 LOC | `TestReplayResultMissingSidecar` (lines 100-111, quoted in §1 above) is the DIRECT analog: same file, same "call the pure function, assert on `.Preview`" shape. Add `TestReplayResultAppendsReplayedMarker` and a Layer-B-analog test for `decodeOperationReplay` beside it. |
| **Unit (untagged)** — adversarial replay/reissue/reclaim triad (D-25) | `internal/gateway/reserve_test.go` `TestReserveReplayOnConflict` (lines 64-81) + `TestReserveDeniesAnUnaccountedPriorDispatch` (lines 124-139, the fabricated-success regression `reserve.go:233-246` documents) | 148 LOC | These two existing tests already exercise exactly the two halves D-25 needs paired: "replay on same key" and "the fabricated-success case, now correctly denied." D-25's new adversarial tests (same-id retry replays / later-round reissue re-executes / reclaim executes once) belong in this same file, using `gatedSpec()`/`testKey()`/`fakeStore` already defined here. |
| **Unit (untagged)** — id uniquify + same-message dedupe (D-12/D-13) | `internal/agent/budget_dedup.go` + `internal/agent/budget_dedup_test.go` (531 LOC) | new source file, new test file | See §Pattern Assignments below — this is the concern-split precedent to COPY, not just a test analog: a dedicated `<name>_<concern>.go` + `_test.go` pair, same package (`agent`), same "loop-guard, sha256-fingerprint-based" vocabulary. |
| **Unit (untagged)** — completion gate widening (D-20) | `internal/agent/llm_agent_completion_test.go` (267 LOC) — extend | existing | `TestCompletionGate_ReadOnlyTurn_Skipped` (line 107) is the test that currently PINS the `!a.sideEffected` short-circuit D-20 removes — it must be rewritten (not deleted silently) to assert the gate NOW runs on read-only turns too. `TestCompletionGate_NotDone_VetoesOnceThenAccepts` (line 158) is the closest analog for the new "vetoes TWICE then accepts" shape (veto budget 1→2). |
| **Unit (untagged)** — memory pure-logic (`looksLikeProse`, `canonicalSubject`, candidate selection) | `internal/arcadedb/memory_test.go` (459 LOC) — extend | existing | `TestValidateRejectsIncompleteFacts` (line 145) and `TestValidateRejectsOversizedFactInputs` (line 168) are the direct analogs for a new `TestValidateRejectsProseObject` table test — same file, same `Fact.validate(limits)` call shape. |
| **`db_integration`** — `aura.tool_invocations` assertions (ACC-02, D-25's scheduler-reclaim simulation) | `internal/gateway/gateway_integration_test.go` | build-tagged `db_integration`, 305+ LOC (not counted against LOC cap — test file) | Full header pattern to copy (build tag, `envOrSkip`, no-skip-as-green rationale) at lines 1-16; `spyTool` (lines 47-62) + `gatedExec` (lines 67-86, mirrors `execTool`'s Decide→Deny/Approve/Replay/Allow ordering exactly) are the harness pieces D-25's live scheduler-reclaim test should reuse rather than re-invent. |
| **`arcadedb_integration`** — memory validity-window assertions (HARN-04, D-23) | `internal/arcadedb/memory_integration_test.go` | build-tagged `arcadedb_integration`, `TestSupersessionClosesTheWindowAndKeepsThePastQueryable` at lines 440-483 | THIS is the closest existing example of a validity-window assertion against a live database — read it in full before writing D-15/D-16's `fact_key`-targeted-close live test: it isolates a fresh `subject`/`runID` per test (`isolate(t, client)`), writes two facts with `Supersedes: true`, asserts `written.Superseded`, then re-queries BOTH the present (`SearchFacts(..., time.Time{})`) and the past (`SearchFacts(..., now.AddDate(-3,0,0))`) to prove the old fact is closed-not-deleted (`hit.ValidTo != ""`). The new test for D-16's ambiguity-refusal path (>1 candidate → refuse, siblings untouched) is a direct structural sibling: write N facts sharing subject+predicate, call the ambiguous-supersede path, assert `refused==true` and that ALL N facts are STILL open (`ValidTo == ""`). |
| **`arcadedb_integration`** — MCP-level refusal payload (D-17) | `cmd/arcadedb-mcp/tool_memory_test.go` (430 LOC, likely untagged/unit against a fake or in-memory client — verify tag before use) | existing, extend | `TestUpsertFactRejectsBadInput` (line 197) is the closest analog for a new `TestUpsertFactRefusesOnAmbiguousSupersede` — same handler-under-test shape (`memoryUpsertFactHandler`), same input/output struct assertions. |
| **Live conversation (ACC-01/D-22/D-23, manual-only)** | none — this is explicitly not a `go test` tier | N/A | Success Criterion 4 is proven by correcting the real F-1-caused misdiagnosis fact in live long-term memory, read back via OTel + `aura.tool_invocations` + the ArcadeDB graph itself, scored per CLAUDE.md's Definition of Done. No file to point at. |

---

## File Classification

| New/Modified File | Role | Data Flow | Current LOC / 600 cap | Closest Analog | Match Quality |
|---|---|---|---|---|---|
| `internal/agent/idempotency_operation.go` | utility (key derivation) | transform | 56 — plenty of headroom | itself (target file); sibling shape: `internal/agent/tools/spec.go` `OperationFingerprint` | exact (same package, same concern) |
| `internal/agent/llm_agent.go` | controller (agent turn loop) | event-driven / dispatch | **579 / 600 — 21 LOC of headroom, WATCH** | itself; D-03's precedent is its OWN lines 208-213 (`turnCtx`/`ic.WithContext` for `RequestID`) | exact — the analog is in the same file |
| **NEW** `internal/agent/llm_agent_call_dedup.go` (proposed name) | utility (loop-guard transform) | transform | 0 → new file, budget ~150-250 LOC | `internal/agent/budget_dedup.go` (272 LOC) + `internal/agent/budget_dedup_test.go` (531 LOC) | exact — same package, same "concern split out per RESEARCH #1 so no file approaches 600 LOC" precedent |
| `internal/agent/llm_agent_completion.go` | controller (critic gate) | request-response | 287 / 600 | itself; `gateCompletion` (lines 58-72) is the exact target | exact |
| `internal/gateway/reserve.go` | service (idempotent-write orchestration) | CRUD / idempotent-write | 305 / 600 | itself; `resultExpiredMarker`/`replayResult` (§1 above) is the exact target | exact |
| `internal/gateway/guard.go` | config/validation (boot-time guard) | batch (boot-time loop) | 34 / 600 | itself; `ValidateClassifiable` is the exact shape to extend with a second loop body | exact |
| `internal/arcadedb/memory.go` | model/service (bitemporal fact store) | CRUD | **495 / 600 — 105 LOC headroom, THREE decisions land here, WATCH** | itself; `closeSupersededStatement`/`UpsertFact` (§4 above) | exact |
| `cmd/arcadedb-mcp/tool_memory.go` | controller (MCP tool handler) | request-response | 320 / 600 | itself; `memoryUpsertFactHandler` (lines 59-103) | exact |
| `internal/toolinvocations/redact.go` (fix-on-touch, stale comment only) | utility (redaction) | transform | 77 / 600 | itself — comment-only fix per Claude's Discretion note | exact (no logic change) |
| `.planning/ROADMAP.md` §45/§46, `prd.md` (amendment commits, D-08) | docs, not source | N/A | N/A | N/A | N/A — prose amendment, not a pattern-mappable file |
| `CLAUDE.md` (arcadedb_integration claim correction) | docs, not source | N/A | N/A | N/A | N/A |

**God-class accounting (CLAUDE.md's ≤600 LOC / refactor-on-touch rule):**
- `internal/agent/llm_agent.go` is **579/600 already**, before this phase's own one-line D-03
  change. The file's OWN established discipline is to split concerns out the moment they'd grow it
  (see `budget_dedup.go`'s header: "split out of budget.go per RESEARCH #1 so neither file
  approaches the 600-LOC no-god-class cap"). **D-12/D-13's new logic (id uniquify + same-message
  dedupe) MUST NOT be added inline to `llm_agent.go`** — it must land in a NEW file
  (`internal/agent/llm_agent_call_dedup.go` or equivalent `<name>_<concern>.go` name), with only
  the two call-site lines (one right after `consume()` returns at line 433, one right before the
  history append at line 546) touching `llm_agent.go` itself. Even so, D-03's insertion + those two
  call-site lines will likely tip `llm_agent.go` over 600 — **budget for a companion split** if it
  does (e.g. move `dispatch`'s already-separate concerns further, or extract the loop's
  per-call-setup block used at lines 299-355 into `llm_agent_round.go`); flag this explicitly in
  the plan's file-budget rather than discovering it mid-implementation.
- `internal/arcadedb/memory.go` is **495/600** with **three decisions (D-15, D-16, D-18) landing in
  the same phase** — RESEARCH.md already flags this file as "the one to watch." Concrete estimate:
  `fact_key`-targeted UPDATE (+~15 LOC), ambiguity candidate-selection function (+~40-60 LOC),
  `looksLikeProse` validation rule (+~20-30 LOC), `FactHit.FactKey` field + two SELECT-clause edits
  (+~10 LOC) — plausibly **+90-115 LOC**, landing at **585-610**. **If it crosses 600, the concrete
  split is `memory_supersede.go`**: move `closeSupersededStatement`, the new exact-match UPDATE
  constant, `UpsertFact`'s `if fact.Supersedes` branch, and the new ambiguity-candidate-selection
  helper into it (same package `arcadedb`), leaving `memory.go` with schema bootstrap, `Fact`/
  `FactHit` types, `UpsertFact`'s entity-minting + create-edge core, `SearchFacts`, and
  `FactsAbout`. This mirrors the `memory.go`/`memory_provenance.go` split that already exists in
  this same directory (provenance concerns already live in a sibling file).

---

## Pattern Assignments

### `internal/agent/idempotency_operation.go` (utility, transform)
**Analog:** itself — target file, full contents quoted in §3 above.
**Change:** add `RoundOrdinal uint32` to the anonymous `FingerprintTyped` struct literal
(lines 32-39); add the fail-closed extraction (`round, ok := modelRoundFromContext(ctx); if !ok {
return nil, errMissingModelRound }`) using the SAME sentinel-error shape as
`errUnsupportedParentOperation` (line 12) and the SAME `switch`/`default`-adjacent fail-closed
posture already in this function (lines 22-27, quoted in §2 above).

### `internal/agent/llm_agent.go` (controller, event-driven)
**Analog:** itself — lines 208-213 (`turnCtx`/`tools.WithRequestID`/`ic.WithContext`), quoted in
full in §3 above.
**Change:** ONE line inserted between the existing lines 546 and 547 (confirmed exact current
content: `a.history = append(...)` then `done, infraErr := a.dispatch(ic, ...)`):
`ic = ic.WithContext(withModelRound(ic.Ctx, modelRound))`. Copy the SAME copy-semantics call
(`InvocationContext.WithContext`, `internal/agent/agent.go:74-80`) the line-213 precedent already
uses — do not invent a second context-threading mechanism.

### NEW `internal/agent/llm_agent_call_dedup.go` (utility, transform)
**Analog:** `internal/agent/budget_dedup.go` (272 LOC) — copy its shape wholesale: a top-of-file
doc comment explaining WHY the split exists and WHY the specific algorithm was chosen (see
`budget_dedup.go:1-20`, quoted below), `package agent`, sha256-based fingerprinting for the
dedupe-by-`(name,arguments)` half:
```go
// internal/agent/budget_dedup.go:1-29 (header pattern to copy)
// Two-tier loop-guard dedup (D-18/A2), split out of budget.go per RESEARCH #1 so
// neither file approaches the 600-LOC no-god-class cap (CLAUDE.md).
// ...
package agent

import (
	"crypto/sha256"
	"os"
	"slices"
	"strings"
	"sync"
)
```
**Two functions this file houses (D-12/D-13):**
1. `uniquifyToolCallIDs(calls []llm.ToolCall) []llm.ToolCall` — the `<id>_d<n>` collision repair +
   `call_<sha256("name:args:index")[:12]>` blank-id fallback (D-13). Called immediately after
   `consume()` returns, at `llm_agent.go:433`, before the terminal/runnable partition in `dispatch`.
2. `dedupeSameMessageCalls(calls []llm.ToolCall) []llm.ToolCall` — drops a later call whose
   `(name, arguments)` exactly matches an earlier call in the SAME message (D-12 point 2). Called
   immediately before the history append at `llm_agent.go:546`.
**Test sibling:** `internal/agent/llm_agent_call_dedup_test.go`, following
`internal/agent/budget_dedup_test.go` (531 LOC) table-driven style.
**REUSABLE CODE note:** both functions operate on `[]llm.ToolCall`; if both need a
canonical-argument-comparison helper, reuse `internal/canonicaljson.CanonicalArgs` (already used at
`llm_agent_dispatch.go:76`, `c := canonicaljson.CanonicalArgs(call.Function.Arguments)`) rather than
writing a second JSON-canonicalization routine.

### `internal/agent/llm_agent_completion.go` (controller, request-response)
**Analog:** itself — `gateCompletion` (lines 58-72), full file quoted above.
**Change:** drop `!a.sideEffected` from the guard at line 59 (`if !a.cfg.CompletionGate ||
!a.sideEffected || a.completionAttempts >= 1`); raise `>= 1` to `>= 2`; add a second, more specific
nudge string beside `completionVetoPrefix`. Existing test to rewrite (not silently delete):
`TestCompletionGate_ReadOnlyTurn_Skipped` (`llm_agent_completion_test.go:107`) currently PINS the
behavior D-20 removes.

### `internal/gateway/reserve.go` (service, CRUD/idempotent-write)
**Analog:** itself — full marker/replay seam quoted in §1 above.
**Change:** `const replayedMarker = "..."` beside `resultExpiredMarker` (lines 24-28); append it in
`replayResult` (Layer A) and add equivalent logic to `decodeOperationReplay` (Layer B, genuinely
new); extract the shared append step into one small helper per the REUSABLE CODE flag in §1; OTel
attribute plumbing resolved per §1's "Open Question 1" recommendation (attribute at the
`tool.execute` span in `llm_agent_tool.go`/`tracing.go`, not inside this file, since no span/ctx is
available here today).

### `internal/gateway/guard.go` (config/validation, batch)
**Analog:** itself — `ValidateClassifiable` (full file, quoted above).
**Change:** one more loop body in the SAME function (or a second exported function following the
identical shape), panicking on a registered `Mutating` tool with empty `OperationScope`,
`OperationNormalizer`, or `ReplayPolicy` — copy the exact `panic(fmt.Sprintf(...))` idiom at
lines 28-32.

### `internal/arcadedb/memory.go` (model/service, CRUD)
**Analog:** itself — `closeSupersededStatement`/`UpsertFact`/`Fact.validate`/`FactHit`, all quoted
in full in §4 above.
**Changes:** (a) new exact-match UPDATE constant beside `closeSupersededStatement`; (b) `FactKey`
field on `FactHit` + `fact_key` added to both SELECT projections + `factHitFromRow`; (c) ambiguity
candidate-selection logic for D-16 (0/1/>1 matches); (d) `looksLikeProse` validation rule added to
`Fact.validate` (lines 110-158), following the existing `validateRuneLimit` per-field-loop
convention (lines 131-148) rather than a bespoke check. **Watch the LOC budget — see the
god-class accounting above.**

### `cmd/arcadedb-mcp/tool_memory.go` (controller, request-response)
**Analog:** itself — `memoryUpsertFactHandler` (lines 59-103), `MemoryUpsertFactInput`/`Output`
structs, full relevant sections quoted above.
**Changes:** (a) `supersedes_fact_key` field on `MemoryUpsertFactInput`; (b) `refused bool, reason
string, candidates []MemorySearchHit` on `MemoryUpsertFactOutput` (D-17 — a normal successful
return, NOT an `mcp.ToolCallError`; the existing handler already returns `(nil,
MemoryUpsertFactOutput{...}, nil)` on every path, which is the shape to keep); (c) MEM-04's
host-side subject canonicalization inserted before the `arcadedb.Fact{}` construction at
line 80, mirroring `internal/agent/mcptools/bridge_memory.go:45-58`'s `withMemoryUserIdentifier`
(full file quoted above) — same "resolve identity, rewrite the argument before it reaches the
domain call" shape, but placed in THIS handler (not the bridge) per D-19's explicit rationale that
the bridge is bypassed by CLI/host-driven calls.

---

## Shared Patterns

### Fail-closed context extraction with an explicit sentinel error
**Source:** `internal/agent/idempotency_operation.go:12, 22-27` (quoted in full in §3).
**Apply to:** D-04's round-ordinal extraction inside `deriveToolOperationContext` — same file,
same function, same idiom already used four lines above the insertion point.

### Marker-in-preview, not a new ledger row or struct field
**Source:** `internal/gateway/reserve.go:24-28, 293-296` (quoted in full in §1).
**Apply to:** D-10's `replayedMarker`. Never add a `ToolResult.Replayed bool` field or a new
`event_kind` — the established idiom is a bracketed string appended to `.Preview`.

### OTel span lifecycle: `startXSpan`/`endXSpan` pair with `attribute.X(...)` at End
**Source:** `internal/agent/tracing.go:176-198` (`startToolSpan`/`endToolSpan`), and the sibling
`startTurnSpan`/`endTurnSpan` (lines 149-174) which shows the SAME pattern for a different span.
**Apply to:** D-10's `aura.tool.replayed`/`aura.tool.replay_layer` attributes — call
`span.SetAttributes(attribute.Bool(...), attribute.String(...))` immediately before `.End()`,
exactly like `endToolSpan`'s existing `attribute.Bool("tool.success", ...)` call.

### Boot-time fail-loud panic guard, one loop over `reg.All()`
**Source:** `internal/gateway/guard.go` (full file, quoted above), which itself documents mirroring
`tools.Registry.Register`'s duplicate-name panic and `Registry.Validate`'s fail-closed boot check.
**Apply to:** D-09's second assertion — literally extend the same function/file.

### Per-turn/per-round context-value threading via `WithContext` copy semantics
**Source:** `internal/agent/llm_agent.go:208-213` (quoted in full in §3) — `ic =
ic.WithContext(turnCtx)` after `context.WithValue`-wrapping via `tools.WithRequestID`.
**Apply to:** D-03's round-ordinal threading. This is the single most load-bearing analog in this
phase: it is the SAME function, the SAME receiver, doing the SAME kind of context-value handoff for
a sibling value (`RequestID`) already, one loop-iteration-scope higher (per-turn vs. per-round).

### Ambiguity resolved by returning candidates, not by erroring
**Source:** no existing Aura precedent (confirmed by RESEARCH.md Pattern 3) — this is genuinely new
to the codebase, adopted from hermes (`tools/memory_tool.py:462-491,615-642`, cited but not
independently re-read this session). The nearest Aura idiom it must NOT be confused with is
`mcp.ToolCallError` + `DeterministicNoEffect()` (`internal/mcp/tool_error.go:50-65`), explicitly
rejected by D-17.
**Apply to:** D-16/D-17's refusal contract in `tool_memory.go`.

---

## No Analog Found

| File/Logic | Role | Data Flow | Reason |
|---|---|---|---|
| Ambiguity candidate-selection (D-16, 0/1/>1 matches → refuse/close/refuse) | service logic in `memory.go` | transform | No existing Aura code resolves a target by returning a candidate set on ambiguity anywhere in this codebase (confirmed, RESEARCH.md Pattern 3) — build per D-16's spec, hermes-derived. |
| `looksLikeProse` predicate (MEM-05) | validation in `memory.go` | transform | No existing prose-detection rule anywhere in `Fact.validate` or elsewhere in `internal/arcadedb` (confirmed, RESEARCH.md Finding 4) — new logic, shape left to Claude's Discretion (rune bound, newline rule, terminal-punctuation rule). |
| OTel span attribute plumbing across the `reserve.go` → `execTool` → `runTool` boundary (D-10) | observability wiring | transform | `reserve.go`'s replay functions take no `context.Context`/span today (confirmed, RESEARCH.md Finding 3, Open Question 1) — the attribute site is `tracing.go`'s existing `startToolSpan`/`endToolSpan` idiom, but the EXACT signature change to reach it from `reserve.go` is genuinely new plumbing, not a copy of an existing call site. |

---

## Metadata

**Analog search scope:** `internal/agent`, `internal/gateway`, `internal/arcadedb`,
`cmd/arcadedb-mcp`, `internal/agent/tools`, `internal/agent/mcptools`, `internal/idempotency`
(read-only reference), `internal/toolinvocations` (read-only reference for the schema constraint).
**Files scanned (full or targeted read):** `idempotency_operation.go`, `llm_agent.go`,
`llm_agent_completion.go`, `llm_agent_dispatch.go`, `llm_agent_retry.go`, `llm_agent_tool.go`,
`model_round.go`, `agent.go`, `tracing.go`, `budget_dedup.go`, `reserve.go`, `guard.go`,
`decide.go`, `reserve_test.go`, `gateway_integration_test.go`, `memory.go`,
`memory_provenance.go`, `memory_test.go`, `memory_integration_test.go`, `tool_memory.go`,
`tool_memory_test.go`, `bridge_memory.go`, `tools/spec.go` — 23 files.
**Pattern extraction date:** 2026-08-13.
**Consumed 45-RESEARCH.md's file:line facts directly**; every excerpt above was independently
re-read in this session (not copy-pasted from RESEARCH.md's prose) to get exact current line
numbers and full surrounding context for the planner.
