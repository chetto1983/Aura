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
		// SparseChunks stays 0: ingest hard-wires it, so a fixture that sets it green-lights
		// an output the production path can never produce. This test used to assert 3.
		job: &documents.Job{ID: "job-1", DocumentID: "doc-1", FileName: "manual.pdf"},
	}
	recorder := &recordingVersionRecorder{record: documents.DocumentVersionRecord{
		Document: documents.Document{ID: "catalog-1", IdentityID: "00000000-0000-0000-0000-000000000001", SearchDocumentID: "doc-1", Title: "Manual"},
		Version:  documents.DocumentVersion{ID: "version-1", IdentityID: "00000000-0000-0000-0000-000000000001", DocumentID: "catalog-1", SearchDocumentID: "doc-1", VersionNumber: 1, PipelineGeneration: 1},
	}}
	pipeline := &recordingDocumentPipeline{result: documents.PipelineRunResult{
		DocumentID: "catalog-1", SearchDocumentID: "doc-1", VersionID: "version-1",
		VersionNumber: 1, PipelineGeneration: 1, PassageCount: 3,
	}}
	ctx := documents.WithClaimedIngestionJob(context.Background(), documents.IngestionJob{
		ID: "queue-job-1", IdentityID: "00000000-0000-0000-0000-000000000001", AssetID: "asset-1",
		LockedBy: "worker-1", LeaseGeneration: 1,
	})

	result, err := (&DocumentProcessor{Objects: objects, Ingest: ingest, VersionRecorder: recorder, Pipeline: pipeline}).ProcessAsset(ctx, Asset{
		ID:           "asset-1",
		IdentityID:   "00000000-0000-0000-0000-000000000001",
		SourceKind:   SourceWeb,
		Scope:        ScopeThread,
		ObjectBucket: "b",
		ObjectKey:    "k",
		ObjectETag:   "etag-1",
		FileName:     "manual.pdf",
		MIMEType:     "application/pdf",
		SizeBytes:    9,
		ContentHash:  "sha256",
	})
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}
	if result.Status != StatusSearchable || result.DocumentID != "doc-1" {
		t.Fatalf("result = %+v, want searchable doc-1", result)
	}
	if result.Metadata["document_job_id"] != "job-1" {
		t.Fatalf("metadata = %#v, want the document job id", result.Metadata)
	}
	if _, leaked := result.Metadata["sparse_chunks"]; leaked {
		t.Fatalf("metadata still carries sparse_chunks: %#v", result.Metadata)
	}
	// The summary is user-visible on every upload. It used to read "manual.pdf indexed
	// with 0 searchable chunks" — always zero, because ingest stopped chunking.
	if !strings.Contains(result.Summary, "manual.pdf") || !strings.Contains(result.Summary, "3 verified passages") {
		t.Fatalf("Summary = %q, want the verified passage count", result.Summary)
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
	if recorder.asset.ID != "asset-1" || recorder.job.DocumentID != "doc-1" {
		t.Fatalf("version recorder saw asset/job = %#v/%#v", recorder.asset, recorder.job)
	}
	if recorder.hashes.SHA256 != "bb094c25184067415837d8dc66cfa65366384a80625877252719369a2dc80575" || recorder.hashes.SHA1 == "" {
		t.Fatalf("version recorder hashes = %#v", recorder.hashes)
	}
	if pipeline.req.JobID != "queue-job-1" || pipeline.req.Objects != objects || pipeline.req.Path == "" {
		t.Fatalf("pipeline request = %#v", pipeline.req)
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

type recordingVersionRecorder struct {
	asset  Asset
	job    documents.Job
	hashes documents.ContentHashes
	err    error
	record documents.DocumentVersionRecord
}

func (r *recordingVersionRecorder) RecordDocumentAsset(
	_ context.Context,
	asset Asset,
	job documents.Job,
	hashes documents.ContentHashes,
) (documents.DocumentVersionRecord, error) {
	r.asset = asset
	r.job = job
	r.hashes = hashes
	return r.record, r.err
}

type recordingDocumentPipeline struct {
	req    documents.PipelineRunRequest
	result documents.PipelineRunResult
	err    error
	calls  int
}

func (p *recordingDocumentPipeline) Run(_ context.Context, req documents.PipelineRunRequest) (documents.PipelineRunResult, error) {
	p.req = req
	p.calls++
	return p.result, p.err
}
