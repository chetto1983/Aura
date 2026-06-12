package documents

import (
	"context"
	"encoding/json"
	"fmt"
)

const defaultSparseBatchSize = 250

type KnowledgeClient interface {
	Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
	Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}

type Indexer struct {
	Client    KnowledgeClient
	BatchSize int
}

func (i *Indexer) UpsertSparse(ctx context.Context, doc ExtractedDocument) (int, error) {
	if i.Client == nil {
		return 0, fmt.Errorf("document indexer has no knowledge client")
	}
	if len(doc.Chunks) == 0 {
		return 0, fmt.Errorf("document has no chunks")
	}
	if _, err := i.Client.Write(ctx, documentUpsertQuery, map[string]any{"document": documentParams(doc)}); err != nil {
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
	if _, err := i.Client.Write(ctx, documentSearchableQuery, map[string]any{
		"document_id": doc.ID,
		"chunk_count": len(doc.Chunks),
	}); err != nil {
		return total, fmt.Errorf("mark document searchable: %w", err)
	}
	return total, nil
}

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
			"id":        chunk.ID,
			"embedding": chunk.Embedding,
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
		})
	}
	return out, nil
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
  d.updated_at = datetime(),
  d.created_at = coalesce(d.created_at, datetime())
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
  c.updated_at = datetime(),
  c.created_at = coalesce(c.created_at, datetime())
MERGE (d)-[:HAS_CHUNK]->(c)
RETURN count(c) AS chunks
`

const documentSearchableQuery = `
MATCH (d:Document {id: $document_id})
SET
  d.status = "searchable",
  d.chunk_count = $chunk_count,
  d.updated_at = datetime()
RETURN d.id AS document_id
`

const embeddingUpsertQuery = `
UNWIND $chunks AS chunk
MATCH (c:Chunk {id: chunk.id})
SET
  c.embedding = chunk.embedding,
  c.embedded_at = datetime()
WITH count(c) AS embedded
MATCH (d:Document {id: $document_id})
SET
  d.status = $status,
  d.embedded_chunk_count = $embedded_count,
  d.updated_at = datetime()
RETURN embedded AS embedded
`
