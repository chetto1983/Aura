package main

// serve_studio.go wires the cockpit Studio over the media dependencies the chat tools already
// use: the same catalog, the same two shared generation paths, the same job store and the same
// asset service. Nothing here builds a second provider client, so a clamp or an audit field
// fixed for the agent is fixed for the Studio in the same commit.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/redact"
)

// studioJobs is what the Studio needs of the job store beyond mediagen.JobStore: its own
// history page, and the row a synchronous image generation is already finished in.
type studioJobs interface {
	ListStudio(ctx context.Context, ownerID, beforeID string, kind mediagen.Kind, limit int) ([]mediagen.Job, error)
	InsertImage(ctx context.Context, job mediagen.Job) (mediagen.Job, error)
}

// studioAssetStore is the asset service's Studio half: the generated image it stores, the
// identity's recent images it lists, and the reference upload it finalizes.
type studioAssetStore interface {
	IngestAgentFile(ctx context.Context, req assets.AgentIngestRequest) (assets.Asset, error)
	ListRecentImages(ctx context.Context, identityID string, limit int) ([]assets.Asset, error)
	FinalizeUnprocessed(ctx context.Context, identityID, assetID string, modality assets.Modality) (assets.Asset, error)
}

// studioBackend serves agui.StudioBackend over the live media dependencies. submit, generate
// and track are the shared paths as closures so the wiring, not this type, decides what they
// are — and so a test can prove a refusal reached none of them.
type studioBackend struct {
	catalog     agui.MediaCatalogLister
	settings    mediagen.Settings
	credentials mediagen.MediaCredentials
	submit      func(context.Context, mediagen.VideoSubmission) (mediagen.Job, error)
	generate    func(context.Context, mediagen.ImageGeneration) (mediagen.GeneratedImage, error)
	track       func(mediagen.Job)
	assets      studioAssetStore
	jobs        studioJobs
}

var _ agui.StudioBackend = studioBackend{}

// Models answers the picker: the shared catalog on the live route, and the identity's stored
// default for that kind so the composer opens on the model a chat generation would use.
func (b studioBackend) Models(ctx context.Context, kind mediagen.Kind) (string, []mediagen.Model, error) {
	models, err := b.catalog.List(ctx, kind, false)
	if err != nil {
		return "", nil, err
	}
	defaultModel, err := b.settings.Model(ctx, kind)
	if err != nil {
		return "", nil, err
	}
	return defaultModel, models, nil
}

// listed refuses a model the catalog does not list. The Studio names its model per request, so
// an unlisted one is a stale or forged form, never a model to try on the operator's money. A
// catalog that cannot be read refuses the same way: the request is not sent on a guess.
func (b studioBackend) listed(ctx context.Context, kind mediagen.Kind, model string) (*mediagen.Model, error) {
	models, err := b.catalog.List(ctx, kind, false)
	if err != nil {
		if errors.Is(err, agui.ErrMediaCatalogLocalRoute) {
			return nil, err
		}
		slog.Error("aura serve: the Studio could not read the model catalog; nothing was generated",
			"kind", kind, "err", redact.String(err.Error()))
		return nil, &mediagen.Error{Code: "job_failed", Message: "The model catalog could not be read, so nothing was generated."}
	}
	for _, listed := range models {
		if listed.ID == model {
			return &listed, nil
		}
	}
	return nil, &mediagen.Error{Code: "unsupported", Message: "That model is not one this route offers."}
}

// SubmitVideo submits only a model the catalog lists, on the Studio surface: the job belongs to
// no conversation and to no tool call, and is tracked without an inline waiter because nothing
// is waiting for it — the page reads the row back.
func (b studioBackend) SubmitVideo(ctx context.Context, owner string, req agui.StudioVideoRequest) (mediagen.Job, error) {
	entry, err := b.listed(ctx, mediagen.KindVideo, req.Model)
	if err != nil {
		return mediagen.Job{}, err
	}
	// Every option the Studio's form leaves out is filled with the least expensive one the
	// model declares, because the provider's own default for an absent field is whatever it
	// sells best — on a per-second SKU, the long, high, audible clip.
	input := mediagen.CheapestVideoInput(mediagen.VideoInput{
		Prompt: req.Prompt, Duration: req.Duration, Resolution: req.Resolution, AspectRatio: req.AspectRatio,
		FirstFrameAssetID: req.FirstFrameAssetID, LastFrameAssetID: req.LastFrameAssetID,
		Audio: req.Audio, Seed: req.Seed,
	}, entry)
	job, err := b.submit(ctx, mediagen.VideoSubmission{
		Owner: owner, Surface: mediagen.SurfaceStudio, Model: req.Model, Input: input,
	})
	if err != nil {
		return mediagen.Job{}, err
	}
	b.track(job)
	return job, nil
}

// GenerateImage pays once, stores the image as the identity's own asset outside every
// conversation, and records the generation. A stored image whose record fails is reported as
// such: the asset is there, so nothing is generated again. The recorded origin is the one the
// generator itself paid, carried back on the result: a second credential read could answer a
// different route, and the row would then name an endpoint the image never came from.
func (b studioBackend) GenerateImage(ctx context.Context, owner string, req agui.StudioImageRequest) (mediagen.Job, error) {
	if _, err := b.listed(ctx, mediagen.KindImage, req.Model); err != nil {
		return mediagen.Job{}, err
	}
	generated, err := b.generate(ctx, mediagen.ImageGeneration{
		Owner: owner, Model: req.Model,
		Input: mediagen.ImageInput{Prompt: req.Prompt, AspectRatio: req.AspectRatio, ReferenceAssetIDs: req.ReferenceAssetIDs},
	})
	if err != nil {
		return mediagen.Job{}, err
	}
	extension, err := mediagen.ImageExtension(generated.Result.MIMEType)
	if err != nil {
		return mediagen.Job{}, err
	}
	// The provider returns bytes, not a job id, so the Studio mints the one the asset's source
	// ref and the recorded row share.
	providerID := "image-" + uuid.NewString()
	asset, err := b.assets.IngestAgentFile(ctx, assets.AgentIngestRequest{
		IdentityID: owner, SourceRef: "studio:" + providerID,
		FileName: "generated" + extension, MIMEType: generated.Result.MIMEType,
		Modality: assets.ModalityImage, SizeBytes: int64(len(generated.Result.Bytes)),
		Reader: bytes.NewReader(generated.Result.Bytes),
	})
	if err != nil {
		slog.Error("aura serve: a paid Studio image could not be stored", "owner", owner,
			"provider_job_id", providerID, "err", redact.String(err.Error()))
		return mediagen.Job{}, &mediagen.Error{Code: "job_failed", Message: "The image was generated but could not be saved."}
	}
	request, err := mediagen.ImageRecord(generated)
	if err != nil {
		return mediagen.Job{}, err
	}
	return b.jobs.InsertImage(ctx, mediagen.Job{
		IdentityID: owner, Surface: mediagen.SurfaceStudio, Kind: mediagen.KindImage,
		ProviderJobID: providerID, Model: req.Model, Request: request, AssetID: asset.ID,
		CostUSD: generated.Result.CostUSD,
	})
}

func (b studioBackend) History(ctx context.Context, owner, beforeID string, kind mediagen.Kind, limit int) ([]mediagen.Job, error) {
	return b.jobs.ListStudio(ctx, owner, beforeID, kind, limit)
}

func (b studioBackend) Library(ctx context.Context, owner string, limit int) ([]assets.Asset, error) {
	return b.assets.ListRecentImages(ctx, owner, limit)
}

// FinalizeUpload accepts an uploaded reference as an image and nothing else: a document
// finalized here would become a reference no generation could read.
func (b studioBackend) FinalizeUpload(ctx context.Context, owner, assetID string) (assets.Asset, error) {
	return b.assets.FinalizeUnprocessed(ctx, owner, assetID, assets.ModalityImage)
}

// wireStudio serves the Studio only when every part of it is live: both shared generation
// paths, the settings the picker defaults from, the watcher a submitted clip needs, the job
// store its history reads and the asset service its library reads. Missing one, the routes
// stay unwired and answer 503 rather than half-serving a page that pays.
func wireStudio(server *agui.Server, chat *chatEnv, media *mediaDeps, watcher *mediagen.Watcher) {
	if media == nil || media.settings == nil || watcher == nil || chat.assets == nil ||
		!media.submitter.Configured() || !media.imager.Configured() {
		return
	}
	jobs, ok := media.jobs.(studioJobs)
	if !ok {
		return
	}
	server.SetStudio(studioBackend{
		catalog:     mediaCatalogRoute{catalog: media.catalog, runtime: chat.llmRuntime},
		settings:    media.settings,
		credentials: media.credentials,
		submit:      media.submitter.Submit,
		generate:    media.imager.Generate,
		// A Studio clip has no inline waiter: the page polls its history rather than holding a
		// request open, so the watcher's completion is the only thing waiting for the job.
		track:  func(job mediagen.Job) { watcher.Track(job, false) },
		assets: chat.assets,
		jobs:   jobs,
	})
}
