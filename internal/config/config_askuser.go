package config

import "github.com/chetto1983/aura/internal/envutil"

const defaultAskUserPauseTTLSec = 48 * 60 * 60

// AskUserConfig bounds unanswered approval pauses. Forty-eight hours leaves room for a
// complete working-day absence without turning a boundary-time decision into an expiry;
// it is intentionally unrelated to the in-flight MCP elicitation timeout.
type AskUserConfig struct {
	PauseTTLSec int // AURA_ASKUSER_PAUSE_TTL_SEC; <=0 explicitly disables expiry
}

func loadAskUserConfig() AskUserConfig {
	return AskUserConfig{
		PauseTTLSec: envutil.IntDefault("AURA_ASKUSER_PAUSE_TTL_SEC", defaultAskUserPauseTTLSec),
	}
}
