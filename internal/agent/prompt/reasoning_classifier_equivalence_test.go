package prompt

import (
	"context"
	"testing"
)

// TestReasoningClassifier_EquivalenceGoldenVerdicts pins the migrated
// (semindex.Classifier-backed) classifier's tier verdicts to a golden
// (input → ReasoningTier) table over the curated seeds, proving the structural
// migration changed no observable classification behavior. It uses the
// synthetic one-hot vecFor/fakeEmbedder doubles (deterministic, no sidecar,
// free in CI) so the per-tier centroids land on orthogonal axes exactly as the
// pre-migration anchor build did. A drift in the centroid/cosine/margin math
// (e.g. a regression in the shared semindex core) flips a verdict and fails
// here. This is the behavior-preservation proof, NOT an accuracy gate — the
// live accuracy raise is Plan 04.
func TestReasoningClassifier_EquivalenceGoldenVerdicts(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{})

	golden := []struct {
		name  string
		input string
		want  ReasoningTier
	}{
		// none — stable facts / chit-chat / short transforms (no low/high keyword)
		{"none/capital-fact", "qual e la capitale dell'Italia", ReasoningTierNone},
		{"none/identity", "dimmi una cosa qualunque su di te", ReasoningTierNone},
		{"none/short-ack", "va tutto bene allora", ReasoningTierNone},
		// low — current-info lookups / small tool use
		{"low/weather", "che meteo fa a Roma domani", ReasoningTierLow},
		{"low/news", "cerca le notizie di oggi", ReasoningTierLow},
		{"low/price", "quanto costa il bitcoin", ReasoningTierLow},
		// high — code / debug / design / proofs / scraping
		{"high/debug-script", "debugga il mio script python", ReasoningTierHigh},
		{"high/db-schema", "progetta lo schema di un database", ReasoningTierHigh},
		{"high/scraping", "scrivi uno script di scraping con gestione errori", ReasoningTierHigh},
	}

	for _, tc := range golden {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := c.Classify(context.Background(), tc.input)
			if !ok {
				t.Fatalf("Classify(%q) returned not-usable; want %q,true", tc.input, tc.want)
			}
			if got != tc.want {
				t.Errorf("Classify(%q) = %q; want %q (verdict drift — semindex centroid math regressed)", tc.input, got, tc.want)
			}
		})
	}
}
