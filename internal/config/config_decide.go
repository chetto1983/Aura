package config

import (
	"os"
	"strings"
)

// DecideOff is the AURA_DECIDE_MODEL value that switches the decision model off, as opposed to an
// empty value, which is the local sidecar.
const DecideOff = "off"

// DecideConfig is the decision model's wiring. BaseURL is the local sidecar and has no default: the
// installer writes it only when it installs the sidecar. Model is read live from aura.settings by
// settings.DecideRoute; this snapshot is what the environment named at boot.
type DecideConfig struct {
	BaseURL string // AURA_DECIDE_BASE_URL
	Model   string // AURA_DECIDE_MODEL: "" is local, DecideOff is off, anything else is an OpenRouter id
}

// LoadDecide reads the decision model's wiring from the environment.
func LoadDecide() DecideConfig {
	return DecideConfig{
		BaseURL: strings.TrimSpace(os.Getenv("AURA_DECIDE_BASE_URL")),
		Model:   strings.TrimSpace(os.Getenv("AURA_DECIDE_MODEL")),
	}
}
