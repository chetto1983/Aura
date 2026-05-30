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
// override these. The model id uses the `:exacto` routing variant (OpenRouter
// curated tool-calling quality).
const (
	defaultProvider          = "openrouter"
	defaultModel             = "deepseek/deepseek-v4-flash:exacto"
	defaultBaseURL           = "https://openrouter.ai/api/v1"
	defaultTemperature       = 0.7
	defaultMaxTokens         = 4096
	defaultTotalTimeoutSec   = 120
	defaultConnectTimeoutSec = 10

	// L2 budget inputs (Phase 4 AM, resolves OPEN QUESTION 2). ContextWindow is
	// the ~1M DeepSeek-V4 window; MaxOutputTokens is a sane reservation cap. The
	// conversations L2 formula reads these: hard_cap = ContextWindow -
	// max(MaxOutputTokens, 20000) - 13000.
	defaultContextWindow   = 1_000_000
	defaultMaxOutputTokens = 32_768

	// OpenRouter attribution headers (D-20): visibility in the OpenRouter
	// dashboard and possible discount tiers. Sent on every request.
	headerReferer = "https://github.com/chetto1983/aura"
	headerTitle   = "Aura"
)

// AURA_LLM_* env var names (AURA_<DOMAIN>_<UNIT> convention). OPENROUTER_API_KEY
// stays the canonical third-party secret name (NOT renamed to AURA_*).
const (
	envAPIKey            = "OPENROUTER_API_KEY" //nolint:gosec // G101: env var NAME, not a credential
	envModel             = "AURA_LLM_MODEL"
	envBaseURL           = "AURA_LLM_BASE_URL"
	envTemperature       = "AURA_LLM_TEMPERATURE"
	envMaxTokens         = "AURA_LLM_MAX_TOKENS" //nolint:gosec // G101 false positive: env var NAME, not a credential
	envTotalTimeoutSec   = "AURA_LLM_TOTAL_TIMEOUT_SEC"
	envConnectTimeoutSec = "AURA_LLM_CONNECT_TIMEOUT_SEC"
	envContextWindow     = "AURA_MODEL_CONTEXT_WINDOW"
	envMaxOutputTokens   = "AURA_MODEL_MAX_OUTPUT_TOKENS" //nolint:gosec // G101 false positive: env var NAME, not a credential
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
	Temperature       float64
	MaxTokens         int
	ContextWindow     int // total model context window (tokens); L2 budget input
	MaxOutputTokens   int // output-token reservation cap; L2 budget input
	Headers           map[string]string
	Prices            map[string]Price
}

// fileConfig mirrors the JSON shape of ~/.aura/llm.json. Every field is a
// pointer so an absent key leaves the lower-tier value untouched (an explicit
// JSON null/zero would otherwise clobber a default). The prices map overrides
// the seeded table entry-by-entry.
type fileConfig struct {
	Provider          *string           `json:"provider,omitempty"`
	Model             *string           `json:"model,omitempty"`
	BaseURL           *string           `json:"base_url,omitempty"`
	APIKey            *string           `json:"api_key,omitempty"`
	TotalTimeoutSec   *int              `json:"total_timeout_sec,omitempty"`
	ConnectTimeoutSec *int              `json:"connect_timeout_sec,omitempty"`
	Temperature       *float64          `json:"temperature,omitempty"`
	MaxTokens         *int              `json:"max_tokens,omitempty"`
	ContextWindow     *int              `json:"context_window,omitempty"`
	MaxOutputTokens   *int              `json:"max_output_tokens,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Prices            map[string]Price  `json:"prices,omitempty"`
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
	_ = godotenv.Load() // best-effort; .env feeds OPENROUTER_API_KEY into the env

	cfg := &Config{
		Provider:          defaultProvider,
		Model:             defaultModel,
		BaseURL:           defaultBaseURL,
		Temperature:       defaultTemperature,
		MaxTokens:         defaultMaxTokens,
		ContextWindow:     defaultContextWindow,
		MaxOutputTokens:   defaultMaxOutputTokens,
		TotalTimeoutSec:   defaultTotalTimeoutSec,
		ConnectTimeoutSec: defaultConnectTimeoutSec,
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

	if cfg.APIKey == "" {
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
	if fc.Temperature != nil {
		cfg.Temperature = *fc.Temperature
	}
	if fc.MaxTokens != nil {
		cfg.MaxTokens = *fc.MaxTokens
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
