package learningretention

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/neostore"
)

// GraphSpec is a fixed, Aura-owned learned-node schema. Values are composition
// constants, never user input, because Cypher labels/property names cannot be
// parameterized.
type GraphSpec struct {
	Store, Label, BucketField string
}

// GraphStore exposes bounded candidate/count/delete operations without loading
// embeddings or learned text/query. Manual seed labels are intentionally absent.
type GraphStore struct {
	Client neostore.GraphClient
	Spec   GraphSpec
}

// NewReasoningGraphStore constructs the ReasoningExample retention surface.
func NewReasoningGraphStore(client neostore.GraphClient) *GraphStore {
	return &GraphStore{Client: client, Spec: GraphSpec{Store: "reasoning", Label: "ReasoningExample", BucketField: "tier"}}
}

// NewToolSelectionGraphStore constructs the ToolSelectionExample retention surface.
func NewToolSelectionGraphStore(client neostore.GraphClient) *GraphStore {
	return &GraphStore{Client: client, Spec: GraphSpec{Store: "tool_selection", Label: "ToolSelectionExample", BucketField: "tool"}}
}

// Name returns the finite store class used by deterministic seeds and metrics.
func (s *GraphStore) Name() string { return s.Spec.Store }

// Expired returns one oldest-first page strictly older than cutoff.
func (s *GraphStore) Expired(ctx context.Context, cutoff time.Time, limit int) ([]Candidate, error) {
	query := fmt.Sprintf(`MATCH (e:%s {source: 'learned'})
WHERE e.updated_at < datetime($cutoff)
RETURN e.hash AS hash, e.%s AS bucket, e.updated_at AS updated_at,
       e.quality AS quality, e.novelty AS novelty, e.policy_version AS policy_version
ORDER BY e.updated_at ASC, e.hash ASC
LIMIT $limit`, s.Spec.Label, s.Spec.BucketField)
	return s.readCandidates(ctx, query, map[string]any{"cutoff": cutoff.UTC().Format(time.RFC3339Nano), "limit": limit}, limit)
}

// Buckets returns one bounded, ordered page of learned bucket keys.
func (s *GraphStore) Buckets(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(`MATCH (e:%s {source: 'learned'})
WHERE e.%s IS NOT NULL
RETURN DISTINCT e.%s AS bucket
ORDER BY bucket ASC
LIMIT $limit`, s.Spec.Label, s.Spec.BucketField, s.Spec.BucketField)
	rows, err := s.read(ctx, query, map[string]any{"limit": limit})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, min(len(rows), limit))
	for _, row := range rows {
		if bucket := neostore.AsString(row["bucket"]); bucket != "" && len(out) < limit {
			out = append(out, bucket)
		}
	}
	return out, nil
}

// BucketCount counts learned nodes in one bucket without loading their content.
func (s *GraphStore) BucketCount(ctx context.Context, bucket string) (int, error) {
	query := fmt.Sprintf(`MATCH (e:%s {source: 'learned', %s: $bucket}) RETURN count(e) AS count`, s.Spec.Label, s.Spec.BucketField)
	return s.count(ctx, query, map[string]any{"bucket": bucket})
}

// Candidates returns one newest-first bounded metadata window for a bucket.
func (s *GraphStore) Candidates(ctx context.Context, bucket string, limit int) ([]Candidate, error) {
	query := fmt.Sprintf(`MATCH (e:%s {source: 'learned', %s: $bucket})
RETURN e.hash AS hash, e.%s AS bucket, e.updated_at AS updated_at,
       e.quality AS quality, e.novelty AS novelty, e.policy_version AS policy_version
ORDER BY e.updated_at DESC, e.hash ASC
LIMIT $limit`, s.Spec.Label, s.Spec.BucketField, s.Spec.BucketField)
	return s.readCandidates(ctx, query, map[string]any{"bucket": bucket, "limit": limit}, limit)
}

// TotalCount counts all learned nodes in this store, excluding manual seeds.
func (s *GraphStore) TotalCount(ctx context.Context) (int, error) {
	query := fmt.Sprintf(`MATCH (e:%s {source: 'learned'}) RETURN count(e) AS count`, s.Spec.Label)
	return s.count(ctx, query, nil)
}

// GlobalCandidates returns one newest-first bounded store-wide metadata window.
func (s *GraphStore) GlobalCandidates(ctx context.Context, limit int) ([]Candidate, error) {
	query := fmt.Sprintf(`MATCH (e:%s {source: 'learned'})
RETURN e.hash AS hash, e.%s AS bucket, e.updated_at AS updated_at,
       e.quality AS quality, e.novelty AS novelty, e.policy_version AS policy_version
ORDER BY e.updated_at DESC, e.hash ASC
LIMIT $limit`, s.Spec.Label, s.Spec.BucketField)
	return s.readCandidates(ctx, query, map[string]any{"limit": limit}, limit)
}

// Delete removes exact learned hashes only when their timestamp version is unchanged.
func (s *GraphStore) Delete(ctx context.Context, candidates []Candidate) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}
	rows := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, map[string]any{"hash": candidate.Hash, "updated_at": candidate.UpdatedAt.UTC().Format(time.RFC3339Nano)})
	}
	query := fmt.Sprintf(`UNWIND $rows AS row
MATCH (e:%s {source: 'learned', hash: row.hash})
WHERE e.updated_at = datetime(row.updated_at)
WITH e DELETE e
RETURN count(*) AS deleted`, s.Spec.Label)
	result, err := s.Client.Write(ctx, query, map[string]any{"rows": rows})
	if err != nil {
		return 0, err
	}
	if len(result) == 1 {
		if deleted := asInt(result[0]["deleted"]); deleted > 0 {
			return min(deleted, len(candidates)), nil
		}
	}
	// Some mcp-neo4j-cypher write transports acknowledge mutations without
	// returning Cypher records. Verify only this bounded hash page rather than
	// treating a successful delete as zero or reloading the store. A hash that
	// was concurrently updated remains present and correctly counts as retained.
	hashes := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		hashes = append(hashes, candidate.Hash)
	}
	verifyQuery := fmt.Sprintf(`UNWIND $hashes AS hash
OPTIONAL MATCH (e:%s {source: 'learned', hash: hash})
RETURN count(e) AS remaining`, s.Spec.Label)
	remainingRows, err := s.Client.Read(ctx, verifyQuery, map[string]any{"hashes": hashes})
	if err != nil {
		return 0, err
	}
	if len(remainingRows) != 1 {
		return 0, fmt.Errorf("learning retention %s: missing delete verification", s.Name())
	}
	return max(0, len(candidates)-asInt(remainingRows[0]["remaining"])), nil
}

func (s *GraphStore) readCandidates(ctx context.Context, query string, params map[string]any, limit int) ([]Candidate, error) {
	rows, err := s.read(ctx, query, params)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, min(len(rows), limit))
	for _, row := range rows {
		if len(out) == limit {
			break
		}
		hash := neostore.AsString(row["hash"])
		updatedAt, ok := asTime(row["updated_at"])
		if hash == "" || !ok {
			continue
		}
		out = append(out, Candidate{Hash: hash, Bucket: neostore.AsString(row["bucket"]), UpdatedAt: updatedAt,
			Quality: asFloat(row["quality"]), Novelty: asFloat(row["novelty"]), PolicyVersion: neostore.AsString(row["policy_version"])})
	}
	return out, nil
}

func (s *GraphStore) count(ctx context.Context, query string, params map[string]any) (int, error) {
	rows, err := s.read(ctx, query, params)
	if err != nil {
		return 0, err
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("learning retention %s: missing count", s.Name())
	}
	return asInt(rows[0]["count"]), nil
}

func (s *GraphStore) read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	if s == nil || s.Client == nil {
		return nil, fmt.Errorf("learning retention graph client is nil")
	}
	return s.Client.Read(ctx, query, params)
}

func asTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), true
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		return parsed.UTC(), err == nil
	default:
		return time.Time{}, false
	}
}

func asFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	default:
		return 0
	}
}

func asInt(value any) int { return int(asFloat(value)) }
