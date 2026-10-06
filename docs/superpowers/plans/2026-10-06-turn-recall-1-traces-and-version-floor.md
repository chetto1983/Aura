# Turn recall, plan 1 of 3: tool-only traces and the ArcadeDB version floor

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record the tool calls of turns that exposed no reasoning in the ArcadeDB reasoning graph, and enforce the ArcadeDB 26.10.1 floor that turn recall's vector filters depend on.

**Architecture:** Two independent corrections the turn-recall spec requires before any recall code exists. The trace builder in `internal/runner` opens a trace on the first valid tool event instead of waiting for reasoning, and ArcadeDB validation accepts an empty summary only where tool calls stand in for it. `TenantClients.For` verifies the server version once before it hands out the first tenant client, which wires `VerifySecureVersion` for the first time.

**Tech Stack:** Go 1.26, ArcadeDB 26.10.1 (HTTP API), PostgreSQL (unchanged), `go test -race`, build tags `arcadedb_integration`.

**Spec:** `docs/superpowers/specs/2026-10-06-turn-recall-design.md` (commit `4583dc0bc`), sections "Persisting what was learned → Tool calls" and "The engine and client constraints". Plans 2 (turn recall core) and 3 (`tool_search` dense leg) follow this one.

## Global Constraints

- Every Go file stays at or under 600 lines (`make file-size`); split on touch.
- Never run `go build ./...` locally: the pre-commit hook builds. Validate per package with `go vet` and `go test -race`.
- Go runs in WSL. Below, `W '<cmd>'` means: `wsl -e bash -lc 'cd /mnt/d/Aura && export PATH=$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH && <cmd>'`.
- Commit from WSL with lefthook, never `--no-verify`. Write the message with the Write tool to a file in your scratchpad directory, then `W 'git add <new files> && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- <paths>'`. Every message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit with pathspecs only. Leave unrelated dirty files alone: `.claude/settings.json`, `prd.md` (modified by another session), `Aura_analisi_codebase.md`, `docs/verification/`, and anything else this plan does not name.
- ArcadeDB live tests (recipe **A**): `W 'source scripts/lib/disposable_stack.sh && export ARCADEDB_URL=http://127.0.0.1:2480 ARCADEDB_PASSWORD="$(read_secret ARCADEDB_PASSWORD)" CI=true && go test -tags arcadedb_integration -race -count=1 -run "<PATTERN>" ./internal/arcadedb/'`. It needs the local ArcadeDB 26.10.1 (`make memory-up`). Each test creates and drops its own database. If no local ArcadeDB is reachable, the `agent-memory-eval` CI job runs the whole tier on push, and the task stays open until that job is green on the SHA.
- No reasoning text is ever synthesized. An empty summary is stored as `""`.
- The version floor is exactly `26.10.1`.
- Comments only where the why is not obvious; all code, comments and messages in English.

## Review Focus

1. **A turn whose only tool call failed.** It must still leave a trace: a failed call with its redacted observation is the most useful thing the graph can hold. Pinned by `TestReasoningGraphRecordsAFailedToolOnlyTurn` (Task 1).
2. **A turn whose reasoning was hidden (`ShowReasoning` off) but ran tools.** The tools are recorded; not one character of reasoning reaches the trace. Pinned by `TestReasoningGraphRecordsAHiddenReasoningTurnByItsTools` (Task 1).
3. **A tool event from another provider attempt after the trace opened.** It is dropped, and a repudiated attempt's tools never survive the discard. Pinned by `TestReasoningTraceBuilderRejectsForeignRunsAndRepeatedCalls` and `TestReasoningGraphDiscardDropsAToolOnlyAttempt` (Task 1).
4. **A server below the floor.** Every `For` refuses, nothing is provisioned on it, and the refusal is re-checked on the next call instead of being cached. Pinned by `TestTenantClientsRefuseAServerBelowTheFloor` (Task 2).
5. **A deployment with no admin credential.** The floor is checked with the tenant's own credential, which must be allowed to read the server version. Measured before the wiring by `TestTenantCredentialReadsTheServerVersionLive` (Task 2). If it is refused, the plan stops.

---

### Task 1: Tool-only reasoning traces

**Files:**
- Modify: `internal/runner/runner_reasoning_graph.go` (`ObserveReasoning`, `ObserveToolInvocation`, `CommitSourceTurn`; new `start`)
- Modify: `internal/arcadedb/memory_reasoning_validate.go` (`normalizeReasoningTrace`; new `normalizeReasoningSummary`, `traceHasToolCalls`)
- Create: `internal/runner/runner_reasoning_graph_tools_test.go`
- Modify: `internal/arcadedb/memory_reasoning_validate_test.go`
- Modify: `internal/arcadedb/memory_reasoning_live_test.go`

**Interfaces:**
- Consumes: the existing test helpers in `internal/runner/runner_reasoning_graph_test.go`: `newReasoningTestRunner(t, cap, showReasoning)`, `recordingReasoningGraphSink`, `reasoningGraphEvent`, `reasoningGraphToolEvents`, `reasoningGraphFinalEvent`, `persistReasoningGraphEvents`; in `internal/arcadedb`: `validReasoningTrace`, `freshReasoningTrace`, `disposableMemoryClient`, `constantEmbedder`, `reembedMemory`.
- Produces: a `ReasoningTrace` whose `ProviderSummary`, and whose steps' `ProviderSummary`, may be `""` when the step carries tool calls. Plan 2's `RecallTurns` reads these traces' `ReasoningToolCall.tool_name`.

- [ ] **Step 1: Write the failing runner tests**

Create `internal/runner/runner_reasoning_graph_tools_test.go`:

```go
package runner

import (
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

// A turn that exposed no reasoning still ran its tools, and turn recall reads those tools
// back (spec 2026-10-06, "Tool calls"). Measured that day: the graph reached 9 of the lab
// VM's 16 tool turns, because the others had no reasoning to open a trace.
func TestReasoningGraphRecordsATurnThatOnlyRanTools(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-task", "task", `{"action":"create"}`, "ok", "scheduled", nil)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 {
		t.Fatalf("traces = %#v, want the tool-only turn recorded", sink.traces)
	}
	trace := sink.traces[0]
	if trace.TraceID != runID.String() || trace.ProviderSummary != "" {
		t.Fatalf("trace = %#v, want run %s and no summary", trace, runID)
	}
	if len(trace.Steps) != 1 || trace.Steps[0].ProviderSummary != "" || trace.Steps[0].CreatedAt.IsZero() {
		t.Fatalf("steps = %#v, want one dated summary-less step", trace.Steps)
	}
	calls := trace.Steps[0].ToolCalls
	if len(calls) != 1 || calls[0].ToolName != "task" || calls[0].Status != "succeeded" || calls[0].SourceRef != trace.SourceRef {
		t.Fatalf("tool calls = %#v", calls)
	}
}

// Reasoning that arrives after the first tool joins the trace the tool opened, as the
// step after that tool.
func TestReasoningGraphToolOpensTheTraceAndReasoningJoinsIt(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-a", "task", `{"action":"list"}`, "ok", "two tasks", nil)
	events = append(events, reasoningGraphEvent(runID, t0.Add(time.Second), "Delete the older one."))
	events = append(events, reasoningGraphToolEvents(runID, t0.Add(2*time.Second), "call-b", "task",
		`{"action":"delete"}`, "ok", "deleted", nil)...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(3*time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 {
		t.Fatalf("traces = %#v", sink.traces)
	}
	trace := sink.traces[0]
	if trace.ProviderSummary != "Delete the older one." || len(trace.Steps) != 2 {
		t.Fatalf("trace = %#v, want the reasoning as summary and two steps", trace)
	}
	first, second := trace.Steps[0], trace.Steps[1]
	if first.ProviderSummary != "" || len(first.ToolCalls) != 1 || first.ToolCalls[0].CallID != "call-a" {
		t.Fatalf("first step = %#v, want the summary-less step call-a opened", first)
	}
	if second.ProviderSummary != "Delete the older one." || len(second.ToolCalls) != 1 || second.ToolCalls[0].CallID != "call-b" {
		t.Fatalf("second step = %#v", second)
	}
}

// A failed call is the one fact the next turn must learn from, so a turn whose only tool
// failed is recorded with the failure's redacted reason.
func TestReasoningGraphRecordsAFailedToolOnlyTurn(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-bad", "whatsapp__send_message",
		`{"to":"Marco"}`, "error", "contact not found (token=sk-secret)", nil)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(time.Second), "Non trovo Marco."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 || len(sink.traces[0].Steps) != 1 {
		t.Fatalf("traces = %#v, want the failed tool-only turn recorded", sink.traces)
	}
	call := sink.traces[0].Steps[0].ToolCalls[0]
	if call.Status != "failed" || !strings.Contains(call.Observation, "contact not found") {
		t.Fatalf("failed call = %#v, want its reason", call)
	}
	if strings.Contains(call.Observation, "sk-secret") {
		t.Fatalf("failure observation skipped redaction: %q", call.Observation)
	}
}

// With ShowReasoning off the stream is redacted at the source and never observed, but the
// tools the turn ran are runtime facts, not reasoning: they are recorded, and the trace
// carries no reasoning text at all.
func TestReasoningGraphRecordsAHiddenReasoningTurnByItsTools(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, false)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := []*agent.Event{reasoningGraphEvent(runID, t0, "hidden thoughts about the reminder")}
	events = append(events, reasoningGraphToolEvents(runID, t0.Add(time.Second), "call-task", "task",
		`{"action":"create"}`, "ok", "scheduled", nil)...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(2*time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 {
		t.Fatalf("traces = %#v, want the tools of the hidden turn recorded", sink.traces)
	}
	trace := sink.traces[0]
	if trace.ProviderSummary != "" || len(trace.Steps) != 1 || trace.Steps[0].ProviderSummary != "" {
		t.Fatalf("trace = %#v, want no reasoning text", trace)
	}
	if calls := trace.Steps[0].ToolCalls; len(calls) != 1 || calls[0].ToolName != "task" {
		t.Fatalf("tool calls = %#v", calls)
	}
}

// A discarded provider attempt takes its tools with it, even when no reasoning opened the
// trace.
func TestReasoningGraphDiscardDropsAToolOnlyAttempt(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-old", "task", `{"action":"create"}`, "ok", "scheduled", nil)
	discard := &agent.Event{RequestID: runID, Timestamp: t0.Add(time.Second)}
	discard.Actions.DiscardStreamed = true
	events = append(events, discard)
	events = append(events, reasoningGraphToolEvents(runID, t0.Add(2*time.Second), "call-new", "task",
		`{"action":"create"}`, "ok", "scheduled", nil)...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(3*time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 || len(sink.traces[0].Steps) != 1 {
		t.Fatalf("traces = %#v", sink.traces)
	}
	if calls := sink.traces[0].Steps[0].ToolCalls; len(calls) != 1 || calls[0].CallID != "call-new" {
		t.Fatalf("tool calls = %#v, want only the accepted attempt's call", calls)
	}
}

// Once a tool event opened the trace, another attempt's events are foreign and a repeated
// call ID is the same call: neither adds a tool.
func TestReasoningTraceBuilderRejectsForeignRunsAndRepeatedCalls(t *testing.T) {
	var b ReasoningTraceBuilder
	runID, foreign := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	end := func(run uuid.UUID, at time.Time, callID string) *agent.Event {
		return reasoningGraphToolEvents(run, at, callID, "task", `{"action":"create"}`, "ok", "scheduled", nil)[1]
	}

	b.ObserveToolInvocation(end(runID, t0, "call-1"))
	b.ObserveToolInvocation(end(foreign, t0.Add(time.Second), "call-2"))
	b.ObserveToolInvocation(end(runID, t0.Add(2*time.Second), "call-1"))
	b.ObserveReasoning(reasoningGraphEvent(foreign, t0.Add(3*time.Second), "foreign reasoning"))

	trace, ok := b.CommitSourceTurn(uuid.NewString(), uuid.NewString(), 2, t0.Add(4*time.Second))
	if !ok {
		t.Fatal("the tool-only trace was not committed")
	}
	var calls []arcadedb.ReasoningToolCall
	for _, step := range trace.Steps {
		calls = append(calls, step.ToolCalls...)
	}
	if trace.TraceID != runID.String() || trace.ProviderSummary != "" || len(calls) != 1 || calls[0].CallID != "call-1" {
		t.Fatalf("trace = %#v, want run %s with call-1 once and no foreign text", trace, runID)
	}
}
```

- [ ] **Step 2: Run them and see them fail**

Run: `W 'go test -race -count=1 -run "TestReasoningGraphRecords|TestReasoningGraphToolOpens|TestReasoningGraphDiscardDropsAToolOnly|TestReasoningTraceBuilderRejects" ./internal/runner/'`
Expected: FAIL. The tool-only tests report `traces = [], want ...`; `TestReasoningTraceBuilderRejectsForeignRunsAndRepeatedCalls` reports `the tool-only trace was not committed`.

- [ ] **Step 3: Write the failing validation tests**

In `internal/arcadedb/memory_reasoning_validate_test.go`, add three cases to the `cases` map of `TestNormalizeReasoningTraceRejectsAMalformedTrace`:

```go
		"summary-less step without tool calls": {
			func(trace *ReasoningTrace) {
				trace.Steps[0].ProviderSummary = ""
				trace.Steps[0].ToolCalls = nil
			},
			"step provider_summary must be non-empty",
		},
		"summary-less trace without tool calls": {
			func(trace *ReasoningTrace) {
				trace.ProviderSummary = ""
				trace.Steps[0].ToolCalls = nil
			},
			"provider_summary must be non-empty",
		},
		"tool-only trace with an unsupported tool status": {
			func(trace *ReasoningTrace) {
				trace.ProviderSummary = ""
				trace.Steps[0].ProviderSummary = ""
				trace.Steps[0].ToolCalls[0].Status = "maybe"
			},
			"unsupported reasoning tool status",
		},
```

and append this test to the same file:

```go
// A turn that exposed no reasoning but ran tools is stored with empty summaries; nothing
// is synthesized in their place.
func TestNormalizeReasoningTraceAcceptsATurnThatOnlyRanTools(t *testing.T) {
	t.Parallel()
	trace := validReasoningTrace()
	trace.ProviderSummary = ""
	trace.Steps[0].ProviderSummary = ""

	normalized, err := normalizeReasoningTrace(trace)
	if err != nil {
		t.Fatalf("normalizeReasoningTrace refused a tool-only trace: %v", err)
	}
	if normalized.ProviderSummary != "" || normalized.Steps[0].ProviderSummary != "" {
		t.Fatalf("summaries = %q / %q, want both empty", normalized.ProviderSummary, normalized.Steps[0].ProviderSummary)
	}
	if len(normalized.Steps[0].ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", normalized.Steps[0].ToolCalls)
	}
}
```

- [ ] **Step 4: Run them and see them fail**

Run: `W 'go test -race -count=1 -run "TestNormalizeReasoningTrace" ./internal/arcadedb/'`
Expected: FAIL. `TestNormalizeReasoningTraceAcceptsATurnThatOnlyRanTools` reports `refused a tool-only trace: arcadedb: reasoning provider_summary must be non-empty and canonical`, and the new unsupported-status case fails on the summary first, so its error does not name the status. The other two new cases already pass, because today every empty summary is refused.

- [ ] **Step 5: Implement the builder change**

In `internal/runner/runner_reasoning_graph.go`:

1. Add `start` after `Reset`:

```go
// start opens the trace on an attempt's first observed event, reasoning or tool, so a turn
// that exposed no reasoning still records what it ran.
func (b *ReasoningTraceBuilder) start(ev *agent.Event) {
	if b.runID != uuid.Nil {
		return
	}
	b.runID = ev.RequestID
	b.createdAt = ev.Timestamp.UTC()
	if b.createdAt.IsZero() {
		b.createdAt = time.Now().UTC()
	}
}
```

2. In `ObserveReasoning`, replace the block

```go
	if b.runID == uuid.Nil {
		b.runID = ev.RequestID
		b.createdAt = ev.Timestamp.UTC()
		if b.createdAt.IsZero() {
			b.createdAt = time.Now().UTC()
		}
	}
```

with `b.start(ev)`.

3. Replace `ObserveToolInvocation` (comment and body) with:

```go
// ObserveToolInvocation joins one structured runtime tool event to the active trace. The
// attempt's first valid tool event opens the trace when no reasoning did: measured
// 2026-10-06, the graph reached 9 of the lab VM's 16 tool turns, because a turn with no
// exposed reasoning had nothing to open it. Once open, another attempt's events are foreign.
func (b *ReasoningTraceBuilder) ObserveToolInvocation(ev *agent.Event) {
	if ev == nil || ev.RequestID == uuid.Nil || ev.Actions.ToolInvocation == nil ||
		ev.Actions.ToolInvocation.Event != agent.ToolInvocationEnd {
		return
	}
	if b.runID != uuid.Nil && ev.RequestID != b.runID {
		return
	}
	ti := ev.Actions.ToolInvocation
	policy := lookupReasoningToolPolicy(ti.ToolName)
	status, ok := reasoningToolStatus(ti.Status)
	if !ok || strings.TrimSpace(ti.ToolName) == "" || strings.TrimSpace(ti.ToolCallID) == "" {
		return
	}
	if _, duplicate := b.seenCalls[ti.ToolCallID]; duplicate {
		return
	}
	b.start(ev)
	if len(b.steps) == 0 {
		b.steps = append(b.steps, reasoningStepBuilder{createdAt: b.createdAt})
	}
	step := &b.steps[len(b.steps)-1]
	if len(step.tools) == reasoningGraphMaxToolsPerStep {
		return
	}
	tool := arcadedb.ReasoningToolCall{
		CallID: strings.TrimSpace(ti.ToolCallID), ToolName: ti.ToolName,
		Status: status, DurationMillis: max(ti.DurationMS, 0),
		ArgumentDigest: reasoningArgumentDigest(ti.Arguments),
	}
	// A failure is recorded for every tool, opted in or not: "it failed" without
	// the reason is the one fact that cannot be acted on.
	if policy.observation || status != "succeeded" {
		tool.Observation = reasoningObservation(ti.ResultPreview)
	}
	if policy.artifact && status == "succeeded" {
		tool.ArtifactRefs = reasoningArtifactRefs(ti.Meta)
	}
	if status == "succeeded" {
		tool.EntityRefs = reasoningEntityRefs(ti.Arguments, policy.entityArgFields)
	}
	step.tools = append(step.tools, tool)
	if b.seenCalls == nil {
		b.seenCalls = make(map[string]struct{})
	}
	b.seenCalls[ti.ToolCallID] = struct{}{}
	b.afterTool = true
}
```

4. In `CommitSourceTurn`, update the doc comment and drop the summary requirement:

```go
// CommitSourceTurn finalizes one successful trace against an already-committed
// authoritative assistant turn. A step needs reasoning or a tool call; a trace needs one
// such step.
func (b *ReasoningTraceBuilder) CommitSourceTurn(
	identityID, conversationID string,
	turnSeq int,
	terminalAt time.Time,
) (arcadedb.ReasoningTrace, bool) {
	summary := strings.TrimSpace(b.summary.String())
	if b.runID == uuid.Nil || strings.TrimSpace(identityID) == "" ||
		strings.TrimSpace(conversationID) == "" || turnSeq <= 0 {
		return arcadedb.ReasoningTrace{}, false
	}
```

and in its loop replace

```go
		stepSummary := strings.TrimSpace(pending.summary.String())
		if stepSummary == "" {
			continue
		}
```

with

```go
		stepSummary := strings.TrimSpace(pending.summary.String())
		if stepSummary == "" && len(pending.tools) == 0 {
			continue
		}
```

The existing `if len(steps) == 0 { return ..., false }` stays.

- [ ] **Step 6: Implement the validation change**

In `internal/arcadedb/memory_reasoning_validate.go`:

1. In `normalizeReasoningTrace`, replace

```go
	summary, err := normalizeReasoningEvidence("provider_summary", trace.ProviderSummary, reasoningSummaryRunes)
```

with

```go
	summary, err := normalizeReasoningSummary("provider_summary", trace.ProviderSummary, traceHasToolCalls(trace))
```

and replace

```go
		step.ProviderSummary, err = normalizeReasoningEvidence(
			"step provider_summary", step.ProviderSummary, reasoningSummaryRunes)
```

with

```go
		step.ProviderSummary, err = normalizeReasoningSummary(
			"step provider_summary", step.ProviderSummary, len(step.ToolCalls) > 0)
```

2. Add after `normalizeReasoningTrace`:

```go
// normalizeReasoningSummary normalizes a provider summary. An empty one is accepted only
// where tool calls stand in for it: a turn that exposed no reasoning still records what it
// ran, and no reasoning text is synthesized for it. validateReasoningText stays strict for
// everything else.
func normalizeReasoningSummary(name, value string, hasToolCalls bool) (string, error) {
	if value == "" && hasToolCalls {
		return "", nil
	}
	return normalizeReasoningEvidence(name, value, reasoningSummaryRunes)
}

func traceHasToolCalls(trace ReasoningTrace) bool {
	return slices.ContainsFunc(trace.Steps, func(step ReasoningStep) bool { return len(step.ToolCalls) > 0 })
}
```

3. Add `"slices"` to the imports.

- [ ] **Step 7: Run the unit tests and see them pass**

Run: `W 'go vet ./internal/runner/ ./internal/arcadedb/ && go test -race -count=1 ./internal/runner/ ./internal/arcadedb/'`
Expected: PASS, including the existing `TestReasoningGraphTracer` subtests "hidden and synthetic sources have no producer" (no tools, so still no trace) and `TestReasoningGraphRetryDiscard`. If an existing test asserted that a turn with tools and no reasoning produces no trace, that assertion encoded the defect: change it, and say why in the commit body.

- [ ] **Step 8: Write the live test**

Append to `internal/arcadedb/memory_reasoning_live_test.go`:

```go
// A tool-only trace must survive the real engine: provider_summary is MANDATORY in the
// schema, and "" has to count as present. The embedding pass then sets the empty summary
// aside once, with a space stamp and no vector, instead of retrying it on every run.
func TestReasoningGraphLive_ToolOnlyTraceIsStoredAndSetAsideOnce(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	route := constantEmbedder{value: 1, space: "es1-tool-only"}
	trace := freshReasoningTrace()
	trace.TraceID = "trace-tool-only"
	trace.ProviderSummary = ""
	trace.Steps[0].ProviderSummary = ""
	if err := client.WithEmbedder(route).UpsertReasoningTrace(ctx, trace); err != nil {
		t.Fatalf("UpsertReasoningTrace(tool-only): %v", err)
	}

	rows, err := client.Query(ctx,
		"SELECT provider_summary, out('HAS_STEP').out('INVOKED').tool_name AS tools FROM ReasoningTrace WHERE trace_id = :trace_id",
		map[string]any{"trace_id": trace.TraceID})
	if err != nil {
		t.Fatalf("read back the trace: %v", err)
	}
	if len(rows) != 1 || rowString(rows[0], "provider_summary") != "" {
		t.Fatalf("stored trace = %#v, want one row with an empty summary", rows)
	}
	if tools := rowStrings(rows[0], "tools"); len(tools) != 1 || tools[0] != "shell_exec" {
		t.Fatalf("stored tools = %#v, want the one call", rows[0]["tools"])
	}

	first, err := client.WithEmbedder(route).reembedMemory(ctx)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := client.WithEmbedder(route).reembedMemory(ctx)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if first.embedded != 0 || first.refused != 1 || second.refused != 0 {
		t.Fatalf("passes = %+v then %+v, want the empty trace set aside once", first, second)
	}
}
```

`validReasoningTrace`'s one tool is `shell_exec`, succeeded. If `rowStrings` returns nested lists for the traversal, flatten them in the test, not in production code: production code that reads this traversal arrives in plan 2.

- [ ] **Step 9: Run the live test**

Run recipe **A** with `-run TestReasoningGraphLive_ToolOnlyTraceIsStoredAndSetAsideOnce`.
Expected: PASS. If the engine refuses `""` for the MANDATORY property, stop: the spec's "stored as an empty summary" rests on that, and the operator decides how to change it.

- [ ] **Step 10: Commit**

Message:

```
fix(memory): record the tool calls of turns with no reasoning

The reasoning graph reached 9 of the lab VM's 16 tool turns on 2026-10-06.
ObserveToolInvocation dropped a tool event until reasoning had opened the
trace and a step, CommitSourceTurn dropped steps without a summary, and
validation refused an empty summary, so a turn that ran at effort none
and scheduled a reminder left no tool call behind. Turn recall reads
those calls to preload tools, and a failed call is the fact the next turn
learns from.

The first valid tool event now opens the trace; foreign attempts, repeated
call IDs and discarded attempts are still dropped. An empty summary is
accepted only on a step or trace that carries tool calls, and no reasoning
text is synthesized.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Paths: `internal/runner/runner_reasoning_graph.go internal/runner/runner_reasoning_graph_tools_test.go internal/arcadedb/memory_reasoning_validate.go internal/arcadedb/memory_reasoning_validate_test.go internal/arcadedb/memory_reasoning_live_test.go` (`git add` the new test file first).

---

### Task 2: Enforce ArcadeDB 26.10.1 in `TenantClients.For`

**Files:**
- Modify: `internal/arcadedb/admin.go` (`minSecureVersion`, `VerifySecureVersion`'s message)
- Modify: `internal/arcadedb/tenant_clients.go` (`TenantClients.verified`, new `verifyServer`, call in `For`)
- Create: `internal/arcadedb/tenant_version_live_test.go`
- Modify: `internal/arcadedb/tenant_clients_test.go` (GET route in the recorder, two tests)
- Modify: `internal/arcadedb/version_test.go`, `internal/arcadedb/client_admin_test.go`
- Modify: `CLAUDE.md` (the ArcadeDB line under Persistence)

**Interfaces:**
- Consumes: `Client.VerifySecureVersion(ctx) error`, `Client.ServerVersion`, `parseVersion`, `DatabaseFor`, `TenantUserFor`, `TenantCredentials{secret}`, test helpers `newTenantHTTPRecorder`, `resolverCredentials`, `resolverIdentity`, `envOr`.
- Produces: `TenantClients.For` returns an error naming `26.10.1` when the server is older; plan 2's `RecallTurns` relies on that floor (ArcadeData/arcadedb#8959).

- [ ] **Step 1: Measure whether a tenant credential can read the server version**

The floor is checked through the admin client when one exists, and through the tenant client otherwise. `cmd/arcadedb-mcp` can run with no admin. Nothing in ArcadeDB's documentation says whether a non-root user may call `GET /api/v1/server?mode=basic`, so measure it before wiring anything.

Create `internal/arcadedb/tenant_version_live_test.go`:

```go
//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The version floor runs on the tenant client when no admin is configured (cmd/arcadedb-mcp
// can run that way), so a tenant's own credential must be allowed to read the server
// version. ArcadeDB's documentation does not say either way for a non-root user; this
// measures it on the pinned engine.
func TestTenantCredentialReadsTheServerVersionLive(t *testing.T) {
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	ctx := context.Background()
	admin, err := New(Config{
		BaseURL: base, Database: "unused", User: envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	credentials := &TenantCredentials{secret: []byte(strings.Repeat("v", 32))}
	identity := uuid.NewString()
	database, err := DatabaseFor(identity)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.DropDatabase(context.Background(), database)
		_ = admin.DropUser(context.Background(), TenantUserFor(database))
	})

	tenant, err := NewTenantClients(Config{BaseURL: base}, admin, nil, credentials).For(ctx, identity)
	if err != nil {
		t.Fatalf("provision the tenant: %v", err)
	}
	if err := tenant.VerifySecureVersion(ctx); err != nil {
		t.Fatalf("a tenant credential cannot read the server version: %v", err)
	}
	if _, err := NewTenantClients(Config{BaseURL: base}, nil, nil, credentials).For(ctx, identity); err != nil {
		t.Fatalf("a resolver without an admin refused the provisioned tenant: %v", err)
	}
}
```

Run recipe **A** with `-run TestTenantCredentialReadsTheServerVersionLive`.
Expected: PASS against 26.10.1 with the floor still at 26.4.2. If `VerifySecureVersion` fails with 401 or 403, **stop and report the response to the operator**. Do not skip the check when there is no admin, and do not weaken it.

- [ ] **Step 2: Write the failing unit tests**

In `internal/arcadedb/tenant_clients_test.go`:

1. Add two fields to `tenantHTTPRecorder`:

```go
	version      string // what GET /api/v1/server answers; "" answers the floor itself
	versionCalls int
```

2. In `serveHTTP`, right before `if request.URL.Path != "/api/v1/server" {`, add:

```go
	if request.URL.Path == "/api/v1/server" && request.Method == http.MethodGet {
		r.mu.Lock()
		r.versionCalls++
		version := r.version
		r.mu.Unlock()
		if version == "" {
			version = "26.10.1 (build test)"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
		return
	}
```

3. Add a reader next to `counts`:

```go
func (r *tenantHTTPRecorder) versionReads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.versionCalls
}
```

4. Append two tests:

```go
// A server below the floor is refused before anything is provisioned on it, and the refusal
// is not cached: an upgraded server is accepted without a restart.
func TestTenantClientsRefuseAServerBelowTheFloor(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	recorder.version = "26.9.1"
	admin, err := New(recorder.config("admin", "root", "root-password"))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	resolver := NewTenantClients(recorder.config("template", "shared", "shared-password"), admin, nil, resolverCredentials())

	for range 2 {
		if _, err := resolver.For(t.Context(), resolverIdentity); err == nil || !strings.Contains(err.Error(), "26.10.1") {
			t.Fatalf("For on 26.9.1 = %v, want the version floor refusal", err)
		}
	}
	if databases, users := recorder.counts(); databases != 0 || users != 0 {
		t.Fatalf("a refused server got %d databases and %d users", databases, users)
	}
	if reads := recorder.versionReads(); reads != 2 {
		t.Fatalf("version reads = %d, want the refusal re-checked on every call", reads)
	}

	recorder.mu.Lock()
	recorder.version = "26.10.1"
	recorder.mu.Unlock()
	if _, err := resolver.For(t.Context(), resolverIdentity); err != nil {
		t.Fatalf("For after the upgrade: %v", err)
	}
}

// The floor is read once per resolver, not once per identity or per call.
func TestTenantClientsVerifyTheServerOnce(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	admin, err := New(recorder.config("admin", "root", "root-password"))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	resolver := NewTenantClients(recorder.config("template", "shared", "shared-password"), admin, nil, resolverCredentials())
	for _, identity := range []string{resolverIdentity, "10000000-0000-0000-0000-000000000002", resolverIdentity} {
		if _, err := resolver.For(t.Context(), identity); err != nil {
			t.Fatalf("For(%s): %v", identity, err)
		}
	}
	if reads := recorder.versionReads(); reads != 1 {
		t.Fatalf("version reads = %d, want one per resolver", reads)
	}
}
```

In `internal/arcadedb/version_test.go`, replace `TestVersionComparisonAtTheCVEBoundary` (comment included) with:

```go
// The floor decides whether per-identity memory is safe to run at all, so its boundary is
// pinned exactly: 26.4.2 closed CVE-2026-44221, and 26.10.1 stopped treating a vector
// filter that matches nothing as no filter (ArcadeData/arcadedb#8959), which turn recall's
// space and label filters rely on.
func TestVersionComparisonAtTheFloor(t *testing.T) {
	t.Parallel()
	below := []string{"26.10.0", "26.9.1", "26.4.2", "26.4.1", "25.12.0", "1.0.0"}
	atOrAbove := []string{"26.10.1", "26.10.2", "26.11.0", "27.0.0"}
	for _, raw := range below {
		v, err := parseVersion(raw)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", raw, err)
		}
		if !v.Less(minSecureVersion) {
			t.Errorf("%s is below the floor but was accepted", raw)
		}
	}
	for _, raw := range atOrAbove {
		v, err := parseVersion(raw)
		if err != nil {
			t.Fatalf("parseVersion(%q): %v", raw, err)
		}
		if v.Less(minSecureVersion) {
			t.Errorf("%s meets the floor but was refused", raw)
		}
	}
}
```

In `internal/arcadedb/client_admin_test.go`, `TestServerVersionAndSecurityBoundary`, change the `patched` row's body to `{"version":"26.10.1 (build x)"}` and add a row after it:

```go
		{name: "below the floor", status: http.StatusOK, body: `{"version":"26.9.1"}`, wantErr: true},
```

- [ ] **Step 3: Run them and see them fail**

Run: `W 'go test -race -count=1 -run "TestTenantClients|TestVersionComparisonAtTheFloor|TestServerVersionAndSecurityBoundary" ./internal/arcadedb/'`
Expected: FAIL. `TestTenantClientsRefuseAServerBelowTheFloor` gets a nil error, `TestTenantClientsVerifyTheServerOnce` sees 0 reads, `TestVersionComparisonAtTheFloor` accepts `26.10.0` and others, and the `below the floor` row returns no error.

- [ ] **Step 4: Raise the floor**

In `internal/arcadedb/admin.go`, extend the `minSecureVersion` comment and value:

```go
// minSecureVersion is the oldest ArcadeDB release Aura's memory is safe on.
//
// CVE-2026-44221 (CVSS 9.0, fixed in 26.4.2) was two defects: an uninitialized
// fileAccessMap treated as permissive, and — the one that matters here —
// ArcadeDBServer.createDatabase() omitting the security factory, "disabling
// record-level authorization for newly created databases". Aura's memory creates
// a database per identity AT RUNTIME and rests its entire isolation on the server
// refusing a credential scoped elsewhere. On an affected version it would not
// refuse, and nothing would say so.
//
// 26.10.1 raised the floor on 2026-10-06. Before it, a vector.neighbors filter that
// matched nothing was treated as no filter (ArcadeData/arcadedb#8959), so turn recall
// would rank another embedding space's vectors, or unlabelled turns, the moment an
// identity had none matching. It also closes GHSA-h2j4-28h8-cj5v.
//
// So the pin in compose.yaml is not enough: a downgrade must be a refusal, not a
// silent loss of isolation.
var minSecureVersion = [3]int{26, 10, 1}
```

and in `VerifySecureVersion` replace the refusal with:

```go
	if version.Less(minSecureVersion) {
		return fmt.Errorf(
			"arcadedb %s is older than %d.%d.%d, the oldest release Aura's memory is safe on: "+
				"before 26.4.2 a database created at runtime is left unauthorized (CVE-2026-44221), "+
				"and before 26.10.1 a vector filter matching nothing ranks every vector (#8959)",
			raw, minSecureVersion[0], minSecureVersion[1], minSecureVersion[2])
	}
```

Update `VerifySecureVersion`'s doc comment to: `// VerifySecureVersion refuses a server older than minSecureVersion.`

- [ ] **Step 5: Wire the floor into `For`**

In `internal/arcadedb/tenant_clients.go`:

1. Add a field to `TenantClients`, after `inflight`:

```go
	verified bool // the server cleared minSecureVersion; a refusal is re-checked on the next call
```

2. In `For`, right after

```go
	client, err := t.client(database)
	if err != nil {
		return nil, fmt.Errorf("memory for %s: %w", identityID, err)
	}
```

insert

```go
	if err := t.verifyServer(ctx, client); err != nil {
		return nil, fmt.Errorf("memory for %s: %w", identityID, err)
	}
```

3. Add after `For`:

```go
// verifyServer refuses an ArcadeDB older than minSecureVersion before the first tenant
// client is handed out, and remembers a pass. It runs here rather than at boot so that a
// slow ArcadeDB start is a retried read, not a failed boot. The admin reads the version
// when there is one; otherwise the tenant's own credential does, which
// TestTenantCredentialReadsTheServerVersionLive measured to be allowed.
func (t *TenantClients) verifyServer(ctx context.Context, tenant *Client) error {
	t.mu.Lock()
	verified := t.verified
	t.mu.Unlock()
	if verified {
		return nil
	}
	server := tenant
	if t.admin != nil {
		server = t.admin
	}
	if err := server.VerifySecureVersion(ctx); err != nil {
		return err
	}
	t.mu.Lock()
	t.verified = true
	t.mu.Unlock()
	return nil
}
```

4. Update `For`'s doc comment: `// For returns the client for one identity, provisioning its database, scoped credential, and memory schema on the first successful call. The first call of a resolver also refuses a server older than minSecureVersion.`

- [ ] **Step 6: Run the tests and see them pass**

Run: `W 'go vet ./internal/arcadedb/ && go test -race -count=1 ./internal/arcadedb/ ./cmd/arcadedb-mcp/ ./cmd/aura/'`
Expected: PASS. If a fake ArcadeDB server in another test now answers `GET /api/v1/server` with 404 or a decode error, give that fake the same route as Step 2 (`{"version":"26.10.1"}`). That fixes the test's model of the server; it does not change what the test asserts. Name each such file in the commit body.

Then run recipe **A** with `-run "TestTenantCredentialReadsTheServerVersionLive|TestReasoningGraphLive_ToolOnlyTraceIsStoredAndSetAsideOnce|TestConversationProjectionLive"`.
Expected: PASS against the local 26.10.1.

- [ ] **Step 7: Update CLAUDE.md**

In `CLAUDE.md`, under `## Persistence`, replace the sentence

`Richiede ≥ 26.4.2 (CVE-2026-44221): `VerifySecureVersion` rifiuta di partire sotto quella soglia.`

with

`Richiede ≥ 26.10.1: sotto 26.4.2 un database creato a runtime resta senza autorizzazione (CVE-2026-44221), e sotto 26.10.1 un filtro di `vector.neighbors` che non trova niente vale come nessun filtro (#8959). `TenantClients.For` lo verifica una volta per resolver con `VerifySecureVersion`, prima di consegnare il primo client tenant.`

- [ ] **Step 8: Commit**

Message:

```
fix(arcadedb): enforce the 26.10.1 floor before the first tenant client

VerifySecureVersion had no production caller, so the floor CLAUDE.md
described was enforced nowhere (found 2026-10-06). TenantClients.For now
reads the server version once per resolver, through the admin when there
is one and through the tenant's own credential otherwise, which a live
test measured to be allowed. A refusal is not cached: an upgraded server
is accepted without a restart.

The floor rises from 26.4.2 to 26.10.1. Before it, a vector.neighbors
filter that matched nothing was no filter (ArcadeData/arcadedb#8959), and
turn recall's space and label filters depend on that. 26.10.1 also closes
GHSA-h2j4-28h8-cj5v. The compose pin already ships it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Paths: `internal/arcadedb/admin.go internal/arcadedb/tenant_clients.go internal/arcadedb/tenant_version_live_test.go internal/arcadedb/tenant_clients_test.go internal/arcadedb/version_test.go internal/arcadedb/client_admin_test.go CLAUDE.md`, plus any fake server fixed in Step 6 (`git add` the new test file first).

---

### Task 3: Ship, verify on the lab VM, record in the PRD

**Files:**
- Modify: `prd.md` §10, after the measurement only. Another session has uncommitted edits in `prd.md`: before touching it, run `git diff -- prd.md` and stop to ask the operator if those edits are still uncommitted, rather than committing them along with yours.

- [ ] **Step 1: Run the local gates**

Run: `W 'make quality'`
Expected: exit 0 (vet, file-size, lint and dupl, deadcode, test-race, vuln, build).

Run: `W 'bash scripts/coverage_docker.sh'`
Expected: the aggregate owned-surface coverage is at least 85% and the package policy holds. `internal/arcadedb` delegates to its live profile, which the `agent-memory-eval` job measures. If Docker is not reachable from WSL, CI is the authority: say so and continue.

- [ ] **Step 2: Push and wait for every job on this SHA**

Run: `W 'LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master'`
Then: `gh run list --branch master --limit 10 --json databaseId,headSha,name,status,conclusion`, and `gh run watch <id>` for each run on the pushed SHA.
Expected: every job green on this SHA, including `agent-memory-eval`, which runs `./internal/arcadedb/` with `arcadedb_integration`, and "Publish Aura edge image". A red job is fixed before anything else, whoever's it looks like.

- [ ] **Step 3: Let the updater roll the VM, then check it**

Never update the VM by hand. The updater applies a new aura image after about 15 minutes without chat activity. Then, read-only over SSH (lab VM `192.168.101.158`, user `aura`; scripts read the password from an untracked file, never inline):
- `sudo docker inspect aura --format '{{.Image}}'` matches the digest the edge publish job pushed;
- `sudo docker logs aura --since 30m 2>&1 | grep -c 'older than 26.10.1'` prints `0`;
- ArcadeDB answers `26.10.1` on `GET /api/v1/server?mode=basic` (root password from the container's `JAVA_OPTS`, curl against the container IP from the host);
- `sudo docker logs aura --since 30m 2>&1 | grep -i 'conversation projection\|reasoning graph delivery failed'` shows no new failure.

- [ ] **Step 4: Run tool turns on the VM and measure coverage**

Drive two turns with the operator's own account, through the cockpit API (`scripts/musr_live_run_authula_helpers.sh`: `fetch_auth_config`, `sign_in` with `VM_COCKPIT_EMAIL`/`VM_COCKPIT_PASSWORD` from `D:\Aura\.env.google`, never printed; then `POST /api/conversations` and `POST /agent/run`). Each turn asks for a reminder a few minutes out, so it runs `task`. Use fresh wording that appears in no corpus.

For each turn, read the user turn's persisted `reasoning` column in `aura.conversation_turns`. If it is null, the turn exposed no reasoning and exercises this fix.

Then measure, on the identity's memory database, every user turn since the rollout: how many invoked a tool according to `aura.tool_invocations`, and how many reach a tool name in the graph. The graph query is:

```sql
SELECT turn_seq, out('NEXT_TURN').in('INITIATED_BY').out('HAS_STEP').out('INVOKED').tool_name AS tools
FROM ConversationTurn WHERE role = 'user' AND occurred_at > :rollout ORDER BY occurred_at
```

The Postgres side is `select count(distinct request_id) from aura.tool_invocations where event_kind = 'end' and ts > :rollout`.

Expected: every tool turn since the rollout reaches its tools in the graph. If no turn ran without exposed reasoning, the live path of the fix was not exercised: report that, with the live ArcadeDB test as the evidence that does exist, and ask the operator whether to repeat with reasoning display off for one turn. Do not change a VM setting without that answer.

Score the E2E (Definition of Done: above 9.8) on these checks: the image and floor are live, the projection is healthy, every new tool turn is in the graph, and at least one turn with no exposed reasoning is in it.

- [ ] **Step 5: Amend the PRD with what was measured**

In `prd.md` §10 ("Memory authority, capture and reasoning"), add a paragraph in the style of the 2026-10-06 entries. It says:
- what changed: the first valid tool event opens a trace; an empty summary is accepted only with tool calls;
- the measured coverage: 9 of 16 before, N of N after, with the date and the VM;
- the version floor, now 26.10.1 and enforced in `TenantClients.For`;
- "What this does not show": the number of turns measured and the route they ran on.

Commit only this paragraph:

```
docs(prd): record tool-only traces and the 26.10.1 floor on the VM

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Use `git add -p prd.md` for your own hunk alone if the other session's edits are still there, after the operator has answered Task 3's question about them.

- [ ] **Step 6: Push the PRD commit and confirm CI**

Push as in Step 2. A docs-only change needs the same green run on its SHA before the plan is closed.
