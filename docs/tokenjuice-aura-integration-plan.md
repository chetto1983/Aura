# TokenJuice Go integration plan for Aura — implementation-ready

**Date:** 2026-05-18
**Algorithm source:** `docs/tokenjuice-algorithm-spec.md`
**Rules + license source:** `docs/tokenjuice-rules-catalog.md`
**Reference glue (Rust, GPLv3 — study only, do NOT copy):**
`D:/tmp/openhuman/src/openhuman/tokenjuice/tool_integration.rs`

---

## Goal

Port the upstream MIT `vincentkoc/tokenjuice` engine — a deterministic, rule-driven CLI-output compactor — into Aura as a pure Go library under `internal/tokenjuice/`, then wire it into the agent loop at the single point where each tool result is appended to the conversation state. The result: large `execute_shell` / `execute_code` outputs (git status, go test, rg, npm install, build/tsc, …) shrink **before** they reach `governance.Apply`, which then keeps doing what it already does — orphan/backfill, microcompact, hard-cap. The algorithm and pass-through safety rules are spelled out in `docs/tokenjuice-algorithm-spec.md` §3–§5; the priority-ranked rule starter set is in `docs/tokenjuice-rules-catalog.md` §4.3 / Top-10.

---

## Package layout

```
internal/tokenjuice/
├── tokenjuice.go            — public API: Compact, CompactWithRules, options
├── classify.go              — matchesRule, scoreRule, classifyExecution
├── reduce.go                — reduceWithRules, applyRule, formatInline,
│                              selectInlineText, file-content-inspection bypass
├── postprocess.go           — git/status porcelain rewriter (only post-processor
│                              we port in v1; cloud/gh deferred)
├── text.go                  — stripANSI + compiled regex package vars,
│                              normalizeLines, trimEmptyEdges, dedupeAdjacent,
│                              headTail, pluralize, clampText, clampTextMiddle,
│                              countTextChars (uniseg wrapper)
├── tokenize.go              — tokenizeCommand, normalizeExecutionInput,
│                              isFileContentInspectionCommand
├── types.go                 — Input, Options, Result, Stats, Rule, CompiledRule,
│                              RuleMatch, RuleFilters, RuleTransforms,
│                              RuleSummarize, RuleCounter, RuleFailure,
│                              RuleOutputMatch, ClassificationResult
├── rules.go                 — LoadBuiltinRules, LoadRules, LoadOptions,
│                              compileRule (regex precompile + drop-on-error)
├── builtin/                 — vendored rule JSON (10 rules, MIT — see §License)
│   ├── generic_fallback.json
│   ├── generic_help.json
│   ├── git_status.json
│   ├── git_log_oneline.json
│   ├── git_diff_stat.json
│   ├── tests_go_test.json
│   ├── tests_pytest.json
│   ├── search_rg.json
│   ├── build_tsc.json
│   └── install_npm_install.json
├── builtin_embed.go         — //go:embed builtin/*.json + (id, path) table
└── tokenjuice_test.go       — unit + fixture-driven tests
```

**Why this split (vs the upstream Rust nested `text/` + `rules/` subdirs).** Aura's convention (`internal/agent/governance/`, `internal/agent/tools/registry/`) is one flat package per concern. The reduce/classify/text helpers are all internal to TokenJuice and never reused outside, so one flat package keeps the public surface tiny (one Go import for callers). The builtin rule JSONs get their own subdir because `//go:embed builtin/*.json` is cleaner with a dedicated folder. We carve out `postprocess.go` separately so future post-processors (`cloud/gh` in v1.1) drop in without touching `reduce.go`.

---

## Public API surface

```go
package tokenjuice

// Input describes one tool invocation, modelled after the upstream
// ToolExecutionInput shape but trimmed to the fields Aura actually populates.
type Input struct {
    ToolName string   // e.g. "execute_shell", "execute_code"
    Argv     []string // optional; auto-derived from Command when empty
    Command  string   // optional; auto-derived from Argv when empty
    Stdout   string   // captured stdout (Aura already concatenates stderr into this for execute_shell)
    Stderr   string   // optional; used when caller can split streams
    ExitCode *int     // nil = unknown ≡ success; non-zero triggers failure-preserving head/tail
}

// Options are caller-side knobs. Zero values use upstream defaults.
type Options struct {
    MaxInlineChars int     // default 1200 graphemes (algorithm spec §1.4)
    ForceRuleID    string  // optional; bypasses scoring when set + found
    Raw            bool    // skip the engine entirely; return raw text
    // MinInputBytes and MinRatio guard the outer "don't bother / don't make it worse"
    // edges (algorithm spec §5.1). Zero values use upstream constants 512 and 0.95.
    MinInputBytes int
    MinRatio      float64
}

// Result is what the caller inlines into the LLM tool message.
type Result struct {
    InlineText string // payload to feed back into state.AddToolResultMessage
    Applied    bool   // false ⇒ InlineText == original Stdout (passthrough)
    RuleID     string // "git/status" | "generic/fallback" | "none/too-small" | …
    Stats      Stats
}

type Stats struct {
    ToolName       string
    OriginalBytes  int
    CompactedBytes int
    OriginalChars  int // graphemes of strip_ansi(raw)
    CompactedChars int // graphemes of inline
    Family         string
    Confidence     float64 // 1.0 forced, 0.9 matched, 0.2 fallback
}

func (s Stats) Ratio() float64 { /* CompactedBytes / OriginalBytes, 1.0 when zero */ }

// Compact runs the engine with the builtin rule set. Safe to call concurrently.
func Compact(in Input, opts Options) Result

// CompactWithRules is the explicit-rules form, used by tests + a future user
// overlay. The rule slice MUST contain "generic/fallback" or the call will
// synthesize an in-memory fallback (we DO NOT panic, contrary to upstream
// reduce.rs:799 — see §Failure-modes Open question #1).
func CompactWithRules(in Input, rules []*CompiledRule, opts Options) Result

// LoadBuiltinRules returns the 10 starter rules, compiled at first call and
// memoized via sync.OnceValue. Never returns nil.
func LoadBuiltinRules() []*CompiledRule

// LoadRules supports an optional user-layer overlay; see §Rule loading.
type LoadOptions struct {
    Builtin  bool   // default true
    UserDir  string // e.g. ~/.config/aura/tokenjuice/rules; "" = skip
}
func LoadRules(opts LoadOptions) ([]*CompiledRule, error)
```

**When to use which.** Aura's agent loop always calls `tokenjuice.Compact(in, Options{})` — the builtin set is enough and the call is allocation-cheap because rules are loaded once via `sync.OnceValue`. `CompactWithRules` exists for tests (deterministic rule sets) and for the future operator override path. Tools and channel adapters do not touch this API directly — only `internal/agent/executor.go` and `internal/agent/exec_helpers.go` do.

---

## Dependencies

| Module | Why | Cost |
|---|---|---|
| `github.com/rivo/uniseg` | Grapheme clustering — algorithm spec §4.3 mandates grapheme-aware limits/clamps for CJK + emoji + ZWJ. Pure-Go, MIT, zero transitive deps. | One module, ~250 LOC vendored equivalent. |

**Nothing else.** Specifically:
- **No regex library** — Go's stdlib `regexp` (RE2) is already a strict superset of what the 96 upstream patterns use (spec §7.1). Scanning the 10-rule starter set confirms zero lookaround / backreferences.
- **No JSON library** — `encoding/json` handles the rule format unchanged.
- **No YAML / TOML** — TokenJuice rules are JSON only (spec §7).
- **No `dirs`/path package** — `os.UserHomeDir()` + `filepath.WalkDir` cover the user-layer overlay if/when we ship it.

This matches Aura's memory rule `feedback_no_new_deps_without_need` — one new dep, justified, no transitive bloat. `uniseg` is small enough that we could vendor a single-file `text/width.go` of our own if we want zero new go.mod entries, but the upstream library is so trim that adding it is the better trade.

---

## Wiring into Aura's agent loop

The integration point is **NOT** `internal/agent/loop.go` itself — that file just dispatches and reads back via `state.AddToolResultMessage`. The actual write of each tool result string is in `internal/agent/executor.go::ExecuteToolCalls` (and its sibling `internal/agent/exec_helpers.go::ExecuteToolCalls`, which the Telegram-channel path uses). We compact at that boundary.

### internal/agent/executor.go (line ~85-94)

Current code:

```go
go func(i int, call llm.ToolCall) {
    defer wg.Done()
    raw, _, execErr := e.executeOneTool(ctx, call)
    var awaitErr *tools.ErrAwaitingUserInput
    if errors.As(execErr, &awaitErr) {
        awaitErr.ToolCallID = call.ID
        outcomes[i] = toolOutcome{id: call.ID, awaitingUser: awaitErr}
        return
    }
    wrapped := WrapUntrustedToolResult(call.Name, raw)
    outcomes[i] = toolOutcome{id: call.ID, content: limitToolContent(wrapped, e.maxChars)}
}(i, callCopy)
```

Proposed diff (pseudo-Go):

```go
go func(i int, call llm.ToolCall) {
    defer wg.Done()
    raw, _, execErr := e.executeOneTool(ctx, call)
    var awaitErr *tools.ErrAwaitingUserInput
    if errors.As(execErr, &awaitErr) {
        awaitErr.ToolCallID = call.ID
        outcomes[i] = toolOutcome{id: call.ID, awaitingUser: awaitErr}
        return
    }
    // TokenJuice compaction — runs BEFORE WrapUntrustedToolResult so the
    // untrusted-envelope wraps the SHORTER text. Runs BEFORE governance.Apply
    // because Apply operates on the full conversation slice; we want the
    // shrunk-once payload to be what microcompact later potentially stubs.
    tjIn := tokenjuice.Input{
        ToolName: call.Name,
        Argv:     argvFromArgs(call.Arguments),
        Command:  commandFromArgs(call.Arguments),
        Stdout:   raw,
        ExitCode: exitCodeFromToolOutput(call.Name, raw),
    }
    tj := tokenjuice.Compact(tjIn, tokenjuice.Options{})
    if tj.Applied {
        e.logger.Debug("tokenjuice: compacted",
            "tool", call.Name,
            "rule", tj.RuleID,
            "bytes_in", tj.Stats.OriginalBytes,
            "bytes_out", tj.Stats.CompactedBytes,
            "ratio", tj.Stats.Ratio(),
        )
    }
    wrapped := WrapUntrustedToolResult(call.Name, tj.InlineText)
    outcomes[i] = toolOutcome{id: call.ID, content: limitToolContent(wrapped, e.maxChars)}
}(i, callCopy)
```

The same diff applies verbatim to `internal/agent/exec_helpers.go::ExecuteToolCalls` around line 100–135 (the result is currently held in `result` before `WrapUntrustedToolResult` at line 145 — insert the Compact call between those two).

**Helper functions (new, in `internal/agent/tool_input.go` or near argKeysFromCall):**
- `argvFromArgs(args map[string]any) []string` — handles three shapes: `args["argv"] []string`, `args["command"] string` (whitespace-split), `args["command"]+args["args"]`. Mirrors the Rust `extract_command_argv` (`tool_integration.rs:173-206`).
- `commandFromArgs(args map[string]any) string` — single-string fallback, e.g. `args["command"]`. Empty when not shell-like.
- `exitCodeFromToolOutput(toolName, raw string) *int` — for `execute_shell`/`execute_code` only, parse the leading `exit_code: %d\n` prefix that `exec.go:181` and `exec.go:443` emit. Return `nil` for any other tool.

### Why BEFORE `governance.Apply`, not after

`governance.Apply` operates on the **whole** message history slice each turn (`loop.go:307`). TokenJuice compacts a **single** fresh tool result. Doing TokenJuice first means:

1. The compacted text lands in `state.AddToolResultMessage` once and stays compact for every subsequent turn — no re-running on each `Apply` call.
2. `governance.MicrocompactKeepRecent` (the rolling-window stub) sees an already-small payload, so when it later swaps the message for `"[execute_shell result omitted]"`, we lose less signal per stub.
3. `DefaultMaxToolResultChars = 24000` (the hard cap) almost never trips — TokenJuice has already brought a 100 KB `go test` log down to 1.2 KB.

Order to maintain (document this in a comment on `governance.Apply`):

```
tool returns raw stdout
   ↓
tokenjuice.Compact            (per-result, BEFORE inline)
   ↓
WrapUntrustedToolResult       (per-result envelope)
   ↓
limitToolContent              (per-result, runes-based, executor.maxChars)
   ↓
state.AddToolResultMessage    (commit to conversation history)
   ↓
[future turn]
governance.Apply              (whole-slice: orphan/backfill/microcompact/truncate)
   ↓
client.Chat                   (LLM)
```

### internal/agent/tools/registry/*.go

Most tools (`web_fetch`, `search_memory`, `read_source`, `read_skill`, MCP, `ask_user`, `agent_note`, `auth`, `source.*`) do NOT have an exit-code concept and should NOT be modified. TokenJuice receives `ExitCode: nil` from them, which is treated identically to `ExitCode: 0` (spec §5.4) — the failure-preserving branch never triggers, and the engine just runs the normal `generic/fallback` head/tail. That is exactly the desired behaviour.

The **two** tools that DO produce an exit code are `execute_shell` (`registry/exec.go:443`) and `execute_code` (`registry/exec.go:181`). Both already prefix their output with `exit_code: %d\nelapsed_ms: %d\n\n` — we parse that prefix in `exitCodeFromToolOutput` rather than changing the tool surface. Strip the prefix from the `Stdout` we hand to TokenJuice so rules see the raw command output (`go test` rules expect plain `running N tests` at the top, not Aura's framing).

No registry file is edited. The plumbing is one-way: registry produces a string with a known header, the executor parses + strips + Compacts.

### internal/agent/governance/governance.go

**No code edits.** Add a doc comment at the top of `Apply`:

```go
// TokenJuice compaction runs PER tool result in internal/agent/executor.go
// before each result is committed to state. Apply runs on the full message
// slice each LLM turn AFTER all results are committed; it therefore sees
// TokenJuice's compacted payloads, not raw stdout. The two stages are
// complementary: TokenJuice = intelligent per-result shaping; governance =
// whole-conversation hygiene (orphan-drop, backfill, rolling stub, hard cap).
```

That comment is the entire change to governance.

---

## Rule loading strategy

**Recommendation: builtin only for v1. Add user-layer overlay (`~/.config/aura/tokenjuice/rules/`) in v1.1 only if an operator asks.** Drop the upstream project-layer (`<cwd>/.tokenjuice/rules/`) entirely — Aura is single-author and runs as a long-lived daemon, the per-cwd discovery model doesn't fit.

Rationale:

- **Boot cost.** `filepath.WalkDir` on first start adds 1–5 ms per discovered directory + JSON parse. Negligible — but the operational complexity ("why doesn't my rule apply?") isn't.
- **Aura already has overlays.** Prompt overlays (`SOUL.md`, `AGENT.md`, …) and skills (`internal/skills`) are the two operator-facing extension points. A third one for tokenjuice rules without a concrete request creates configuration sprawl.
- **The 10 builtin rules already cover ≥80%** of Aura's `execute_shell` workload per the rules-catalog §4.3.

Implementation sketch (v1):

```go
//go:embed builtin/*.json
var builtinFS embed.FS

var builtinRules = sync.OnceValue(func() []*CompiledRule {
    rules, err := loadFromFS(builtinFS, "builtin")
    if err != nil {
        // Match Aura's "boot non-fatal" philosophy: log and continue with
        // an in-memory fallback synthesized below.
        slog.Default().Warn("tokenjuice: failed loading builtin rules; using synthetic fallback", "err", err)
        return []*CompiledRule{syntheticFallback()}
    }
    return rules
})

func LoadBuiltinRules() []*CompiledRule { return builtinRules() }
```

`syntheticFallback()` constructs a minimal `generic/fallback` rule in code so the engine is always usable even if the embedded FS reads fail. This resolves algorithm-spec Open Question #1 (panic vs synthesize) in favour of synthesize — consistent with Aura's existing "boot non-fatal" philosophy (`cmd/aura/main.go` MCP-server handling, sidecar handling).

---

## Observability

Each `Compact` call emits one structured log line via `slog` (Aura's standard logger), at `debug` level when applied, `debug` when skipped, `warn` only on suspicious outcomes.

```
slog.Debug("tokenjuice.compact",
    "tool", call.Name,
    "rule", tj.RuleID,
    "applied", tj.Applied,
    "bytes_in", tj.Stats.OriginalBytes,
    "bytes_out", tj.Stats.CompactedBytes,
    "chars_in", tj.Stats.OriginalChars,
    "chars_out", tj.Stats.CompactedChars,
    "ratio", tj.Stats.Ratio(),
    "family", tj.Stats.Family,
    "confidence", tj.Stats.Confidence,
)
```

**No payload bytes ever logged** — only stats. This matches the existing Aura rule that tool arguments and tool results are NEVER logged in full (`CLAUDE.md`: "Only tool names and argument *keys* are logged").

WARN cases:
- `tj.Applied && tj.Stats.Ratio() < 0.05` — extreme compaction, possibly over-aggressive rule. Emit `slog.Warn("tokenjuice.aggressive_compaction", ...)` with rule id so we can audit it.
- Rule loading errors (per-rule JSON parse or regex compile failure) — already logged at `warn` inside `compileRule` per spec §1.3 ("logged and silently dropped").

`/api/health` surface (optional v1.1):
- Add a `tokenjuice` block to the existing `/api/maintenance` or `/api/health` JSON: `{ enabled: bool, rules_loaded: int, hourly_bytes_saved: int, hourly_compactions: int }`. The counters live in a single `internal/tokenjuice/metrics.go` with `atomic.Int64` fields; Aura's existing logs-to-stats pipeline (`internal/logging`) can scrape them. Defer this — log lines are sufficient until we want a dashboard widget.

---

## Failure modes + safety

| Failure | Behaviour | Where enforced |
|---|---|---|
| Rule with broken regex | Drop the pattern from `CompiledRule.Compiled`, log `slog.Warn`. Other patterns in the rule still apply. Never abort. | `compileRule` (algorithm spec §1.3, §7) |
| Empty / nil stdout | `buildRawText` returns `""`; `select_inline_text` short-circuits to `"(no output)"` (or `onEmpty` if rule defines it). Result is passthrough. | `reduce.go::buildRawText`, spec §6 |
| Input < `MinInputBytes` (512 by default) | Return `Result{InlineText: in.Stdout, Applied: false, RuleID: "none/too-small"}` immediately. No rules evaluated. | `Compact` entry guard, spec §5.1 |
| `ratio > MinRatio` (0.95 by default) | Return original; keep `RuleID` for debug visibility but `Applied: false`. | `Compact` outer guard, spec §5.1 |
| Panic inside `applyRule` (rule regex pathological, infinite-loop counter) | Per-call `defer recover()`; on panic return `Result{InlineText: in.Stdout, Applied: false, RuleID: "none/panic"}` + `slog.Error("tokenjuice.panic", ...)`. | `Compact` top-level recover |
| Builtin embed FS empty / corrupt | `syntheticFallback()` provides an in-memory `generic/fallback` rule; engine still runs. | `LoadBuiltinRules` (resolved Open Q #1) |
| Forced `ForceRuleID` not found | Fall through to scored classification (per spec §2.3 "deliberate — callers can probe optional rule IDs"). No error. | `classifyExecution` |
| User passes a tool the rule set doesn't know | `generic/fallback` wins with confidence 0.2; head/tail summarisation only. | `classifyExecution` + spec §2.3 |
| File-content tool (`cat`/`head`/`tail`/…) under fallback | Bypass — return raw text. The agent's `read_file` semantics are preserved. | `reduce.go::isFileContentInspectionCommand`, spec §5.5 |

The engine **never** modifies or drops content silently — every guarded skip path returns the original `Stdout` byte-for-byte. This is the same invariant openhuman's `passes_through_incompressible_output` test asserts (`tool_integration.rs:237-252`).

---

## Performance

Aura's per-call budget:

- **p50 tool result:** ~1–10 KB (typical `git status`, `rg`, MCP call). Compact target: **<1 ms**.
- **p99 tool result:** ~100 KB (large `go test` failure log, web_fetch HTML, oversized `kubectl logs`). Compact target: **<5 ms**.
- **Pathological 1 MB input:** **<50 ms**. Any longer and we should consider chunking, but real tool outputs basically never reach 1 MB after Aura's own runtime caps.

The hot path is the grapheme walk in `clampText` / `clampTextMiddle` (spec §8.2, §8.3). Mitigations baked into the Go port from day one:

- Iterate `uniseg.NewGraphemes` with running byte index — do NOT `collect()` to a `[]string` slice (the Rust port allocates ~1 M small strings on a 1 MB input; we sidestep that).
- Hoist git/status post-processor regexes to package-level `var ... = regexp.MustCompile(...)` (spec §7 + §8.2). No per-call recompile.
- Skip `<512 bytes` outright before classification — that's >50% of typical Aura tool results.

Benchmarks to write under `internal/tokenjuice/tokenjuice_test.go`:

```go
func BenchmarkCompactSmall(b *testing.B)   // 1 KB git status, expected <100µs
func BenchmarkCompactMedium(b *testing.B)  // 10 KB go test failure, <500µs
func BenchmarkCompactLarge(b *testing.B)   // 1 MB synthetic line log, <50ms
func BenchmarkCompactPassthrough(b *testing.B) // 200 B output, <1µs (early exit)
func BenchmarkClassifyOnly(b *testing.B)   // classify against 10-rule set, <50µs
```

Add a CI guard (Go's built-in `-benchtime=1x` + `testing.B.ReportMetric`) that flips the build red if `BenchmarkCompactMedium` regresses past 1 ms p99 on a clean runner.

---

## Rollout plan

| ID | Story | Estimate | Deliverable |
|---|---|---|---|
| **US-TJ01** | Package skeleton: types, options, Result, Stats; uniseg dep added; empty `Compact()` that returns passthrough; ≥1 unit test per type | 0.5 day | `internal/tokenjuice/{types.go,tokenjuice.go,tokenjuice_test.go}` compile + pass |
| **US-TJ02** | Classify pipeline: `matchesRule`, `scoreRule`, `classifyExecution`, including stable alphabetical tiebreak + forced-rule fallback. Tests with synthetic 3-rule set. | 0.5 day | `classify.go` + tests reproducing spec §2.3 examples |
| **US-TJ03** | Reduce pipeline + text helpers: `applyRule`, `formatInline`, `selectInlineText`, all of `text.go` (stripANSI / normalize / dedupe / headTail / clamp). Tests against spec §6 edge cases. | 1.5 days | `reduce.go` + `text.go` + tests; algorithm spec §3.8 pseudocode realised |
| **US-TJ04** | Vendor 10 builtin rules + `//go:embed` + git/status post-processor; fixture-driven tests modelled on `docs/tokenjuice-rules-catalog.md` §6 (the three upstream fixtures + the eight Aura-side ones in §6.4) | 1 day | `builtin/*.json` + `postprocess.go` + `tokenjuice_test.go` ≥ 30 cases green |
| **US-TJ05** | Wire into `internal/agent/executor.go` + `internal/agent/exec_helpers.go` behind feature flag `AURA_TOKENJUICE_ENABLED=true` (default ON locally, OFF in CI for first run). Add `argvFromArgs` / `exitCodeFromToolOutput` helpers. End-to-end probe under `cmd/probe_chat` runs `execute_shell git status` and asserts the resulting tool message ≤ 1.2 KB. | 1 day | One commit; flag read in `internal/config`; probe script |
| **US-TJ06** | Observability: structured `slog.Debug` per call; `/api/maintenance` or `/api/health` block (optional) reporting `rules_loaded` + 24h-window `bytes_saved`. Per-process metrics via `atomic.Int64`. | 0.5 day | `metrics.go`, debug log lines, optional health-endpoint patch |
| **US-TJ07** | Measurement: re-run the 21+9 probe suite under `cmd/probe_chat` with TokenJuice ON vs OFF. Record `bytes_in / bytes_out` per turn, write `docs/tokenjuice-measurement-2026-MM-DD.md` with before/after token + latency numbers. | 1 day | Markdown report committed; if median per-heavy-turn savings ≥ 10%, recommend flipping default ON |
| **US-TJ08** | Flip default to enabled if US-TJ07 confirms savings. Otherwise: triage the bottom-N rules + measure again. | 0.5 day | One-line config default flip + a commit message that cites the measurement doc |

**Total: 6.5 days of focused work, 8 days with normal interruption overhead.** Sized to fit a single calm sprint following the "one module per slice, one commit per story" memory rule.

---

## Open questions for the porter — triaged

Spec §10 lists 10 open questions. Decisions for Aura, ordered by when they must be made:

| # | Question | Decision | When |
|---|---|---|---|
| 1 | Panic vs synthesize when `generic/fallback` is missing | **Synthesize** in-memory (boot non-fatal). | **US-TJ04** — must be in the loader from day one |
| 2 | Rule overlay: builtin / user / project | **Builtin only** for v1; user-layer in v1.1 if asked; never project. | **US-TJ04** |
| 3 | Thread-local regex cache vs `MustCompile` package vars | **Package vars** for git/status post-processor regexes; user rule regexes pre-compiled at load. No runtime cache. | **US-TJ04** |
| 4 | `MaxInlineChars` measured in graphemes — should we add a token-aware override? | **Defer.** Document the unit clearly; revisit only if measurement (US-TJ07) shows token-count drift > 20% across model families. | **US-TJ07** |
| 5 | `prettyPrintJson` transform — cost vs latent feature | **Port it** (small, isolated). None of our 10 starter rules use it. Document the latent cost. | **US-TJ04** |
| 6 | `Partial` streaming semantics | **Reserved field, not used.** Aura runs serial today. | **US-TJ01** (just include the field, leave unbranched) |
| 7 | `combinedText` vs `stdout` + `stderr` | **Use `Stdout` only for v1** — Aura's `execute_shell` already concatenates `stderr` into the post-`exit_code:` body. Plumb separate streams when MCP tools start exposing them. | **US-TJ05** |
| 8 | Rule provenance surfaced in UI | **Log only** for v1 (`tokenjuice.compact` slog line with `rule` field). UI surface deferred. | **US-TJ06** |
| 9 | Per-tool `MaxInlineChars` override | **Defer.** One global default. Re-open if a specific tool measurably needs more headroom. | **post-US-TJ07** |
| 10 | What to do with `Confidence` | **Log it.** No hard gating on `confidence < 0.5`. We want signal first, action later. | **US-TJ06** |

---

## What this plan DOESN'T cover

- **Project-layer rule overlay** (`<cwd>/.tokenjuice/rules/`) — Aura is a single-tenant daemon, not a per-project CLI. Deferred indefinitely.
- **Plugin / dynamic rule loading at runtime** — out of scope. Rules ship in the binary or live under one fixed user dir.
- **Streaming compaction** — Aura's tool execution is request/response, not chunked. The `Partial` field is reserved but unused (Open Q #6).
- **HTML-to-markdown** conversion for `web_fetch` results — separate concern. Upstream's `openhuman/providers/gmail/post_process.rs::fast_html_to_text` is a candidate to port separately if/when `web_fetch` output needs structural compaction; TokenJuice's `generic/fallback` head/tail is good enough for v1.
- **`cloud/gh` JSON-table post-processor** — algorithm spec §3.2 step 10 documents this. Skipped in v1; the rule still classifies and head/tail-summarises, just without the porcelain rewrite. Re-add in v1.1 only if `gh` becomes a real Aura workflow.
- **`bun-test` / `vitest` / `jest` rules** — not in the starter 10. Add as soon as Aura's web testing starts running inside `execute_shell`.
- **Per-conversation total-bytes-saved counter** surfaced to the user. Logs cover audit; UI defers to v1.1.
- **License compliance for vendored rule JSONs** — addressed: per rules-catalog §5, we either rewrite the 10 rules in our own words (preferred — full freedom + zero attribution overhead) OR vendor verbatim from upstream MIT under `internal/tokenjuice/builtin/LICENSE-UPSTREAM`. Recommend rewriting given the small starter set; revisit if we ever expand to all 96.

---

**Files referenced for this plan:**
- `D:/Aura/docs/tokenjuice-algorithm-spec.md` (sections §1–§10)
- `D:/Aura/docs/tokenjuice-rules-catalog.md` (sections §1–§7, top-10 list)
- `D:/tmp/openhuman/src/openhuman/tokenjuice/tool_integration.rs` (study-only, GPLv3)
- `D:/Aura/internal/agent/loop.go:213-647` (agent loop driver; no edits needed)
- `D:/Aura/internal/agent/executor.go:69-129` (primary insertion point)
- `D:/Aura/internal/agent/exec_helpers.go:42-157` (secondary insertion point)
- `D:/Aura/internal/agent/governance/governance.go:67-79` (orderly-composition documented, not edited)
- `D:/Aura/internal/agent/tools/registry/exec.go:181, 443` (where `exit_code: %d\n…` prefix originates)
- `D:/Aura/internal/llm/client.go:30-50` (`llm.Message`, `llm.ToolCall` — read-only references)
