//go:build document_ingest_live

package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultCLIIngestFixture is the corpus file this tier ingests when
// AURA_DOC_TEST_INGEST_FILE is unset. It lives outside the repo (real documents are not
// committed), and it is the exact file whose CLI ingest produced 92 graph chunks, zero
// embeddings, zero document_embed jobs — the measurement this test exists to make
// impossible to repeat silently.
const defaultCLIIngestFixture = "/mnt/d/turing_AgentMemory_MCP/test/Costituzione.pdf"

// liveGraphCount reads one named integer out of a Cypher result row. countFromRows cannot
// serve here: it returns the FIRST of chunks/count/embedded it finds, so a row carrying
// both would report the chunk total twice and the assertion would pass on a document with
// no vectors at all.
func liveGraphCount(t *testing.T, rows []map[string]any, key string) int {
	t.Helper()
	if len(rows) == 0 {
		t.Fatalf("cypher returned no rows, want one carrying %q", key)
	}
	value, ok := rows[0][key]
	if !ok {
		t.Fatalf("cypher row %v has no %q", rows[0], key)
	}
	switch n := value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	case float64:
		return int(n)
	default:
		t.Fatalf("cypher %q = %#v, want a number", key, value)
		return 0
	}
}

// purgeLiveDocument removes everything one ingest wrote. The live Postgres and Neo4j hold
// the real deployment's data, so this tier owns exactly the rows and nodes it created and
// gives them back — it never truncates a table or deletes by label.
func purgeLiveDocument(t *testing.T, pool *pgxpool.Pool, graph KnowledgeClient, documentID, sourceID, embedJobKey string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := graph.Write(ctx,
		"MATCH (d:Document {id:$id}) OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c:Chunk) DETACH DELETE c, d",
		map[string]any{"id": documentID}); err != nil {
		t.Errorf("purge graph document %s: %v", documentID, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.document_ingest_jobs WHERE source_id = $1", sourceID); err != nil {
		t.Errorf("purge ingest ledger for %s: %v", sourceID, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.ingestion_jobs WHERE idempotency_key = $1", embedJobKey); err != nil {
		t.Errorf("purge embed job %s: %v", embedJobKey, err)
	}
}

// claimEmbedJobPayload asserts the document_embed job the ingest enqueued exists, and
// takes it off the queue before draining it. Taking it matters: a running `aura serve`
// polls aura.ingestion_jobs every second with no job_type filter, so a row left queued
// would be embedded twice — once by this test and once by production. The delete is how
// the test owns the drain instead of racing the daemon for it.
func claimEmbedJobPayload(t *testing.T, pool *pgxpool.Pool, key string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var id, status, stage string
	var payloadJSON []byte
	err := pool.QueryRow(ctx, `
SELECT id::text, status, stage, payload
FROM aura.ingestion_jobs
WHERE idempotency_key = $1`, key).Scan(&id, &status, &stage, &payloadJSON)
	if err != nil {
		t.Fatalf("no %s job for %q: %v — the ingest reported success without queueing any vector work",
			IngestionJobTypeDocumentEmbed, key, err)
	}
	if status != "queued" || stage != "embedding" {
		t.Fatalf("embed job %s is status=%q stage=%q, want queued/embedding", id, status, stage)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("embed job payload: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.ingestion_jobs WHERE id = $1", id); err != nil {
		t.Fatalf("claim embed job %s: %v", id, err)
	}
	t.Logf("claimed %s job %s (idempotency_key=%s)", IngestionJobTypeDocumentEmbed, id, key)
	return payload
}

type liveIngestFixture struct {
	cfg   *config.Config
	pool  *pgxpool.Pool
	graph KnowledgeClient
	path  string
}

// TestLiveCLIIngestEmbedsAndNeverReportsReadyWithoutVectors drives the ingest path the
// `aura docs ingest` command composes — durable embed queue included — end to end against
// the live stack.
//
// It pins three things the CLI ingest genuinely owns. (1) The ingest enqueues a durable
// document_embed job; before the composition fix it enqueued nothing, and a 92-chunk
// document with zero vectors reported itself searchable. (2) Draining that job embeds
// every chunk: the graph's embedded-chunk count equals its chunk count, both logged, both
// measured, neither hardcoded. (3) When the embedder is dead, nothing claims otherwise —
// the ingest ledger never reaches "complete" and a catalogued document flips to "failed".
// That last assertion could not fire before: the worker handed the catalog a doc_<hex>
// search id where a uuid was expected, the store rejected it, and the error was swallowed
// into a WARN.
//
// NO-SKIP-AS-GREEN: under $CI a missing fixture is a misconfigured job (t.Fatal), never a
// silent green; locally it skips so a developer without the corpus is not blocked.
func TestLiveCLIIngestEmbedsAndNeverReportsReadyWithoutVectors(t *testing.T) {
	path := os.Getenv("AURA_DOC_TEST_INGEST_FILE")
	if path == "" {
		path = defaultCLIIngestFixture
	}
	if _, err := os.Stat(path); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("document_ingest_live requires a readable fixture at AURA_DOC_TEST_INGEST_FILE (default %s): %v — a skipped tier must never pass as green",
				defaultCLIIngestFixture, err)
		}
		t.Skipf("set AURA_DOC_TEST_INGEST_FILE (default %s) to run this tier locally: %v", defaultCLIIngestFixture, err)
	}

	cfg := config.LoadDB()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcp.Close() }()

	fixture := liveIngestFixture{cfg: cfg, pool: pool, graph: mcp, path: path}
	t.Run("a live embedder embeds every chunk and the ledger says complete", func(t *testing.T) {
		liveIngestEmbedsEveryChunk(t, ctx, fixture)
	})
	t.Run("a dead embedder never leaves the document claiming to be ready", func(t *testing.T) {
		liveDeadEmbedderMarksTheDocumentFailed(t, ctx, fixture)
	})
}

// ingestService composes the ingest half exactly as cmd/aura's newDocumentIngestService
// does: the durable queue, not a synchronous worker, so what lands in Postgres is what the
// CLI really writes.
func (f liveIngestFixture) ingestService() *Service {
	baseURL := f.cfg.DocumentsBaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8083"
	}
	return &Service{
		Jobs:          NewPostgresJobStore(f.pool),
		Extractor:     &ExtractClient{BaseURL: baseURL},
		Indexer:       &Indexer{Client: f.graph},
		Searcher:      &Searcher{Client: f.graph, MUSRIsolation: f.cfg.MUSRIsolation},
		Embedder:      &DurableEmbeddingQueue{Jobs: NewPostgresIngestionJobStore(f.pool)},
		MaxBytes:      int64(f.cfg.AssetMaxDocumentBytes),
		MUSRIsolation: f.cfg.MUSRIsolation,
	}
}

// embeddingWorker mirrors cmd/aura's runtimeDocumentEmbeddingHandler, catalog included —
// the catalog seam is the half that has never worked.
func (f liveIngestFixture) embeddingWorker(generator EmbeddingGenerator) *EmbeddingWorker {
	if generator == nil {
		generator = &EmbeddingClient{BaseURL: f.cfg.Neo4j.EmbedURL, Dimensions: f.cfg.Neo4j.EmbedDimensions}
	}
	return &EmbeddingWorker{
		Jobs:      NewPostgresJobStore(f.pool),
		Generator: generator,
		Indexer: &Indexer{
			Client:           f.graph,
			EmbeddingModel:   "local-sidecar",
			EmbeddingVersion: fmt.Sprintf("dim:%d", f.cfg.Neo4j.EmbedDimensions),
		},
		Catalog:    NewPostgresCatalogStore(f.pool),
		MaxRetries: 1,
		Backoff:    50 * time.Millisecond,
	}
}

// ingest runs one scoped ingest and registers the purge that gives the live databases back.
func (f liveIngestFixture) ingest(t *testing.T, ctx context.Context, sourceID string) (*Job, string) {
	t.Helper()
	start := time.Now()
	job, err := f.ingestService().IngestPath(ctx, IngestRequest{SourceID: sourceID, SourceKind: "local"}, f.path)
	if err != nil {
		t.Fatalf("ingest %s: %v", f.path, err)
	}
	embedJobKey := IngestionJobTypeDocumentEmbed + ":" + job.DocumentID + ":" + job.ContentHash
	t.Cleanup(func() { purgeLiveDocument(t, f.pool, f.graph, job.DocumentID, sourceID, embedJobKey) })
	t.Logf("ingested %s as %s: chunks=%d status=%q in %s",
		job.FileName, job.DocumentID, job.SparseChunks, job.Status, time.Since(start))
	if job.SparseChunks < 1 {
		t.Fatalf("ingested %d chunks, want >= 1", job.SparseChunks)
	}
	if job.Status != JobSearchable {
		t.Fatalf("ingest status = %q, want %q", job.Status, JobSearchable)
	}
	return job, embedJobKey
}

func (f liveIngestFixture) graphChunkCounts(t *testing.T, ctx context.Context, documentID string) (chunks, embedded int) {
	t.Helper()
	rows, err := f.graph.Read(ctx, `
MATCH (:Document {id:$id})-[:HAS_CHUNK]->(c:Chunk)
RETURN count(c) AS chunks, count(c.embedding) AS embedded`, map[string]any{"id": documentID})
	if err != nil {
		t.Fatalf("count chunks for %s: %v", documentID, err)
	}
	return liveGraphCount(t, rows, "chunks"), liveGraphCount(t, rows, "embedded")
}

func liveIngestEmbedsEveryChunk(t *testing.T, ctx context.Context, f liveIngestFixture) {
	sourceID := fmt.Sprintf("cli-ingest-live-%d", time.Now().UnixNano())
	job, embedJobKey := f.ingest(t, ctx, sourceID)

	chunks, embedded := f.graphChunkCounts(t, ctx, job.DocumentID)
	t.Logf("before drain: graph chunks=%d embedded=%d", chunks, embedded)
	if chunks != job.SparseChunks {
		t.Fatalf("graph holds %d chunks, ledger says %d", chunks, job.SparseChunks)
	}

	payload := claimEmbedJobPayload(t, f.pool, embedJobKey)
	handler := &EmbeddingJobHandler{Worker: f.embeddingWorker(nil)}
	start := time.Now()
	if err := handler.HandleIngestionJob(ctx, IngestionJob{
		JobType: IngestionJobTypeDocumentEmbed,
		Payload: payload,
	}); err != nil {
		t.Fatalf("drain %s: %v", IngestionJobTypeDocumentEmbed, err)
	}
	drain := time.Since(start)

	chunks, embedded = f.graphChunkCounts(t, ctx, job.DocumentID)
	t.Logf("after drain in %s: graph chunks=%d embedded=%d", drain, chunks, embedded)
	if embedded != chunks {
		t.Fatalf("%d of %d chunks carry an embedding — the document is half-indexed and says nothing about it",
			embedded, chunks)
	}

	final, err := NewPostgresJobStore(f.pool).GetByDocumentID(ctx, job.DocumentID)
	if err != nil {
		t.Fatalf("read back ingest job: %v", err)
	}
	t.Logf("ledger: status=%q sparse=%d embedded=%d", final.Status, final.SparseChunks, final.EmbeddedChunks)
	if final.Status != JobComplete {
		t.Fatalf("ledger status = %q, want %q", final.Status, JobComplete)
	}
	if final.EmbeddedChunks != final.SparseChunks {
		t.Fatalf("ledger says %d embedded of %d sparse", final.EmbeddedChunks, final.SparseChunks)
	}
}

func liveDeadEmbedderMarksTheDocumentFailed(t *testing.T, ctx context.Context, f liveIngestFixture) {
	sourceID := fmt.Sprintf("cli-ingest-live-dead-%d", time.Now().UnixNano())
	job, embedJobKey := f.ingest(t, ctx, sourceID)

	// The catalog row an upload would have left behind, saying "ready" the moment the
	// bytes landed. A CLI ingest writes no such row of its own — aura.documents is
	// written only by the AG-UI create/upload chain — so the test supplies the exact
	// state the guarantee exists to correct, keyed the way RecordAssetVersion keys it.
	identityID := seedDocumentTestIdentity(t, ctx, f.pool)
	catalog := NewPostgresCatalogStore(f.pool)
	doc, err := catalog.CreateDocument(ctx, CreateDocumentRequest{
		IdentityID: identityID,
		Scope:      DocumentScopeLibrary,
		Title:      "cli ingest live: dead embedder",
		Metadata:   map[string]any{"search_document_id": job.DocumentID},
		Status:     DocumentStatusReady,
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), "DELETE FROM aura.documents WHERE id = $1", doc.ID)
	})

	payload := claimEmbedJobPayload(t, f.pool, embedJobKey)
	handler := &EmbeddingJobHandler{Worker: f.embeddingWorker(&fakeEmbeddingGenerator{failures: 99})}
	if err := handler.HandleIngestionJob(ctx, IngestionJob{
		JobType: IngestionJobTypeDocumentEmbed,
		Payload: payload,
	}); err == nil {
		t.Fatal("draining with a dead embedder returned nil")
	}

	chunks, embedded := f.graphChunkCounts(t, ctx, job.DocumentID)
	t.Logf("dead embedder: graph chunks=%d embedded=%d", chunks, embedded)
	if embedded != 0 {
		t.Fatalf("%d chunks carry an embedding after a dead embedder", embedded)
	}

	final, err := NewPostgresJobStore(f.pool).GetByDocumentID(ctx, job.DocumentID)
	if err != nil {
		t.Fatalf("read back ingest job: %v", err)
	}
	t.Logf("ledger after failure: status=%q embedded=%d error=%q", final.Status, final.EmbeddedChunks, final.Error)
	if final.Status == JobComplete {
		t.Fatalf("ledger status = %q after every embedding failed", final.Status)
	}
	if final.Error == "" {
		t.Fatal("ledger records no error after every embedding failed")
	}

	var status, reason string
	if err := f.pool.QueryRow(ctx,
		"SELECT status, coalesce(metadata->>'status_reason', '') FROM aura.documents WHERE id = $1",
		doc.ID).Scan(&status, &reason); err != nil {
		t.Fatalf("read back catalog status: %v", err)
	}
	t.Logf("catalog after failure: status=%q reason=%q", status, reason)
	if status != string(DocumentStatusFailed) {
		t.Fatalf("catalog status = %q, want %q — a document that promises semantic search it cannot do is worse than an error",
			status, DocumentStatusFailed)
	}
}
