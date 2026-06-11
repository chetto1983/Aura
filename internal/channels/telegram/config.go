package telegram

import (
	"os"
	"strconv"
)

// Config is the Telegram channel's own config surface (Phase 13 / Slice 9a,
// UX-02). Per the config.go package doc convention, per-subsystem configs live
// in their owning packages; this is the Telegram one. The central
// internal/config.Config owns the setup/vision/sidecar knobs (those cross
// subsystems); the bot token + the channel's own throttles live here.
//
// Env naming (CLAUDE.md): TELEGRAM_BOT_TOKEN keeps upstream/third-party naming
// (it is a Telegram-issued credential, NOT an Aura knob); the throttles are
// Aura-native and use the AURA_<DOMAIN>_<UNIT> convention. All values use the
// silent-fallback contract (malformed → default, never boot-fatal).
type Config struct {
	// BotToken is the Telegram Bot API token (TELEGRAM_BOT_TOKEN, upstream
	// naming). Empty default → the channel is unconfigured; the setup wizard
	// (plan 13-07) can supply it via POST /setup/token, and the registry's
	// enable gate decides whether the channel starts at all.
	BotToken string

	// StatusThrottleMS bounds status-pane (msg #1) edits — PRD 1500ms default.
	StatusThrottleMS int
	// ContentThrottleMS bounds streamed-content (msg #2) edits — 500ms default.
	ContentThrottleMS int
	// ChatRateLimitMS bounds the per-chat_id send queue — 1000ms default.
	ChatRateLimitMS int

	// ReasoningFIFORunes caps the live CoT window (AURA_REASONING_FIFO_RUNES, default
	// 4096). The on/off master switch is AURA_SHOW_REASONING in llm.Config (propagated
	// to the channel by the composition root) — this is only the window size.
	ReasoningFIFORunes int
}

// LoadConfig reads the Telegram channel config from the environment with
// silent-fallback defaults (mirrors config.envIntDefault: unset/empty/malformed
// → fallback, never fatal — a typo in a throttle tweak must not block boot).
func LoadConfig() Config {
	return Config{
		BotToken:           os.Getenv("TELEGRAM_BOT_TOKEN"),
		StatusThrottleMS:   envIntDefault("AURA_TELEGRAM_STATUS_THROTTLE_MS", 1500),
		ContentThrottleMS:  envIntDefault("AURA_TELEGRAM_CONTENT_THROTTLE_MS", 500),
		ChatRateLimitMS:    envIntDefault("AURA_TELEGRAM_CHAT_RATE_LIMIT_MS", 1000),
		ReasoningFIFORunes: envIntDefault("AURA_REASONING_FIFO_RUNES", 4096),
	}
}

// envIntDefault returns the int value of key, falling back to fallback when the
// variable is unset, empty, or fails to parse. Parse failures are silently
// absorbed — the fallback beats a fatal boot on a misformatted ad-hoc tweak
// (the config.envIntDefault contract, local copy to keep the telegram package
// free of an internal/config import).
func envIntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
