package assets

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

const documentProcessorIdentity = "b130c94d-a213-463a-a797-ec124104363a"

func claimedFor(assetID string) context.Context {
	return documents.WithClaimedIngestionJob(context.Background(), documents.IngestionJob{
		ID: "queue-job-1", IdentityID: documentProcessorIdentity, AssetID: assetID,
		LockedBy: "worker-1", LeaseGeneration: 1,
	})
}

func uploadedAsset(kind SourceKind) Asset {
	return Asset{
		ID:           "66d3bcbb-c902-478c-86f3-63bd73018d17",
		IdentityID:   documentProcessorIdentity,
		SourceKind:   kind,
		Scope:        ScopeThread,
		ObjectBucket: "aura-" + documentProcessorIdentity,
		ObjectKey:    "chat/66d3bcbb-c902-478c-86f3-63bd73018d17.txt",
		FileName:     "rapporto-prova.txt",
		MIMEType:     "text/plain",
		SizeBytes:    119,
	}
}

// The id is a CROSS-LANGUAGE contract, so the expected value is not recomputed from the
// same Go function this asserts. It is the literal that services/ingest/identity.py
// produced for this exact (identity, "s3", key) triple when it was run against the
// installed sidecar on 2026-08-17 — the sidecar being the side that actually writes the
// IndexedDocument row Aura then looks up.
func TestDocumentProcessorNamesTheObjectTheWayTheIndexDoes(t *testing.T) {
	const wantFromTheSidecar = "doc_403e3fe4482dca1c94d1f48c9d715e28"

	result, err := (&DocumentProcessor{}).ProcessAsset(
		claimedFor("66d3bcbb-c902-478c-86f3-63bd73018d17"), uploadedAsset(SourceWeb))
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}
	if result.DocumentID != wantFromTheSidecar {
		t.Fatalf("DocumentID = %q, want the id the ingest sidecar files this object under (%q)",
			result.DocumentID, wantFromTheSidecar)
	}
}

// The channel must not reach the id. Web, Telegram and the CLI are wrappers over one door,
// and deriving from SourceKind is exactly the bug this replaced: an upload was filed under
// ("web", sha256(assetID)) while the index filed it under ("s3", object key), so the two
// could never meet and the knowledge catalog was empty for every uploaded document.
func TestDocumentProcessorIDDoesNotDependOnTheChannel(t *testing.T) {
	ids := make(map[string]bool)
	for _, kind := range []SourceKind{SourceWeb, SourceTelegram, SourceCLI} {
		result, err := (&DocumentProcessor{}).ProcessAsset(
			claimedFor("66d3bcbb-c902-478c-86f3-63bd73018d17"), uploadedAsset(kind))
		if err != nil {
			t.Fatalf("ProcessAsset(%s): %v", kind, err)
		}
		ids[result.DocumentID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("the channel reached the document id: %v", ids)
	}
}

// StatusProcessing, not StatusSearchable: nothing here produced a passage. Whether the
// document is searchable is ArcadeDB's answer, asked by BuildKnowledgeCatalog's isIndexed.
func TestDocumentProcessorReportsStoredNotSearchable(t *testing.T) {
	asset := uploadedAsset(SourceWeb)

	result, err := (&DocumentProcessor{}).ProcessAsset(claimedFor(asset.ID), asset)
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}
	if result.Status != StatusProcessing {
		t.Fatalf("Status = %q, want %q", result.Status, StatusProcessing)
	}
	// The object coordinates are the handoff to the sidecar: they are what a Passage's
	// source_key is later matched against, so losing them breaks find-then-open silently.
	if result.Metadata["object_bucket"] != asset.ObjectBucket ||
		result.Metadata["object_key"] != asset.ObjectKey {
		t.Fatalf("metadata = %#v, want the object coordinates the sidecar indexes from", result.Metadata)
	}
	if !strings.Contains(result.Summary, asset.FileName) || !strings.Contains(result.Summary, "sidecar") {
		t.Fatalf("Summary = %q, want it to name the file and say indexing is out of process", result.Summary)
	}
}

func TestDocumentProcessorRefusesWithoutAnOwningClaim(t *testing.T) {
	asset := uploadedAsset(SourceWeb)
	tests := map[string]context.Context{
		"no claim at all":      context.Background(),
		"claim owns no lease":  documents.WithClaimedIngestionJob(context.Background(), documents.IngestionJob{ID: "queue-job-1", IdentityID: documentProcessorIdentity, LockedBy: "worker-1"}),
		"claim of another one": claimedFor("a-different-asset"),
	}
	for name, ctx := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := (&DocumentProcessor{}).ProcessAsset(ctx, asset); err == nil {
				t.Fatal("an asset was settled without a claim that owns it")
			}
		})
	}
}

// An asset whose bytes never got a key cannot be named, and returning an id derived from an
// empty key would be a value the index can never hold.
func TestDocumentProcessorRefusesAnAssetWithNoObjectKey(t *testing.T) {
	asset := uploadedAsset(SourceWeb)
	asset.ObjectKey = ""

	if _, err := (&DocumentProcessor{}).ProcessAsset(claimedFor(asset.ID), asset); err == nil {
		t.Fatal("an asset with no object key was given a document id")
	}
}
