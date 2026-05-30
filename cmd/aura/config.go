// config subcommand dispatcher for `aura config {show|get|set}`. Lives in package
// main alongside cmd/aura/main.go's switch case "config", mirroring db.go:19-44.
// show: print the effective llm.Config with APIKey shown as REDACTED (D-24/D-28 —
// the real key value NEVER reaches stdout). get: read a dotted file-tier key
// (llm.model, llm.base_url, ...) from the effective config. set: read-modify-write
// ~/.aura/llm.json (creating the dir+file if absent), persisting only file-tier
// keys. Unknown key / bad usage -> stderr + os.Exit(1). The set path never persists
// the API key (it normally comes from .env/the environment, not this file).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
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
	default:
		configUsage()
		os.Exit(1)
	}
}

func configUsage() {
	fmt.Fprintln(os.Stderr, "usage: aura config {show|get <key>|set <key> <value>}")
	fmt.Fprintln(os.Stderr, "  keys: llm.provider llm.model llm.base_url llm.temperature llm.max_tokens llm.total_timeout_sec llm.connect_timeout_sec")
}

// configShow prints the effective config. config.Load fails-fast on an empty API
// key, so `show` resolves llm.Config directly (it tolerates an empty key — the
// operator may run `show` before setting one) and renders APIKey as REDACTED when
// a key IS present (D-24/D-28).
func configShow() {
	cfg, err := loadLLMConfigTolerant()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
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
	fmt.Printf("total_timeout_sec:   %d\n", cfg.TotalTimeoutSec)
	fmt.Printf("connect_timeout_sec: %d\n", cfg.ConnectTimeoutSec)
}

// configGet reads a single dotted key from the effective config.
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
	if err := writeConfigFile(path, raw); err != nil {
		fmt.Fprintln(os.Stderr, "config set:", err)
		os.Exit(1)
	}
	fmt.Printf("ok: set %s = %s in %s\n", key, value, path)
}

// loadLLMConfigTolerant resolves the effective llm.Config but, unlike llm.Load,
// does NOT fail on an empty API key — `aura config show/get` must work before a
// key is ever configured. It re-runs the load and swallows only ErrMissingAPIKey;
// any other (malformed-file / bad-env) error still surfaces.
func loadLLMConfigTolerant() (*llm.Config, error) {
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
		if err := setIntKey(raw, "max_tokens", value); err != nil {
			return err
		}
	case "llm.total_timeout_sec":
		if err := setIntKey(raw, "total_timeout_sec", value); err != nil {
			return err
		}
	case "llm.connect_timeout_sec":
		if err := setIntKey(raw, "connect_timeout_sec", value); err != nil {
			return err
		}
	case "llm.api_key":
		return fmt.Errorf("llm.api_key is not settable here — set OPENROUTER_API_KEY in .env or the environment (D-28)")
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

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s) //nolint:errcheck // marshaling a string never fails
	return b
}

func jsonNumber(s string) json.RawMessage { return json.RawMessage(s) }

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
