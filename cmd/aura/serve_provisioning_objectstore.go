package main

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
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
	CreateKey(ctx context.Context, name string) (accessKeyID, secretAccessKey string, err error)
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
}

func newObjectStoreProvisionAdapter(client objectStoreMinter, store objectStoreCredentialResolver) *objectStoreProvisionAdapter {
	return &objectStoreProvisionAdapter{client: client, store: store, bucketIDs: map[string]string{}}
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
	return objectstore.Credentials{Bucket: bucket, AccessKey: ak, SecretKey: sk}, nil
}

// ProvisionObjectStore satisfies agui.ObjectStoreProvisioner by delegating to
// EnsureForIdentity and discarding the minted/resolved credentials — the saga contract only
// needs success/failure, not the triple.
func (a *objectStoreProvisionAdapter) ProvisionObjectStore(ctx context.Context, id string) error {
	_, err := a.EnsureForIdentity(ctx, id)
	return err
}

func (a *objectStoreProvisionAdapter) DeprovisionObjectStore(ctx context.Context, id string) error {
	ictx := identityctx.WithIdentityID(ctx, id)
	if creds, err := a.store.Resolve(ictx); err == nil {
		_ = a.client.DeleteKey(ctx, creds.AccessKey)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	a.mu.Lock()
	bucketID := a.bucketIDs[id]
	a.mu.Unlock()
	// bucketID is only remembered by the SAME adapter instance that provisioned it, so a
	// purge running in a later process (the grace window is days) cannot delete the bucket
	// this way — the key + DB row ARE removed (Resolve/store.Delete read the DB), leaving an
	// inert, credential-less bucket. That residual bucket is recorded as a data-retention
	// follow-up in 36-14-SUMMARY (fail-closed-secure: no key → unreachable).
	if bucketID != "" {
		if err := a.client.DeleteBucket(ctx, bucketID); err != nil {
			return err
		}
	}
	return a.store.Delete(ctx, id)
}
