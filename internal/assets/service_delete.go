package assets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
)

// Delete retires an asset in three steps: the row is marked deleting, which hides it at once,
// the object leaves the owner's bucket, and the row leaves the table. Only the first step can
// fail the call. When a later one fails the row stays deleting, the failure is logged, and the
// retention sweep finishes it.
//
// Before this, the delete stopped at the first step and discarded the object's error: measured
// on the lab VM on 2026-09-28, 258 rows sat in deleting with nothing ever taking them further.
func (s *Service) Delete(ctx context.Context, identityID, assetID string) (Asset, error) {
	if s.Store == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	asset, err := s.Store.Delete(ctx, assetID, identityID)
	if err != nil {
		return Asset{}, err
	}
	if err := s.finishDelete(context.WithoutCancel(ctx), asset); err != nil {
		slog.Warn("aura assets: delete left for the retention sweep", "asset_id", asset.ID, "err", err)
	}
	return asset, nil
}

// finishDelete removes the object of a deleting row and then the row. A key already gone from
// the bucket counts as removed, so a retry after a partial failure converges.
func (s *Service) finishDelete(ctx context.Context, asset Asset) error {
	if err := s.removeObject(ctx, asset); err != nil {
		return err
	}
	return s.Store.Finalize(ctx, asset.ID, asset.IdentityID)
}

// removeObject deletes the asset's bytes from the store of its OWNER, never from whoever
// happens to be asking: the retention sweep runs with no principal at all.
func (s *Service) removeObject(ctx context.Context, asset Asset) error {
	if asset.ObjectBucket == "" || asset.ObjectKey == "" {
		return nil
	}
	objects, _, err := s.objectsFor(identityctx.WithIdentityID(ctx, asset.IdentityID))
	if err != nil {
		return err
	}
	if objects == nil {
		return errors.New("asset object store is not configured")
	}
	if err := objects.Delete(ctx, assetRef(asset)); err != nil && !objectstore.IsNotFound(err) {
		return err
	}
	return nil
}
