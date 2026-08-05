package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// PipelineStage is one immutable-result stage in the document DAG.
type PipelineStage string

// Every string here is persisted or hashed into something persisted: the stage
// names key the pipeline stage ledger, and the normalization tag feeds the embed
// stage's input fingerprint. Editing one is not a rename — it orphans the ledger
// rows written under the old spelling and re-embeds every document already ingested.
const (
	PipelineStageIdentify     PipelineStage = "identify"
	PipelineStageConvert      PipelineStage = "convert"
	PipelineStageChunk        PipelineStage = "chunk"
	PipelineStageEmbed        PipelineStage = "embed"
	PipelineStageProject      PipelineStage = "project"
	PipelineStageActivate     PipelineStage = "activate"
	PipelineStageCard         PipelineStage = "card"
	EmbeddingNormalizationMRL               = "mrl-prefix-l2-renormalize-v1"
)

// PassageLocator carries only source coordinates proven by conversion. Optional
// fields stay absent; the pipeline never invents a page, box, sheet, or cell.
type PassageLocator struct {
	SelfRef      string    `json:"self_ref,omitempty"`
	HeadingPath  []string  `json:"heading_path,omitempty"`
	Captions     []string  `json:"captions,omitempty"`
	PageNumber   *int      `json:"page_number,omitempty"`
	BoundingBox  []float64 `json:"bounding_box,omitempty"`
	CharStart    *int      `json:"char_start,omitempty"`
	CharEnd      *int      `json:"char_end,omitempty"`
	SheetName    string    `json:"sheet_name,omitempty"`
	TableName    string    `json:"table_name,omitempty"`
	RowNumber    *int      `json:"row_number,omitempty"`
	ColumnNumber *int      `json:"column_number,omitempty"`
	CellRef      string    `json:"cell_ref,omitempty"`
}

// Passage is the immutable, provenance-bearing unit shared by PostgreSQL and
// the per-identity ArcadeDB projection.
type Passage struct {
	ID                   string         `json:"id"`
	IdentityID           string         `json:"identity_id"`
	DocumentID           string         `json:"document_id"`
	SearchDocumentID     string         `json:"search_document_id"`
	VersionID            string         `json:"version_id"`
	VersionNumber        int            `json:"version_number"`
	OriginalSHA256       string         `json:"original_sha256"`
	PipelineGeneration   int64          `json:"pipeline_generation"`
	Ordinal              int            `json:"ordinal"`
	Text                 string         `json:"text"`
	ContextualizedText   string         `json:"contextualized_text"`
	NormalizedTextSHA256 string         `json:"normalized_text_sha256"`
	Locator              PassageLocator `json:"locator"`
}

// DeterministicVersionID derives an RFC-4122 variant, version-8 UUID from the
// logical document UUID and canonical raw SHA-256. The same bytes replay to the
// same version across workers and processes.
func DeterministicVersionID(documentID, rawSHA256 string) (string, error) {
	if _, err := uuid.Parse(strings.TrimSpace(documentID)); err != nil {
		return "", fmt.Errorf("document id: %w", err)
	}
	rawSHA256, err := canonicalSHA256(rawSHA256)
	if err != nil {
		return "", fmt.Errorf("raw sha256: %w", err)
	}
	return deterministicUUID("aura.document.version.v1", documentID, rawSHA256), nil
}

// DeterministicPassageID distinguishes repeated text at different anchors and
// occurrences while replaying the same structural passage to the same UUID.
func DeterministicPassageID(versionID, structuralAnchor string, occurrence int, normalizedTextSHA256 string) (string, error) {
	if _, err := uuid.Parse(strings.TrimSpace(versionID)); err != nil {
		return "", fmt.Errorf("version id: %w", err)
	}
	if strings.TrimSpace(structuralAnchor) == "" {
		return "", fmt.Errorf("structural anchor is required")
	}
	if occurrence < 0 {
		return "", fmt.Errorf("occurrence must be non-negative")
	}
	normalizedTextSHA256, err := canonicalSHA256(normalizedTextSHA256)
	if err != nil {
		return "", fmt.Errorf("normalized text sha256: %w", err)
	}
	return deterministicUUID(
		"aura.document.passage.v1", versionID, strings.TrimSpace(structuralAnchor),
		strconv.Itoa(occurrence), normalizedTextSHA256,
	), nil
}

// NormalizedTextSHA256 canonicalizes Unicode and whitespace before hashing.
func NormalizedTextSHA256(text string) string {
	normalized := strings.Join(strings.Fields(norm.NFC.String(text)), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// CanonicalLocator returns the most precise proven locator, falling back to the
// immutable passage ordinal only when conversion supplied no richer coordinate.
func CanonicalLocator(locator PassageLocator, ordinal int) string {
	if cell := strings.TrimSpace(locator.CellRef); cell != "" {
		if sheet := strings.TrimSpace(locator.SheetName); sheet != "" {
			return "sheet:" + url.PathEscape(sheet) + "!" + url.PathEscape(cell)
		}
		return "cell:" + url.PathEscape(cell)
	}
	if locator.PageNumber != nil && *locator.PageNumber > 0 {
		return "page:" + strconv.Itoa(*locator.PageNumber)
	}
	if ref := strings.TrimSpace(locator.SelfRef); ref != "" {
		return "ref:" + url.PathEscape(ref)
	}
	if locator.CharStart != nil && *locator.CharStart >= 0 {
		end := *locator.CharStart
		if locator.CharEnd != nil && *locator.CharEnd >= end {
			end = *locator.CharEnd
		}
		return fmt.Sprintf("char:%d-%d", *locator.CharStart, end)
	}
	if ordinal < 0 {
		ordinal = 0
	}
	return "passage:" + strconv.Itoa(ordinal)
}

// CitationToken binds an answerable passage to one immutable active version.
func CitationToken(searchDocumentID string, versionNumber int, locator PassageLocator, ordinal int) (string, error) {
	searchDocumentID = strings.TrimSpace(searchDocumentID)
	if searchDocumentID == "" {
		return "", fmt.Errorf("search document id is required")
	}
	if versionNumber <= 0 {
		return "", fmt.Errorf("version number must be positive")
	}
	return fmt.Sprintf("document:%s@%d#%s", url.PathEscape(searchDocumentID), versionNumber, CanonicalLocator(locator, ordinal)), nil
}

func deterministicUUID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	var id uuid.UUID
	copy(id[:], h.Sum(nil)[:16])
	id[6] = (id[6] & 0x0f) | 0x80
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String()
}

func canonicalSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("must be 64 hexadecimal characters")
	}
	return value, nil
}
