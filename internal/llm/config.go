package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

// Built-in defaults (D-22). The const block is the single source for the
// load-order base tier; later tiers (.env, ~/.aura/llm.json, AURA_LLM_*)
// override these. The model id uses the `:nitro` routing variant.
const (
	defaultProvider          = "openrouter"
	defaultModel             = "deepseek/deepseek-v4-flash:nitro"
	defaultBaseURL           = "https://openrouter.ai/api/v1"
	defaultTemperature       = 0.7
	defaultMaxTokens         = 4096
	defaultTotalTimeoutSec   = 120
	defaultConnectTimeoutSec = 10
	// defaultStreamIdleTimeoutSec bounds a stream stall (B-08): no bytes at all —
	// not even an SSE keep-alive comment — within this window aborts the read as a
	// retryable transport stall. The watchdog resets on ANY bytes (keep-alives
	// included), so a long reasoning phase that streams `: OPENROUTER PROCESSING`
	// pings does NOT trip it; only a dead connection does. It fires before the 120s
	// total-call timeout so a hung upstream is caught early. 0 disables it.
	defaultStreamIdleTimeoutSec = 60
	defaultAdaptiveReasoning    = true
	defaultShowReasoning        = true

	// L2 budget inputs (Phase 4 AM, resolves OPEN QUESTION 2). ContextWindow is
	// the ~1M DeepSeek-V4 window; MaxOutputTokens is a sane reservation cap. The
	// conversations L2 formula reads these: hard_cap = ContextWindow -
	// max(MaxOutputTokens, 20000) - 13000.
	defaultContextWindow   = 1_000_000
	defaultMaxOutputTokens = 32_768

	// defaultCompletionGate is the production default for the completion critic
	// gate (amendment #54 / D-43): ON. The zero-value Config (hand-built in unit
	// tests) leaves it false, so the gate is OFF unless a test opts in — Load is
	// the only path that turns it on.
	defaultCompletionGate = true

	// OpenRouter attribution headers (D-20): visibility in the OpenRouter
	// dashboard and possible discount tiers. Sent on every request.
	headerReferer = "https://github.com/chetto1983/aura"
	headerTitle   = "Aura"
)

// AURA_LLM_* env var names (AURA_<DOMAIN>_<UNIT> convention). OPENROUTER_API_KEY
// stays the canonical third-party secret name (NOT renamed to AURA_*).
const (
	envAPIKey               = "OPENROUTER_API_KEY" //nolint:gosec // G101: env var NAME, not a credential
	envProvider             = "AURA_LLM_PROVIDER"
	envModel                = "AURA_LLM_MODEL"
	envBaseURL              = "AURA_LLM_BASE_URL"
	envTemperature          = "AURA_LLM_TEMPERATURE"
	envMaxTokens            = "AURA_LLM_MAX_TOKENS" //nolint:gosec // G101 false positive: env var NAME, not a credential
	envAdaptiveReasoning    = "AURA_LLM_ADAPTIVE_REASONING"
	envShowReasoning        = "AURA_SHOW_REASONING"
	envTotalTimeoutSec      = "AURA_LLM_TOTAL_TIMEOUT_SEC"
	envConnectTimeoutSec    = "AURA_LLM_CONNECT_TIMEOUT_SEC"
	envStreamIdleTimeoutSec = "AURA_LLM_STREAM_IDLE_TIMEOUT_SEC"
	envContextWindow        = "AURA_MODEL_CONTEXT_WINDOW"
	envMaxOutputTokens      = "AURA_MODEL_MAX_OUTPUT_TOKENS" //nolint:gosec // G101 false positive: env var NAME, not a credential

	// Completion gate knobs (amendment #54 / D-43). AURA_COMPLETION_* domain
	// (not AURA_LLM_*) but they ride in llm.Config because the agent reads cfg
	// directly — no extra plumbing through runner.Deps / LlmAgentConfig.
	envCompletionGate        = "AURA_COMPLETION_GATE"
	envCompletionCriticModel = "AURA_COMPLETION_CRITIC_MODEL"

	// envOpenRouterMiddleOut arms the fix-plan 1.11 overflow belt (see
	// Config.OpenRouterMiddleOut). Default OFF.
	envOpenRouterMiddleOut = "AURA_LLM_OPENROUTER_MIDDLE_OUT"
)

// ErrMissingAPIKey is the clear, non-panic error surfaced when the API key is
// empty after the full load-order chain (D-22, SPEC Req#5). Tests assert this
// sentinel, so callers compare with errors.Is rather than the string.
var ErrMissingAPIKey = errors.New("llm: API key is empty (set OPENROUTER_API_KEY in .env or the environment)")

// Config is the resolved LLM configuration (D-22). APIKey is a plain field set
// only by Load and read only at request-build time; the struct has no String()
// or custom log representation, so a %+v dump exposes it — callers MUST NOT log
// the struct (D-28 structural redaction; the Plan-04 anti-leak test enforces it).
type Config struct {
	Provider          string
	Model             string
	BaseURL           string
	APIKey            string
	TotalTimeoutSec   int
	ConnectTimeoutSec int
	// StreamIdleTimeoutSec bounds the gap with NO bytes on an open stream (B-08).
	// >0 arms a per-read idle watchdog in the openai_compat client that aborts a
	// stalled stream with a retryable ErrStreamIdleTimeout; 0 disables it.
	StreamIdleTimeoutSec int
	Temperature          float64
	MaxTokens            int
	AdaptiveReasoning    bool

	// ShowReasoning surfaces the model's chain-of-thought instead of redacting it
	// (AURA_SHOW_REASONING, default true). It is the SINGLE master switch for live
	// CoT: the adaptive-reasoning policy reads it to request reasoning UNEXCLUDED
	// (exclude:false → the provider streams the reasoning text; with the default
	// true the stream carries zero reasoning deltas), and the composition root
	// propagates it to the Telegram channel so the AG-UI translator passes the real
	// delta through and the status pane renders the live 💭 window. Default true
	// surfaces the live CoT end-to-end (set AURA_SHOW_REASONING=false to redact).
	ShowReasoning   bool
	ContextWindow   int // total model context window (tokens); L2 budget input
	MaxOutputTokens int // output-token reservation cap; L2 budget input
	Headers         map[string]string
	Prices          map[string]Price

	// CompletionGate enables the agent's completion critic gate (amendment #54 /
	// D-43): a voluntary termination (text_response / content-stop) on a turn
	// that mutated host state is verified by a critic call before it is accepted.
	// Zero-value false (off) so hand-built test configs skip it; Load() defaults
	// it on. CompletionCriticModel overrides the critic model; empty → Model.
	CompletionGate        bool
	CompletionCriticModel string

	// OpenRouterMiddleOut is the opt-in overflow belt (fix-plan 1.11): when ON
	// (AND the resolved reasoning target is OpenRouter) the wire layer sets
	// transforms:["middle-out"], OpenRouter's provider-side lossy truncation
	// with no tool-pair awareness. It is a last-resort net against a hard 400
	// "context length exceeded" when the local trim under-counts on the
	// OpenRouter path — NOT a compaction mechanism (Aura's own context
	// management stays primary). Default OFF; dormant while Aura runs local
	// (llamacpp target never sets the wire key regardless of this knob).
	// AURA_LLM_OPENROUTER_MIDDLE_OUT.
	OpenRouterMiddleOut bool
}

// fileConfig mirrors the JSON shape of ~/.aura/llm.json. Every field is a
// pointer so an absent key leaves the lower-tier value untouched (an explicit
// JSON null/zero would otherwise clobber a default). The prices map overrides
// the seeded table entry-by-entry.
type fileConfig struct {
	Provider             *string           `json:"provider,omitempty"`
	Model                *string           `json:"model,omitempty"`
	BaseURL              *string           `json:"base_url,omitempty"`
	APIKey               *string           `json:"api_key,omitempty"`
	TotalTimeoutSec      *int              `json:"total_timeout_sec,omitempty"`
	ConnectTimeoutSec    *int              `json:"connect_timeout_sec,omitempty"`
	StreamIdleTimeoutSec *int              `json:"stream_idle_timeout_sec,omitempty"`
	Temperature          *float64          `json:"temperature,omitempty"`
	MaxTokens            *int              `json:"max_tokens,omitempty"`
	AdaptiveReasoning    *bool             `json:"adaptive_reasoning,omitempty"`
	ShowReasoning        *bool             `json:"show_reasoning,omitempty"`
	ContextWindow        *int              `json:"context_window,omitempty"`
	MaxOutputTokens      *int              `json:"max_output_tokens,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	Prices               map[string]Price  `json:"prices,omitempty"`
}

// Load resolves the LLM configuration with the locked 4-tier precedence
// (SPEC Req#5, D-22):
//
//	built-in default < .env (OPENROUTER_API_KEY -> APIKey) < ~/.aura/llm.json < AURA_LLM_*
//
// A missing .env file or a missing llm.json is NOT an error. A malformed-but-set
// AURA_LLM_* numeric value IS fail-fast (budget.go style). An empty APIKey after
// the whole chain returns ErrMissingAPIKey — never a panic, never a silent default.
func Load() (*Config, error) {
	return load(false)
}

// LoadAllowEmptyKey resolves the same configuration tiers as Load but permits an
// empty API key. Long-lived daemons use this to boot setup channels; the LLM call
// path still fails closed before any upstream request.
func LoadAllowEmptyKey() (*Config, error) {
	return load(true)
}

func load(allowEmptyKey bool) (*Config, error) {
	_ = godotenv.Load() // best-effort; .env feeds OPENROUTER_API_KEY into the env

	cfg := &Config{
		Provider:             defaultProvider,
		Model:                defaultModel,
		BaseURL:              defaultBaseURL,
		Temperature:          defaultTemperature,
		MaxTokens:            defaultMaxTokens,
		AdaptiveReasoning:    defaultAdaptiveReasoning,
		ShowReasoning:        defaultShowReasoning,
		ContextWindow:        defaultContextWindow,
		MaxOutputTokens:      defaultMaxOutputTokens,
		TotalTimeoutSec:      defaultTotalTimeoutSec,
		ConnectTimeoutSec:    defaultConnectTimeoutSec,
		StreamIdleTimeoutSec: defaultStreamIdleTimeoutSec,
		Headers: map[string]string{
			"HTTP-Referer": headerReferer,
			"X-Title":      headerTitle,
		},
		Prices: defaultPrices(),
	}

	// Tier 2: .env / environment supplies the API key under its canonical name.
	cfg.APIKey = os.Getenv(envAPIKey)

	// Tier 3: ~/.aura/llm.json (absent file is fine).
	if err := applyFileConfig(cfg); err != nil {
		return nil, err
	}

	// Tier 4: AURA_LLM_* env overrides (fail-fast on malformed numbers).
	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}

	// Completion gate (amendment #54 / D-43): default ON. A malformed bool falls
	// back to the default (non-fatal, like the root config bool knobs) — a typo
	// in an opt-out toggle must not block startup. Empty critic model → the loop
	// model is reused at call time.
	cfg.CompletionGate = envBool(envCompletionGate, defaultCompletionGate)
	cfg.CompletionCriticModel = os.Getenv(envCompletionCriticModel)
	cfg.OpenRouterMiddleOut = envBool(envOpenRouterMiddleOut, false)

	if cfg.APIKey == "" && !allowEmptyKey {
		return nil, ErrMissingAPIKey
	}
	return cfg, nil
}

// configFilePath returns ~/.aura/llm.json. A failure to resolve the home dir is
// not fatal — it yields an empty path that applyFileConfig treats as "no file".
func configFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".aura", "llm.json")
}

// applyFileConfig overlays ~/.aura/llm.json onto cfg. A missing file is not an
// error; a present-but-unparseable file IS (a corrupt config should fail loudly,
// not be silently ignored).
func applyFileConfig(cfg *Config) error {
	path := configFilePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // operator-owned config path, not user input
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("llm: read %s: %w", path, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("llm: parse %s: %w", path, err)
	}
	overlayFile(cfg, &fc)
	return nil
}

// overlayFile applies the non-nil fileConfig fields onto cfg.
func overlayFile(cfg *Config, fc *fileConfig) {
	if fc.Provider != nil {
		cfg.Provider = *fc.Provider
	}
	if fc.Model != nil {
		cfg.Model = *fc.Model
	}
	if fc.BaseURL != nil {
		cfg.BaseURL = *fc.BaseURL
	}
	if fc.APIKey != nil {
		cfg.APIKey = *fc.APIKey
	}
	if fc.TotalTimeoutSec != nil {
		cfg.TotalTimeoutSec = *fc.TotalTimeoutSec
	}
	if fc.ConnectTimeoutSec != nil {
		cfg.ConnectTimeoutSec = *fc.ConnectTimeoutSec
	}
	if fc.StreamIdleTimeoutSec != nil {
		cfg.StreamIdleTimeoutSec = *fc.StreamIdleTimeoutSec
	}
	if fc.Temperature != nil {
		cfg.Temperature = *fc.Temperature
	}
	if fc.MaxTokens != nil {
		cfg.MaxTokens = *fc.MaxTokens
	}
	if fc.AdaptiveReasoning != nil {
		cfg.AdaptiveReasoning = *fc.AdaptiveReasoning
	}
	if fc.ShowReasoning != nil {
		cfg.ShowReasoning = *fc.ShowReasoning
	}
	if fc.ContextWindow != nil {
		cfg.ContextWindow = *fc.ContextWindow
	}
	if fc.MaxOutputTokens != nil {
		cfg.MaxOutputTokens = *fc.MaxOutputTokens
	}
	for k, v := range fc.Headers {
		cfg.Headers[k] = v
	}
	for k, v := range fc.Prices {
		cfg.Prices[k] = v
	}
}

// applyEnvOverrides applies AURA_LLM_* env values onto cfg. String knobs use
// silent presence checks; numeric knobs are fail-fast on a set-but-malformed
// value (an operator typo must not be silently absorbed into a default).
func applyEnvOverrides(cfg *Config) error {
	if v := os.Getenv(envProvider); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv(envModel); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv(envBaseURL); v != "" {
		cfg.BaseURL = v
	}
	if v, ok, err := envFloat(envTemperature); err != nil {
		return err
	} else if ok {
		cfg.Temperature = v
	}
	if v, ok, err := envInt(envMaxTokens); err != nil {
		return err
	} else if ok {
		cfg.MaxTokens = v
	}
	if v := os.Getenv(envAdaptiveReasoning); v != "" {
		cfg.AdaptiveReasoning = envBool(envAdaptiveReasoning, cfg.AdaptiveReasoning)
	}
	if v := os.Getenv(envShowReasoning); v != "" {
		cfg.ShowReasoning = envBool(envShowReasoning, cfg.ShowReasoning)
	}
	if v, ok, err := envInt(envContextWindow); err != nil {
		return err
	} else if ok {
		cfg.ContextWindow = v
	}
	if v, ok, err := envInt(envMaxOutputTokens); err != nil {
		return err
	} else if ok {
		cfg.MaxOutputTokens = v
	}
	if v, ok, err := envInt(envTotalTimeoutSec); err != nil {
		return err
	} else if ok {
		cfg.TotalTimeoutSec = v
	}
	if v, ok, err := envInt(envConnectTimeoutSec); err != nil {
		return err
	} else if ok {
		cfg.ConnectTimeoutSec = v
	}
	if v, ok, err := envInt(envStreamIdleTimeoutSec); err != nil {
		return err
	} else if ok {
		cfg.StreamIdleTimeoutSec = v
	}
	return nil
}

// envInt reads key as an int. ok is false when unset/empty; a set-but-malformed
// value is a fail-fast error (D-22, mirrors budget.go envIntFailFast).
func envInt(key string) (val int, ok bool, err error) {
	v := os.Getenv(key)
	if v == "" {
		return 0, false, nil
	}
	n, perr := strconv.Atoi(v)
	if perr != nil {
		return 0, false, fmt.Errorf("%s=%q: not a valid integer", key, v)
	}
	return n, true, nil
}

// envBool reads key as a bool, falling back to `fallback` when unset, empty, or
// malformed. Unlike envInt/envFloat this is NOT fail-fast: the completion-gate
// toggle is operative, not budget-load-bearing — a typo should not block boot
// (mirrors the root config envBoolDefault).
func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// envFloat reads key as a float, fail-fast on a set-but-malformed value.
func envFloat(key string) (val float64, ok bool, err error) {
	v := os.Getenv(key)
	if v == "" {
		return 0, false, nil
	}
	f, perr := strconv.ParseFloat(v, 64)
	if perr != nil {
		return 0, false, fmt.Errorf("%s=%q: not a valid float", key, v)
	}
	return f, true, nil
}
