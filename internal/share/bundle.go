// bundle.go implements the D-09 (amended) agent-artifacts-only bundling filter and the
// copy-on-share blob writer that backs it — the two structural answers to this phase's
// sharpest security weight.
//
// BundleFilter reproduces web/src/chat/artifacts/useThreadArtifacts.ts:33-37's
// selectAgentArtifacts predicate byte-for-byte, enforced here at the SERVER trust boundary
// instead of only in the browser: `source_kind === 'agent' && status !== 'deleted' && status
// !== 'canceled'`. Aura already ships this rule client-side; this file is where it becomes
// load-bearing. A user's own upload (`source_kind='web'` — possibly a passport scan) MUST
// NEVER enter a share, above all a public one. Claude ships the identical rule: "If you
// share a chat containing an attached file, the file itself is not included in the shared
// snapshot and remains private." The filter is an ALLOWLIST OF ONE (assets.SourceAgent), not
// a denylist of the human source kinds — an unrecognized or future SourceKind therefore
// fails CLOSED (silently excluded) rather than requiring this function to be updated the day
// a fifth kind is added.
//
// bundleArtifacts/dropBlobs/dropSnapshotBlobs implement COPY, NEVER REFERENCE (D-09/R-11):
// bytes are copied into `share/{shareID}/...` object-store keys at mint/update time, and the
// resolve path (service.go) never resolves a share's asset id back through the
// identity-scoped assets.Service. open-webui shipped exactly the opposite bug — "Access to a
// file through a shared chat now only grants read access" (CHANGELOG.md:331) is their fix
// notice for having granted WRITE through a share link. Aura's recipient token addresses
// `share/{id}/...` blobs and has no path to `identity/{owner}/asset/...` at all.
package share

import (
	"context"
	"fmt"
	"io"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

// ArtifactLister is the consumer-declared seam (the IdentityPurger pattern,
// internal/cron/handlers/identity_purge.go:20-27) Service.Create uses to list a
// conversation's candidate artifacts before filtering. The live agui.AssetService's
// ListForThread(ctx, identityID, threadID) satisfies it structurally — internal/share
// imports neither internal/agui nor its HTTP layer. threadID and conversationID are the
// same identifier in this codebase's model (AG-UI thread == conversation, mirroring
// runner_delete.go's "sessionID == conversationID (D-26)" convention), so callers pass the
// conversation id in the threadID slot.
type ArtifactLister interface {
	ListForThread(ctx context.Context, identityID, threadID string) ([]assets.Asset, error)
}

// ArtifactOpener is the consumer-declared seam bundleArtifacts uses to open one
// identity-scoped artifact's bytes for copying into the share's token-scoped namespace. The
// live agui.AssetService's OpenForIdentity(ctx, assetID, identityID) satisfies it
// structurally — again with no internal/agui import. The caller closes the returned
// ReadCloser (mirroring OpenForIdentity's own documented contract).
type ArtifactOpener interface {
	OpenForIdentity(ctx context.Context, assetID, identityID string) (io.ReadCloser, assets.Asset, error)
}

// BundleFilter is the ONLY place this rule is decided: a candidate artifact survives into a
// share if and only if its SourceKind is assets.SourceAgent (never a string literal — the
// real constant, so a future rename of the value is a compile error here, not a silent
// mismatch) AND its Status is neither StatusDeleted nor StatusCanceled. It is exported and
// pure (mirroring selectAgentArtifacts's own "exported for direct unit coverage of the
// filter... without a render" rationale) so this file's central security property is
// unit-testable with no store, no ArtifactOpener, no context. The returned slice preserves
// input order; it never mutates the input slice.
func BundleFilter(artifacts []assets.Asset) []assets.Asset {
	out := make([]assets.Asset, 0, len(artifacts))
	for _, a := range artifacts {
		if a.SourceKind != assets.SourceAgent {
			continue
		}
		if a.Status == assets.StatusDeleted || a.Status == assets.StatusCanceled {
			continue
		}
		out = append(out, a)
	}
	return out
}

// bundleArtifacts copies every already-filtered artifact's bytes into
// objectstore.ShareArtifactKey(shareID, snapshotID, assetID) — the token-scoped namespace a
// recipient's share token can address, lexically disjoint from the owner's
// identity-scoped `identity/...` lane. Every returned SnapshotArtifact is built via
// projectArtifact (redact.go) from exactly the four allowlisted ArtifactMeta fields of the
// SAME candidate the caller already fetched — never from a value re-derived after the copy —
// so this list is byte-for-byte what BuildSnapshot's own artifact projection would produce
// from the identical input. ownerIdentityID scopes each src.OpenForIdentity read to the
// share's owner (the only identity the artifacts ever belonged to); ctx cancellation aborts
// the copy loop between artifacts, never mid-Put.
func bundleArtifacts(
	ctx context.Context,
	store objectstore.Store,
	bucket string,
	shareID, snapshotID uuid.UUID,
	ownerIdentityID string,
	src ArtifactOpener,
	artifacts []assets.Asset,
) ([]SnapshotArtifact, error) {
	out := make([]SnapshotArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		assetID, err := uuid.Parse(a.ID)
		if err != nil {
			return nil, fmt.Errorf("bundle artifact %s: %w", a.ID, err)
		}
		rc, _, err := src.OpenForIdentity(ctx, a.ID, ownerIdentityID)
		if err != nil {
			return nil, fmt.Errorf("bundle artifact %s: open: %w", a.ID, err)
		}
		_, putErr := store.Put(ctx, objectstore.ObjectRef{
			Bucket: bucket,
			Key:    objectstore.ShareArtifactKey(shareID, snapshotID, assetID),
		}, rc, objectstore.PutOptions{MIMEType: a.MIMEType, Size: a.SizeBytes})
		closeErr := rc.Close()
		if putErr != nil {
			return nil, fmt.Errorf("bundle artifact %s: put: %w", a.ID, putErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("bundle artifact %s: close: %w", a.ID, closeErr)
		}
		out = append(out, projectArtifact(ArtifactMeta{
			AssetID:   a.ID,
			FileName:  a.FileName,
			MIMEType:  a.MIMEType,
			SizeBytes: a.SizeBytes,
		}))
	}
	return out, nil
}

// dropBlobs reclaims every byte under one share's ENTIRE revoke prefix
// (objectstore.ShareKeyPrefix(shareID)) — every snapshot's canonical JSON and every bundled
// artifact the share ever held. It is the Revoke-path cleanup (R-10: the shared_links FK
// CASCADE removes the ROW, never the object-store bytes) and is idempotent by construction:
// List on an already-empty prefix returns zero objects, so a second call is a no-op, not an
// error — a crash between Revoke's drop-then-stamp steps re-runs this harmlessly.
func dropBlobs(ctx context.Context, store objectstore.Store, bucket string, shareID uuid.UUID) error {
	objs, err := store.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: objectstore.ShareKeyPrefix(shareID)})
	if err != nil {
		return fmt.Errorf("drop blobs %s: list: %w", shareID, err)
	}
	for _, o := range objs {
		if err := store.Delete(ctx, o.Ref); err != nil {
			return fmt.Errorf("drop blobs %s: delete %s: %w", shareID, o.Ref.Key, err)
		}
	}
	return nil
}

// dropSnapshotBlobs reclaims only ONE snapshot's blobs (its canonical JSON plus every
// artifact bundled under it) — the narrower scope Service.Update's old-snapshot cleanup
// needs. It is deliberately NOT dropBlobs(shareID): Update writes the NEW snapshot's keys
// (under the same share/<shareID>/ prefix) BEFORE swapping the row's snapshot_id pointer, so
// for a moment the old and new snapshots' keys coexist under one share prefix — reclaiming
// the whole share prefix here would delete the brand-new snapshot's bytes together with the
// stale ones. Same idempotency guarantee as dropBlobs (an empty-prefix List is a no-op).
func dropSnapshotBlobs(ctx context.Context, store objectstore.Store, bucket string, shareID, snapshotID uuid.UUID) error {
	prefix := "share/" + shareID.String() + "/snapshot/" + snapshotID.String() + "/"
	objs, err := store.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: prefix})
	if err != nil {
		return fmt.Errorf("drop snapshot blobs %s/%s: list: %w", shareID, snapshotID, err)
	}
	for _, o := range objs {
		if err := store.Delete(ctx, o.Ref); err != nil {
			return fmt.Errorf("drop snapshot blobs %s/%s: delete %s: %w", shareID, snapshotID, o.Ref.Key, err)
		}
	}
	return nil
}
