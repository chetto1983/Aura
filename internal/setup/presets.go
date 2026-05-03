package setup

// Preset describes a one-click LLM provider configuration. The wizard
// renders these in a dropdown; selecting one populates base_url and the
// suggested model. The user can still override any field manually.
type Preset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	NeedsKey    bool   `json:"needs_key"`
	ProbePath   string `json:"probe_path"` // appended to base_url for /v1/models style listing
	Description string `json:"description"`
}

// LLMPresets covers the providers the .env example already documents
// (OpenAI / Mistral / Anthropic via openai-compatible / Ollama / Groq /
// DeepSeek / Together / Fireworks). The "custom" preset lets the user
// enter their own URL.
var LLMPresets = []Preset{
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
		ID:          "anthropic",
		Label:       "Anthropic Claude",
		BaseURL:     "https://api.anthropic.com/v1",
		Model:       "claude-sonnet-4-6",
		NeedsKey:    true,
		ProbePath:   "/models",
		Description: "Claude family. Native API; OpenAI-compatible mode used.",
	},
	{
		ID:          "ollama",
		Label:       "Ollama (free, local)",
		BaseURL:     "http://localhost:11434/v1",
		Model:       "llama3.1:8b",
		NeedsKey:    false,
		ProbePath:   "/models",
		Description: "Runs on your machine. No API key, no cost. Install from ollama.com.",
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

// PresetByID returns the matching preset or false.
func PresetByID(id string) (Preset, bool) {
	for _, p := range LLMPresets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
