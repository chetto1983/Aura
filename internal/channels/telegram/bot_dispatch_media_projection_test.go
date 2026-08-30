package telegram

import (
	"context"
	"testing"

	assetspkg "github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/llm"
)

// TestWithTurnMediaProjectionArmsImagesOnly pins amendment #198's second defect:
// Telegram turns must arm the SAME llm.ContentProjection seam the AG-UI gateway arms,
// scoped to this turn's native media only — a document stays catalog-only.
func TestWithTurnMediaProjectionArmsImagesOnly(t *testing.T) {
	t.Parallel()
	ingress := &recordingAssetIngress{}
	tg := &Telegram{deps: Deps{Assets: ingress}}
	image := assetspkg.Asset{ID: "a-img", IdentityID: "id-1", Modality: assetspkg.ModalityImage}
	doc := assetspkg.Asset{ID: "a-doc", IdentityID: "id-1", Modality: assetspkg.ModalityDocument}

	ctx := tg.withTurnMediaProjection(context.Background(), 7, []assetspkg.Asset{image, doc})
	proj, ok := llm.ContentProjectionFromContext(ctx)
	if !ok {
		t.Fatal("an image attachment must arm the content projection")
	}
	if len(proj.ReferenceIDs) != 1 || proj.ReferenceIDs[0] != "a-img" {
		t.Fatalf("projection must reference the image only, got %v", proj.ReferenceIDs)
	}
	if proj.Principal.OwnerID != "id-1" {
		t.Fatalf("projection principal must be the asset owner, got %q", proj.Principal.OwnerID)
	}
	loader, ok := proj.Loader.(assetspkg.TurnMediaLoader)
	if !ok {
		t.Fatalf("loader must be the shared assets.TurnMediaLoader, got %T", proj.Loader)
	}
	if loader.ThreadID != convID(7) || !loader.Allowed["a-img"] || loader.Allowed["a-doc"] {
		t.Fatalf("loader must scope to this chat's thread and allow the image only, got thread=%q allowed=%v", loader.ThreadID, loader.Allowed)
	}

	if _, armed := llm.ContentProjectionFromContext(tg.withTurnMediaProjection(context.Background(), 7, []assetspkg.Asset{doc})); armed {
		t.Fatal("a document-only turn must not arm the content projection")
	}
	if _, armed := llm.ContentProjectionFromContext(tg.withTurnMediaProjection(context.Background(), 7, nil)); armed {
		t.Fatal("a text turn must not arm the content projection")
	}
}
