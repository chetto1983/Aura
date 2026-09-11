package agui

// settings_api_validate.go holds the Settings API's value checks, split out of settings_api.go
// (600-LOC cap).

import (
	"errors"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/openrouterprovision"
	"github.com/chetto1983/aura/internal/settings"
)

var (
	errInvalidInt         = errors.New("value must be an integer")
	errInvalidBool        = errors.New("value must be a boolean (true/false)")
	errInvalidServicesCap = errors.New("the services cap must be a USD amount above zero")
)

// validateSettingKeyValue holds the checks one key needs beyond its Kind. The services cap
// becomes the limit of the key Aura mints for speech, embeddings and vision, so a value the
// provider cannot take is refused here rather than failing at mint time. Empty leaves it unset.
func validateSettingKeyValue(key, value string) error {
	if key != servicesCapSetting || strings.TrimSpace(value) == "" {
		return nil
	}
	limit, err := openrouterprovision.NewUSDCapFromString(value)
	if err != nil || limit <= 0 {
		return errInvalidServicesCap
	}
	return nil
}

// validateSettingValue rejects a value that does not parse for its Kind (an int
// knob like AURA_LOOP_MAX_STEPS must be an int; a bool like AURA_MEMORY_PRELOAD_ENABLED
// must parse) so a bad value never reaches config.Load's silent default fallback.
func validateSettingValue(kind settings.Kind, value string) error {
	switch kind {
	case settings.KindInt:
		if _, err := strconv.Atoi(value); err != nil {
			return errInvalidInt
		}
	case settings.KindBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return errInvalidBool
		}
	}
	return nil
}

func isLLMTokenSetting(key string) bool {
	switch key {
	case "AURA_LLM_MAX_TOKENS",
		"AURA_MODEL_CONTEXT_WINDOW",
		"AURA_MODEL_MAX_OUTPUT_TOKENS":
		return true
	default:
		return false
	}
}

func validatePendingLLMTokenSetting(rows []sqlc.AuraSettings, key, value string) error {
	cfg, err := llm.LoadAllowEmptyKey()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if isLLMTokenSetting(row.Key) {
			if err := applyLLMTokenSetting(cfg, row.Key, row.Value); err != nil {
				return err
			}
		}
	}
	if err := applyLLMTokenSetting(cfg, key, value); err != nil {
		return err
	}
	return cfg.Validate()
}

func applyLLMTokenSetting(cfg *llm.Config, key, value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return errInvalidInt
	}
	switch key {
	case "AURA_LLM_MAX_TOKENS":
		cfg.MaxTokens = parsed
	case "AURA_MODEL_CONTEXT_WINDOW":
		cfg.ContextWindow = parsed
		cfg.ContextWindowConfigured = true
	case "AURA_MODEL_MAX_OUTPUT_TOKENS":
		cfg.MaxOutputTokens = parsed
		cfg.MaxOutputTokensConfigured = true
	}
	return nil
}
