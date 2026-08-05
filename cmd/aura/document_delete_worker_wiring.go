package main

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	assetspkg "github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimeDurableDeleteProcessor struct {
	worker     *documents.DurableDeleteWorker
	identityID string
	workerID   string
}

func (p *runtimeDurableDeleteProcessor) ProcessOnce(ctx context.Context) (int, error) {
	results, err := p.worker.ProcessOwner(ctx, documents.ClaimDeleteJobsRequest{
		IdentityID: p.identityID, WorkerID: p.workerID,
		LeaseDuration: 20 * time.Minute, BatchSize: 1,
	})
	return len(results), err
}

func newRuntimeDurableDeleteTenantProcessor(
	cfg *config.Config,
	pool *pgxpool.Pool,
	accepted acceptedAssetProcessor,
	router *usersandbox.SandboxRouter,
) (runtimeIngestionProcessor, bool) {
	assetService, ok := accepted.(*assetspkg.Service)
	if !ok || cfg == nil || pool == nil || router == nil || assetService.Objects == nil {
		return nil, false
	}
	index, err := newRuntimeDocumentIndex(cfg, nil, false)
	if err != nil {
		return &runtimeFailedProcessor{err: fmt.Errorf("durable document delete: %w", err)}, true
	}
	bundle := &assetspkg.ObjectResolverBundle{
		Resolver: assetService.IdentityObjects, PerIdentityStore: assetService.PerIdentityStore,
		SharedBucket: assetService.Bucket,
	}
	worker := &documents.DurableDeleteWorker{
		Store:     documents.NewPostgresDurableDeleteStore(pool),
		Projector: runtimeDeleteProjector{index: index},
		Objects: runtimeDeleteObjectResolver{
			bundle: bundle, shared: assetService.Objects,
		},
		Staged: runtimeStagedDocumentPurger{router: router},
		Config: documents.DurableDeleteWorkerConfig{
			LeaseDuration: 20 * time.Minute, RetryBase: time.Minute,
		},
	}
	return &runtimeTenantIngestionProcessor{
		identities: identity.New(pool), width: runtimeDeleteWorkerWidth,
		workerIDPrefix: runtimeDeleteWorkerID,
		worker: func(identityID, workerID string) runtimeIngestionProcessor {
			return &runtimeDurableDeleteProcessor{
				worker: worker, identityID: identityID, workerID: workerID,
			}
		},
	}, true
}

type runtimeFailedProcessor struct {
	err error
}

func (p *runtimeFailedProcessor) ProcessOnce(context.Context) (int, error) {
	return 0, p.err
}

type runtimeDeleteObjectResolver struct {
	bundle *assetspkg.ObjectResolverBundle
	shared objectstore.Store
}

func (r runtimeDeleteObjectResolver) ResolveDeleteObjectStore(
	ctx context.Context,
	identityID string,
) (documents.ResolvedDeleteObjectStore, error) {
	store, bucket, err := r.bundle.ResolveForIdentity(ctx, r.shared, identityID)
	return documents.ResolvedDeleteObjectStore{Store: store, Bucket: bucket}, err
}

type runtimeDeleteProjector struct {
	index *arcadedb.DocumentIndex
}

func (p runtimeDeleteProjector) TombstoneGeneration(
	ctx context.Context,
	identityID, documentID, generation string,
) (documents.DeleteProjectionResult, error) {
	result, err := p.index.TombstoneGeneration(ctx, identityID, documentID, generation)
	return documents.DeleteProjectionResult{Found: result.Found}, err
}

func (p runtimeDeleteProjector) DeleteGeneration(
	ctx context.Context,
	identityID, documentID, generation string,
) (documents.DeleteProjectionResult, error) {
	result, err := p.index.DeleteGeneration(ctx, identityID, documentID, generation)
	return documents.DeleteProjectionResult{Found: result.Found}, err
}
