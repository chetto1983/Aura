package assets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
)

// Finalize accepts an upload and queues its modality's processor: the summary, transcript or
// index entry that makes it knowledge. A queue failure leaves the asset failed, not accepted.
func (s *Service) Finalize(ctx context.Context, identityID, assetID string) (Asset, error) {
	asset, err := s.accept(ctx, identityID, assetID)
	if err != nil {
		return asset, err
	}
	if err := s.enqueueProcessing(ctx, asset); err != nil {
		updated, _ := s.Store.SetStatus(ctx, asset.ID, identityID, StatusFailed, "processing_enqueue_failed", err.Error())
		return updated, err
	}
	return asset, nil
}

// ErrWrongModality refuses an asset finalized for a use that needs another modality.
var ErrWrongModality = errors.New("assets: the asset is not of the expected modality")

// FinalizeUnprocessed accepts an upload that is the input of one generation rather than
// knowledge: the same checks as Finalize, and no processing, so no vision summary is paid for
// and nothing is filed for the index. The asset must be of modality.
func (s *Service) FinalizeUnprocessed(ctx context.Context, identityID, assetID string, modality Modality) (Asset, error) {
	return s.accept(ctx, identityID, assetID, modality)
}

// FinalizeMedia accepts an editor source: a picture, a clip or a sound the operator will cut.
// Same checks as Finalize and no processing. For audio the processor is speech-to-text, and a
// music bed transcribed on the appliance's CPU on every upload says nothing to anyone.
func (s *Service) FinalizeMedia(ctx context.Context, identityID, assetID string) (Asset, error) {
	return s.accept(ctx, identityID, assetID, ModalityImage, ModalityVideo, ModalityAudio)
}

// storedSizeMatchesUpload compares the object the store actually holds with the size the upload
// declared. A presigned PUT that never carried its body leaves a 0-byte object the browser
// believes it uploaded, and finalize used to accept it: a broken tile in the Studio picker and a
// chat card over nothing (live, 2026-09-19). A declared 0 still refuses an empty object.
func storedSizeMatchesUpload(declared, stored int64) error {
	if stored == 0 {
		return fmt.Errorf("%w: the uploaded object is empty", ErrAssetIncomplete)
	}
	if declared > 0 && stored != declared {
		return fmt.Errorf("%w: %d bytes stored, %d declared", ErrAssetIncomplete, stored, declared)
	}
	return nil
}

// refuse marks the asset refused with the reason, drops the object no one will read, and hands
// the caller the same error — the one exit every acceptance check takes. A drop that fails is
// logged and left: the refused row keeps the key, so retiring the row removes the object.
func (s *Service) refuse(ctx context.Context, objects objectstore.Store, ref objectstore.ObjectRef, asset Asset, identityID string, cause error) (Asset, error) {
	updated, _ := s.Store.SetStatus(ctx, asset.ID, identityID, StatusRefused, "asset_refused", cause.Error())
	if err := objects.Delete(context.WithoutCancel(ctx), ref); err != nil && !objectstore.IsNotFound(err) {
		slog.Warn("aura assets: a refused upload kept its object", "asset_id", asset.ID, "err", err)
	}
	return updated, cause
}

// accept takes an uploaded object through every check that makes it an asset — it exists and
// is within the limits — and stops at accepted. No allowed modality accepts any; otherwise a
// mismatch is refused before a byte is read. The recorded modality may be the client's hint, so
// the name and declared type must also infer an allowed one: the ingest walker reads the name,
// and a manual.pdf hinted as a sound would be filed as media and indexed anyway.
func (s *Service) accept(ctx context.Context, identityID, assetID string, allowed ...Modality) (Asset, error) {
	if s.Store == nil || s.Objects == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	asset, err := s.Store.GetForIdentity(ctx, assetID, identityID)
	if err != nil {
		return Asset{}, err
	}
	if len(allowed) > 0 && (!slices.Contains(allowed, asset.Modality) ||
		!slices.Contains(allowed, InferModality(asset.FileName, asset.MIMEType))) {
		return Asset{}, ErrWrongModality
	}
	objects, _, err := s.objectsFor(identityctx.WithIdentityID(ctx, identityID))
	if err != nil {
		return Asset{}, err
	}
	ref := assetRef(asset)
	attrs, err := objects.Head(ctx, ref)
	if err != nil {
		_, _ = s.Store.SetStatus(ctx, asset.ID, identityID, StatusFailed, "object_missing", "uploaded object was not found")
		return Asset{}, err
	}
	if err = s.Limits.Validate(asset.Modality, asset.FileName, attrs.SizeBytes); err != nil {
		return s.refuse(ctx, objects, ref, asset, identityID, err)
	}
	if err = storedSizeMatchesUpload(asset.DeclaredSizeBytes, attrs.SizeBytes); err != nil {
		return s.refuse(ctx, objects, ref, asset, identityID, err)
	}
	asset, err = s.Store.MarkUploaded(ctx, asset.ID, identityID, attrs.SizeBytes, attrs.ETag)
	if err != nil {
		return Asset{}, err
	}
	hash, sniffed, err := s.hashAndSniff(ctx, objects, ref, asset.FileName)
	if err != nil {
		return Asset{}, err
	}
	return s.Store.MarkAccepted(ctx, asset.ID, identityID, attrs.SizeBytes, hash, sniffed)
}
