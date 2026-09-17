package assets

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

// A one-pixel PNG: enough for hashAndSniff to agree the bytes are the image the name claims.
var onePixelPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func TestFinalizeUnprocessedAcceptsWithoutEnqueueing(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue

	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID: serviceIdentityID, SourceKind: SourceWeb, ThreadID: "thread-1",
		FileName: "frame.png", MIMEType: "image/png", DeclaredSizeBytes: int64(len(onePixelPNG)),
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, bytes.NewReader(onePixelPNG),
		objectstore.PutOptions{MIMEType: "image/png", Size: int64(len(onePixelPNG))}); err != nil {
		t.Fatalf("Put object: %v", err)
	}

	accepted, err := svc.FinalizeUnprocessed(context.Background(), serviceIdentityID, resp.Asset.ID, ModalityImage)
	if err != nil {
		t.Fatalf("FinalizeUnprocessed() error = %v", err)
	}
	if accepted.Status != StatusAccepted {
		t.Fatalf("status = %q, want %q", accepted.Status, StatusAccepted)
	}
	if accepted.SizeBytes != int64(len(onePixelPNG)) || accepted.MIMEType != "image/png" {
		t.Fatalf("accepted asset = %#v", accepted)
	}
	if queue.calls != 0 {
		t.Fatalf("EnqueueAssetProcessing called %d time(s), want none: a generation input is never processed", queue.calls)
	}
}

func TestFinalizeUnprocessedRefusesAnotherModality(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue

	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID: serviceIdentityID, SourceKind: SourceWeb, ThreadID: "thread-1",
		FileName: "manual.pdf", MIMEType: "application/pdf", DeclaredSizeBytes: 9,
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, strings.NewReader("%PDF test"),
		objectstore.PutOptions{MIMEType: "application/pdf", Size: 9}); err != nil {
		t.Fatalf("Put object: %v", err)
	}

	if _, err := svc.FinalizeUnprocessed(context.Background(), serviceIdentityID, resp.Asset.ID, ModalityImage); !errors.Is(err, ErrWrongModality) {
		t.Fatalf("FinalizeUnprocessed() error = %v, want ErrWrongModality", err)
	}
	// Refused before a byte is read: the row is untouched, so nothing was headed, hashed or
	// sniffed, and the upload is still there for the use it was actually presigned for.
	asset, err := store.GetForIdentity(context.Background(), resp.Asset.ID, serviceIdentityID)
	if err != nil {
		t.Fatalf("GetForIdentity() error = %v", err)
	}
	if asset.Status != StatusPresigned {
		t.Fatalf("status = %q, want it untouched at %q", asset.Status, StatusPresigned)
	}
	if queue.calls != 0 {
		t.Fatalf("EnqueueAssetProcessing called %d time(s), want none", queue.calls)
	}
}
