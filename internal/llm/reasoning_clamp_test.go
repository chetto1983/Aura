package llm

import "testing"

// The live failure this exists to stop: z-ai/glm-5.3-flash advertises max/high/low and
// refuses "none" outright, so the classifier's cheapest tier killed the cheapest turns.
func TestClampReasoningEffortSubstitutesTheNearestAcceptedGear(t *testing.T) {
	glm := Config{SupportedReasoningEfforts: []ReasoningEffort{
		ReasoningEffortMax, ReasoningEffortHigh, ReasoningEffortLow,
	}}
	gemini := Config{SupportedReasoningEfforts: []ReasoningEffort{
		ReasoningEffortHigh, ReasoningEffortMedium, ReasoningEffortLow,
	}}

	for _, tc := range []struct {
		name       string
		cfg        Config
		want, gets ReasoningEffort
	}{
		{"none becomes the lowest glm accepts", glm, ReasoningEffortNone, ReasoningEffortLow},
		{"none becomes the lowest gemini accepts", gemini, ReasoningEffortNone, ReasoningEffortLow},
		{"a supported effort is untouched", glm, ReasoningEffortHigh, ReasoningEffortHigh},
		{"medium falls to low, not up to high", glm, ReasoningEffortMedium, ReasoningEffortLow},
		// Equidistant from max and high: the tie resolves downward, so a substitution
		// never buys more reasoning than the classifier asked for.
		{"xhigh resolves down to high on a tie", glm, ReasoningEffortXHigh, ReasoningEffortHigh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.ClampReasoningEffort(tc.want); got != tc.gets {
				t.Errorf("clamp(%q) = %q, want %q", tc.want, got, tc.gets)
			}
		})
	}
}

// A model that published no set must be left exactly alone. Substituting an effort for a
// model that told us nothing would trade a loud 400 for a silent behaviour change, which
// is the worse of the two.
func TestClampReasoningEffortLeavesAnUndeclaredModelAlone(t *testing.T) {
	var undeclared Config
	for _, want := range []ReasoningEffort{ReasoningEffortNone, ReasoningEffortMax, ""} {
		if got := undeclared.ClampReasoningEffort(want); got != want {
			t.Errorf("clamp(%q) = %q on a model that declared nothing", want, got)
		}
	}
}

// The allowlist is the security boundary on catalogue input: a token Aura does not model
// must be dropped, never carried through to the wire.
func TestClampAdvertisedEffortsDropsUnknownTokens(t *testing.T) {
	got := clampAdvertisedEfforts([]string{"high", "ludicrous", "low", "high", ""})
	if len(got) != 2 || got[0] != ReasoningEffortHigh || got[1] != ReasoningEffortLow {
		t.Fatalf("efforts = %v, want [high low] with the unknown and the duplicate dropped", got)
	}
}
