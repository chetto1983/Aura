package config

import "testing"

func TestDocumentPipelineConfigDefaultsAndValidation(t *testing.T) {
	t.Setenv("AURA_DOCUMENT_MAX_PASSAGES_PER_VERSION", "")
	t.Setenv("AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO", "")
	got := loadDocumentPipelineConfig()
	if got.MaxPassagesPerVersion != DefaultDocumentMaxPassages ||
		got.MaxActivePassages != DefaultDocumentMaxActivePassages ||
		got.DenseMaxDistanceRatio != DefaultDocumentDenseDistanceRatio {
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
	config.DenseMaxDistanceRatio = 1.1
	if err := config.Validate(); err == nil {
		t.Fatal("invalid dense threshold accepted")
	}
}

func TestDocumentPipelineFloatKnobsFallbackAndOverride(t *testing.T) {
	t.Setenv("AURA_DOCUMENT_LEXICAL_MIN_SCORE", "3.5")
	if got := loadDocumentPipelineConfig().LexicalMinScore; got != 3.5 {
		t.Fatalf("lexical min = %v", got)
	}
	t.Setenv("AURA_DOCUMENT_LEXICAL_MIN_SCORE", "invalid")
	if got := loadDocumentPipelineConfig().LexicalMinScore; got != DefaultDocumentLexicalMinScore {
		t.Fatalf("fallback lexical min = %v", got)
	}
}
