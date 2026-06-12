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
	if len(fake.writeCalls) != 4 {
		t.Fatalf("write calls = %d", len(fake.writeCalls))
	}
	last := fake.writeCalls[len(fake.writeCalls)-1]
	if !strings.Contains(last.query, `d.status = "searchable"`) {
		t.Fatalf("last query should mark searchable:\n%s", last.query)
	}
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
	}, "content-hash", resp, time.Unix(10, 0))
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
