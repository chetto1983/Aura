package documents

import (
	"context"
	"fmt"
	"math"
)

const documentCorpusRevisionReadQuery = `
MATCH (revision:DocumentCorpusRevision {singleton: 'corpus'})
RETURN revision.epoch AS epoch
`

// ReadDocumentCorpusEpoch returns the positive, service-owned revision used to
// bracket document retrieval. Missing or malformed state fails closed because a
// fabricated/default epoch would make an adaptive delivery irreproducible.
func ReadDocumentCorpusEpoch(
	ctx context.Context,
	client KnowledgeClient,
) (uint64, error) {
	if client == nil {
		return 0, fmt.Errorf("documents: corpus revision client is unavailable")
	}
	rows, err := client.Read(ctx, documentCorpusRevisionReadQuery, nil)
	if err != nil {
		return 0, fmt.Errorf("documents: read corpus revision: %w", err)
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("documents: corpus revision singleton is unavailable")
	}
	epoch, ok := positiveCorpusEpoch(rows[0]["epoch"])
	if !ok {
		return 0, fmt.Errorf("documents: corpus revision is invalid")
	}
	return epoch, nil
}

func positiveCorpusEpoch(value any) (uint64, bool) {
	switch epoch := value.(type) {
	case int:
		return positiveSignedEpoch(int64(epoch))
	case int32:
		return positiveSignedEpoch(int64(epoch))
	case int64:
		return positiveSignedEpoch(epoch)
	case uint:
		return positiveUnsignedEpoch(uint64(epoch))
	case uint32:
		return positiveUnsignedEpoch(uint64(epoch))
	case uint64:
		return positiveUnsignedEpoch(epoch)
	case float64:
		if epoch <= 0 || math.Trunc(epoch) != epoch || epoch > math.MaxUint64 {
			return 0, false
		}
		return uint64(epoch), true
	default:
		return 0, false
	}
}

func positiveSignedEpoch(epoch int64) (uint64, bool) {
	if epoch <= 0 {
		return 0, false
	}
	return uint64(epoch), true
}

func positiveUnsignedEpoch(epoch uint64) (uint64, bool) {
	return epoch, epoch > 0
}
