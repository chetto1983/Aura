// service.go composes the primitives from store.go/audit.go/token.go/expiry.go/snapshot.go/
// bundle.go into the share lifecycle: Create, Update, Revoke, ResolveByToken, ResolveInternal.
// The ordered-lifecycle-with-owner-gate-first shape is stolen verbatim from
// internal/runner/runner_delete.go:38-74 (DeleteConversationLifecycle): a numbered doc comment
// matching numbered body steps, the owner gate running before any side effect, and a
// best-effort step logging a warning rather than blocking the operation.
//
// Every cross-package dependency is a consumer-declared seam (the IdentityPurger pattern,
// internal/cron/handlers/identity_purge.go:20-27): this package imports neither
// internal/agui nor internal/conversations. The live *conversations.Store and
// *agui.AssetService satisfy these seams via a thin adapter built at the composition root
// (a later plan's wiring, not this one's concern).
package share

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

// anonymousAuditPrincipal is the share_audit identity_id recorded for a public-tier
// ResolveByToken "open" event: there is no Aura identity behind an anonymous token holder
// (D-14 forbids recording any recipient PII — no IP, no user-agent, no identity), so this
// literal marker fills the NOT NULL column the same way audit.go's own header notes the
// column holds the literal "local" for the seeded operator — a free-text value, not a real
// identity reference.
const anonymousAuditPrincipal = "public"

// ErrSharePublicDisabled is returned by Create when the public tier is requested while the
// org kill-switch (AURA_SHARE_PUBLIC_ENABLED, default false) is off. It is re-checked HERE,
// inside Create, not only at the capability-gated handler mount (plan 37F-10) — RequireCapability
// returns the next handler unchanged when the web-auth secret is not configured (loopback
// dev), so the capability gate can structurally not exist at all; this in-handler check is the
// gate that still holds when that one cannot (R-08/OQ2).
var ErrSharePublicDisabled = errors.New("share: public tier is disabled")

// Tier is the D-01 share tier a caller requests at Create time. The zero value (empty string)
// and any value other than TierPublic resolve to TierInternal — see Create's fail-closed tier
// switch. There is no third "resolved" tier value: a caller can never observe or request
// anything but exactly these two.
type Tier string

// The two tiers Create ever writes to aura.shared_links.tier. There is no "auto" or "default"
// tier constant on purpose: Create's own switch is where "absent/unrecognized -> internal" is
// decided, not a symbolic value a caller could pass to opt into the same fallback explicitly.
const (
	TierInternal Tier = "internal"
	TierPublic   Tier = "public"
)

// ConversationReader is the consumer-declared seam Create/Update use to reach the owning
// conversation without importing internal/conversations (the IdentityPurger pattern).
type ConversationReader interface {
	// GetForIdentity is the OWNER GATE: Create/Update call it FIRST, before any side effect.
	// It returns ConvMeta (title/model/creation time only — never the owner's identity id,
	// which this package must never see even transitively) on success. ANY non-nil error is
	// treated as "identityID does not own conversationID" and mapped to ErrShareNotFound —
	// this package never inspects the error's type or imports
	// conversations.ErrConversationNotFound to distinguish "foreign" from "absent": those two
	// cases must be indistinguishable to the caller regardless (D-06 404-not-403), so there is
	// nothing to gain by telling them apart here.
	GetForIdentity(ctx context.Context, conversationID, identityID string) (ConvMeta, error)
	// LoadHistory reconstructs the RAW, UNSCOPED turn history. The caller MUST have already
	// run the owner gate via GetForIdentity — LoadHistory itself performs no ownership check,
	// mirroring conversations.Store.LoadHistory's own documented "Unscoped" contract.
	LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error)
}

// CreateRequest carries one Create call's inputs. An absent or unrecognized Tier resolves to
// TierInternal (D-01: never default to public) — see Create's tier switch.
type CreateRequest struct {
	ConversationID   string
	OwnerIdentityID  string
	Tier             Tier
	ExpiryOption     ExpiryOption
	CustomExpiryDays int
}

// CreateResult is Create's return value. Token carries the PLAINTEXT share token exactly
// ONCE — populated only for a public-tier Create, empty for internal — and is never returned,
// logged, or persisted again by anything in this package after this call returns.
type CreateResult struct {
	Link  Link
	Token string
}

// Service composes the share lifecycle. Every field is injected at construction: the object
// store in particular is built from the SHARED credentials at the composition root and handed
// in directly — NEVER pulled from the ctx-keyed per-identity credential resolver objectstore
// already exposes elsewhere, which reads the principal off ctx and would return the shared
// bucket for an empty/anonymous principal only by accident (its own "is this the shared
// principal" helper treats an empty string as shared). An accident is not a design;
// Service.objects is wired explicitly instead.
type Service struct {
	store     *Store
	audit     *AuditWriter
	objects   objectstore.Store
	bucket    string
	conv      ConversationReader
	artifacts ArtifactLister
	opener    ArtifactOpener

	maxExpiryDays int
	publicEnabled bool
}

// NewService builds the share lifecycle service. maxExpiryDays and publicEnabled are the
// config.ShareConfig values the composition root already loads (AURA_SHARE_MAX_EXPIRY_DAYS /
// AURA_SHARE_PUBLIC_ENABLED) — passed as plain values, mirroring expiry.go's own
// deliberately-injected-not-read convention, so this constructor stays deterministically
// testable.
func NewService(
	store *Store,
	audit *AuditWriter,
	objects objectstore.Store,
	bucket string,
	conv ConversationReader,
	artifacts ArtifactLister,
	opener ArtifactOpener,
	maxExpiryDays int,
	publicEnabled bool,
) *Service {
	return &Service{
		store:         store,
		audit:         audit,
		objects:       objects,
		bucket:        bucket,
		conv:          conv,
		artifacts:     artifacts,
		opener:        opener,
		maxExpiryDays: maxExpiryDays,
		publicEnabled: publicEnabled,
	}
}

// toArtifactMetas projects a filtered artifact list down to BuildSnapshot's narrow
// ArtifactMeta input — shared by Create and Update so the two never independently re-derive
// (and risk diverging) the same four-field mapping.
func toArtifactMetas(filtered []assets.Asset) []ArtifactMeta {
	out := make([]ArtifactMeta, 0, len(filtered))
	for _, a := range filtered {
		out = append(out, ArtifactMeta{AssetID: a.ID, FileName: a.FileName, MIMEType: a.MIMEType, SizeBytes: a.SizeBytes})
	}
	return out
}

// resolveTier applies D-01's fail-closed tier default: an absent Tier ("") or ANY
// unrecognized value resolves to TierInternal, and only an explicit TierPublic request ever
// yields the public tier — there is no second, explicit "internal" case to fall through from;
// the default arm below IS the fail-closed contract, not a fallback from a missing case.
// Extracted as its own pure function (no ctx, no store, no side effect) so this exact security
// property is unit-testable with no database at all: see bundle_test.go's untagged
// TestDefaultTierIsInternal (co-located there rather than in a new file, per this plan's own
// "prefer the untagged home" guidance).
func resolveTier(requested Tier) Tier {
	switch requested {
	case TierPublic:
		return TierPublic
	default:
		return TierInternal
	}
}

// Create mints a new share link for req.ConversationID, owner-scoped to
// req.OwnerIdentityID. Numbered steps in this comment match the numbered steps in the body
// below (the runner_delete.go discipline):
//
//  1. OWNER GATE FIRST (D-06) — conv.GetForIdentity. A miss (foreign OR absent conversation)
//     returns ErrShareNotFound before anything is minted, built, or written — SC4 rows 1/2.
//  2. Resolve the tier. Absent or unrecognized -> internal (D-01: public is NEVER the default).
//  3. If public: the org kill-switch is re-checked HERE (not only at the capability-gated
//     handler mount, R-08/OQ2) -> ErrSharePublicDisabled when off; then ResolveExpiry
//     rejects/clamps the expiry, so a public mint with no expiry never reaches the DB CHECK.
//  4. Load history + list candidate artifacts; BundleFilter to the agent-only allowlist
//     (D-09 amended) — a user's own upload never survives past this line.
//  5. BuildSnapshot -> the redacted Snapshot; marshal via Snapshot.JSON().
//  6. Mint a snapshot_id; Put the canonical JSON at ShareSnapshotKey; bundleArtifacts the
//     filtered set into token-scoped keys under the share's revoke prefix.
//  7. If public: Mint() the token, store Hash(token). Internal: no token at all (the
//     migration 0040 tier-shape CHECK requires token_hash IS NULL for tier='internal').
//  8. Insert the row; audit "create".
//  9. Return CreateResult carrying the plaintext token EXACTLY ONCE (public only) — never
//     logged, never wrapped into an error, never stored anywhere else in this process.
func (svc *Service) Create(ctx context.Context, req CreateRequest) (CreateResult, error) {
	// 1. Owner gate FIRST.
	conv, err := svc.conv.GetForIdentity(ctx, req.ConversationID, req.OwnerIdentityID)
	if err != nil {
		return CreateResult{}, ErrShareNotFound
	}

	// 2. Resolve the tier — fail-closed default (extracted as resolveTier, see its own doc).
	tier := resolveTier(req.Tier)

	// 3. Public-tier preconditions: the org kill-switch, then the mandatory expiry.
	var expiresAt *time.Time
	if tier == TierPublic {
		if !svc.publicEnabled {
			return CreateResult{}, ErrSharePublicDisabled
		}
		exp, err := ResolveExpiry(req.ExpiryOption, req.CustomExpiryDays, time.Now().UTC(), svc.maxExpiryDays)
		if err != nil {
			return CreateResult{}, fmt.Errorf("create share: %w", err)
		}
		expiresAt = &exp
	}

	// 4. Load history + candidate artifacts; filter to the agent-only allowlist.
	history, err := svc.conv.LoadHistory(ctx, req.ConversationID)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create share: load history: %w", err)
	}
	rawArtifacts, err := svc.artifacts.ListForThread(ctx, req.OwnerIdentityID, req.ConversationID)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create share: list artifacts: %w", err)
	}
	filtered := BundleFilter(rawArtifacts)

	// 5. Build the redacted snapshot.
	now := time.Now().UTC()
	snap, err := BuildSnapshot(conv, history, toArtifactMetas(filtered), now)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create share: build snapshot: %w", err)
	}
	snapJSON, err := snap.JSON()
	if err != nil {
		return CreateResult{}, fmt.Errorf("create share: %w", err)
	}

	// 6. Mint IDs; write the canonical snapshot blob + bundle the filtered artifacts.
	shareID := uuid.Must(uuid.NewV7())
	snapshotID := uuid.Must(uuid.NewV7())
	if _, err := svc.objects.Put(ctx, objectstore.ObjectRef{
		Bucket: svc.bucket,
		Key:    objectstore.ShareSnapshotKey(shareID, snapshotID),
	}, bytes.NewReader(snapJSON), objectstore.PutOptions{MIMEType: "application/json", Size: int64(len(snapJSON))}); err != nil {
		return CreateResult{}, fmt.Errorf("create share: put snapshot: %w", err)
	}
	if _, err := bundleArtifacts(ctx, svc.objects, svc.bucket, shareID, snapshotID, req.OwnerIdentityID, svc.opener, filtered); err != nil {
		return CreateResult{}, fmt.Errorf("create share: bundle artifacts: %w", err)
	}

	// 7. Mint the token for a public tier; internal carries none.
	var tokenHash []byte
	var plaintext string
	if tier == TierPublic {
		pt, hash, mErr := Mint()
		if mErr != nil {
			return CreateResult{}, fmt.Errorf("create share: mint: %w", mErr)
		}
		plaintext = pt
		tokenHash = hash[:]
	}

	// 8. Persist the row + audit "create".
	convUUID, err := uuid.Parse(req.ConversationID)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create share: %w", err)
	}
	ownerUUID, err := uuid.Parse(req.OwnerIdentityID)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create share: %w", err)
	}
	link := Link{
		ID:              shareID,
		OwnerIdentityID: ownerUUID,
		ConversationID:  convUUID,
		Tier:            string(tier),
		SnapshotID:      snapshotID,
		SnapshotBucket:  svc.bucket,
		ExpiresAt:       expiresAt,
	}
	if err := svc.store.Insert(ctx, link, tokenHash); err != nil {
		return CreateResult{}, fmt.Errorf("create share: %w", err)
	}
	if err := svc.audit.Append(ctx, req.OwnerIdentityID, shareID, convUUID, AuditCreate, string(tier), ""); err != nil {
		return CreateResult{}, fmt.Errorf("create share: audit: %w", err)
	}

	// 9. Return the plaintext token exactly once (public only).
	return CreateResult{Link: link, Token: plaintext}, nil
}

// Update re-snapshots shareID to a NEW snapshot_id, owner-scoped, keeping the SAME token
// (D-06 "Update" semantics — Claude/open-webui's "refresh the link" behavior, never
// "unshare and re-share"). Numbered steps match the body:
//
//  1. Owner gate FIRST — store.GetForIdentity(shareID, identityID). A miss returns
//     ErrShareNotFound before anything below it runs.
//  2. Re-fetch conversation metadata + fresh history + candidate artifacts (the identical
//     fetch/filter Create performs) — defense-in-depth: shared_links.owner_identity_id
//     already binds this share to identityID at Create time and conversation ownership never
//     changes, but re-verifying costs nothing and keeps the title/model current.
//  3. Build the new redacted snapshot; mint a NEW snapshot_id; Put its canonical JSON +
//     bundle the (possibly changed) artifact set at that NEW snapshot_id's keys — NEVER
//     overwrite the live blob mid-write, which is the entire reason the key carries
//     snapshot_id.
//  4. Swap the row's snapshot_id pointer + bump updated_at. The token column is untouched.
//  5. Drop the OLD snapshot's now-unreachable blobs — AFTER the swap succeeds, never before:
//     the new snapshot must be durably pointed-at before the old bytes are freed.
//  6. Audit "update".
func (svc *Service) Update(ctx context.Context, shareID, identityID string) (Link, error) {
	// 1. Owner gate FIRST.
	link, err := svc.store.GetForIdentity(ctx, shareID, identityID)
	if err != nil {
		return Link{}, err
	}

	// 2. Fresh conversation metadata + history + candidate artifacts.
	convID := link.ConversationID.String()
	conv, err := svc.conv.GetForIdentity(ctx, convID, identityID)
	if err != nil {
		return Link{}, fmt.Errorf("update share %s: %w", shareID, ErrShareNotFound)
	}
	history, err := svc.conv.LoadHistory(ctx, convID)
	if err != nil {
		return Link{}, fmt.Errorf("update share %s: load history: %w", shareID, err)
	}
	rawArtifacts, err := svc.artifacts.ListForThread(ctx, identityID, convID)
	if err != nil {
		return Link{}, fmt.Errorf("update share %s: list artifacts: %w", shareID, err)
	}
	filtered := BundleFilter(rawArtifacts)

	// 3. Build + write the NEW snapshot (a fresh snapshot_id, never the live one in place).
	now := time.Now().UTC()
	snap, err := BuildSnapshot(conv, history, toArtifactMetas(filtered), now)
	if err != nil {
		return Link{}, fmt.Errorf("update share %s: build snapshot: %w", shareID, err)
	}
	snapJSON, err := snap.JSON()
	if err != nil {
		return Link{}, fmt.Errorf("update share %s: %w", shareID, err)
	}
	newSnapshotID := uuid.Must(uuid.NewV7())
	if _, err := svc.objects.Put(ctx, objectstore.ObjectRef{
		Bucket: svc.bucket,
		Key:    objectstore.ShareSnapshotKey(link.ID, newSnapshotID),
	}, bytes.NewReader(snapJSON), objectstore.PutOptions{MIMEType: "application/json", Size: int64(len(snapJSON))}); err != nil {
		return Link{}, fmt.Errorf("update share %s: put snapshot: %w", shareID, err)
	}
	if _, err := bundleArtifacts(ctx, svc.objects, svc.bucket, link.ID, newSnapshotID, identityID, svc.opener, filtered); err != nil {
		return Link{}, fmt.Errorf("update share %s: bundle artifacts: %w", shareID, err)
	}

	// 4. Swap the pointer (the token column is untouched by UpdateSnapshot).
	oldSnapshotID := link.SnapshotID
	if err := svc.store.UpdateSnapshot(ctx, shareID, identityID, newSnapshotID, now); err != nil {
		return Link{}, fmt.Errorf("update share %s: %w", shareID, err)
	}

	// 5. Drop the OLD snapshot's blobs — after the swap succeeds, never before.
	if err := dropSnapshotBlobs(ctx, svc.objects, svc.bucket, link.ID, oldSnapshotID); err != nil {
		return Link{}, fmt.Errorf("update share %s: drop old snapshot: %w", shareID, err)
	}

	// 6. Audit "update".
	if err := svc.audit.Append(ctx, identityID, link.ID, link.ConversationID, AuditUpdate, link.Tier, ""); err != nil {
		return Link{}, fmt.Errorf("update share %s: audit: %w", shareID, err)
	}

	link.SnapshotID = newSnapshotID
	link.UpdatedAt = now
	return link, nil
}

// Revoke stamps shareID revoked, owner-scoped. Numbered steps match the body:
//
//  1. Owner gate FIRST — store.GetForIdentity(shareID, identityID).
//  2. DROP BLOBS, THEN STAMP revoked_at — never the reverse (R-10). A crash between the two
//     re-runs the idempotent delete on the next call (dropBlobs is a no-op on an empty
//     prefix); stamp-then-delete would instead orphan bytes permanently the moment the stamp
//     lands and a crash follows before the delete.
//  3. Audit "revoke".
func (svc *Service) Revoke(ctx context.Context, shareID, identityID string) error {
	// 1. Owner gate FIRST.
	link, err := svc.store.GetForIdentity(ctx, shareID, identityID)
	if err != nil {
		return err
	}

	// 2. Drop blobs, THEN stamp revoked_at.
	if err := dropBlobs(ctx, svc.objects, svc.bucket, link.ID); err != nil {
		return fmt.Errorf("revoke share %s: drop blobs: %w", shareID, err)
	}
	if err := svc.store.RevokeForIdentity(ctx, shareID, identityID, time.Now().UTC()); err != nil {
		return fmt.Errorf("revoke share %s: %w", shareID, err)
	}

	// 3. Audit "revoke".
	if err := svc.audit.Append(ctx, identityID, link.ID, link.ConversationID, AuditRevoke, link.Tier, ""); err != nil {
		return fmt.Errorf("revoke share %s: audit: %w", shareID, err)
	}
	return nil
}

// getSnapshot fetches + unmarshals the canonical snapshot blob for link — the shared
// resolve-time step both ResolveByToken and ResolveInternal run identically.
func (svc *Service) getSnapshot(ctx context.Context, link Link) (Snapshot, error) {
	rc, _, err := svc.objects.Get(ctx, objectstore.ObjectRef{
		Bucket: svc.bucket,
		Key:    objectstore.ShareSnapshotKey(link.ID, link.SnapshotID),
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("get snapshot blob: %w", err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot blob: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return snap, nil
}

// ResolveByToken resolves the public tier's D-13/D-15 token predicate to its redacted
// Snapshot: Hash the plaintext, resolve via the store's lazy fail-closed predicate, Get +
// unmarshal the canonical snapshot blob. ANY error along this path — an unknown token, a
// revoked link, an expired link, or a corrupted/missing blob — collapses to the SAME
// ErrShareNotFound: there is no oracle anywhere in this function distinguishing those cases.
// Audits "open" with NO recipient PII (D-14): anonymousAuditPrincipal, never an IP, a
// user-agent, or any identity — the ledger proves the link was used, not who used it.
func (svc *Service) ResolveByToken(ctx context.Context, plaintext string) (Snapshot, Link, error) {
	hash := Hash(plaintext)
	link, err := svc.store.ResolveByToken(ctx, hash[:])
	if err != nil {
		return Snapshot{}, Link{}, ErrShareNotFound
	}
	snap, err := svc.getSnapshot(ctx, link)
	if err != nil {
		return Snapshot{}, Link{}, ErrShareNotFound
	}
	if err := svc.audit.Append(ctx, anonymousAuditPrincipal, link.ID, link.ConversationID, AuditOpen, link.Tier, ""); err != nil {
		return Snapshot{}, Link{}, fmt.Errorf("resolve share: audit: %w", err)
	}
	return snap, link, nil
}

// ResolveInternal serves the internal tier's D-10 bearer-within-auth read: it is the resolver
// behind GET /api/shares/{id}/data (plan 37F-10), the internal tier's ONLY consumer — without
// that route the internal tier (D-01's DEFAULT share action) mints rows no recipient could
// ever open.
//
// UNLIKE Create/Update/Revoke above, this method runs NO owner gate — deliberately, not by
// omission. D-10 makes the LINK the capability and RequireAuth (the route mount, plan 37F-12)
// the gate; the snapshot is already redacted (D-08), so a bearer never sees more than the
// owner chose to share. This is SC4 row 3 — the ONE row in this phase where the expected
// answer is 200, not 404. A reader who has just absorbed three functions' worth of
// owner-gate-first discipline may read this absence as a bug and "fix" it by adding an owner
// filter — DO NOT: that would silently reduce the internal tier to an owner-only view and
// break D-10/SC4 row 3.
//
// Tier and liveness are resolved ENTIRELY by Store.ResolveLiveByID's SQL predicate
// (tier='internal' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())) —
// there is no Go-side tier or liveness re-check here. A public-tier id, a revoked link, and
// an expired link therefore never reach this function as a row to inspect; they all arrive as
// the identical ErrShareNotFound miss ResolveLiveByID itself returns. Re-checking either
// property in Go would invite a distinguishable error and a drift between this resolver and
// ResolveByToken's identical predicate — do not add one.
//
// Audits "open" WITH the resolving identityID — unlike ResolveByToken's PII-free anonymous
// open, an internal-tier bearer already has a first-class Aura identity, and recording it is
// what makes "authenticated and auditable" (the OQ#4 rationale, plan 37F-01) true rather than
// aspirational.
func (svc *Service) ResolveInternal(ctx context.Context, shareID, identityID string) (Snapshot, Link, error) {
	id, err := uuid.Parse(shareID)
	if err != nil {
		return Snapshot{}, Link{}, ErrShareNotFound
	}
	link, err := svc.store.ResolveLiveByID(ctx, id)
	if err != nil {
		return Snapshot{}, Link{}, ErrShareNotFound
	}
	snap, err := svc.getSnapshot(ctx, link)
	if err != nil {
		return Snapshot{}, Link{}, ErrShareNotFound
	}
	if err := svc.audit.Append(ctx, identityID, link.ID, link.ConversationID, AuditOpen, link.Tier, ""); err != nil {
		return Snapshot{}, Link{}, fmt.Errorf("resolve internal share %s: audit: %w", shareID, err)
	}
	return snap, link, nil
}
