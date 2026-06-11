package telegram

import "testing"

func TestLoadConfigDefaults(t *testing.T) {
	// Clear every knob so the defaults are what we observe.
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("AURA_TELEGRAM_STATUS_THROTTLE_MS", "")
	t.Setenv("AURA_TELEGRAM_CONTENT_THROTTLE_MS", "")
	t.Setenv("AURA_TELEGRAM_CHAT_RATE_LIMIT_MS", "")
	t.Setenv("AURA_REASONING_FIFO_RUNES", "")

	cfg := LoadConfig()

	if cfg.BotToken != "" {
		t.Errorf("BotToken = %q, want empty default", cfg.BotToken)
	}
	if cfg.StatusThrottleMS != 1500 {
		t.Errorf("StatusThrottleMS = %d, want 1500", cfg.StatusThrottleMS)
	}
	if cfg.ContentThrottleMS != 500 {
		t.Errorf("ContentThrottleMS = %d, want 500", cfg.ContentThrottleMS)
	}
	if cfg.ChatRateLimitMS != 1000 {
		t.Errorf("ChatRateLimitMS = %d, want 1000", cfg.ChatRateLimitMS)
	}
	if cfg.ReasoningFIFORunes != 4096 {
		t.Errorf("ReasoningFIFORunes = %d, want 4096", cfg.ReasoningFIFORunes)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("AURA_TELEGRAM_STATUS_THROTTLE_MS", "2000")
	t.Setenv("AURA_TELEGRAM_CONTENT_THROTTLE_MS", "750")
	t.Setenv("AURA_TELEGRAM_CHAT_RATE_LIMIT_MS", "1200")
	t.Setenv("AURA_REASONING_FIFO_RUNES", "2048")

	cfg := LoadConfig()

	if cfg.BotToken != "123:abc" {
		t.Errorf("BotToken = %q, want 123:abc", cfg.BotToken)
	}
	if cfg.StatusThrottleMS != 2000 {
		t.Errorf("StatusThrottleMS = %d, want 2000", cfg.StatusThrottleMS)
	}
	if cfg.ContentThrottleMS != 750 {
		t.Errorf("ContentThrottleMS = %d, want 750", cfg.ContentThrottleMS)
	}
	if cfg.ChatRateLimitMS != 1200 {
		t.Errorf("ChatRateLimitMS = %d, want 1200", cfg.ChatRateLimitMS)
	}
	if cfg.ReasoningFIFORunes != 2048 {
		t.Errorf("ReasoningFIFORunes = %d, want 2048", cfg.ReasoningFIFORunes)
	}
}

func TestLoadConfigMalformedFallsBackSilently(t *testing.T) {
	t.Setenv("AURA_TELEGRAM_STATUS_THROTTLE_MS", "not-a-number")
	t.Setenv("AURA_TELEGRAM_CONTENT_THROTTLE_MS", "")
	t.Setenv("AURA_TELEGRAM_CHAT_RATE_LIMIT_MS", "12.5") // non-int
	t.Setenv("AURA_REASONING_FIFO_RUNES", "lots")

	cfg := LoadConfig()

	// Malformed → silent fallback to the documented defaults, never fatal.
	if cfg.StatusThrottleMS != 1500 {
		t.Errorf("StatusThrottleMS = %d, want 1500 (malformed → fallback)", cfg.StatusThrottleMS)
	}
	if cfg.ContentThrottleMS != 500 {
		t.Errorf("ContentThrottleMS = %d, want 500", cfg.ContentThrottleMS)
	}
	if cfg.ChatRateLimitMS != 1000 {
		t.Errorf("ChatRateLimitMS = %d, want 1000 (malformed → fallback)", cfg.ChatRateLimitMS)
	}
	if cfg.ReasoningFIFORunes != 4096 {
		t.Errorf("ReasoningFIFORunes = %d, want 4096 (malformed → fallback)", cfg.ReasoningFIFORunes)
	}
}
