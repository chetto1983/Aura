package assets

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// objectKeyViolation is what aura.assets answers when its (identity_id, object_key) UNIQUE
// index is already taken.
func objectKeyViolation() error {
	return &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "assets_identity_object_key_idx",
		Message:        "duplicate key value violates unique constraint",
	}
}

// holdsObjectKey is the fake's copy of that index. It counts deleted rows too, as the index
// does: until 2026-10-04 the fake let two library presigns of one name both succeed, which is
// how a presign that Postgres refused passed its unit test.
func (s *fakeAssetStore) holdsObjectKey(identityID, objectKey string) bool {
	for _, held := range s.assets {
		if held.IdentityID == identityID && held.ObjectKey == objectKey {
			return true
		}
	}
	return false
}

func (s *fakeAssetStore) Rearm(_ context.Context, id, identityID string, req CreateRequest) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusPresigned
		asset.FileName = req.FileName
		asset.MIMEType = req.MIMEType
		asset.Modality = req.Modality
		asset.DeclaredSizeBytes = req.DeclaredSizeBytes
		asset.SizeBytes = 0
		asset.ContentHash = ""
		asset.ErrorCode, asset.ErrorMessage = "", ""
	})
}

func libraryPresign(name string, size int64) PresignRequest {
	return PresignRequest{
		IdentityID: serviceIdentityID, SourceKind: SourceWeb, Scope: ScopeLibrary,
		FileName: name, MIMEType: "application/pdf", DeclaredSizeBytes: size,
	}
}

// A library name uploaded again replaces the file: the same row and object key, back to
// presigned, holding the NEW declared size -- so finalize checks the new bytes and does not
// refuse them, and delete the stored file, for differing from the old one.
func TestPresignReplacesALibraryFileOfTheSameName(t *testing.T) {
	t.Parallel()
	svc, store := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 1000, MaxImageBytes: 100, MaxAudioBytes: 100})
	ctx := context.Background()
	first, err := svc.Presign(ctx, libraryPresign("Report.pdf", 40))
	if err != nil {
		t.Fatalf("first presign: %v", err)
	}
	if _, err := store.SetStatus(ctx, first.Asset.ID, serviceIdentityID, StatusComplete, "", ""); err != nil {
		t.Fatalf("settle the first upload: %v", err)
	}
	second, err := svc.Presign(ctx, libraryPresign("report.pdf", 90))
	if err != nil {
		t.Fatalf("second presign of the same name: %v", err)
	}
	got := second.Asset
	if got.ID != first.Asset.ID || got.ObjectKey != first.Asset.ObjectKey {
		t.Fatalf("replacement made a new file: first %s %s, second %s %s",
			first.Asset.ID, first.Asset.ObjectKey, got.ID, got.ObjectKey)
	}
	if got.Status != StatusPresigned || got.DeclaredSizeBytes != 90 || got.FileName != "report.pdf" {
		t.Fatalf("re-armed row = status %s, declared %d, name %q; want presigned, 90, report.pdf",
			got.Status, got.DeclaredSizeBytes, got.FileName)
	}
	if second.Upload.URL == "" {
		t.Fatal("the replacement got no upload URL")
	}
}

// A name whose row is being deleted is refused, not re-armed: the delete is about to remove
// the object the new upload would write.
func TestPresignRefusesALibraryNameStillBeingDeleted(t *testing.T) {
	t.Parallel()
	svc, store := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 1000, MaxImageBytes: 100, MaxAudioBytes: 100})
	ctx := context.Background()
	first, err := svc.Presign(ctx, libraryPresign("old.pdf", 40))
	if err != nil {
		t.Fatalf("first presign: %v", err)
	}
	if _, err := store.Delete(ctx, first.Asset.ID, serviceIdentityID); err != nil {
		t.Fatalf("mark deleting: %v", err)
	}
	if _, err := svc.Presign(ctx, libraryPresign("old.pdf", 40)); !errors.Is(err, ErrLibraryNameBusy) {
		t.Fatalf("presign over a deleting row = %v, want ErrLibraryNameBusy", err)
	}
}

// A thread attachment takes a random key, so a key violation there is not a replacement and
// must come back as it arrived.
func TestPresignReturnsAThreadKeyViolationAsIs(t *testing.T) {
	t.Parallel()
	svc, store := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 1000, MaxImageBytes: 100, MaxAudioBytes: 100})
	store.duplicateKey = true
	req := libraryPresign("notes.pdf", 10)
	req.Scope = ScopeThread
	_, err := svc.Presign(context.Background(), req)
	if err == nil || errors.Is(err, ErrLibraryNameBusy) {
		t.Fatalf("thread presign over a taken key = %v, want the store's own error", err)
	}
}
