package agui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

// ObjectStoreExportDestination persists archives outside the request lifetime.
// Keys are owner-scoped and include the immutable expiry used by the resumable URL.
type ObjectStoreExportDestination struct {
	objects objectstore.Store
	bucket  string
}

// NewObjectStoreExportDestination constructs durable export storage in bucket.
func NewObjectStoreExportDestination(objects objectstore.Store, bucket string) *ObjectStoreExportDestination {
	if objects == nil || bucket == "" {
		return nil
	}
	return &ObjectStoreExportDestination{objects: objects, bucket: bucket}
}

// Publish atomically stores one owner-scoped archive for the requested retention window.
func (d *ObjectStoreExportDestination) Publish(ctx context.Context, ownerID, exportID string, body io.Reader, size int64, _ string, expiresAt time.Time) error {
	ref, err := d.ref(ownerID, exportID, expiresAt)
	if err != nil || size < 0 {
		return errors.New("invalid durable owner export")
	}
	attrs, err := d.objects.Put(ctx, ref, body, objectstore.PutOptions{MIMEType: "application/zip", Size: size})
	if err != nil {
		return err
	}
	if attrs.SizeBytes != size {
		return errors.New("durable owner export size mismatch")
	}
	return nil
}

// Open returns an unexpired archive only for the owner encoded into its object key.
func (d *ObjectStoreExportDestination) Open(ctx context.Context, ownerID, exportID string, expiresAt time.Time) (io.ReadCloser, error) {
	if !expiresAt.After(time.Now().UTC()) {
		return nil, ErrOwnerExportNotFound
	}
	ref, err := d.ref(ownerID, exportID, expiresAt)
	if err != nil {
		return nil, ErrOwnerExportNotFound
	}
	body, _, err := d.objects.Get(ctx, ref)
	if err != nil {
		return nil, ErrOwnerExportNotFound
	}
	return body, nil
}

func (d *ObjectStoreExportDestination) ref(ownerID, exportID string, expiresAt time.Time) (objectstore.ObjectRef, error) {
	if d == nil || d.objects == nil || d.bucket == "" || expiresAt.IsZero() {
		return objectstore.ObjectRef{}, errors.New("owner export destination unavailable")
	}
	owner, err := uuid.Parse(ownerID)
	if err != nil {
		return objectstore.ObjectRef{}, err
	}
	export, err := uuid.Parse(exportID)
	if err != nil {
		return objectstore.ObjectRef{}, err
	}
	key := fmt.Sprintf("identity/%s/owner-export/%s/%d.zip", owner, export, expiresAt.Unix())
	return objectstore.ObjectRef{Bucket: d.bucket, Key: key}, nil
}
