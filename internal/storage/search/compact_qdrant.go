package search

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/qdrant"
)

type CompactMemoryQdrantIndex struct {
	client       qdrant.Client
	collection   string
	embedFn      EmbeddingFunction
	batchEmbedFn BatchEmbeddingFunction
	logger       *slog.Logger
}

func CompactMemoryQdrantCollection(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	return base + "_compact"
}

func NewCompactMemoryQdrantIndex(cfg QdrantConfig, embedFn EmbeddingFunction, logger *slog.Logger) (*CompactMemoryQdrantIndex, error) {
	return NewCompactMemoryQdrantIndexWithBatch(cfg, embedFn, nil, logger)
}

func NewCompactMemoryQdrantIndexWithBatch(cfg QdrantConfig, embedFn EmbeddingFunction, batchEmbedFn BatchEmbeddingFunction, logger *slog.Logger) (*CompactMemoryQdrantIndex, error) {
	if embedFn == nil {
		return nil, fmt.Errorf("embedding function is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	client, collection, err := newQdrantClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &CompactMemoryQdrantIndex{client: client, collection: collection, embedFn: embedFn, batchEmbedFn: batchEmbedFn, logger: logger}, nil
}

func (i *CompactMemoryQdrantIndex) Recreate(ctx context.Context, docs []memoryindex.Document) (memoryindex.VectorReport, error) {
	if i == nil || i.client == nil {
		return memoryindex.VectorReport{}, fmt.Errorf("compact qdrant index unavailable")
	}
	if len(docs) == 0 {
		if err := i.client.DeleteCollection(ctx, i.collection); err != nil {
			return memoryindex.VectorReport{}, err
		}
		return memoryindex.VectorReport{Collection: i.collection}, nil
	}

	// QDRANT-01 warm-cache short-circuit: reuse the existing collection if it
	// already has points instead of re-embedding every document.
	//
	// WR-03 / T-01-24 (accepted): vector-size drift from EMBEDDING_MODEL swaps
	// is not detected here because CollectionInfo does not expose the stored
	// vector size. Search queries will fail loudly with a Qdrant dimension
	// error if the operator changes models without rebuilding the collection.
	info, infoErr := i.client.CollectionInfo(ctx, i.collection)
	if infoErr != nil {
		i.logger.Warn("compact qdrant warm-cache probe failed; proceeding with full rebuild", "collection", i.collection, "error", infoErr)
	} else if info.PointsCount > 0 {
		i.logger.Info("compact qdrant warm-cache hit, skipping rebuild", "collection", i.collection, "points_count", info.PointsCount)
		return memoryindex.VectorReport{
			Collection: i.collection,
		}, nil
	}

	points, vectorSize, err := i.pointsForDocuments(ctx, docs)
	if err != nil {
		return memoryindex.VectorReport{}, err
	}
	if err := i.client.DeleteCollection(ctx, i.collection); err != nil {
		return memoryindex.VectorReport{}, err
	}
	if err := i.client.CreateCollection(ctx, i.collection, vectorSize); err != nil {
		return memoryindex.VectorReport{}, err
	}
	if err := i.client.Upsert(ctx, i.collection, points); err != nil {
		return memoryindex.VectorReport{}, err
	}
	return memoryindex.VectorReport{
		Collection:  i.collection,
		DocsIndexed: len(points),
		VectorSize:  vectorSize,
	}, nil
}

func (i *CompactMemoryQdrantIndex) Upsert(ctx context.Context, docs []memoryindex.Document) error {
	if i == nil || i.client == nil || len(docs) == 0 {
		return nil
	}
	points, vectorSize, err := i.pointsForDocuments(ctx, docs)
	if err != nil {
		return err
	}
	if err := i.client.Upsert(ctx, i.collection, points); err != nil {
		// If the collection does not exist yet, create it and retry.
		if isNotFoundErr(err) {
			if createErr := i.client.CreateCollection(ctx, i.collection, vectorSize); createErr != nil {
				return createErr
			}
			return i.client.Upsert(ctx, i.collection, points)
		}
		return err
	}
	return nil
}

func (i *CompactMemoryQdrantIndex) Delete(ctx context.Context, docIDs []string) error {
	if i == nil || i.client == nil || len(docIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(docIDs))
	for _, docID := range docIDs {
		docID = strings.TrimSpace(docID)
		if docID == "" {
			continue
		}
		ids = append(ids, qdrantPointID("compact:"+docID))
	}
	if len(ids) == 0 {
		return nil
	}
	return i.client.Delete(ctx, i.collection, ids)
}

func (i *CompactMemoryQdrantIndex) Search(ctx context.Context, query string, filter memoryindex.Filter) ([]memoryindex.Document, error) {
	if i == nil || i.client == nil {
		return nil, fmt.Errorf("compact qdrant index unavailable")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 8
	}
	vector, err := i.embedFn(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding compact qdrant query: %w", err)
	}
	if len(vector) == 0 {
		return nil, fmt.Errorf("embedding compact qdrant query returned empty vector")
	}
	points, err := i.client.Search(ctx, i.collection, vector, limit*3, true)
	if err != nil {
		return nil, err
	}
	kinds := map[string]bool{}
	for _, kind := range filter.Kinds {
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind != "" {
			kinds[kind] = true
		}
	}
	out := make([]memoryindex.Document, 0, len(points))
	for _, point := range points {
		doc := compactDocumentFromPayload(point.Payload)
		doc.Score = float64(point.Score)
		if len(kinds) > 0 && !kinds[strings.ToLower(doc.Kind)] {
			continue
		}
		if filter.ChatID > 0 && doc.Kind == memoryindex.KindArchive && doc.ChatID != filter.ChatID {
			continue
		}
		out = append(out, doc)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (i *CompactMemoryQdrantIndex) pointsForDocuments(ctx context.Context, docs []memoryindex.Document) ([]qdrant.Point, int, error) {
	type pendingDoc struct {
		doc     memoryindex.Document
		content string
	}
	pending := make([]pendingDoc, 0, len(docs))
	for _, doc := range docs {
		if strings.TrimSpace(doc.ID) == "" || strings.TrimSpace(doc.Body) == "" {
			continue
		}
		content := compactDocumentContent(doc)
		pending = append(pending, pendingDoc{doc: doc, content: content})
	}
	if len(pending) == 0 {
		return nil, 0, fmt.Errorf("no compact memory documents to index")
	}
	texts := make([]string, len(pending))
	for idx, item := range pending {
		texts[idx] = item.content
	}
	vectors, err := i.embedDocuments(ctx, texts)
	if err != nil {
		return nil, 0, err
	}
	if len(vectors) != len(pending) {
		return nil, 0, fmt.Errorf("embedding compact memory returned %d vectors for %d documents", len(vectors), len(pending))
	}
	points := make([]qdrant.Point, 0, len(pending))
	vectorSize := 0
	for idx, item := range pending {
		vector := vectors[idx]
		if len(vector) == 0 {
			return nil, 0, fmt.Errorf("embedding compact memory %s returned empty vector", item.doc.ID)
		}
		if vectorSize == 0 {
			vectorSize = len(vector)
		} else if len(vector) != vectorSize {
			return nil, 0, fmt.Errorf("embedding compact memory %s returned vector size %d, want %d", item.doc.ID, len(vector), vectorSize)
		}
		points = append(points, qdrant.Point{
			ID:      qdrantPointID("compact:" + item.doc.ID),
			Vector:  vector,
			Payload: compactPayload(item.doc, item.content),
		})
	}
	return points, vectorSize, nil
}

func (i *CompactMemoryQdrantIndex) embedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if i.batchEmbedFn != nil {
		vectors, err := i.batchEmbedFn(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("batch embedding compact memory: %w", err)
		}
		return vectors, nil
	}
	vectors := make([][]float32, 0, len(texts))
	for idx, text := range texts {
		vector, err := i.embedFn(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding compact memory index %d: %w", idx, err)
		}
		vectors = append(vectors, vector)
	}
	return vectors, nil
}

func compactPayload(doc memoryindex.Document, content string) map[string]string {
	return map[string]string{
		"doc_id":          doc.ID,
		"kind":            doc.Kind,
		"title":           doc.Title,
		"content":         content,
		"body":            doc.Body,
		"handle":          doc.Handle,
		"source_id":       doc.SourceID,
		"page":            strconv.Itoa(doc.Page),
		"chat_id":         strconv.FormatInt(doc.ChatID, 10),
		"conversation_id": strconv.FormatInt(doc.ConversationID, 10),
		"proposal_id":     strconv.FormatInt(doc.ProposalID, 10),
		"status":          doc.Status,
		"entities":        strings.Join(doc.Entities, " "),
		"tags":            strings.Join(doc.Tags, " "),
		"updated_at":      doc.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

func compactDocumentContent(doc memoryindex.Document) string {
	parts := []string{doc.Kind, doc.Title, doc.Body, doc.Handle, doc.SourceID, doc.Status}
	if len(doc.Entities) > 0 {
		parts = append(parts, strings.Join(doc.Entities, " "))
	}
	if len(doc.Tags) > 0 {
		parts = append(parts, strings.Join(doc.Tags, " "))
	}
	return strings.Join(nonEmptyStrings(parts), "\n")
}

func compactDocumentFromPayload(payload map[string]string) memoryindex.Document {
	page, _ := strconv.Atoi(payload["page"])
	chatID, _ := strconv.ParseInt(payload["chat_id"], 10, 64)
	conversationID, _ := strconv.ParseInt(payload["conversation_id"], 10, 64)
	proposalID, _ := strconv.ParseInt(payload["proposal_id"], 10, 64)
	updatedAt, _ := time.Parse(time.RFC3339Nano, payload["updated_at"])
	body := strings.TrimSpace(payload["body"])
	if body == "" {
		body = payload["content"]
	}
	return memoryindex.Document{
		ID:             payload["doc_id"],
		Kind:           payload["kind"],
		Title:          payload["title"],
		Body:           body,
		Handle:         payload["handle"],
		SourceID:       payload["source_id"],
		Page:           page,
		ChatID:         chatID,
		ConversationID: conversationID,
		ProposalID:     proposalID,
		Status:         payload["status"],
		Entities:       strings.Fields(payload["entities"]),
		Tags:           strings.Fields(payload["tags"]),
		UpdatedAt:      updatedAt,
	}
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// isNotFoundErr checks if an error indicates a missing resource (404).
// Used to detect missing Qdrant collections during upsert.
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}
