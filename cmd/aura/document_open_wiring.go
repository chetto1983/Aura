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
	index, err := newRuntimeDocumentIndex(o.cfg, false)
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

// OpenObject hands the document opener the bytes and the media type the store recorded.
func (o identityObjectOpener) OpenObject(
	ctx context.Context,
	identityID, key string,
) (io.ReadCloser, string, error) {
	store, bucket, err := o.resolve(ctx, identityID, key)
	if err != nil {
		return nil, "", err
	}
	body, attrs, err := store.Get(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key})
	if err != nil {
		return nil, "", err
	}
	return body, attrs.MIMEType, nil
}

// OpenSeekable is what the file manager's direct route reads: what the store knows about the
// object, which it needs to render a file inline instead of only offering it as a download, and
// bytes it can serve from any offset, which Range requests need. The SAME resolve as OpenObject,
// so the two cannot come to disagree about which bucket a key belongs to.
func (o identityObjectOpener) OpenSeekable(
	ctx context.Context,
	identityID, key string,
) (*objectstore.SeekableObject, agui.FileAttrs, error) {
	store, bucket, err := o.resolve(ctx, identityID, key)
	if err != nil {
		return nil, agui.FileAttrs{}, err
	}
	object, err := objectstore.OpenSeekableObject(ctx, store, objectstore.ObjectRef{Bucket: bucket, Key: key})
	if err != nil {
		return nil, agui.FileAttrs{}, err
	}
	attrs := object.Attrs()
	return object, agui.FileAttrs{
		MIMEType:  attrs.MIMEType,
		SizeBytes: attrs.SizeBytes,
		// The object's metadata, not the index: it is already in this response, it is right
		// for an object the ingest sidecar has not reached yet, and it is the same string the
		// sidecar itself reads to name the document.
		FileName: objectstore.DecodeFileName(attrs.Metadata[objectstore.MetadataFileName]),
	}, nil
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
