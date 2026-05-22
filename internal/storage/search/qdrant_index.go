package search

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/storage/qdrant"
)

func (r *qdrantRepository) Index(ctx context.Context, id string, content string, metadata map[string]string) error {
	vector, err := r.embedFn(ctx, content)
	if err != nil {
		return fmt.Errorf("embedding %s: %w", id, err)
	}
	if len(vector) == 0 {
		return fmt.Errorf("embedding %s returned empty vector", id)
	}
	// WR-07: skip CreateCollection if the collection has already been
	// initialized in this process (either by a previous Index call or by
	// IndexWikiPages). Qdrant's CreateCollection is idempotent at the HTTP
	// level (200 if collection exists with same params), so the previous
	// behavior was correct but added a network round-trip per single-doc
	// upsert. The r.indexed flag persists for the process lifetime; on a
	// fresh process we still hit the create path on the first Index call.
	r.mu.RLock()
	alreadyIndexed := r.indexed
	r.mu.RUnlock()
	if !alreadyIndexed {
		if err := r.client.CreateCollection(ctx, r.collectionQdrant, len(vector)); err != nil {
			// WR-07: surface a clearer hint when CreateCollection fails with
			// a dimension mismatch (most likely cause: operator swapped
			// EMBEDDING_MODEL without rebuilding the collection — see T-01-24).
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "dim") || strings.Contains(lower, "vector size") {
				r.logger.Warn("qdrant CreateCollection failed with dimension hint; embedding model may have changed since last rebuild",
					"collection", r.collectionQdrant, "new_vector_size", len(vector), "error", err)
			}
			return err
		}
		r.mu.Lock()
		r.indexed = true
		r.mu.Unlock()
	}
	payload := map[string]string{
		"doc_id":  id,
		"content": content,
	}
	for key, value := range metadata {
		payload[key] = value
	}
	if err := r.client.Upsert(ctx, r.collectionQdrant, []qdrant.Point{{
		ID:      qdrantPointID(id),
		Vector:  vector,
		Payload: payload,
	}}); err != nil {
		return err
	}
	return nil
}

func (r *qdrantRepository) IndexWikiPages(ctx context.Context) error {
	_, err := rebuildQdrantWikiDocumentsWithClient(ctx, r.wikiDir, r.embedFn, r.client, r.collectionQdrant, r.expectedDim, r.skipDimRebuild, r.logger)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.indexed = true
	r.mu.Unlock()
	return nil
}

// RebuildWikiIndex triggers a full Qdrant collection rebuild. When dryRun is
// true it queries the current state and returns a report without modifying
// anything. Implements search.WikiIndexRebuilder.
func (r *qdrantRepository) RebuildWikiIndex(ctx context.Context, dryRun bool) (QdrantRebuildReport, error) {
	if dryRun {
		info, err := r.client.CollectionInfo(ctx, r.collectionQdrant)
		if err != nil {
			return QdrantRebuildReport{Collection: r.collectionQdrant}, err
		}
		_, pages, _ := loadWikiDocuments(r.wikiDir, r.logger)
		cur := saturateUint64ToInt(info.PointsCount)
		return QdrantRebuildReport{
			Collection:      r.collectionQdrant,
			DocsIndexed:     cur,
			PagesIndexed:    pages,
			VectorSize:      info.VectorSize,
			PriorVectorSize: info.VectorSize,
		}, nil
	}
	report, err := rebuildQdrantWikiDocumentsWithClient(ctx, r.wikiDir, r.embedFn, r.client, r.collectionQdrant, r.expectedDim, r.skipDimRebuild, r.logger)
	if err != nil {
		return report, err
	}
	r.mu.Lock()
	r.indexed = true
	r.mu.Unlock()
	return report, nil
}

func (r *qdrantRepository) ReindexWikiPage(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	doc, found, err := loadWikiPageDocument(r.wikiDir, slug, r.logger)
	if err != nil {
		return err
	}
	if !found {
		return r.client.Delete(ctx, r.collectionQdrant, []string{
			qdrantPointID(slug),
			qdrantPointID("graph:node:" + slug),
		})
	}
	return r.Index(ctx, doc.ID, doc.Content, doc.Metadata)
}

// RebuildQdrantWikiDocuments recreates the configured collection from Aura's
// wiki pages and graph cards.
func RebuildQdrantWikiDocuments(ctx context.Context, wikiDir string, embedFn EmbeddingFunc, cfg QdrantConfig, logger *slog.Logger) (QdrantRebuildReport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if embedFn == nil {
		return QdrantRebuildReport{}, fmt.Errorf("embedding function is required")
	}
	client, collection, err := newQdrantClientFromConfig(cfg)
	if err != nil {
		return QdrantRebuildReport{}, err
	}
	return rebuildQdrantWikiDocumentsWithClient(ctx, wikiDir, embedFn, client, collection, cfg.OutputDim, cfg.SkipDimMismatchRebuild, logger)
}

// rebuildQdrantWikiDocumentsWithClient implements the rebuild logic using an
// already-constructed qdrant.Client and collection name. expectedDim, when > 0,
// enables boot-time dim-mismatch detection: if the existing collection's vector
// size differs from expectedDim the warm-cache is bypassed and a full rebuild
// runs (unless skipDimRebuild is true, which skips it with a warning instead).
func rebuildQdrantWikiDocumentsWithClient(ctx context.Context, wikiDir string, embedFn EmbeddingFunc, client qdrant.Client, collection string, expectedDim int, skipDimRebuild bool, logger *slog.Logger) (QdrantRebuildReport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if embedFn == nil {
		return QdrantRebuildReport{}, fmt.Errorf("embedding function is required")
	}
	if err := client.Health(ctx); err != nil {
		return QdrantRebuildReport{}, err
	}

	// QDRANT-01 warm-cache short-circuit: if the collection already exists with
	// points, skip the rebuild and reuse the cached vectors — UNLESS a
	// dim-mismatch is detected (US-WIKI-FIX-04).
	info, infoErr := client.CollectionInfo(ctx, collection)
	priorVectorSize := 0
	if infoErr != nil {
		// Defensive fallback: a transient probe failure must not block startup.
		// Continue with the full rebuild path -- correctness > performance.
		logger.Warn("qdrant collection info probe failed; proceeding with full rebuild", "collection", collection, "error", infoErr)
	} else {
		priorVectorSize = info.VectorSize
	}

	if infoErr == nil && info.PointsCount > 0 {
		// Dim-mismatch detection: compare existing collection dim against configured target.
		if expectedDim > 0 && info.VectorSize > 0 && info.VectorSize != expectedDim {
			if skipDimRebuild {
				logger.Warn("qdrant collection dim check: mismatch, skipped by env",
					"collection", collection, "current", info.VectorSize, "expected", expectedDim, "action", "skipped_by_env")
				docsIndexed := saturateUint64ToInt(info.PointsCount)
				_, pages, loadErr := loadWikiDocuments(wikiDir, logger)
				pagesIndexed := pages
				if loadErr != nil {
					pagesIndexed = PagesIndexedUnknown
				}
				return QdrantRebuildReport{
					Collection:      collection,
					PagesIndexed:    pagesIndexed,
					DocsIndexed:     docsIndexed,
					VectorSize:      info.VectorSize,
					PriorVectorSize: priorVectorSize,
				}, nil
			}
			// Fall through to full rebuild — dim mismatch, drop and recreate.
			logger.Warn("qdrant collection dim check: mismatch, triggering auto-rebuild",
				"collection", collection, "current", info.VectorSize, "expected", expectedDim, "action", "rebuild")
		} else {
			// Warm-cache hit: collection exists and dim is correct (or unknown).
			if expectedDim > 0 && info.VectorSize > 0 {
				logger.Info("qdrant collection dim check",
					"collection", collection, "current", info.VectorSize, "expected", expectedDim, "action", "match")
			}
			// Still load pages so PagesIndexed is meaningful in the report.
			// W2: surface the loadWikiDocuments error at warn level instead of swallowing it.
			_, pages, loadErr := loadWikiDocuments(wikiDir, logger)
			// WR-05: distinguish "load failed → count unknown" from "load succeeded →
			// 0 pages on disk". Use PagesIndexedUnknown (-1) as the sentinel so
			// downstream consumers (e.g. /api/health, debug_qdrant) do not silently
			// report 0 pages when the enumeration failed.
			pagesIndexed := pages
			if loadErr != nil {
				logger.Warn("warm-cache hit: pages_on_disk count unavailable", "error", loadErr, "collection", collection)
				pagesIndexed = PagesIndexedUnknown
			}
			// WR-01: saturate uint64 → int conversion. On 32-bit platforms the
			// naked int(info.PointsCount) would wrap for values above MaxInt32.
			// Aura is unlikely to ever index >2 billion points but the guard
			// keeps the report sane on every architecture.
			docsIndexed := saturateUint64ToInt(info.PointsCount)
			if loadErr != nil {
				logger.Info("qdrant warm-cache hit (pages_on_disk unavailable)", "collection", collection, "points_count", info.PointsCount)
			} else {
				logger.Info("qdrant warm-cache hit, skipping rebuild", "collection", collection, "points_count", info.PointsCount, "pages_on_disk", pages)
			}
			return QdrantRebuildReport{
				Collection:      collection,
				PagesIndexed:    pagesIndexed,
				DocsIndexed:     docsIndexed, // W1: live points count, not 0
				VectorSize:      0,
				PriorVectorSize: priorVectorSize,
			}, nil
		}
	}

	docs, pages, err := loadWikiDocuments(wikiDir, logger)
	if err != nil {
		return QdrantRebuildReport{}, err
	}
	if len(docs) == 0 {
		return QdrantRebuildReport{Collection: collection, PagesIndexed: pages}, nil
	}

	points := make([]qdrant.Point, 0, len(docs))
	var skipped []SkippedDoc
	vectorSize := 0
	for _, doc := range docs {
		vector, err := embedFn(ctx, doc.Content)
		if err != nil {
			logger.Warn("rebuild: skipping doc", "id", doc.ID, "error", err)
			skipped = append(skipped, SkippedDoc{DocID: doc.ID, Reason: err.Error()})
			continue
		}
		if len(vector) == 0 {
			skipErr := fmt.Errorf("embedding returned empty vector")
			logger.Warn("rebuild: skipping doc", "id", doc.ID, "error", skipErr)
			skipped = append(skipped, SkippedDoc{DocID: doc.ID, Reason: skipErr.Error()})
			continue
		}
		if vectorSize == 0 {
			vectorSize = len(vector)
		} else if len(vector) != vectorSize {
			return QdrantRebuildReport{}, fmt.Errorf("embedding %s returned vector size %d, want %d", doc.ID, len(vector), vectorSize)
		}
		payload := map[string]string{
			"doc_id":  doc.ID,
			"content": doc.Content,
		}
		for key, value := range doc.Metadata {
			payload[key] = value
		}
		points = append(points, qdrant.Point{
			ID:      qdrantPointID(doc.ID),
			Vector:  vector,
			Payload: payload,
		})
	}

	if len(points) == 0 {
		return QdrantRebuildReport{
			Collection:      collection,
			PagesIndexed:    pages,
			SkippedDocs:     skipped,
			PriorVectorSize: priorVectorSize,
		}, fmt.Errorf("rebuild: all %d documents failed to embed", len(docs))
	}

	if err := client.DeleteCollection(ctx, collection); err != nil {
		return QdrantRebuildReport{}, err
	}
	if err := client.CreateCollection(ctx, collection, vectorSize); err != nil {
		return QdrantRebuildReport{}, err
	}
	if err := client.Upsert(ctx, collection, points); err != nil {
		return QdrantRebuildReport{}, err
	}
	return QdrantRebuildReport{
		Collection:      collection,
		DocsIndexed:     len(points),
		PagesIndexed:    pages,
		VectorSize:      vectorSize,
		PriorVectorSize: priorVectorSize,
		SkippedDocs:     skipped,
	}, nil
}
