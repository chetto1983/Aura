package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/chetto1983/aura/internal/settings"
)

// mediaResumeRetryInterval spaces the retries of a boot recovery that could not reach every
// owner. A late resume loses nothing: a job's ceiling counts from its stored creation time, and
// a job resumed past it still gets its final attempt.
const mediaResumeRetryInterval = 30 * time.Second

// mediaDeps are the live media dependencies serve builds once, for every media tool and the
// video watcher: one catalog and one client over one HTTP client with no overall timeout — a
// generation is bounded by the tool call's context, not by a transport deadline shorter than it.
type mediaDeps struct {
	credentials mediaCredentials
	// settings is nil when the settings store cannot be built; the tools then refuse.
	settings   mediagen.Settings
	catalog    *mediagen.Catalog
	client     *mediagen.Client
	references mediaAssetAdapter
	// jobs is nil without a pool: no job can be persisted or resumed.
	jobs mediagen.JobStore
	// maxVideoBytes is the video ceiling read once at boot for assets.Limits.MaxVideoBytes.
	maxVideoBytes int64
}

// newMediaDeps returns nil without an asset service: nothing a generation produces could be
// stored.
func newMediaDeps(chat *chatEnv) *mediaDeps {
	if chat.assets == nil {
		return nil
	}
	httpClient := &http.Client{}
	media := &mediaDeps{
		catalog:       mediagen.NewCatalog(httpClient),
		client:        mediagen.NewClient(httpClient, chat.assets.Limits.MaxImageBytes),
		references:    mediaAssetAdapter{svc: chat.assets},
		maxVideoBytes: chat.assets.Limits.MaxVideoBytes,
	}
	// identityLLMResolver answers a nil pointer without a pool; stored in the interface it
	// would stop reading as nil and be called through.
	if resolver := identityLLMResolver(chat); resolver != nil {
		media.credentials.resolver = resolver
	}
	if store, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret); err != nil {
		slog.Warn("aura serve: media settings unavailable — generation tools refused", "err", err)
	} else {
		media.settings = newMediaSettings(store)
	}
	if chat.pool != nil {
		media.jobs = mediagen.NewStore(chat.pool)
	}
	return media
}

// wireMediaTools gives the retained media tools their live dependencies. A tool that cannot be
// served is left with no dependencies at all, so it refuses before any paid request instead of
// running on a partial wiring.
func wireMediaTools(chat *chatEnv, media *mediaDeps) {
	image := chat.toolHandles.ImageGenerate
	if image == nil || media == nil || media.settings == nil {
		return
	}
	image.Credentials = media.credentials
	image.Settings = media.settings
	image.Catalog = media.catalog
	image.Client = media.client
	image.References = media.references
	image.Assets = sendFileAssetAdapter{svc: chat.assets}
	image.MaxImageBytes = chat.assets.Limits.MaxImageBytes
}

// wireVideoTool gives the retained video tool its live dependencies once the watcher exists, all
// or nothing like the image tool: without a watcher no submitted job could be supervised or woken,
// so the tool keeps its zero value and refuses before any paid request.
func wireVideoTool(chat *chatEnv, media *mediaDeps, watcher *mediagen.Watcher) {
	video := chat.toolHandles.VideoGenerate
	if video == nil || media == nil || media.settings == nil || watcher == nil {
		return
	}
	video.Credentials = media.credentials
	video.Settings = media.settings
	video.Catalog = media.catalog
	video.Client = media.client
	video.References = media.references
	video.Jobs = media.jobs
	video.Watcher = watcher
	video.VideoAssets = media.references
	video.MaxImageBytes = chat.assets.Limits.MaxImageBytes
	video.MaxVideoBytes = media.maxVideoBytes
}

// newMediaWatcher builds the daemon's one video watcher, living as long as ctx, or returns nil
// when no job can be persisted or the boot video ceiling bounds nothing — NewWatcher would panic
// on that ceiling, and every clip would be refused under it anyway.
func newMediaWatcher(ctx context.Context, media *mediaDeps, notify func(mediagen.Completion)) *mediagen.Watcher {
	if media == nil || media.jobs == nil {
		return nil
	}
	if err := mediagen.ValidByteLimit(media.maxVideoBytes); err != nil {
		slog.Warn("aura serve: no usable video byte ceiling — detached video jobs are not supervised",
			"max_video_bytes", media.maxVideoBytes)
		return nil
	}
	return mediagen.NewWatcher(ctx, media.jobs, media.client, media.credentials, media.references, notify,
		mediagen.WatcherOptions{
			PollInterval:  mediagen.VideoPollInterval,
			MaxAge:        mediagen.VideoJobMaxAge,
			MaxVideoBytes: media.maxVideoBytes,
		})
}

type mediaJobResumer interface {
	Resume(ctx context.Context, ownerID string) error
}

// mediaJobRecovery resumes the video jobs of the identities that existed at boot, each owner
// exactly once: Resume is not idempotent, and a second one wakes a completed, undelivered job
// again. The job store has no cross-identity listing, so the owners come from the identity
// roster and every read stays under the owner's row-level security. Identities created after
// boot have no earlier jobs to resume. Deactivated ones are skipped, as by every other runtime
// sweep that acts for an owner: their jobs stay in the store for a restart after reactivation.
type mediaJobRecovery struct {
	resumer    mediaJobResumer
	identities runtimeIdentityLister
	listed     bool
	pending    []string
}

// newMediaJobRecovery returns the recovery as a sweeper: runServe runs its first pass
// synchronously, before any request can reach a tool that tracks the same owner's jobs, and
// its ticks retry what that pass could not reach. It is nil, the disabled sweeper, without a
// watcher.
func newMediaJobRecovery(watcher *mediagen.Watcher, identities runtimeIdentityLister) *conversations.Sweeper {
	if watcher == nil || identities == nil {
		return nil
	}
	recovery := &mediaJobRecovery{resumer: watcher, identities: identities}
	return conversations.NewSweeper(conversations.SweeperConfig{
		Interval: mediaResumeRetryInterval,
		Sweep:    recovery.sweep,
	})
}

// sweep lists the roster until a listing succeeds, then resumes every owner not yet resumed. A
// failed Resume tracked nothing — it fails reading the owner's recoverable jobs — so retrying it
// cannot supervise a job twice. An owner retried after requests are served can race a tool that
// tracks the same job; the job's delivery claim still gives one delivery, at worst after a
// second wake.
func (r *mediaJobRecovery) sweep(ctx context.Context) {
	if !r.listed {
		owners, err := r.identities.ListIdentities(ctx)
		if err != nil {
			slog.Warn("aura serve: video job recovery could not list identities; retrying",
				"code", mediagen.ErrorCode(err), "err", redact.String(err.Error()))
			return
		}
		for _, owner := range owners {
			if owner.ID != "" && !owner.Deactivated {
				r.pending = append(r.pending, owner.ID)
			}
		}
		r.listed = true
	}
	unresumed := r.pending[:0]
	for _, ownerID := range r.pending {
		if err := r.resumer.Resume(ctx, ownerID); err != nil {
			slog.Warn("aura serve: video job recovery failed for an identity; retrying",
				"owner", ownerID, "code", mediagen.ErrorCode(err), "err", redact.String(err.Error()))
			unresumed = append(unresumed, ownerID)
		}
	}
	r.pending = unresumed
}
