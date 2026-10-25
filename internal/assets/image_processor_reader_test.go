package assets

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
)

// ownerReader is the ChatGPT reader's stand-in: it records whose account it was asked to use.
type ownerReader struct{ identity string }

func (r *ownerReader) Describe(ctx context.Context, _ []byte, _, _ string) (string, error) {
	r.identity = identityctx.IdentityID(ctx)
	return "a dog on a bench", nil
}

func TestImageProcessorReadsUnderTheAssetOwner(t *testing.T) {
	objects := objectstore.NewFake()
	ref := objectstore.ObjectRef{Bucket: "b", Key: "image/original"}
	if _, err := objects.Put(context.Background(), ref, strings.NewReader("image"),
		objectstore.PutOptions{MIMEType: "image/png", Size: 5}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	reader := &ownerReader{}
	result, err := NewImageProcessor(objects, multimodal.VisionConfig{
		PrimaryReader: reader,
		Model:         "gpt-6.1-sol",
	}).ProcessAsset(context.Background(), Asset{
		ID: "asset-1", IdentityID: "owner-1", Modality: ModalityImage, ObjectBucket: ref.Bucket,
		ObjectKey: ref.Key, FileName: "dog.png", MIMEType: "image/png",
	})
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}
	if reader.identity != "owner-1" {
		t.Fatalf("reader identity = %q, want the asset owner", reader.identity)
	}
	if result.Summary != "a dog on a bench" || result.Metadata["vision_model"] != "gpt-6.1-sol" {
		t.Fatalf("result = %q, vision_model %v", result.Summary, result.Metadata["vision_model"])
	}
}
