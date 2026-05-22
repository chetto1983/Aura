package qdrant

// Point represents a vector point to be upserted into Qdrant.
type Point struct {
	ID      string            `json:"id"`
	Vector  []float32         `json:"vector"`
	Payload map[string]string `json:"payload"`
}

// ScoredPoint represents a point returned from a Qdrant search query.
type ScoredPoint struct {
	ID      any               `json:"id"`
	Score   float32           `json:"score"`
	Payload map[string]string `json:"payload"`
}

// ScrollPoint is a point returned from a Qdrant scroll (list-all) request.
type ScrollPoint struct {
	ID      any               `json:"id"`
	Payload map[string]string `json:"payload"`
}

// CollectionInfo holds metadata about a Qdrant collection.
// Sourced from GET /collections/{name} response field "result".
type CollectionInfo struct {
	Status              string `json:"status"`
	PointsCount         uint64 `json:"points_count"`
	IndexedVectorsCount uint64 `json:"indexed_vectors_count"`
	// VectorSize is the dimension of the primary (unnamed) vector space,
	// extracted from result.config.params.vectors.size. Zero when the
	// collection uses named vectors or when the field is absent in the
	// Qdrant response.
	VectorSize int
}
