package arcadedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// DocumentProjectionSchemaVersion is folded into the pipeline generation fingerprint,
	// so bumping it re-projects every document in every tenant. Change it only when the
	// stored passage shape stops being readable by this package.
	DocumentProjectionSchemaVersion = "arcadedb-document-projection-v1"

	documentProjectionType       = "DocumentProjection"
	documentPassageType          = "Passage"
	documentPassageEdge          = "HAS_PASSAGE"
	maxProjectionIdentifierRunes = 512
	maxProjectedPassageRunes     = 262_144

	defaultCandidatePassageLimit = 10_000
	defaultActivePassageLimit    = 100_000
	defaultRetrievalCandidateCap = 200
	defaultDocumentFilterCap     = 100
	defaultDocumentQueryRunes    = 2_048
)

// BoundingBox preserves Docling's coordinates without inventing a coordinate origin.
type BoundingBox struct {
	Left   float64
	Top    float64
	Right  float64
	Bottom float64
}

// CharacterSpan locates a passage in its converted structural item.
type CharacterSpan struct {
	Start int64
	End   int64
}

// ProjectedPassage is one immutable passage in one pipeline generation.
type ProjectedPassage struct {
	PassageID        string
	Ordinal          int64
	Text             string
	NormalizedSHA256 string
	SelfRef          string
	HeadingPath      []string
	Captions         []string
	PageNumber       *int64
	BoundingBox      *BoundingBox
	CharacterSpan    *CharacterSpan
	SheetName        string
	TableName        string
	RowNumber        *int64
	ColumnNumber     *int64
	CellReference    string
	Embedding        []float64
}

// ProjectionGeneration carries the immutable fields shared by all of its passages.
type ProjectionGeneration struct {
	IdentityID         string
	DocumentID         string
	SearchDocumentID   string
	VersionID          string
	VersionNumber      int64
	RawSHA256          string
	PipelineGeneration string
	Passages           []ProjectedPassage
}

// ProjectionResult distinguishes a new projection from an exact idempotent replay.
type ProjectionResult struct {
	ProjectionKey string
	Fingerprint   string
	PassageCount  int
	Created       bool
	Active        bool
}

// TombstoneResult reports whether the selected generation existed and changed state.
type TombstoneResult struct {
	ProjectionKey     string
	PassageCount      int
	Found             bool
	AlreadyTombstoned bool
}

// DeleteProjectionResult reports the exact immutable generation removed from ArcadeDB.
type DeleteProjectionResult struct {
	ProjectionKey string
	PassageCount  int
	Found         bool
}

// DocumentIndexConfig fixes the physical vector schema and bounds every read and write.
type DocumentIndexConfig struct {
	Dimensions             int
	MaxCandidatePassages   int
	MaxActivePassages      int
	MaxRetrievalCandidates int
	MaxDocumentFilters     int
	MaxQueryRunes          int
}

func (cfg DocumentIndexConfig) normalized() (DocumentIndexConfig, error) {
	if cfg.Dimensions <= 0 {
		return DocumentIndexConfig{}, fmt.Errorf("arcadedb: document embedding dimensions must be positive")
	}
	cfg.MaxCandidatePassages = defaultLimit(cfg.MaxCandidatePassages, defaultCandidatePassageLimit)
	cfg.MaxActivePassages = defaultLimit(cfg.MaxActivePassages, defaultActivePassageLimit)
	cfg.MaxRetrievalCandidates = defaultLimit(cfg.MaxRetrievalCandidates, defaultRetrievalCandidateCap)
	cfg.MaxDocumentFilters = defaultLimit(cfg.MaxDocumentFilters, defaultDocumentFilterCap)
	cfg.MaxQueryRunes = defaultLimit(cfg.MaxQueryRunes, defaultDocumentQueryRunes)
	limits := []struct {
		name  string
		value int
	}{
		{"candidate passages", cfg.MaxCandidatePassages},
		{"active passages", cfg.MaxActivePassages},
		{"retrieval candidates", cfg.MaxRetrievalCandidates},
		{"document filters", cfg.MaxDocumentFilters},
		{"query runes", cfg.MaxQueryRunes},
	}
	for _, limit := range limits {
		if limit.value <= 0 {
			return DocumentIndexConfig{}, fmt.Errorf("arcadedb: document %s limit must be positive", limit.name)
		}
	}
	if cfg.MaxCandidatePassages > cfg.MaxActivePassages {
		return DocumentIndexConfig{}, fmt.Errorf(
			"arcadedb: candidate passage limit %d exceeds active passage limit %d",
			cfg.MaxCandidatePassages, cfg.MaxActivePassages,
		)
	}
	return cfg, nil
}

// TenantClientResolver resolves one identity to its physically isolated database.
type TenantClientResolver interface {
	For(context.Context, string) (*Client, error)
}

type documentSchemaCall struct {
	done chan struct{}
	err  error
}

// DocumentIndex owns the rebuildable ArcadeDB document projection for every tenant.
type DocumentIndex struct {
	tenants TenantClientResolver
	config  DocumentIndexConfig

	schemaMu     sync.Mutex
	schemaReady  map[string]struct{}
	schemaCalls  map[string]*documentSchemaCall
	projectMu    sync.Mutex
	projectLocks map[string]*sync.Mutex
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
	return &DocumentIndex{
		tenants: tenants, config: normalized,
		schemaReady: make(map[string]struct{}), schemaCalls: make(map[string]*documentSchemaCall),
		projectLocks: make(map[string]*sync.Mutex),
	}, nil
}

func (d *DocumentIndex) projectionLock(identityID string) *sync.Mutex {
	d.projectMu.Lock()
	defer d.projectMu.Unlock()
	lock := d.projectLocks[identityID]
	if lock == nil {
		lock = &sync.Mutex{}
		d.projectLocks[identityID] = lock
	}
	return lock
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
	if err := d.ensureTenantSchema(ctx, identityID, client); err != nil {
		return nil, err
	}
	return client, nil
}

func (d *DocumentIndex) ensureTenantSchema(ctx context.Context, identityID string, client *Client) error {
	d.schemaMu.Lock()
	if _, ready := d.schemaReady[identityID]; ready {
		d.schemaMu.Unlock()
		return nil
	}
	if call, running := d.schemaCalls[identityID]; running {
		d.schemaMu.Unlock()
		select {
		case <-call.done:
			return call.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &documentSchemaCall{done: make(chan struct{})}
	d.schemaCalls[identityID] = call
	d.schemaMu.Unlock()

	call.err = ensureDocumentSchema(ctx, client, d.config.Dimensions)
	d.schemaMu.Lock()
	if call.err == nil {
		d.schemaReady[identityID] = struct{}{}
	}
	delete(d.schemaCalls, identityID)
	close(call.done)
	d.schemaMu.Unlock()
	return call.err
}

func ensureDocumentSchema(ctx context.Context, client *Client, dimensions int) error {
	for _, statement := range documentSchemaStatements(dimensions) {
		if _, err := client.Command(ctx, statement, nil); err != nil {
			return fmt.Errorf("arcadedb: ensure document schema: %w", err)
		}
	}
	return nil
}

func documentSchemaStatements(dimensions int) []string {
	return []string{
		"CREATE VERTEX TYPE " + documentProjectionType + " IF NOT EXISTS",
		"CREATE PROPERTY " + documentProjectionType + ".projection_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".document_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".search_document_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".version_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".version_number IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentProjectionType + ".raw_sha256 IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".pipeline_generation IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".schema_version IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".projection_fingerprint IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentProjectionType + ".passage_count IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentProjectionType + ".active IF NOT EXISTS BOOLEAN",
		"CREATE PROPERTY " + documentProjectionType + ".created_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + documentProjectionType + ".tombstoned_at IF NOT EXISTS DATETIME",
		"CREATE INDEX IF NOT EXISTS ON " + documentProjectionType + " (projection_key) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + documentProjectionType + " (document_id) NOTUNIQUE",

		"CREATE VERTEX TYPE " + documentPassageType + " IF NOT EXISTS",
		"CREATE PROPERTY " + documentPassageType + ".passage_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".passage_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".projection_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".document_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".search_document_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".version_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".version_number IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentPassageType + ".raw_sha256 IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".pipeline_generation IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".schema_version IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".ordinal IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentPassageType + ".text IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".normalized_text_sha256 IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".self_ref IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".heading_path IF NOT EXISTS LIST OF STRING",
		"CREATE PROPERTY " + documentPassageType + ".captions IF NOT EXISTS LIST OF STRING",
		"CREATE PROPERTY " + documentPassageType + ".page_number IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentPassageType + ".bbox_left IF NOT EXISTS DOUBLE",
		"CREATE PROPERTY " + documentPassageType + ".bbox_top IF NOT EXISTS DOUBLE",
		"CREATE PROPERTY " + documentPassageType + ".bbox_right IF NOT EXISTS DOUBLE",
		"CREATE PROPERTY " + documentPassageType + ".bbox_bottom IF NOT EXISTS DOUBLE",
		"CREATE PROPERTY " + documentPassageType + ".char_start IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentPassageType + ".char_end IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentPassageType + ".sheet_name IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".table_name IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".row_number IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentPassageType + ".column_number IF NOT EXISTS LONG",
		"CREATE PROPERTY " + documentPassageType + ".cell_reference IF NOT EXISTS STRING",
		"CREATE PROPERTY " + documentPassageType + ".embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		"CREATE PROPERTY " + documentPassageType + ".active IF NOT EXISTS BOOLEAN",
		"CREATE PROPERTY " + documentPassageType + ".created_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + documentPassageType + ".tombstoned_at IF NOT EXISTS DATETIME",
		"CREATE INDEX IF NOT EXISTS ON " + documentPassageType + " (passage_key) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + documentPassageType + " (projection_key) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + documentPassageType + " (active, document_id) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + documentPassageType + " (text) FULL_TEXT " +
			"METADATA {analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}",
		"CREATE INDEX IF NOT EXISTS ON " + documentPassageType + " (embedding) LSM_VECTOR METADATA " +
			"{ \"dimensions\": " + strconv.Itoa(dimensions) +
			", \"similarity\": \"COSINE\", \"quantization\": \"NONE\" }",

		"CREATE EDGE TYPE " + documentPassageEdge + " IF NOT EXISTS",
	}
}

func requiredString(row map[string]any, key string) (string, error) {
	value, ok := row[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is missing or not a non-empty string", key)
	}
	return value, nil
}

func requiredNonNegativeInt(row map[string]any, key string) (int, error) {
	value, ok := exactInt64(row[key])
	if !ok || value < 0 || int64(int(value)) != value {
		return 0, fmt.Errorf("%s is missing or not a non-negative integer", key)
	}
	return int(value), nil
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

func requiredBool(row map[string]any, key string) (bool, error) {
	value, ok := row[key].(bool)
	if !ok {
		return false, fmt.Errorf("%s is missing or not a boolean", key)
	}
	return value, nil
}

func validateIdentifier(name, value string) error {
	if value == "" {
		return fmt.Errorf("arcadedb: %s must be non-empty", name)
	}
	if utf8.RuneCountInString(value) > maxProjectionIdentifierRunes {
		return fmt.Errorf("arcadedb: %s exceeds %d characters", name, maxProjectionIdentifierRunes)
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

func normalizeLabels(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func finiteBoundingBox(box BoundingBox) bool {
	for _, value := range []float64{box.Left, box.Top, box.Right, box.Bottom} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}
