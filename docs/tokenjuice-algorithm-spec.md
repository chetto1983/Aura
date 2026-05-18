# TokenJuice Algorithm Specification — for Go port to Aura

**Source studied:** `D:/tmp/openhuman/src/openhuman/tokenjuice/` (Rust port of
[vincentkoc/tokenjuice](https://github.com/vincentkoc/tokenjuice), MIT, 96
vendored rules).
**Analysis date:** 2026-05-18.
**Goal:** terminal-output compaction engine — shrinks verbose tool output (git
status, npm install, cargo test, docker, kubectl, gh, ripgrep, …) before it
hits the LLM context window. The engine is a pure library: no JSON-RPC, no
CLI, no artifact store.

---

## 1. Data structures

All Rust types are decorated `#[serde(rename_all = "camelCase")]` and optional
fields use `#[serde(default)]`. For the Go port: use `json:"toolName,omitempty"`
struct tags so existing rule JSON files load unmodified.

### 1.1 `ToolExecutionInput` (`types.rs:209-241`)

Describes one tool invocation. Only `tool_name` is required.

| Field | Type | Required | Notes |
|---|---|---|---|
| `toolName` | `string` | yes | Agent-level tool name (`"bash"`, `"exec"`, `"shell"`, `"browser_navigate"`, …). |
| `toolCallId` | `*string` | no | Opaque identifier passed through for tracing. |
| `runId` | `*string` | no | Outer agent run ID. |
| `command` | `*string` | no | Free-form shell command. If absent and `argv` is set, `matches_rule` synthesizes one by `argv.join(" ")` (`classify.rs:25-32`). |
| `argv` | `*[]string` | no | Pre-tokenized command vector. If absent and `command` is set, `normalize_execution_input` (`reduce.rs:80-96`) fills it via `tokenize_command`. |
| `args` | `*map[string]any` | no | Original JSON arg blob from the agent — unused by reduction, kept for caller bookkeeping. |
| `cwd` | `*string` | no | Working directory (unused at runtime; informational). |
| `partial` | `*bool` | no | Marks streaming/partial output. |
| `stdout` | `*string` | no | Captured stdout. |
| `stderr` | `*string` | no | Captured stderr. |
| `combinedText` | `*string` | no | If present, OVERRIDES the `stdout + "\n" + stderr` concatenation (`reduce.rs:454-467`). |
| `exitCode` | `*int32` | no | Drives failure-preserving behaviour. `nil` ≡ success. |
| `startedAt` / `finishedAt` / `durationMs` | `*float64` | no | Timing telemetry. |
| `metadata` | `*map[string]any` | no | Caller-side bag. |

JSON shape example:
```json
{
  "toolName": "bash",
  "argv": ["git", "status"],
  "stdout": "On branch main\n…",
  "exitCode": 0
}
```

### 1.2 `JsonRule` — user-facing rule (`types.rs:128-155`)

| Field | Type | Default | Purpose |
|---|---|---|---|
| `id` | `string` | required | `"<family>/<name>"`, e.g. `"git/status"`. Globally unique. |
| `family` | `string` | required | Used by `format_inline` to gate facts-line emission (see §3.5). Examples: `git-status`, `test-results`, `search`, `help`, `dependency-install`, `generic`, `developer-cli`. |
| `description` | `*string` | nil | Documentation only. |
| `priority` | `*int32` | 0 | Specificity boost: multiplied by 1000 in `score_rule`. The only rule using it in the builtin set is `generic/help` (`priority: 25`). |
| `onEmpty` | `*string` | nil | Returned verbatim as the `summary` if filtering removes every line (e.g. `"npm install: ok"`). |
| `matchOutput` | `*[]RuleOutputMatch` | nil | Short-circuit map of regex → canned message. First match wins, evaluated AFTER `strip_ansi` + `pretty_print_json` and BEFORE skip/keep/dedupe (`reduce.rs:508-519`). |
| `counterSource` | `*enum{preKeep,postKeep}` | `postKeep` | Whether counters run before or after `keepPatterns` filter. |
| `match` | `RuleMatch` | required (may be empty) | Classification predicate. |
| `filters` | `*RuleFilters` | nil | `skipPatterns` (drop) + `keepPatterns` (retain). |
| `transforms` | `*RuleTransforms` | nil | `stripAnsi`, `trimEmptyEdges`, `dedupeAdjacent`, `prettyPrintJson` (bool flags). |
| `summarize` | `*RuleSummarize` | head=6 tail=6 (`reduce.rs:625-628`) | Head/tail window sizes. |
| `counters` | `*[]RuleCounter` | nil | Named regex → integer counts surfaced in `CompactResult.facts`. |
| `failure` | `*RuleFailure` | nil | Failure-mode overrides: `preserveOnFailure` (bool) + larger `head`/`tail` defaults (6/12 if unset). |

`RuleMatch` sub-shape (`types.rs:31-50`):

| Field | Type | Semantics |
|---|---|---|
| `toolNames` | `*[]string` | `input.toolName` must be in this list. |
| `argv0` | `*[]string` | `argv[0]` must be in this list. |
| `argvIncludes` | `*[][]string` | ALL groups must match; a group matches when EVERY string in the group is present somewhere in argv. |
| `argvIncludesAny` | `*[][]string` | At least ONE group must fully match (same group semantics as above). |
| `commandIncludes` | `*[]string` | ALL substrings must appear in `command`. |
| `commandIncludesAny` | `*[]string` | At least one substring must appear. |

If a sub-field is `nil`, that dimension is unconstrained. An EMPTY `match: {}`
matches everything (this is how `generic/fallback` works).

`RuleCounter` (`types.rs:91-97`) and `RuleOutputMatch` (`types.rs:102-107`):
both carry `pattern: string` + optional `flags: string`. Flags accept `i`
(case-insensitive) and `m` (multiline). Upstream JS forces a `u` (Unicode)
flag; Rust's regex is Unicode-by-default so `u` is dropped during translation
(`compiler.rs:21-46`). Go's `regexp/syntax` is also Unicode-by-default — same
treatment applies.

### 1.3 `CompiledRule` (`types.rs:184-200`)

Built once at load time. Holds:

- `rule: JsonRule` — original metadata.
- `source: RuleOrigin` — `Builtin | User | Project` enum (for diagnostics).
- `path: string` — filesystem path or `"builtin:<id>"`.
- `compiled: CompiledParts` — pre-built regex slices:
  - `skipPatterns: []*regexp.Regexp`
  - `keepPatterns: []*regexp.Regexp`
  - `counters: []CompiledCounter` (`{name string; pattern *regexp.Regexp}`)
  - `outputMatches: []CompiledOutputMatch` (`{pattern *regexp.Regexp; message string}`)

**Pre-computed at load time:** every regex. Each pattern is compiled once,
errors are logged and the bad regex is silently DROPPED — not fatal
(`compiler.rs:33-45`).

**Pre-computed at runtime (per call):** the `command` fallback string when
only `argv` was provided; the joined `argv` for `argvIncludes` walks. Both are
local-only.

### 1.4 `ReduceOptions` (`types.rs:250-263`)

| Field | Type | Default | Meaning |
|---|---|---|---|
| `classifier` | `*string` | nil | Force a specific rule `id`; bypasses scoring. |
| `maxInlineChars` | `*usize` | **1200** (`reduce.rs:810`) | Hard cap on final inline length, measured in GRAPHEME CLUSTERS — not bytes. |
| `raw` | `*bool` | false | Pass-through; no reduction. |
| `cwd` | `*string` | nil | Project-layer rule discovery root. |

The only knobs that mutate behaviour. There is NO option for head/tail counts,
dedupe, ANSI strip — those live on the rule itself. `tool_integration.rs`
adds two more constants (not exposed via options):
- `MIN_COMPACT_INPUT_BYTES = 512` (`tool_integration.rs:24`) — outputs
  shorter than this are returned untouched.
- `MIN_COMPACT_RATIO = 0.95` (`tool_integration.rs:29`) — if the compacted
  form is more than 95 % of the input, the raw output is kept.

And one internal constant in `reduce.rs:26`:
- `TINY_OUTPUT_MAX_CHARS = 240` — drives the pass-through decision in
  `select_inline_text`.

### 1.5 Output shapes

`ClassificationResult` (`types.rs:281-286`):
- `family: string`
- `confidence: f64` — `1.0` if forced, `0.9` if scored match, `0.2` if
  `generic/fallback` was the winner or no rule matched.
- `matchedReducer: *string` — the winning rule `id`, `nil` only on the
  empty-rule-set path.

`ReductionStats` (`types.rs:272-276`):
- `rawChars: usize` — grapheme count of `strip_ansi(rawText)`.
- `reducedChars: usize` — grapheme count of final `inlineText`.
- `ratio: f64` — `reducedChars / rawChars`, or `1.0` when `rawChars == 0`.

`CompactResult` (`types.rs:291-302`):
- `inlineText: string` — the payload to inline into the LLM message.
- `previewText: *string` — the intermediate `summary` before
  `format_inline`/clamping (used by upstream UIs).
- `facts: *map[string]usize` — counter results; only included when
  non-empty.
- `stats: ReductionStats`
- `classification: ClassificationResult`

---

## 2. Algorithm — classify (rule matching)

Driver: `classify_execution(input, rules, forcedRuleId)` →
`ClassificationResult` (`classify.rs:143-213`).

### 2.1 Predicate `matches_rule(rule, input)` (`classify.rs:21-79`)

Order of checks (short-circuits on first failure):

1. **`toolNames`** — if set, `input.toolName` ∈ list.
2. **`argv0`** — if set, `argv[0]` ∈ list. Missing `argv` is treated as
   `argv[0] = ""`.
3. **`argvIncludes`** — every group must satisfy `includes_all(argv,
   group)` — i.e. EVERY string in the group is found EXACTLY (not a substring,
   `contains` over `Vec<String>`) somewhere in `argv` (`classify.rs:16-18`).
4. **`argvIncludesAny`** — at least one group satisfies `includes_all`.
5. **`commandIncludes`** — every substring is in `command` (substring match
   on the string).
6. **`commandIncludesAny`** — at least one substring is in `command`.

Missing dimensions are ignored (not failing). Command fallback for callers
that only pass `argv`: `command = argv.join(" ")` (`classify.rs:25-32`).
This means `commandIncludes: ["pytest"]` matches when argv is
`["python", "-m", "pytest"]` even with no explicit command.

### 2.2 Scoring `score_rule` (`classify.rs:87-133`)

`score = priority×1000 + |argv0|×100 + Σ|argvIncludes groups|×40 +
Σ|argvIncludesAny groups|×35 + |commandIncludes|×25 +
|commandIncludesAny|×20 + |toolNames|×10`.

Higher = more specific. Stable tiebreaker: alphabetical by `rule.id`
(`classify.rs:184-191`).

### 2.3 Classification flow

```go
func ClassifyExecution(input ToolExecutionInput, rules []*CompiledRule,
                       forcedRuleID *string) ClassificationResult {
    if forcedRuleID != nil {
        if r := findByID(rules, *forcedRuleID); r != nil {
            return ClassificationResult{
                Family: r.Rule.Family, Confidence: 1.0,
                MatchedReducer: &r.Rule.ID,
            }
        }
        // forced ID not found → fall through to normal matching (NOT an error)
    }
    matched := filterMatching(rules, input)        // matches_rule for each
    if len(matched) == 0 {
        return ClassificationResult{Family: "generic", Confidence: 0.2}
        //                            matchedReducer = nil
    }
    sort.SliceStable(matched, func(i, j int) bool {
        si, sj := scoreRule(matched[i]), scoreRule(matched[j])
        if si != sj { return si > sj }
        return matched[i].Rule.ID < matched[j].Rule.ID
    })
    best := matched[0]
    confidence := 0.9
    if best.Rule.ID == "generic/fallback" { confidence = 0.2 }
    return ClassificationResult{
        Family: best.Rule.Family, Confidence: confidence,
        MatchedReducer: &best.Rule.ID,
    }
}
```

**Fallback semantics.** `generic/fallback` has an EMPTY `match: {}` (matches
everything) but score 0, so any non-empty match outranks it. It is sorted
LAST in the compiled rule list (`loader.rs:205-214`) so it never shadows a
specific rule that ties on score. Confidence is forced to `0.2` when it
wins, which downstream callers can use to decide whether to trust the result.

**Forced-rule-not-found behaviour** (`classify.rs:test:454-461`): if
`forcedRuleId` does not exist in the rule set, the function silently falls
through to normal matching. This is deliberate — callers can probe optional
rule IDs without error handling.

---

## 3. Algorithm — reduce (main pipeline)

Driver: `reduce_execution_with_rules(input, rules, opts)` →
`CompactResult` (`reduce.rs:745-856`).

### 3.1 Top-level order of operations

```text
input
  ↓
normalize_execution_input          (synthesize argv from command if needed)
  ↓
build_raw_text                     (combined_text | stdout+stderr | one of them)
  ↓
measured_raw_chars = count_text_chars(strip_ansi(raw_text))      // graphemes
  ↓
classification = classify_execution(...)
  ↓
if opts.raw → return raw verbatim with ratio=1.0
  ↓
if classification == generic/fallback AND argv0 ∈ {cat,sed,head,tail,nl,bat,batcat,jq,yq}
   → return raw verbatim (file-content inspection bypass)              (reduce.rs:778-792)
  ↓
matched_rule := find rules by classification.matchedReducer
               (or fall back to generic/fallback rule object — must be present)
  ↓
apply_rule(matched_rule, input, raw_text) → {summary, facts}     // §3.2
  ↓
compact_text = format_inline(classification, input, summary, facts)  // §3.5
  ↓
selected = select_inline_text(...)                                    // §3.6
  ↓
inline_text = if family=="help" || selected contains '\n'
              then clamp_text_middle(selected, maxInlineChars)
              else clamp_text(selected, maxInlineChars)
  ↓
reduced_chars = count_text_chars(inline_text)
  ↓
return CompactResult{ inline_text, preview=summary, facts, stats, classification }
```

### 3.2 `apply_rule` step-by-step (`reduce.rs:478-645`)

Order (early returns marked `→ RETURN`):

1. **`prettyPrintJson` transform** — if rule sets it, `text =
   pretty_print_json_if_possible(text)` (parses leading `{` or `[`, pretty-prints,
   else leaves text alone). Runs on the WHOLE string before line splitting.
2. **`normalize_lines`** — split on `\n` (after `\r\n → \n`), `trimEnd` each line.
3. **`stripAnsi` transform** — re-normalise after stripping ANSI escapes.
4. **`outputMatches`** — `output_match_text =
   trim_empty_edges(lines).join("\n")`. First entry whose pattern matches →
   `→ RETURN {summary: entry.message, facts: empty}`. NOTE: counters do
   NOT run on this path.
5. **`skipPatterns`** — `lines.retain(!any(pat.is_match(line)))`.
6. **Snapshot for pre-keep counters** — `pre_keep_lines = lines.clone()`.
7. **`keepPatterns`** — if non-empty: keep only lines that match at least one
   pattern, but ONLY IF the resulting set is non-empty (otherwise the rule's
   keep is a no-op for this output; `reduce.rs:542-558`).
8. **`trimEmptyEdges`** — drop blank lines at both ends.
9. **`dedupeAdjacent`** — collapse consecutive duplicates (keeps first).
10. **Rule-specific post-processors** (hard-coded by `rule.id`):
    - `"git/status"` → `rewrite_git_status_lines` (`reduce.rs:198-234`): adds
      section tracking; rewrites `modified:` → `M: <path>`, `new file:` →
      `A:`, `deleted:` → `D:`, `renamed:` → `R:`, `??` → `?? <path>`;
      maps section headers (`"Changes not staged for commit:"` →
      `"Changes not staged:"`); collapses consecutive blanks.
    - `"cloud/gh"` → `rewrite_gh_lines` (`reduce.rs:292-328`): tries to parse
      each non-empty line as a JSON object; if all parse, format as
      `#<id> <title> [<status>] (<branch>) <Nc> {labels} <date>`. Otherwise,
      if `argv[0] == "gh"`, treat as a whitespace-split table.
11. **Counters** — iterate counter regexes over `pre_keep_lines` if
    `counterSource == preKeep`, else over the post-keep `lines`. Insert into
    `facts: map[string]usize`.
12. **`onEmpty`** — if `lines.is_empty()` and rule has `onEmpty`: `→ RETURN
    {summary: rule.onEmpty, facts}`.
13. **Failure-preserving head/tail** —
    - `is_failure := input.exitCode != nil && *input.exitCode != 0`
    - if `is_failure && rule.failure.preserveOnFailure`: use
      `failure.head` (default 6), `failure.tail` (default 12).
    - else: use `summarize.head` (default 6), `summarize.tail` (default 6).
14. **`head_tail`** — keep first N + `"... K lines omitted ..."` + last M
    (only when `lines.len() > head + tail`).
15. **Return** `summary = compacted.join("\n").trim()`, `facts`.

### 3.3 Command tokenization `tokenize_command` (`reduce.rs:33-77`)

Minimal POSIX-ish parser (not a full shell parser):

- Whitespace splits tokens; only outside quotes.
- Single AND double quotes both quote contiguous runs. Quotes do NOT nest.
- Backslash escapes the next character literally (always — even inside
  quotes; this matches the Rust code, slightly looser than POSIX). A trailing
  bare `\\` is preserved as a literal `\\`.
- No variable expansion, no globbing, no command substitution. This is
  deliberately a TOKENIZER, not a SHELL.

For the Go port: write this as `func tokenizeCommand(s string) []string` and
range over `[]rune` to keep multibyte safety. Do NOT reach for
`shlex`-style libraries — the upstream behaviour is the spec, and the
backslash-anywhere rule will diverge from POSIX shlex.

### 3.4 `normalize_execution_input` (`reduce.rs:80-96`)

If `input.argv` is non-empty, return unchanged. Else if `input.command` is
non-empty, tokenize it and set `argv`. If `tokenize_command` yields zero
tokens, leave argv as-is (don't set an empty slice).

### 3.5 `format_inline` (`reduce.rs:669-699`)

Assembles the final inline string from `summary` + `facts`:

1. Collect `factParts := pluralize(count, name)` for every counter with
   `count > 0`. Sort alphabetically.
2. `lines := []`
3. If `exitCode != 0`: prepend `"exit <N>"`.
4. Compute `includeFacts`:
   ```
   includeFacts =
       family == "search"
    OR (family != "git-status" AND family != "help" AND summary.contains("omitted"))
    OR (family == "test-results" AND exitCode != 0)
   ```
   This is the rule that "facts only show up when they add signal" — e.g.
   `git status` already prints the M:/A:/D: lines so a "5 modified files"
   header is redundant; a failing cargo-test on the other hand DOES get the
   `2 failed tests, 2 passed tests` header.
5. If `includeFacts && len(factParts) > 0`: append `strings.Join(factParts,
   ", ")`.
6. Append `summary`.
7. Return `strings.Join(lines, "\n").Trim()`.

### 3.6 `select_inline_text` (`reduce.rs:705-735`)

Decides between the compacted summary and a "passthrough" — a lightly
normalised version of the raw text (`strip_ansi` + `normalize_lines` +
`trim_empty_edges`).

```
if family == "git-status":
    return compact            // always compact for git-status
passthrough = build_passthrough_text(input, raw_text)
                              // prepends "exit N\n" if exitCode != 0;
                              // "(no output)" if normalised text is empty
raw_chars = graphemes(strip_ansi(raw_text))
compact_chars = graphemes(compact_text)
passthrough_limit = (family == "help") ? maxInlineChars : TINY_OUTPUT_MAX_CHARS // 240
if graphemes(passthrough) > passthrough_limit:
    return compact            // passthrough too long
if raw_chars <= maxInlineChars && compact_chars >= raw_chars:
    return passthrough        // compaction would make it LONGER → bail
if graphemes(passthrough) <= compact_chars:
    return passthrough        // raw is already shorter than the compaction
return compact
```

This is the safety net that stops the engine from being WORSE than the input
for short outputs.

### 3.7 Final clamp (`reduce.rs:819-824`)

- If `family == "help"` OR `selected` contains `\n` → `clamp_text_middle`
  (keep 70 % head, 30 % tail, marker `"\n... omitted ...\n"`).
- Else → `clamp_text` (tail-truncate, suffix `"\n... truncated ..."`).

Both work on GRAPHEME clusters, not bytes.

### 3.8 Go-style pseudocode for the full pipeline

```go
func ReduceExecutionWithRules(in ToolExecutionInput, rules []*CompiledRule,
                              opts ReduceOptions) CompactResult {
    in = normalizeExecutionInput(in)
    rawText := buildRawText(in)
    rawChars := countTextChars(stripANSI(rawText))
    cls := ClassifyExecution(in, rules, opts.Classifier)

    if opts.Raw {
        return CompactResult{InlineText: rawText, Stats: ReductionStats{
            RawChars: rawChars, ReducedChars: rawChars, Ratio: 1.0,
        }, Classification: cls}
    }
    if classID(cls) == "generic/fallback" && isFileContentInspectionCommand(in) {
        return CompactResult{InlineText: rawText, Stats: passthrough(rawChars), Classification: cls}
    }

    matched := findRuleByID(rules, classID(cls))
    if matched == nil { matched = findRuleByID(rules, "generic/fallback") }
    if matched == nil { panic("generic/fallback rule must be present") }

    summary, facts := applyRule(matched, in, rawText)
    compact := formatInline(cls, in, summaryOrEmpty(summary), facts)
    maxInline := opts.MaxInlineChars
    if maxInline == 0 { maxInline = 1200 }
    selected := selectInlineText(cls, in, rawText, compact, maxInline)
    var inline string
    if cls.Family == "help" || strings.Contains(selected, "\n") {
        inline = clampTextMiddle(selected, maxInline)
    } else {
        inline = clampText(selected, maxInline)
    }
    reduced := countTextChars(inline)
    ratio := 1.0
    if rawChars > 0 { ratio = float64(reduced) / float64(rawChars) }
    return CompactResult{
        InlineText:  inline,
        PreviewText: nilIfEmpty(summary),
        Facts:       nilIfEmpty(facts),
        Stats:       ReductionStats{RawChars: rawChars, ReducedChars: reduced, Ratio: ratio},
        Classification: cls,
    }
}
```

---

## 4. Text helpers — what each does + ordering

All live under `text/`. Public surface re-exported from `text/mod.rs:1-13`.

| Helper | File:line | Signature | What it does |
|---|---|---|---|
| `strip_ansi(text)` | `text/ansi.rs:29-44` | `(string) → string` | Strips CSI, OSC (complete + incomplete), single-char `ESC <@-_>` escapes, then removes any lone `\x1b` byte. Five Lazy-compiled regexes (`ANSI_CSI`, `ANSI_OSC`, `ANSI_CSI_INCOMPLETE`, `ANSI_OSC_INCOMPLETE`, `ANSI_SINGLE`). |
| `normalize_lines(text)` | `text/process.rs:17-22` | `(string) → []string` | Replaces `\r\n` with `\n`, splits on `\n`, calls `trim_end` on each line. EMPTY lines are preserved (they become `""`). |
| `trim_empty_edges(lines)` | `text/process.rs:29-43` | `([]string) → []string` | Drops blank lines (`trim().is_empty()`) at start and end. Returns empty slice if everything is blank. |
| `dedupe_adjacent(lines)` | `text/process.rs:50-58` | `([]string) → []string` | Collapses consecutive identical lines; keeps the first. |
| `head_tail(lines, head, tail)` | `text/process.rs:66-76` | `([]string, usize, usize) → []string` | If `len ≤ head+tail`: passthrough. Else: first `head` lines + `"... K lines omitted ..."` (K = len-head-tail) + last `tail` lines. |
| `clamp_text(text, maxChars)` | `text/process.rs:115-125` | `(string, usize) → string` | TAIL truncation in grapheme units. Suffix `"\n... truncated ..."`. After cutting to `body_chars` graphemes, calls `trim_head_to_line_boundary` to back up to the last newline (only if that newline is in the SECOND half of the truncated head — otherwise keeps the raw cut). |
| `clamp_text_middle(text, maxChars)` | `text/process.rs:129-148` | `(string, usize) → string` | MIDDLE truncation. Keeps `ceil(0.7 * body)` graphemes of head and the remainder as tail. Marker `"\n... omitted ...\n"`. Both head and tail are aligned to line boundaries (`trim_head_to_line_boundary`, `trim_tail_to_line_boundary`). |
| `pluralize(count, noun)` | `text/process.rs:155-183` | `(usize, string) → string` | English plural. Special cases: if noun already ends in `passed`/`failed`/`skipped` → no inflection. If `count == 1` → singular. Else: sibilant endings (`s,x,z,sh,ch`) → add `es`; consonant + `y` → strip `y` + `ies`; else add `s`. |
| `count_text_chars(text)` | `text/width.rs:18-20` | `(string) → usize` | Counts GRAPHEME CLUSTERS via `unicode-segmentation`. This is what bounds `maxInlineChars`. |
| `count_terminal_cells(text)` | `text/width.rs:62-64` | `(string) → usize` | Sums `grapheme_width` over clusters: emoji and CJK return 2, ASCII 1, combining marks / ZWJ / variation selectors 0. NOT used in the reduce pipeline today — exposed for future width-aware truncation. |
| `graphemes(text)` | `text/width.rs:11-13` | `(string) → []string` | Helper returning the cluster slice. |

### 4.1 Pipeline ordering (rule-driven)

Inside `apply_rule` the canonical order is:

```
prettyPrintJson(string)
  → normalize_lines             (always — to obtain []string)
  → stripAnsi (if rule asks)    (re-normalises after)
  → outputMatches               (→ short-circuit RETURN)
  → skipPatterns
  → snapshot pre_keep_lines
  → keepPatterns                (only commits if non-empty result)
  → trimEmptyEdges (if rule asks)
  → dedupeAdjacent (if rule asks)
  → rule-specific post-processor (git/status, cloud/gh)
  → counters (over pre_keep_lines or lines, per counterSource)
  → onEmpty                     (→ short-circuit RETURN)
  → head_tail                   (failure or normal head/tail)
```

After `apply_rule` the OUTER pipeline does:

```
format_inline                   (prepend "exit N", facts line, summary)
  → select_inline_text          (passthrough vs compact decision)
  → clamp_text_middle | clamp_text
```

### 4.2 Edge cases per helper

- **`normalize_lines`** preserves empty interior lines as `""` — needed
  later for blank-line semantics in `rewrite_git_status_lines`.
- **`trim_empty_edges`** uses `trim().is_empty()` so whitespace-only lines
  also count as blank.
- **`head_tail` boundary** — exactly `head + tail` lines passes through
  unchanged (no marker emitted); `head + tail + 1` already triggers the
  omission marker with `K = 1`.
- **`clamp_text` short input** — when `count_text_chars(text) ≤ maxChars`
  returns `text` verbatim (no copy semantics, but the Rust signature is
  `String` so a `to_owned` happens). For Go: just `return text`.
- **`clamp_text_middle` short input** — same short-circuit.
- **`trim_head_to_line_boundary`** — backs up to last newline ONLY if that
  newline is at position ≥ len/2 (otherwise keeps the raw cut — would
  truncate too aggressively to a single line of head).
- **`trim_tail_to_line_boundary`** — forward to first newline ONLY if it
  is at position ≤ len.div_ceil(2). Mirror of the head rule.
- **`pluralize` edge** — `noun.len() ≥ 2` check before the `[^aeiou]y`
  rewrite guards single-char strings.
- **`strip_ansi` ordering** — OSC then CSI then OSC-incomplete then
  CSI-incomplete then single-char; trailing lone `\x1b` removed via
  `replace`. The "incomplete" regexes are anchored with `$` for streaming
  cases where the trailer was cut mid-sequence (`text/ansi.rs:17-22`).

### 4.3 Grapheme/CJK/emoji preservation

The README claim "CJK, emoji, and other multi-byte text are preserved
grapheme-by-grapheme" is implemented in `text/width.rs`:

- `count_text_chars` (the function used by every limit/clamp) calls
  `text.graphemes(true).count()` (`width.rs:18-20`). So `count_text_chars("中")
  == 1`, `count_text_chars("😀") == 1`, `count_text_chars("e\u{0301}")
  == 1` — composed grapheme clusters are atomic.
- `clamp_text` / `clamp_text_middle` slice with `text.graphemes(true)` and
  reassemble via `concat()`. They never split a grapheme cluster, so a
  family-emoji ZWJ sequence is either kept or dropped whole.
- `count_terminal_cells` is a separate width function (emoji = 2, CJK = 2,
  ASCII = 1, combining marks = 0). It is currently NOT wired into the
  reduce pipeline — it exists for callers that need column-accurate widths
  (e.g. a terminal renderer). The Go port can defer this.

**Go equivalent:** the `golang.org/x/text/unicode/norm` package does NOT do
grapheme clustering; you want
`github.com/rivo/uniseg` (`uniseg.GraphemeClusterCount(s)`,
`uniseg.NewGraphemes(s)`). For width, `uniseg.StringWidth(s)` matches
unicode-width's behaviour closely enough for tool output. **Both packages
are pure-Go, MIT, and have no transitive deps.**

---

## 5. Pass-through safety guarantees

Two layers of safety:

### 5.1 `tool_integration.rs` — outer guard

`compact_tool_output(toolName, arguments, output, exitCode)` is the
caller-facing entry point.

1. **Min input bytes** (`tool_integration.rs:77-93`): if
   `len(output) < MIN_COMPACT_INPUT_BYTES (=512)`, return verbatim with
   `applied=false, rule_id="none/too-small"`. This avoids distorting tiny
   outputs through a rule designed for long logs.
2. **Min ratio** (`tool_integration.rs:114-161`): after reduce runs,
   compute `ratio = compactedBytes / originalBytes`. Keep the compacted
   form ONLY if `ratio ≤ MIN_COMPACT_RATIO (=0.95)` AND `compactedBytes <
   originalBytes` (the second clause guards a `ratio == 1.0` edge from FP
   rounding). Otherwise return the original.
3. **Stats are always reported.** `CompactionStats{ tool_name, original_bytes,
   compacted_bytes, rule_id, applied }`. `rule_id` falls back to `family`
   when no rule matched.

### 5.2 `reduce.rs` — inner guard (`select_inline_text`)

Three "don't make it worse" rules (§3.6):

- **Compaction would make it longer** — if `raw_chars ≤ maxInlineChars` AND
  `compact_chars ≥ raw_chars`, fall back to passthrough.
- **Passthrough is shorter than compact** — pass it through.
- **Passthrough is too long for inline budget** — use compact (the only
  branch that COMMITS to compaction).

### 5.3 Failure preservation

`rule.failure.preserveOnFailure == true` AND `exitCode != 0` switches
`apply_rule` from the normal `summarize.head/tail` to the larger
`failure.head/tail` window (defaults 6/12 if the rule's failure block is
sparse) — see `reduce.rs:612-629`. This is HOW most CLI rules keep error
context: `generic/fallback` uses `failure: { head:12, tail:20 }` against a
normal `summarize: {head:8, tail:8}`.

### 5.4 `exitCode` handling — three independent effects

1. `format_inline` prepends `"exit <N>"` line whenever `exitCode != 0`
   (`reduce.rs:683-685`).
2. `build_passthrough_text` does the same prepend for the passthrough branch
   (`reduce.rs:659-661`).
3. `apply_rule` swaps to failure-mode head/tail when
   `rule.failure.preserveOnFailure`.
4. `format_inline.includeFacts` requires `exitCode != 0` for the
   `test-results` family.

Missing `exitCode` is treated identically to `exitCode == 0` — there is no
"unknown" branch.

### 5.5 File-content inspection bypass

`reduce.rs:99-112` lists `cat sed head tail nl bat batcat jq yq` as
"file-content tools". When `classification.matchedReducer == "generic/fallback"`
AND `argv[0]` (basename) is in this list → return the raw text untouched.
The rationale: head-tail-summarising a `cat file.json` would destroy the
agent's read, and these tools never match a specific rule by design.

---

## 6. Edge cases the code handles

| Case | Handling | Reference |
|---|---|---|
| ANSI escape codes | Five-regex strip covering CSI, OSC (complete + incomplete), single-byte FE-range escapes, lone `\x1b`. | `text/ansi.rs:7-44` |
| Mixed-width text (CJK + ASCII + emoji) | All limits use grapheme counts via `unicode-segmentation`. Char-count and terminal-cell-width are separate functions. | `text/width.rs:18-20`, `width.rs:62-64` |
| Empty input | `build_raw_text` returns `""`; `apply_rule` `lines` becomes `[""]` after `normalize_lines`; `trim_empty_edges` empties it. With `onEmpty` → that string; without → `summary_or_empty` returns `"(no output)"`. | `reduce.rs:454-467`, `reduce.rs:862-873` |
| Tool returns binary | `strip_ansi` will leave most of the bytes; `head_tail` slices line-by-line so it can't split mid-grapheme. There is no explicit binary detector. The min-ratio safety in `compact_tool_output` will usually return the raw if the rule mangles it. | implicit |
| stdout vs stderr separation | `build_raw_text`: if `combined_text` is set, use it. Else if both stdout+stderr non-empty: `stdout + "\n" + stderr`. Else: whichever is non-empty. There is no per-stream rule. | `reduce.rs:454-467` |
| Very long single line (no newlines) | `head_tail` is a no-op (one line ≤ head+tail). `clamp_text` truncates by grapheme to `maxInlineChars - len("\n... truncated ...")`. `trim_head_to_line_boundary` returns the text unchanged because there is no newline. | `text/process.rs:84-95`, `clamp_text` body |
| Adjacent duplicate lines | `dedupeAdjacent` transform → `dedupe_adjacent`. Only adjacent; not global de-dup. | `text/process.rs:50-58` |
| Trailing whitespace | `normalize_lines` calls `trim_end` per line. | `text/process.rs:17-22` |
| Whitespace-only blank lines | `trim_empty_edges` uses `trim().is_empty()`. | `text/process.rs:29-43` |
| Invalid regex in user rule | `compile_rule`/`build_regex` logs a `debug!` and drops the pattern from the compiled set. Never panics. | `compiler.rs:33-45` |
| Forced rule ID not found | Falls through to normal classification — not an error. | `classify.rs:148-162`, test `forced_rule_id_not_found_falls_back_to_matching` |
| Empty rule set | The pipeline asserts `generic/fallback` must exist (`.expect("generic/fallback rule must be present")` `reduce.rs:799`). The loader places it last so this is upheld for all sane sets. The Go port can either panic in the same place or — better — gracefully synthesise an in-memory fallback. |
| Multiple rules tied on score | Alphabetical-by-id tiebreaker. | `classify.rs:184-191` |
| Output that is JSON | `prettyPrintJson` transform parses and reformats; if it doesn't parse, leaves the text alone. `cloud/gh` post-processor parses per-line JSON. | `reduce.rs:437-448`, `292-328` |
| `combined_text` overrides streams | Yes — used by callers who pre-merged. | `reduce.rs:454-457` |
| Streaming / partial output | `partial: bool` flag is stored on `ToolExecutionInput` but the reduce pipeline does NOT branch on it. ANSI strip handles trailing incomplete sequences. | `types.rs:223-224`, `text/ansi.rs:17-22` |
| Tiny output (<512 bytes) | `compact_tool_output` skips the whole pipeline. | `tool_integration.rs:77-93` |
| Compaction not worthwhile (>95%) | Outer guard returns raw. | `tool_integration.rs:114-120` |
| Files-content tool fall-through | `cat`/`sed`/`head`/`tail`/`nl`/`bat`/`batcat`/`jq`/`yq` skip the reducer when fallback would have applied. | `reduce.rs:99-112` |

---

## 7. Go port mapping (Rust → Go)

| Rust idiom | Go equivalent | Notes |
|---|---|---|
| `Option<T>` | `*T` for nullable; pointer-free zero value when "absent ≡ default" | Field-by-field choice. `Option<usize>` for `summarize.head` → `*int` (so a missing value is distinguishable from `0`, which means "no head lines"). |
| `Result<T, E>` | `(T, error)` | Most TokenJuice surface is infallible — only the loader returns errors via panic-or-log. Keep your Go API infallible at runtime; log + drop on bad rules. |
| `once_cell::sync::Lazy<T>` | `sync.Once` + package-level var, or `init()` for unconditional setup | `BUILTIN_RULES` → load once at package init. ANSI regexes → compile via `init()` (they are simple and small). |
| `include_str!("…")` | `//go:embed` with `embed.FS` | `//go:embed vendor/rules/*.json` then `fs.ReadFile(name)`. Keep the `(id, json)` mapping table next to the embed directive. |
| `serde_json::from_str` | `encoding/json.Unmarshal` | Add struct tags `json:"toolName,omitempty"`. CRITICAL: every optional field needs `,omitempty` AND ideally a pointer type to preserve "absent vs zero" semantics required by `unwrap_or(default)` patterns. |
| `regex::Regex` | `*regexp.Regexp` | Rust's `regex` crate disallows lookbehind/lookahead and backreferences; Go's `regexp` (RE2) does too. The vendored patterns work as-is. **Verified by scanning all 96 rule JSONs — no lookaround used.** |
| `regex::Regex::new(pattern)` with `i`/`m` flags | `regexp.Compile("(?im)" + pattern)` | RE2 inline flag syntax matches Rust's. Drop `u` flag if present in upstream JSON (it's a JS-only artefact). |
| `unicode-segmentation::graphemes(true)` | `github.com/rivo/uniseg.NewGraphemes(s)` | Iterator over clusters. `uniseg.GraphemeClusterCount(s)` is the direct `count_text_chars` analogue. |
| `unicode-width::UnicodeWidthChar::width` | `uniseg.StringWidth(s)` (or write your own table of CJK / emoji ranges if you want to vendor nothing) | Only needed for `count_terminal_cells`, which is currently unused by the reduce path. Can defer. |
| `dirs::home_dir()` | `os.UserHomeDir()` | Returns `(string, error)`. Mirror the fallback to `.` on error. |
| `std::fs::read_dir` + recursive walk | `filepath.WalkDir` | Skip symlinks (`d.Type()&fs.ModeSymlink != 0`); filter out `.schema.json` / `.fixture.json`. Sort filenames before processing for determinism (Rust's `sort_by_key(file_name)`). |
| `HashMap<K, V>` | `map[K]V` | Same semantics. Iteration is non-deterministic in both. |
| `Vec<T>` | `[]T` | Capacity hints (`with_capacity`) → `make([]T, 0, n)` when worth it. |
| `thread_local!` regex cache (`reduce.rs:880-898`) | `sync.Map` or a per-goroutine cache via context | Caches compiled regex by pattern string. For Go, a global `sync.Map[string]*regexp.Regexp` is simplest. Or pre-compile the hard-coded git-status regexes at `init()` and drop the cache entirely (those patterns are static — only the user-supplied rule patterns are dynamic, and those are compiled at load time). |
| `log::debug!` / `log::info!` | `zap.SugaredLogger.Debugf` (Aura's standard via `internal/logging`) | TokenJuice logs cumulative byte counts and rule ids, never payload bytes. Mirror that — never log compaction inputs. |
| `dhat-rs` profiling | `go test -memprofile=mem.out` + `go tool pprof` | The Rust port flags potential allocators; we use Go's built-ins. |
| `serde_yaml` | NOT NEEDED — TokenJuice rules are JSON only | The Rust crate is listed in the workspace `Cargo.toml` for unrelated code. |

### 7.1 Rust regex syntax that COULD bite

Scanned all 96 vendored rules for syntax that Go's RE2 rejects. None of them
use:

- Lookbehind / lookahead (`(?=…)`, `(?<=…)`)
- Backreferences (`\1`, `\k<name>`)
- Possessive quantifiers (`a++`)
- Atomic groups (`(?>…)`)
- Unicode property escapes other than implicit `u` (e.g. `\p{Letter}`)

Patterns that DO appear and need attention:

- `^...$` with multiline flag — emit as `(?m)` prefix; RE2 supports this.
- Non-capturing groups `(?:…)` — supported.
- Character classes `[ MADRCU?!]{2}` — supported.
- Inline alternation `error|warn|binary file` — supported.
- Escaped double quotes inside JSON strings `(?:use \"git .+\")` — JSON
  decoder strips the backslash; the resulting Go regex `(use "git .+")`
  compiles cleanly.

Conclusion: **straight port. No syntax translation layer required.**

### 7.2 Suggested Go package layout

```
internal/tokenjuice/
├── tokenjuice.go              // public surface: Reduce(), CompactToolOutput()
├── types.go                   // ToolExecutionInput, ReduceOptions, CompactResult, ReductionStats, ClassificationResult
├── rules/
│   ├── rule.go                // JsonRule, CompiledRule, RuleOrigin, RuleMatch, …
│   ├── compile.go             // CompileRule + buildRegex
│   ├── load.go                // LoadRules, LoadBuiltinRules, LoadRuleOptions, three-layer overlay
│   ├── builtin.go             // embed.FS + (id, path) table
│   └── vendor/rules/*.json    // 96 vendored files, untouched
├── classify.go                // matchesRule, scoreRule, ClassifyExecution
├── reduce.go                  // reduceExecutionWithRules, applyRule, formatInline, selectInlineText, helpers
├── tokenize.go                // tokenizeCommand, normalizeExecutionInput, isFileContentInspectionCommand
├── text/
│   ├── ansi.go                // StripANSI + compiled regex package vars
│   ├── lines.go               // NormalizeLines, TrimEmptyEdges, DedupeAdjacent, HeadTail, Pluralize
│   ├── clamp.go               // ClampText, ClampTextMiddle, trim*ToLineBoundary
│   └── width.go               // CountTextChars, Graphemes (uniseg wrapper)
└── integration.go             // CompactToolOutput + CompactionStats + extractCommandArgv
```

Wire into Aura at `internal/chat/agentloop.go` (or wherever tool results are
captured) by replacing `result := <raw tool output>` with
`compacted, stats := tokenjuice.CompactToolOutput(toolName, arguments,
result, exitCode)`.

---

## 8. Performance notes from the code

### 8.1 What the Rust port already mitigates

1. **Regex precompilation** at rule-load time (`compiler.rs:50-122`). Per-call
   cost is `O(rules * filters)` regex `is_match` invocations, not `Regex::new`.
2. **Thread-local regex cache** for the ad-hoc patterns inside
   `rewrite_git_status_line` (`reduce.rs:880-898`). These patterns are NOT
   on the rule JSON — they are baked into the post-processor and could
   otherwise be recompiled on every line. The Go port should hoist them to
   `var gitStatusModifiedRe = regexp.MustCompile(…)` package-level vars
   instead of replicating the cache.
3. **Lazy builtin rules** — `BUILTIN_RULES: Lazy<Vec<CompiledRule>>` in
   `tool_integration.rs:31`. Go: load once in `init()` or via
   `sync.OnceValue`.
4. **Pass-through skip** when bytes < 512 — never even hits the classifier
   for tiny tool calls.

### 8.2 Known footguns to watch in the Go port

- **`String.replace_all` on regex chains in `strip_ansi`** allocates per
  call. For Aura's load this is fine (one call per tool result, output is
  capped at ~MB), but the chain currently makes FIVE passes over the
  string. A single-pass byte-level state machine would be faster — defer
  this until profiling says it matters.
- **`text.graphemes(true).collect()` to a `Vec<&str>`** in `clamp_text` /
  `clamp_text_middle` allocates the full grapheme slice. For a 1 MB input
  that is ~1 M small allocations through the segmenter. The Go port can
  iterate `uniseg.NewGraphemes` and tally a running byte index for the cut
  — no intermediate slice needed. This is the single most allocation-heavy
  spot in the whole engine.
- **`HashMap::insert` in `overlay_and_sort`** is fine. The README's
  `html2md` cautionary tale (894 MB allocation for 10 KB HTML input) does
  NOT apply to TokenJuice — there are no recursive walkers, no
  backtracking regexes, no string-concatenation loops in the hot path.

### 8.3 Expected runtime budget (Aura)

For a typical agent loop step:
- 1–3 tool calls per step.
- Tool outputs 0–500 KB.
- 96 builtin rules → 96 `matches_rule` predicate evaluations per call.
  Each predicate is at most 6 short-circuited list lookups → < 1 µs total
  on modern hardware.
- Regex passes per call: O(lines) × O(skip + keep + counter patterns) for
  the matched rule, typically 4–8 patterns × few hundred lines. Microseconds.
- Grapheme walk: dominant cost. ~10 MB/s for `uniseg` on the typical line
  ratio → a 1 MB tool output costs ~100 ms. **This is the budget item that
  matters.** If Aura sees this in profiling, switch the clamp loops to
  byte-index iteration (see §8.2).

---

## 9. Compaction examples — what shrinks how much

Drawn from fixtures and rule tests in the upstream Rust crate
(`tests/fixtures/*.json` and `reduce_tests.rs`).

| Scenario | Rule | Before (chars) | After (chars) | Ratio | Source |
|---|---|---|---|---|---|
| `git status` with 1 modified file | `git/status` | 134 (5 lines incl. blanks + hint) | 25 (2 lines: `"Changes not staged:\nM: src/foo.rs"`) | ~0.19 | `tests/fixtures/git_status_modified.fixture.json` |
| `git status` with 50 modified files | `git/status` | ~2000 (51 lines `\tmodified:   src/file_<i>.rs` + header) | ~250 (`head=10 tail=4` window + `... omitted ...`) | ~0.12 | `tool_integration.rs:222-234` test `compacts_long_git_status_via_argv` |
| `cargo test` failure (3 tests, 1 panic) | `tests/cargo-test` | 471 (15 lines) | 348 (`"exit 1\n2 failed tests, 2 passed tests\n…"`, full failure block preserved by `failure.head=18 tail=18`) | ~0.74 | `tests/fixtures/cargo_test_failure.fixture.json` |
| `npm install` clean re-run | `install/npm-install` (`matchOutput`) | thousands of chars of `up to date, audited 412 packages…` | `"npm install: up to date"` (canned) | <0.01 | `vendor/rules/install__npm-install.json` matchOutput rule |
| Unknown tool, 20 lines × ~8 chars | `generic/fallback` | 142 chars | 134 chars (head 8 + omit marker + tail 8 = 17 lines incl. marker) | ~0.94 | `tests/fixtures/fallback_long_output.fixture.json` |
| Short (<512 B) anything | (skipped) | N | N | 1.00 | `tool_integration.rs:77-93` |
| Anything where compact ≥ raw | (skipped) | N | N | 1.00 | `reduce.rs:705-735` |
| `cargo test` all-green, 100 tests | `tests/cargo-test` | several KB | head 12 + tail 10 lines | typical ~0.10–0.20 | inferred from rule windows |

Generalisations:

- **`matchOutput` rules** (npm install clean, git status clean) → 99 %+
  reduction.
- **Specific CLI rules with `keepPatterns`** (search/rg, pytest) → 50–90 %
  reduction depending on signal density.
- **Specific CLI rules with `summarize` only** (cargo test, git status) →
  60–90 % on long outputs, 0 % (passthrough) on short ones.
- **`generic/fallback`** → 50–90 % on outputs > 16 lines, 0 % on shorter.

---

## 10. Open questions for the Go port author

Decide these deliberately — they are NOT mechanical translations.

1. **`generic/fallback` assertion at runtime.** Rust panics via `.expect` if
   the rule list is missing the fallback (`reduce.rs:799`). Aura's broader
   design philosophy is "boot non-fatal"; do we mirror that and
   gracefully synthesise an in-memory fallback rule, or panic on startup at
   the loader to make the misconfiguration loud?
2. **Three-layer rule overlay scope.** Builtin + user (`~/.config/tokenjuice/rules/`)
   + project (`<cwd>/.tokenjuice/rules/`) — does Aura want any of these,
   or builtin only? Aura's wiki and skills already give the user
   editable behaviour overlays; another disk-discovery surface adds boot
   cost and a new "where do I put it" question. **Default recommendation:
   builtin only for v1, add user-layer when a real use case appears.**
3. **`thread_local!` regex cache vs package-level vars.** The Rust cache
   exists because the git-status post-processor builds regexes from string
   literals at call sites. The Go idiomatic move is `var (gitModifiedRe =
   regexp.MustCompile(…))` package vars — but that bakes the patterns into
   binary code, not the JSON rule file. Pick: stay literally true to
   upstream (pattern string still appears in source) or migrate to
   `regexp.MustCompile`-on-init (faster, but a small divergence).
4. **`countTextChars` measures graphemes; tokens are bytes.** Upstream
   measures char limits in grapheme clusters. The LLM context budget is in
   tokens (not chars and not bytes). Should `maxInlineChars` be renamed
   `MaxInlineGraphemes` and a separate token-aware override be threaded in
   later? Recommendation: keep `MaxInlineChars=1200` as the default,
   document the unit clearly, and let callers tune per model.
5. **`prettyPrintJson` cost.** The transform parses the whole output as
   JSON before deciding. For a 1 MB log that starts with `[` but is not
   JSON, you eat the full parse cost. None of the 96 builtin rules set
   `prettyPrintJson: true`, so this is a latent feature. Decision: port it,
   but document the cost; or omit until a rule needs it.
6. **`partial: bool` semantics.** The field is on `ToolExecutionInput` and
   serialised, but the reduce pipeline does NOT branch on it. If Aura
   wants to stream-compact during a tool execution (rather than after
   completion), we need to define what `partial` should do — probably
   "skip outputMatches and skip onEmpty" so we don't lock in a verdict
   early. Decision: copy the field through but document it as reserved
   until streaming is in scope.
7. **`combinedText` vs `stdout`+`stderr`.** Today Aura tools return one
   string blob, not stdout/stderr separately. Wiring TokenJuice in is
   trivial via `combinedText`, but if we want failure-mode rules to gain
   signal from stderr-only, we'd need to plumb the two streams. Decision:
   v1 wires `combinedText`, future work splits streams once tool plumbing
   supports it.
8. **Rule provenance UI.** The Rust `CompactionStats` reports
   `rule_id`. Should Aura surface that to the user (e.g. "compacted by
   `git/status`, 12 KB → 1.2 KB")? Useful for trust, but only meaningful
   when the user can do something with it. Probably yes, as a
   structured-log field, not in the visible reply.
9. **Per-tool override of `maxInlineChars`.** A `gh issue list` reply that
   matters fully may need 4 KB; a generic `ls` reply almost never does.
   Today there is one global default. Decision: ship one knob, instrument
   to see if differentiation is needed.
10. **What to do with `confidence`.** Today nothing downstream consumes
    the field. Two options: drop it from the surface; OR feed it to
    `tool_integration.applied` as a hard "don't compact if confidence
    < 0.5" guard. The upstream Rust ignores it. We should at minimum log
    it so we can tell when the engine fell to fallback.

---

## Plain-English summary

TokenJuice is a deterministic, rule-driven compactor for verbose CLI tool
output. The agent runs a command (`git status`, `npm install`, `cargo test`,
`gh pr list`, …), captures stdout plus an exit code, and hands the blob to
the engine. The engine first classifies the call by walking a list of
JSON-defined rules — each rule says "I apply when the tool is `exec` and
`argv[0]` is `cargo` and `argv` contains `test`" — picks the highest-scoring
match (specificity wins, ties break alphabetically), and falls back to a
generic head/tail rule when nothing fits. The matched rule then drives a
fixed pipeline: strip ANSI escapes, normalise line endings, optionally
short-circuit on a canned message (`"npm install: ok"`), drop noise lines
matching `skipPatterns`, keep only lines matching `keepPatterns`, dedupe
adjacent duplicates, optionally run a rule-specific post-processor (the
two hard-coded ones rewrite `git status` to compact `M:`/`A:`/`D:` form and
parse `gh` JSON tables), count named-pattern occurrences for the facts
header, and finally keep the first N + an omission marker + last M lines —
where N/M are bigger when the command failed. The result is glued together
with an optional `exit N` prefix and the facts header, then passed through
a "don't make it worse" safety check that bails to the raw text if compaction
didn't shrink things meaningfully, and finally clamped to the inline budget
(1200 graphemes by default) with line-aware truncation that respects CJK,
emoji, and combining-mark boundaries. An outer wrapper skips the whole thing
for outputs under 512 bytes or where compaction stayed above 95 % of the
original.
