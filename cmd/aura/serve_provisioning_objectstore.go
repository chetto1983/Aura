package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
	"github.com/chetto1983/aura/internal/redact"
)

// objectStoreCredentialResolver is the store-side seam objectStoreProvisionAdapter needs:
// resolve/persist/delete a per-identity credential row. *objectstore.IdentityStore satisfies
// it; the interface exists so EnsureForIdentity is testable (fake resolver simulating the
// shared/provisioned/pgx.ErrNoRows paths) without a live Postgres pool.
type objectStoreCredentialResolver interface {
	Resolve(ctx context.Context) (objectstore.Credentials, error)
	Put(ctx context.Context, identity, bucket, accessKey, secretPlaintext string) error
	Delete(ctx context.Context, identity string) error
}

// objectStoreMinter is the Garage-admin-side seam: create a bucket + a scoped key and grant
// it, or tear both down. *garageadmin.Client satisfies it; the interface exists so
// EnsureForIdentity/ProvisionObjectStore is testable (fake minter) without a live Garage
// admin API.
type objectStoreMinter interface {
	CreateBucket(ctx context.Context, globalAlias string) (string, error)
	BucketIDByAlias(ctx context.Context, globalAlias string) (string, error)
	CreateKey(ctx context.Context, name string) (accessKeyID, secretAccessKey string, err error)
	KeyIDByName(ctx context.Context, name string) (accessKeyID string, err error)
	AllowBucketKey(ctx context.Context, bucketID, accessKeyID string, perms garageadmin.Permissions) error
	DeleteBucket(ctx context.Context, bucketID string) error
	DeleteKey(ctx context.Context, accessKeyID string) error
}

var (
	_ objectStoreCredentialResolver = (*objectstore.IdentityStore)(nil)
	_ objectStoreMinter             = (*garageadmin.Client)(nil)
)

// objectStoreProvisionAdapter satisfies agui.ObjectStoreProvisioner over the Garage Admin
// API v2 client + the plan-06 identity_store (promoted verbatim from liveObjectStore).
// ProvisionObjectStore/EnsureForIdentity are idempotent (Resolve → skip when the secret row
// already exists); the adapter remembers each bucket's internal id so DeprovisionObjectStore
// can delete it.
type objectStoreProvisionAdapter struct {
	client    objectStoreMinter
	store     objectStoreCredentialResolver
	mu        sync.Mutex
	bucketIDs map[string]string
	// ensureCORS writes the browser-upload CORS rule onto a bucket using AURA'S OWN key, not
	// the identity's. Optional: nil in tests that are not about it, and in any wiring with no
	// S3 endpoint to talk to.
	ensureCORS func(ctx context.Context, bucket string) error
	// auraAccessKey is the key ensureCORS authenticates as, and therefore the key that must
	// own the bucket before Garage will accept the rule.
	auraAccessKey string
	corsDone      map[string]struct{}
	// emptier removes a bucket's objects before it is dropped, over the SAME shared aura
	// credentials ensureCORS uses. Optional for the same reason: nil in tests that are not
	// about it, and in any wiring with no S3 endpoint to talk to.
	emptier objectStoreEmptier
}

// objectStoreEmptier is the object-level half of the teardown, satisfied by
// *objectstore.S3Store. It is a separate port from objectStoreMinter because the two speak
// different protocols to different endpoints: the minter drives Garage's admin API, which
// has no object-level call at all, and this drives S3.
type objectStoreEmptier interface {
	List(ctx context.Context, req objectstore.ListRequest) ([]objectstore.ObjectInfo, error)
	Delete(ctx context.Context, ref objectstore.ObjectRef) error
}

var _ objectStoreEmptier = (*objectstore.S3Store)(nil)

func newObjectStoreProvisionAdapter(client objectStoreMinter, store objectStoreCredentialResolver) *objectStoreProvisionAdapter {
	return &objectStoreProvisionAdapter{
		client:    client,
		store:     store,
		bucketIDs: map[string]string{},
		corsDone:  map[string]struct{}{},
	}
}

// browserUploadCORSFor builds the ensureCORS hook: an S3 client bound to AURA's own key,
// writing the same rule cmd/aura/objectstore.go writes for the shared bucket.
//
// Aura's key, not the identity's, and that is the whole design. Garage gates PutBucketCors
// behind the owner permission, which garageadmin.ReadWrite denies an identity on purpose —
// owner would let it re-grant and delete its own bucket from the S3 data plane. So the process
// that creates the bucket is the one that owns it, and the identity keeps read+write.
//
// Nil when there is no endpoint to talk to, which leaves the hook off in a filesystem run.
func browserUploadCORSFor(cfg *config.Config) func(context.Context, string) error {
	if strings.TrimSpace(cfg.ObjectStoreEndpoint) == "" {
		return nil
	}
	return func(ctx context.Context, bucket string) error {
		store, err := objectstore.NewS3(ctx, objectstore.S3Config{
			Endpoint:       cfg.ObjectStoreEndpoint,
			PublicEndpoint: cfg.ObjectStorePublicEndpoint,
			Region:         cfg.ObjectStoreRegion,
			AccessKey:      cfg.ObjectStoreAccessKey,
			SecretKey:      cfg.ObjectStoreSecretKey,
			PathStyle:      cfg.ObjectStorePathStyle,
		})
		if err != nil {
			return err
		}
		return store.ConfigureBrowserUploadCORS(ctx, bucket)
	}
}

// sharedObjectStoreEmptierFor builds the teardown's object-level client on AURA's own key —
// the same credentials browserUploadCORSFor uses, and for the same reason: grantAuraOwnership
// gives that key owner rights on every per-identity bucket, while the identity's own key is
// deliberately not an owner and, on a resumed purge, no longer exists at all.
//
// Nil when there is no endpoint to talk to, which leaves a filesystem run exactly as it was.
//
// context.Background() is deliberate rather than a shortcut: NewS3 consumes its ctx only in
// awsconfig.LoadDefaultConfig, which issues no request — every actual call carries the saga's
// own ctx.
func sharedObjectStoreEmptierFor(cfg *config.Config) objectStoreEmptier {
	if strings.TrimSpace(cfg.ObjectStoreEndpoint) == "" {
		return nil
	}
	store, err := objectstore.NewS3(context.Background(), objectstore.S3Config{
		Endpoint:       cfg.ObjectStoreEndpoint,
		PublicEndpoint: cfg.ObjectStorePublicEndpoint,
		Region:         cfg.ObjectStoreRegion,
		AccessKey:      cfg.ObjectStoreAccessKey,
		SecretKey:      cfg.ObjectStoreSecretKey,
		PathStyle:      cfg.ObjectStorePathStyle,
	})
	if err != nil {
		slog.Warn("aura serve: object-store teardown client unavailable — a purge will leave a non-empty bucket for Garage to refuse", "err", err)
		return nil
	}
	return store
}

// configureCORS gives an identity's own bucket the rule that lets a browser PUT to it.
//
// It exists because the "one bucket per identity" split left a gap nothing reported:
// ConfigureBrowserUploadCORS runs once at boot against the SHARED bucket, so a minted
// identity bucket never got the rule and every browser upload died in the preflight. The
// presign still succeeded, the PUT was refused before it reached Garage, and the asset row
// stayed at 'presigned' — measured on the live deployment 2026-08-13, where every 'web' row
// sits there and only 'agent' rows (server-side Put, no browser) complete.
//
// On the RESOLVE path too, not just the mint: the buckets that need this already exist, so a
// fix that only ran on CreateBucket would repair nothing that is broken today. Once per
// identity per process, because this sits on the path every presign takes.
//
// A failure is logged, never returned. The bucket is provisioned and every server-side path
// still works; refusing the credentials would break more than the missing rule does.
func (a *objectStoreProvisionAdapter) configureCORS(ctx context.Context, bucket string) {
	if a.ensureCORS == nil || a.auraAccessKey == "" {
		return
	}
	a.mu.Lock()
	_, done := a.corsDone[bucket]
	if !done {
		a.corsDone[bucket] = struct{}{}
	}
	bucketID := a.bucketIDs[bucket]
	a.mu.Unlock()
	if done {
		return
	}
	if err := a.grantAuraOwnership(ctx, bucket, bucketID); err != nil {
		slog.Warn("objectstore: browser-upload CORS not configured; uploads from the cockpit will fail",
			"bucket", redact.Line(bucket), "stage", "grant", "err", err)
		return
	}
	if err := a.ensureCORS(ctx, bucket); err != nil {
		slog.Warn("objectstore: browser-upload CORS not configured; uploads from the cockpit will fail",
			"bucket", redact.Line(bucket), "stage", "put-cors", "err", err)
	}
}

// grantAuraOwnership makes Aura's own key the bucket's owner, which is what Garage requires
// before it will accept a bucket-level configuration call. The identity's key is untouched
// and stays read+write: see garageadmin.ReadWriteOwner for why that split is the whole point.
//
// BucketIDByAlias, not CreateBucket: on the resolve path the bucket was minted by an earlier
// process, and asking "create" for an id that already exists reads as a mint at the call site
// — a test written against this counted one and was right to.
func (a *objectStoreProvisionAdapter) grantAuraOwnership(ctx context.Context, bucket, bucketID string) error {
	if bucketID == "" {
		id, err := a.client.BucketIDByAlias(ctx, bucket)
		if err != nil {
			return err
		}
		bucketID = id
		a.mu.Lock()
		a.bucketIDs[bucket] = bucketID
		a.mu.Unlock()
	}
	return a.client.AllowBucketKey(ctx, bucketID, a.auraAccessKey, garageadmin.ReadWriteOwner)
}

// EnsureForIdentity synchronously and idempotently resolves an identity's object-store
// credentials (Amendment #88 Task 3): the shared aura-assets bucket for the local/operator
// principal (D-11), or the identity's OWN aura-<id> bucket + scoped key — minting it on
// first use if absent. Both the shared path and an already-provisioned identity resolve via
// the fast a.store.Resolve call with zero minting. A resolve error other than "not yet
// provisioned" (pgx.ErrNoRows) is fail-closed and propagated without minting anything.
func (a *objectStoreProvisionAdapter) EnsureForIdentity(ctx context.Context, id string) (objectstore.Credentials, error) {
	ictx := identityctx.WithIdentityID(ctx, id)
	if creds, err := a.store.Resolve(ictx); err == nil {
		// Only the identity's OWN bucket. The shared one is configured at boot by
		// cmd/aura/objectstore.go, and a second writer of the same rule would just be one
		// more thing to keep in step. Compared against the derived name rather than a
		// literal, so the two cannot drift.
		if own, derr := garageadmin.BucketForIdentity(id); derr == nil && creds.Bucket == own {
			a.configureCORS(ctx, creds.Bucket)
		}
		return creds, nil // shared principal OR already provisioned — idempotent no-mint
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Credentials{}, err
	}
	bucket, err := garageadmin.BucketForIdentity(id)
	if err != nil {
		return objectstore.Credentials{}, err
	}
	bucketID, err := a.client.CreateBucket(ctx, bucket)
	if err != nil {
		return objectstore.Credentials{}, err
	}
	ak, sk, err := a.client.CreateKey(ctx, "aura-"+id)
	if err != nil {
		return objectstore.Credentials{}, err
	}
	if err := a.client.AllowBucketKey(ctx, bucketID, ak, garageadmin.ReadWrite); err != nil {
		return objectstore.Credentials{}, err
	}
	if err := a.store.Put(ctx, id, bucket, ak, sk); err != nil {
		return objectstore.Credentials{}, err
	}
	a.mu.Lock()
	a.bucketIDs[id] = bucketID
	a.mu.Unlock()
	creds := objectstore.Credentials{Bucket: bucket, AccessKey: ak, SecretKey: sk}
	a.configureCORS(ctx, creds.Bucket)
	return creds, nil
}

// ProvisionObjectStore satisfies agui.ObjectStoreProvisioner by delegating to
// EnsureForIdentity and discarding the minted/resolved credentials — the saga contract only
// needs success/failure, not the triple.
func (a *objectStoreProvisionAdapter) ProvisionObjectStore(ctx context.Context, id string) error {
	_, err := a.EnsureForIdentity(ctx, id)
	return err
}

// DeprovisionObjectStore removes the identity's scoped key, then its bucket, then the row
// that names them — in that order, so a failure anywhere leaves the row still pointing at
// whatever survived and the resumable saga can finish the job on a re-run. Deleting the row
// first would strand the key with nothing left to find it by.
//
// MEASURED 2026-09-08 on the live deployment, purging twelve identities: the previous shape
// left all twelve keys AND all twelve buckets behind while journalling the step `done`. Two
// independent causes, both closed here. DeleteKey's error was discarded, so an unreachable
// admin API (AURA_GARAGE_ADMIN_ENDPOINT defaults to compose's internal http://garage:3903,
// which does not resolve outside the compose network) read as a successful teardown. And the
// bucket id was read only from the in-process map the PROVISIONING process filled, which a
// purge process — a later one by days, by design — never has; garageadmin.Client has
// published BucketIDByAlias the whole time, and the alias is derived from the identity id.
func (a *objectStoreProvisionAdapter) DeprovisionObjectStore(ctx context.Context, id string) error {
	if err := a.deprovisionKey(ctx, id); err != nil {
		return err
	}
	bucketID, err := a.deprovisionBucketID(ctx, id)
	if err != nil {
		return err
	}
	if bucketID != "" {
		if err := a.emptyBucket(ctx, id, bucketID); err != nil {
			return err
		}
		if err := a.client.DeleteBucket(ctx, bucketID); err != nil {
			return err
		}
	}
	return a.store.Delete(ctx, id)
}

// emptyBucket removes every object in the identity's bucket, because Garage refuses to drop
// a bucket that still holds one: DeleteBucket answers 409 BucketNotEmpty, measured
// 2026-09-08 against the live admin API on four buckets each holding a single 154-byte seed
// document. Emptying is an S3 operation — the admin API offers no object-level call at all —
// so this composes objectstore.S3Store's existing List and Delete rather than reaching for
// anything new.
//
// It runs through the SHARED aura credentials, which grantAuraOwnership has given owner
// rights on every per-identity bucket, and NOT through the identity's own key: that key is
// already deleted by this point, and on a resumed purge its credential row is gone too.
// A nil emptier leaves the previous behaviour intact — the delete is still attempted and
// Garage's own BucketNotEmpty is the loud failure — so a deployment with no S3 credentials
// wired is no worse off than before.
func (a *objectStoreProvisionAdapter) emptyBucket(ctx context.Context, id, bucketID string) error {
	if a.emptier == nil {
		return nil
	}
	bucket, err := garageadmin.BucketForIdentity(id)
	if err != nil {
		return err
	}
	// Re-assert the ownership provisioning grants, because a bucket that missed it is
	// otherwise undeletable by anything. Measured 2026-09-08: three orphaned buckets carried
	// ONE object each and ZERO authorized keys, so ListObjectsV2 answered 403 AccessDenied
	// and no code path could empty them — and Garage will not drop a non-empty bucket. The
	// grant is idempotent, it is the same one grantAuraOwnership makes, and it lands on a
	// bucket that is about to be destroyed.
	if a.auraAccessKey != "" {
		if err := a.client.AllowBucketKey(ctx, bucketID, a.auraAccessKey, garageadmin.ReadWriteOwner); err != nil {
			return fmt.Errorf("grant teardown access to %q: %w", bucket, err)
		}
	}
	objects, err := a.emptier.List(ctx, objectstore.ListRequest{Bucket: bucket})
	if err != nil {
		return fmt.Errorf("list objects in %q: %w", bucket, err)
	}
	for _, obj := range objects {
		// Addressed by the derived bucket rather than the listing's own Ref, so a
		// surprising response can never point the delete at another identity's bucket.
		ref := objectstore.ObjectRef{Bucket: bucket, Key: obj.Ref.Key}
		if err := a.emptier.Delete(ctx, ref); err != nil {
			return fmt.Errorf("delete object %q in %q: %w", ref.Key, bucket, err)
		}
	}
	return nil
}

// deprovisionKey deletes the identity's scoped key, taking the access key id from the
// credential row when it is still there and falling back to the name Garage knows it by
// when it is not. The fallback is not hypothetical: it is the state a run whose key delete
// silently failed left the live deployment in on 2026-09-08 — row gone, key live, and no
// access key id recorded anywhere. An absent key on either route is a converged step.
func (a *objectStoreProvisionAdapter) deprovisionKey(ctx context.Context, id string) error {
	ictx := identityctx.WithIdentityID(ctx, id)
	creds, err := a.store.Resolve(ictx)
	if err == nil {
		return a.client.DeleteKey(ctx, creds.AccessKey)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	name, err := garageadmin.KeyNameForIdentity(id)
	if err != nil {
		return err
	}
	accessKey, err := a.client.KeyIDByName(ctx, name)
	if errors.Is(err, garageadmin.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return a.client.DeleteKey(ctx, accessKey)
}

// deprovisionBucketID resolves the bucket to delete, preferring the id this adapter
// instance minted and falling back to the alias derived from the identity. An empty return
// with a nil error means there is nothing left to delete: an alias Garage does not know
// (ErrBucketNotFound) is a converged step, unlike a lookup that could not be read at all.
func (a *objectStoreProvisionAdapter) deprovisionBucketID(ctx context.Context, id string) (string, error) {
	a.mu.Lock()
	bucketID := a.bucketIDs[id]
	a.mu.Unlock()
	if bucketID != "" {
		return bucketID, nil
	}
	bucket, err := garageadmin.BucketForIdentity(id)
	if err != nil {
		return "", err
	}
	bucketID, err = a.client.BucketIDByAlias(ctx, bucket)
	if errors.Is(err, garageadmin.ErrBucketNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return bucketID, nil
}
