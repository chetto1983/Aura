// config subcommand dispatcher for `aura config {show|get|set}`. Lives in package
// main alongside cmd/aura/main.go's switch case "config", mirroring db.go:19-44.
// show: print the effective llm.Config with APIKey shown as REDACTED (D-24/D-28 —
// the real key value NEVER reaches stdout). "Effective" here means all FIVE tiers the
// daemon itself resolves, aura.settings included — see applySettingsOverlay for why the
// database tier is not optional to a command that claims the word. get: read a dotted
// key (llm.model, llm.base_url, ...) off that same resolution. set: read-modify-write
// ~/.aura/llm.json (creating the dir+file if absent), persisting only file-tier
// keys. Unknown key / bad usage -> stderr + os.Exit(1). The set path never persists
// the API key (it normally comes from .env/the environment, not this file).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
)

// redactedAPIKey is the constant rendered in place of the real key by `show`
// (D-28). A test asserts it is present and the real key value is absent.
const redactedAPIKey = "REDACTED"

func runConfig(args []string) {
	if len(args) < 1 {
		configUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "show":
		configShow()
	case "get":
		configGet(args[1:])
	case "set":
		configSet(args[1:])
	case "validate":
		configValidate(args[1:])
	default:
		configUsage()
		os.Exit(1)
	}
}

func configUsage() {
	fmt.Fprintln(os.Stderr, "usage: aura config {show|get <key>|set <key> <value>|validate [--profile <p>] [--json]}")
	fmt.Fprintln(os.Stderr, "  keys: llm.provider llm.model llm.base_url llm.temperature llm.max_tokens llm.context_window llm.max_output_tokens llm.adaptive_reasoning llm.total_timeout_sec llm.connect_timeout_sec")
	fmt.Fprintln(os.Stderr, "  validate: lint the effective config against a runtime profile; non-zero exit if any Fatal violation")
}

// configShow prints the effective config. config.Load fails-fast on an empty API
// key, so `show` resolves llm.Config directly (it tolerates an empty key — the
// operator may run `show` before setting one) and renders APIKey as REDACTED when
// a key IS present (D-24/D-28).
func configShow() {
	cfg, note, err := loadLLMConfigAndOverlayNote()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}
	if note != "" {
		fmt.Fprintf(os.Stderr, "warning: %s — the values below are the file and environment tiers, which a running daemon may be overriding\n", note)
	}
	apiKey := ""
	if cfg.APIKey != "" {
		apiKey = redactedAPIKey
	}
	fmt.Printf("provider:            %s\n", cfg.Provider)
	fmt.Printf("model:               %s\n", cfg.Model)
	fmt.Printf("base_url:            %s\n", cfg.BaseURL)
	fmt.Printf("api_key:             %s\n", apiKey)
	fmt.Printf("temperature:         %g\n", cfg.Temperature)
	fmt.Printf("max_tokens:          %d\n", cfg.MaxTokens)
	fmt.Printf("adaptive_reasoning:  %t\n", cfg.AdaptiveReasoning)
	fmt.Printf("total_timeout_sec:   %d\n", cfg.TotalTimeoutSec)
	fmt.Printf("connect_timeout_sec: %d\n", cfg.ConnectTimeoutSec)
}

// configGet reads a single dotted key from the effective config, aura.settings included.
// It does not print the overlay note: a `get` is consumed by scripts, so a warning on
// stderr would be the wrong shape there — `show` is the command that explains itself.
func configGet(args []string) {
	if len(args) != 1 {
		configUsage()
		os.Exit(1)
	}
	cfg, err := loadLLMConfigTolerant()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}
	val, ok := getConfigKey(cfg, args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown config key %q\n", args[0])
		configUsage()
		os.Exit(1)
	}
	fmt.Println(val)
}

// configSet read-modify-writes ~/.aura/llm.json with the supplied file-tier key.
func configSet(args []string) {
	if len(args) != 2 {
		configUsage()
		os.Exit(1)
	}
	key, value := args[0], args[1]
	path, err := userConfigFilePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config set:", err)
		os.Exit(1)
	}
	raw, err := readConfigFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config set:", err)
		os.Exit(1)
	}
	if err := setConfigKey(raw, key, value); err != nil {
		fmt.Fprintln(os.Stderr, "config set:", err)
		configUsage()
		os.Exit(1)
	}
	if isConfigTokenKey(key) {
		cfg, loadErr := loadLLMConfigTolerant()
		if loadErr != nil {
			fmt.Fprintln(os.Stderr, "config set:", loadErr)
			os.Exit(1)
		}
		if err := validateConfigCandidate(cfg, key, value); err != nil {
			fmt.Fprintln(os.Stderr, "config set:", err)
			os.Exit(1)
		}
	}
	if err := writeConfigFile(path, raw); err != nil {
		fmt.Fprintln(os.Stderr, "config set:", err)
		os.Exit(1)
	}
	fmt.Printf("ok: set %s = %s in %s\n", key, value, path)
}

// settingsListerForCLI opens the keyless overlay pool and hands back a Lister over
// aura.settings, a closer, and -- when it cannot -- the reason why. It is a var so a
// test can drive both branches without a live Postgres, mirroring doctorLookupLLMKey's
// own seam in doctor.go.
var settingsListerForCLI = func(ctx context.Context) (settings.Lister, func(), string) {
	pool, ok, err := openSettingsOverlayPool(ctx)
	switch {
	case err != nil:
		return nil, nil, "aura.settings unreachable: " + err.Error()
	case !ok:
		return nil, nil, "no database configured, so aura.settings was not consulted"
	}
	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		pool.Close()
		return nil, nil, "aura.settings unreadable: " + err.Error()
	}
	return store, pool.Close, ""
}

// applySettingsOverlay copies the allowlisted, non-secret aura.settings rows onto the process
// environment exactly as the daemon does at boot (chat_boot_settings.go's
// resolveConfigAndPool), so the CLI resolves the SAME configuration `aura serve` is running;
// the stored key comes in through effectiveLLMKeyForCLI.
//
// Without it these commands report the compiled-in defaults as "effective" whenever the
// deployment is configured from the cockpit -- which is the normal case, since
// aura.settings is where those values are meant to live. Measured on 2026-09-09: an
// operator running Ollama saw `provider: openrouter`, `model:
// deepseek/deepseek-v4-flash:nitro` and the OpenRouter base URL, which are
// internal/llm/config.go's three default constants verbatim, with no llm.json on disk
// and the real backend recorded only in Postgres.
//
// Returns the reason it could not be applied, so the caller says so rather than passing
// a lower tier off as the effective one. An unreachable database is NOT fatal: reading
// the file and env tiers is still useful, and `config show` must keep working before the
// database exists.
func applySettingsOverlay(ctx context.Context) string {
	lister, closeLister, why := settingsListerForCLI(ctx)
	if lister == nil {
		return why
	}
	defer closeLister()
	if err := settings.OverlayEnv(ctx, lister); err != nil {
		return "aura.settings could not be applied: " + err.Error()
	}
	return ""
}

// loadLLMConfigTolerant resolves the effective llm.Config across every tier the daemon
// resolves — built-in default < .env < ~/.aura/llm.json < AURA_LLM_* < aura.settings —
// but, unlike llm.Load, does NOT fail on an empty API key: `aura config show/get` must
// work before a key is ever configured. Only ErrMissingAPIKey is swallowed; any other
// (malformed-file / bad-env) error still surfaces, as does a note when the database tier
// could not be read.
func loadLLMConfigTolerant() (*llm.Config, error) {
	cfg, _, err := loadLLMConfigAndOverlayNote()
	return cfg, err
}

// loadLLMConfigAndOverlayNote is loadLLMConfigTolerant for the one caller that prints
// the overlay note (`show`); the others discard it through the wrapper above.
func loadLLMConfigAndOverlayNote() (*llm.Config, string, error) {
	ctx := context.Background()
	note := applySettingsOverlay(ctx)
	cfg, err := resolveLLMConfigTiers()
	if err == nil {
		if key := effectiveLLMKeyForCLI(ctx); key != "" {
			cfg.APIKey = key
		}
	}
	return cfg, note, err
}

// effectiveLLMKeyForCLI is the key the daemon would use: the aura.settings row, which never
// reaches the environment, then the environment.
func effectiveLLMKeyForCLI(ctx context.Context) string {
	if lister, closeLister, _ := settingsListerForCLI(ctx); lister != nil {
		defer closeLister()
		if rows, err := lister.List(ctx); err == nil {
			for _, row := range rows {
				if row.Key == "OPENROUTER_API_KEY" && strings.TrimSpace(row.Value) != "" {
					return row.Value
				}
			}
		}
	}
	return os.Getenv("OPENROUTER_API_KEY")
}

// resolveLLMConfigTiers runs llm.Load's own tier chain, tolerating an empty API key so
// `show` works before one is set.
func resolveLLMConfigTiers() (*llm.Config, error) {
	cfg, err := llm.Load()
	if err == nil {
		return cfg, nil
	}
	if isMissingAPIKey(err) {
		// Re-run with a placeholder key so the rest of the chain (file + env
		// overrides) still applies, then blank it back out for display.
		os.Setenv("OPENROUTER_API_KEY", "x") //nolint:errcheck,gosec // transient placeholder so Load resolves the non-key tiers; blanked immediately
		cfg, err = llm.Load()
		os.Unsetenv("OPENROUTER_API_KEY") //nolint:errcheck
		if err != nil {
			return nil, err
		}
		cfg.APIKey = ""
		return cfg, nil
	}
	return nil, err
}

func isMissingAPIKey(err error) bool {
	return err != nil && strings.Contains(err.Error(), llm.ErrMissingAPIKey.Error())
}

// getConfigKey reads a dotted key off the resolved config (D-24). Returns the
// string rendering and ok=false for an unknown key.
func getConfigKey(cfg *llm.Config, key string) (string, bool) {
	switch key {
	case "llm.provider":
		return cfg.Provider, true
	case "llm.model":
		return cfg.Model, true
	case "llm.base_url":
		return cfg.BaseURL, true
	case "llm.temperature":
		return strconv.FormatFloat(cfg.Temperature, 'g', -1, 64), true
	case "llm.max_tokens":
		return strconv.Itoa(cfg.MaxTokens), true
	case "llm.context_window":
		return strconv.Itoa(cfg.ContextWindow), true
	case "llm.max_output_tokens":
		return strconv.Itoa(cfg.MaxOutputTokens), true
	case "llm.adaptive_reasoning":
		return strconv.FormatBool(cfg.AdaptiveReasoning), true
	case "llm.total_timeout_sec":
		return strconv.Itoa(cfg.TotalTimeoutSec), true
	case "llm.connect_timeout_sec":
		return strconv.Itoa(cfg.ConnectTimeoutSec), true
	default:
		return "", false
	}
}

// setConfigKey applies a dotted key=value onto the parsed llm.json map. String
// keys are stored verbatim; numeric keys are validated (a bad number is a clear
// error, never silently persisted). The api_key is intentionally NOT settable
// here (D-28: it comes from .env / the environment, not this file).
func setConfigKey(raw map[string]json.RawMessage, key, value string) error {
	switch key {
	case "llm.provider":
		raw["provider"] = jsonString(value)
	case "llm.model":
		raw["model"] = jsonString(value)
	case "llm.base_url":
		raw["base_url"] = jsonString(value)
	case "llm.temperature":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("llm.temperature %q: not a valid float", value)
		}
		raw["temperature"] = jsonNumber(strconv.FormatFloat(f, 'g', -1, 64))
	case "llm.max_tokens":
		if err := setPositiveIntKey(raw, "max_tokens", value); err != nil {
			return err
		}
	case "llm.context_window":
		if err := setPositiveIntKey(raw, "context_window", value); err != nil {
			return err
		}
	case "llm.max_output_tokens":
		if err := setPositiveIntKey(raw, "max_output_tokens", value); err != nil {
			return err
		}
	case "llm.adaptive_reasoning":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("llm.adaptive_reasoning %q: not a valid bool", value)
		}
		raw["adaptive_reasoning"] = jsonBool(b)
	case "llm.total_timeout_sec":
		if err := setIntKey(raw, "total_timeout_sec", value); err != nil {
			return err
		}
	case "llm.connect_timeout_sec":
		if err := setIntKey(raw, "connect_timeout_sec", value); err != nil {
			return err
		}
	case "llm.api_key":
		return fmt.Errorf("llm.api_key is not settable here — Aura mints it once an admin connects OpenRouter in the first-run setup (D-28)")
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

func setIntKey(raw map[string]json.RawMessage, jsonKey, value string) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s %q: not a valid integer", jsonKey, value)
	}
	raw[jsonKey] = jsonNumber(strconv.Itoa(n))
	return nil
}

func setPositiveIntKey(raw map[string]json.RawMessage, jsonKey, value string) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s %q: not a valid integer", jsonKey, value)
	}
	if n <= 0 {
		return fmt.Errorf("%s %d: must be positive", jsonKey, n)
	}
	raw[jsonKey] = jsonNumber(strconv.Itoa(n))
	return nil
}

func isConfigTokenKey(key string) bool {
	switch key {
	case "llm.max_tokens", "llm.context_window", "llm.max_output_tokens":
		return true
	default:
		return false
	}
}

func validateConfigCandidate(cfg *llm.Config, key, value string) error {
	if !isConfigTokenKey(key) {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	candidate := *cfg
	switch key {
	case "llm.max_tokens":
		candidate.MaxTokens = parsed
	case "llm.context_window":
		candidate.ContextWindow = parsed
	case "llm.max_output_tokens":
		candidate.MaxOutputTokens = parsed
	}
	return candidate.Validate()
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s) //nolint:errcheck // marshaling a string never fails
	return b
}

func jsonNumber(s string) json.RawMessage { return json.RawMessage(s) }

func jsonBool(v bool) json.RawMessage {
	if v {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}

// userConfigFilePath returns ~/.aura/llm.json, creating no file yet.
func userConfigFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".aura", "llm.json"), nil
}

// readConfigFile returns the parsed llm.json as a raw map (an absent file yields
// an empty map so `set` creates it on write).
func readConfigFile(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-owned config path
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	return raw, nil
}

// writeConfigFile creates ~/.aura (if absent) and writes the map back as indented
// JSON with 0600 perms (the file may later hold a key).
func writeConfigFile(path string, raw map[string]json.RawMessage) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
