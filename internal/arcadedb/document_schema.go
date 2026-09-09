package arcadedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	documentPassageType = "Passage"
	// documentEmbeddingProperty is the vector property `vector.rerank` re-scores against.
	documentEmbeddingProperty  = "embedding"
	maxDocumentIdentifierRunes = 512

	defaultRetrievalCandidateCap = 200
	defaultDocumentFilterCap     = 100
	defaultDocumentQueryRunes    = 2_048
	// See DenseMaxDistance for the measurement this number comes from.
	defaultDocumentDenseMaxDistance = 0.72
	// See RelevanceFloor for the measurement this number comes from.
	defaultDocumentRelevanceFloor = 0.32
)

// CharacterSpan locates a passage in the extracted text.
type CharacterSpan struct {
	Start int64
	End   int64
}

// DocumentIndexConfig fixes the physical vector schema and bounds every read and write.
type DocumentIndexConfig struct {
	Dimensions             int
	MaxRetrievalCandidates int
	MaxDocumentFilters     int
	MaxQueryRunes          int
	// DenseMaxDistance bounds the dense leg so retrieval can abstain at all: vector.neighbors
	// returns its k nearest however far away they are, so with no bound every query — including
	// one the corpus cannot answer — comes back with candidates.
	//
	// 0.72 is the midpoint of a measured band, not a guess. On the live 1079-passage corpus
	// (2026-09-09, EmbeddingGemma 768d) the nearest neighbour for five answerable questions sat
	// at 0.247, 0.400, 0.420, 0.651 and 0.712; for five the corpus does not hold ("ricetta della
	// carbonara", "chi ha vinto il mondiale 1982", potatura, traghetti, vitamina B12) at 0.706,
	// 0.731, 0.794, 0.798 and 0.799. It keeps all five true matches and drops four of the five
	// others. The margin is thin — 0.7124 against 0.7063 — so this is a knob with its
	// measurement written beside it, not a clean separation: "orari dei traghetti per la
	// Sardegna" is still admitted, which is what RelevanceFloor below finally rejects.
	DenseMaxDistance float64

	// RelevanceFloor is the abstention gate, and it sits AFTER the rerank on purpose.
	// DenseMaxDistance bounds the dense leg only; a lexical hit on an incidental word still
	// entered the result set with nothing left to reject it, because `vector.fuse` scores by
	// reciprocal rank -- 1/(60+rank), identical for every source's rank 1 -- and a rank
	// carries no relevance. Measured 2026-09-09 on the live 1079-passage corpus: all five
	// out-of-corpus questions scored exactly 0.016393442, to the digit. No threshold on that
	// number can separate anything. `vector.rerank` re-scores the fused candidates against
	// their full-precision vectors and emits a real cosine, which can be thresholded.
	//
	// 0.32 is the midpoint of 0.2990..0.3445, measured the same day on the same corpus.
	// Worst true match: "w2-ee2226e3", a bare worker identifier, at 0.3445. Best false
	// match: "orari dei traghetti per la Sardegna" at 0.2990 -- the one DenseMaxDistance
	// admitted. The rest sat far from the edge: answerable 0.4508..0.7530, out-of-corpus
	// 0.1737..0.2171 ("ricetta della carbonara" 0.1803).
	//
	// The identifier group is why the floor is 0.32 and not the 0.3749 the prose questions
	// alone suggested: an exact identifier is a short query against long prose, so its cosine
	// runs low even when the match is exact and correct. Memory solves the same tension with
	// a query-shape exemption (lexicalScoreFloor returns 0 for a one-token query); here the
	// measured band does not need one, and a floor chosen without those five queries would
	// have rejected a correct lookup.
	//
	// What the measurement does NOT support: a claim beyond 15 queries, one corpus, one
	// embedder, one day, and top-1 only. The band is 0.0455 wide. That memory measured 0.28
	// on a different corpus is a second, independent measurement, not a shared constant.
	RelevanceFloor float64
}

func (cfg DocumentIndexConfig) normalized() (DocumentIndexConfig, error) {
	if cfg.Dimensions <= 0 {
		return DocumentIndexConfig{}, fmt.Errorf("arcadedb: document embedding dimensions must be positive")
	}
	cfg.MaxRetrievalCandidates = defaultLimit(cfg.MaxRetrievalCandidates, defaultRetrievalCandidateCap)
	cfg.MaxDocumentFilters = defaultLimit(cfg.MaxDocumentFilters, defaultDocumentFilterCap)
	cfg.MaxQueryRunes = defaultLimit(cfg.MaxQueryRunes, defaultDocumentQueryRunes)
	cfg.DenseMaxDistance = defaultFloatLimit(cfg.DenseMaxDistance, defaultDocumentDenseMaxDistance)
	cfg.RelevanceFloor = defaultFloatLimit(cfg.RelevanceFloor, defaultDocumentRelevanceFloor)
	limits := []struct {
		name  string
		value int
	}{
		{"retrieval candidates", cfg.MaxRetrievalCandidates},
		{"document filters", cfg.MaxDocumentFilters},
		{"query runes", cfg.MaxQueryRunes},
	}
	for _, limit := range limits {
		if limit.value <= 0 {
			return DocumentIndexConfig{}, fmt.Errorf("arcadedb: document %s limit must be positive", limit.name)
		}
	}
	return cfg, nil
}

// TenantClientResolver resolves one identity to its physically isolated database.
type TenantClientResolver interface {
	For(context.Context, string) (*Client, error)
}

// DocumentIndex reads the CocoIndex-owned document records for every tenant.
type DocumentIndex struct {
	tenants TenantClientResolver
	config  DocumentIndexConfig
}

// NewDocumentIndex validates its immutable schema and capacity contract without doing I/O.
func NewDocumentIndex(tenants TenantClientResolver, cfg DocumentIndexConfig) (*DocumentIndex, error) {
	if tenants == nil {
		return nil, fmt.Errorf("arcadedb: document tenant resolver is not configured")
	}
	normalized, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	return &DocumentIndex{tenants: tenants, config: normalized}, nil
}

func (d *DocumentIndex) schemaVersion() string {
	return "document-v1:standard-analyzer:cosine:none:" + strconv.Itoa(d.config.Dimensions)
}

func (d *DocumentIndex) tenantClient(ctx context.Context, identityID string) (*Client, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return nil, fmt.Errorf("arcadedb: document identity must be non-empty")
	}
	client, err := d.tenants.For(ctx, identityID)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: document tenant %s: %w", identityID, err)
	}
	if client == nil {
		return nil, fmt.Errorf("arcadedb: document tenant %s returned a nil client", identityID)
	}
	return client, nil
}

func requiredString(row map[string]any, key string) (string, error) {
	value, ok := row[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is missing or not a non-empty string", key)
	}
	return value, nil
}

func exactInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return 0, false
		}
		return int64(typed), float64(int64(typed)) == typed
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	default:
		return 0, false
	}
}

func validateIdentifier(name, value string) error {
	if value == "" {
		return fmt.Errorf("arcadedb: %s must be non-empty", name)
	}
	if utf8.RuneCountInString(value) > maxDocumentIdentifierRunes {
		return fmt.Errorf("arcadedb: %s exceeds %d characters", name, maxDocumentIdentifierRunes)
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
