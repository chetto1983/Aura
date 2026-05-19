package tools

import (
	"os"
	"strings"
)

func op12PrecallValidatorEnabled() bool {
	return envFlagEnabled("AURA_OP12_PRECALL_VALIDATOR_ENABLED")
}

func op12RetryHintEnabled() bool {
	return envFlagEnabled("AURA_OP12B_RETRY_HINT_ENABLED")
}

func envFlagEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on", "enabled", "enable":
		return true
	default:
		return false
	}
}
