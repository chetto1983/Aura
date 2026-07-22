package agui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

const (
	ownerExportExpiryPrefix = "owner-export-expiry/"
	ownerExportSweepBatch   = 128
)

// ObjectStoreExportDestination persists archives outside the request lifetime.
// Keys are owner-scoped and include the immutable expiry used by the resumable URL.
type ObjectStoreExportDestination struct {
	objects objectstore.Store
	bucket  string
	now     func() time.Time
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
	record, err := d.expiryRecordRef(ownerID, exportID, expiresAt)
	if err != nil {
		return errors.New("invalid durable owner export")
	}
	// Write the durable expiry record first. A crash before the archive Put leaves a
	// harmless record that the retryable sweep will eventually finalize; the inverse
	// order could strand an untracked archive forever.
	if _, err := d.objects.Put(ctx, record, strings.NewReader(ref.Key), objectstore.PutOptions{
		MIMEType: "text/plain", Size: int64(len(ref.Key)),
	}); err != nil {
		return err
	}
	attrs, err := d.objects.Put(ctx, ref, body, objectstore.PutOptions{MIMEType: "application/zip", Size: size})
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		cleanupErr := d.objects.Delete(cleanupCtx, record)
		return errors.Join(err, cleanupErr)
	}
	if attrs.SizeBytes != size {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(
			errors.New("durable owner export size mismatch"),
			d.objects.Delete(cleanupCtx, ref),
			d.objects.Delete(cleanupCtx, record),
		)
	}
	return nil
}

// Open returns an unexpired archive only for the owner encoded into its object key.
func (d *ObjectStoreExportDestination) Open(ctx context.Context, ownerID, exportID string, expiresAt time.Time) (io.ReadCloser, error) {
	if !expiresAt.After(d.clock()) {
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

// SweepExpired physically deletes at most one bounded batch of expired archives.
// The expiry record is deleted only after the archive, so any failure is naturally
// retryable on the next retention sweep without losing the object reference.
func (d *ObjectStoreExportDestination) SweepExpired(ctx context.Context, now time.Time) (int, error) {
	if d == nil || d.objects == nil || d.bucket == "" {
		return 0, errors.New("owner export destination unavailable")
	}
	records, err := d.objects.List(ctx, objectstore.ListRequest{
		Bucket: d.bucket, Prefix: ownerExportExpiryPrefix, Limit: ownerExportSweepBatch,
	})
	if err != nil {
		return 0, fmt.Errorf("list owner export expiry records: %w", err)
	}

	now = now.UTC()
	deleted := 0
	var sweepErrs []error
	for _, record := range records {
		archive, expiresAt, parseErr := d.parseExpiryRecord(record.Ref)
		if parseErr != nil {
			sweepErrs = append(sweepErrs, parseErr)
			continue
		}
		if expiresAt.After(now) {
			break
		}
		if err := d.objects.Delete(ctx, archive); err != nil {
			sweepErrs = append(sweepErrs, fmt.Errorf("delete owner export %s: %w", archive.Key, err))
			continue
		}
		if err := d.objects.Delete(ctx, record.Ref); err != nil {
			sweepErrs = append(sweepErrs, fmt.Errorf("finalize owner export %s: %w", record.Ref.Key, err))
			continue
		}
		deleted++
	}
	return deleted, errors.Join(sweepErrs...)
}

func (d *ObjectStoreExportDestination) ref(ownerID, exportID string, expiresAt time.Time) (objectstore.ObjectRef, error) {
	if d == nil || d.objects == nil || d.bucket == "" || expiresAt.IsZero() || expiresAt.Unix() < 0 {
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

func (d *ObjectStoreExportDestination) expiryRecordRef(ownerID, exportID string, expiresAt time.Time) (objectstore.ObjectRef, error) {
	archive, err := d.ref(ownerID, exportID, expiresAt)
	if err != nil {
		return objectstore.ObjectRef{}, err
	}
	owner := strings.Split(archive.Key, "/")[1]
	export := strings.Split(archive.Key, "/")[3]
	key := fmt.Sprintf("%s%020d/%s/%s.ref", ownerExportExpiryPrefix, expiresAt.Unix(), owner, export)
	return objectstore.ObjectRef{Bucket: d.bucket, Key: key}, nil
}

func (d *ObjectStoreExportDestination) parseExpiryRecord(record objectstore.ObjectRef) (objectstore.ObjectRef, time.Time, error) {
	if record.Bucket != d.bucket || !strings.HasPrefix(record.Key, ownerExportExpiryPrefix) {
		return objectstore.ObjectRef{}, time.Time{}, errors.New("invalid owner export expiry record")
	}
	parts := strings.Split(strings.TrimPrefix(record.Key, ownerExportExpiryPrefix), "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".ref") {
		return objectstore.ObjectRef{}, time.Time{}, fmt.Errorf("invalid owner export expiry record %q", record.Key)
	}
	expiresUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expiresUnix < 0 {
		return objectstore.ObjectRef{}, time.Time{}, fmt.Errorf("invalid owner export expiry record %q", record.Key)
	}
	owner, ownerErr := uuid.Parse(parts[1])
	export, exportErr := uuid.Parse(strings.TrimSuffix(parts[2], ".ref"))
	if ownerErr != nil || exportErr != nil {
		return objectstore.ObjectRef{}, time.Time{}, fmt.Errorf("invalid owner export expiry record %q", record.Key)
	}
	expiresAt := time.Unix(expiresUnix, 0).UTC()
	archive, err := d.ref(owner.String(), export.String(), expiresAt)
	if err != nil {
		return objectstore.ObjectRef{}, time.Time{}, fmt.Errorf("invalid owner export expiry record %q", record.Key)
	}
	return archive, expiresAt, nil
}

func (d *ObjectStoreExportDestination) clock() time.Time {
	if d != nil && d.now != nil {
		return d.now().UTC()
	}
	return time.Now().UTC()
}
