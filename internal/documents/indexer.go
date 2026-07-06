package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const defaultSparseBatchSize = 250
const defaultEmbeddingModel = "default"
const defaultEmbeddingVersion = "v1"

// KnowledgeClient is the subset of Neo4j knowledge operations used by indexing.
type KnowledgeClient interface {
	Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
	Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}

// Indexer writes extracted documents, chunks, and embeddings into Neo4j.
type Indexer struct {
	Client           KnowledgeClient
	BatchSize        int
	EmbeddingModel   string
	EmbeddingVersion string
}

// UpsertSparse stores document metadata and searchable text chunks.
func (i *Indexer) UpsertSparse(ctx context.Context, doc ExtractedDocument) (int, error) {
	if i.Client == nil {
		return 0, fmt.Errorf("document indexer has no knowledge client")
	}
	if len(doc.Chunks) == 0 {
		return 0, fmt.Errorf("document has no chunks")
	}
	// LO-03: normalize an empty ingest identity to the operator (local UUID …001) before the
	// ownership MERGE — an empty-identity ingest (a CLI/legacy path that never threads the
	// `local` UUID) would otherwise MERGE (:User {identifier:""}), orphaning the doc from
	// everyone post-flip (scoped retrieval fails closed on `$identity <> ""`). Attributing it
	// to the operator keeps it reachable, matching the retrieval/backfill owner convention.
	identity := doc.IdentityID
	if strings.TrimSpace(identity) == "" {
		identity = OperatorIdentity
	}
	if _, err := i.Client.Write(ctx, documentUpsertQuery, map[string]any{
		"document": documentParams(doc),
		"identity": identity,
	}); err != nil {
		return 0, fmt.Errorf("upsert document: %w", err)
	}

	batchSize := i.BatchSize
	if batchSize <= 0 {
		batchSize = defaultSparseBatchSize
	}
	total := 0
	for start := 0; start < len(doc.Chunks); start += batchSize {
		end := min(start+batchSize, len(doc.Chunks))
		chunkParams, err := chunksParams(doc.Chunks[start:end], doc.FileName)
		if err != nil {
			return total, err
		}
		rows, err := i.Client.Write(ctx, chunkUpsertQuery, map[string]any{
			"document_id": doc.ID,
			"chunks":      chunkParams,
		})
		if err != nil {
			return total, fmt.Errorf("upsert chunks %d-%d: %w", start, end, err)
		}
		total += countFromRows(rows, end-start)
	}
	if pairs := nextChunkPairs(doc.Chunks); len(pairs) > 0 {
		if _, err := i.Client.Write(ctx, nextChunkUpsertQuery, map[string]any{
			"document_id": doc.ID,
			"pairs":       pairs,
		}); err != nil {
			return total, fmt.Errorf("link next-chunk edges: %w", err)
		}
	}
	if _, err := i.Client.Write(ctx, documentSearchableQuery, map[string]any{
		"document_id": doc.ID,
		"chunk_count": len(doc.Chunks),
	}); err != nil {
		return total, fmt.Errorf("mark document searchable: %w", err)
	}
	return total, nil
}

// UpsertEmbeddings stores vector embeddings for already indexed chunks.
func (i *Indexer) UpsertEmbeddings(ctx context.Context, documentID string, chunks []EmbeddedChunk, status JobStatus, embeddedCount int) (int, error) {
	if i.Client == nil {
		return 0, fmt.Errorf("document indexer has no knowledge client")
	}
	if len(chunks) == 0 {
		return 0, nil
	}
	params := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		params = append(params, map[string]any{
			"id":                chunk.ID,
			"embedding":         chunk.Embedding,
			"embedding_model":   i.embeddingModel(),
			"embedding_version": i.embeddingVersion(),
			"active":            true,
		})
	}
	rows, err := i.Client.Write(ctx, embeddingUpsertQuery, map[string]any{
		"document_id":    documentID,
		"chunks":         params,
		"status":         string(status),
		"embedded_count": embeddedCount,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert embeddings: %w", err)
	}
	return countFromRows(rows, len(chunks)), nil
}

// DeactivateDocument marks one document and its chunks inactive in the graph.
func (i *Indexer) DeactivateDocument(ctx context.Context, documentID string) error {
	if i.Client == nil {
		return fmt.Errorf("document indexer has no knowledge client")
	}
	if documentID == "" {
		return fmt.Errorf("document id is required")
	}
	_, err := i.Client.Write(ctx, documentDeactivateQuery, map[string]any{"document_id": documentID})
	if err != nil {
		return fmt.Errorf("deactivate document graph: %w", err)
	}
	return nil
}

func documentParams(doc ExtractedDocument) map[string]any {
	return map[string]any{
		"id":                   doc.ID,
		"source_id":            doc.SourceID,
		"source_kind":          doc.SourceKind,
		"file_name":            doc.FileName,
		"mime_type":            doc.MIMEType,
		"size_bytes":           doc.SizeBytes,
		"content_hash":         doc.ContentHash,
		"title":                doc.Title,
		"status":               string(JobExtracting),
		"chunk_count":          len(doc.Chunks),
		"embedded_chunk_count": 0,
		"active":               true,
	}
}

func chunksParams(chunks []Chunk, fileName string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		locator, err := json.Marshal(chunk.Locator)
		if err != nil {
			return nil, fmt.Errorf("marshal locator for %s: %w", chunk.ID, err)
		}
		out = append(out, map[string]any{
			"id":           chunk.ID,
			"document_id":  chunk.DocumentID,
			"source_id":    chunk.SourceID,
			"file_name":    fileName,
			"content_hash": chunk.ContentHash,
			"chunk_hash":   chunk.ChunkHash,
			"chunk_index":  chunk.ChunkIndex,
			"chunk_count":  chunk.ChunkCount,
			"kind":         chunk.Kind,
			"text":         chunk.Text,
			"locator_json": string(locator),
			"heading_path": chunk.HeadingPath,
			"active":       true,
		})
	}
	return out, nil
}

func (i *Indexer) embeddingModel() string {
	if i.EmbeddingModel != "" {
		return i.EmbeddingModel
	}
	return defaultEmbeddingModel
}

func (i *Indexer) embeddingVersion() string {
	if i.EmbeddingVersion != "" {
		return i.EmbeddingVersion
	}
	return defaultEmbeddingVersion
}

// nextChunkPairs returns the consecutive {prev,next} chunk-id pairs that form a
// document's reading-order chain, ordered by chunk index. A document with fewer
// than two chunks has no edges.
func nextChunkPairs(chunks []Chunk) []map[string]any {
	if len(chunks) < 2 {
		return nil
	}
	ordered := slices.Clone(chunks)
	slices.SortFunc(ordered, func(a, b Chunk) int { return a.ChunkIndex - b.ChunkIndex })
	pairs := make([]map[string]any, 0, len(ordered)-1)
	for i := 0; i+1 < len(ordered); i++ {
		pairs = append(pairs, map[string]any{
			"prev": ordered[i].ID,
			"next": ordered[i+1].ID,
		})
	}
	return pairs
}

func countFromRows(rows []map[string]any, fallback int) int {
	if len(rows) == 0 {
		return fallback
	}
	for _, key := range []string{"chunks", "count", "embedded"} {
		if v, ok := rows[0][key]; ok {
			switch n := v.(type) {
			case int:
				return n
			case int64:
				return int(n)
			case int32:
				return int(n)
			case float64:
				return int(n)
			}
		}
	}
	return fallback
}

// documentUpsertQuery upserts the :Document node AND, atomically in the same write,
// MERGEs the (:User {identifier: $identity})-[:HAS_DOCUMENT]->(:Document) ownership edge
// that identity-scoped retrieval fails closed against (Phase 36 MUSR-01, D-09). The edge
// is attached on EVERY ingest regardless of the AURA_MUSR_ISOLATION flag — the flag gates
// read enforcement only, so the graph is owner-ready before the plan-12 flip (no re-ingest).
// The `WITH u` fence between the User MERGE and the re-MATCH of the Document is mandatory
// (spike-085 gotcha #a: a MERGE directly followed by a MATCH errors at runtime); the shape
// mirrors the shipped memory LINK_USER_TO_ENTITY (queries.py) verbatim.
const documentUpsertQuery = `
MERGE (d:Document {id: $document.id})
SET
  d.source_id = $document.source_id,
  d.source_kind = $document.source_kind,
  d.file_name = $document.file_name,
  d.mime_type = $document.mime_type,
  d.size_bytes = $document.size_bytes,
  d.content_hash = $document.content_hash,
  d.title = $document.title,
  d.status = $document.status,
  d.chunk_count = $document.chunk_count,
  d.embedded_chunk_count = coalesce(d.embedded_chunk_count, $document.embedded_chunk_count),
  d.active = $document.active,
  d.deleted_at = NULL,
  d.updated_at = datetime(),
  d.created_at = coalesce(d.created_at, datetime())
WITH d
MERGE (u:User {identifier: $identity})
  ON CREATE SET u.id = $identity, u.created_at = datetime()
WITH u
MATCH (d:Document {id: $document.id})
MERGE (u)-[:HAS_DOCUMENT]->(d)
RETURN d.id AS document_id
`

const chunkUpsertQuery = `
MATCH (d:Document {id: $document_id})
UNWIND $chunks AS chunk
MERGE (c:Chunk {id: chunk.id})
SET
  c.document_id = chunk.document_id,
  c.source_id = chunk.source_id,
  c.file_name = chunk.file_name,
  c.content_hash = chunk.content_hash,
  c.chunk_hash = chunk.chunk_hash,
  c.chunk_index = chunk.chunk_index,
  c.chunk_count = chunk.chunk_count,
  c.kind = chunk.kind,
  c.text = chunk.text,
  c.locator_json = chunk.locator_json,
  c.heading_path = chunk.heading_path,
  c.active = chunk.active,
  c.deleted_at = NULL,
  c.updated_at = datetime(),
  c.created_at = coalesce(c.created_at, datetime())
MERGE (d)-[:HAS_CHUNK]->(c)
RETURN count(c) AS chunks
`

// nextChunkUpsertQuery links consecutive chunks into a reading-order chain. It
// MATCHes the already-upserted chunk nodes (so it never creates bare duplicates)
// and MERGEs the relationship, making re-ingest idempotent. All inputs are bound
// $-parameters (no string interpolation), keeping the write mojibake-safe.
const nextChunkUpsertQuery = `
UNWIND $pairs AS pair
MATCH (a:Chunk {id: pair.prev})
MATCH (b:Chunk {id: pair.next})
MERGE (a)-[:NEXT_CHUNK]->(b)
RETURN count(*) AS chunks
`

const documentSearchableQuery = `
MATCH (d:Document {id: $document_id})
SET
  d.status = "searchable",
  d.chunk_count = $chunk_count,
  d.active = true,
  d.deleted_at = NULL,
  d.updated_at = datetime()
RETURN d.id AS document_id
`

const embeddingUpsertQuery = `
UNWIND $chunks AS chunk
MATCH (c:Chunk {id: chunk.id})
SET
  c.embedding = chunk.embedding,
  c.embedding_model = chunk.embedding_model,
  c.embedding_version = chunk.embedding_version,
  c.active = chunk.active,
  c.deleted_at = NULL,
  c.embedded_at = datetime()
WITH count(c) AS embedded
MATCH (d:Document {id: $document_id})
SET
  d.status = $status,
  d.embedded_chunk_count = $embedded_count,
  d.active = true,
  d.deleted_at = NULL,
  d.updated_at = datetime()
RETURN embedded AS embedded
`

const documentDeactivateQuery = `
MATCH (d:Document {id: $document_id})
SET
  d.active = false,
  d.deleted_at = datetime(),
  d.updated_at = datetime()
WITH d
OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c:Chunk)
SET
  c.active = false,
  c.deleted_at = datetime(),
  c.updated_at = datetime()
RETURN count(c) AS chunks
`
