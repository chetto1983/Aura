package api

// Preset describes a one-click LLM provider configuration. The wizard
// renders these in a dropdown; selecting one populates base_url and the
// suggested model. The user can still override any field manually.
type SetupPreset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	NeedsKey    bool   `json:"needs_key"`
	ProbePath   string `json:"probe_path"` // appended to base_url for /v1/models style listing
	Description string `json:"description"`
}

// SetupLLMPresets covers common OpenAI-compatible providers. The "custom" preset
// lets the user enter any compatible URL, while OpenRouter is just one hosted
// routing option rather than a special Aura integration.
var SetupLLMPresets = []SetupPreset{
	{
		ID:          "openai",
		Label:       "OpenAI",
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4o-mini",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "Most popular, paid. Get a key at platform.openai.com.",
	},
	{
		ID:          "mistral",
		Label:       "Mistral",
		BaseURL:     "https://api.mistral.ai/v1",
		Model:       "mistral-large-latest",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "EU-based, cheaper. Get a key at console.mistral.ai.",
	},
	{
		ID:          "openrouter",
		Label:       "OpenRouter",
		BaseURL:     "https://openrouter.ai/api/v1",
		Model:       "deepseek/deepseek-chat",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "OpenAI-compatible router for many hosted models.",
	},
	{
		ID:          "groq",
		Label:       "Groq",
		BaseURL:     "https://api.groq.com/openai/v1",
		Model:       "llama-3.1-70b-versatile",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "Fast inference, free tier available.",
	},
	{
		ID:          "deepseek",
		Label:       "DeepSeek",
		BaseURL:     "https://api.deepseek.com",
		Model:       "deepseek-chat",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "Cheap, OpenAI-compatible.",
	},
	{
		ID:          "together",
		Label:       "Together AI",
		BaseURL:     "https://api.together.xyz/v1",
		Model:       "meta-llama/Llama-3.1-70B-Instruct-Turbo",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "Open-model hosting.",
	},
	{
		ID:          "custom",
		Label:       "Custom (enter URL manually)",
		BaseURL:     "",
		Model:       "",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "Any OpenAI-compatible endpoint.",
	},
}

// SetupPresetByID returns the matching preset or false.
func SetupPresetByID(id string) (SetupPreset, bool) {
	for _, p := range SetupLLMPresets {
		if p.ID == id {
			return p, true
		}
	}
	return SetupPreset{}, false
}
