package telegram

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/llm"
)

func toolDefinitionNames(defs []llm.ToolDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Name)
	}
	return out
}

func orderToolDefinitionsForAllowlist(defs []llm.ToolDefinition, allowlist []string) []llm.ToolDefinition {
	if len(defs) <= 1 || len(allowlist) == 0 {
		return defs
	}
	positions := make(map[string]int, len(allowlist))
	for i, name := range allowlist {
		if _, ok := positions[name]; !ok {
			positions[name] = i
		}
	}
	out := append([]llm.ToolDefinition(nil), defs...)
	sort.SliceStable(out, func(i, j int) bool {
		left, leftOK := positions[out[i].Name]
		right, rightOK := positions[out[j].Name]
		if !leftOK && !rightOK {
			return false
		}
		if !leftOK {
			return false
		}
		if !rightOK {
			return true
		}
		return left < right
	})
	return out
}

func toolAllowed(name string, allowlist []string) bool {
	for _, allowed := range allowlist {
		if allowed == name {
			return true
		}
	}
	return false
}

func skillNameFromReadFileArgs(args map[string]any) string {
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

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" {
			continue
		}
		if !stringSliceContains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func stringSliceContains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func toolResultUsage(raw string, cfg *config.Config) (llm.TokenUsage, float64) {
	var resp struct {
		Metrics struct {
			TokensPrompt     int `json:"tokens_prompt"`
			TokensCompletion int `json:"tokens_completion"`
			TokensTotal      int `json:"tokens_total"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &resp); err != nil {
		return llm.TokenUsage{}, 0
	}
	usage := llm.TokenUsage{
		PromptTokens:     resp.Metrics.TokensPrompt,
		CompletionTokens: resp.Metrics.TokensCompletion,
		TotalTokens:      resp.Metrics.TokensTotal,
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return llm.TokenUsage{}, 0
	}
	var inputPerM, outputPerM float64
	if cfg != nil {
		inputPerM = cfg.CostInputPerMTokens
		outputPerM = cfg.CostOutputPerMTokens
	}
	return usage, estimateUsageCost(usage, inputPerM, outputPerM)
}

func userFacingFatalToolResult(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.Contains(trimmed, "not exposed in the active toolset") {
		if strings.Contains(trimmed, "write_file") || strings.Contains(trimmed, "apply_patch") {
			return "Non posso modificare direttamente i file con il toolset attivo. La memoria del turno resta gestita dalla cattura automatica post-turn e, se non e' low-risk, dalla coda review."
		}
		return "Una capacita' interna richiesta non e' disponibile nel toolset attivo. Ho fermato l'azione invece di usare un permesso non esposto."
	}
	return raw
}

func estimateUsageCost(usage llm.TokenUsage, inputPerM, outputPerM float64) float64 {
	prompt := usage.PromptTokens
	completion := usage.CompletionTokens
	if prompt == 0 && completion == 0 {
		prompt = usage.TotalTokens
	}
	return (float64(prompt)*inputPerM + float64(completion)*outputPerM) / 1_000_000
}
