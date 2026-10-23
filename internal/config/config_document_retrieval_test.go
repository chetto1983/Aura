package config

import (
	"strings"
	"testing"
	"time"
)

func TestDocumentRetrievalConfigDefaultsAndValidation(t *testing.T) {
	t.Setenv("AURA_DOCUMENT_RETRIEVAL_CANDIDATES", "")
	t.Setenv("AURA_DOCUMENT_FUSION_STRATEGY", "")
	got := loadDocumentRetrievalConfig()
	if got.RetrievalCandidates != DefaultDocumentRetrievalCandidates ||
		got.FusionStrategy != DefaultDocumentFusionStrategy {
		t.Fatalf("defaults = %#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentRetrievalConfigRejectsUnknownEngineStrategy(t *testing.T) {
	config := loadDocumentRetrievalConfig()
	config.FusionStrategy = "COSINE"
	if err := config.Validate(); err == nil {
		t.Fatal("unknown fusion strategy accepted")
	}
}

// The overrides are how a new space's document floors are measured (spec §9). One outside its
// range, or not a number, refuses the boot: ignoring it would serve the measured floors to an
// operator who believes theirs are in force.
func TestDocumentFloorOverridesLoadAndValidate(t *testing.T) {
	const ratio, relevance = "AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO", "AURA_DOCUMENT_MIN_RELEVANCE"
	load := func(t *testing.T, distance, minimum string) *Config {
		t.Helper()
		t.Setenv(ratio, distance)
		t.Setenv(relevance, minimum)
		return &Config{DocumentRetrieval: loadDocumentRetrievalConfig()}
	}
	cfg := load(t, "", "")
	if got := cfg.DocumentRetrieval; got.DenseMaxDistance != 0 || got.MinRelevance != 0 {
		t.Fatalf("unset overrides = %v / %v, want 0 / 0", got.DenseMaxDistance, got.MinRelevance)
	}
	if violations := cfg.gateDocumentRetrieval(ProfileDev); len(violations) != 0 {
		t.Fatalf("unset overrides: violations = %#v", violations)
	}
	cfg = load(t, "0.9", " 0.4 ")
	if got := cfg.DocumentRetrieval; got.DenseMaxDistance != 0.9 || got.MinRelevance != 0.4 {
		t.Fatalf("overrides = %v / %v, want 0.9 / 0.4", got.DenseMaxDistance, got.MinRelevance)
	}
	if violations := cfg.gateDocumentRetrieval(ProfileDev); len(violations) != 0 {
		t.Fatalf("valid overrides: violations = %#v", violations)
	}
	for name, test := range map[string]struct{ distance, minimum, knob string }{
		"negative ratio":       {"-1", "", ratio},
		"ratio above 2":        {"2.5", "", ratio},
		"relevance above 1":    {"", "1.5", relevance},
		"relevance not number": {"", "abc", relevance},
		"ratio NaN":            {"NaN", "", ratio},
	} {
		t.Run(name, func(t *testing.T) {
			violations := load(t, test.distance, test.minimum).gateDocumentRetrieval(ProfileDev)
			if len(violations) != 1 || violations[0].Sev != Fatal || !strings.Contains(violations[0].Msg, test.knob) {
				t.Fatalf("violations = %#v, want one Fatal naming %s", violations, test.knob)
			}
		})
	}
}

func TestAssetProcessingLeaseHasItsOwnCurrentKnob(t *testing.T) {
	cfg := &Config{AssetProcessingLeaseDuration: 59 * time.Second}
	violations := cfg.gateAssetProcessingLease()
	if len(violations) != 1 || !strings.Contains(violations[0].Knob, "ASSET_PROCESSING") {
		t.Fatalf("violations = %#v", violations)
	}
}
