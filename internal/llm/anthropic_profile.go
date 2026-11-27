package llm

// AnthropicProvider identifies the native Claude Messages API route.
const AnthropicProvider = "anthropic"

// AnthropicBaseURL is the sole permitted endpoint; the house key is never sent elsewhere.
const AnthropicBaseURL = "https://api.anthropic.com"

// ResolveAnthropicProfile drops inherited credentials and leaves the cost unknown. It
// never touches the network: the key lives in the settings store and the client reads
// it per call, and the model's limits and price are filled by the account catalogue.
func (c *Config) ResolveAnthropicProfile() error {
	candidate := *c
	candidate.APIKey = ""
	candidate.Headers = nil
	candidate.CostStatus = CostStatusUnknown
	if err := candidate.Validate(); err != nil {
		return err
	}
	*c = candidate
	return nil
}
