package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRetrievalStore answers the routing and revalidation half of retrieval. It is
// deliberately separate from the projection store: PostgreSQL owns which version is
// active and openable, and that verdict overrides anything the vector index still holds.
type PostgresRetrievalStore struct {
	pool *pgxpool.Pool
}

// NewPostgresRetrievalStore tolerates a nil pool so wiring can build the retriever
// before the pool exists; each method then reports "not configured" rather than panicking.
func NewPostgresRetrievalStore(pool *pgxpool.Pool) *PostgresRetrievalStore {
	return &PostgresRetrievalStore{pool: pool}
}

const readyDocumentJoins = `
JOIN aura.document_versions version
  ON version.id = document.active_version_id
 AND version.identity_id = document.identity_id
 AND version.document_id = document.id
JOIN aura.storage_objects raw_object
  ON raw_object.id = version.storage_object_id
 AND raw_object.identity_id = document.identity_id
 AND raw_object.document_id = document.id
 AND raw_object.version_id = version.id
JOIN aura.assets asset
  ON asset.id = version.asset_id
 AND asset.identity_id = document.identity_id
`

const readyDocumentWhere = `
document.identity_id = $1::uuid
AND document.status = 'ready'
AND document.deleted_at IS NULL
AND document.pipeline_generation > 0
AND version.status = 'ready'
AND version.deleted_at IS NULL
AND version.pipeline_generation = document.pipeline_generation
AND raw_object.status = 'live'
AND raw_object.deleted_at IS NULL
AND raw_object.asset_id = asset.id
AND raw_object.sha256 = version.sha256
AND asset.status = 'searchable'
AND asset.deleted_at IS NULL
AND asset.catalog_document_id = document.id
AND asset.document_version_id = version.id
AND asset.pipeline_generation = document.pipeline_generation
`

const resolveDocumentScopeSQL = `
SELECT document.id::text, document.search_document_id
FROM aura.documents document
` + readyDocumentJoins + `
WHERE ` + readyDocumentWhere + `
  AND (document.id::text = ANY($2::text[]) OR document.search_document_id = ANY($2::text[]))
ORDER BY document.id
`

const routeDocumentCardsSQL = `
SELECT document.id::text, document.search_document_id, document.title, document.tags,
       document.digest, document.card, version.id::text, version.version_number,
       version.sha256, version.pipeline_generation,
       ts_rank(document.digest_tsv,
         websearch_to_tsquery('simple', public.unaccent('public.unaccent'::regdictionary, $2::text)))
FROM aura.documents document
` + readyDocumentJoins + `
WHERE ` + readyDocumentWhere + `
  AND ($3::text[] = '{}'::text[] OR document.id::text = ANY($3::text[]))
  AND document.digest_tsv @@ websearch_to_tsquery(
        'simple', public.unaccent('public.unaccent'::regdictionary, $2::text))
ORDER BY 11 DESC, document.id ASC
LIMIT $4
`

// ResolveDocumentScope narrows caller-supplied document ids to the ones this identity
// actually owns. Unknown ids are dropped rather than rejected, so a stale id in a client
// request cannot be used to probe whether a document exists in another tenant.
func (s *PostgresRetrievalStore) ResolveDocumentScope(
	ctx context.Context,
	identityID string,
	documentIDs []string,
) ([]string, error) {
	if len(documentIDs) == 0 {
		return nil, nil
	}
	if err := validateRetrievalStore(s, identityID); err != nil {
		return nil, err
	}
	requested := make(map[string]struct{}, len(documentIDs))
	for _, id := range documentIDs {
		requested[id] = struct{}{}
	}
	resolved := make(map[string]struct{}, len(documentIDs))
	matched := make(map[string]struct{}, len(documentIDs))
	err := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, resolveDocumentScopeSQL, identityID, documentIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var catalogID, searchID string
			if err := rows.Scan(&catalogID, &searchID); err != nil {
				return err
			}
			resolved[catalogID] = struct{}{}
			if _, ok := requested[catalogID]; ok {
				matched[catalogID] = struct{}{}
			}
			if _, ok := requested[searchID]; ok {
				matched[searchID] = struct{}{}
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("documents: resolve retrieval scope: %w", err)
	}
	if len(matched) != len(requested) {
		return nil, ErrInvalidDocumentScope
	}
	out := make([]string, 0, len(resolved))
	for id := range resolved {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// RouteDocumentCards is the lexical leg over title, tags, digest and card. It runs even
// when the vector index is reachable: the cascade needs a candidate set that survives an
// embedding failure, and this is the leg that degrades to.
func (s *PostgresRetrievalStore) RouteDocumentCards(
	ctx context.Context,
	identityID string,
	query string,
	documentIDs []string,
	limit int,
) ([]RetrievalCard, error) {
	if err := validateRetrievalStore(s, identityID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, fmt.Errorf("documents: card route requires a query and positive limit")
	}
	cards := make([]RetrievalCard, 0, min(limit, 32))
	err := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, routeDocumentCardsSQL, identityID, query, documentIDs, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			card, err := scanRetrievalCard(rows)
			if err != nil {
				return err
			}
			cards = append(cards, card)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return cards, nil
}

type retrievalCandidateInput struct {
	Key                string `json:"key"`
	PassageID          string `json:"passage_id"`
	DocumentID         string `json:"document_id"`
	SearchDocumentID   string `json:"search_document_id"`
	VersionID          string `json:"version_id"`
	VersionNumber      int64  `json:"version_number"`
	RawSHA256          string `json:"raw_sha256"`
	PipelineGeneration int64  `json:"pipeline_generation"`
	Ordinal            int64  `json:"ordinal"`
	NormalizedSHA256   string `json:"normalized_sha256"`
}

const revalidateDocumentCandidatesSQL = `
WITH candidate AS MATERIALIZED (
  SELECT * FROM jsonb_to_recordset($2::jsonb) AS input(
    key text, passage_id uuid, document_id uuid, search_document_id text,
    version_id uuid, version_number bigint, raw_sha256 text,
    pipeline_generation bigint, ordinal bigint, normalized_sha256 text
  )
)
SELECT candidate.key, document.id::text, document.search_document_id,
       document.title, document.tags, document.digest, document.card,
       version.id::text, version.version_number, version.sha256,
       version.pipeline_generation,
       chunk.id::text, chunk.ordinal, chunk.text, chunk.normalized_text_sha256,
       chunk.self_ref, chunk.heading_path, chunk.captions, chunk.page_number,
       chunk.bounding_box, chunk.char_start, chunk.char_end, chunk.sheet_name,
       chunk.table_name, chunk.row_number, chunk.column_number, chunk.cell_ref
FROM candidate
JOIN aura.documents document ON document.id = candidate.document_id
` + readyDocumentJoins + `
JOIN aura.document_chunks chunk
  ON chunk.id = candidate.passage_id
 AND chunk.identity_id = document.identity_id
 AND chunk.document_id = document.id
 AND chunk.version_id = version.id
WHERE ` + readyDocumentWhere + `
  AND candidate.search_document_id = document.search_document_id
  AND candidate.version_id = version.id
  AND candidate.version_number = version.version_number
  AND candidate.raw_sha256 = version.sha256
  AND candidate.pipeline_generation = document.pipeline_generation
	AND candidate.ordinal = chunk.ordinal
  AND chunk.search_document_id = candidate.search_document_id
  AND chunk.version_number = candidate.version_number
  AND chunk.original_sha256 = candidate.raw_sha256
  AND chunk.pipeline_generation = candidate.pipeline_generation
  AND chunk.normalized_text_sha256 = candidate.normalized_sha256
  AND chunk.active
  AND chunk.deleted_at IS NULL
  AND EXISTS (
    SELECT 1 FROM aura.document_embeddings embedding
    WHERE embedding.identity_id = document.identity_id
      AND embedding.document_id = document.id
      AND embedding.version_id = version.id
      AND embedding.chunk_id = chunk.id
      AND embedding.pipeline_generation = document.pipeline_generation
      AND embedding.vector_id = candidate.passage_id::text
      AND embedding.projection_status = 'active'
      AND embedding.projected_at IS NOT NULL
      AND embedding.active
      AND embedding.deleted_at IS NULL
  )
ORDER BY candidate.key
`

// RevalidateDocumentCandidates re-checks every candidate against the active generation
// before any text, locator or citation is returned. The projection can outlive an
// activation swap or a delete, so a hit that is not confirmed here is dropped.
func (s *PostgresRetrievalStore) RevalidateDocumentCandidates(
	ctx context.Context,
	identityID string,
	candidates []arcadedb.PassageCandidate,
) (map[string]RevalidatedCandidate, error) {
	if err := validateRetrievalStore(s, identityID); err != nil {
		return nil, err
	}
	inputs, err := candidateInputs(candidates)
	if err != nil {
		return nil, err
	}
	validated := make(map[string]RevalidatedCandidate, len(inputs))
	if len(inputs) == 0 {
		return validated, nil
	}
	payload, err := json.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("documents: encode retrieval candidates: %w", err)
	}
	err = db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, revalidateDocumentCandidatesSQL, identityID, payload)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			candidate, err := scanValidatedCandidate(rows, &key)
			if err != nil {
				return err
			}
			if _, duplicate := validated[key]; duplicate {
				return fmt.Errorf("documents: duplicate validated retrieval candidate %s", key)
			}
			validated[key] = candidate
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return validated, nil
}

func candidateInputs(candidates []arcadedb.PassageCandidate) ([]retrievalCandidateInput, error) {
	inputs := make([]retrievalCandidateInput, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := candidateCoordinateKey(candidate)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		for name, value := range map[string]string{
			"passage": candidate.PassageID, "document": candidate.DocumentID,
			"version": candidate.VersionID,
		} {
			if _, err := uuid.Parse(value); err != nil {
				return nil, fmt.Errorf("documents: candidate %s id is invalid", name)
			}
		}
		generation, err := strconv.ParseInt(candidate.PipelineGeneration, 10, 64)
		if err != nil || generation <= 0 || candidate.VersionNumber <= 0 {
			return nil, fmt.Errorf("documents: candidate generation or version is invalid")
		}
		input := retrievalCandidateInput{
			Key: key, PassageID: candidate.PassageID, DocumentID: candidate.DocumentID,
			SearchDocumentID: candidate.SearchDocumentID, VersionID: candidate.VersionID,
			VersionNumber: candidate.VersionNumber, RawSHA256: candidate.RawSHA256,
			PipelineGeneration: generation, Ordinal: candidate.Ordinal,
			NormalizedSHA256: candidate.NormalizedSHA256,
		}
		seen[key], inputs = struct{}{}, append(inputs, input)
	}
	return inputs, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanRetrievalCard(row rowScanner) (RetrievalCard, error) {
	var card RetrievalCard
	var rank float32
	err := row.Scan(
		&card.CatalogID, &card.DocumentID, &card.Title, &card.Tags,
		&card.Digest, &card.Card, &card.VersionID, &card.VersionNumber,
		&card.OriginalSHA256, &card.PipelineGeneration, &rank,
	)
	card.Rank = float64(rank)
	return card, err
}

func scanValidatedCandidate(row rowScanner, key *string) (RevalidatedCandidate, error) {
	var card RetrievalCard
	var passage arcadedb.PassageCandidate
	var pageNumber, charStart, charEnd, rowNumber, columnNumber pgtype.Int4
	var sheetName, tableName, cellReference pgtype.Text
	var boundingBox []byte
	err := row.Scan(
		key, &card.CatalogID, &card.DocumentID, &card.Title, &card.Tags,
		&card.Digest, &card.Card, &card.VersionID, &card.VersionNumber,
		&card.OriginalSHA256, &card.PipelineGeneration,
		&passage.PassageID, &passage.Ordinal, &passage.Text, &passage.NormalizedSHA256,
		&passage.SelfRef, &passage.HeadingPath, &passage.Captions, &pageNumber,
		&boundingBox, &charStart, &charEnd, &sheetName, &tableName,
		&rowNumber, &columnNumber, &cellReference,
	)
	if err != nil {
		return RevalidatedCandidate{}, err
	}
	passage.DocumentID, passage.SearchDocumentID = card.CatalogID, card.DocumentID
	passage.VersionID, passage.VersionNumber = card.VersionID, card.VersionNumber
	passage.RawSHA256 = card.OriginalSHA256
	passage.PipelineGeneration = strconv.FormatInt(card.PipelineGeneration, 10)
	passage.PageNumber, passage.RowNumber = int64FromInt4(pageNumber), int64FromInt4(rowNumber)
	passage.ColumnNumber = int64FromInt4(columnNumber)
	passage.SheetName, passage.TableName = sheetName.String, tableName.String
	passage.CellReference = cellReference.String
	if charStart.Valid != charEnd.Valid {
		return RevalidatedCandidate{}, fmt.Errorf("documents: authoritative character span is incomplete")
	}
	if charStart.Valid {
		passage.CharacterSpan = &arcadedb.CharacterSpan{
			Start: int64(charStart.Int32), End: int64(charEnd.Int32),
		}
	}
	if len(boundingBox) > 0 {
		var coordinates []float64
		if err := json.Unmarshal(boundingBox, &coordinates); err != nil || len(coordinates) != 4 {
			return RevalidatedCandidate{}, fmt.Errorf("documents: authoritative bounding box is invalid")
		}
		passage.BoundingBox = &arcadedb.BoundingBox{
			Left: coordinates[0], Top: coordinates[1], Right: coordinates[2], Bottom: coordinates[3],
		}
	}
	return RevalidatedCandidate{Card: card, Passage: passage}, nil
}

func int64FromInt4(value pgtype.Int4) *int64 {
	if !value.Valid {
		return nil
	}
	converted := int64(value.Int32)
	return &converted
}

func validateRetrievalStore(store *PostgresRetrievalStore, identityID string) error {
	if store == nil || store.pool == nil {
		return fmt.Errorf("documents: retrieval store is not configured")
	}
	if _, err := uuid.Parse(strings.TrimSpace(identityID)); err != nil {
		return fmt.Errorf("documents: retrieval identity is invalid")
	}
	return nil
}
