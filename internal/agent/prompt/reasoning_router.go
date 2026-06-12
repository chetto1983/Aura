package prompt

import (
	"encoding/json"
	"strings"
)

// ReasoningRouterSystemPrompt is the LLM-"oracle" prompt that classifies a turn
// into a reasoning tier. It backs both the agent's LLM-router fallback (when the
// embedding classifier abstains) and the self-improvement learner's oracle.
const ReasoningRouterSystemPrompt = "Classify the latest user request into one reasoning tier for the next assistant turn. Reply only JSON: {\"tier\":\"none\"|\"low\"|\"high\"}. none=greeting/simple stable fact/direct short transform. low=current web/news/weather/prices/schedules/lookups or small tool use. high=coding/debugging/design/proofs/scraping/multi-step analysis."

// ParseReasoningRouterTier extracts the tier from a router-LLM reply, tolerating
// surrounding prose/fences. Returns an empty (invalid) tier when unparseable.
func ParseReasoningRouterTier(raw string) ReasoningTier {
	var obj struct {
		Tier string `json:"tier"`
	}
	body := strings.TrimSpace(raw)
	if start := strings.Index(body, "{"); start >= 0 {
		if end := strings.LastIndex(body, "}"); end >= start {
			body = body[start : end+1]
		}
	}
	if err := json.Unmarshal([]byte(body), &obj); err == nil {
		return normalizeReasoningTier(obj.Tier)
	}
	return normalizeReasoningTier(raw)
}

func normalizeReasoningTier(s string) ReasoningTier {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "` \t\r\n\"'")
	s = strings.TrimPrefix(s, "json")
	s = strings.TrimSpace(s)
	switch s {
	case "none", "no", "off", "false":
		return ReasoningTierNone
	case "low", "small", "light":
		return ReasoningTierLow
	case "high", "deep":
		return ReasoningTierHigh
	default:
		return ""
	}
}
