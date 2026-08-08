// The replay branch in DocumentProcessor.ProcessAsset, exercised without a database or a
// container: both cases turn on one boolean the recorder returns.
//
// It used to assert that a replay SKIPPED the in-process pipeline. There is no pipeline to
// skip any more, but the distinction it protected outlived it and is why these tests were
// rewritten rather than deleted: a replay onto the document's PUBLISHED version genuinely
// has passages behind it already, so it alone may answer searchable. Every other outcome --
// including a replay onto a version still processing -- has nothing indexed yet and must say
// so, or the agent is offered a document retrieval cannot find (context.go:63).

package assets

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
)

func TestDocumentProcessorClaimsSearchableOnlyForActiveReplay(t *testing.T) {
	tests := []struct {
		name           string
		replayedActive bool
		wantStatus     Status
		wantSummary    string
	}{
		{
			name:           "replay onto a version that is not the published one is still pending",
			replayedActive: false,
			wantStatus:     StatusProcessing,
			wantSummary:    "sidecar",
		},
		{
			name:           "replay onto the published version is already indexed",
			replayedActive: true,
			wantStatus:     StatusSearchable,
			wantSummary:    "already indexed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runReplayProcessor(t, test.replayedActive)
			if result.Status != test.wantStatus || result.DocumentID != "doc-1" {
				t.Fatalf("result = %+v, want %s doc-1", result, test.wantStatus)
			}
			if !strings.Contains(result.Summary, test.wantSummary) {
				t.Fatalf("Summary = %q, want it to mention %q", result.Summary, test.wantSummary)
			}
		})
	}
}

// TestDocumentProcessorReplayLeavesJobForTheQueue pins the metadata key the ingestion
// handler reads. pipeline_activation_job_id means "the activation statement already marked
// this job succeeded"; the replay path activates nothing, so emitting it would leave the
// job leased until it dead-lettered.
func TestDocumentProcessorReplayLeavesJobForTheQueue(t *testing.T) {
	result := runReplayProcessor(t, true)
	if _, present := result.Metadata["pipeline_activation_job_id"]; present {
		t.Fatalf("replay result claims the activation committed the job: %#v", result.Metadata)
	}
	if result.Metadata["replayed_active"] != true {
		t.Fatalf("replay result metadata = %#v, want replayed_active", result.Metadata)
	}
	if result.Metadata["document_version_id"] != "version-1" {
		t.Fatalf("replay result version id = %#v", result.Metadata["document_version_id"])
	}
}

func runReplayProcessor(t *testing.T, replayedActive bool) Result {
	t.Helper()
	const identity = "00000000-0000-0000-0000-000000000001"
	objects := objectstore.NewFake()
	ref := objectstore.ObjectRef{Bucket: "b", Key: "k"}
	if _, err := objects.Put(context.Background(), ref, strings.NewReader("%PDF test"),
		objectstore.PutOptions{MIMEType: "application/pdf", Size: 9}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	ingest := &recordingIngestor{
		job: &documents.Job{ID: "job-1", DocumentID: "doc-1", FileName: "manual.pdf"},
	}
	recorder := &recordingVersionRecorder{record: documents.DocumentVersionRecord{
		Document: documents.Document{
			ID: "catalog-1", IdentityID: identity, SearchDocumentID: "doc-1", Title: "Manual",
		},
		Version: documents.DocumentVersion{
			ID: "version-1", IdentityID: identity, DocumentID: "catalog-1",
			SearchDocumentID: "doc-1", VersionNumber: 1, PipelineGeneration: 1,
		},
		ReplayedActive: replayedActive,
	}}
	ctx := documents.WithClaimedIngestionJob(context.Background(), documents.IngestionJob{
		ID: "queue-job-1", IdentityID: identity, AssetID: "asset-1",
		LockedBy: "worker-1", LeaseGeneration: 1,
	})
	result, err := (&DocumentProcessor{
		Objects: objects, Ingest: ingest, VersionRecorder: recorder,
	}).ProcessAsset(ctx, Asset{
		ID: "asset-1", IdentityID: identity, SourceKind: SourceWeb, Scope: ScopeThread,
		ObjectBucket: "b", ObjectKey: "k", ObjectETag: "etag-1", FileName: "manual.pdf",
		MIMEType: "application/pdf", SizeBytes: 9, ContentHash: "sha256",
	})
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}
	return result
}
