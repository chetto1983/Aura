package conversations

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestValidateCompactionTrigger(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		contextWindow  int
		triggerPercent int
		fixedOverhead  int
		wantErr        bool
	}{
		{
			name:           "disabled",
			enabled:        false,
			contextWindow:  100000,
			triggerPercent: 50,
			fixedOverhead:  5000,
			wantErr:        false,
		},
		{
			name:           "trigger at 0",
			enabled:        true,
			contextWindow:  100000,
			triggerPercent: 0,
			fixedOverhead:  5000,
			wantErr:        false,
		},
		{
			name:           "trigger at 100",
			enabled:        true,
			contextWindow:  100000,
			triggerPercent: 100,
			fixedOverhead:  5000,
			wantErr:        false,
		},
		{
			name:           "valid configuration",
			enabled:        true,
			contextWindow:  100000,
			triggerPercent: 50,
			fixedOverhead:  5000,
			wantErr:        false,
		},
		{
			name:           "zero context window",
			enabled:        true,
			contextWindow:  0,
			triggerPercent: 50,
			fixedOverhead:  5000,
			wantErr:        true,
		},
		{
			name:           "fixed overhead exceeds trigger budget",
			enabled:        true,
			contextWindow:  1000,
			triggerPercent: 50,   // triggerBudget = 500
			fixedOverhead:  1000, // exceeds triggerBudget
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCompactionTrigger(tt.enabled, tt.contextWindow, tt.triggerPercent, tt.fixedOverhead)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCompactionTrigger() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContextConfig_EarlyCompactionTokens(t *testing.T) {
	tests := []struct {
		name   string
		config ContextConfig
		want   int
	}{
		{
			name: "no summarizer",
			config: ContextConfig{
				ContextWindow:            100000,
				Summarizer:               nil,
				FixedOverheadTokens:      1000,
				CompactionTriggerPercent: 50,
			},
			want: 0,
		},
		{
			name: "zero context window",
			config: ContextConfig{
				ContextWindow:            0,
				Summarizer:               &mockSummarizer{},
				FixedOverheadTokens:      1000,
				CompactionTriggerPercent: 50,
			},
			want: 0,
		},
		{
			name: "trigger at 0",
			config: ContextConfig{
				ContextWindow:            100000,
				Summarizer:               &mockSummarizer{},
				FixedOverheadTokens:      1000,
				CompactionTriggerPercent: 0,
			},
			want: 0,
		},
		{
			name: "trigger at 100",
			config: ContextConfig{
				ContextWindow:            100000,
				Summarizer:               &mockSummarizer{},
				FixedOverheadTokens:      1000,
				CompactionTriggerPercent: 100,
			},
			want: 0,
		},
		{
			name: "fixed overhead exceeds budget",
			config: ContextConfig{
				ContextWindow:            1000,
				Summarizer:               &mockSummarizer{},
				FixedOverheadTokens:      10000, // more than any trigger budget
				CompactionTriggerPercent: 50,
			},
			want: 0,
		},
		{
			name: "valid configuration",
			config: ContextConfig{
				ContextWindow:            100000,
				Summarizer:               &mockSummarizer{},
				FixedOverheadTokens:      1000,
				CompactionTriggerPercent: 50,
			},
			// triggerBudget = 100000 * 50 / 100 = 50000
			// after fixed overhead: 50000 - 1000 = 49000
			want: 49000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.earlyCompactionTokens()
			if got != tt.want {
				t.Errorf("earlyCompactionTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestContextConfig_HardCap(t *testing.T) {
	// Test HardCap method (public wrapper around hardCap)
	config := ContextConfig{
		ContextWindow:              100000,
		MaxOutputTokens:            8192,
		CompactionTriggerPercent:   50,
		FixedOverheadTokens:        1000,
		ProviderErrorReserveTokens: 0,
	}
	got := config.HardCap()
	// hardCap = ContextWindow - max(MaxOutputTokens, 20000) - 13000 - ProviderErrorReserveTokens
	// = 100000 - max(8192, 20000) - 13000 - 0
	// = 100000 - 20000 - 13000 = 67000
	want := 67000
	if got != want {
		t.Errorf("HardCap() = %d, want %d", got, want)
	}
}

// mockSummarizer implements Summarizer for testing
type mockSummarizer struct{}

func (m *mockSummarizer) Summarize(ctx context.Context, rounds []llm.Message) (string, error) {
	return "", nil
}
