package config

import "testing"

func TestDocumentPipelineConfigDefaultsAndValidation(t *testing.T) {
	t.Setenv("AURA_DOCUMENT_MAX_PASSAGES_PER_VERSION", "")
	t.Setenv("AURA_DOCUMENT_FUSION_STRATEGY", "")
	got := loadDocumentPipelineConfig()
	if got.MaxPassagesPerVersion != DefaultDocumentMaxPassages ||
		got.MaxActivePassages != DefaultDocumentMaxActivePassages ||
		got.FusionStrategy != DefaultDocumentFusionStrategy {
		t.Fatalf("defaults = %#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentPipelineConfigRejectsUncalibratedCapacity(t *testing.T) {
	config := loadDocumentPipelineConfig()
	config.MaxPassagesPerVersion = DefaultDocumentMaxPassages + 1
	if err := config.Validate(); err == nil {
		t.Fatal("uncalibrated passage capacity accepted")
	}
	config = loadDocumentPipelineConfig()
	// A strategy the engine does not implement must be refused at boot: vector.fuse would
	// reject it per query, so a typo would fail every retrieval at runtime instead.
	config.FusionStrategy = "COSINE"
	if err := config.Validate(); err == nil {
		t.Fatal("unknown fusion strategy accepted")
	}
}
