# Phase 32: Quality Cleanup — Dead Code + Shared Helpers - Pattern Map

**Mapped:** 2026-06-29
**Files analyzed:** 24 (3 new pkgs ×2 files + ~14 in-place/test-extension + web)
**Analogs found:** 22 / 24 (2 web items are byte-identical-copy collapses → no "analog" needed, the canonical file IS the target)

> This is a **behavior-preserving** phase. The single most load-bearing reuse is the **leaf-package
> shape** for the three new `internal/` packages. The repo already has two textbook precedents born
> from the *exact same kind of dedup audit finding* this phase acts on: `internal/secret` ("keep ONE
> list, divergence caused finding B-09") and `internal/canonicaljson` (pure stdlib leaf, table+fuzz
> +property test, consumed by `agent` + `agent/workflow`). Copy those wholesale.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/neostore/neostore.go` (NEW) | utility (shared leaf) | transform (Cypher coercion + hash) | `internal/canonicaljson/canonicaljson.go` (leaf shape) + `internal/reasoningstore/store.go:80-121` (source) | exact (structure) + exact (source copies) |
| `internal/neostore/neostore_test.go` (NEW) | test (characterization/parity) | transform | `internal/canonicaljson/canonicaljson_test.go` | exact |
| `internal/envutil/envutil.go` (NEW) | utility (shared leaf) | transform (env parse + fallback) | `internal/secret/envkey.go` (leaf shape) + `internal/config/config_env.go:28-54` (source) | exact (structure) + exact (source) |
| `internal/envutil/envutil_test.go` (NEW) | test (table, env-mutation) | transform | `internal/config/config_test.go:536-578` (`t.Setenv` tests of the OLD copies) | exact |
| `internal/agentrender/agentrender.go` (NEW) | utility (render primitives) | transform (event→usage shaping) | `cmd/aura/chat_render.go:111-229` (source) | exact (source) |
| `internal/agentrender/agentrender_test.go` (NEW) | test (parity + eval fix) | transform | `internal/canonicaljson/canonicaljson_test.go` (table shape) | role-match |
| `internal/canonicaljson/canonicaljson.go` (MODIFY: add `CanonicalArgs`) | utility | transform | self — `canonicalArgs`/`canonArgs` move INTO the home pkg | exact |
| `internal/agent/llm_agent_args.go` (MODIFY: drop `canonicalArgs`) | service | transform | `internal/agent/workflow/loop.go:345-355` (twin copy) | exact |
| `internal/agent/workflow/loop.go` (MODIFY: drop `canonArgs`) | service | transform | `internal/agent/llm_agent_args.go:62-76` (twin copy) | exact |
| `internal/agent/llm_agent_retry.go` (MODIFY: extract `isTransientNetworkErr`, WIDEN tool) | service | event-driven (retry) | `internal/agent/llm_agent_stream_retry.go:77-113` (sibling classifier) | role-match (asymmetric — see Pitfall) |
| `internal/agent/llm_agent_stream_retry.go` (MODIFY: delegate to shared subset, STRICT) | service | event-driven (retry) | self (parity) | exact |
| `internal/agui/governance_api.go` (MODIFY: `indexByte`→stdlib, inline `stringList`) | controller (HTTP handler) | request-response | `strings.IndexByte` / `append([]string{}, …)` (stdlib) | exact (stdlib swap) |
| `internal/agent/llm_agent.go` (MODIFY: restructure discarded `Build()` @235) | service | request-response | self (branch parity) | exact |
| `internal/db/` (MODIFY: add `NumericFromFloat`/`FloatFromNumeric`) | model (PG helper) | transform | `internal/conversations/store_helpers.go:150-177` (source) ≡ `internal/cachemetrics/store_helpers.go:73-97` | exact (source, near-identical) |
| `internal/settings/settings.go` (MODIFY: delete 2 `AURA_MEMORY_EMBED_*` keys) | config | — | self (allowlist entries) | n/a (deletion) |
| `internal/channels/deps.go` (MODIFY/DELETE: telebot blank import) | config (build anchor) | — | n/a | n/a (deletion) |
| `internal/assets/types.go` (TRIAGE: `Status{Created,Embedding,Canceled}`) | model | — | n/a — escalate (D-02/D-04) | n/a (escalate) |
| `cmd/aura/agent.go` (KEEP @127 + add load-bearing test) | controller (CLI) | request-response | `internal/agent/agenttest/mocks.go:61` (fake omits `RequestID`) | n/a (keep + test) |
| `internal/web/throttle_test.go` (NEW) | test (concurrency) | event-driven (semaphore) | `internal/web/main_test.go` (goleak `TestMain`) | exact |
| `internal/setup/handlers.go` test (NEW/extend) | test (ordering regression) | request-response (SSE) | existing `internal/setup` tests | role-match |
| `internal/channels/telegram/profile_onboarding.go` test (extend) | test (keyword fallback) | transform | existing telegram tests | role-match |
| `internal/agent/llm_agent_completion.go` test (extend `truncateTailBytes`) | test (UTF-8) | transform | existing `TestTruncateBytes` (same file's sibling) | exact |
| `internal/webauth/authula.go` test (extend `ensureAuthulaSearchPath`) | test (DSN parse) | transform | existing `internal/webauth` tests | role-match |
| `web/src/conversations/useConversations.ts` + `web/src/governance/governanceApi.ts` (MODIFY: import canonical `getJSON`) | utility | request-response | `web/src/api/json.ts` (canonical) | exact (byte-identical copies) |
| `web/src/board/BoardLayout.tsx` + `web/src/.../McpLifecycleCluster.tsx` (MODIFY: adopt `focusTrap`) | component | event-driven (a11y) | `web/src/a11y/focusTrap.ts` (canonical, bug-fixing) | role-match (NOT byte-parity) |
| skeleton consumers (`ConversationSidebar`, `SearchPanel`, `governanceView`) (MODIFY) | component | — | `web/src/components/skeleton/Skeleton.tsx` (keep rich) | role-match |

---

## Shared Patterns

These three patterns recur across every new/modified file and should be applied uniformly.

### Shared Pattern A — `internal/` shared-helper leaf package (THE template for neostore/envutil/agentrender)

**Source of truth:** `internal/secret/envkey.go` — born from the *same kind* of audit finding ("keep ONE
list; divergence let a bare `*_KEY` leak on one path, B-09"). This is the closest *conceptual* analog;
`internal/canonicaljson` is the closest *structural* analog (pure stdlib leaf, no internal deps).

**Package-doc convention** (lead the file with WHY one copy exists — both analogs do this):

```go
// internal/secret/envkey.go:1-6
// Package secret holds the canonical secret-env-key predicate shared by every
// site that filters or redacts environment variables before they reach a child
// process or an exported config. Keeping ONE list prevents the divergence that
// let a bare *_KEY (e.g. PRIVATE_KEY) be redacted on one path and leak on
// another (audit finding B-09).
package secret
```

```go
// internal/canonicaljson/canonicaljson.go:1-17  (leaf — stdlib only)
import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)
```

**Apply to:**
- `neostore` doc → "canonical Neo4j store-helper home; 3 byte-identical copies of `hashText`/`asString`/`asFloats` existed across reasoningstore/toolselectstore/activelearn (slice-B QA-B-01/02/03)."
- `envutil` doc → "canonical env-parse-with-fallback helpers; 3 self-documented copies existed (config/channels/telegram, QA-C-02)."
- `agentrender` doc → "shared REPL/eval event-render primitives; a silent `json.Number` drift existed between the two copies (QA-C-04)."

### Shared Pattern B — characterization/parity table test + goleak (D-09/D-10 vehicle)

**Sources:** `internal/canonicaljson/canonicaljson_test.go` (table + fuzz + `pgregory.net/rapid`
property), `internal/config/config_test.go:536-578` (`t.Setenv` table), `internal/web/main_test.go`
(goleak `TestMain`).

Table-of-cases idiom (copy this exact shape; `tc`/`tt` named struct, subtests):

```go
// internal/secret/envkey_test.go:5-53  (canonical table-test shape)
func TestIsSecretEnvKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "private key", key: "PRIVATE_KEY", want: true},
		// ... union of inputs ...
		{name: "empty", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSecretEnvKey(tt.key); got != tt.want {
				t.Fatalf("IsSecretEnvKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
```

`t.Setenv` env-mutation idiom for `envutil` (these ARE the OLD-copy tests — the parity test feeds the
union of these inputs to both the old `config.envIntDefault` AND the new `envutil.IntDefault`):

```go
// internal/config/config_test.go:551-566
func TestEnvIntDefault_ParsesValid_FallsBackOnGarbage(t *testing.T) {
	t.Setenv("AURA_TEST_INT_VALID", "42")
	if got := envIntDefault("AURA_TEST_INT_VALID", 7); got != 42 { ... }
	t.Setenv("AURA_TEST_INT_GARBAGE", "not-a-number")
	if got := envIntDefault("AURA_TEST_INT_GARBAGE", 7); got != 7 { ... }   // garbage → fallback
	t.Setenv("AURA_TEST_INT_EMPTY", "")
	if got := envIntDefault("AURA_TEST_INT_EMPTY", 13); got != 13 { ... }   // empty → fallback
}
```

goleak `TestMain` (mandatory for any pkg with goroutines, e.g. `internal/web/throttle`):

```go
// internal/web/main_test.go:1-23
//go:build !web_integration

package web

import (
	"testing"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

**Parity assertion (D-09 — "old copies agree, then extracted matches"):** for each extraction the test
must assert `oldCopyA(x) == oldCopyB(x) == NewExtracted(x)` for the union of inputs, committed GREEN
*before* the move (D-10). Concrete per-helper union tables are in 32-RESEARCH.md §Validation Architecture.

### Shared Pattern C — import-boundary safety (verified, must hold)

The `agent ⇸ agui` boundary is one-directional: `agui` imports `agent`; `agent` does NOT import `agui`.
None of the three new packages import `agui`. Cycle map (all safe leaves):
`neostore → stdlib only`; `envutil → stdlib only`; `agentrender → agent + llm` (neither imports back).
`canonicaljson.CanonicalArgs` is import-cycle-free because both `agent` and `agent/workflow` already
import `canonicaljson` (see `llm_agent_args.go:8`).

---

## Pattern Assignments

### `internal/neostore/neostore.go` (NEW — utility leaf, canonical per D-06)

**Structural analog:** `internal/canonicaljson/canonicaljson.go` (pure leaf) + `internal/secret/envkey.go` (doc style).
**Source copies (byte-identical — verified):** `internal/reasoningstore/store.go` ≡ `internal/toolselectstore/store.go`.

**`GraphClient` seam** (lift verbatim from `reasoningstore/store.go:19-22`; toolselectstore's `:22-25` is a "Copied verbatim from reasoningstore.GraphClient" twin):

```go
// internal/reasoningstore/store.go:19-22  → becomes neostore.GraphClient (exported)
type GraphClient interface {
	Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
	Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}
```

**`HashText` / `AsString` / `AsFloats`** (from `reasoningstore/store.go:80-121`; the 3rd `HashText` copy is `activelearn/learner.go:113-116` as `hashText`; toolselectstore names its hash `hashQuery` `:115-118`):

```go
// internal/reasoningstore/store.go:80-121  → export as neostore.HashText/AsString/AsFloats
func hashText(text string) string {                       // sha256→hex content-MERGE key (NOT a password)
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func asString(v any) string {
	if s, ok := v.(string); ok { return s }
	return ""
}

// asFloats coerces a Cypher embedding column: APOC JSON string ("[-0.02,...]") OR a raw []any list.
func asFloats(v any) []float64 {
	switch t := v.(type) {
	case string:
		var out []float64
		if json.Unmarshal([]byte(t), &out) != nil { return nil }   // malformed string → nil
		return out
	case []any:
		out := make([]float64, 0, len(t))
		for _, x := range t {
			switch n := x.(type) {
			case float64: out = append(out, n)
			case int64:   out = append(out, float64(n))
			case int:     out = append(out, float64(n))
			default:      return nil                                // any non-numeric element → nil
			}
		}
		return out
	default:
		return nil
	}
}
```

**Call-site migration:** `reasoningstore` + `toolselectstore` delete their local `hashText`/`hashQuery`/`asString`/`asFloats`/`GraphClient` and reference `neostore.*`; `activelearn` imports `neostore.HashText`. `*knowledge.Client` already satisfies `neostore.GraphClient` structurally — no change at the satisfying type.
**Parity caveat (D-09):** the `AsFloats` table MUST capture the nil-vs-empty distinction (`"[]"` and `[]any{}` cases) — see 32-RESEARCH.md.

---

### `internal/neostore/neostore_test.go` (NEW — parity test)

**Analog:** `internal/canonicaljson/canonicaljson_test.go` (table-of-cases + subtests). Use Shared Pattern B. No goleak needed (no goroutines). Union-of-inputs cases enumerated in 32-RESEARCH.md §Validation Architecture (HashText: `""`/`"a"`/unicode/long; AsString: string→self, int/nil/[]any/map→`""`; AsFloats: the 8 listed shapes incl. nil-vs-empty).

---

### `internal/envutil/envutil.go` (NEW — utility leaf, minimal per D-06)

**Structural analog:** `internal/secret/envkey.go` (stdlib leaf, predicate-style).
**Source copy (canonical):** `internal/config/config_env.go:28-54` — twins at `channels/telegram/config.go:56-66` (`envIntDefault`) and `channels/registry.go:162-173` (`envChannelEnabled`, a bool-default wrapper).

```go
// internal/config/config_env.go:28-54  → export as envutil.IntDefault/BoolDefault (silently absorb bad input)
func envIntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" { return fallback }
	n, err := strconv.Atoi(v)
	if err != nil { return fallback }        // malformed → fallback, never fatal
	return n
}

func envBoolDefault(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" { return fallback }
	b, err := strconv.ParseBool(v)
	if err != nil { return fallback }
	return b
}
```

**Call-site migration:** `config_env.go` helpers become thin `envutil.*` calls (or delete + call directly); `telegram/config.go` drops its copy → `envutil.IntDefault`; `channels/registry.go` `envChannelEnabled` keeps key-building, delegates the parse → `envutil.BoolDefault(key, true)`.
**Scope boundary (CRITICAL, from RESEARCH):** D-07 "adopt for agent-tool knobs (QA-A-05/08)" = **mechanical `os.Getenv`+parse → `envutil` swap only**. Moving reads to construction-time / `config.Load` is QUAL-04 → Phase 33/34 (OUT). Keep `envutil` to the 3 named copies; touch agent-tool knobs only if those files are otherwise edited.
**Optional API to mirror `config_env.go`:** also expose `StringDefault` (`:17-22`) + `SliceDefault` (`:60-73`) if the planner wants full parity; not required by the 3 copies.

---

### `internal/envutil/envutil_test.go` (NEW — table test, env-mutation)

**Analog (exact):** `internal/config/config_test.go:536-578` — those tests already exercise the OLD copies with `t.Setenv` (valid / garbage→fallback / empty→fallback). The parity test feeds the union to both `config.envIntDefault` AND `envutil.IntDefault`. Drives ≥85% trivially. See Shared Pattern B.

---

### `internal/agentrender/agentrender.go` (NEW — render primitives, minimal per D-06)

**Source (~80 LOC):** `cmd/aura/chat_render.go:111-229` ≡ `internal/eval/capture_cot_eval.go:153-229`.
**⚠ Adopt the superset:** `chat_render`'s `anyInt` handles `json.Number` (`:222-228`); the eval copy does NOT (silently zeroes token counts). Export `chat_render`'s version — this is a **behavior fix** for the eval path the parity test must document.

```go
// cmd/aura/chat_render.go:157-160 / 199-228  → export agentrender.IsToolResultPreview/UsageFromStateDelta/AnyInt
func isToolResultPreview(ev *agent.Event) bool {              // tool-result events carry tool_call_id
	_, ok := ev.Actions.StateDelta["tool_call_id"]
	return ok
}

func usageFromStateDelta(d map[string]any) llm.Usage {
	if d == nil { return llm.Usage{} }
	u := llm.Usage{
		PromptTokens:     anyInt(d["prompt_tokens"]),
		CompletionTokens: anyInt(d["completion_tokens"]),
		CachedTokens:     anyInt(d["cache_hit_tokens"]),
	}
	if c, ok := anyFloat(d["cost_usd"]); ok { u.Cost = &c }
	return u
}

func anyInt(v any) int {
	switch n := v.(type) {
	case int:        return n
	case int64:      return int(n)
	case float64:    return int(n)
	case json.Number:                                        // SUPERSET — eval copy lacks this branch
		if i, err := n.Int64(); err == nil { return int(i) }
		return 0
	default:
		return 0
	}
}
```

**API to export:** `FlushRemainder`, `IsToolResultPreview`, `IsTerminalToolCall`, `UsageFromStateDelta`, `AnyInt`, `AnyFloat` (`FlushRemainder` is `chat_render.go:116-128`; `IsTerminalToolCall` is `:164-171`).
**Boundary:** `agentrender → agent + llm` only (Shared Pattern C). Optional cycle-avoidance: make `IsToolResultPreview` take `map[string]any` (the StateDelta) to drop the `agent` import — minor signature change at 2 call sites, either way is boundary-safe.

---

### `internal/agentrender/agentrender_test.go` (NEW — parity + eval fix)

**Analog:** `internal/canonicaljson/canonicaljson_test.go` table shape. The `AnyInt` table is the key one: cases `int`/`int64`/`float64`/`json.Number(valid)`/`json.Number(invalid)`/`nil`/other; assert merged == `chat_render`'s superset and **document** eval's old `json.Number`→0. Other helpers are byte/struct-equal parity.

---

### `internal/canonicaljson/canonicaljson.go` — ADD `CanonicalArgs` (QA-A-01)

**Source (byte-identical except name):** `internal/agent/llm_agent_args.go:66-76` (`canonicalArgs`) ≡ `internal/agent/workflow/loop.go:345-355` (`canonArgs`). Home is `canonicaljson` because both call sites already import it.

```go
// internal/agent/llm_agent_args.go:66-76  → canonicaljson.CanonicalArgs(rawArgs string) []byte
func canonicalArgs(rawArgs string) []byte {
	var v any
	if err := json.Unmarshal([]byte(rawArgs), &v); err != nil {
		return []byte(rawArgs)                 // non-JSON → raw bytes (stable fingerprint on malformed input)
	}
	canon, err := canonicaljson.Marshal(v)
	if err != nil {
		return []byte(rawArgs)                 // un-canonicalizable → raw bytes
	}
	return canon
}
```

**Migration:** add `CanonicalArgs` to `canonicaljson` (test in `canonicaljson_test.go`); delete `canonicalArgs` from `llm_agent_args.go` and `canonArgs` from `workflow/loop.go`; repoint both call sites. Parity union: valid object (unsorted→sorted), array, nested, number forms (`1e3`/`1.0`/large int), string/bool/null, malformed (→raw), empty (→raw `""`), whitespace-only.

---

### `internal/agent/llm_agent_retry.go` + `llm_agent_stream_retry.go` — `isTransientNetworkErr` (QA-A-02) ⚠ ASYMMETRIC

**The two classifiers are NOT symmetric — a naïve "both delegate to one predicate" breaks the stream path.**

```go
// internal/agent/llm_agent_retry.go:56-68  — isTransientToolErr: DeadlineExceeded → TRUE
func isTransientToolErr(err error) bool {
	if err == nil { return false }
	if errors.Is(err, context.DeadlineExceeded) { return true }   // tool path RETRIES deadline
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() { return true }
	return false
}
```

```go
// internal/agent/llm_agent_stream_retry.go:77-113  — retryableStreamOpenError: DeadlineExceeded → FALSE (guard FIRST)
func retryableStreamOpenError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false                                              // stream path does NOT retry deadline — opposite of tool
	}
	var httpErr *openai_compat.HTTPError
	if errors.As(err, &httpErr) { return httpErr.StatusCode == 429 || httpErr.StatusCode >= 500 }
	// ... url.Error, ErrStreamIdleTimeout, typed syscall sentinels (io.EOF/ECONNRESET/…), retryableNetworkText fallback
}
```

**Shared subset to extract** (typed network sentinels both share): `isTransientNetworkErr(err)` = `net.Error.Timeout()` ∨ `errors.Is` of `io.ErrUnexpectedEOF`/`io.EOF`/`syscall.ECONNRESET`/`ECONNREFUSED`/`ETIMEDOUT`; `nil`/other → false. Excludes `context.*`, HTTP status, url.Error, `retryableNetworkText`, `ErrStreamIdleTimeout`.
- `isTransientToolErr` (**INTENTIONAL WIDENING**) = `errors.Is(err, context.DeadlineExceeded) || isTransientNetworkErr(err)`. Now also retries ECONNRESET/EOF. Characterize OLD, assert NEW widened set, document.
- `retryableStreamOpenError` (**STRICT PARITY**) = keep the leading `context.*→false` guard FIRST, then HTTPError, url.Error, `ErrStreamIdleTimeout`, `isTransientNetworkErr(err)`, `retryableNetworkText` last. Golden table captured BEFORE refactor; output identical after.

---

### `internal/agui/governance_api.go` — stdlib swaps (QA-C-10) IN PLACE (no extraction)

```go
// internal/agui/governance_api.go:213  → strings.IndexByte / strings.Cut
if i := indexByte(entry, '='); i >= 0 { key = entry[:i] }     // delete indexByte (:226-231), use strings.IndexByte

// internal/agui/governance_api.go:199-203  → inline; KEEP non-nil-empty (Pitfall 4)
func stringList(in []string) []string {                        // currently: make([]string,0,len) + append
	out := make([]string, 0, len(in))
	out = append(out, in...)
	return out
}
// call site :177  NetworkAllowlist: stringList(server.Runtime.Network)
//   → NetworkAllowlist: append([]string{}, server.Runtime.Network...)   ← []string{} NOT nil, so JSON stays [] not null
```

Add/keep a test asserting empty `Runtime.Network` → marshalled `NetworkAllowlist` is `[]` (never `null`). The handler-level `envChips` (`:209-222`) output must stay byte-identical.

---

### `internal/db/` — ADD `NumericFromFloat`/`FloatFromNumeric` (Open Question #1 — RECOMMENDED home)

**Source (near-identical; only the error *string* differs — assert value+err-presence, NOT message):**
- `internal/conversations/store_helpers.go:150-177` (`numericFromFloat`, err uses `"+/-%.4f"`, `numericMaxCost=999999.9999` @ `:21`)
- `internal/cachemetrics/store_helpers.go:73-97` (`numericFromFloat`, err uses `"±%v"`, `numericMaxCost` @ `:65`)
- `floatFromNumeric` is byte-identical across both.

```go
// internal/conversations/store_helpers.go:164-166  (numeric logic identical to cachemetrics)
func numericFromFloat(f float64) (pgtype.Numeric, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f > numericMaxCost || f < -numericMaxCost {
		return pgtype.Numeric{}, fmt.Errorf("cost %v out of numeric(10,4) range +/-%.4f", f, numericMaxCost)
	}
	// ... scale ×1e4 → round half-away-from-zero → big.NewInt mantissa, Exp: -numericScale
}
```

**Proposed API:** `db.NumericFromFloat(f float64) (pgtype.Numeric, error)`, `db.FloatFromNumeric(n pgtype.Numeric) float64`, `db.DefaultNumericMaxCost`. Both callers already import `internal/db` (already coverage-gated → no new gate registration). Existing `internal/db/db_unit_test.go` is the test to extend.
**⚠ Note (deviation from D-07 literal):** D-07 *lists* this under `neostore`, but it is a Postgres helper, not Neo4j — putting it in `db` keeps "3 new packages" = exactly neostore/envutil/agentrender (D-13). Flag to operator (Open Question #1).

---

## Dead-Code Triage Targets (QUAL-02 — verdicts from 32-RESEARCH.md, no new analog)

| Item | File:line | Verdict | Action |
|------|-----------|---------|--------|
| `RequestID` re-stamp | `cmd/aura/agent.go:127` | **LOAD-BEARING — KEEP** | Fake `agenttest.InfiniteToolCallAgent` omits `RequestID` (`agenttest/mocks.go:61-65`); add a dry-run test asserting every event carries `requestID` (GREEN with line 127, RED without). Clarify comment. Surface to operator that this QUAL-02 item resolved to keep. |
| `assets.Status{Created,Embedding,Canceled}` | `internal/assets/types.go:14,20,25` | **INTENDED-BUT-UNWIRED → ESCALATE** | Designed lifecycle (`docs/superpowers/plans/2026-06-18-industrial-multimodal-asset-pipeline.md`); ~7 more siblings also unused. D-02 (wiring = new feature) + D-04 (exported) → operator keep/kill. Recommend KEEP+annotate. Do NOT confuse with `onboarding.StatusCanceled` (different pkg, IS used). |
| `agui.indexByte` / `stringList` | `governance_api.go:199,226` | **REINVENTED-STDLIB → SWAP/INLINE** | See agui assignment above. |
| telebot blank import | `internal/channels/deps.go:37` | **REDUNDANT → SAFE REMOVE** | In-code "CI pin gate" justification is FALSE (zero telebot ref in `.github/`/`scripts/`/`Makefile`); `telegram/bot.go:18` keeps `go.mod:38` DIRECT. Remove → `go mod tidy` → confirm `gopkg.in/telebot.v4` stays DIRECT → build green. If import block empties, move package-doc to `doc.go` or delete `deps.go` (Open Question #4). |
| `AURA_MEMORY_EMBED_*` | `internal/settings/settings.go:61-62` | **SIDECAR-OWNED → REMOVE-FROM-GO + DOCUMENT (D-05)** | **Full-stack** change (NOT a one-liner): Go `AllowedKeys` ×2, `ModelSettingsPanel.tsx:39-40` union + `:102-112` array, i18n en+it `settings.fields.memoryEmbed*` (symmetric — Pitfall 6), `compose.yaml`/`.env.example` doc, **rebuild+commit `internal/webui/dist`** (Pitfall 3). No migration (`OverlayEnv` allowlist guard ignores stale rows). `AURA_MEMORY_EMBED_API_KEY` is `Secret` — don't log on removal. |
| discarded `Build()` | `internal/agent/llm_agent.go:235` | **DEAD WORK → RESTRUCTURE** | `if adaptiveTierOK { req = BuildWithReasoningTier(...) } else { req = Build(...) }`. Parity: chosen `req` byte-identical to old chosen `req` per branch. |
| `truncateRunes` ×2 | `assets/context.go:133`, `rerank/client.go:167` | **DUP 5-LINER → ACCEPT or fold (Open Question #2)** | No string-util home exists (`internal/strutil` absent). Prefer accept-with-cross-ref-comment; a new `internal/strutil` would itself need coverage registration. Low priority. |

---

## Web Dedup Targets (QUAL-03/QA-D + D-08)

| Item | Canonical (import this) | Copies to delete/adopt | Note |
|------|------------------------|------------------------|------|
| `getJSON` (QA-D-01) | `web/src/api/json.ts:1-10` | `conversations/useConversations.ts:61-70` (unexported) + `governance/governanceApi.ts:113-122` (exported) | Byte-identical → just import. Repoint anything importing `getJSON` *from* governanceApi. Opportunistic: fold `useConversations.createConversation` hand-rolled POST → `postJSON` (`json.ts:12-23`). |
| `focusTrap` (QA-D-02) | `web/src/a11y/focusTrap.ts` (has `disabled` filter via `isFocusable`) | `BoardLayout.tsx:56-91` (omits disabled filter) + `McpLifecycleCluster.tsx:208-238` (queries only `button`) | **NOT byte-parity — the copies are BUGGY; adopting fixes a11y.** Consumer tests (`BoardLayout.test.tsx`, `McpLifecycleCluster`) must stay green. |
| Skeleton (QA-D-08 / D-08) | `components/skeleton/Skeleton.tsx` (349 LOC rich, keep) | retire `components/ui/skeleton.tsx` (13 LOC shadcn); migrate 3 consumers (`ConversationSidebar`, `SearchPanel`, `governanceView`) | **Highest visual-regression risk** → Playwright E2E + dist rebuild. Then barrel exports QA-D-13/D-18 become real. Reverse choice defensible (Assumption A4). |

**All web edits → rebuild + commit `internal/webui/dist`** (Pitfall 3 — the `web-dist-freshness` CI gate diffs it; this already bit Phase 31).

---

## Test-Gap Targets (QUAL-05 — extend existing test files)

| Target | Location | Analog test | Note |
|--------|----------|-------------|------|
| `web/throttle` concurrency | `internal/web/throttle.go:40-48` (`acquire`/`sem`, `perHostLimit=2`) | NEW `throttle_test.go` + `internal/web/main_test.go` goleak | acquire blocks at limit; release frees; ctx-cancel → `ok=false` + no-op release (NOT deferred against an untaken token); per-host isolation; concurrent race. |
| setup ordering | `internal/setup/handlers.go:146` (`InvalidateToken`-before-`writeSSE`) | existing `internal/setup` tests | regression asserting `InvalidateToken` called before first SSE write. |
| Telegram keyword fallback | `internal/channels/telegram/profile_onboarding.go:362` (`answersFromText`) | existing telegram tests | Italian-keyword-fallback table (only LLM-extractor path tested today). |
| `truncateTailBytes` | `internal/agent/llm_agent_completion.go:209-221` (+`truncateBytesKeepingTail` `:196-206`) | existing `TestTruncateBytes` (same file's sibling) | UTF-8 boundary table: `n<=0`, `len(s)<=n`, multi-byte mid-rune walk-back, head+marker+tail. |
| Authula DSN | `internal/webauth/authula.go:292` (`ensureAuthulaSearchPath`) | existing `internal/webauth` tests | Table: empty / malformed / has-`search_path` / no-`search_path`. **Stays in Phase 32** (pure fn, no cutover infra). |
| `memory_integration` CI leg | `.github/workflows/ci.yml:606-719` — **already exists & runs live** | — | **DOCUMENT, do not add** (D-12). Verify `memory_recall_integration_test.go:1,52` tag + `t.Fatal`-under-`$CI` one last time. |

---

## No Analog Found

| File | Role | Reason | Planner guidance |
|------|------|--------|------------------|
| `web/src/conversations/useConversations.ts` / `governanceApi.ts` (getJSON) | utility | Copies are **byte-identical** to `api/json.ts` — there is no "analog to copy", the canonical file IS the import target. | Delete copies, `import { getJSON } from '../api/json'`. |
| `internal/assets/types.go` (`Status*`) | model | Triage = **escalate**, not implement — no pattern to copy this phase (D-02/D-04). | Operator keep/kill decision before any code. |

---

## Metadata

**Analog search scope:** `internal/` (47 packages), `cmd/aura/`, `web/src/`, `.github/workflows/`.
**Files scanned (read):** `canonicaljson.{go,_test.go}`, `secret/envkey.{go,_test.go}`, `reasoningstore/store.go`, `toolselectstore/store.go`, `config/config_env.go`, `config/config_test.go:536-578`, `web/throttle.go`, `web/main_test.go`, `agent/llm_agent_args.go`, `agent/workflow/loop.go:335-356`, `agent/llm_agent_retry.go`, `agent/llm_agent_stream_retry.go`, `agui/governance_api.go:170-231`, `cmd/aura/chat_render.go:100-229`, `web/src/api/json.ts`, `web/src/a11y/focusTrap.ts`; greps for numeric helpers + skeleton/web duplicate files.
**Pattern extraction date:** 2026-06-29
**Boundary verified:** `agent ⇸ agui` one-directional; all 3 new packages are import-cycle-safe leaves/near-leaves.
```