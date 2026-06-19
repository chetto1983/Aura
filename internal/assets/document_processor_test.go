package assets

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
)

func TestDocumentProcessorStreamsObjectToIngestPath(t *testing.T) {
	objects := objectstore.NewFake()
	ref := objectstore.ObjectRef{Bucket: "b", Key: "k"}
	if _, err := objects.Put(context.Background(), ref, strings.NewReader("%PDF test"), objectstore.PutOptions{MIMEType: "application/pdf", Size: 9}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	ingest := &recordingIngestor{
		job: &documents.Job{ID: "job-1", DocumentID: "doc-1", FileName: "manual.pdf", SparseChunks: 3},
	}

	result, err := (&DocumentProcessor{Objects: objects, Ingest: ingest}).ProcessAsset(context.Background(), Asset{
		ID:           "asset-1",
		SourceKind:   SourceWeb,
		ObjectBucket: "b",
		ObjectKey:    "k",
		FileName:     "manual.pdf",
		MIMEType:     "application/pdf",
		SizeBytes:    9,
	})
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}
	if result.Status != StatusSearchable || result.DocumentID != "doc-1" {
		t.Fatalf("result = %+v, want searchable doc-1", result)
	}
	if result.Metadata["document_job_id"] != "job-1" || result.Metadata["sparse_chunks"] != 3 {
		t.Fatalf("metadata = %#v, want document job id and sparse chunk count", result.Metadata)
	}
	if ingest.req.OriginalPath != "object://b/k" {
		t.Fatalf("OriginalPath = %q, want object://b/k", ingest.req.OriginalPath)
	}
	if ingest.req.SourceID != "asset-1" || ingest.req.SourceKind != string(SourceWeb) {
		t.Fatalf("IngestRequest source = %q/%q, want asset-1/%s", ingest.req.SourceID, ingest.req.SourceKind, SourceWeb)
	}
	if ingest.req.FileName != "manual.pdf" || ingest.req.MIMEType != "application/pdf" || ingest.req.SizeBytes != 9 {
		t.Fatalf("IngestRequest file fields = %#v", ingest.req)
	}
	if ingest.payload != "%PDF test" {
		t.Fatalf("ingested payload = %q, want object bytes", ingest.payload)
	}
	if _, err := os.Stat(ingest.path); !os.IsNotExist(err) {
		t.Fatalf("temporary asset path still exists after ProcessAsset: stat err=%v", err)
	}
}

type recordingIngestor struct {
	req     documents.IngestRequest
	path    string
	payload string
	job     *documents.Job
	err     error
}

func (r *recordingIngestor) IngestPath(_ context.Context, req documents.IngestRequest, path string) (*documents.Job, error) {
	r.req = req
	r.path = path
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r.payload = string(data)
	if r.err != nil {
		return nil, r.err
	}
	return r.job, nil
}
