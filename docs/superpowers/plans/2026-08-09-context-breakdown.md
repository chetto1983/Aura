# Context Breakdown (Part A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura report what its context window is actually made of, per LLM call, so the 7k–75k prefix swing measured on 2026-08-09 stops being invisible.

**Architecture:** A pure categorizer in `internal/agent/prompt` computes token counts per category from the same values `PromptBuilder.buildBase` uses to build the wire request — so the breakdown cannot drift from what is sent. It reports through an injected callback; the agent wires that callback to an OTel gauge. Separately, per-call `llm.Usage` is persisted for every LLM call, not only the terminal one, so the breakdown has provider ground truth to reconcile against.

**Tech Stack:** Go 1.25+, `github.com/pkoukk/tiktoken-go` (vendored cl100k_base, already a dependency), OpenTelemetry metrics via `internal/obs`, Postgres via sqlc.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-09-history-token-budget-design.md` §4.
- **No file over 600 LOC.** `internal/agent/prompt/builder.go` is 159 LOC and must stay small; new logic goes in its own file.
- **Metric vocabulary is enforced in code.** `internal/obs/catalog.go` gates attribute keys via `allowedAttributeValues` and `AllAttributeKeys()`. An unregistered key or value is silently dropped — Task 3 exists entirely to prevent that.
- **`messages[0]` must stay byte-identical** (CAP-04 cache invariant). The breakdown is read-only over the request; it must never mutate `req`.
- **Do not rebuild the LLM wire boundary.** `internal/llm/openai_compat/usage.go` already parses `cost`, `cached_tokens` and `cache_write_tokens` correctly, and `internal/llm/prices.go:38-39` already prefers the provider's reported cost (D-18). This plan only changes *persistence*.
- **Reasoning is excluded from context by design** (`store_append.go:46`, `store_reasoning.go:14`). There is no reasoning category.
- Post-edit validation after every Go file edit: `go vet ./...`, `go build ./...`, `go test ./internal/<package>/`, `go test -race ./internal/<package>/`.
- Coverage floor for touched packages: 85%.

---

### Task 1: Export a reusable token counter

`internal/conversations` owns a vendored, offline, `sync.Once`-cached cl100k_base encoder (`tiktoken.go`). `internal/agent/prompt` must not duplicate it, and must not import the whole conversations package either. Export a thin function; the composition root passes it as a value.

**Files:**
- Modify: `internal/conversations/tiktoken.go`
- Test: `internal/conversations/tiktoken_test.go`

**Interfaces:**
- Consumes: existing unexported `encoder()` and `countTokens(enc, text)` in the same file.
- Produces: `func conversations.CountTokens(text string) int` — returns 0 for empty input and 0 if the encoder failed to initialize.

- [ ] **Step 1: Write the failing test**

Add to `internal/conversations/tiktoken_test.go`:

```go
func TestCountTokensExported(t *testing.T) {
	if got := CountTokens(""); got != 0 {
		t.Fatalf("CountTokens(%q) = %d, want 0", "", got)
	}
	// "hello world" is two ordinary cl100k_base tokens.
	if got := CountTokens("hello world"); got != 2 {
		t.Fatalf("CountTokens(%q) = %d, want 2", "hello world", got)
	}
	// Counting is additive-ish and strictly grows with content.
	short := CountTokens("a")
	long := CountTokens("a much longer string with several words in it")
	if long <= short {
		t.Fatalf("CountTokens did not grow with content: short=%d long=%d", short, long)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversations/ -run TestCountTokensExported -v`
Expected: FAIL — `undefined: CountTokens`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/conversations/tiktoken.go`:

```go
// CountTokens is the exported cl100k_base count used by callers outside this
// package (the prompt-composition breakdown). It reuses the same vendored,
// once-initialized encoder as the L1/L2 ladder rather than standing up a second
// one, so a breakdown and a budget gate can never disagree about a string's size.
// A failed encoder init returns 0: the breakdown is observability, and must never
// be the thing that fails a turn.
func CountTokens(text string) int {
	enc, err := encoder()
	if err != nil {
		return 0
	}
	return countTokens(enc, text)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversations/ -run TestCountTokensExported -v`
Expected: PASS

Run: `go vet ./... && go build ./... && go test -race ./internal/conversations/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/conversations/tiktoken.go internal/conversations/tiktoken_test.go
git commit -m "Export the ladder's own token counter instead of standing up a second"
```

---

### Task 2: The pure breakdown categorizer

**Files:**
- Create: `internal/agent/prompt/context_breakdown.go`
- Test: `internal/agent/prompt/context_breakdown_test.go`

**Interfaces:**
- Consumes: `llm.Request` (`internal/llm/client.go:46` — `ToolDef{Type, Function{Name, Description, Parameters}}`), `llm.Message`, `Budget.block()` from `builder.go:74`.
- Produces:
  - `type Category string` with constants `CategorySystemPrompt`, `CategoryToolDefs`, `CategoryMCP`, `CategoryVolatileHints`, `CategoryConversation`
  - `type TokenCounter func(string) int`
  - `type Breakdown struct { Categories map[Category]int; Total int }`
  - `func ComputeBreakdown(req llm.Request, hints string, count TokenCounter) Breakdown`

MCP tools are identified by the `__` namespace separator that `DeferredRoster` documents (`internal/agent/tools/manifest.go:91-93`: `"<namespace>__<tool>"`).

- [ ] **Step 1: Write the failing test**

Create `internal/agent/prompt/context_breakdown_test.go`:

```go
package prompt

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// wordCount is a deterministic stand-in for a real tokenizer: one token per
// non-empty rune-run separated by spaces. Unit tests must not depend on
// cl100k_base's exact numbers, only on the categorizer's routing.
func wordCount(s string) int {
	n, inWord := 0, false
	for _, r := range s {
		if r == ' ' || r == '\n' {
			inWord = false
			continue
		}
		if !inWord {
			n++
			inWord = true
		}
	}
	return n
}

func toolDef(name, description string) llm.ToolDef {
	var d llm.ToolDef
	d.Type = "function"
	d.Function.Name = name
	d.Function.Description = description
	return d
}

func TestComputeBreakdownRoutesEachCategory(t *testing.T) {
	req := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "sys a b"},   // 3
			{Role: llm.RoleUser, Content: "user c d e"},  // 4
			{Role: llm.RoleAssistant, Content: "ok"},     // 1
		},
		Tools: []llm.ToolDef{
			toolDef("shell_exec", "run one two"),              // name 1 + desc 3 = 4
			toolDef("memory__memory_search", "find a b c"),    // name 1 + desc 4 = 5
		},
	}

	got := ComputeBreakdown(req, "hint one two", wordCount) // 3

	want := map[Category]int{
		CategorySystemPrompt:  3,
		CategoryConversation:  5,
		CategoryToolDefs:      4,
		CategoryMCP:           5,
		CategoryVolatileHints: 3,
	}
	for category, wantTokens := range want {
		if got.Categories[category] != wantTokens {
			t.Errorf("category %q = %d, want %d", category, got.Categories[category], wantTokens)
		}
	}
	if got.Total != 20 {
		t.Errorf("Total = %d, want 20", got.Total)
	}
}

func TestComputeBreakdownNoSystemMessage(t *testing.T) {
	req := llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "a b"}}}

	got := ComputeBreakdown(req, "", wordCount)

	if got.Categories[CategorySystemPrompt] != 0 {
		t.Errorf("system_prompt = %d, want 0 when messages[0] is not system",
			got.Categories[CategorySystemPrompt])
	}
	if got.Categories[CategoryConversation] != 2 {
		t.Errorf("conversation = %d, want 2", got.Categories[CategoryConversation])
	}
}

func TestComputeBreakdownNilCounterIsSafe(t *testing.T) {
	req := llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "a b"}}}

	got := ComputeBreakdown(req, "hint", nil)

	if got.Total != 0 {
		t.Errorf("Total = %d, want 0 with a nil counter", got.Total)
	}
}

func TestComputeBreakdownCountsToolParameters(t *testing.T) {
	def := toolDef("fs_read", "read")
	def.Function.Parameters = []byte(`{"a" "b"}`)

	got := ComputeBreakdown(llm.Request{Tools: []llm.ToolDef{def}}, "", wordCount)

	// name 1 + description 1 + parameters 2 = 4
	if got.Categories[CategoryToolDefs] != 4 {
		t.Errorf("tool_definitions = %d, want 4 (parameters must be counted)",
			got.Categories[CategoryToolDefs])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/prompt/ -run TestComputeBreakdown -v`
Expected: FAIL — `undefined: ComputeBreakdown`

- [ ] **Step 3: Write minimal implementation**

Create `internal/agent/prompt/context_breakdown.go`:

```go
package prompt

import (
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// Category is one bucket of the context-composition breakdown. The set is closed:
// it is projected onto a bounded metric attribute (internal/obs/catalog.go), where
// an unregistered value is dropped without error.
type Category string

// CategorySystemPrompt and its siblings are the whole vocabulary.
const (
	CategorySystemPrompt  Category = "system_prompt"
	CategoryToolDefs      Category = "tool_definitions"
	CategoryMCP           Category = "mcp"
	CategoryVolatileHints Category = "volatile_hints"
	CategoryConversation  Category = "conversation"
)

// AllCategories is the enumeration used by the metric registration and its test.
func AllCategories() []Category {
	return []Category{
		CategorySystemPrompt, CategoryToolDefs, CategoryMCP,
		CategoryVolatileHints, CategoryConversation,
	}
}

// TokenCounter returns the token count of a string. It is injected rather than
// imported so this package stays free of any tokenizer dependency and its tests
// stay independent of cl100k_base's exact numbers.
type TokenCounter func(string) int

// Breakdown is one request's composition. Total is the sum of Categories, not an
// independent measurement, so the two can never disagree.
type Breakdown struct {
	Categories map[Category]int
	Total      int
}

// mcpNameSeparator marks a namespaced tool ("<namespace>__<tool>"), the shape
// DeferredRoster documents in internal/agent/tools/manifest.go.
const mcpNameSeparator = "__"

// ComputeBreakdown attributes every token of a built request to exactly one
// category. It is READ-ONLY over req: messages[0] must stay byte-identical
// (CAP-04), so nothing here may mutate the request it measures.
//
// hints is the volatile trailing block (Budget.block()); it is passed separately
// because by the time it is a message it is indistinguishable from a user turn.
//
// A nil counter yields an empty breakdown rather than panicking: this is
// observability, and it must never be the reason a turn fails.
func ComputeBreakdown(req llm.Request, hints string, count TokenCounter) Breakdown {
	out := Breakdown{Categories: make(map[Category]int, len(AllCategories()))}
	if count == nil {
		return out
	}

	for i, msg := range req.Messages {
		category := CategoryConversation
		if i == 0 && msg.Role == llm.RoleSystem {
			category = CategorySystemPrompt
		}
		out.Categories[category] += count(msg.Content)
	}

	for _, def := range req.Tools {
		category := CategoryToolDefs
		if strings.Contains(def.Function.Name, mcpNameSeparator) {
			category = CategoryMCP
		}
		out.Categories[category] += count(def.Function.Name) +
			count(def.Function.Description) +
			count(string(def.Function.Parameters))
	}

	out.Categories[CategoryVolatileHints] += count(hints)

	for _, tokens := range out.Categories {
		out.Total += tokens
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/prompt/ -run TestComputeBreakdown -v`
Expected: PASS (4 tests)

Run: `go vet ./... && go build ./... && go test -race ./internal/agent/prompt/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/prompt/context_breakdown.go internal/agent/prompt/context_breakdown_test.go
git commit -m "Attribute every request token to a category, read-only over the request"
```

---

### Task 3: Register the metric vocabulary

`internal/obs/catalog.go` drops any attribute key or value it does not know, without erroring. This task registers the new dimension *and* writes the test that fails when a future category is added without registering it.

**Files:**
- Modify: `internal/obs/catalog.go`
- Test: `internal/obs/catalog_test.go`

**Interfaces:**
- Consumes: `prompt.AllCategories()` from Task 2.
- Produces: `obs.AttributeCategory AttributeKey = "category"`, `obs.AgentContextTokensID InstrumentID = "agent_context_tokens"`, and a `gauge` descriptor named `aura_agent_context_tokens`.

- [ ] **Step 1: Write the failing test**

Add to `internal/obs/catalog_test.go`:

```go
func TestContextTokensInstrumentRegistered(t *testing.T) {
	var found bool
	for _, d := range descriptors {
		if d.ID != AgentContextTokensID {
			continue
		}
		found = true
		if d.PrometheusName != "aura_agent_context_tokens" {
			t.Errorf("PrometheusName = %q, want aura_agent_context_tokens", d.PrometheusName)
		}
		if d.Kind != KindGauge {
			t.Errorf("Kind = %q, want %q", d.Kind, KindGauge)
		}
		if len(d.Attributes) != 1 || d.Attributes[0] != AttributeCategory {
			t.Errorf("Attributes = %v, want [%q]", d.Attributes, AttributeCategory)
		}
	}
	if !found {
		t.Fatal("AgentContextTokensID has no descriptor; the instrument would never exist")
	}
}

func TestCategoryAttributeIsBounded(t *testing.T) {
	if !IsBoundedAttribute(AttributeCategory) {
		t.Fatal("AttributeCategory is not in allowedAttributeValues; every value would be dropped")
	}
	var listed bool
	for _, key := range AllAttributeKeys() {
		if key == AttributeCategory {
			listed = true
		}
	}
	if !listed {
		t.Fatal("AttributeCategory missing from AllAttributeKeys(); the vocabulary gate would not see it")
	}
}
```

Add to `internal/agent/prompt/context_breakdown_test.go` — the guard that makes a future category impossible to add silently:

```go
func TestEveryCategoryIsARegisteredMetricValue(t *testing.T) {
	for _, category := range AllCategories() {
		if !obs.AllowedAttributeValue(obs.AttributeCategory, string(category)) {
			t.Errorf("category %q is not a registered metric value; it would be emitted and silently dropped", category)
		}
	}
}
```

with `"github.com/chetto1983/aura/internal/obs"` added to that test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/obs/ -run 'TestContextTokens|TestCategoryAttribute' -v`
Expected: FAIL — `undefined: AgentContextTokensID`

- [ ] **Step 3: Write minimal implementation**

In `internal/obs/catalog.go`, add to the attribute-key constant block (near `AttributeState`, line ~37):

```go
	AttributeCategory   AttributeKey = "category"
```

Add the instrument ID next to `AgentTurnsID` in the `InstrumentID` constant block:

```go
	AgentContextTokensID InstrumentID = "agent_context_tokens"
```

Add the descriptor to `descriptors`, after the `AgentTurnsID` line:

```go
	gauge(AgentContextTokensID, "aura.agent.context.tokens", "aura_agent_context_tokens", []AttributeKey{AttributeCategory}, "Tokens occupying the request context, by composition category.", "1"),
```

Register the bounded values in `allowedAttributeValues`:

```go
	AttributeCategory: finiteSet(
		"system_prompt", "tool_definitions", "mcp", "volatile_hints", "conversation", ValueOther,
	),
```

Extend `AllAttributeKeys()`:

```go
func AllAttributeKeys() []AttributeKey {
	return []AttributeKey{AttributeOperation, AttributeToolClass, AttributeTransport, AttributeOutcome, AttributeErrorClass, AttributeState, AttributeCategory}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/obs/ ./internal/agent/prompt/ -v`
Expected: PASS

Run: `go vet ./... && go build ./... && go test -race ./internal/obs/ ./internal/agent/prompt/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/obs/catalog.go internal/obs/catalog_test.go internal/agent/prompt/context_breakdown_test.go
git commit -m "Register the category dimension, and fail the build when a new one is not"
```

---

### Task 4: Report the breakdown from the builder chokepoint

**Files:**
- Modify: `internal/agent/prompt/builder.go:18-21,146-159`
- Modify: `internal/agent/metrics.go`
- Modify: `internal/agent/llm_agent_construct.go:44`
- Test: `internal/agent/prompt/builder_test.go`, `internal/agent/metrics_observability_test.go`

**Interfaces:**
- Consumes: `ComputeBreakdown` (Task 2), `obs.AgentContextTokensID` (Task 3), `conversations.CountTokens` (Task 1).
- Produces:
  - `func NewPromptBuilderWithObserver(count TokenCounter, report func(Breakdown)) *PromptBuilder`
  - `func recordContextBreakdown(b prompt.Breakdown)` in `internal/agent`

`NewPromptBuilder()` keeps its exact current behaviour so every existing call site and test is untouched.

- [ ] **Step 1: Write the failing test**

Add to `internal/agent/prompt/builder_test.go`:

```go
func TestBuilderReportsBreakdownOnEveryBuildPath(t *testing.T) {
	reg := tools.NewRegistry()
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "hi"},
	}

	var reports []Breakdown
	b := NewPromptBuilderWithObserver(
		func(s string) int { return len(s) },
		func(bd Breakdown) { reports = append(reports, bd) },
	)

	b.Build(history, reg, "openrouter", llm.Config{Model: "m"}, Budget{}, nil)
	b.BuildWithReasoningTier(history, reg, "openrouter", llm.Config{Model: "m"}, Budget{}, ReasoningTier{}, nil)
	b.BuildWithReasoningOverride(history, reg, "openrouter", llm.Config{Model: "m"}, Budget{}, "", nil)

	if len(reports) != 3 {
		t.Fatalf("got %d breakdown reports, want 3 (one per build path)", len(reports))
	}
	if reports[0].Categories[CategorySystemPrompt] != len("sys") {
		t.Errorf("system_prompt = %d, want %d", reports[0].Categories[CategorySystemPrompt], len("sys"))
	}
}

func TestNewPromptBuilderReportsNothing(t *testing.T) {
	reg := tools.NewRegistry()
	// The zero-observer builder must not panic and must stay byte-identical.
	req := NewPromptBuilder().Build(
		[]llm.Message{{Role: llm.RoleSystem, Content: "sys"}},
		reg, "openrouter", llm.Config{Model: "m"}, Budget{}, nil,
	)
	if len(req.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1", len(req.Messages))
	}
}

func TestBuilderDoesNotMutateHistory(t *testing.T) {
	reg := tools.NewRegistry()
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}}
	b := NewPromptBuilderWithObserver(func(s string) int { return len(s) }, func(Breakdown) {})

	b.Build(history, reg, "openrouter", llm.Config{Model: "m"}, Budget{Used: 1, Remaining: 2}, nil)

	if len(history) != 1 || history[0].Content != "sys" {
		t.Fatalf("Build mutated the caller's history: %+v", history)
	}
}
```

Check the existing imports at the top of `builder_test.go` and add any of `tools`, `llm` that are missing.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/prompt/ -run TestBuilder -v`
Expected: FAIL — `undefined: NewPromptBuilderWithObserver`

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/prompt/builder.go`, replace the struct and constructor (lines 18-21):

```go
type PromptBuilder struct {
	count  TokenCounter
	report func(Breakdown)
}

// NewPromptBuilder returns a stateless builder that reports no breakdown.
func NewPromptBuilder() *PromptBuilder { return &PromptBuilder{} }

// NewPromptBuilderWithObserver returns a builder that computes the composition
// breakdown of every request it builds and hands it to report. Both arguments
// must be non-nil to enable it; the breakdown is observability and never changes
// the request, so a nil pair simply disables the measurement.
func NewPromptBuilderWithObserver(count TokenCounter, report func(Breakdown)) *PromptBuilder {
	return &PromptBuilder{count: count, report: report}
}
```

In `buildBase` (line 146), report after the request is complete:

```go
func (b *PromptBuilder) buildBase(history []llm.Message, reg *tools.Registry, cfg llm.Config, budget Budget, activated map[string]struct{}) llm.Request {
	msgs := history
	hints := ""
	if budget.present() {
		hints = budget.block()
		msgs = append(append([]llm.Message(nil), history...), llm.Message{Role: llm.RoleUser, Content: hints})
	}
	req := llm.Request{
		Model:       cfg.Model,
		Messages:    msgs,
		Tools:       reg.RenderToolDefs(activated),
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	}
	// The hint message is already inside req.Messages, so it is passed separately
	// and excluded there — ComputeBreakdown must not count it twice.
	if b.count != nil && b.report != nil {
		b.report(ComputeBreakdown(llm.Request{Messages: history, Tools: req.Tools}, hints, b.count))
	}
	return req
}
```

In `internal/agent/metrics.go`, add the instrument to `agentMetrics` (next to `turnTotal`):

```go
	contextTokens metric.Int64Gauge
```

in both the struct and `newAgentMetrics`:

```go
		contextTokens: mustInt64Gauge(meter, obs.AgentContextTokensID),
```

and the recorder:

```go
// recordContextBreakdown publishes one gauge sample per composition category.
// Categories are the closed prompt.AllCategories() set, which
// TestEveryCategoryIsARegisteredMetricValue pins to the obs vocabulary.
func recordContextBreakdown(b prompt.Breakdown) {
	for category, tokens := range b.Categories {
		metrics.contextTokens.Record(context.Background(), int64(tokens),
			metric.WithAttributes(boundedAttr(obs.AttributeCategory, string(category))))
	}
}
```

If `mustInt64Gauge` does not already exist beside `mustInt64Counter` in that file, add it mirroring the existing helper exactly.

In `internal/agent/llm_agent_construct.go:44`, wire it:

```go
		builder:           prompt.NewPromptBuilderWithObserver(conversations.CountTokens, recordContextBreakdown),
```

adding `"github.com/chetto1983/aura/internal/conversations"` to that file's imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/prompt/ ./internal/agent/ -v -run 'Breakdown|Builder'`
Expected: PASS

Run: `go vet ./... && go build ./... && go test -race ./internal/agent/... ./internal/obs/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/prompt/builder.go internal/agent/prompt/builder_test.go internal/agent/metrics.go internal/agent/llm_agent_construct.go
git commit -m "Measure the request where it is built, so the number cannot drift from the wire"
```

---

### Task 5: Benchmark the measurement before trusting it in the hot path

The breakdown tokenizes the whole history on every LLM call. On the measured deployment history is ~11k tokens, but the Part B budget admits 500k. Measure the cost rather than assuming it.

**Files:**
- Test: `internal/agent/prompt/context_breakdown_bench_test.go`

**Interfaces:**
- Consumes: `ComputeBreakdown`, `conversations.CountTokens`.

- [ ] **Step 1: Write the benchmark**

Create `internal/agent/prompt/context_breakdown_bench_test.go`:

```go
package prompt_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func benchRequest(messages, wordsPerMessage int) llm.Request {
	body := strings.Repeat("lorem ipsum dolor sit amet ", wordsPerMessage/5)
	msgs := make([]llm.Message, 0, messages)
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: body})
	for i := 1; i < messages; i++ {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: body})
	}
	return llm.Request{Messages: msgs}
}

// BenchmarkComputeBreakdownMeasured is sized at the measured deployment:
// ~50 turns of ~220 tokens (spec 2026-08-09 §2.1, 104,232 tokens over 475 turns).
func BenchmarkComputeBreakdownMeasured(b *testing.B) {
	req := benchRequest(50, 220)
	b.ResetTimer()
	for b.Loop() {
		prompt.ComputeBreakdown(req, "hint", conversations.CountTokens)
	}
}

// BenchmarkComputeBreakdownAtBudget is sized at the Part B ceiling: a history
// filling a 500,000-token budget on a 1M window.
func BenchmarkComputeBreakdownAtBudget(b *testing.B) {
	req := benchRequest(2280, 220)
	b.ResetTimer()
	for b.Loop() {
		prompt.ComputeBreakdown(req, "hint", conversations.CountTokens)
	}
}
```

- [ ] **Step 2: Run the benchmarks and record the numbers**

Run: `go test ./internal/agent/prompt/ -bench 'BenchmarkComputeBreakdown' -benchtime 10x -run '^$'`
Expected: both complete. Record ns/op for each.

- [ ] **Step 3: Record the measurement in the spec**

Append the two measured figures to `docs/superpowers/specs/2026-08-09-history-token-budget-design.md` §4, under "How tokens are counted", as a new sentence naming both sizes and both costs.

**If `BenchmarkComputeBreakdownAtBudget` exceeds 50 ms/op**, add one line to that same paragraph recording it as measured, and open a follow-up note in §8 for a per-message count cache keyed by content hash — a full history is re-tokenized on every call within a turn, and the messages are immutable, so caching would remove nearly all of it. Do not build the cache in this plan.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/prompt/context_breakdown_bench_test.go docs/superpowers/specs/2026-08-09-history-token-budget-design.md
git commit -m "Measure what the measurement costs, at both sizes that matter"
```

---

### Task 6: Persist usage for every LLM call, not only the terminal one

`llm.Usage` is captured on every call, but `AppendAssistantTurnWithCacheMetric` runs only for the terminal answer (`internal/runner/runner_persist.go:324`). Measured consequence: `019fa8ba` has 57 assistant turns and 7 `aura.cache_metrics` rows.

**Files:**
- Modify: `internal/runner/runner_persist.go`
- Test: `internal/runner/runner_persist_test.go`

**Interfaces:**
- Consumes: `Runner.cacheMetricParams(convID string, seq int, u llm.Usage, cost float64) (sqlc.InsertCacheMetricParams, error)` (`runner_persist.go:334`), `ConvStore.AppendAssistantTurnWithCacheMetric` (`internal/runner/interfaces.go:43`).

**The gap is transport, not persistence.** Verified 2026-08-09:

- Parsing is correct — `internal/llm/openai_compat/usage.go` reads the provider's native counts.
- Metering is correct — `recordUsage` runs on every call (`internal/agent/llm_agent_consume.go:38`, `llm_agent_completion.go:119`), so `aura_agent_prompt_tokens_total` already counts all of them.
- **Transport is not** — `usageStateDelta` is attached only to the turn's final Event (`internal/agent/llm_agent_events.go:231`), and the Runner rebuilds usage from that delta (`runner_persist.go:475-482`). A call ending in `tool_calls` reaches `flushToolCalls` (`runner_persist.go:179-196`), which writes the turn through a plain `AppendTurn` with no usage and no metric.

So this task adds a carrier, then uses it.

- [ ] **Step 1: Write the failing test**

Add to `internal/runner/runner_persist_test.go`, using the package's existing fake `ConvStore` (see `internal/runner/fakes_test.go`):

```go
func TestIntermediateAssistantTurnWritesCacheMetric(t *testing.T) {
	conv := newFakeConvStore(t) // existing helper in fakes_test.go
	r := newTestRunner(t, conv) // existing helper

	// Two LLM calls in one round: a tool-call turn, then the answer.
	persistIntermediateAssistantTurn(t, r, llm.Usage{PromptTokens: 100, CompletionTokens: 5})
	persistTerminalAssistantTurn(t, r, llm.Usage{PromptTokens: 200, CompletionTokens: 9})

	if got := conv.cacheMetricCalls(); got != 2 {
		t.Fatalf("cache_metrics rows = %d, want 2 (one per LLM call, not one per round)", got)
	}
}
```

Replace `newFakeConvStore` / `newTestRunner` / `cacheMetricCalls` with the real helper names in `internal/runner/fakes_test.go`, and drive the two turns the way the neighbouring tests in `runner_persist_test.go` drive events. If the fake store does not count cache-metric writes, add that counter to it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run TestIntermediateAssistantTurnWritesCacheMetric -v`
Expected: FAIL — `cache_metrics rows = 1, want 2`

- [ ] **Step 3: Carry usage on the tool-call event**

`usageStateDelta` currently rides only the final Event (`internal/agent/llm_agent_events.go:222-233`). The tool-call event must carry the same projection, so the Runner can rebuild it with the existing `usageFromStateDelta` (`runner_persist.go:475-482`) instead of a second decoder.

In `internal/agent/llm_agent_events.go`, the tool-call event builder currently sets:

```go
	ev.Actions.StateDelta = map[string]any{"tool_call_id": run.ToolCallID}
```

Merge the usage projection into that same map rather than replacing it — `tool_call_id` is load-bearing for the Runner's pairing:

```go
	delta := usageStateDelta(usage)
	if delta == nil {
		delta = map[string]any{}
	}
	delta["tool_call_id"] = run.ToolCallID
	ev.Actions.StateDelta = delta
```

Thread the call's `llm.Usage` into that builder from the same place `recordUsage` already receives it (`llm_agent_consume.go:38`). Do not re-derive it: one usage value per call, one source.

- [ ] **Step 4: Write the Runner side**

In `flushToolCalls` (`internal/runner/runner_persist.go:179-196`), replace the plain `AppendTurn` with the metric-carrying write the terminal path uses. `turnTracker` must carry the usage observed on the tool-call event, since `flushToolCalls` runs when the first tool result arrives, not when the event is seen:

```go
	metric, err := r.cacheMetricParams(tr.convID, 0, tr.pendingUsage, r.costOf(tr.pendingUsage))
	if err != nil {
		return err
	}
	if err := r.Conv.AppendAssistantTurnWithCacheMetric(ctx, conversations.AppendTurnParams{
		ConversationID: tr.convID,
		Role:           llm.RoleAssistant,
		ToolCalls:      raw,
	}, metric); err != nil {
		return fmt.Errorf("persist assistant tool_calls turn: %w", err)
	}
```

Add `pendingUsage llm.Usage` to `turnTracker` (`runner_persist.go:33-58`), set it where the tool-call event is handled, and clear it alongside `tr.pendingToolCalls = nil`. For the cost, call the same function `persistAssistantAnswer` calls — read lines 298-333 and reuse it verbatim. **Do not introduce a second cost source and do not pass 0**: `internal/llm/prices.go:38-39` prefers the provider's reported cost, and bypassing it would reintroduce the recompute the codebase deliberately avoids (D-18).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runner/ -v`
Expected: PASS

Run: `go vet ./... && go build ./... && go test -race ./internal/runner/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/llm_agent_events.go internal/agent/llm_agent_consume.go internal/runner/runner_persist.go internal/runner/runner_persist_test.go internal/runner/fakes_test.go
git commit -m "Record every LLM call's usage, not just the one that happens to answer"
```

---

### Task 7: Live verification against provider ground truth

The unit tests prove routing. Only a live turn proves the breakdown reconciles with what the provider actually charged.

**Files:**
- Create: `docs/superpowers/verification/2026-08-09-context-breakdown/RESULTS.md`

- [ ] **Step 1: Bring the stack up and confirm the image is current**

```bash
docker compose up -d
docker compose build aura && docker compose up -d aura
```

The image must be built from the branch HEAD; an old image silently verifies old code.

- [ ] **Step 2: Capture the metric baseline**

```bash
docker exec aura-prometheus-1 sh -c "wget -qO- http://127.0.0.1:9464/metrics" | grep -c aura_agent_context_tokens
```

Expected: `0` before any turn (OTel does not export an instrument with no measurement).

- [ ] **Step 3: Drive two real turns through the cockpit**

Log in and create a conversation using the recipe in `docs/superpowers/specs/2026-08-09-history-token-budget-design.md` §2.3 (`/api/auth/config` → `/auth/email-password/sign-in` → `/api/conversations` → `/agent/run`, each mutating call carrying an `Idempotency-Key` header).

Turn 1: `"Rispondi solo con la parola: pronto"`
Turn 2: `"Carica gli schemi di questi tool con UNA sola chiamata a tool_search: shell_exec, fs_read, fs_write, document_search, web_search, send_file. Poi dimmi solo 'caricati'."`

- [ ] **Step 4: Assert the reconciliation**

```bash
docker exec aura-prometheus-1 sh -c "wget -qO- http://127.0.0.1:9464/metrics" | grep '^aura_agent_context_tokens'
docker exec aura-postgres psql -U aura -d aura -c \
  "SELECT seq, prompt_tokens FROM aura.cache_metrics WHERE conversation_id='<conv>' ORDER BY seq"
```

Three assertions, all of which must hold:

1. Five `aura_agent_context_tokens{category=...}` series exist, one per `prompt.AllCategories()`.
2. `tool_definitions` grows by roughly 12,800 tokens between turn 1 and turn 2 — the §2.3 measurement (9,865 → 22,700, ~2,100 per promoted schema) reproduced through the new instrument.
3. The sum of the categories reconciles with the provider's `prompt_tokens` for the same call within the cl100k-vs-deepseek tokenizer error. **Record the actual percentage difference** — it is the estimate error the spec says must be measured rather than assumed.

- [ ] **Step 5: Assert per-call usage persistence**

```bash
docker exec aura-postgres psql -U aura -d aura -c \
  "SELECT (SELECT count(*) FROM aura.conversation_turns t WHERE t.conversation_id=c.id AND t.role='assistant') assistant_turns,
          (SELECT count(*) FROM aura.cache_metrics m WHERE m.conversation_id=c.id) cache_rows
   FROM aura.conversations c WHERE c.id='<conv>'"
```

Expected: `cache_rows` equals `assistant_turns`. Before Task 6 the second turn's tool-call LLM call left no row.

- [ ] **Step 6: Write the results file and commit**

Record every number measured, including the reconciliation error, and any assertion that did not hold. A failed assertion is a finding, not a reason to omit it.

```bash
git add docs/superpowers/verification/2026-08-09-context-breakdown/RESULTS.md
git commit -m "Verify the breakdown against what the provider actually charged"
```

---

## Definition of Done

- [ ] `go vet ./... && go build ./...` clean
- [ ] `go test -race ./internal/agent/... ./internal/obs/ ./internal/conversations/ ./internal/runner/` green
- [ ] `bash scripts/coverage_docker.sh` — touched packages at or above 85%
- [ ] Task 7's three reconciliation assertions recorded with real numbers, including the estimate error
- [ ] `cache_rows == assistant_turns` on a real multi-call conversation
- [ ] Spec §4 updated with the benchmark figures from Task 5
