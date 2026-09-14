package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	visionCapabilityTimeout = 5 * time.Second
	visionCapabilityTTL     = time.Minute
	// assetMaxVideoBytesTimeout bounds the boot-time AURA_ASSET_MAX_VIDEO_BYTES read the
	// same way visionCapabilityTimeout bounds the vision-route probe above: an aura.settings
	// row lookup over the pool buildAssetService already holds, not a network call, but still
	// bounded so a stalled pool cannot hang boot.
	assetMaxVideoBytesTimeout = 5 * time.Second
)

func buildAssetService(cfg *config.Config, pool *pgxpool.Pool, objectStore objectstore.Store) *assets.Service {
	docProcessor := &assets.DocumentProcessor{}
	imageProc := assets.NewImageProcessor(objectStore, visionConfigFrom(cfg))
	audioProc := assets.NewAudioProcessor(objectStore, sttConfigFrom(cfg))
	svc := &assets.Service{
		Store:          assets.NewStore(pool),
		Objects:        objectStore,
		ProcessingJobs: newRuntimeAssetProcessingQueue(pool),
		Processors: assets.ProcessorSet{
			Document: docProcessor,
			// An uploaded image gets a vision summary (inline chat) AND the document id the
			// ingest sidecar will file it under, so document_open can hand the agent the file
			// once the bucket is reconciled. The document leg NAMES only — no OCR, no chunking
			// runs here (see ImageDocumentProcessor). Fail-soft: either leg can be down.
			Image: &assets.ImageDocumentProcessor{
				Vision:   imageProc,
				Document: docProcessor,
			},
			Audio: audioProc,
		},
		Limits: assets.Limits{
			MaxDocumentBytes: int64(cfg.AssetMaxDocumentBytes),
			MaxImageBytes:    int64(cfg.AssetMaxImageBytes),
			MaxAudioBytes:    int64(cfg.AssetMaxAudioBytes),
			MaxVideoBytes:    assetMaxVideoBytesFor(cfg, pool),
		},
		Bucket:     cfg.ObjectStoreBucket,
		PresignTTL: time.Duration(cfg.AssetPresignTTLSec) * time.Second,
	}
	// Per-identity object isolation (D-08 / VERIF-4): when the pool + Authula KEK secret are
	// present, route every asset object op through the IdentityStore resolver so an identity's
	// bytes land in ITS OWN Garage bucket under ITS OWN key. Absent → the shared Objects store
	// handles every op (pre-cutover / interview-only deploy; assets_test.go's nil-pool call stays
	// valid — no signature change).
	// The knowledge catalog asks the index which documents exist, because the status column it
	// used to read has stopped being written: measured on the live deployment 2026-08-13, no
	// asset had EVER reached 'searchable', so the agent was never told about a single uploaded
	// file. Absent ArcadeDB the field stays nil and the catalog advertises nothing, which is
	// the honest answer when there is no index to ask.
	if strings.TrimSpace(cfg.ArcadeDB.BaseURL) != "" {
		if index, err := newRuntimeDocumentIndex(cfg, nil, false); err == nil {
			svc.DocumentScope = &documents.ArcadeRetrievalControlPlane{Index: index}
		} else {
			slog.Warn("aura assets: knowledge catalog disabled — no document index", "err", err)
		}
	}
	if bundle := buildObjectResolverBundle(cfg, pool); bundle != nil {
		svc.IdentityObjects = bundle.Resolver
		svc.PerIdentityStore = bundle.PerIdentityStore
		imageProc.PerIdentityObjects = bundle
		audioProc.PerIdentityObjects = bundle
	}
	return svc
}

// buildObjectResolverBundle constructs the per-identity object-store resolver + a caching
// per-identity S3Store factory, or nil when the daemon is not yet multi-user provisioned
// (no pool, or no AURA_AUTHULA_SECRET to derive the credential-wrapping KEK). A NewIdentityStore
// failure logs + returns nil so boot never aborts — the shared path still serves every op.
func buildObjectResolverBundle(cfg *config.Config, pool *pgxpool.Pool) *assets.ObjectResolverBundle {
	if pool == nil || strings.TrimSpace(cfg.AuthulaSecret) == "" {
		return nil
	}
	shared := objectstore.Credentials{
		Bucket:    cfg.ObjectStoreBucket,
		AccessKey: cfg.ObjectStoreAccessKey,
		SecretKey: cfg.ObjectStoreSecretKey,
	}
	resolver, err := objectstore.NewIdentityStore(pool, cfg.AuthulaSecret, shared, localSeededIdentityID)
	if err != nil {
		slog.Warn("aura assets: per-identity object resolver disabled", "err", err)
		return nil
	}
	return &assets.ObjectResolverBundle{
		Resolver:         ensuringObjectResolver(cfg, resolver),
		PerIdentityStore: newCachingPerIdentityStoreFactory(cfg),
		SharedBucket:     cfg.ObjectStoreBucket,
	}
}

// ensuringObjectResolver makes an identity's bucket exist the first time that identity
// reaches for it, instead of only when the onboarding saga happened to run.
//
// EnsureForIdentity already documents itself as "minting it on first use if absent" and is
// idempotent, but its ONLY caller was the onboarding resource leg. Identities that arrived
// any other way therefore had no row at all — measured, 26 of them — and the layer above
// used to answer that by handing every one of them the shared bucket. That is now a
// refusal (F-007), which is correct and would leave those identities unable to store
// anything, so the missing half is minting on demand rather than a manual repair step.
//
// Falls back to the plain resolver when the Garage admin API is not configured: an
// interview-only deploy mints nothing, exactly as buildProvisioningPorts decides.
func ensuringObjectResolver(cfg *config.Config, resolver *objectstore.IdentityStore) assets.ObjectResolver {
	endpoint := strings.TrimSpace(cfg.GarageAdminEndpoint)
	token := strings.TrimSpace(cfg.GarageAdminToken)
	if endpoint == "" || token == "" {
		return resolver
	}
	client, err := garageadmin.New(endpoint, token)
	if err != nil {
		slog.Warn("aura assets: garage admin unavailable — buckets are not minted on demand", "err", err)
		return resolver
	}
	adapter := newObjectStoreProvisionAdapter(client, resolver)
	adapter.ensureCORS = browserUploadCORSFor(cfg)
	adapter.auraAccessKey = cfg.ObjectStoreAccessKey
	return mintingResolver{adapter: adapter}
}

type mintingResolver struct{ adapter *objectStoreProvisionAdapter }

func (m mintingResolver) Resolve(ctx context.Context) (objectstore.Credentials, error) {
	return m.adapter.EnsureForIdentity(ctx, identityctx.IdentityID(ctx))
}

// newCachingPerIdentityStoreFactory builds a StoreFactory that layers resolved per-identity
// creds on the SHARED S3 endpoint/region/path-style, caching one S3Store per access key
// (sync.Map) so repeated requests reuse a client. NewS3 is a purely local construction (static
// creds, no network round-trip), so a background context is correct for the lazy build.
func newCachingPerIdentityStoreFactory(cfg *config.Config) assets.StoreFactory {
	var cache sync.Map // access key -> objectstore.Store
	return func(creds objectstore.Credentials) (objectstore.Store, error) {
		if v, ok := cache.Load(creds.AccessKey); ok {
			return v.(objectstore.Store), nil
		}
		st, err := objectstore.NewS3(context.Background(), objectstore.S3Config{
			Endpoint:       cfg.ObjectStoreEndpoint,
			PublicEndpoint: cfg.ObjectStorePublicEndpoint,
			Region:         cfg.ObjectStoreRegion,
			AccessKey:      creds.AccessKey,
			SecretKey:      creds.SecretKey,
			PathStyle:      cfg.ObjectStorePathStyle,
		})
		if err != nil {
			return nil, err
		}
		actual, _ := cache.LoadOrStore(creds.AccessKey, objectstore.Store(st))
		return actual.(objectstore.Store), nil
	}
}

// visionConfigFrom resolves the image route once at boot, which is the lifetime the
// primary-route settings already have: changing them is an appliedRestart key, so a
// snapshot here cannot drift from what the running process is using.
func visionConfigFrom(cfg *config.Config) multimodal.VisionConfig {
	ctx, cancel := context.WithTimeout(context.Background(), visionCapabilityTimeout)
	defer cancel()
	source := llm.NewContentCapabilitySource(cfg.LLM, visionCapabilityTTL)
	return multimodal.VisionConfigFrom(cfg, multimodal.PrimaryAcceptsImages(ctx, source))
}

func sttConfigFrom(cfg *config.Config) multimodal.STTConfig {
	return multimodal.STTConfigFrom(cfg)
}

// assetMaxVideoBytesFor resolves assets.Limits.MaxVideoBytes at boot, over a settings store
// built from the SAME pool buildAssetService already holds (ruling R3: never a second pool).
// No pool at all (the pool-free tests/paths buildAssetService already supports) is the one
// case with no aura.settings to check, so it takes bootAssetMaxVideoBytes' own nil-lister
// contract (the compiled default). Any other failure to read AURA_ASSET_MAX_VIDEO_BYTES —
// the settings store itself failing to build, or resolveAssetMaxVideoBytes below — fails
// CLOSED to 0 so every video upload is refused, never silently falling back to the default
// and hiding a real stored value the store failed to read.
func assetMaxVideoBytesFor(cfg *config.Config, pool *pgxpool.Pool) int64 {
	if pool == nil {
		return resolveAssetMaxVideoBytes(context.Background(), nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), assetMaxVideoBytesTimeout)
	defer cancel()
	store, err := settings.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("aura assets: settings store unavailable — video assets refused and video job recovery disabled until the next restart", "err", err)
		return 0
	}
	return resolveAssetMaxVideoBytes(ctx, store)
}

// resolveAssetMaxVideoBytes is the fail-closed decision isolated from pool/store
// construction so it is unit-testable with a fake settings.Lister, no daemon required: a
// store read error or a stored value bootAssetMaxVideoBytes rejects returns 0, a nil lister
// or an absent row returns the compiled default. The video watcher reads the same 0 and is
// not built, so detached video jobs wait for a restart with a usable ceiling.
func resolveAssetMaxVideoBytes(ctx context.Context, lister settings.Lister) int64 {
	maxBytes, err := bootAssetMaxVideoBytes(ctx, lister)
	if err != nil {
		slog.Warn("aura assets: AURA_ASSET_MAX_VIDEO_BYTES unreadable — video assets refused and video job recovery disabled until the next restart", "err", err)
		return 0
	}
	return maxBytes
}
