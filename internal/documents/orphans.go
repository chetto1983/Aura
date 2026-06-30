package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/objectstore"
)

// StorageOrphanCleanupConfirmation is the exact confirmation phrase for destructive cleanup.
const StorageOrphanCleanupConfirmation = "DELETE ORPHAN OBJECTS"

// StorageLedger lists object ledger rows recorded by the document control plane.
type StorageLedger interface {
	ListStorageObjects(ctx context.Context, bucket, prefix string) ([]objectstore.ObjectRef, error)
}

// ObjectListerDeleter is the objectstore surface needed for orphan cleanup.
type ObjectListerDeleter interface {
	List(context.Context, objectstore.ListRequest) ([]objectstore.ObjectInfo, error)
	Delete(context.Context, objectstore.ObjectRef) error
}

// StorageOrphanService compares object storage contents with the storage ledger.
type StorageOrphanService struct {
	Objects ObjectListerDeleter
	Ledger  StorageLedger
}

// StorageOrphanRequest scopes an orphan scan.
type StorageOrphanRequest struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
}

// StorageOrphanCleanupRequest executes a previously proven dry-run.
type StorageOrphanCleanupRequest struct {
	Bucket      string `json:"bucket"`
	Prefix      string `json:"prefix"`
	DryRunToken string `json:"dry_run_token"`
	Confirm     string `json:"confirm"`
}

// StorageOrphanReport is returned by dry-run and cleanup.
type StorageOrphanReport struct {
	Bucket      string                   `json:"bucket"`
	Prefix      string                   `json:"prefix"`
	DryRunToken string                   `json:"dry_run_token"`
	Orphans     []objectstore.ObjectInfo `json:"orphans"`
	Deleted     int                      `json:"deleted"`
}

// DryRun lists unledgered objects without mutating storage.
func (s *StorageOrphanService) DryRun(ctx context.Context, req StorageOrphanRequest) (StorageOrphanReport, error) {
	if s == nil || s.Objects == nil || s.Ledger == nil {
		return StorageOrphanReport{}, fmt.Errorf("storage orphan service is not configured")
	}
	if strings.TrimSpace(req.Bucket) == "" {
		return StorageOrphanReport{}, fmt.Errorf("bucket is required")
	}
	objects, err := s.Objects.List(ctx, objectstore.ListRequest{Bucket: req.Bucket, Prefix: req.Prefix})
	if err != nil {
		return StorageOrphanReport{}, err
	}
	ledgerRefs, err := s.Ledger.ListStorageObjects(ctx, req.Bucket, req.Prefix)
	if err != nil {
		return StorageOrphanReport{}, err
	}
	ledgered := make(map[objectstore.ObjectRef]struct{}, len(ledgerRefs))
	for _, ref := range ledgerRefs {
		ledgered[ref] = struct{}{}
	}
	orphans := make([]objectstore.ObjectInfo, 0)
	for _, item := range objects {
		if _, ok := ledgered[item.Ref]; ok {
			continue
		}
		orphans = append(orphans, item)
	}
	slices.SortFunc(orphans, func(a, b objectstore.ObjectInfo) int {
		return strings.Compare(a.Ref.Bucket+"/"+a.Ref.Key, b.Ref.Bucket+"/"+b.Ref.Key)
	})
	return StorageOrphanReport{
		Bucket:      req.Bucket,
		Prefix:      req.Prefix,
		DryRunToken: storageOrphanToken(req.Bucket, req.Prefix, orphans),
		Orphans:     orphans,
	}, nil
}

// Cleanup deletes the exact object set proven by a matching dry-run token.
func (s *StorageOrphanService) Cleanup(ctx context.Context, req StorageOrphanCleanupRequest) (StorageOrphanReport, error) {
	if req.Confirm != StorageOrphanCleanupConfirmation {
		return StorageOrphanReport{}, fmt.Errorf("confirmation must be %q", StorageOrphanCleanupConfirmation)
	}
	report, err := s.DryRun(ctx, StorageOrphanRequest{Bucket: req.Bucket, Prefix: req.Prefix})
	if err != nil {
		return StorageOrphanReport{}, err
	}
	if req.DryRunToken == "" || req.DryRunToken != report.DryRunToken {
		return StorageOrphanReport{}, fmt.Errorf("dry-run token mismatch")
	}
	for _, orphan := range report.Orphans {
		if err := s.Objects.Delete(ctx, orphan.Ref); err != nil {
			return report, err
		}
		report.Deleted++
	}
	return report, nil
}

func storageOrphanToken(bucket, prefix string, orphans []objectstore.ObjectInfo) string {
	h := sha256.New()
	_, _ = h.Write([]byte(bucket))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(prefix))
	for _, orphan := range orphans {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(orphan.Ref.Bucket))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(orphan.Ref.Key))
	}
	return hex.EncodeToString(h.Sum(nil))
}
