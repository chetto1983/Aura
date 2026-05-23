package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// ToolDefinitionNames extracts the name from each tool definition.
func ToolDefinitionNames(defs []llm.ToolDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Name)
	}
	return out
}

// EstimateUsageCost converts token counts to an estimated USD cost.
func EstimateUsageCost(usage llm.TokenUsage, inputPerM, outputPerM float64) float64 {
	prompt := usage.PromptTokens
	completion := usage.CompletionTokens
	if prompt == 0 && completion == 0 {
		prompt = usage.TotalTokens
	}
	return (float64(prompt)*inputPerM + float64(completion)*outputPerM) / 1_000_000
}

// SkillNameFromReadFileArgs extracts the skill name from a file(action="read")
// tool call's arguments. Returns empty string when the path does not resolve
// to a valid SKILL.md path.
func SkillNameFromReadFileArgs(args map[string]any) string {
	value, ok := args["path"]
	if !ok {
		return ""
	}
	path := strings.TrimSpace(fmt.Sprint(value))
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 2 || parts[len(parts)-1] != "SKILL.md" {
		return ""
	}
	name := strings.TrimSpace(parts[len(parts)-2])
	if name == "" || strings.EqualFold(name, "skills") {
		return ""
	}
	return name
}

// argsSummaryFor returns a brief argument summary for the "Already done this
// turn" ledger. Priority key list covers the most common tool args. The value
// is quoted (Go fmt %q) so the action line reads: tool_name("value").
func argsSummaryFor(name string, args map[string]any) string {
	_ = name // reserved for future per-tool overrides
	priority := []string{"query", "url", "slug", "path", "action", "text", "q"}
	for _, k := range priority {
		if v, ok := args[k]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 40 {
				s = s[:40] + "…"
			}
			return fmt.Sprintf("%q", s)
		}
	}
	// Fallback: iterate over the map and take the first key found.
	for _, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 40 {
			s = s[:40] + "…"
		}
		return fmt.Sprintf("%q", s)
	}
	return ""
}

// toolCallStatus classifies a tool result string into a human-readable status
// label for the "Already done this turn" ledger.
func toolCallStatus(result string) string {
	trimmed := strings.TrimSpace(result)
	if strings.HasPrefix(trimmed, "Error:") {
		return "FAILED"
	}
	if trimmed == "" {
		return "SUCCESSFUL but empty"
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "no results") ||
		strings.Contains(lower, "0 results") ||
		strings.Contains(lower, "found 0") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no documents") {
		return "SUCCESSFUL but no results"
	}
	return "SUCCESSFUL"
}

// toolCallBriefOutcome returns a one-line preview of a tool result for the
// "Already done this turn" ledger. Skips the untrusted-output prelude when
// present to surface meaningful content.
func toolCallBriefOutcome(result string) string {
	s := strings.TrimSpace(result)
	if s == "" {
		return ""
	}
	// Skip untrusted wrapper prelude ("The following is OUTPUT...").
	const untrustedPrefix = "The following is OUTPUT"
	if strings.HasPrefix(s, untrustedPrefix) {
		if idx := strings.Index(s, ">\n"); idx >= 0 {
			s = strings.TrimSpace(s[idx+2:])
		}
	}
	// Take first non-empty line, capped at 80 chars.
	for _, line := range strings.SplitN(s, "\n", 10) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 80 {
			return line[:80] + "…"
		}
		return line
	}
	return ""
}
