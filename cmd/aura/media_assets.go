package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
)

// mediaAssetAdapter is the media path's view of the asset service. It reads identity-owned
// assets for generation references, with ownership enforced by OpenForIdentity before the
// object store is touched and the modality and size checked by mediagen.LoadReferences, and it
// stores the clips the video watcher collects.
type mediaAssetAdapter struct{ svc *assets.Service }

var (
	_ mediagen.ReferenceReader = mediaAssetAdapter{}
	_ mediagen.VideoAssets     = mediaAssetAdapter{}
)

var errNoAssetService = errors.New("asset service is not configured")

// videoExtensions is keyed by the type http.DetectContentType sniffs from a clip's bytes: the
// provider's Content-Type is never trusted, and assets accepts video only as MP4 or WebM.
var videoExtensions = map[string]string{"video/mp4": ".mp4", "video/webm": ".webm"}

func (a mediaAssetAdapter) Open(ctx context.Context, identityID, assetID string) (io.ReadCloser, mediagen.ReferenceMeta, error) {
	if a.svc == nil {
		return nil, mediagen.ReferenceMeta{}, errNoAssetService
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

// IngestVideo stores a job's clip as the owner's accepted agent video in the job's
// conversation, the asset mediagen.Store.Complete accepts. Its source reference is stable per
// job, so ingesting the job again returns the stored asset. No tool call is recorded: the call
// that later wins the delivery claim binds itself to the asset.
func (a mediaAssetAdapter) IngestVideo(ctx context.Context, job mediagen.Job, data []byte) (string, error) {
	if a.svc == nil {
		return "", errNoAssetService
	}
	mimeType := http.DetectContentType(data)
	extension, ok := videoExtensions[mimeType]
	if !ok {
		return "", &mediagen.Error{Code: "unsupported", Message: "The generated video is neither MP4 nor WebM."}
	}
	asset, err := a.svc.IngestAgentFile(ctx, assets.AgentIngestRequest{
		IdentityID: job.IdentityID,
		ThreadID:   job.ConversationID,
		SourceRef:  "media-job:" + job.ID,
		FileName:   "generated" + extension,
		MIMEType:   mimeType,
		Modality:   assets.ModalityVideo,
		SizeBytes:  int64(len(data)),
		Reader:     bytes.NewReader(data),
	})
	if err != nil {
		return "", err
	}
	return asset.ID, nil
}
