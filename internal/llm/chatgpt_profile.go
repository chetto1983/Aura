package llm

import "maps"

// ChatGPTProvider identifies subscription usage through Sign in with ChatGPT.
const ChatGPTProvider = "chatgpt"

// ChatGPTBaseURL is the sole permitted direct-inference endpoint.
const ChatGPTBaseURL = "https://api.openai.com/v1"

// The plan catalog guarantees model names, but not context limits. This is Aura's
// conservative working budget until the operator pins a window, not a model claim.
const chatGPTWorkingBudget = 32_768

// ResolveChatGPTProfile replaces inherited API credentials, prices and budgets.
func (c *Config) ResolveChatGPTProfile() error {
	candidate := *c
	candidate.APIKey = ""
	candidate.Headers = nil
	candidate.Prices = maps.Clone(c.Prices)
	delete(candidate.Prices, candidate.Model)
	candidate.CostStatus = CostStatusSubscriptionIncluded
	if !candidate.ContextWindowConfigured {
		candidate.ContextWindow = chatGPTWorkingBudget
	}
	if !candidate.MaxOutputTokensConfigured {
		candidate.MaxOutputTokens = DerivedMaxOutputTokens(candidate.ContextWindow)
	}
	candidate.SupportedReasoningEfforts = nil
	candidate.ReasoningMandatory = false
	if err := candidate.Validate(); err != nil {
		return err
	}
	*c = candidate
	return nil
}
