package assets

import (
	"context"
	"strings"
	"testing"
)

// The presign half of the asset service: what it refuses, what scope it assigns, and what
// it is allowed to write into an object key. Split from service_test.go when that file
// crossed the 600-LOC cap (CLAUDE.md refactor-on-touch); no test body changed.

func TestServicePresignRejectsUnsupportedExecutable(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "setup.exe",
		MIMEType:          "application/octet-stream",
		DeclaredSizeBytes: 10,
	})
	if err == nil {
		t.Fatal("Presign(.exe) succeeded, want refusal")
	}
}

func TestServicePresignNeverPutsFilenameInObjectKey(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          `C:\Users\me\Quarterly Secrets.pdf`,
		MIMEType:          "",
		DeclaredSizeBytes: 10,
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	key := strings.ToLower(resp.Asset.ObjectKey)
	// ".pdf" left this list deliberately. The rule is that a key must not leak what the
	// document IS -- keys travel into presigned URLs, S3 access logs and error strings -- and
	// an extension identifies a format, not a document. It is carried because the extractor
	// routes on it: an attachment stored without one reaches Tika as an unknown type and is
	// refused, which is how chat uploads went unindexed.
	for _, forbidden := range []string{"quarterly", "secrets", "users"} {
		if strings.Contains(key, forbidden) {
			t.Fatalf("object key %q contains filename fragment %q", resp.Asset.ObjectKey, forbidden)
		}
	}
	if !strings.HasSuffix(key, ".pdf") {
		t.Fatalf("object key %q dropped the extension the extractor routes on", resp.Asset.ObjectKey)
	}
	if len(store.created) != 1 {
		t.Fatalf("store created %d assets, want 1", len(store.created))
	}
	if resp.Asset.FileName != "Quarterly Secrets.pdf" {
		t.Fatalf("FileName = %q, want sanitized basename", resp.Asset.FileName)
	}
}

func TestServicePresignAcceptsLibraryScope(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 256,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		Scope:             ScopeLibrary,
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 128,
		ModalityHint:      ModalityDocument,
	})
	if err != nil {
		t.Fatalf("Presign returned error: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created %d assets, want 1", len(store.created))
	}
	if store.created[0].Scope != ScopeLibrary {
		t.Fatalf("Create scope = %q, want %q", store.created[0].Scope, ScopeLibrary)
	}
}

func TestServicePresignDefaultsToThreadScope(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "note.txt",
		MIMEType:          "text/plain",
		DeclaredSizeBytes: 32,
		ModalityHint:      ModalityDocument,
	})
	if err != nil {
		t.Fatalf("Presign returned error: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created %d assets, want 1", len(store.created))
	}
	if store.created[0].Scope != ScopeThread {
		t.Fatalf("Create scope = %q, want %q", store.created[0].Scope, ScopeThread)
	}
}
