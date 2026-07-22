package neostore

import "fmt"

// LearningNodeSpec describes one fixed Aura-owned learning label and its fields.
type LearningNodeSpec struct {
	Label        string
	SeedLabel    string
	BucketField  string
	ContentField string
	ValueField   string
	Store        string
}

// LearnedLoadQuery builds the common bounded, deterministic Cypher read contract.
func LearnedLoadQuery(spec LearningNodeSpec) string {
	return fmt.Sprintf(`MATCH (candidate:%[1]s)
WHERE candidate.source = 'learned'
  AND candidate.embedding IS NOT NULL
  AND candidate.updated_at >= datetime($cutoff)
WITH DISTINCT candidate.%[2]s AS bucket ORDER BY bucket LIMIT $store_limit
CALL {
  WITH bucket
  MATCH (e:%[1]s {source: 'learned', %[2]s: bucket})
  WHERE e.embedding IS NOT NULL AND e.updated_at >= datetime($cutoff)
  RETURN e
  ORDER BY e.updated_at DESC, e.hash ASC
  LIMIT $bucket_limit
}
RETURN e.%[3]s AS label, apoc.convert.toJson(e.embedding) AS embedding,
       e.%[4]s AS content, e.hash AS hash, e.created_at AS created_at,
       e.updated_at AS updated_at, e.quality AS quality, e.novelty AS novelty
ORDER BY e.updated_at DESC, e.hash ASC
LIMIT $store_limit`, spec.Label, spec.BucketField, spec.ValueField, spec.ContentField)
}

// LearnedSaveQuery builds one transactionally conditional MERGE with typed status.
func LearnedSaveQuery(spec LearningNodeSpec) string {
	return fmt.Sprintf(`UNWIND $rows AS row
OPTIONAL MATCH (existing:%[1]s {hash: row.hash})
MERGE (capacity:AuraLearningCapacity {store: row.store})
SET capacity.touched_at = datetime(row.updated_at), capacity.policy_version = row.policy_version
WITH row, existing, capacity
CALL {
  WITH row
  MATCH (bucket_node:%[1]s {source: 'learned', %[2]s: row.bucket})
  RETURN count(bucket_node) AS bucket_count
}
CALL {
  WITH row
  MATCH (store_node:%[1]s {source: 'learned'})
  RETURN count(store_node) AS store_count
}
FOREACH (_ IN CASE WHEN existing IS NOT NULL THEN [1] ELSE [] END |
  SET existing.%[3]s = row.label, existing.embedding = row.embedding,
      existing.%[4]s = row.content, existing.bucket = row.bucket,
      existing.%[2]s = row.bucket, existing.updated_at = datetime(row.updated_at),
      existing.quality = row.quality, existing.novelty = row.novelty,
      existing.source = 'learned', existing.policy_version = row.policy_version)
FOREACH (_ IN CASE WHEN existing IS NULL AND bucket_count < row.bucket_cap AND store_count < row.store_cap THEN [1] ELSE [] END |
  MERGE (created:%[1]s {hash: row.hash})
  ON CREATE SET created.created_at = datetime(row.created_at)
  SET created.%[3]s = row.label, created.embedding = row.embedding,
      created.%[4]s = row.content, created.bucket = row.bucket,
      created.%[2]s = row.bucket, created.updated_at = datetime(row.updated_at),
      created.quality = row.quality, created.novelty = row.novelty,
      created.source = 'learned', created.policy_version = row.policy_version)
RETURN CASE
  WHEN existing IS NOT NULL THEN 'updated'
  WHEN bucket_count < row.bucket_cap AND store_count < row.store_cap THEN 'created'
  ELSE 'at_capacity'
END AS status, existing.created_at AS existing_created_at`, spec.Label, spec.BucketField, spec.ValueField, spec.ContentField)
}

// PinnedSaveQuery builds the separate, cap-exempt manual-seed MERGE contract.
func PinnedSaveQuery(spec LearningNodeSpec) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MERGE (seed:%[1]s {hash: row.hash})
ON CREATE SET seed.created_at = datetime(row.created_at)
SET seed.%[2]s = row.label, seed.embedding = row.embedding,
    seed.%[3]s = row.content, seed.updated_at = datetime(row.updated_at),
    seed.source = 'manual'`, spec.SeedLabel, spec.ValueField, spec.ContentField)
}

// PinnedLoadQuery builds the separate deterministic manual-seed read contract.
func PinnedLoadQuery(spec LearningNodeSpec) string {
	return fmt.Sprintf(`MATCH (seed:%[1]s {source: 'manual'})
WHERE seed.embedding IS NOT NULL
RETURN seed.%[2]s AS label, apoc.convert.toJson(seed.embedding) AS embedding,
       seed.%[3]s AS content
ORDER BY seed.updated_at DESC, seed.hash ASC
LIMIT $limit`, spec.SeedLabel, spec.ValueField, spec.ContentField)
}

// ClampLearningScore keeps poisoned quality/novelty inputs inside [0,1].
func ClampLearningScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
