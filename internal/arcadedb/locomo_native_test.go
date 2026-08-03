//go:build arcadedb_integration

// The two production retrieval legs fused by the database instead of by us.
//
// Everything measured up to here ran the retrieval in Go: a BM25 index built in
// process, brute-force cosine over vectors pulled into the client, and an
// additive fusion whose weights I chose. ArcadeDB performs both active legs
// natively using reciprocal-rank fusion, which does not depend on score scale.
//
// vector.fuse is reciprocal-rank fusion, and RRF does not care about scale or
// redundancy: a record's contribution is 1/(60+rank) per list it appears in.
// Verified on the binary, since the docs only show it for vector results:
// it is N-ary, it accepts ANY record lists (two plain SELECTs fuse fine), and
// with two lists a record present in both scores 2/61 against 1/61 for one.
//
// The two legs become two sub-selects of one statement:
//
//	dense    vector.neighbors over an LSM_VECTOR HNSW index
//	lexical  SEARCH_INDEX over the EnglishAnalyzer full-text index
//
// Run: go test -tags arcadedb_integration -run LocomoNative -v -timeout 60m ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
)

const embedDimensions = 1024

func ensureVectorIndex(t *testing.T, client *Client) {
	t.Helper()
	// ARRAY_OF_FLOATS, not `LIST OF FLOAT`. The tutorial's LIST works when rows
	// are written in SQL, and fails on every Cypher write with "declared as LIST
	// of FLOAT but a value of type ARRAY_OF_FLOATS is used" -- the two languages
	// bind a JSON array to different types, and only one of them matches. The
	// vector index takes either.
	// There is no DROP INDEX IF EXISTS in ArcadeDB, so changing the property type
	// is a manual `DROP PROPERTY … FORCE`, which cascade-drops the index with it.
	for _, ddl := range []string{
		"CREATE PROPERTY " + locomoEnglishType + ".embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS ON %s (embedding) LSM_VECTOR "+
			"METADATA { dimensions: %d, similarity: 'COSINE' }",
			locomoEnglishType, embedDimensions),
	} {
		if _, err := client.Command(context.Background(), ddl, nil); err != nil {
			t.Fatalf("ddl %q: %v", ddl, err)
		}
	}
}

const writeEmbeddingsBatch = `
UNWIND $rows AS row
MATCH (t:LocomoTurn {id: row.id})
SET t.embedding = row.embedding
RETURN count(t) AS written
`

func TestLocomoNativeIndexesEmbeddings(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)
	ensureVectorIndex(t, client)

	embedder := &documents.EmbeddingClient{
		BaseURL:    envOr("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
		Model:      envOr("AURA_EMBED_MODEL", "qwen3-embed"),
		Dimensions: embedDimensions,
	}
	if _, err := embedder.Embed(ctx, []string{"probe"}); err != nil {
		t.Skipf("embedding sidecar unavailable: %v", err)
	}

	started := time.Now()
	written := 0
	for index := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		rows, err := client.Query(ctx,
			"SELECT id, text FROM "+locomoEnglishType+
				" WHERE conversation = :c LIMIT 5000", map[string]any{"c": conversation})
		if err != nil {
			t.Fatalf("read %s: %v", conversation, err)
		}
		if len(rows) == 0 {
			t.Skip("LOCOMO turns not loaded; run TestLocomoLoadsEveryConversation first")
		}
		ids, texts := make([]string, 0, len(rows)), make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, rowString(row, "id"))
			texts = append(texts, rowString(row, "text"))
		}
		vectors := embedAll(t, embedder, texts)
		payload := make([]any, 0, len(vectors))
		for i, vector := range vectors {
			payload = append(payload, map[string]any{"id": ids[i], "embedding": vector})
		}
		// Small batches: each row carries 1024 floats, so a 400-row batch is a
		// multi-megabyte statement.
		for start := 0; start < len(payload); start += 50 {
			end := min(start+50, len(payload))
			if _, err := client.Write(ctx, writeEmbeddingsBatch,
				map[string]any{"rows": payload[start:end]}); err != nil {
				t.Fatalf("write embeddings %d-%d: %v", start, end, err)
			}
		}
		written += len(payload)
	}
	t.Logf("indexed %d embeddings in %s", written, time.Since(started).Round(time.Second))
}

// nativeFusedQuery is the whole retrieval, in one statement.
//
// The dense leg over-fetches globally and narrows afterwards because a
// LSM_VECTOR index spans the type, not a conversation, and `dia_id` is unique
// only within one: D1:3 exists in all ten. The lexical leg carries its own
// conversation predicate.
const nativeFusedQuery = "SELECT dia_id, score FROM (SELECT expand(`vector.fuse`(" +
	// dense
	"(SELECT FROM (SELECT expand(`vector.neighbors`('LocomoTurn[embedding]', :qv, 600)))" +
	" WHERE conversation = :c LIMIT 60), " +
	// lexical
	"(SELECT FROM LocomoTurn WHERE SEARCH_INDEX('LocomoTurn[text]', :q)" +
	" AND conversation = :c LIMIT 60), " +
	"{ fusion: 'RRF' }))) WHERE conversation = :c LIMIT 10"

// nativeUnscopedQuery drops the `conversation = :c` predicate that every other
// measurement today carried.
//
// That predicate is an artefact of the benchmark, not of the task: a question
// arriving in production does not say which of ten conversations it belongs to,
// so the real search is over the whole corpus. Scoping to ~600 turns made the
// retrieval easier than it is AND handed the in-process Go implementation a
// corpus small enough to brute-force, which is the size at which an HNSW index
// has nothing to save. Both the recall and the latency comparison need this
// query to mean anything about production.
//
// Turns are matched on the global id, since dia_id repeats across conversations.
const nativeUnscopedQuery = "SELECT id, score FROM (SELECT expand(`vector.fuse`(" +
	"(SELECT expand(`vector.neighbors`('LocomoTurn[embedding]', :qv, 60))), " +
	"(SELECT FROM LocomoTurn WHERE SEARCH_INDEX('LocomoTurn[text]', :q) LIMIT 60), " +
	"{ fusion: 'RRF' }))) LIMIT 10"

func TestLocomoNativeFusedRecall(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)

	if rows, err := client.Query(ctx,
		"SELECT count(*) AS n FROM "+locomoEnglishType+" WHERE embedding IS NOT NULL", nil); err != nil ||
		len(rows) == 0 || rowInt(rows[0], "n") == 0 {
		t.Skip("embeddings not indexed; run TestLocomoNativeIndexesEmbeddings first")
	}

	embedder := &documents.EmbeddingClient{
		BaseURL:    envOr("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
		Model:      envOr("AURA_EMBED_MODEL", "qwen3-embed"),
		Dimensions: embedDimensions,
	}
	// AURA_LOCOMO_MAX_QUESTIONS smoke-tests the pipeline before a full run. A
	// 1536-question pass costs four minutes; the last two defects here -- a graph
	// leg traversing the edge backwards and a conversation filter applied to the
	// entity instead of its turns -- were both visible in the first ten.
	limit := 0
	if raw := envOr("AURA_LOCOMO_MAX_QUESTIONS", ""); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	// Question vectors are batched up front. One embed call per question is a
	// round trip per question for work the sidecar does far better in bulk: the
	// turns were embedded 64 at a time and cost 20 ms each, while a lone call
	// costs 11 ms of latency for one vector. Batching moves the whole embedding
	// cost out of the measured query loop, so what the loop times is retrieval.
	type pending struct {
		conversation string
		question     string
		evidence     []string
	}
	queue := []pending{}
	for index, sample := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		for _, qa := range sample.QA {
			if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
				continue
			}
			if limit > 0 && len(queue) >= limit {
				break
			}
			queue = append(queue, pending{conversation, qa.Question, qa.Evidence})
		}
	}
	questionTexts := make([]string, 0, len(queue))
	for _, item := range queue {
		questionTexts = append(questionTexts, item.question)
	}
	embedStart := time.Now()
	questionVectors := embedAll(t, embedder, questionTexts)
	t.Logf("embedded %d questions in %s (%.1f ms each, batched)", len(queue),
		time.Since(embedStart).Round(time.Millisecond),
		float64(time.Since(embedStart).Milliseconds())/float64(max(len(queue), 1)))

	scoped, unscoped := &recallScore{}, &recallScore{}
	asked := 0
	started := time.Now()
	for i, item := range queue {
		asked++
		params := map[string]any{
			"qv": questionVectors[i], "q": escapeLucene(item.question),
			"c": item.conversation,
		}
		scoped.add(nativeRank(t, client, nativeFusedQuery, params, item.evidence))

		// Unscoped: the evidence must be found among all 5882 turns, and the hit
		// is checked on the global id so a D1:3 from another conversation cannot
		// be mistaken for the right one.
		global := make([]string, 0, len(item.evidence))
		for _, diaID := range item.evidence {
			global = append(global, item.conversation+"#"+diaID)
		}
		unscoped.add(nativeRankBy(t, client, nativeUnscopedQuery, params, "id", global))
	}

	t.Logf("%d questions in %s (%.0f ms each, scoped and unscoped queries + embedding)", asked,
		time.Since(started).Round(time.Second),
		float64(time.Since(started).Milliseconds())/float64(max(asked, 1)))
	t.Logf("%-34s %8s %8s %8s", "native RRF fusion", "R@1", "R@5", "R@10")
	for _, row := range []struct {
		name  string
		score *recallScore
	}{
		{"scoped: dense + lexical", scoped},
		{"UNSCOPED: all 5882 turns", unscoped},
	} {
		t.Logf("%-34s %7.1f%% %7.1f%% %7.1f%%", row.name,
			pct(row.score.hit1, row.score.asked), pct(row.score.hit5, row.score.asked),
			pct(row.score.hit10, row.score.asked))
	}
	t.Logf("hand-rolled Go for comparison: additive dense+BM25 74.8")
}

func nativeRank(
	t *testing.T,
	client *Client,
	statement string,
	params map[string]any,
	evidence []string,
) int {
	return nativeRankBy(t, client, statement, params, "dia_id", evidence)
}

func nativeRankBy(
	t *testing.T,
	client *Client,
	statement string,
	params map[string]any,
	field string,
	evidence []string,
) int {
	t.Helper()
	rows, err := client.Query(context.Background(), statement, params)
	if err != nil {
		// A query whose text tokenises to nothing is a miss, not a failure.
		if strings.Contains(err.Error(), "SEARCH_INDEX") {
			return -1
		}
		t.Fatalf("fused query: %v", err)
	}
	for position, row := range rows {
		if slices.Contains(evidence, rowString(row, field)) {
			return position
		}
	}
	return -1
}
