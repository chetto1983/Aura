package mediagen

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// AssetModality is the kind of identity-owned asset OpenOwned accepts.
type AssetModality string

// The two owned asset kinds media generation reads: the images a request refers to, and the
// clip a finished video job delivers.
const (
	AssetImage AssetModality = "image"
	AssetVideo AssetModality = "video"
)

// ReferenceMeta is what a ReferenceReader reports about one asset before its
// bytes are read: the declared MIME type and modality, checked before the
// stream is bounded and read, and the declared size, checked before that.
type ReferenceMeta struct {
	MIMEType  string
	Modality  string
	SizeBytes int64
}

// ReferenceReader opens one identity-owned asset by ID. The composition root
// implements it over assets.Service; this package only defines the port and
// tests LoadReferences against fakes.
type ReferenceReader interface {
	Open(ctx context.Context, identityID, assetID string) (io.ReadCloser, ReferenceMeta, error)
}

// LoadReferences resolves ids into image_url references for an image or
// video generation request, in the given order. Every id must resolve for
// the call to proceed: a partial result would silently drop a reference the
// caller asked for by name. A caller building a first-frame VideoRequest
// wraps the single returned ImageReference into a FrameReference, adding
// FrameType "first_frame" or "last_frame"; LoadReferences itself is agnostic
// to how its output is used.
func LoadReferences(ctx context.Context, reader ReferenceReader, owner string, ids []string, maxBytes int64) ([]ImageReference, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	refs := make([]ImageReference, 0, len(ids))
	for _, id := range ids {
		ref, err := loadReference(ctx, reader, owner, id, maxBytes)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func loadReference(ctx context.Context, reader ReferenceReader, owner, id string, maxBytes int64) (ImageReference, error) {
	rc, meta, err := OpenOwned(ctx, reader, owner, id, AssetImage, maxBytes)
	if err != nil {
		return ImageReference{}, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return ImageReference{}, err
	}
	dataURL := "data:" + meta.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(data)
	return ImageReference{Type: "image_url", ImageURL: ImageURL{URL: dataURL}}, nil
}

// OpenOwned opens the identity's asset id through reader. Before any byte is read it refuses an
// asset of another modality, by its declared modality and its MIME type alike, and one declaring
// more than maxBytes. The reader it returns fails with too_large past maxBytes, so a declared
// size that understates the stream is still bounded. The caller closes it.
func OpenOwned(ctx context.Context, reader ReferenceReader, owner, id string, modality AssetModality, maxBytes int64) (io.ReadCloser, ReferenceMeta, error) {
	if reader == nil {
		return nil, ReferenceMeta{}, fmt.Errorf("mediagen: no reference reader is configured")
	}
	if err := ValidByteLimit(maxBytes); err != nil {
		return nil, ReferenceMeta{}, err
	}
	rc, meta, err := reader.Open(ctx, owner, id)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ReferenceMeta{}, ctxErr
		}
		// Foreign, deleted and missing IDs are indistinguishable on purpose:
		// none of them should tell the caller which one it was.
		return nil, ReferenceMeta{}, &Error{Code: "asset_not_found", Message: "Referenced asset was not found."}
	}
	var refusal *Error
	switch {
	case meta.Modality != string(modality) || !strings.HasPrefix(meta.MIMEType, string(modality)+"/"):
		refusal = &Error{Code: "unsupported", Message: "Referenced asset is not " + modality.noun() + "."}
	case meta.SizeBytes > maxBytes:
		refusal = &Error{Code: "too_large", Message: "Referenced asset exceeds the configured byte limit."}
	}
	if refusal != nil {
		_ = rc.Close()
		return nil, ReferenceMeta{}, refusal
	}
	return capped(rc, maxBytes), meta, nil
}

func (m AssetModality) noun() string {
	if m == AssetImage {
		return "an image"
	}
	return "a " + string(m)
}
