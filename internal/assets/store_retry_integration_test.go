//go:build db_integration

package assets

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Retry re-arms only an asset whose processing did not succeed. Until 2026-10-26 the cockpit
// retry route went through SetStatus, which re-arms any status: a complete asset went back to
// accepted and was processed again. ResetForRetry is the guarded statement
// (ResetAssetForIngestionRetry), measured here against Postgres itself.
func TestStoreResetForRetryRearmsOnlyAnUnsuccessfulAsset(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := NewStore(pool)
	now := time.Now().UnixNano()

	for _, status := range []Status{StatusFailed, StatusRefused, StatusCanceled} {
		t.Run("rearms "+string(status), func(t *testing.T) {
			key := fmt.Sprintf("chat/%d-%s-retry.pdf", now, status)
			asset := createAcceptedAsset(t, ctx, store, deleteFixture(localIdentityID, "retry-thread", key, ScopeThread))
			t.Cleanup(func() { retireAsset(t, store, asset.ID, localIdentityID) })
			if _, err := store.SetStatus(ctx, asset.ID, localIdentityID, status, "boom", "it broke"); err != nil {
				t.Fatalf("SetStatus %s: %v", status, err)
			}
			got, err := store.ResetForRetry(ctx, asset.ID, localIdentityID)
			if err != nil {
				t.Fatalf("ResetForRetry from %s: %v", status, err)
			}
			if got.Status != StatusAccepted || got.ErrorCode != "" || got.ErrorMessage != "" || !got.ProcessedAt.IsZero() {
				t.Fatalf("ResetForRetry from %s = %+v, want accepted with errors and processed_at cleared", status, got)
			}
		})
	}
	for _, status := range []Status{StatusComplete, StatusProcessing, StatusAccepted} {
		t.Run("refuses "+string(status), func(t *testing.T) {
			key := fmt.Sprintf("chat/%d-%s-noretry.pdf", now, status)
			asset := createAcceptedAsset(t, ctx, store, deleteFixture(localIdentityID, "retry-thread", key, ScopeThread))
			t.Cleanup(func() { retireAsset(t, store, asset.ID, localIdentityID) })
			if _, err := store.SetStatus(ctx, asset.ID, localIdentityID, status, "", ""); err != nil {
				t.Fatalf("SetStatus %s: %v", status, err)
			}
			if _, err := store.ResetForRetry(ctx, asset.ID, localIdentityID); !errors.Is(err, ErrNotRetryable) {
				t.Fatalf("ResetForRetry from %s = %v, want ErrNotRetryable", status, err)
			}
			still, err := store.GetForIdentity(ctx, asset.ID, localIdentityID)
			if err != nil || still.Status != status {
				t.Fatalf("after a refused retry the asset is %v (%v), want it left %s", still.Status, err, status)
			}
		})
	}
}
