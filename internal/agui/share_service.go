// share_service.go declares the WEBSHARE-02/03 share-lifecycle API surface AG-UI handlers
// consume (plan 37F-10) — the share-package analog of AssetService (asset_service.go, the
// exact template this mirrors). ShareService is narrow and consumer-declared: it covers
// exactly what share_api*.go's handlers call, using share.* domain types in every signature
// so this package never re-declares Snapshot/Link/Tier.
package agui

import (
	"context"
	"io"
	"net/http"

	"github.com/chetto1983/aura/internal/share"
	"github.com/google/uuid"
)

// ShareService mirrors share.Service's Create/Update/Revoke/ResolveByToken/ResolveInternal
// (plan 37F-08) verbatim, plus share.Store's two owner-scoped list reads and one
// token/snapshot-scoped artifact opener that Service itself does not expose. The composition
// root (cmd/aura/serve_webui_share.go, plan 37F-12) satisfies this with a thin adapter over
// the real *share.Service + *share.Store + objectstore.Store — the same "consumer-declared
// seam" shape internal/share/service.go's own doc describes for ITS upstream dependencies
// (ConversationReader, ArtifactLister, ArtifactOpener).
type ShareService interface {
	// Create mints a new share link (D-01); req.OwnerIdentityID is the CALLER's identity, and
	// the owner gate runs first, before any side effect.
	Create(ctx context.Context, req share.CreateRequest) (share.CreateResult, error)
	// ListForIdentity returns identityID's own links, newest first, paginated.
	ListForIdentity(ctx context.Context, identityID string, limit, offset int) ([]share.Link, error)
	// ListForConversation returns identityID's links for one conversation (the "Condiviso"
	// filter). A foreign conversation id yields an EMPTY list, never an error.
	ListForConversation(ctx context.Context, conversationID, identityID string) ([]share.Link, error)
	// UpdateSnapshot re-snapshots shareID to a NEW snapshot_id, keeping the SAME token (D-06
	// "Update", never "unshare and re-share"), owner-scoped.
	UpdateSnapshot(ctx context.Context, shareID, identityID string) (share.Link, error)
	// Revoke stamps shareID revoked and drops its object-store blobs, owner-scoped.
	Revoke(ctx context.Context, shareID, identityID string) error
	// ResolveByToken serves the public tier's unauthenticated token predicate. ANY failure —
	// unknown, expired, revoked, or a corrupted blob — collapses to ONE error: there is no
	// oracle to preserve here, and callers must not try to recover one from it.
	ResolveByToken(ctx context.Context, token string) (share.Snapshot, share.Link, error)
	// ResolveInternal serves the D-10 bearer-within-auth read (share.Service.ResolveInternal,
	// plan 37F-08): identityID is the CALLER's identity, recorded ONLY for the audit trail —
	// it is NOT an owner filter, the opposite of every other identity-taking method on this
	// interface. Any authenticated identity holding shareID may resolve its already-redacted
	// snapshot; the gate is RequireAuth at the mount (plan 37F-12) plus the unguessable id,
	// never this argument. A reader who has just absorbed Create/UpdateSnapshot/Revoke's
	// owner-gate-first shape may misread this asymmetry as a bug — it is D-10 by design
	// (SC4 row 3).
	ResolveInternal(ctx context.Context, shareID, identityID string) (share.Snapshot, share.Link, error)
	// OpenArtifact opens ONE bundled artifact's bytes from the share's token-scoped
	// share/<shareID>/snapshot/<snapshotID>/asset/<assetID> object-store key — NEVER the
	// identity-scoped assets.Service (D-09 copy-never-reference). The caller MUST already have
	// confirmed assetID belongs to the resolved snapshot (SC4 row 9) before calling this, and
	// closes the returned io.ReadCloser.
	OpenArtifact(ctx context.Context, shareID, snapshotID uuid.UUID, assetID string) (io.ReadCloser, error)
}

// registerShareRoutes is a placeholder through Task 1 of plan 37F-10 — server.go's Mux()
// needs a stable call site from this commit onward. Task 2 removes this stub and defines the
// real registerShareRoutes (the 4 owner-scoped routes + 2 delegated calls) in share_api.go,
// alongside the other two trust-boundary files' own route registrations — every route string
// this plan ships ends up living in a share_api*.go file, never here.
func (s *Server) registerShareRoutes(mux *http.ServeMux) {
	_ = s.share // wired by SetShareService; consumed by Task 2's share_api*.go handlers
}
