package tokenjuice

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	defaultSummarizeHead = 6
	defaultSummarizeTail = 6
	defaultFailureHead   = 6
	defaultFailureTail   = 12
	tinyOutputMaxChars   = 240
)

// file-content inspection commands — when generic/fallback matches one of
// these, the raw output is returned verbatim (algorithm spec §3.1).
var fileInspectCommands = map[string]bool{
	"cat": true, "sed": true, "head": true, "tail": true,
	"nl": true, "bat": true, "batcat": true, "jq": true, "yq": true,
}

// ReduceExecutionWithRules runs the full compaction pipeline against the
// given rule set. This is the engine behind CompactWithRules.
func ReduceExecutionWithRules(in Input, rules []*CompiledRule, opts Options) Result {
	rawText := buildRawText(in)
	origBytes := len(rawText)

	if isStructuredToolOutput(in.ToolName) {
		return passthroughResult(in.ToolName, rawText, "none/structured-tool",
			ClassificationResult{Family: "structured", Confidence: 0})
	}

	// Outer guard: tiny inputs are returned verbatim (spec §5.1).
	minBytes := opts.MinInputBytes
	if minBytes == 0 {
		minBytes = defaultMinInputBytes
	}
	if origBytes < minBytes {
		return passthroughResult(in.ToolName, rawText, "none/too-small",
			ClassificationResult{Family: "generic", Confidence: 0})
	}

	rawChars := countTextChars(stripAnsi(rawText))

	var forcedRuleID *string
	if opts.ForceRuleID != "" {
		forcedRuleID = &opts.ForceRuleID
	}
	cls := ClassifyExecution(in, rules, forcedRuleID)

	// Raw mode: skip reduction entirely.
	if opts.Raw {
		return passthroughResult(in.ToolName, rawText, "none/raw", cls)
	}

	// File-content inspection bypass (spec §3.1).
	if isFileContentInspectionCommand(in) &&
		cls.MatchedReducer != nil && *cls.MatchedReducer == "generic/fallback" {
		return passthroughResult(in.ToolName, rawText, "none/file-inspection", cls)
	}

	// Locate the matched rule object.
	var rule *CompiledRule
	if cls.MatchedReducer != nil {
		rule = findRuleByID(rules, *cls.MatchedReducer)
	}
	if rule == nil {
		rule = findRuleByID(rules, "generic/fallback")
	}
	if rule == nil {
		// No rules at all — passthrough.
		return passthroughResult(in.ToolName, rawText, "none/no-rules", cls)
	}

	summary, facts := applyRule(rule, in, rawText)

	maxInline := opts.MaxInlineChars
	if maxInline == 0 {
		maxInline = defaultMaxInlineChars
	}

	compact := formatInline(cls, in, summary, facts)
	selected := selectInlineText(cls, in, rawText, compact, maxInline)

	var inline string
	if cls.Family == "help" || strings.Contains(selected, "\n") {
		inline = clampTextMiddle(selected, maxInline)
	} else {
		inline = clampText(selected, maxInline)
	}

	reducedChars := countTextChars(inline)
	ratio := 1.0
	if rawChars > 0 {
		ratio = float64(reducedChars) / float64(rawChars)
	}

	// Min-ratio guard: if compaction doesn't help enough, return raw (spec §5.1).
	minRatio := opts.MinRatio
	if minRatio == 0 {
		minRatio = defaultMinRatio
	}
	if ratio >= minRatio {
		return passthroughResult(in.ToolName, rawText, "none/ratio-guard", cls)
	}

	ruleID := "none/unknown"
	if cls.MatchedReducer != nil {
		ruleID = *cls.MatchedReducer
	}
	return Result{
		InlineText: inline,
		Applied:    true,
		RuleID:     ruleID,
		Stats: Stats{
			ToolName:       in.ToolName,
			OriginalBytes:  origBytes,
			CompactedBytes: len(inline),
			OriginalChars:  rawChars,
			CompactedChars: reducedChars,
			Family:         cls.Family,
			Confidence:     cls.Confidence,
		},
	}
}

// buildRawText returns the canonical input text: Stdout alone or Stdout+Stderr.
func buildRawText(in Input) string {
	if in.Stderr == "" {
		return in.Stdout
	}
	return in.Stdout + "\n" + in.Stderr
}

// isFileContentInspectionCommand returns true when argv[0] is a file-viewing
// tool that the engine should never compact (spec §3.1, §5.5).
func isFileContentInspectionCommand(in Input) bool {
	argv0 := ""
	if len(in.Argv) > 0 {
		argv0 = in.Argv[0]
	} else if in.Command != "" {
		if parts := strings.Fields(in.Command); len(parts) > 0 {
			argv0 = parts[0]
		}
	}
	return fileInspectCommands[argv0]
}

func isStructuredToolOutput(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "agent_note",
		"ask_user",
		"create_document",
		"file",
		"propose_patch",
		"read_source",
		"search",
		"search_memory",
		"skill",
		"source",
		"task",
		"web",
		"web_fetch",
		"web_search",
		"wiki_page":
		return true
	default:
		return false
	}
}

// findRuleByID returns the first rule in rules whose ID matches id, or nil.
func findRuleByID(rules []*CompiledRule, id string) *CompiledRule {
	for _, r := range rules {
		if r.Rule.ID == id {
			return r
		}
	}
	return nil
}

// applyRule runs the reduction pipeline for one rule against rawText.
// Returns the compacted summary string and a fact-count map.
func applyRule(rule *CompiledRule, in Input, rawText string) (string, map[string]int) {
	text := rawText

	// Step 1: prettyPrintJson transform (whole string, before line split).
	if rule.Rule.Transforms != nil && rule.Rule.Transforms.PrettyPrintJSON {
		text = prettyPrintJSONIfPossible(text)
	}

	// Step 2: normalize_lines.
	lines := normalizeLines(text)

	// Step 3: stripAnsi + re-normalise.
	if rule.Rule.Transforms != nil && rule.Rule.Transforms.StripAnsi {
		lines = normalizeLines(stripAnsi(strings.Join(lines, "\n")))
	}

	// Step 4: outputMatches short-circuit.
	if len(rule.Compiled.OutputMatches) > 0 {
		checkText := strings.Join(trimEmptyEdges(lines), "\n")
		for _, om := range rule.Compiled.OutputMatches {
			if om.Pattern.MatchString(checkText) {
				return om.Message, nil
			}
		}
	}

	// Step 5: skipPatterns — drop matching lines.
	if len(rule.Compiled.SkipPatterns) > 0 {
		kept := lines[:0]
		for _, l := range lines {
			skip := false
			for _, rx := range rule.Compiled.SkipPatterns {
				if rx.MatchString(l) {
					skip = true
					break
				}
			}
			if !skip {
				kept = append(kept, l)
			}
		}
		lines = kept
	}

	// Step 6: snapshot for pre-keep counters.
	preKeepLines := lines

	// Step 7: keepPatterns — retain only matching lines (no-op if result is empty).
	if len(rule.Compiled.KeepPatterns) > 0 {
		var kept []string
		for _, l := range lines {
			for _, rx := range rule.Compiled.KeepPatterns {
				if rx.MatchString(l) {
					kept = append(kept, l)
					break
				}
			}
		}
		if len(kept) > 0 {
			lines = kept
		}
	}

	// Step 8: trimEmptyEdges.
	if rule.Rule.Transforms != nil && rule.Rule.Transforms.TrimEmptyEdges {
		lines = trimEmptyEdges(lines)
	}

	// Step 9: dedupeAdjacent.
	if rule.Rule.Transforms != nil && rule.Rule.Transforms.DedupeAdjacent {
		lines = dedupeAdjacent(lines)
	}

	// Step 10: rule-specific post-processors (none for generic/fallback).
	// git/status rewriter will be added in US-TJ04 postprocess.go.

	// Step 10: rule-specific post-processors (git/status porcelain rewriter etc.).
	lines = applyPostProcessor(rule.Rule.ID, lines)

	// Step 11: counters — default postKeep (after keepPatterns + post-processors).
	// Set counterSource:"preKeep" in the rule JSON to count before keepPatterns.
	facts := map[string]int{}
	counterSrc := lines
	if rule.Rule.CounterSource != nil && *rule.Rule.CounterSource == "preKeep" {
		counterSrc = preKeepLines
	}
	for _, c := range rule.Compiled.Counters {
		count := 0
		for _, l := range counterSrc {
			if c.Pattern.MatchString(l) {
				count++
			}
		}
		if count > 0 {
			facts[c.Name] = count
		}
	}

	// Step 12: onEmpty.
	if len(lines) == 0 {
		if rule.Rule.OnEmpty != nil {
			return *rule.Rule.OnEmpty, facts
		}
		return "(no output)", facts
	}

	// Step 13: head/tail sizes — failure-preserving when exit_code != 0.
	head, tail := defaultSummarizeHead, defaultSummarizeTail
	isFailure := in.ExitCode != nil && *in.ExitCode != 0
	if isFailure && rule.Rule.Failure != nil && rule.Rule.Failure.PreserveOnFailure {
		head = defaultFailureHead
		if rule.Rule.Failure.Head != nil {
			head = *rule.Rule.Failure.Head
		}
		tail = defaultFailureTail
		if rule.Rule.Failure.Tail != nil {
			tail = *rule.Rule.Failure.Tail
		}
	} else {
		if rule.Rule.Summarize != nil {
			if rule.Rule.Summarize.Head != nil {
				head = *rule.Rule.Summarize.Head
			}
			if rule.Rule.Summarize.Tail != nil {
				tail = *rule.Rule.Summarize.Tail
			}
		}
	}

	// Step 14: head_tail.
	lines = headTail(lines, head, tail)

	// Step 15: return joined summary.
	return strings.TrimSpace(strings.Join(lines, "\n")), facts
}

// formatInline assembles the final inline string from summary + facts.
// Per algorithm spec §3.5: optionally prepend "exit N", include facts when
// they add signal, then append the summary.
func formatInline(cls ClassificationResult, in Input, summary string, facts map[string]int) string {
	var lines []string

	if in.ExitCode != nil && *in.ExitCode != 0 {
		lines = append(lines, fmt.Sprintf("exit %d", *in.ExitCode))
	}

	if len(facts) > 0 {
		includeFacts := cls.Family == "search" ||
			(cls.Family != "git-status" && cls.Family != "help" && strings.Contains(summary, "omitted")) ||
			(cls.Family == "test-results" && in.ExitCode != nil && *in.ExitCode != 0)

		if includeFacts {
			names := make([]string, 0, len(facts))
			for name := range facts {
				names = append(names, name)
			}
			sort.Strings(names)
			var factParts []string
			for _, name := range names {
				if count := facts[name]; count > 0 {
					factParts = append(factParts, pluralize(count, name))
				}
			}
			if len(factParts) > 0 {
				lines = append(lines, strings.Join(factParts, ", "))
			}
		}
	}

	lines = append(lines, summary)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// selectInlineText decides between the compacted summary and a passthrough
// of the lightly normalised raw text (algorithm spec §3.6).
func selectInlineText(cls ClassificationResult, in Input, rawText, compact string, maxInline int) string {
	if cls.Family == "git-status" {
		return compact
	}

	passthrough := buildPassthroughText(in, rawText)
	passthroughChars := countTextChars(passthrough)

	passLimit := tinyOutputMaxChars
	if cls.Family == "help" {
		passLimit = maxInline
	}
	if passthroughChars > passLimit {
		return compact
	}

	rawChars := countTextChars(stripAnsi(rawText))
	compactChars := countTextChars(compact)

	// Compaction would make it longer → return passthrough.
	if rawChars <= maxInline && compactChars >= rawChars {
		return passthrough
	}
	// Raw is already shorter than the compaction.
	if passthroughChars <= compactChars {
		return passthrough
	}
	return compact
}

// buildPassthroughText returns a lightly normalised version of rawText.
// Prepends "exit N" when exitCode is non-zero; returns "(no output)" if blank.
func buildPassthroughText(in Input, rawText string) string {
	lines := trimEmptyEdges(normalizeLines(stripAnsi(rawText)))
	if len(lines) == 0 {
		if in.ExitCode != nil && *in.ExitCode != 0 {
			return fmt.Sprintf("exit %d\n(no output)", *in.ExitCode)
		}
		return "(no output)"
	}
	text := strings.Join(lines, "\n")
	if in.ExitCode != nil && *in.ExitCode != 0 {
		text = fmt.Sprintf("exit %d\n%s", *in.ExitCode, text)
	}
	return text
}

// prettyPrintJSONIfPossible tries to re-format text as indented JSON.
// Falls back to the original text if parsing fails.
func prettyPrintJSONIfPossible(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}

// passthroughResult builds a Result with Applied=false.
func passthroughResult(toolName, rawText, ruleID string, cls ClassificationResult) Result {
	chars := countTextChars(rawText)
	return Result{
		InlineText: rawText,
		Applied:    false,
		RuleID:     ruleID,
		Stats: Stats{
			ToolName:       toolName,
			OriginalBytes:  len(rawText),
			CompactedBytes: len(rawText),
			OriginalChars:  chars,
			CompactedChars: chars,
			Family:         cls.Family,
			Confidence:     cls.Confidence,
		},
	}
}
