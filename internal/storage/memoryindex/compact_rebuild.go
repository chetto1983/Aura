package memoryindex

import (
	"context"
	"fmt"
)

// CompactRebuildReport is the result of a forced compact memory vector index rebuild.
type CompactRebuildReport struct {
	Collection      string
	PointsIndexed   int
	DocsInSQL       int // documents in compact_memory_documents (SQLite)
	VectorSize      int
	PriorVectorSize int // 0 when collection did not exist before rebuild
}

// forceRebuilder is satisfied by CompactMemoryQdrantIndex, which exposes a
// warm-cache-bypassing rebuild path for POST /api/compact/reindex.
type forceRebuilder interface {
	ForceRebuild(ctx context.Context, docs []Document) (CompactRebuildReport, error)
}

// RebuildCompact forces a full rebuild of the compact Qdrant collection from
// the current SQLite documents, bypassing the warm-cache check.
// Used by POST /api/compact/reindex.
func (s *Store) RebuildCompact(ctx context.Context) (CompactRebuildReport, error) {
	if s == nil || s.db == nil {
		return CompactRebuildReport{}, fmt.Errorf("memoryindex: store unavailable")
	}
	docs, err := s.allDocuments(ctx)
	if err != nil {
		return CompactRebuildReport{}, err
	}
	if s.vector == nil {
		return CompactRebuildReport{DocsInSQL: len(docs)}, nil
	}
	if fr, ok := s.vector.(forceRebuilder); ok {
		report, err := fr.ForceRebuild(ctx, docs)
		if err != nil {
			return CompactRebuildReport{}, err
		}
		report.DocsInSQL = len(docs)
		return report, nil
	}
	// Fallback: use Recreate (warm-cache aware, no prior vector size info).
	report, err := s.vector.Recreate(ctx, docs)
	if err != nil {
		return CompactRebuildReport{}, err
	}
	return CompactRebuildReport{
		Collection:    report.Collection,
		PointsIndexed: report.DocsIndexed,
		DocsInSQL:     len(docs),
		VectorSize:    report.VectorSize,
	}, nil
}
