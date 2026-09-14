package main

import (
	"context"
	"errors"
	"io"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
)

// mediaAssetAdapter reads identity-owned assets for generation references. Ownership is
// enforced by OpenForIdentity before the object store is touched; mediagen.LoadReferences
// then checks the modality and bounds the read.
type mediaAssetAdapter struct{ svc *assets.Service }

var _ mediagen.ReferenceReader = mediaAssetAdapter{}

func (a mediaAssetAdapter) Open(ctx context.Context, identityID, assetID string) (io.ReadCloser, mediagen.ReferenceMeta, error) {
	if a.svc == nil {
		return nil, mediagen.ReferenceMeta{}, errors.New("asset service is not configured")
	}
	rc, asset, err := a.svc.OpenForIdentity(ctx, assetID, identityID)
	if err != nil {
		if rc != nil {
			_ = rc.Close()
		}
		return nil, mediagen.ReferenceMeta{}, err
	}
	return rc, mediagen.ReferenceMeta{
		MIMEType:  asset.MIMEType,
		Modality:  string(asset.Modality),
		SizeBytes: asset.SizeBytes,
	}, nil
}
