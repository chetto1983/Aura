package config

import (
	"strconv"
	"testing"
)

func TestAskUserConfig(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("AURA_ASKUSER_PAUSE_TTL_SEC", "")
		if got := loadAskUserConfig().PauseTTLSec; got != 172800 {
			t.Fatalf("PauseTTLSec = %d, want 172800", got)
		}
	})
	t.Run("explicit", func(t *testing.T) {
		t.Setenv("AURA_ASKUSER_PAUSE_TTL_SEC", "3600")
		if got := loadAskUserConfig().PauseTTLSec; got != 3600 {
			t.Fatalf("PauseTTLSec = %d, want 3600", got)
		}
	})
	t.Run("malformed falls back", func(t *testing.T) {
		t.Setenv("AURA_ASKUSER_PAUSE_TTL_SEC", "not-a-number")
		if got := loadAskUserConfig().PauseTTLSec; got != 172800 {
			t.Fatalf("PauseTTLSec = %d, want default 172800", got)
		}
	})
	for _, knob := range knobRegistry() {
		if knob.Name == "AURA_ASKUSER_PAUSE_TTL_SEC" {
			if knob.Kind != KindInt || knob.Default != strconv.Itoa(defaultAskUserPauseTTLSec) {
				t.Fatalf("TTL registry row = %#v", knob)
			}
			return
		}
	}
	t.Fatal("AURA_ASKUSER_PAUSE_TTL_SEC is absent from the knob registry")
}
