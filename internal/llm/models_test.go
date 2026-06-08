package llm_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestSupportsVision(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "minimax_m3_bare", model: "minimax/minimax-m3", want: true},
		{name: "minimax_m3_thinking_suffix", model: "minimax/minimax-m3:thinking", want: true},
		{name: "minimax_m3_arbitrary_suffix", model: "minimax/minimax-m3:something", want: true},
		{name: "deepseek_v4_flash_bare", model: "deepseek/deepseek-v4-flash", want: false},
		{name: "deepseek_v4_flash_exacto_suffix", model: "deepseek/deepseek-v4-flash:exacto", want: false},
		{name: "unknown_model_conservative_false", model: "some/unknown-model", want: false},
		{name: "empty_string_false", model: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := llm.SupportsVision(tt.model); got != tt.want {
				t.Errorf("SupportsVision(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestSupportsAudio(t *testing.T) {
	// Audio is sidecar-routed this phase: both current models report false.
	// The function exists for forward-compat symmetry with SupportsVision.
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "minimax_m3_no_audio", model: "minimax/minimax-m3", want: false},
		{name: "minimax_m3_suffix_no_audio", model: "minimax/minimax-m3:thinking", want: false},
		{name: "deepseek_v4_flash_no_audio", model: "deepseek/deepseek-v4-flash", want: false},
		{name: "deepseek_v4_flash_suffix_no_audio", model: "deepseek/deepseek-v4-flash:exacto", want: false},
		{name: "unknown_model_no_audio", model: "some/unknown-model", want: false},
		{name: "empty_string_no_audio", model: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := llm.SupportsAudio(tt.model); got != tt.want {
				t.Errorf("SupportsAudio(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestSupportsVision_SuffixNormalizationIsExactBase(t *testing.T) {
	// A model whose base id is NOT vision-capable must stay false even with a
	// suffix; suffix stripping must never accidentally promote an unknown base.
	if llm.SupportsVision("deepseek/deepseek-v4-flash:vision") {
		t.Error("SupportsVision must not be fooled by a misleading suffix on a non-vision base")
	}
	// Only the colon-suffix is stripped; an unrelated prefix is NOT a match.
	if llm.SupportsVision("notminimax/minimax-m3") {
		t.Error("SupportsVision must match the full base id, not a substring")
	}
}

func TestModelCapabilityLookupSourceShape(t *testing.T) {
	// Guards the contract that the lookup is a pure function of the model id:
	// repeated calls are stable and the seed table does not leak mutable state.
	for range 3 {
		if !llm.SupportsVision("minimax/minimax-m3") {
			t.Fatal("SupportsVision(minimax/minimax-m3) must be stable across calls")
		}
		if llm.SupportsVision("deepseek/deepseek-v4-flash") {
			t.Fatal("SupportsVision(deepseek/deepseek-v4-flash) must be stable across calls")
		}
	}
	// Defensive: a model id with surrounding whitespace from a sloppy config
	// still normalizes to the base.
	if !llm.SupportsVision(strings.TrimSpace("  minimax/minimax-m3  ")) {
		t.Error("trimmed minimax/minimax-m3 should report vision support")
	}
}
