package llm

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestChatGPTReasoningCapabilitiesUseIndependentRouteSnapshot(t *testing.T) {
	cfg := Config{Provider: ChatGPTProvider, BaseURL: ChatGPTBaseURL, ReasoningMandatory: true,
		SupportedReasoningEfforts: []ReasoningEffort{ReasoningEffortLow, ReasoningEffortHigh, ReasoningEffortMax}}
	source := NewReasoningCapabilitySource(cfg, time.Hour)
	if source == nil {
		t.Fatal("ChatGPT capability source missing")
	}
	cfg.SupportedReasoningEfforts[0] = ReasoningEffortNone
	got, _, detected := source.AllowedEfforts(context.Background())
	want := []ReasoningEffort{ReasoningEffortLow, ReasoningEffortHigh, ReasoningEffortMax}
	if !detected || !slices.Equal(got, want) {
		t.Fatalf("capabilities %v, detected %v", got, detected)
	}
	got[0] = ReasoningEffortNone
	again, _, _ := source.AllowedEfforts(context.Background())
	if !slices.Equal(again, want) {
		t.Fatal("caller mutated shared capabilities")
	}
}

func TestChatGPTReasoningCapabilitiesMandatoryAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mandatory     bool
		efforts, want []ReasoningEffort
		detected      bool
	}{
		{"mandatory", true, []ReasoningEffort{ReasoningEffortNone, ReasoningEffortHigh}, []ReasoningEffort{ReasoningEffortHigh}, true},
		{"optional", false, []ReasoningEffort{ReasoningEffortNone, ReasoningEffortLow}, []ReasoningEffort{ReasoningEffortNone, ReasoningEffortLow}, true},
		{"no metadata", false, nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Provider: ChatGPTProvider, BaseURL: ChatGPTBaseURL, SupportedReasoningEfforts: tc.efforts, ReasoningMandatory: tc.mandatory}
			got, _, detected := NewReasoningCapabilitySource(cfg, time.Hour).AllowedEfforts(context.Background())
			if !slices.Equal(got, tc.want) || detected != tc.detected {
				t.Fatalf("capabilities %v, detected %v", got, detected)
			}
		})
	}
}
