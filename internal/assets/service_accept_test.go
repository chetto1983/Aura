package assets

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

func TestServiceFinalizeRefusesOversizedActualObject(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 5,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 4,
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, strings.NewReader("123456"), objectstore.PutOptions{MIMEType: "application/pdf", Size: 6}); err != nil {
		t.Fatalf("Put object: %v", err)
	}

	updated, err := svc.Finalize(context.Background(), serviceIdentityID, resp.Asset.ID)
	if err == nil {
		t.Fatal("Finalize() succeeded, want oversized refusal")
	}
	if updated.Status != StatusRefused || updated.ErrorCode != "asset_refused" {
		t.Fatalf("Finalize() updated asset = %#v, want refused asset", updated)
	}
	if _, err := svc.Objects.Head(context.Background(), ref); err == nil {
		t.Fatal("oversized object still exists after refusal")
	}
}

// A presigned PUT that never carried its body leaves a 0-byte object the browser believes it
// uploaded. Finalize used to accept it: the Studio picker then drew a broken tile, and the chat
// card offered an asset with nothing behind it (live, 2026-09-19).
func TestServiceFinalizeRefusesAnObjectShorterThanDeclared(t *testing.T) {
	for _, tc := range []struct {
		name     string
		declared int64
		stored   string
	}{
		{name: "empty object", declared: 4, stored: ""},
		{name: "truncated object", declared: 6, stored: "123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100})

			resp, err := svc.Presign(context.Background(), PresignRequest{
				IdentityID:        serviceIdentityID,
				SourceKind:        SourceWeb,
				ThreadID:          "thread-1",
				FileName:          "manual.pdf",
				MIMEType:          "application/pdf",
				DeclaredSizeBytes: tc.declared,
			})
			if err != nil {
				t.Fatalf("Presign() error = %v", err)
			}
			ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
			if _, err := svc.Objects.Put(context.Background(), ref, strings.NewReader(tc.stored), objectstore.PutOptions{
				MIMEType: "application/pdf",
				Size:     int64(len(tc.stored)),
			}); err != nil {
				t.Fatalf("Put object: %v", err)
			}

			updated, err := svc.Finalize(context.Background(), serviceIdentityID, resp.Asset.ID)
			if err == nil {
				t.Fatal("Finalize() succeeded, want a short-object refusal")
			}
			if updated.Status != StatusRefused || updated.ErrorCode != "asset_refused" {
				t.Fatalf("Finalize() updated asset = %#v, want refused asset", updated)
			}
			if _, err := svc.Objects.Head(context.Background(), ref); err == nil {
				t.Fatal("short object still exists after refusal")
			}
		})
	}
}

// Pictures and clips land in their own folder. The bucket is the identity's own and the file
// manager shows it, so a person browsing it used to find one bag of ids: their PDF, their
// screenshot and a generated clip all under `chat/`.
func TestServicePresignPutsMediaInItsOwnFolder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fileName string
		mimeType string
		want     string
	}{
		{name: "a clip", fileName: "clip.mp4", mimeType: "video/mp4", want: "media/"},
		{name: "a picture", fileName: "panel.png", mimeType: "image/png", want: "media/"},
		{name: "a document", fileName: "manual.pdf", mimeType: "application/pdf", want: "chat/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newAssetServiceTestRig(t, Limits{
				MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100, MaxVideoBytes: 100,
			})

			resp, err := svc.Presign(context.Background(), PresignRequest{
				IdentityID:        serviceIdentityID,
				SourceKind:        SourceWeb,
				ThreadID:          "thread-1",
				FileName:          tc.fileName,
				MIMEType:          tc.mimeType,
				DeclaredSizeBytes: 10,
			})
			if err != nil {
				t.Fatalf("Presign() error = %v", err)
			}
			if !strings.HasPrefix(resp.Asset.ObjectKey, tc.want) {
				t.Fatalf("ObjectKey = %q, want the %q folder", resp.Asset.ObjectKey, tc.want)
			}
		})
	}
}
