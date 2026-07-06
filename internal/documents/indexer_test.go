package documents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIndexerBatchesChunks(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake, BatchSize: 2}
	doc := testDocumentWithChunks(t, 5)

	count, err := indexer.UpsertSparse(t.Context(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("count = %d", count)
	}
	var batchSizes []int
	for _, call := range fake.writeCalls {
		if chunks, ok := call.params["chunks"].([]map[string]any); ok {
			batchSizes = append(batchSizes, len(chunks))
		}
	}
	want := []int{2, 2, 1}
	if len(batchSizes) != len(want) {
		t.Fatalf("batch sizes = %v", batchSizes)
	}
	for i := range want {
		if batchSizes[i] != want[i] {
			t.Fatalf("batch sizes = %v", batchSizes)
		}
	}
}

func TestIndexerSetsDocumentSearchableAfterChunkWrites(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake, BatchSize: 2}
	doc := testDocumentWithChunks(t, 3)

	if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	// document upsert + 2 chunk batches + NEXT_CHUNK edges + searchable mark.
	if len(fake.writeCalls) != 5 {
		t.Fatalf("write calls = %d", len(fake.writeCalls))
	}
	last := fake.writeCalls[len(fake.writeCalls)-1]
	if !strings.Contains(last.query, `d.status = "searchable"`) {
		t.Fatalf("last query should mark searchable:\n%s", last.query)
	}
}

// TestIndexerNormalizesEmptyIdentityToOperator locks LO-03: an empty ingest identity is
// attributed to the operator (local UUID …001) before the ownership MERGE — never
// (:User {identifier:""}), which would orphan the doc from everyone post-flip — while a real
// identity is threaded verbatim. writeCalls[0] is the document/ownership upsert.
func TestIndexerNormalizesEmptyIdentityToOperator(t *testing.T) {
	t.Run("empty identity attributes to operator", func(t *testing.T) {
		fake := &fakeKnowledgeClient{}
		indexer := &Indexer{Client: fake}
		doc := testDocumentWithChunks(t, 1)
		doc.IdentityID = ""
		if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
			t.Fatal(err)
		}
		if got := fake.writeCalls[0].params["identity"]; got != OperatorIdentity {
			t.Fatalf("empty-identity ingest identity param = %#v, want operator %q", got, OperatorIdentity)
		}
	})
	t.Run("real identity threaded verbatim", func(t *testing.T) {
		const principal = "00000000-0000-0000-0000-0000000000ab"
		fake := &fakeKnowledgeClient{}
		indexer := &Indexer{Client: fake}
		doc := testDocumentWithChunks(t, 1)
		doc.IdentityID = principal
		if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
			t.Fatal(err)
		}
		if got := fake.writeCalls[0].params["identity"]; got != principal {
			t.Fatalf("real-identity ingest identity param = %#v, want %q", got, principal)
		}
	})
}

func TestIndexerStopsOnWriteFailure(t *testing.T) {
	fake := &fakeKnowledgeClient{failWriteAt: 2, failErr: errors.New("boom")}
	indexer := &Indexer{Client: fake, BatchSize: 2}
	doc := testDocumentWithChunks(t, 4)

	_, err := indexer.UpsertSparse(t.Context(), doc)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want boom error, got %v", err)
	}
	if len(fake.writeCalls) != 2 {
		t.Fatalf("should stop after failing write, calls = %d", len(fake.writeCalls))
	}
}

func TestIndexerStoresLocatorAsJSON(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake}
	doc := testDocumentWithChunks(t, 1)
	doc.Chunks[0].Locator = Locator{Page: 12}

	if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	var locator string
	for _, call := range fake.writeCalls {
		chunks, ok := call.params["chunks"].([]map[string]any)
		if ok && len(chunks) > 0 {
			locator, _ = chunks[0]["locator_json"].(string)
		}
	}
	if locator != `{"page":12}` {
		t.Fatalf("locator json = %q", locator)
	}
}

func TestIndexerStoresFileNameOnChunks(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake}
	doc := testDocumentWithChunks(t, 1)

	if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.writeCalls {
		chunks, ok := call.params["chunks"].([]map[string]any)
		if ok && len(chunks) > 0 {
			if got := chunks[0]["file_name"]; got != "manual.pdf" {
				t.Fatalf("file_name = %#v", got)
			}
			return
		}
	}
	t.Fatal("chunk batch not written")
}

func TestIndexerWritesLifecycleMetadataOnDocumentsAndChunks(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake}
	doc := testDocumentWithChunks(t, 1)

	if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	document := fake.writeCalls[0].params["document"].(map[string]any)
	if document["active"] != true {
		t.Fatalf("document active = %#v", document["active"])
	}
	if !strings.Contains(fake.writeCalls[0].query, "d.active = $document.active") ||
		!strings.Contains(fake.writeCalls[0].query, "d.deleted_at = NULL") {
		t.Fatalf("document query missing lifecycle writes:\n%s", fake.writeCalls[0].query)
	}
	for _, call := range fake.writeCalls {
		chunks, ok := call.params["chunks"].([]map[string]any)
		if ok && len(chunks) > 0 {
			if chunks[0]["active"] != true {
				t.Fatalf("chunk active = %#v", chunks[0]["active"])
			}
			if !strings.Contains(call.query, "c.active = chunk.active") ||
				!strings.Contains(call.query, "c.deleted_at = NULL") {
				t.Fatalf("chunk query missing lifecycle writes:\n%s", call.query)
			}
			return
		}
	}
	t.Fatal("chunk batch not written")
}

func TestIndexerUpsertsEmbeddings(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake, EmbeddingModel: "text-embedding-3-large", EmbeddingVersion: "2026-06-30"}

	count, err := indexer.UpsertEmbeddings(t.Context(), "doc_1", []EmbeddedChunk{
		{ID: "chunk_1", Embedding: []float64{0.1, 0.2}},
	}, JobComplete, 1)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("embedded count = %d", count)
	}
	params := fake.writeCalls[0].params
	if params["status"] != string(JobComplete) || params["embedded_count"] != 1 {
		t.Fatalf("embedding params = %#v", params)
	}
	chunks := params["chunks"].([]map[string]any)
	if chunks[0]["embedding_model"] != "text-embedding-3-large" ||
		chunks[0]["embedding_version"] != "2026-06-30" ||
		chunks[0]["active"] != true {
		t.Fatalf("embedding chunk params = %#v", chunks[0])
	}
	if !strings.Contains(fake.writeCalls[0].query, "c.embedding_model = chunk.embedding_model") ||
		!strings.Contains(fake.writeCalls[0].query, "c.active = chunk.active") ||
		!strings.Contains(fake.writeCalls[0].query, "c.deleted_at = NULL") {
		t.Fatalf("embedding query missing lifecycle writes:\n%s", fake.writeCalls[0].query)
	}
}

func TestIndexerUpsertEmbeddingsNoopAndMissingClient(t *testing.T) {
	indexer := &Indexer{Client: &fakeKnowledgeClient{}}
	count, err := indexer.UpsertEmbeddings(t.Context(), "doc_1", nil, JobEmbedding, 0)
	if err != nil || count != 0 {
		t.Fatalf("empty embeddings = (%d, %v)", count, err)
	}
	_, err = (&Indexer{}).UpsertEmbeddings(t.Context(), "doc_1", []EmbeddedChunk{{ID: "c"}}, JobEmbedding, 0)
	if err == nil || !strings.Contains(err.Error(), "knowledge client") {
		t.Fatalf("want missing client error, got %v", err)
	}
}

func TestCountFromRowsHandlesNumericShapes(t *testing.T) {
	cases := []struct {
		row  map[string]any
		want int
	}{
		{map[string]any{"count": int64(7)}, 7},
		{map[string]any{"embedded": int32(6)}, 6},
		{map[string]any{"chunks": float64(5)}, 5},
		{map[string]any{"other": "x"}, 3},
	}
	for _, tc := range cases {
		if got := countFromRows([]map[string]any{tc.row}, 3); got != tc.want {
			t.Fatalf("countFromRows(%#v) = %d, want %d", tc.row, got, tc.want)
		}
	}
}

func TestIndexerWritesSequentialNextChunkEdges(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake, BatchSize: 2}
	doc := testDocumentWithChunks(t, 5)

	if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	call, ok := nextChunkWrite(fake.writeCalls)
	if !ok {
		t.Fatal("no NEXT_CHUNK write recorded")
	}
	// Idempotent edge upsert: MERGE the relationship, never CREATE (re-ingest
	// must not duplicate edges).
	if !strings.Contains(call.query, "MERGE") || strings.Contains(call.query, "CREATE") {
		t.Fatalf("NEXT_CHUNK query must MERGE (idempotent) and not CREATE:\n%s", call.query)
	}
	pairs, ok := call.params["pairs"].([]map[string]any)
	if !ok {
		t.Fatalf("pairs param type = %T", call.params["pairs"])
	}
	if len(pairs) != 4 {
		t.Fatalf("NEXT_CHUNK pairs = %d, want chunk_count-1 = 4", len(pairs))
	}
	for i, pair := range pairs {
		wantPrev := ChunkID(doc.ID, i)
		wantNext := ChunkID(doc.ID, i+1)
		if pair["prev"] != wantPrev || pair["next"] != wantNext {
			t.Fatalf("pair[%d] = %#v, want prev=%s next=%s", i, pair, wantPrev, wantNext)
		}
	}
	// Edges must be written AFTER all chunk nodes exist.
	lastChunk := lastHasChunkWriteIndex(fake.writeCalls)
	nextIdx := nextChunkWriteIndex(fake.writeCalls)
	if lastChunk < 0 || nextIdx <= lastChunk {
		t.Fatalf("NEXT_CHUNK write at %d must follow the last HAS_CHUNK write at %d", nextIdx, lastChunk)
	}
}

func TestIndexerSingleChunkWritesNoNextChunkEdges(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake}
	doc := testDocumentWithChunks(t, 1)

	if _, err := indexer.UpsertSparse(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := nextChunkWrite(fake.writeCalls); ok {
		t.Fatal("a 1-chunk document must not write any NEXT_CHUNK edges")
	}
}

func TestIndexerDeactivateDocumentMarksGraphInactive(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	indexer := &Indexer{Client: fake}

	if err := indexer.DeactivateDocument(t.Context(), "doc_1"); err != nil {
		t.Fatal(err)
	}
	call := fake.writeCalls[0]
	if call.params["document_id"] != "doc_1" {
		t.Fatalf("params = %#v", call.params)
	}
	for _, want := range []string{
		"d.active = false",
		"d.deleted_at = datetime()",
		"c.active = false",
		"c.deleted_at = datetime()",
	} {
		if !strings.Contains(call.query, want) {
			t.Fatalf("deactivate query missing %q:\n%s", want, call.query)
		}
	}
}

func nextChunkWrite(calls []knowledgeCall) (knowledgeCall, bool) {
	for _, call := range calls {
		if strings.Contains(call.query, "NEXT_CHUNK") {
			return call, true
		}
	}
	return knowledgeCall{}, false
}

func nextChunkWriteIndex(calls []knowledgeCall) int {
	for i, call := range calls {
		if strings.Contains(call.query, "NEXT_CHUNK") {
			return i
		}
	}
	return -1
}

func lastHasChunkWriteIndex(calls []knowledgeCall) int {
	last := -1
	for i, call := range calls {
		if strings.Contains(call.query, "HAS_CHUNK") {
			last = i
		}
	}
	return last
}

func testDocumentWithChunks(t *testing.T, n int) ExtractedDocument {
	t.Helper()
	resp := &ExtractorResponse{Chunks: make([]ExtractedChunk, n)}
	for i := range resp.Chunks {
		resp.Chunks[i] = ExtractedChunk{Kind: "page", Text: "chunk text", Locator: Locator{Page: i + 1}}
	}
	doc, err := BuildExtractedDocument(IngestRequest{
		SourceID:   "cli",
		SourceKind: "local",
		FileName:   "manual.pdf",
		MIMEType:   "application/pdf",
		SizeBytes:  10,
	}, "content-hash", resp, time.Unix(10, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

type knowledgeCall struct {
	query  string
	params map[string]any
}

type fakeKnowledgeClient struct {
	writeCalls  []knowledgeCall
	readCalls   []knowledgeCall
	readRows    []map[string]any
	failWriteAt int
	failRead    error
	failErr     error
}

func (f *fakeKnowledgeClient) Read(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.readCalls = append(f.readCalls, knowledgeCall{query: query, params: params})
	if f.failRead != nil {
		return nil, f.failRead
	}
	return f.readRows, nil
}

func (f *fakeKnowledgeClient) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.writeCalls = append(f.writeCalls, knowledgeCall{query: query, params: params})
	if f.failWriteAt > 0 && len(f.writeCalls) == f.failWriteAt {
		if f.failErr != nil {
			return nil, f.failErr
		}
		return nil, errors.New("write failed")
	}
	if chunks, ok := params["chunks"].([]map[string]any); ok {
		return []map[string]any{{"chunks": len(chunks)}}, nil
	}
	return []map[string]any{{"chunks": 0}}, nil
}
