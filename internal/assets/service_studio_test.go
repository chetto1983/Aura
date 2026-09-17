package assets

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

// A one-pixel PNG: enough for hashAndSniff to agree the bytes are the image the name claims.
var onePixelPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

// readRefusingStore fails and counts every read. A refusal that has already read the object is
// still a refusal, so only a store that reports the read can tell "checked first" from "checked
// after the bytes were fetched" — the distinction the Studio input path exists to make.
type readRefusingStore struct {
	objectstore.Store
	reads int
}

var errUnexpectedRead = errors.New("the object store was read")

func (s *readRefusingStore) Head(context.Context, objectstore.ObjectRef) (objectstore.Attrs, error) {
	s.reads++
	return objectstore.Attrs{}, errUnexpectedRead
}

func (s *readRefusingStore) Get(context.Context, objectstore.ObjectRef) (io.ReadCloser, objectstore.Attrs, error) {
	s.reads++
	return nil, objectstore.Attrs{}, errUnexpectedRead
}

func (s *readRefusingStore) GetFrom(context.Context, objectstore.ObjectRef, int64) (io.ReadCloser, error) {
	s.reads++
	return nil, errUnexpectedRead
}

// presignAndPut presigns one upload and puts its bytes, returning the presigned asset. declared
// is what the caller PROMISED, which Presign checks; the bytes are what actually landed, which
// only the accept path measures. They differ when the test is about that gap.
func presignAndPut(t *testing.T, svc *Service, name, mimeType string, body []byte, declared int64) Asset {
	t.Helper()
	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID: serviceIdentityID, SourceKind: SourceWeb, ThreadID: "thread-1",
		FileName: name, MIMEType: mimeType, DeclaredSizeBytes: declared,
	})
	if err != nil {
		t.Fatalf("Presign(%s) error = %v", name, err)
	}
	ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, bytes.NewReader(body),
		objectstore.PutOptions{MIMEType: mimeType, Size: int64(len(body))}); err != nil {
		t.Fatalf("Put %s: %v", name, err)
	}
	return resp.Asset
}

func TestFinalizeUnprocessedAcceptsWithoutEnqueueing(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue

	presigned := presignAndPut(t, svc, "frame.png", "image/png", onePixelPNG, int64(len(onePixelPNG)))
	accepted, err := svc.FinalizeUnprocessed(context.Background(), serviceIdentityID, presigned.ID, ModalityImage)
	if err != nil {
		t.Fatalf("FinalizeUnprocessed() error = %v", err)
	}
	if accepted.Status != StatusAccepted {
		t.Fatalf("status = %q, want %q", accepted.Status, StatusAccepted)
	}
	// The size, the hash and the sniffed type are what the shared accept path produces: a
	// shortcut that marked the row accepted from the declared values would leave the hash empty.
	if accepted.SizeBytes != int64(len(onePixelPNG)) || accepted.MIMEType != "image/png" {
		t.Fatalf("accepted asset = %#v", accepted)
	}
	if accepted.ContentHash == "" {
		t.Fatal("content hash is empty: the bytes were never hashed, so the accept path was skipped")
	}
	if queue.calls != 0 {
		t.Fatalf("EnqueueAssetProcessing called %d time(s), want none: a generation input is never processed", queue.calls)
	}
}

// The same limits Finalize enforces: an input to a generation is not exempt from them just
// because nothing will process it.
func TestFinalizeUnprocessedEnforcesTheSizeLimit(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100, MaxImageBytes: 4, MaxAudioBytes: 100})
	// Presign validates the DECLARED size, so the refusal under test has to come from the
	// measured one: the promise fits, the bytes do not.
	presigned := presignAndPut(t, svc, "frame.png", "image/png", onePixelPNG, 4)
	refused, err := svc.FinalizeUnprocessed(context.Background(), serviceIdentityID, presigned.ID, ModalityImage)
	if err == nil {
		t.Fatal("FinalizeUnprocessed() succeeded, want the oversized refusal")
	}
	if refused.Status != StatusRefused {
		t.Fatalf("status = %q, want %q", refused.Status, StatusRefused)
	}
}

func TestFinalizeUnprocessedRefusesAnotherModality(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue
	presigned := presignAndPut(t, svc, "manual.pdf", "application/pdf", []byte("%PDF test"), 9)
	// Swapped in only now, so the upload above still lands: from here every read is a failure
	// the test can see.
	spy := &readRefusingStore{Store: svc.Objects}
	svc.Objects = spy

	if _, err := svc.FinalizeUnprocessed(context.Background(), serviceIdentityID, presigned.ID, ModalityImage); !errors.Is(err, ErrWrongModality) {
		t.Fatalf("FinalizeUnprocessed() error = %v, want ErrWrongModality", err)
	}
	if spy.reads != 0 {
		t.Fatalf("the object store was read %d time(s): the modality is checked before a byte is fetched", spy.reads)
	}
	// The row is untouched too, so the upload is still there for the use it was presigned for.
	asset, err := store.GetForIdentity(context.Background(), presigned.ID, serviceIdentityID)
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

// The picker asks for a page; the service decides what a page may be, because the SQL LIMIT
// below it takes whatever it is given.
func TestListRecentImagesClampsTheLimit(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{MaxImageBytes: 100})
	for _, tc := range []struct{ asked, want int }{
		{-5, 1}, {0, 1}, {1, 1}, {30, 30}, {recentImagesMax, recentImagesMax}, {1000, recentImagesMax},
	} {
		if _, err := svc.ListRecentImages(context.Background(), serviceIdentityID, tc.asked); err != nil {
			t.Fatalf("ListRecentImages(%d) error = %v", tc.asked, err)
		}
		if store.lastImageLimit != tc.want {
			t.Fatalf("ListRecentImages(%d) asked the store for %d, want %d", tc.asked, store.lastImageLimit, tc.want)
		}
	}
}

func TestListRecentImagesNeedsAStore(t *testing.T) {
	svc := &Service{}
	if _, err := svc.ListRecentImages(context.Background(), serviceIdentityID, 10); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("ListRecentImages() error = %v, want the unconfigured refusal", err)
	}
}
