package prompt

import (
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

const (
	noReasoningMaxTokens    = 512
	smallReasoningMaxTokens = 2048
)

// applyAdaptiveReasoning maps the latest genuine user request to a conservative
// OpenRouter reasoning tier. The production request remains provider-neutral:
// Build populates llm.Request.Reasoning, and the wire layer decides how to
// serialize it.
func applyAdaptiveReasoning(req *llm.Request, provider string, cfg llm.Config, history []llm.Message) {
	if !cfg.AdaptiveReasoning || !isOpenRouterReasoningTarget(provider, cfg.BaseURL) {
		return
	}

	tier := classifyReasoningTier(lastGenuineUserContent(history))
	req.Reasoning = tier.reasoning()
	req.MaxTokens = tier.maxTokens(cfg.MaxTokens)
}

func isOpenRouterReasoningTarget(provider, baseURL string) bool {
	if !strings.EqualFold(provider, "openrouter") {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(baseURL))
	return base == "" || strings.Contains(base, "openrouter.ai")
}

type reasoningTier int

const (
	tierNoReasoning reasoningTier = iota
	tierSmallReasoning
	tierDeepReasoning
)

func (t reasoningTier) reasoning() llm.ReasoningConfig {
	switch t {
	case tierDeepReasoning:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh, Exclude: boolPtr(false)}
	case tierSmallReasoning:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortLow, Exclude: boolPtr(false)}
	default:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortNone, Exclude: boolPtr(true)}
	}
}

func (t reasoningTier) maxTokens(configuredMax int) int {
	switch t {
	case tierDeepReasoning:
		return configuredOrDefault(configuredMax)
	case tierSmallReasoning:
		return cappedTokens(smallReasoningMaxTokens, configuredMax)
	default:
		return cappedTokens(noReasoningMaxTokens, configuredMax)
	}
}

func classifyReasoningTier(content string) reasoningTier {
	q := strings.ToLower(content)
	deepIndicators := []string{
		"analyze", "analyse", "compare", "debug", "prove", "justify", "derive", "design",
		"tradeoff", "tradeoffs", "architecture", "distributed", "multi-step",
		"step by step", "complex", "why", "how", "failure", "production",
		"scheduler", "memory retrieval", "contradiction", "script", "scraping",
		"scrape", "crawler", "codice", "codifica", "implement", "implementa",
	}
	smallIndicators := []string{
		"cerca", "notizie", "news", "search", "trova", "aggiornamenti",
		"lookup", "ricerca", "recupera", "fonti", "sources",
	}

	deepHits := countIndicators(q, deepIndicators)
	smallHits := countIndicators(q, smallIndicators)
	wordCount := len(strings.Fields(content))
	deepScore := float64(deepHits)*0.35 + minFloat(float64(wordCount)/40.0, 0.55)
	if deepHits >= 1 && deepScore >= 0.55 {
		return tierDeepReasoning
	}
	if smallHits >= 1 {
		return tierSmallReasoning
	}
	return tierNoReasoning
}

func lastGenuineUserContent(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role != llm.RoleUser || strings.TrimSpace(m.Content) == "" {
			continue
		}
		if isSyntheticUserHint(m.Content) {
			continue
		}
		return m.Content
	}
	return ""
}

func isSyntheticUserHint(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "<budget>") ||
		strings.HasPrefix(trimmed, "<workspace>") ||
		strings.HasPrefix(trimmed, "Stop calling tools.") ||
		strings.HasPrefix(trimmed, "You have run out of tool-call budget") ||
		strings.HasPrefix(trimmed, "You have already called `") ||
		strings.HasPrefix(trimmed, "Completion check FAILED:")
}

func countIndicators(q string, indicators []string) int {
	hits := 0
	for _, indicator := range indicators {
		if strings.Contains(q, indicator) {
			hits++
		}
	}
	return hits
}

func cappedTokens(target, configuredMax int) int {
	if configuredMax <= 0 {
		return target
	}
	if target > configuredMax {
		return configuredMax
	}
	return target
}

func configuredOrDefault(configuredMax int) int {
	if configuredMax > 0 {
		return configuredMax
	}
	return 4096
}

func boolPtr(v bool) *bool { return &v }

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
