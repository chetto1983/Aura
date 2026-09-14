package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/redact"
)

// VideoGenerate submits one video on the identity's own OpenRouter key with the
// operator-selected model and waits a short live window for it. A clip that takes longer is
// left to the daemon's watcher, which wakes the conversation, and the model then collects it
// with job_id. Every dependency is injected at serve boot; the static and manifest registries
// hold a zero value whose Execute refuses before any request.
type VideoGenerate struct {
	Credentials mediagen.MediaCredentials
	Settings    mediagen.Settings
	Catalog     *mediagen.Catalog
	Client      *mediagen.Client
	References  mediagen.ReferenceReader
	Jobs        mediagen.JobStore
	Watcher     *mediagen.Watcher
	// VideoAssets reads a finished job's clip back for delivery; nothing is ingested twice.
	VideoAssets mediagen.ReferenceReader
	// MaxImageBytes bounds the first frame and the references, MaxVideoBytes the delivered clip.
	MaxImageBytes int64
	MaxVideoBytes int64
}

// videoGenerateParameters has no root anyOf for "prompt or job_id": Anthropic rejects a tool
// schema with a root composition keyword, and a loaded deferred tool's schema rides every later
// request. Execute enforces the requirement instead, as task and skill do (D-10).
const videoGenerateParameters = `{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "What the video shows, or how the first frame should move. Required to generate a video; leave it out when collecting with job_id."},
    "job_id": {"type": "string", "description": "Collect mode: the job_id an earlier video_generate call returned. Delivers the finished video or reports its status; nothing new is generated. Required instead of prompt when collecting."},
    "duration": {"type": "integer", "minimum": 1, "description": "Length in seconds; the nearest duration the model offers is used."},
    "resolution": {"type": "string", "enum": ["480p", "720p", "768p", "1080p", "1K", "2K", "4K"], "description": "Requested resolution; the nearest one the model offers is used."},
    "aspect_ratio": {"type": "string", "enum": ["16:9", "9:16", "1:1", "4:3", "3:4", "3:2", "2:3", "21:9", "9:21"], "description": "Requested shape; the nearest ratio the model offers is used."},
    "first_frame_asset_id": {"type": "string", "description": "Asset id of an image to animate: the video starts from it."},
    "reference_asset_ids": {"type": "array", "items": {"type": "string"}, "description": "Asset ids of images the video should follow."},
    "audio": {"type": "boolean", "description": "Ask for sound; sent only when the model can generate audio."}
  },
  "additionalProperties": false
}`

const videoGenerateDescription = `Generate one video from a prompt, or animate an image by passing its asset id as first_frame_asset_id. The operator chooses the model.
Submit: {"prompt":"waves breaking at dawn, slow pan","duration":6,"aspect_ratio":"16:9"}.
A clip that finishes quickly is delivered by this call. Otherwise the call returns {"status":"in_progress","job_id":...}: tell the user the video is on its way and end your turn; do not poll and do not submit it again. The runtime notifies this conversation when the job finishes; then call video_generate once with only that job_id to deliver it.
Collect: {"job_id":"<job_id from the result or the notification>"}.
Read adjustments and errors; do not invent a model or a download URL.`

type videoGenerateArgs struct {
	Prompt            string   `json:"prompt"`
	JobID             string   `json:"job_id"`
	Duration          int      `json:"duration"`
	Resolution        string   `json:"resolution"`
	AspectRatio       string   `json:"aspect_ratio"`
	FirstFrameAssetID string   `json:"first_frame_asset_id"`
	ReferenceAssetIDs []string `json:"reference_asset_ids"`
	Audio             *bool    `json:"audio"`
}

// videoJobRecordTimeout bounds the Insert that makes an accepted submission durable. The Insert
// is detached from the turn: a turn cancelled right after the provider accepted the job must
// still leave the row a watcher and a restart can resume.
const videoJobRecordTimeout = 10 * time.Second

func (g *VideoGenerate) Spec() Spec {
	return Spec{
		Name:                "video_generate",
		Summary:             "Generate a video or animate an image; collect a completed video job.",
		Description:         videoGenerateDescription,
		Parameters:          json.RawMessage(videoGenerateParameters),
		Deferred:            true,
		Mutating:            true,
		OperationScope:      OperationScopeAgent,
		OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy:        ReplayToolResult,
	}
}

// Execute collects the job named by job_id, even when a prompt is also given, or submits a new
// video. Every refusal that can be decided locally is decided before the paid request; every
// failure is an {error,message} result, never a Go error.
func (g *VideoGenerate) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args videoGenerateArgs
	if err := decodeStrictArgs(raw, &args); err != nil {
		return errorResult("unsupported", "Pass an object with a prompt to generate a video, or with a job_id to collect one; the operator chooses the model, so no other field is accepted."), nil
	}
	jobID := strings.TrimSpace(args.JobID)
	switch {
	case jobID == "" && strings.TrimSpace(args.Prompt) == "":
		return errorResult("unsupported", "A prompt is required to generate a video; to collect a finished video, pass its job_id instead."), nil
	case jobID == "" && args.Duration < 0:
		return errorResult("unsupported", "duration must be a positive number of seconds."), nil
	case !g.configured():
		return errorResult("unsupported", "Video generation is not available in this session."), nil
	}
	owner, ok := mediaDeliveryOwner(ctx)
	if !ok {
		return errorResult("unsupported", "Video generation needs a signed-in conversation to deliver the video into."), nil
	}
	if jobID != "" {
		return g.collect(ctx, owner, jobID), nil
	}
	return g.submit(ctx, owner, args), nil
}

func (g *VideoGenerate) configured() bool {
	return g.Credentials != nil && g.Settings != nil && g.Catalog != nil && g.Client != nil &&
		g.References != nil && g.Jobs != nil && g.Watcher != nil && g.VideoAssets != nil &&
		g.MaxImageBytes > 0 && g.MaxVideoBytes > 0
}

// submit charges exactly once: credentials, the live model and wait, the clamp, the images and
// the persisted request are all settled before the one POST, which is never repeated.
func (g *VideoGenerate) submit(ctx context.Context, owner string, args videoGenerateArgs) ToolResult {
	baseURL, apiKey, err := g.Credentials.For(ctx, owner)
	if err != nil {
		return mediaErrorResult(err)
	}
	model, err := g.Settings.Model(ctx, mediagen.KindVideo)
	if err != nil {
		return mediaErrorResult(err)
	}
	inlineWait, err := g.Settings.VideoInlineWait(ctx)
	if err != nil {
		return mediaErrorResult(err)
	}
	input, adjustments, err := g.clamp(ctx, baseURL, model, args)
	if err != nil {
		return mediaErrorResult(err)
	}
	req, err := g.request(ctx, owner, model, input)
	if err != nil {
		return mediaErrorResult(err)
	}
	persisted, err := mediagen.JobRequest(req, mediagen.JobAudit{
		Origin: baseURL, FirstFrameAssetID: input.FirstFrameAssetID,
		ReferenceAssetIDs: input.ReferenceAssetIDs, Adjustments: adjustments,
	})
	if err != nil {
		return mediaErrorResult(err)
	}
	remote, err := g.Client.SubmitVideo(ctx, baseURL, apiKey, req)
	if err != nil {
		return mediaErrorResult(err)
	}
	job, err := g.record(ctx, owner, model, persisted, remote)
	if err != nil {
		return errorResult("job_failed", "The video was submitted but could not be recorded, so it cannot be delivered. It was not submitted again.")
	}
	return g.awaitInline(ctx, owner, job, inlineWait)
}

func (g *VideoGenerate) clamp(ctx context.Context, baseURL, model string, args videoGenerateArgs) (mediagen.VideoInput, []string, error) {
	entry, adjustments, err := mediaCatalogEntry(ctx, g.Catalog, baseURL, mediagen.KindVideo, model)
	if err != nil {
		return mediagen.VideoInput{}, nil, err
	}
	input, notes, err := mediagen.ClampVideo(mediagen.VideoInput{
		Prompt: args.Prompt, Duration: args.Duration, Resolution: args.Resolution, AspectRatio: args.AspectRatio,
		FirstFrameAssetID: args.FirstFrameAssetID, ReferenceAssetIDs: args.ReferenceAssetIDs, Audio: args.Audio,
	}, entry)
	if err != nil {
		return mediagen.VideoInput{}, nil, err
	}
	return input, append(adjustments, notes...), nil
}

// request reads the kept first frame and references and builds the provider body.
func (g *VideoGenerate) request(ctx context.Context, owner, model string, input mediagen.VideoInput) (mediagen.VideoRequest, error) {
	req := mediagen.VideoRequest{
		Model: model, Prompt: input.Prompt, Duration: input.Duration, Resolution: input.Resolution,
		AspectRatio: input.AspectRatio, GenerateAudio: input.Audio,
	}
	if input.FirstFrameAssetID != "" {
		frame, err := mediagen.LoadReferences(ctx, g.References, owner, []string{input.FirstFrameAssetID}, g.MaxImageBytes)
		if err != nil {
			return mediagen.VideoRequest{}, err
		}
		req.FrameImages = []mediagen.FrameReference{{Type: frame[0].Type, ImageURL: frame[0].ImageURL, FrameType: "first_frame"}}
	}
	references, err := mediagen.LoadReferences(ctx, g.References, owner, input.ReferenceAssetIDs, g.MaxImageBytes)
	if err != nil {
		return mediagen.VideoRequest{}, err
	}
	req.InputReferences = references
	return req, nil
}

// record persists the accepted job. Only pending and in_progress rows exist before the watcher
// has looked, and a provider saying completed has not produced an Aura asset yet, so any answer
// but pending is stored in_progress and the watcher's first poll decides it. A lost Insert is
// the excluded submission interval: logged for reconciliation, never submitted again.
func (g *VideoGenerate) record(ctx context.Context, owner, model string, request json.RawMessage, remote mediagen.RemoteVideo) (mediagen.Job, error) {
	tc, _ := toolCallCtx(ctx)
	status := mediagen.StatusInProgress
	if remote.Status == mediagen.StatusPending {
		status = mediagen.StatusPending
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), videoJobRecordTimeout)
	defer cancel()
	job, err := g.Jobs.Insert(recordCtx, mediagen.Job{
		IdentityID: owner, ConversationID: tc.sessionID, ToolCallID: tc.toolCallID, ProviderJobID: remote.ID,
		Model: model, Request: request, Status: status, CostUSD: remote.CostUSD,
	})
	if err != nil {
		slog.Error("video_generate: the provider accepted a video job that could not be recorded; it is not submitted again",
			"owner", owner, "provider_job_id", remote.ID, "err", redact.String(err.Error()))
	}
	return job, err
}

// awaitInline waits the live window for the persisted job. Wait's window is its duration, not a
// deadline on ctx: an expired context can never claim, while a window that ends only detaches.
// A clip finished inside the window but not delivered goes back to the wake path.
func (g *VideoGenerate) awaitInline(ctx context.Context, owner string, job mediagen.Job, window time.Duration) ToolResult {
	waiter := g.Watcher.Track(job, true)
	finished, owns := waiter.Wait(ctx, window)
	if !owns {
		waiter.Release()
		return videoInProgressResult(job, mediagen.StatusInProgress)
	}
	if finished.Status != mediagen.StatusCompleted {
		return videoOutcomeResult(finished)
	}
	result, settled := g.handOver(ctx, owner, finished)
	if !settled {
		waiter.Release()
		return videoInProgressResult(job, mediagen.StatusInProgress)
	}
	return result
}
