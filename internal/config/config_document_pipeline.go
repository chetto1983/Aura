package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/envutil"
)

// The capacity constants are not merely fallbacks: Validate reuses each one as the
// hard ceiling for its knob, so an AURA_DOCUMENT_* override can only narrow the
// envelope the pipeline was calibrated against, never widen it. The two scoring
// constants are ordinary defaults — their bounds are the unit interval and
// non-negativity, not these values.
const (
	DefaultDocumentMaxPassages       = 10_000
	DefaultDocumentMaxActivePassages = 100_000
	// DefaultDocumentEmbedBatchSize holds the PER-REQUEST token envelope constant while the
	// chunk budget grew. It was 32 against 512-token chunks (~16k tokens per request, the
	// shape the embed sidecar was calibrated against); chunks are now 2048
	// (defaultDocumentChunkTokens), so 8 keeps the same ~16k. Raising the chunk size without
	// lowering this would have quadrupled the request and re-run the HTTP 400
	// "exceeds the available context size" that dead-lettered the embed stage before.
	DefaultDocumentEmbedBatchSize = 8

	// maxDocumentEmbedBatchSize is the largest batch the embed sidecar has actually been
	// run against (32 × 512-token chunks). It bounds the knob; it is not the default.
	maxDocumentEmbedBatchSize          = 32
	DefaultDocumentPipelineAttempts    = 5
	DefaultDocumentPipelineLeaseSec    = 1_200
	DefaultDocumentRetrievalCandidates = 200
	DefaultDocumentDenseDistanceRatio  = 0.55
	DefaultDocumentLexicalMinScore     = 2.0
)

// DocumentPipelineConfig bounds candidate construction and production retrieval.
type DocumentPipelineConfig struct {
	MaxPassagesPerVersion int
	MaxActivePassages     int
	EmbedBatchSize        int
	MaxAttempts           int
	LeaseDuration         time.Duration
	RetrievalCandidates   int
	DenseMaxDistanceRatio float64
	LexicalMinScore       float64
}

func loadDocumentPipelineConfig() DocumentPipelineConfig {
	return DocumentPipelineConfig{
		MaxPassagesPerVersion: envutil.IntDefault(
			"AURA_DOCUMENT_MAX_PASSAGES_PER_VERSION", DefaultDocumentMaxPassages,
		),
		MaxActivePassages: envutil.IntDefault(
			"AURA_DOCUMENT_MAX_ACTIVE_PASSAGES", DefaultDocumentMaxActivePassages,
		),
		EmbedBatchSize: envutil.IntDefault(
			"AURA_DOCUMENT_EMBED_BATCH_SIZE", DefaultDocumentEmbedBatchSize,
		),
		MaxAttempts: envutil.IntDefault(
			"AURA_DOCUMENT_PIPELINE_MAX_ATTEMPTS", DefaultDocumentPipelineAttempts,
		),
		LeaseDuration: time.Duration(envutil.IntDefault(
			"AURA_DOCUMENT_PIPELINE_LEASE_SEC", DefaultDocumentPipelineLeaseSec,
		)) * time.Second,
		RetrievalCandidates: envutil.IntDefault(
			"AURA_DOCUMENT_RETRIEVAL_CANDIDATES", DefaultDocumentRetrievalCandidates,
		),
		DenseMaxDistanceRatio: floatEnvDefault(
			"AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO", DefaultDocumentDenseDistanceRatio,
		),
		LexicalMinScore: floatEnvDefault(
			"AURA_DOCUMENT_LEXICAL_MIN_SCORE", DefaultDocumentLexicalMinScore,
		),
	}
}

// Validate rejects a capacity or quality profile outside the calibrated envelope.
func (c DocumentPipelineConfig) Validate() error {
	if c == (DocumentPipelineConfig{}) {
		return nil
	}
	if c.MaxPassagesPerVersion <= 0 || c.MaxPassagesPerVersion > DefaultDocumentMaxPassages {
		return fmt.Errorf("AURA_DOCUMENT_MAX_PASSAGES_PER_VERSION must be between 1 and %d", DefaultDocumentMaxPassages)
	}
	if c.MaxActivePassages < c.MaxPassagesPerVersion || c.MaxActivePassages > DefaultDocumentMaxActivePassages {
		return fmt.Errorf("AURA_DOCUMENT_MAX_ACTIVE_PASSAGES must be between the per-version cap and %d", DefaultDocumentMaxActivePassages)
	}
	// Ceiling, not the default: using the default as its own maximum is what made the chunk
	// budget untunable and cost recall. 32 is the batch the sidecar was proven against at
	// the older 512-token chunk size, so it stays reachable for operators running smaller
	// chunks — it is simply no longer the default.
	if c.EmbedBatchSize <= 0 || c.EmbedBatchSize > maxDocumentEmbedBatchSize {
		return fmt.Errorf("AURA_DOCUMENT_EMBED_BATCH_SIZE must be between 1 and %d", maxDocumentEmbedBatchSize)
	}
	if c.MaxAttempts <= 0 || c.MaxAttempts > DefaultDocumentPipelineAttempts {
		return fmt.Errorf("AURA_DOCUMENT_PIPELINE_MAX_ATTEMPTS must be between 1 and %d", DefaultDocumentPipelineAttempts)
	}
	if c.LeaseDuration < time.Minute || c.LeaseDuration > DefaultDocumentPipelineLeaseSec*time.Second {
		return fmt.Errorf("AURA_DOCUMENT_PIPELINE_LEASE_SEC must be between 60 and %d", DefaultDocumentPipelineLeaseSec)
	}
	if c.RetrievalCandidates <= 0 || c.RetrievalCandidates > DefaultDocumentRetrievalCandidates {
		return fmt.Errorf("AURA_DOCUMENT_RETRIEVAL_CANDIDATES must be between 1 and %d", DefaultDocumentRetrievalCandidates)
	}
	if c.DenseMaxDistanceRatio <= 0 || c.DenseMaxDistanceRatio > 1 {
		return fmt.Errorf("AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO must be in (0,1]")
	}
	if c.LexicalMinScore < 0 {
		return fmt.Errorf("AURA_DOCUMENT_LEXICAL_MIN_SCORE must be non-negative")
	}
	return nil
}

func floatEnvDefault(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func (c *Config) gateDocumentPipeline(profile RuntimeProfile) []Violation {
	if c == nil || c.DocumentPipeline == (DocumentPipelineConfig{}) {
		return nil
	}
	if err := c.DocumentPipeline.Validate(); err != nil {
		return []Violation{{Knob: "AURA_DOCUMENT_*", Sev: Fatal, Msg: err.Error()}}
	}
	if profile.Strict() && (strings.TrimSpace(c.Embed.Revision) == "" ||
		!isLowerHexSHA256(c.Embed.Fingerprint)) {
		return []Violation{{
			Knob: "AURA_EMBED_REVISION/AURA_EMBED_FINGERPRINT",
			Sev:  Fatal,
			Msg:  "production document indexing requires the deployed embedding revision and artifact SHA-256",
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
