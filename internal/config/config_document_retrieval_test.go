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

func TestAssetProcessingLeaseHasItsOwnCurrentKnob(t *testing.T) {
	cfg := &Config{AssetProcessingLeaseDuration: 59 * time.Second}
	violations := cfg.gateAssetProcessingLease()
	if len(violations) != 1 || !strings.Contains(violations[0].Knob, "ASSET_PROCESSING") {
		t.Fatalf("violations = %#v", violations)
	}
}
