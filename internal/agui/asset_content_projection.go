package agui

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/llm"
)

type assetContentPartLoader struct {
	assets   AssetService
	threadID string
	allowed  map[string]bool
}

var _ llm.ContentPartLoader = assetContentPartLoader{}

func (l assetContentPartLoader) LoadContentPart(ctx context.Context, _ string, ownerID, id string) (llm.VerifiedContentPart, error) {
	if l.assets == nil || !l.allowed[id] || strings.TrimSpace(ownerID) == "" {
		return llm.VerifiedContentPart{}, fmt.Errorf("asset content reference is not authorized")
	}
	rc, asset, err := l.assets.OpenForIdentity(ctx, id, ownerID)
	if err != nil {
		return llm.VerifiedContentPart{}, err
	}
	defer func() { _ = rc.Close() }()
	if asset.ID != id || (asset.ThreadID != "" && asset.ThreadID != l.threadID) {
		return llm.VerifiedContentPart{}, fmt.Errorf("asset content scope changed")
	}
	if asset.Modality != assets.ModalityImage && asset.Modality != assets.ModalityAudio {
		return llm.VerifiedContentPart{}, fmt.Errorf("asset modality %q is not native media", asset.Modality)
	}
	if asset.SizeBytes <= 0 || strings.TrimSpace(asset.ContentHash) == "" {
		return llm.VerifiedContentPart{}, fmt.Errorf("asset has no accepted size or digest")
	}
	body, err := io.ReadAll(io.LimitReader(rc, asset.SizeBytes+1))
	if err != nil {
		return llm.VerifiedContentPart{}, err
	}
	if int64(len(body)) != asset.SizeBytes {
		return llm.VerifiedContentPart{}, fmt.Errorf("asset size changed: got %d want %d", len(body), asset.SizeBytes)
	}
	digest := sha256.Sum256(body)
	wantDigest, err := hex.DecodeString(strings.TrimSpace(asset.ContentHash))
	if err != nil || len(wantDigest) != sha256.Size || subtle.ConstantTimeCompare(digest[:], wantDigest) != 1 {
		return llm.VerifiedContentPart{}, fmt.Errorf("asset digest changed")
	}
	return llm.VerifiedContentPart{
		ID:           asset.ID,
		MIMEType:     asset.MIMEType,
		Digest:       hex.EncodeToString(digest[:]),
		FallbackText: asset.Summary,
		Bytes:        body,
	}, nil
}
