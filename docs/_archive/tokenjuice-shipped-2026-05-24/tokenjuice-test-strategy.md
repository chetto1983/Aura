# TokenJuice Aura port — Test strategy + safety analysis

**Analysis date:** 2026-05-18
**Target package:** `internal/tokenjuice/` (Go port of `D:/tmp/openhuman/src/openhuman/tokenjuice/`)
**Integration site:** `internal/agent/loop.go` (and/or `internal/agent/toolexec.go`)
**Source materials reviewed:**
- `D:/tmp/openhuman/src/openhuman/tokenjuice/reduce_tests.rs` (66 unit tests)
- `D:/tmp/openhuman/src/openhuman/tokenjuice/text_tests.rs` (28 unit tests — strip_ansi, clamp, dedupe, 3-layer overlay, invalid-regex)
- `D:/tmp/openhuman/src/openhuman/tokenjuice/tests/fixtures/*.json` (3 fixtures)
- `docs/tokenjuice-algorithm-spec.md`, `docs/tokenjuice-rules-catalog.md`
- `cmd/probe_chat/cases.go` (Setup / Verify / Cleanup pattern)
- `.agents/skills/aura-qa-pipeline/SKILL.md` (8 discipline rules)
- `CLAUDE.md` (VALIDATE WITH VERIFIED BENCHMARKS; TESTS VERIFY QUALITY AND METRICS)

The goal of this document is to give the Go port author a complete and executable test plan: every layer, every test name, every probe ground-truth assertion, the safety dimensions, the rollback diff, and the post-ship measurement criterion. Nothing here is aspirational — every test maps to a function or behaviour described in the algorithm spec or the upstream Rust suite.

---

## Test pyramid

Four layers, allocated by ROI of the layer × cost-to-author × signal-on-failure. The percentages refer to test *count*, not lines of code.

| Layer | % of total | Tooling | What it catches |
|---|---|---|---|
| 1. Unit tests | **60 %** | `go test ./internal/tokenjuice/...` | Algorithm correctness per function — regex compilation, grapheme math, pluralize, head_tail, classify scoring, select_inline_text branch table. Cheapest tier; ~50 ms per package run; no Aura runtime required. Mirrors the 94 Rust tests in `reduce_tests.rs` + `text_tests.rs`. |
| 2. Fixture tests | **20 %** | `go test ./internal/tokenjuice/...` table-driven | End-to-end pipeline against frozen golden outputs. Catches regressions where a unit-level change ripples through the apply_rule pipeline (filter → dedupe → post-processor → counter → onEmpty → head_tail → format_inline → select_inline_text → clamp). The 3 upstream fixtures + 3-5 Aura-specific additions. |
| 3. Integration tests | **10 %** | `go test ./internal/agent/...` with stub LLM client | Wiring into the agent loop. Catches "compaction never runs", "compaction shrinks the wrong message", "stats not recorded", "feature flag ignored". |
| 4. E2E probes | **10 %** | `cmd/probe_chat -case=<name>` against live `/api/chat` | Real-world behavioural validation per Aura's Q&A discipline. Asserts that an Aura turn that invokes a verbose tool actually produces a compacted result in the persisted `tool_attempts` row AND that the user-visible reply still contains the semantic content. |

**Justification of the split:**

- Unit tests dominate because TokenJuice is a *pure library* with a fixed, well-specified algorithm and zero I/O. Every algorithmic invariant maps cleanly to a unit test; that's where the bug-discovery yield per test-author hour is highest.
- Fixture tests are the natural format for golden-output validation but bear higher maintenance cost (one upstream behaviour change = many fixtures to regen). 20 % is enough to lock the pipeline behaviour without taxing maintenance.
- Integration tests are intentionally thin: TokenJuice's wiring surface is ~50 lines (one call site in `loop.go` per tool result; one env-flag check; one stats sink). More integration tests than that = duplicated unit coverage.
- E2E probes are the most expensive (real LLM, ~10-30 s per case), so we ration them. The goal of an E2E probe is NOT to test the algorithm — it is to prove the *integration* is real: that a tool result that hits the LLM context window is shorter than the same result without TokenJuice. Three probes (happy, passthrough, disabled) suffice; we add more only if a regression is observed.

---

## Unit tests — per function

Package layout follows the suggested `internal/tokenjuice/` tree from `tokenjuice-algorithm-spec.md` §7.2. Test names below are the literal Go test function names; each is a fresh `func TestXxx(t *testing.T)` so failures are individually addressable.

### 1. `text/ansi.go` — `StripANSI`

Signature: `func StripANSI(text string) string`

Happy path:
- `TestStripANSI_BasicCSI` — `"\x1b[32mhello\x1b[0m"` → `"hello"`
- `TestStripANSI_OSC` — `"\x1b]0;title\x07rest"` → `"rest"`
- `TestStripANSI_SingleByteEscape` — `"\x1b=foo"` → `"foo"`

Edge:
- `TestStripANSI_Empty` — `""` → `""`
- `TestStripANSI_OnlyEscapes` — `"\x1b[0m\x1b[1m\x1b[31m"` → `""`
- `TestStripANSI_IncompleteCSI` — `"green\x1b[3"` (stream cut mid-sequence) → `"green"`
- `TestStripANSI_IncompleteOSC` — `"label\x1b]0;title"` (no BEL terminator) → `"label"`
- `TestStripANSI_LoneEscByte` — `"text\x1b"` → `"text"`
- `TestStripANSI_CJKIntact` — `"\x1b[32m中文\x1b[0m"` → `"中文"`
- `TestStripANSI_EmojiIntact` — `"\x1b[1m😀 hi\x1b[0m"` → `"😀 hi"`
- `TestStripANSI_ZWJSequence` — family emoji `U+1F468 U+200D U+1F469 U+200D U+1F467` inside CSI wrappers must survive whole
- `TestStripANSI_MultiByteOnlyNoEscapes` — `"こんにちは"` returned verbatim
- `TestStripANSI_MixedScripts` — Arabic + CJK + Latin + emoji wrapped in CSI

Negative:
- `TestStripANSI_NeverPanicsOnRandomBytes` — fuzz-style: 1 KB of `rand.Read` bytes must not panic
- `TestStripANSI_PreservesNonANSIControlChars` — `"\thello\nworld\r"` retains tab/newline/CR (only ESC-prefixed sequences are stripped)

### 2. `text/lines.go` — `NormalizeLines`

Signature: `func NormalizeLines(text string) []string`

- `TestNormalizeLines_Basic` — `"a\nb\nc"` → `[a b c]`
- `TestNormalizeLines_CRLF` — `"a\r\nb\r\n"` → `[a b ""]`
- `TestNormalizeLines_TrimEndPerLine` — `"a  \nb\t\n"` → `[a b]`
- `TestNormalizeLines_PreservesInteriorEmpty` — `"a\n\nb"` → `[a "" b]` (interior blanks survive)
- `TestNormalizeLines_TrailingNewlineProducesEmpty` — `"a\n"` → `[a ""]`
- `TestNormalizeLines_Empty` — `""` → `[""]`
- `TestNormalizeLines_OnlyNewlines` — `"\n\n"` → `["" "" ""]`

### 3. `text/lines.go` — `TrimEmptyEdges`

Signature: `func TrimEmptyEdges(lines []string) []string`

- `TestTrimEmptyEdges_Basic` — `["" a b ""]` → `[a b]`
- `TestTrimEmptyEdges_WhitespaceOnlyCountsAsBlank` — `["   " a "\t" ""]` → `[a]`
- `TestTrimEmptyEdges_AllBlank` — `["" "  "]` → `[]`
- `TestTrimEmptyEdges_NoEdges` — `[a b]` → `[a b]`
- `TestTrimEmptyEdges_PreservesInteriorBlank` — `[a "" b]` → `[a "" b]`
- `TestTrimEmptyEdges_NilInput` — `nil` → `nil` (or `[]`; assert no panic + non-nil-or-empty)

### 4. `text/lines.go` — `DedupeAdjacent`

Signature: `func DedupeAdjacent(lines []string) []string`

- `TestDedupeAdjacent_CollapsesRun` — `[a a a b]` → `[a b]`
- `TestDedupeAdjacent_PreservesNonAdjacent` — `[a b a]` → `[a b a]`
- `TestDedupeAdjacent_SingleElement` — `[only]` → `[only]`
- `TestDedupeAdjacent_AllIdentical` — `[x x x x x]` → `[x]`
- `TestDedupeAdjacent_EmptyLinesDeduplicated` — `[a "" "" b "" c]` → `[a "" b "" c]`
- `TestDedupeAdjacent_MultibyteLines` — `[日本語 日本語 日本語]` → `[日本語]`
- `TestDedupeAdjacent_Nil` — `nil` → `[]` (no panic)

### 5. `text/lines.go` — `HeadTail`

Signature: `func HeadTail(lines []string, head, tail int) []string`

- `TestHeadTail_Basic20Lines` — 20 lines, head=8 tail=8 → 8 + marker + 8 = 17 lines; marker text `"... 4 lines omitted ..."`
- `TestHeadTail_ExactBoundary` — `len == head+tail` → passthrough, no marker (e.g. 16 lines with 8/8)
- `TestHeadTail_OneOverBoundary` — `len == head+tail+1` → 8 + `"... 1 line omitted ..."` + 8 (singular)
- `TestHeadTail_ZeroHead` — head=0 tail=2 over 5 lines → `[marker, line3, line4]`
- `TestHeadTail_ZeroTail` — head=2 tail=0 over 5 lines → `[line0, line1, marker]`
- `TestHeadTail_HeadPlusTailExceedsLen` — head=5 tail=5 over 3 lines → passthrough unchanged
- `TestHeadTail_EmptyInput` — `[]` → `[]`
- `TestHeadTail_SingularPluralization` — exactly 1 line omitted uses singular `"line"` (not `"lines"`)

### 6. `text/clamp.go` — `ClampText` and `ClampTextMiddle`

Signatures:
- `func ClampText(text string, maxChars int) string`
- `func ClampTextMiddle(text string, maxChars int) string`

For `ClampText` (tail-truncate, suffix `"\n... truncated ..."`):
- `TestClampText_ShortInputPassesThrough` — len < maxChars → verbatim
- `TestClampText_ExactBoundary` — len == maxChars → verbatim
- `TestClampText_TruncatesAndAppendsMarker`
- `TestClampText_BacksUpToLineBoundary_WhenInSecondHalf` — newline at ≥ len/2 → cut at newline
- `TestClampText_KeepsRawCut_WhenNewlineInFirstHalf` — newline before len/2 → no backup
- `TestClampText_NoNewlineInTruncatedHead` — single long line, no newline → raw grapheme cut
- `TestClampText_GraphemeSafeCJK` — 500 CJK chars, max 100, output count_text_chars ≤ 100 + marker overhead and never contains partial codepoint
- `TestClampText_GraphemeSafeEmoji` — 500 emoji, max 50, all output graphemes are whole emoji
- `TestClampText_MaxCharsZero` — max=0 → just the marker (or empty; assert no panic)

For `ClampTextMiddle` (middle truncate, marker `"\n... omitted ...\n"`, 70/30 split):
- `TestClampTextMiddle_ShortInputPassesThrough`
- `TestClampTextMiddle_InsertsOmissionMarker` — long input contains `"omitted"`
- `TestClampTextMiddle_HeadTailRatio70_30` — for 1000 chars max 200, head section ≈ ceil(0.7 × body)
- `TestClampTextMiddle_GraphemeSafeCJK` — never splits inside multi-byte cluster (re-implement Rust's `clamp_text_middle_output_is_valid_utf8` by asserting every output grapheme appears whole in source)
- `TestClampTextMiddle_GraphemeSafeEmoji` — emoji of length 100, max 20, output graphemes are all complete `"😀"` (or marker chars)
- `TestClampTextMiddle_GraphemeSafeZWJ` — family ZWJ sequence not split into partial cluster
- `TestClampTextMiddle_HeadAlignsToLineBoundary`
- `TestClampTextMiddle_TailAlignsToLineBoundary`
- `TestClampTextMiddle_GraphemeCountRespectsLimit` — count ≤ 2×max (lenient bound for marker)

### 7. `text/width.go` — `CountTextChars`, `Graphemes`

Signatures:
- `func CountTextChars(text string) int`
- `func Graphemes(text string) []string`

- `TestCountTextChars_ASCII` — `"hello"` → 5
- `TestCountTextChars_CJK` — `"中"` → 1
- `TestCountTextChars_Emoji` — `"😀"` → 1
- `TestCountTextChars_CombiningMark` — `"é"` → 1
- `TestCountTextChars_ZWJSequence` — family emoji → 1
- `TestCountTextChars_Empty` — `""` → 0
- `TestGraphemes_RoundTripCount` — `len(Graphemes(s)) == CountTextChars(s)` for mixed-script string
- `TestGraphemes_NeverSplitsCluster` — for `"é"`, `Graphemes` returns 1 element of length 3 bytes

### 8. `text/pluralize.go` — `Pluralize`

Signature: `func Pluralize(count int, noun string) string`

- `TestPluralize_OneIsSingular` — `Pluralize(1, "file")` → `"1 file"`
- `TestPluralize_ZeroIsPlural` — `Pluralize(0, "file")` → `"0 files"`
- `TestPluralize_AddsS` — `Pluralize(3, "file")` → `"3 files"`
- `TestPluralize_AddsES_Sibilant_s` — `Pluralize(2, "miss")` → `"2 misses"`
- `TestPluralize_AddsES_Sibilant_x` — `Pluralize(2, "box")` → `"2 boxes"`
- `TestPluralize_AddsES_Sibilant_z` — `Pluralize(2, "buzz")` → `"2 buzzes"`
- `TestPluralize_AddsES_sh` — `Pluralize(2, "wish")` → `"2 wishes"`
- `TestPluralize_AddsES_ch` — `Pluralize(2, "match")` → `"2 matches"`
- `TestPluralize_ConsonantYRewrite` — `Pluralize(2, "city")` → `"2 cities"`
- `TestPluralize_VowelYNoRewrite` — `Pluralize(2, "key")` → `"2 keys"`
- `TestPluralize_PassedSpecialCase` — `Pluralize(5, "passed")` → `"5 passed"` (already inflected)
- `TestPluralize_FailedSpecialCase` — `Pluralize(5, "failed")` → `"5 failed"`
- `TestPluralize_SkippedSpecialCase` — `Pluralize(5, "skipped")` → `"5 skipped"`
- `TestPluralize_PassedTest_Compound` — `Pluralize(2, "passed test")` → `"2 passed tests"` (last word `test` inflects; the rule is "trailing word ends in passed/failed/skipped")
- `TestPluralize_EmptyNoun` — `Pluralize(2, "")` → `"2 "` (no panic)
- `TestPluralize_SingleCharNoun` — `Pluralize(2, "y")` → `"2 ys"` (length-2 guard avoids consonant-y rewrite)

### 9. `tokenize.go` — `TokenizeCommand`

Signature: `func TokenizeCommand(s string) []string`

- `TestTokenizeCommand_Basic` — `"git status --short"` → `[git status --short]`
- `TestTokenizeCommand_DoubleQuoted` — `echo "hello world"` → `[echo "hello world"]` (one token)
- `TestTokenizeCommand_SingleQuoted` — `echo 'hello world'` → `[echo "hello world"]`
- `TestTokenizeCommand_BackslashEscapesSpace` — `echo hello\ world` → `[echo "hello world"]`
- `TestTokenizeCommand_BackslashInsideQuotes` — `echo "a\\b"` → `[echo "a\\b"]` (escape is literal — diverges from POSIX, matches Rust)
- `TestTokenizeCommand_TrailingBackslash` — `echo hello\\` → `[echo "hello\\"]` (preserved literally)
- `TestTokenizeCommand_Empty` — `""` → `[]`
- `TestTokenizeCommand_OnlyWhitespace` — `"   "` → `[]`
- `TestTokenizeCommand_EmptyQuotes` — `"''"` → `[]` (quotes contain nothing → no token emitted)
- `TestTokenizeCommand_MultibyteUnquoted` — `日本語 中文` → `[日本語 中文]`
- `TestTokenizeCommand_NestedQuotesNotAllowed` — `"echo "a"b"c"` parsed as defined-by-Rust behaviour (capture expected output)

### 10. `tokenize.go` — `NormalizeExecutionInput`, `IsFileContentInspectionCommand`

- `TestNormalizeExecutionInput_FillsArgvFromCommand`
- `TestNormalizeExecutionInput_SkipsWhenArgvPresent`
- `TestNormalizeExecutionInput_NoOpWhenEmptyCommand`
- `TestNormalizeExecutionInput_NoOpWhenTokenizeYieldsEmpty` — command `"''"` keeps argv nil
- `TestIsFileContentInspection_Cat` — argv `[cat foo.txt]` → true
- `TestIsFileContentInspection_Jq` — argv `[jq . file.json]` → true
- `TestIsFileContentInspection_AbsolutePath` — argv `[/usr/bin/cat]` → true (basename match)
- `TestIsFileContentInspection_NotMatching_Git` — argv `[git status]` → false
- `TestIsFileContentInspection_EmptyArgv` — argv `[]` → false
- `TestIsFileContentInspection_AllInspectionTools` — table-driven over `{cat, sed, head, tail, nl, bat, batcat, jq, yq}`

### 11. `classify.go` — `MatchesRule`, `ScoreRule`, `ClassifyExecution`

Signatures:
- `func MatchesRule(rule *CompiledRule, input ToolExecutionInput) bool`
- `func ScoreRule(rule *CompiledRule) int`
- `func ClassifyExecution(input ToolExecutionInput, rules []*CompiledRule, forced *string) ClassificationResult`

MatchesRule (one happy + one negative per dimension):
- `TestMatchesRule_ToolNames_Hits`
- `TestMatchesRule_ToolNames_Misses`
- `TestMatchesRule_Argv0_Hits`
- `TestMatchesRule_Argv0_MissingArgvTreatedAsEmpty` — rule needs argv0=[git], input has argv=nil → no match
- `TestMatchesRule_ArgvIncludes_AllGroupsMustMatch`
- `TestMatchesRule_ArgvIncludes_GroupMissingOneToken_Fails`
- `TestMatchesRule_ArgvIncludesAny_OneGroupSuffices`
- `TestMatchesRule_CommandIncludes_AllSubstrings`
- `TestMatchesRule_CommandIncludesAny_OneSubstring`
- `TestMatchesRule_EmptyMatch_MatchesEverything` — `match: {}` rule fires on any input
- `TestMatchesRule_CommandFallbackFromArgv` — rule has `commandIncludes: ["pytest"]`, input has `argv=[python -m pytest]`, no command → matches because command is synthesized

ScoreRule:
- `TestScoreRule_PriorityDominates` — rule with priority=25 outranks all structural specificity in the builtin set
- `TestScoreRule_Argv0_100Per`
- `TestScoreRule_ArgvIncludes_40PerToken`
- `TestScoreRule_ArgvIncludesAny_35PerToken`
- `TestScoreRule_CommandIncludes_25Per`
- `TestScoreRule_CommandIncludesAny_20Per`
- `TestScoreRule_ToolNames_10Per`
- `TestScoreRule_GenericFallbackScoresZero`

ClassifyExecution:
- `TestClassify_ForcedRuleID` — opts.classifier=`"git/status"` → confidence=1.0
- `TestClassify_ForcedRuleID_NotFound_FallsThrough` — opts.classifier=`"nonexistent"` → normal classification, no error
- `TestClassify_NoMatches_ReturnsGenericLowConfidence` — empty rule list (or no rule matches) → family=`"generic"`, confidence=0.2, matchedReducer=nil
- `TestClassify_SpecificBeatsGenericFallback` — git/status outranks generic/fallback
- `TestClassify_AlphabeticalTieBreak` — two rules tied on score → lower rule.id wins
- `TestClassify_GenericFallbackForcesConfidence0_2`

### 12. `reduce.go` — `BuildRawText`, `ApplyRule`, `FormatInline`, `SelectInlineText`, `ReduceExecutionWithRules`

Signatures match spec §3.8.

BuildRawText:
- `TestBuildRawText_CombinedTextWins` — combined_text set → use it, ignore stdout/stderr
- `TestBuildRawText_StdoutPlusStderr` — both non-empty → `stdout + "\n" + stderr`
- `TestBuildRawText_OnlyStdout`
- `TestBuildRawText_OnlyStderr`
- `TestBuildRawText_AllEmpty` — returns `""`

ApplyRule (test pipeline order step by step):
- `TestApplyRule_PrettyPrintJsonRunsFirst`
- `TestApplyRule_StripAnsiBeforeOutputMatches` — outputMatch pattern only matches stripped form
- `TestApplyRule_OutputMatch_ShortCircuitsReturn` — first match wins, skipPatterns never run
- `TestApplyRule_OutputMatch_CountersNotInvoked` — counters absent from result when outputMatch fired
- `TestApplyRule_SkipPatternsDropLines`
- `TestApplyRule_KeepPatterns_RetainsOnlyMatching`
- `TestApplyRule_KeepPatterns_FallbackWhenZeroMatches` — keep produces empty → revert to pre-keep lines
- `TestApplyRule_TrimEmptyEdges_AppliedWhenSet`
- `TestApplyRule_DedupeAdjacent_AppliedWhenSet`
- `TestApplyRule_GitStatusPostProcessor_RewritesModified` — `\tmodified:   foo.rs` → `M: foo.rs`
- `TestApplyRule_GitStatusPostProcessor_RewritesNewFile` — `\tnew file:   bar.rs` → `A: bar.rs`
- `TestApplyRule_GitStatusPostProcessor_RewritesDeleted` — → `D:`
- `TestApplyRule_GitStatusPostProcessor_RewritesRenamed` — → `R:`
- `TestApplyRule_GitStatusPostProcessor_RewritesUntracked` — → `?? foo.txt`
- `TestApplyRule_GitStatusPostProcessor_StripsOnBranch`
- `TestApplyRule_GitStatusPostProcessor_StripsGitAddHints`
- `TestApplyRule_GitStatusPostProcessor_StripsDivergedMessage`
- `TestApplyRule_GitStatusPostProcessor_CollapsesConsecutiveBlanks`
- `TestApplyRule_GitStatusPostProcessor_ShortensSectionHeaders` — `"Changes not staged for commit:"` → `"Changes not staged:"`
- `TestApplyRule_GhPostProcessor_JSONLines` — JSON per-line → `#42 title [open] (branch) Nc {labels} date`
- `TestApplyRule_GhPostProcessor_TableFallback` — non-JSON whitespace table → table-formatter
- `TestApplyRule_GhPostProcessor_LabelsAsObjects`
- `TestApplyRule_GhPostProcessor_LabelsAsStringArray`
- `TestApplyRule_GhPostProcessor_LabelsNonArrayIgnored`
- `TestApplyRule_GhPostProcessor_CommentsAsArray` — labels=[{...},{...}] → `2c`
- `TestApplyRule_GhPostProcessor_CommentsAsNumber` — `comments: 4` → `4c`
- `TestApplyRule_GhPostProcessor_DisplayTitleAndDatabaseId` — workflow runs
- `TestApplyRule_GhPostProcessor_MissingTitle_FallsToTable`
- `TestApplyRule_GhPostProcessor_EmptyLines_DoesNotPanic`
- `TestApplyRule_Counters_PostKeep`
- `TestApplyRule_Counters_PreKeep_SurvivesKeepFilter`
- `TestApplyRule_OnEmpty_ReturnsCustomMessage`
- `TestApplyRule_HeadTail_NormalMode`
- `TestApplyRule_HeadTail_FailureMode_LargerWindow` — exit_code != 0 + preserveOnFailure=true → uses failure.head/tail
- `TestApplyRule_HeadTail_FailureDefault6_12_WhenUnset`

FormatInline:
- `TestFormatInline_ExitCodePrefix_NonZero` — `exit 1` prepended
- `TestFormatInline_NoExitPrefix_OnZero`
- `TestFormatInline_NoExitPrefix_OnMissingExit` — nil exit code = no prefix
- `TestFormatInline_IncludeFacts_SearchFamily`
- `TestFormatInline_IncludeFacts_NonGitStatus_WhenSummaryContainsOmitted`
- `TestFormatInline_IncludeFacts_TestResults_OnFailureOnly` — exit_code==0 → no facts header
- `TestFormatInline_ExcludeFacts_GitStatusFamily` — git-status never shows the facts header
- `TestFormatInline_ExcludeFacts_HelpFamily`
- `TestFormatInline_FactsSortedAlphabetically`
- `TestFormatInline_FactsWithZeroCountExcluded`
- `TestFormatInline_FactsJoinedWithCommaSpace`

SelectInlineText:
- `TestSelectInlineText_GitStatusFamily_AlwaysReturnsCompact`
- `TestSelectInlineText_HelpFamily_PassthroughLimitIsMaxInlineChars`
- `TestSelectInlineText_DefaultPassthroughLimitIs240` — TINY_OUTPUT_MAX_CHARS
- `TestSelectInlineText_RawShorterThanCompact_ReturnsPassthrough` — short raw, oversized compact → bail
- `TestSelectInlineText_PassthroughShorterThanCompact_ReturnsPassthrough`
- `TestSelectInlineText_PassthroughTooLong_ReturnsCompact`
- `TestSelectInlineText_BuildPassthroughText_PrependsExitCode` — nonzero exit → `"exit 2\n..."`
- `TestSelectInlineText_BuildPassthroughText_NoOutputMarker` — empty normalised → `"(no output)"`

ReduceExecutionWithRules (top-level):
- `TestReduce_RawOptionReturnsVerbatim` — opts.raw=true → InlineText==rawText, Ratio==1.0
- `TestReduce_FileContentInspectionBypass_Cat` — argv=[cat foo.txt] + generic/fallback → raw passthrough, ratio==1.0
- `TestReduce_FileContentInspectionBypass_Jq`
- `TestReduce_FileContentInspectionBypass_OnlyWhenFallbackWouldApply` — argv=[cat foo.txt] but classification matches a specific rule → bypass NOT triggered
- `TestReduce_RatioMeasuredAfterClamp`
- `TestReduce_RatioOneWhenRawCharsZero`
- `TestReduce_MaxInlineCharsDefault1200`
- `TestReduce_MaxInlineCharsRespected` — opts.maxInlineChars=200 → output ≤ ~300 (suffix slack)
- `TestReduce_HelpFamily_UsesMiddleClamp`
- `TestReduce_MultilineSelected_UsesMiddleClamp`
- `TestReduce_SingleLineSelected_UsesTailClamp`
- `TestReduce_PreviewTextSetWhenSummaryNonEmpty`
- `TestReduce_FactsSetWhenNonEmpty`
- `TestReduce_FactsNilWhenEmpty`
- `TestReduce_StatsRawCharsCountedAfterStripANSI`
- `TestReduce_StatsRatioComputed`
- `TestReduce_NoFallbackRule_GracefulSynthesizedFallback` — empty rule list does NOT panic (Aura divergence from Rust panic; see Open Q1 in algorithm spec)

### 13. `rules/compile.go` — rule compilation defensive paths

- `TestCompileRule_InvalidSkipPatternDropped` — `"[invalid"` silently dropped, other patterns retained
- `TestCompileRule_AllInvalidSkipPatterns_ProducesEmptyVec`
- `TestCompileRule_InvalidKeepPatternDropped`
- `TestCompileRule_InvalidCounterPatternDropped`
- `TestCompileRule_InvalidOutputMatchPatternDropped`
- `TestCompileRule_FlagsI_AppliedAsRE2InlineFlag`
- `TestCompileRule_FlagsM_AppliedAsRE2InlineFlag`
- `TestCompileRule_FlagsU_Ignored` — `u` flag from upstream JS is dropped (Go is Unicode by default)

### 14. `rules/load.go` — builtin loader

- `TestLoadBuiltinRules_NonEmpty`
- `TestLoadBuiltinRules_ContainsGenericFallback` — required invariant
- `TestLoadBuiltinRules_GenericFallbackSortedLast` — appears as last element
- `TestLoadBuiltinRules_HelpRuleHasPriority25`
- `TestLoadBuiltinRules_AllVendoredRulesCompile` — for each embedded rule JSON, assert it parses without error

### 15. `integration.go` — `CompactToolOutput`

Signature: `func CompactToolOutput(toolName string, arguments map[string]any, output string, exitCode *int) (string, CompactionStats)`

- `TestCompactToolOutput_BelowMinInputBytes_PassThrough` — len(output) < 512 → return verbatim, applied=false, rule_id=`"none/too-small"`
- `TestCompactToolOutput_MinRatioGuard` — compaction ratio > 0.95 → keep original, applied=false
- `TestCompactToolOutput_RatioEdge_CompactedEqualsRaw` — compactedBytes == originalBytes → keep original (guard clause for FP rounding)
- `TestCompactToolOutput_StatsAlwaysRecorded` — even when applied=false, stats struct is populated
- `TestCompactToolOutput_StatsAppliedTrueWhenCompacted`
- `TestCompactToolOutput_RuleIDFallsBackToFamilyWhenNoRuleMatched`
- `TestCompactToolOutput_NilArgumentsHandled`
- `TestCompactToolOutput_ToolNameRoutedToClassifier`

---

## Fixture tests

Path: `internal/tokenjuice/fixtures_test.go` + `internal/tokenjuice/testdata/fixtures/`. The 3 upstream JSONs are MIT-licensed and can be copied verbatim (we ship `LICENSE-UPSTREAM` in the rules dir per the catalog §5).

### Upstream fixtures

#### Fixture 1: `cargo_test_failure.fixture.json`

- **Path:** `internal/tokenjuice/testdata/fixtures/cargo_test_failure.fixture.json`
- **What it tests:** end-to-end `tests/cargo-test` rule: exit-code prefix, facts header (`2 failed tests, 2 passed tests`), `failure.preserveOnFailure=true` widening the head/tail window to 18/18, `skipPatterns` removal of `Compiling/Finished/Running` lines, `format_inline` facts ordering.
- **Go test shape:**
  ```go
  func TestFixture_CargoTestFailure(t *testing.T) {
      fx := loadFixture(t, "cargo_test_failure.fixture.json")
      rules := tokenjuice.LoadBuiltinRules()
      result := tokenjuice.ReduceExecutionWithRules(fx.Input, rules, tokenjuice.ReduceOptions{})
      if result.InlineText != fx.ExpectedOutput {
          t.Errorf("mismatch:\nwant:\n%s\n\ngot:\n%s\n\ndiff: %s",
              fx.ExpectedOutput, result.InlineText, diff(fx.ExpectedOutput, result.InlineText))
      }
      if result.Classification.MatchedReducer == nil || *result.Classification.MatchedReducer != "tests/cargo-test" {
          t.Errorf("expected matchedReducer=tests/cargo-test, got %v", result.Classification.MatchedReducer)
      }
  }
  ```

#### Fixture 2: `git_status_modified.fixture.json`

- **Path:** `internal/tokenjuice/testdata/fixtures/git_status_modified.fixture.json`
- **What it tests:** `git/status` rule: porcelain rewriter (`modified:` → `M:`), section header rewrite (`Changes not staged for commit:` → `Changes not staged:`), `skipPatterns` removal of `On branch` + `(use "git …")` hints.
- **Expected output:** `"Changes not staged:\nM: src/foo.rs"`
- **Go test shape:** identical pattern; matches the user-provided code shape verbatim.

#### Fixture 3: `fallback_long_output.fixture.json`

- **Path:** `internal/tokenjuice/testdata/fixtures/fallback_long_output.fixture.json`
- **What it tests:** `generic/fallback` head=8 tail=8 window on a 20-line input. Asserts the omission marker text format `"... 4 lines omitted ..."`.
- **Go test shape:** identical pattern.

### `loadFixture` helper

```go
type Fixture struct {
    Description    string                       `json:"description"`
    Input          tokenjuice.ToolExecutionInput `json:"input"`
    ExpectedOutput string                       `json:"expectedOutput"`
    Options        *tokenjuice.ReduceOptions    `json:"options,omitempty"`
}

func loadFixture(t *testing.T, name string) Fixture {
    t.Helper()
    path := filepath.Join("testdata", "fixtures", name)
    raw, err := os.ReadFile(path)
    if err != nil { t.Fatalf("read fixture %s: %v", name, err) }
    var fx Fixture
    if err := json.Unmarshal(raw, &fx); err != nil {
        t.Fatalf("unmarshal fixture %s: %v", name, err)
    }
    return fx
}
```

### Aura-specific new fixtures (3-5 recommended)

The upstream suite has only 3 fixtures; the rules-catalog §6.4 itself recommends more. We add Aura-specific ones covering the 10 builtin rules we ship (§4.3 of the catalog). Adding all 10 would be redundant with unit tests — the goal of new fixtures is to lock the *integration of pipeline branches* that unit tests cover only one phase at a time.

| File | Tests | Rationale |
|---|---|---|
| `git_log_oneline_30.fixture.json` | `git/log-oneline` with 30 commits; verify head=8 tail=6 + `commit` counter surfaced via `omitted` trigger. | Covers `format_inline` facts-include-when-summary-contains-omitted branch on a non-fallback rule. |
| `pytest_failure_prekeep.fixture.json` | `tests/pytest` mixed pass+fail output; verify `counterSource=preKeep` keeps `passed test` count truthful even when keepPatterns drops `PASSED` lines. | Covers the only `preKeep` branch in the entire builtin set. |
| `npm_install_uptodate.fixture.json` | `install/npm-install` with `up to date, audited 250 packages` → expect canned `"npm install: up to date"`. | Covers `matchOutput` short-circuit on a real rule (not a synthetic one). |
| `npm_install_empty.fixture.json` | `install/npm-install` with empty stdout → expect `onEmpty` canned `"npm install: ok"`. | Covers `onEmpty` short-circuit. |
| `help_argv_priority.fixture.json` | `aws --help` long output → expect `generic/help` wins (priority=25) over `cloud/aws`, head=80 tail=40 applies, middle clamp used (multiline + help family). | Covers priority + help-family-uses-middle-clamp interaction. |

Each is committed as JSON in `testdata/fixtures/`. The catalog §5 license note applies: if we wrote them ourselves (we will — they're trivial), no attribution required.

---

## Integration tests

Path: `internal/agent/loop_test.go` (or a new `internal/agent/tokenjuice_integration_test.go` to avoid bloating `loop_test.go`).

### Stub LLM client

`internal/agent/loop_test.go` already has a stub LLM client pattern. Re-use it; do not invent a new harness.

### Stub tool

```go
type stubLongOutputTool struct {
    output string
}
func (s stubLongOutputTool) Name() string { return "stub_long_output" }
func (s stubLongOutputTool) Description() string { return "test stub" }
func (s stubLongOutputTool) Parameters() ... { ... }
func (s stubLongOutputTool) Execute(...) (string, error) { return s.output, nil }
```

### Test cases (each is one `func TestAgentLoop_*`)

1. **`TestAgentLoop_TokenJuiceShrinksLongOutput`**
   - Setup: stub LLM returns a tool call to `stub_long_output`; tool returns a 5 KB git-status-shaped string ("On branch …" + 100 `\tmodified:   file_<i>.rs` lines).
   - Run the agent loop one step.
   - Assert: the tool-result message appended to `state.Messages` has `len(content)` ≤ 1500 (rough: 1200-char clamp + small overhead).
   - Assert: the tool-result message contains `"M: file_"` (porcelain rewrite happened).
   - Assert: the original output is not in `state.Messages` content.

2. **`TestAgentLoop_TokenJuicePassThroughShort`**
   - Setup: stub tool returns `"ok\n"` (3 bytes, far below MIN_COMPACT_INPUT_BYTES=512).
   - Assert: the tool-result message contains `"ok\n"` verbatim.
   - Assert: the recorded `CompactionStats.Applied == false`, `RuleID == "none/too-small"`.

3. **`TestAgentLoop_TokenJuiceLogsRuleID`**
   - Setup: stub tool returns the cargo-test-failure fixture stdout; LLM stubbed to call once and stop.
   - Assert: the structured-logging sink (use `zaptest.NewObserver`) recorded a log entry with `rule_id="tests/cargo-test"`, `applied=true`, `original_bytes`, `compacted_bytes`.
   - Assert: per CLAUDE.md "No secrets in logs", the log entry does NOT contain the tool's raw output bytes — only counts.

4. **`TestAgentLoop_TokenJuiceBrokenRuleNoCrash`**
   - Setup: monkey-patch the rules slice (or inject via test helper) with one rule containing all-invalid regexes alongside `generic/fallback`.
   - Assert: agent loop completes without panic.
   - Assert: tool result reaches the LLM (compaction degraded to fallback, but pipeline survives).

5. **`TestAgentLoop_TokenJuiceDisabledByFlag`**
   - Setup: `t.Setenv("AURA_TOKENJUICE_ENABLED", "false")` (or whatever the wired config key is); stub tool returns 5 KB output.
   - Assert: the tool-result message content equals the raw output byte-for-byte.
   - Assert: no compaction stats were recorded.
   - Cleanup is handled by `t.Setenv` (auto-revert on test end).

6. **`TestAgentLoop_TokenJuicePerToolOptOut`** (only if `AURA_TOKENJUICE_DISABLED_TOOLS` is shipped — see rollback plan)
   - Setup: `t.Setenv("AURA_TOKENJUICE_DISABLED_TOOLS", "stub_long_output,web_fetch")`.
   - Assert: stub_long_output result NOT compacted; a different stub tool (e.g. `stub_other`) IS compacted with the same input.

7. **`TestAgentLoop_TokenJuiceRaceFreeUnderConcurrentTurns`**
   - Setup: 16 concurrent goroutines each running one agent-loop step with the same long stub output.
   - Run with `-race`.
   - Assert: all 16 finish without race detector firing.
   - Assert: each goroutine's `state.Messages` last-tool-result is the same compacted string (no cross-goroutine state leak).

---

## E2E probes (cmd/probe_chat)

Each probe follows the existing `cmd/probe_chat/cases.go` Setup/Verify/Cleanup shape and obeys the 8 mandatory discipline rules in `.agents/skills/aura-qa-pipeline/SKILL.md`. The Verify function MUST:
1. Make at least one ground-truth assertion that does NOT read `r.Reply` (rule 1).
2. Print a human-readable preview (first 200 chars or structural summary) of the artifact to stderr (rule 2).
3. Have an idempotent Cleanup that uses timestamped names (rule 8).

Where the stats are persisted: per the algorithm spec §1.5 + §5.1, `CompactToolOutput` returns `CompactionStats` alongside the compacted output. The wiring (`internal/agent/loop.go`) MUST persist these stats to a queryable surface — recommended: a new column on `tool_attempts` (`compaction_rule_id TEXT`, `compaction_original_bytes INT`, `compaction_compacted_bytes INT`, `compaction_applied INT`). Probe verifiers read this column via `env.DB`.

### Probe 1: `tool-tokenjuice-shrinks-execute-shell`

- **Category:** `tools-files` (the probe exercises a verbose tool output)
- **Setup:** none — we craft the prompt so the tool output is deterministic.
- **Prompt:**
  > Esegui questo comando shell esatto e poi raccontami in due righe cosa ha stampato: `python3 -c "import sys; [print(f'line {i}') for i in range(200)]; print('MARKER-XXXX', file=sys.stderr); sys.exit(0)"`. Non interpretare, esegui letteralmente.

  (`MARKER-XXXX` is replaced with `MARKER-{stamp}` so a re-run gets a unique marker. The 200-line output guarantees ≥ MIN_COMPACT_INPUT_BYTES.)
- **Verify:**
  ```go
  Verify: func(r ChatReply, env *Env) []string {
      var miss []string
      if r.ToolCalls == 0 {
          miss = append(miss, "expected ≥1 tool call (execute_shell), got 0")
      }
      // Ground truth #1: tool_attempts row exists and was compacted
      row, err := env.DB.QueryRow(`
          SELECT compaction_applied, compaction_rule_id,
                 compaction_original_bytes, compaction_compacted_bytes
          FROM tool_attempts
          WHERE conversation_id = ? AND tool_name = 'execute_shell'
          ORDER BY id DESC LIMIT 1`, r.ConversationID).Scan(&applied, &ruleID, &origB, &compB)
      if err != nil {
          miss = append(miss, fmt.Sprintf("DB ground truth: tool_attempts lookup: %v", err))
          return miss
      }
      if applied != 1 {
          miss = append(miss, fmt.Sprintf("DB: compaction NOT applied (rule_id=%q, orig=%d, comp=%d)", ruleID, origB, compB))
      }
      if compB >= origB {
          miss = append(miss, fmt.Sprintf("DB: compacted_bytes %d not smaller than original %d", compB, origB))
      }
      ratio := float64(compB) / float64(origB)
      if ratio > 0.95 {
          miss = append(miss, fmt.Sprintf("DB: ratio %.3f exceeds MIN_COMPACT_RATIO; should not have applied", ratio))
      }
      // Stderr preview (rule 2) — show the ratio for human inspection
      fmt.Fprintf(os.Stderr, "[tokenjuice] %s: rule=%s, %d → %d bytes (ratio %.2f)\n",
          r.Name, ruleID, origB, compB, ratio)
      fmt.Fprintf(os.Stderr, "[reply preview] %s\n", safePreview(r.Reply, 200))

      // Ground truth #2: the marker the model saw must equal what the prompt asked for
      // (asserts compaction didn't destroy the load-bearing token)
      marker := fmt.Sprintf("MARKER-%s", stamp)
      if !strings.Contains(r.Reply, marker) {
          miss = append(miss, fmt.Sprintf("reply does not mention required marker %q — compaction may have stripped signal", marker))
      }
      // Substantive reply check (rule per CLAUDE.md TESTS VERIFY QUALITY)
      if len(strings.TrimSpace(r.Reply)) < 40 {
          miss = append(miss, fmt.Sprintf("reply too short: %d chars", len(r.Reply)))
      }
      return miss
  },
  ```
- **Cleanup:** none — conversation rows are timestamped and accumulate by design (per existing probe patterns).

### Probe 2: `tool-tokenjuice-passthrough-short`

- **Category:** `tools-memory`
- **Setup:** none.
- **Prompt:**
  > Cerca nella memoria la parola "kumquat" con limite 1 risultato. Poi rispondimi solo "ok" in una riga.

  (Designed to produce a small `search_memory` tool result — typically a few hundred bytes.)
- **Verify:**
  ```go
  Verify: func(r ChatReply, env *Env) []string {
      var miss []string
      // Find the search_memory attempt row
      var applied int; var ruleID string; var origB, compB int
      err := env.DB.QueryRow(`
          SELECT compaction_applied, compaction_rule_id,
                 compaction_original_bytes, compaction_compacted_bytes
          FROM tool_attempts
          WHERE conversation_id = ? AND tool_name = 'search_memory'
          ORDER BY id DESC LIMIT 1`, r.ConversationID).Scan(&applied, &ruleID, &origB, &compB)
      if err != nil {
          miss = append(miss, fmt.Sprintf("DB lookup: %v", err))
          return miss
      }
      if origB >= 512 {
          // If the search returned more than the min-bytes threshold, this probe
          // is not exercising the passthrough branch. Document and continue.
          fmt.Fprintf(os.Stderr, "[tokenjuice] short-output probe got %d bytes — above 512 threshold; passthrough not exercised this run\n", origB)
      } else {
          if applied != 0 {
              miss = append(miss, fmt.Sprintf("expected applied=false for %d-byte output (below 512 threshold); got applied=1", origB))
          }
          if ruleID != "none/too-small" {
              miss = append(miss, fmt.Sprintf("expected rule_id=none/too-small, got %q", ruleID))
          }
      }
      fmt.Fprintf(os.Stderr, "[tokenjuice] passthrough probe: orig=%d, comp=%d, applied=%d, rule=%s\n", origB, compB, applied, ruleID)
      return miss
  },
  ```

### Probe 3: `tool-tokenjuice-disabled-flag` (negative)

- **Category:** `tools-files`
- **Setup:** flip the runtime config to disable TokenJuice. Two implementation choices:
  1. (Preferred) Set the SQLite settings override via the existing settings catalog (`internal/api/settings.go`): `UPDATE settings SET value='false' WHERE key='aura.tokenjuice.enabled';` then call `/api/settings/reload` or restart the agent loop.
  2. (Fallback) Container restart with `AURA_TOKENJUICE_ENABLED=false` injected.
  The Setup function records the prior value so Cleanup can restore it.
- **Prompt:** same as Probe 1 (deterministic 200-line shell output).
- **Verify:**
  ```go
  Verify: func(r ChatReply, env *Env) []string {
      var miss []string
      var applied int; var ruleID string; var origB, compB int
      err := env.DB.QueryRow(`
          SELECT compaction_applied, compaction_rule_id,
                 compaction_original_bytes, compaction_compacted_bytes
          FROM tool_attempts
          WHERE conversation_id = ? AND tool_name = 'execute_shell'
          ORDER BY id DESC LIMIT 1`, r.ConversationID).Scan(&applied, &ruleID, &origB, &compB)
      if err != nil {
          miss = append(miss, fmt.Sprintf("DB lookup: %v", err))
          return miss
      }
      if applied != 0 {
          miss = append(miss, fmt.Sprintf("flag disabled but compaction was applied (rule=%s)", ruleID))
      }
      if origB != compB {
          miss = append(miss, fmt.Sprintf("flag disabled but bytes differ: orig=%d compacted=%d", origB, compB))
      }
      fmt.Fprintf(os.Stderr, "[tokenjuice] disabled-flag probe: orig=%d, comp=%d, applied=%d\n", origB, compB, applied)
      return miss
  },
  Cleanup: func(env *Env) {
      // Restore the prior setting captured in Setup
      _ = env.restoreSetting("aura.tokenjuice.enabled")
  },
  ```

All three probes register in `cmd/probe_chat/cases.go::allCases` and run via `go run ./cmd/probe_chat -case=tool-tokenjuice-shrinks-execute-shell -json`.

---

## Performance benchmarks

Path: `internal/tokenjuice/bench_test.go`. Run via:

```bash
go test -bench=. -benchmem -benchtime=2s ./internal/tokenjuice/
```

Benchmarks:

```go
var (
    benchSmall   = strings.Repeat("line of output\n", 60)         // ~900 B
    benchMedium  = generateGitStatusLike(500)                      // ~10 KB
    benchLarge   = strings.Repeat("more verbose output line\n", 40000) // ~1 MB
    benchCJK     = strings.Repeat("中文测试", 2500)                 // ~10 KB, all multi-byte
    benchPatholo = strings.Repeat("a", 1_000_000)                  // single-line 1 MB no newlines
)

func BenchmarkCompact_Small(b *testing.B) { ... }
func BenchmarkCompact_Medium(b *testing.B) { ... }
func BenchmarkCompact_Large(b *testing.B) { ... }
func BenchmarkCompact_CJK(b *testing.B) { ... }
func BenchmarkCompact_PathologicalSingleLine(b *testing.B) { ... }
func BenchmarkClassify_Only(b *testing.B) { ... } // isolate classifier
func BenchmarkApplyRule_GitStatus(b *testing.B) { ... }
func BenchmarkApplyRule_CargoTest(b *testing.B) { ... }
```

**Targets (assert in a separate `TestPerformanceBudget` that runs the benchmarks programmatically via `testing.Benchmark`):**

| Benchmark | p99 target | Allocations target |
|---|---|---|
| Compact_Small (≤ 1 KB) | < 1 ms | < 50 allocs/op |
| Compact_Medium (≤ 10 KB) | **< 5 ms** | < 500 allocs/op |
| Compact_Large (1 MB) | **< 50 ms** | < 50 000 allocs/op |
| Compact_CJK (10 KB) | < 10 ms | < 1000 allocs/op (grapheme walk is the dominant cost — accept higher than ASCII) |
| Compact_PathologicalSingleLine | < 200 ms, **must not hang** | bounded |
| Classify_Only | < 50 µs | < 10 allocs/op |

Targets justified by algorithm-spec §8.3: typical Aura turn has 1-3 tool calls of 0-500 KB; we need p99 well below typical agent-step wall-clock (~2-5 s) so TokenJuice is never the bottleneck.

**Allocation budget enforcement:** `go test -bench=. -benchmem -count=5` and parse `B/op` + `allocs/op` from output. The Compact_Large 50 000 allocs target is the watch item — the algorithm spec §8.2 flags `clamp_text`'s full grapheme slice as the single biggest allocation site. If we exceed budget, switch to `uniseg.NewGraphemes` byte-index iteration (no intermediate slice).

---

## Safety analysis — what could go wrong

Six dimensions. Each has Risk → Mitigation → Verification (which test or probe asserts the mitigation works).

### 1. Over-compaction loses signal

- **Risk:** A rule's `skipPatterns` or `head_tail` is too aggressive → the LLM never sees the load-bearing data → the LLM hallucinates around the gap. Example: a `git log` rule that strips commit messages would leave just hashes; the agent then guesses at why each commit happened.
- **Mitigation:**
  - Algorithm `select_inline_text` (§3.6) bails to passthrough if `compact_chars >= raw_chars` for short inputs.
  - `tool_integration` outer guard returns raw when `ratio > 0.95`.
  - Rules pinned to the 10-rule starter set (catalog §4.3) — known-good, upstream-validated.
- **Verification:**
  - **Probe 1** asserts that `MARKER-{stamp}` (a load-bearing token deliberately injected into the tool output) is still present in the model's reply after compaction. A regression that strips the marker fails this assertion.
  - Unit test `TestApplyRule_KeepPatterns_FallbackWhenZeroMatches` ensures keepPatterns never blanks the output to empty.
  - Fixture `npm_install_uptodate` asserts the canned-message replacement is the expected text (not garbage).
  - Per-rule golden: each new fixture verifies the `expectedOutput` byte-for-byte.

### 2. Under-compaction wastes the feature

- **Risk:** Rules too conservative → average ratio stays near 1.0 → cost unchanged but complexity tax remains.
- **Mitigation:**
  - Measure per-rule ratio in production via the `compaction_*` columns on `tool_attempts`.
  - Release-gate probe asserts the suite-average ratio ≤ 0.8 for heavy probes (probes producing > 2 KB tool output).
- **Verification:**
  - Probe 1 logs the ratio to stderr; the QA pipeline reads aggregated values.
  - New invariant test in `internal/tokenjuice/integration_test.go`:
    ```go
    func TestCompactionRatio_HeavyOutputs_Under0_8(t *testing.T) {
        // Feed 10 representative heavy outputs through CompactToolOutput;
        // assert mean ratio ≤ 0.8.
    }
    ```
  - QA-pipeline `docs/qa-baselines.json` adds a `tokens_per_turn_p50` row; the success criterion below makes this concrete.

### 3. Regex DoS (catastrophic backtracking)

- **Risk:** A user-supplied rule (if we ever ship the user layer) or a future builtin pattern with nested-quantifier ambiguity → exponential time on adversarial input → tool result blocks the agent loop forever.
- **Mitigation:**
  - Go's `regexp` package is RE2-based; **does not backtrack**, guaranteed linear-time match. Algorithm spec §7 explicitly verifies all 96 vendored patterns are RE2-compatible.
  - Aura ships builtin-rules-only for v1 (catalog Open Q2; spec Open Q2 recommends builtin-only). No user-supplied regex surface.
- **Verification:**
  - `BenchmarkCompact_PathologicalSingleLine` feeds 1 MB of `"a"` with no newlines through every rule; asserts < 200 ms.
  - `TestCompileRule_InvalidSkipPatternDropped` proves the compiler degrades gracefully — invalid patterns NEVER reach the matcher.
  - Add a "fuzz against pathological inputs" test using `testing.F`:
    ```go
    func FuzzCompactToolOutput(f *testing.F) {
        f.Add("a", "execute_shell", 0)
        f.Fuzz(func(t *testing.T, output, tool string, exit int) {
            done := make(chan struct{})
            go func() { defer close(done); tokenjuice.CompactToolOutput(tool, nil, output, &exit) }()
            select {
            case <-done:
            case <-time.After(2*time.Second): t.Fatalf("CompactToolOutput hung on input")
            }
        })
    }
    ```

### 4. Privacy / credential leak

- **Risk:** A tool output that contains a leaked credential (a stderr line like `Authorization: Bearer sk-abc123…`) goes through `head_tail` → the line survives → ends up in the LLM context → ends up in the conversation archive → ends up in logs. TokenJuice is not in scope for credential redaction, but it MUST NOT make leaks worse.
- **Mitigation:**
  - **Out of scope** for TokenJuice itself — credential redaction lives in a separate pre-processing pass (recommendation: add `internal/sanitize/credentials.go` as a pre-step before `CompactToolOutput`; can ship later).
  - Per CLAUDE.md "Tool argument privacy", existing code never logs argument values. TokenJuice **only logs counts** (`original_bytes`, `compacted_bytes`, `rule_id`, `applied`) — never bytes.
  - The `compaction_*` columns on `tool_attempts` store byte counts, not output content. The output itself goes through the existing `conversations.tool_calls JSON` archive surface; no new exposure surface is added by TokenJuice.
- **Verification:**
  - `TestAgentLoop_TokenJuiceLogsRuleID` asserts the structured-log entry contains rule_id + byte counts but NOT raw output bytes.
  - Add `TestCompactToolOutput_DoesNotLogPayload` — install a `zaptest.NewObserver` and assert no observed log entry's `Message` or `Fields` map contains the input string.
  - Probe variant `tool-tokenjuice-no-credential-leak`:
    ```go
    Prompt: "Esegui: echo 'X-Test-Auth-Sentinel: SENTINEL-{stamp}'. Confermami solo che il comando è eseguito."
    Verify: assert SENTINEL-{stamp} survives in the conversation archive (compaction must NOT silently strip a line) AND assert no log line in the run contains SENTINEL-{stamp} (TokenJuice itself never leaks).
    ```

### 5. Encoding corruption (mid-grapheme cut)

- **Risk:** Byte-level truncation lands inside a multi-byte UTF-8 codepoint or a multi-codepoint grapheme cluster → `\xef\xbf\xbd` replacement chars or worse, invalid UTF-8 that downstream sanitizers reject.
- **Mitigation:** Every limit/truncation in the spec uses grapheme clusters via `github.com/rivo/uniseg` (spec §4.3). `clamp_text` and `clamp_text_middle` slice on grapheme boundaries.
- **Verification:**
  - `TestClampText_GraphemeSafeCJK`
  - `TestClampText_GraphemeSafeEmoji`
  - `TestClampTextMiddle_GraphemeSafeCJK`
  - `TestClampTextMiddle_GraphemeSafeEmoji`
  - `TestClampTextMiddle_GraphemeSafeZWJ`
  - `TestStripANSI_ZWJSequence`, `TestStripANSI_EmojiIntact`, `TestStripANSI_CJKIntact`
  - All ported verbatim from `text_tests.rs` (which exists precisely to verify this).

### 6. Cross-turn state leak

- **Risk:** Package-level mutable state (a global stats counter, a regex cache that holds rule-specific keys, a thread-local) leaks between independent agent turns → metrics get attributed to the wrong conversation, or worse, a cached regex from rule N collides with rule M.
- **Mitigation:**
  - Builtin rules loaded once at process boot into immutable `[]*CompiledRule` slice (algorithm spec §8.1).
  - `CompactionStats` is returned per call — never stored in a package-level singleton.
  - Hard-coded git-status post-processor regexes hoisted to package-level `var (...= regexp.MustCompile(…))` (spec §7 Open Q3 recommendation) — compiled once, used by many callers, read-only after init.
  - Aura's agent loop already runs per-conversation goroutines (CLAUDE.md "1 goroutine = 1 agent loop"); TokenJuice MUST NOT introduce any new shared mutable state.
- **Verification:**
  - `TestAgentLoop_TokenJuiceRaceFreeUnderConcurrentTurns` — 16 concurrent goroutines under `-race`.
  - Add `TestCompactToolOutput_NoSharedMutableState` — call `CompactToolOutput` twice with different inputs; assert the second call's stats are independent of the first (no monotonic counter polluting).
  - Run `go test -race ./internal/tokenjuice/... ./internal/agent/...` in CI.

---

## Rollback plan

Three escalation tiers — pick the lightest that restores stability.

### Tier 1: Soft disable via env flag

**Env vars introduced:**

| Var | Default | Effect |
|---|---|---|
| `AURA_TOKENJUICE_ENABLED` | `true` | Master switch. When `false`, `CompactToolOutput` short-circuits and returns the raw output with `applied=false, rule_id="disabled"`. |
| `AURA_TOKENJUICE_DISABLED_TOOLS` | `""` | Comma-separated list of tool names to skip compaction for, even when the master switch is on. E.g. `"execute_shell,web_fetch"`. Matched against `toolName` exactly. |
| `AURA_TOKENJUICE_MAX_INLINE_CHARS` | `1200` | Override the inline grapheme cap globally. Useful if a specific model variant needs more or less. |

Settings catalog wiring: each var also gets a row in `internal/api/settings.go` so runtime overrides via `/api/settings` work without restart (per the existing applier pattern, `internal/config/applier.go`).

**Tier 1 rollback recipe:**

```bash
# Option A — container env (requires restart)
echo "AURA_TOKENJUICE_ENABLED=false" >> data/.env
docker compose restart aura

# Option B — settings API (no restart)
curl -X PATCH http://localhost:18080/api/settings \
  -H "Authorization: Bearer $AURA_CHAT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"aura.tokenjuice.enabled": "false"}'
```

### Tier 2: Per-tool opt-out

If one specific tool's output is being mangled (e.g. `web_fetch` HTML pages getting head/tailed in a useless way), opt that tool out without touching the rest:

```bash
curl -X PATCH http://localhost:18080/api/settings \
  -H "Authorization: Bearer $AURA_CHAT_TOKEN" \
  -d '{"aura.tokenjuice.disabled_tools": "web_fetch,execute_code"}'
```

### Tier 3: Hard revert

Revert the wiring commit while keeping the package on disk for a future re-attempt:

```bash
# Find the wiring commit (the one that adds CompactToolOutput call into loop.go)
git log --oneline --all -- internal/agent/loop.go | head -10

# Revert just that commit
git revert <commit-hash>
git push origin master
```

**Diff to revert (single commit):** the wiring is ~20 lines in `internal/agent/loop.go` (or `toolexec.go`) — wherever the tool-result string is assembled before being appended to `state.Messages`. The package `internal/tokenjuice/` stays, untouched, ready for re-wiring after fix.

**Documentation note:** this plan originally referenced the active QA bug log at `docs/qa-bug-log.md`. That 2026-05-18/19 QA surface is now archived at `docs/_archive/qa-2026-05-18-19/qa-bug-log.md`; new regressions should be recorded in the current phase benchmark/progress file instead. The probe `tool-tokenjuice-disabled-flag` is the regression case that proves Tier 1 still works.

---

## Measurement — was the port worth it?

Per `.agents/skills/aura-qa-pipeline/SKILL.md` "Performance baselines", every Phase 5 run captures `docs/qa-baselines.json` with per-case `elapsed_ms`, `tokens`, `tool_calls`, `llm_calls`. We extend the baseline schema with:

```json
{
  "case": "tool-tokenjuice-shrinks-execute-shell",
  "tokens_total": 4823,
  "tokens_input": 3201,
  "tokens_output": 1622,
  "compaction_total_bytes_saved": 18432,
  "compaction_avg_ratio": 0.21,
  "elapsed_ms": 12450
}
```

### Success criterion (explicit)

> **TokenJuice is justified if, after one week of production usage:**
> 1. **`tokens_input` per heavy probe (probes producing > 2 KB tool output before compaction) drops by ≥ 10 %** vs the pre-TokenJuice baseline captured immediately before US-TJ08 flips the default to enabled.
> 2. **Zero probe regressions** — every probe that was PASS pre-TokenJuice remains PASS post-TokenJuice. A regression that is fixed within the same QA run does not count against this.
> 3. **No P0 bug attributable to TokenJuice** opens in the current phase benchmark/progress file during the same week.
>
> If any of the three fails after one week, file an issue in the current phase progress file titled `tokenjuice-roi-shortfall` and either tune the rule set (US-TJ09 — currently un-planned) OR roll back per Tier 3.

### Cost-savings sanity check

For a representative heavy case (Probe 1, 200-line shell output, ratio 0.21):
- Bytes saved per turn: ~18 KB
- Tokens saved per turn: ~18 KB × (1 token / 4 chars) ≈ 4500 tokens
- Sonnet input price: $3 / 1 M tokens
- Cost saved per turn: 4500 × $3 / 1e6 = **$0.0135 / turn**
- At 100 such turns / day: **$1.35 / day = ~$40 / month**

This is the "expected" savings; the actual reduction depends on Aura's traffic mix and what fraction of turns hit a verbose tool. The success criterion (10 % token reduction on heavy cases) is calibrated low enough to be detectable above noise but high enough that anything less is not worth the complexity tax of 96 rules and a new package.

### Where the baseline lives

- `docs/qa-baselines.json` — already-committed, schema-extended in the same commit that lands US-TJ08.
- `docs/qa-runs/{TS}.md` — every Phase 5 run writes one summary row including `compaction_avg_ratio` so trend lines are reconstructable from the repo history alone.

---

## Test execution timeline (per US-TJ rollout plan)

The integration plan referenced in the user's prompt sketches 8 user stories US-TJ01..US-TJ08. Each layer of the test pyramid fires in the user story where its tested code first exists. A story is not "done" until its mapped tests are green AND committed.

| US | Scope | Tests that fire | Stop condition |
|---|---|---|---|
| **US-TJ01** | Package skeleton: `internal/tokenjuice/`, types, errors, no algorithm yet. | None. (Hand-spun "package exists" test is wasteful.) | `go build ./internal/tokenjuice/` passes. |
| **US-TJ02** | Text helpers — `text/{ansi,lines,clamp,width,pluralize}.go`. | All unit tests in §§1-8 of this doc. (~90 tests.) | `go test ./internal/tokenjuice/text/...` green. `-race` green. Coverage ≥ 90 % on `text/` (grapheme math is exhaustively tested upstream — match that bar). |
| **US-TJ03** | Classifier — `classify.go`, tokenize.go. | Unit tests in §§9-11. | `go test` green. |
| **US-TJ04** | Rules — `rules/{compile,load,builtin}.go` + embedded JSONs. Fixture tests added here because they need real rules. | Unit tests in §§13-14. **All 3 upstream fixture tests + the 5 Aura-specific fixtures.** | Fixture tests green. `LoadBuiltinRules()` returns the documented count of starter rules (10). Each compiles. |
| **US-TJ05** | Reduce pipeline — `reduce.go` + `integration.go`. | Unit tests in §12 and §15. Bench tests in §"Performance benchmarks". | Bench targets met. `TestPerformanceBudget` green. |
| **US-TJ06** | Wire into `internal/agent/loop.go`. Add `compaction_*` columns to `tool_attempts` schema (one migration in `internal/db/migrations`). | **All integration tests** in §"Integration tests". | Integration tests green. `-race` green on `internal/agent/...`. Settings catalog wiring exposes the 3 env vars. |
| **US-TJ07** | Observability — structured logs, `/api/maintenance/tokenjuice/stats` read endpoint (optional, for dashboard UI), E2E probes added. | **All 3 E2E probes** registered in `cmd/probe_chat/cases.go`. `tool-tokenjuice-disabled-flag` Setup must restore prior settings via Cleanup. | All 3 probes pass against `docker compose up -d` + token. |
| **US-TJ08** | Default flip — `AURA_TOKENJUICE_ENABLED=true` in `data/.env.example` and `compose.yaml`. Re-baseline. | Re-run the full probe suite via `go run ./cmd/probe_chat -json`. Capture new `docs/qa-baselines.json`. Diff against the pre-flip baseline. | Suite green; success criterion (10 % token reduction on heavy probes, zero regressions) met. Else: hold the flip, file `tokenjuice-roi-shortfall`. |

### CI gating

- Every commit on the TokenJuice branch runs `go test -race ./internal/tokenjuice/... ./internal/agent/...` + `golangci-lint run ./internal/tokenjuice/...`. Per memory `golangci_lint_catches_what_audits_miss`, lint deltas block.
- E2E probes are NOT in CI (they need the runtime, sidecars, an LLM key, and minutes of wall-clock). They run manually as part of the QA-pipeline Phase 5 between US-TJ07 and US-TJ08, and again the morning of the default-flip.

---

## Appendix — out-of-scope items (deliberately deferred)

1. **Three-layer rule overlay** (user + project + builtin). Spec Open Q2 recommends builtin-only for v1. Not in any US-TJ story. Re-evaluate when a real user need surfaces.
2. **Streaming / `partial: bool`** semantics. Spec Open Q6: copy the field through, document as reserved. No tests for it.
3. **Token-aware `maxInlineChars`.** Spec Open Q4: keep grapheme-based for v1, document the unit clearly, callers tune per model.
4. **`prettyPrintJson` transform.** No vendored rule uses it. Port the code, do not test exhaustively beyond `TestApplyRule_PrettyPrintJsonRunsFirst` and the two upstream Rust tests (`pretty_print_json_array_output`, `pretty_print_json_non_json_passthrough`).
5. **Per-tool override of `maxInlineChars`.** Ship one global knob; instrument to see if differentiation is needed. No test.
6. **Confidence-based "don't compact" guard.** Spec Open Q10. Out of scope; log confidence in `tool_attempts.compaction_classification_confidence` column for later analysis but don't gate on it.

Each deferral was annotated in the archived `docs/_archive/qa-2026-05-18-19/qa-triage.md`; new deferrals should live in the active phase progress file.
