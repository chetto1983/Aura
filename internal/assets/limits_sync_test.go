package assets

import (
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

// imageExts are the documents-allowlist entries that ride the ModalityImage path in the
// asset layer (vision/OCR), NOT the ModalityDocument path — so they are intentionally
// absent from documentExts. Keep this in step with InferModality's image handling.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// TestDocumentAllowlistInSyncWithIngest guards the assets<->documents allowlist drift that
// silently 400'd pptx/html uploads in production (the ingest pipeline supported them but the
// upload presign rejected them). Every NON-IMAGE format the ingest pipeline
// (internal/documents) accepts MUST also be accepted by the asset-upload allowlist, and the
// asset allowlist must not accept anything ingest cannot handle.
func TestDocumentAllowlistInSyncWithIngest(t *testing.T) {
	ingest := documents.SupportedDocumentExts()

	for _, ext := range ingest {
		if imageExts[ext] {
			continue // image formats go through ModalityImage, not the document allowlist
		}
		if !documentExts[ext] {
			t.Errorf("ingest accepts %q but the asset-upload allowlist (internal/assets/limits.go documentExts) rejects it — drift; add it", ext)
		}
	}

	ingestSet := make(map[string]bool, len(ingest))
	for _, ext := range ingest {
		ingestSet[ext] = true
	}
	for ext := range documentExts {
		if !ingestSet[ext] {
			t.Errorf("asset-upload allows %q but the ingest pipeline (internal/documents) does not — uploads of it will fail downstream", ext)
		}
	}
}
