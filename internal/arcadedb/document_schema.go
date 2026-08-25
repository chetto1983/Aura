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
	documentPassageType        = "Passage"
	maxDocumentIdentifierRunes = 512

	defaultRetrievalCandidateCap = 200
	defaultDocumentFilterCap     = 100
	defaultDocumentQueryRunes    = 2_048
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
}

func (cfg DocumentIndexConfig) normalized() (DocumentIndexConfig, error) {
	if cfg.Dimensions <= 0 {
		return DocumentIndexConfig{}, fmt.Errorf("arcadedb: document embedding dimensions must be positive")
	}
	cfg.MaxRetrievalCandidates = defaultLimit(cfg.MaxRetrievalCandidates, defaultRetrievalCandidateCap)
	cfg.MaxDocumentFilters = defaultLimit(cfg.MaxDocumentFilters, defaultDocumentFilterCap)
	cfg.MaxQueryRunes = defaultLimit(cfg.MaxQueryRunes, defaultDocumentQueryRunes)
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
