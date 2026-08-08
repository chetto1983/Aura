package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeDocumentOpener backs the document_open tool. It builds the object store
// and asset service per call — the same lazy shape runtimeDocumentIngestor uses
// for the opposite direction — so the tool can be registered from the shared
// buildRegistry without dragging boot-time object-store construction into every
// path that only wants a manifest.
type runtimeDocumentOpener struct {
	cfg  *config.Config
	pool *pgxpool.Pool
}

func newRuntimeDocumentOpener(cfg *config.Config, pool *pgxpool.Pool) *runtimeDocumentOpener {
	return &runtimeDocumentOpener{cfg: cfg, pool: pool}
}

func (o *runtimeDocumentOpener) OpenDocument(
	ctx context.Context,
	identityID, documentID string,
) (io.ReadCloser, documents.OpenedDocument, error) {
	if o == nil || o.cfg == nil || o.pool == nil {
		return nil, documents.OpenedDocument{}, fmt.Errorf("document opener is not configured")
	}
	objectStore, err := buildObjectStore(ctx, o.cfg)
	if err != nil {
		return nil, documents.OpenedDocument{}, fmt.Errorf("object store: %w", err)
	}
	index, err := newRuntimeDocumentIndex(o.cfg, nil, false)
	if err != nil {
		return nil, documents.OpenedDocument{}, fmt.Errorf("document index: %w", err)
	}
	service := &documents.OpenService{
		Index:   index,
		Objects: identityObjectOpener{objects: objectStore, resolver: buildObjectResolverBundle(o.cfg, o.pool), bucket: o.cfg.ObjectStoreBucket},
	}
	return service.OpenDocument(ctx, identityID, documentID)
}

// identityObjectOpener reads and writes objects with the OWNER's own credentials.
//
// The resolver is the gate, not a courtesy: it returns the identity's own store and bucket,
// so a key belonging to somebody else cannot be touched even if the caller supplied one. A
// nil resolver is the pre-provisioning deployment, where the shared store is the only store.
//
// Both directions go through the SAME resolve, so a read and a write can never disagree
// about which bucket a key belongs to.
type identityObjectOpener struct {
	objects  objectstore.Store
	resolver *assets.ObjectResolverBundle
	bucket   string
}

func (o identityObjectOpener) resolve(
	ctx context.Context, identityID, key string,
) (objectstore.Store, string, error) {
	if o.objects == nil {
		return nil, "", fmt.Errorf("object store is not configured")
	}
	if strings.TrimSpace(key) == "" {
		return nil, "", fmt.Errorf("no object key was given")
	}
	if o.resolver == nil {
		return o.objects, o.bucket, nil
	}
	store, bucket, err := o.resolver.ResolveForIdentity(ctx, o.objects, identityID)
	if err != nil {
		return nil, "", err
	}
	return store, bucket, nil
}

func (o identityObjectOpener) OpenObject(
	ctx context.Context,
	identityID, key string,
) (io.ReadCloser, error) {
	store, bucket, err := o.resolve(ctx, identityID, key)
	if err != nil {
		return nil, err
	}
	body, _, err := store.Get(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key})
	return body, err
}

// ReadObject is OpenObject plus what the store knows about the bytes, which the file
// manager needs to render a file inline instead of only offering it as a download.
func (o identityObjectOpener) ReadObject(
	ctx context.Context,
	identityID, key string,
) (io.ReadCloser, agui.FileAttrs, error) {
	store, bucket, err := o.resolve(ctx, identityID, key)
	if err != nil {
		return nil, agui.FileAttrs{}, err
	}
	body, attrs, err := store.Get(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key})
	if err != nil {
		return nil, agui.FileAttrs{}, err
	}
	return body, agui.FileAttrs{MIMEType: attrs.MIMEType, SizeBytes: attrs.SizeBytes}, nil
}

// PutObject stores an uploaded file in the owner's own bucket. Nothing is enqueued: the
// ingest sidecar reconciles the bucket, so appearing in it IS the trigger.
func (o identityObjectOpener) PutObject(
	ctx context.Context,
	identityID, key, mimeType string,
	size int64,
	body io.Reader,
) error {
	store, bucket, err := o.resolve(ctx, identityID, key)
	if err != nil {
		return err
	}
	_, err = store.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key}, body,
		objectstore.PutOptions{MIMEType: mimeType, Size: size})
	return err
}
