//go:build neo4j_integration

package learningretention

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
)

// TestLearningCompactorLiveNeo4j proves the exact 10k global boundary against a
// disposable graph. Its dedicated labels isolate the destructive compaction
// probe from product and manual-seed labels even if a developer points at a
// non-empty local Neo4j instance.
func TestLearningCompactorLiveNeo4j(t *testing.T) {
	cfg := config.LoadDB()
	if strings.TrimSpace(cfg.Neo4j.Password) == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("neo4j_integration requires NEO4J_PASSWORD under CI")
		}
		t.Skip("set NEO4J_PASSWORD and start the disposable stack")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	const (
		learnedLabel = "LearningRetentionTest"
		seedLabel    = "LearningRetentionTestSeed"
	)
	cleanup := func() {
		_, _ = client.Write(context.Background(),
			"MATCH (n) WHERE n:LearningRetentionTest OR n:LearningRetentionTestSeed DETACH DELETE n", nil)
	}
	cleanup()
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Microsecond)
	rows := make([]map[string]any, 0, 10_002)
	for i := 0; i < 10_002; i++ {
		rows = append(rows, map[string]any{
			"hash": fmt.Sprintf("live-%05d", i), "bucket": []string{"a", "b"}[i%2],
			"updated_at": now.Add(time.Duration(i-10_002) * time.Second).Format(time.RFC3339Nano),
			"quality":    float64(i%10) / 10, "novelty": float64((i*3)%10) / 10,
			"policy_version": "learning-v1",
		})
	}
	for start := 0; start < len(rows); start += 500 {
		end := min(start+500, len(rows))
		_, err = client.Write(ctx, `UNWIND $rows AS row
CREATE (e:LearningRetentionTest {source: 'learned', hash: row.hash, bucket: row.bucket,
 updated_at: datetime(row.updated_at), quality: row.quality, novelty: row.novelty,
 policy_version: row.policy_version})`, map[string]any{"rows": rows[start:end]})
		if err != nil {
			t.Fatalf("seed learned batch %d: %v", start/500, err)
		}
	}
	_, err = client.Write(ctx, `CREATE (:LearningRetentionTest {source: 'learned', hash: 'expired', bucket: 'a',
 updated_at: datetime($updated_at), quality: 1.0, novelty: 1.0, policy_version: 'learning-v1'}),
 (:LearningRetentionTestSeed {source: 'manual', hash: 'pinned'})`, map[string]any{
		"updated_at": now.Add(-91 * 24 * time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}

	store := &GraphStore{Client: client, Spec: GraphSpec{Store: "integration", Label: learnedLabel, BucketField: "bucket"}}
	compactor := &Compactor{Stores: []Store{store}, Config: Config{
		TTL: 90 * 24 * time.Hour, BucketCap: 6_000, StoreCap: 10_000,
		BatchSize: 2, BucketLimit: 8, PolicyVersion: "learning-v1",
	}, Now: func() time.Time { return now }}
	report, err := compactor.CompactBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Expired != 1 || report.Evicted != 2 {
		t.Fatalf("first report = %+v", report)
	}
	if count, err := store.TotalCount(ctx); err != nil || count != 10_000 {
		t.Fatalf("learned total = %d, %v", count, err)
	}
	seedRows, err := client.Read(ctx, fmt.Sprintf("MATCH (s:%s {source: 'manual', hash: 'pinned'}) RETURN count(s) AS count", seedLabel), nil)
	if err != nil || len(seedRows) != 1 || asInt(seedRows[0]["count"]) != 1 {
		t.Fatalf("pinned seed changed: rows=%v err=%v", seedRows, err)
	}
	newestRows, err := client.Read(ctx, "MATCH (e:LearningRetentionTest {hash: 'live-10001'}) RETURN count(e) AS count", nil)
	if err != nil || len(newestRows) != 1 || asInt(newestRows[0]["count"]) != 1 {
		t.Fatalf("newest reservation lost: rows=%v err=%v", newestRows, err)
	}
	second, err := compactor.CompactBatch(ctx)
	if err != nil || second.Expired != 0 || second.Evicted != 0 {
		t.Fatalf("idempotent rerun = %+v, %v", second, err)
	}
}
