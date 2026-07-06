//nolint:revive // Internal asset object-resolution seam is exported for composition-root wiring.
package assets

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5"
)

// ObjectResolver resolves the per-identity S3 credentials for the identity carried on ctx
// (satisfied by *objectstore.IdentityStore). The asset path consumes it so each identity's
// bytes land in ITS OWN Garage bucket under ITS OWN key (D-08 bucket-per-identity), instead
// of the single shared static credential every identity shared before Phase 36.
type ObjectResolver interface {
	Resolve(ctx context.Context) (objectstore.Credentials, error)
}

// StoreFactory builds a per-identity object Store from resolved credentials. The composition
// root supplies a CACHING factory so repeated requests reuse one S3Store per access key
// rather than rebuilding an S3 client on every asset op.
type StoreFactory func(objectstore.Credentials) (objectstore.Store, error)

// resolveObjects returns the object Store + bucket to use for the identity carried on ctx.
//
// Fallback discipline — defense-in-depth OVER the record-level assets.Store.GetForIdentity
// owner-scoping that already gates asset record/presigned-URL access:
//   - resolver/factory nil          → the injected shared store+bucket (backward compat: the
//     pre-resolver shared path, so every existing shared-path unit test is unaffected).
//   - Resolve → pgx.ErrNoRows       → the shared store+bucket EXPLICITLY. An unprovisioned
//     non-local identity degrades to shared; IdentityStore.Resolve fail-closes a FOREIGN
//     identity to ErrNoRows, so this can NEVER resolve to a foreign bucket (F-007).
//   - Resolve → any other error     → propagate (a real fault: bad KEK, DB down, decrypt fail).
//   - creds.Bucket == sharedBucket  → the shared store (IdentityStore.Resolve returns the
//     shared creds for the local/empty principal, D-11 operator-unchanged; no client rebuild).
//   - otherwise                     → a per-identity store built from the resolved creds.
func resolveObjects(ctx context.Context, resolver ObjectResolver, factory StoreFactory, shared objectstore.Store, sharedBucket string) (objectstore.Store, string, error) {
	if resolver == nil || factory == nil {
		return shared, sharedBucket, nil
	}
	creds, err := resolver.Resolve(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shared, sharedBucket, nil
		}
		return nil, "", err
	}
	if creds.Bucket == sharedBucket {
		return shared, sharedBucket, nil
	}
	st, err := factory(creds)
	if err != nil {
		return nil, "", err
	}
	return st, creds.Bucket, nil
}

// ObjectResolverBundle carries the per-identity object-resolution seam so each processor
// holds ONE optional field instead of three. A nil bundle → the processor's injected shared
// store (backward compat).
//
// SharedBucket is the REAL shared bucket (cfg.ObjectStoreBucket). The processor reads
// asset.ObjectBucket regardless, but resolveObjects needs the real shared bucket — NOT
// asset.ObjectBucket — to correctly detect the local/shared principal: a per-identity asset's
// ObjectBucket EQUALS the resolved creds.Bucket, so passing asset.ObjectBucket would wrongly
// short-circuit a per-identity owner to the shared (global-key) store, which cannot read that
// owner's bucket (F-007 per-bucket grants). The owner's OWN creds are required to read it.
type ObjectResolverBundle struct {
	Resolver         ObjectResolver
	PerIdentityStore StoreFactory
	SharedBucket     string
}

// storeForAsset returns the object Store to read asset's bytes with, resolving the ASSET
// OWNER's per-identity credentials (asset.ObjectBucket was written by the routed Put/Presign
// under that owner, so only the owner's creds can read it). The processor Gets
// ObjectRef{asset.ObjectBucket, asset.ObjectKey} on the returned store. A nil bundle → shared.
//
// Processors run on a background/durable-worker context that carries NO principal, so the
// owner identity MUST be stamped from asset.IdentityID here (not read from ctx).
func (b *ObjectResolverBundle) storeForAsset(ctx context.Context, shared objectstore.Store, asset Asset) (objectstore.Store, error) {
	if b == nil {
		return shared, nil
	}
	ictx := identityctx.WithIdentityID(ctx, asset.IdentityID)
	st, _, err := resolveObjects(ictx, b.Resolver, b.PerIdentityStore, shared, b.SharedBucket)
	return st, err
}
