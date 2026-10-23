package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/envutil"
)

// Defaults for the document-retrieval and asset-processing knobs this file loads.
const (
	DefaultAssetProcessingLeaseSec     = 1_200
	DefaultDocumentRetrievalCandidates = 200
	DefaultDocumentFusionStrategy      = "RRF"
)

// DocumentRetrievalConfig bounds the one native fused ranking read from ArcadeDB.
type DocumentRetrievalConfig struct {
	RetrievalCandidates int
	FusionStrategy      string
	// DenseMaxDistance and MinRelevance override the document floors measured for the
	// embedding space (spec §9) when positive; zero keeps the measured ones.
	DenseMaxDistance float64
	MinRelevance     float64
}

func loadDocumentRetrievalConfig() DocumentRetrievalConfig {
	return DocumentRetrievalConfig{
		RetrievalCandidates: envutil.IntDefault(
			"AURA_DOCUMENT_RETRIEVAL_CANDIDATES", DefaultDocumentRetrievalCandidates,
		),
		FusionStrategy: envDefault(
			"AURA_DOCUMENT_FUSION_STRATEGY", DefaultDocumentFusionStrategy,
		),
		DenseMaxDistance: floorOverrideEnv("AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO"),
		MinRelevance:     floorOverrideEnv("AURA_DOCUMENT_MIN_RELEVANCE"),
	}
}

// floorOverrideEnv is key's value, zero when unset. A value that does not parse loads as NaN,
// which Validate refuses: a silent zero would serve the measured floors to an operator who
// believes theirs are in force.
func floorOverrideEnv(key string) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return math.NaN()
	}
	return value
}

// Validate rejects an out-of-range RetrievalCandidates; a zero-value config
// (unset) is left alone rather than defaulted here.
func (c DocumentRetrievalConfig) Validate() error {
	if c == (DocumentRetrievalConfig{}) {
		return nil
	}
	if c.RetrievalCandidates <= 0 || c.RetrievalCandidates > DefaultDocumentRetrievalCandidates {
		return fmt.Errorf(
			"AURA_DOCUMENT_RETRIEVAL_CANDIDATES must be between 1 and %d",
			DefaultDocumentRetrievalCandidates,
		)
	}
	switch c.FusionStrategy {
	case "RRF", "DBSF", "LINEAR":
	default:
		return fmt.Errorf("AURA_DOCUMENT_FUSION_STRATEGY must be RRF, DBSF or LINEAR")
	}
	for _, floor := range []struct {
		key        string
		value, max float64
	}{
		{"AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO", c.DenseMaxDistance, 2},
		{"AURA_DOCUMENT_MIN_RELEVANCE", c.MinRelevance, 1},
	} {
		if !(floor.value >= 0 && floor.value <= floor.max) {
			return fmt.Errorf("%s must be a number in (0, %g], or unset for the measured floors", floor.key, floor.max)
		}
	}
	return nil
}

func (c *Config) gateDocumentRetrieval(profile RuntimeProfile) []Violation {
	if c == nil || c.DocumentRetrieval == (DocumentRetrievalConfig{}) {
		return nil
	}
	if err := c.DocumentRetrieval.Validate(); err != nil {
		return []Violation{{Knob: "AURA_DOCUMENT_RETRIEVAL_*", Sev: Fatal, Msg: err.Error()}}
	}
	if profile.Strict() && (strings.TrimSpace(c.Embed.Revision) == "" ||
		!isLowerHexSHA256(c.Embed.Fingerprint)) {
		return []Violation{{
			Knob: "AURA_EMBED_REVISION/AURA_EMBED_FINGERPRINT", Sev: Fatal,
			Msg: "production document indexing requires the deployed embedding revision and artifact SHA-256",
		}}
	}
	return nil
}

func (c *Config) gateAssetProcessingLease() []Violation {
	if c == nil || c.AssetProcessingLeaseDuration == 0 {
		return nil
	}
	maximum := time.Duration(DefaultAssetProcessingLeaseSec) * time.Second
	if c.AssetProcessingLeaseDuration < time.Minute || c.AssetProcessingLeaseDuration > maximum {
		return []Violation{{
			Knob: "AURA_ASSET_PROCESSING_LEASE_SEC", Sev: Fatal,
			Msg: fmt.Sprintf(
				"AURA_ASSET_PROCESSING_LEASE_SEC must be between 60 and %d",
				DefaultAssetProcessingLeaseSec,
			),
		}}
	}
	return nil
}

func isLowerHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
