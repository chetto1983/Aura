package mediagen

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
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
	if reader == nil {
		return ImageReference{}, fmt.Errorf("mediagen: no reference reader is configured")
	}
	rc, meta, err := reader.Open(ctx, owner, id)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ImageReference{}, ctxErr
		}
		// Foreign, deleted and missing IDs are indistinguishable on purpose:
		// none of them should tell the caller which one it was.
		return ImageReference{}, &Error{Code: "asset_not_found", Message: "Referenced asset was not found."}
	}
	defer func() { _ = rc.Close() }()

	if meta.Modality != "image" || !strings.HasPrefix(meta.MIMEType, "image/") {
		return ImageReference{}, &Error{Code: "unsupported", Message: "Referenced asset is not an image."}
	}
	if meta.SizeBytes > maxBytes {
		return ImageReference{}, &Error{Code: "too_large", Message: "Referenced asset exceeds the configured byte limit."}
	}

	data, err := readCapped(rc, maxBytes)
	if err != nil {
		return ImageReference{}, err
	}

	dataURL := "data:" + meta.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(data)
	return ImageReference{Type: "image_url", ImageURL: ImageURL{URL: dataURL}}, nil
}
