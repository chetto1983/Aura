package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mediagen"
)

// VideoGenerate submits one video on the identity's own OpenRouter key with the
// operator-selected model and waits a short live window for it. A clip that takes longer is
// left to the daemon's watcher, which wakes the conversation, and the model then collects it
// with job_id. Every dependency is injected at serve boot; the static and manifest registries
// hold a zero value whose Execute refuses before any request.
type VideoGenerate struct {
	// Submitter is the shared path that pays for a clip; the cockpit Studio submits through the
	// same one, so a clamp or an audit field is never decided twice.
	Submitter *mediagen.VideoSubmitter
	Settings  mediagen.Settings
	Jobs      mediagen.JobStore
	Watcher   *mediagen.Watcher
	// VideoAssets reads a finished job's clip back for delivery; nothing is ingested twice.
	VideoAssets mediagen.ReferenceReader
	// MaxVideoBytes bounds the delivered clip.
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
    "last_frame_asset_id": {"type": "string", "description": "Asset id of an image the video should end on; needs first_frame_asset_id and a model that accepts an end frame."},
    "reference_asset_ids": {"type": "array", "items": {"type": "string"}, "description": "Asset ids of images the video should follow."},
    "audio": {"type": "boolean", "description": "Ask for sound; sent only when the model can generate audio."}
  },
  "additionalProperties": false
}`

const videoGenerateDescription = `Generate one video from a prompt, or animate an image by passing its asset id as first_frame_asset_id. The operator chooses the model.
Submit: {"prompt":"waves breaking at dawn, slow pan","duration":6,"aspect_ratio":"16:9"}.
A clip that finishes quickly is delivered by this call and shown to the user; do not send it again. Otherwise the call returns {"status":"in_progress","job_id":...}: tell the user the video is on its way and end your turn; do not poll and do not submit it again. The runtime notifies this conversation when the job finishes; then call video_generate once with only that job_id to deliver it.
Collect: {"job_id":"<job_id from the result or the notification>"}.
Read adjustments and errors; do not invent a model or a download URL.`

type videoGenerateArgs struct {
	Prompt            string   `json:"prompt"`
	JobID             string   `json:"job_id"`
	Duration          int      `json:"duration"`
	Resolution        string   `json:"resolution"`
	AspectRatio       string   `json:"aspect_ratio"`
	FirstFrameAssetID string   `json:"first_frame_asset_id"`
	LastFrameAssetID  string   `json:"last_frame_asset_id"`
	ReferenceAssetIDs []string `json:"reference_asset_ids"`
	Audio             *bool    `json:"audio"`
}

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
	case jobID == "" && args.LastFrameAssetID != "" && args.FirstFrameAssetID == "":
		return errorResult("unsupported", "last_frame_asset_id needs first_frame_asset_id: a video can only end on an image when it also starts from one. Nothing was generated."), nil
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
	return g.Submitter.Configured() && g.Settings != nil && g.Jobs != nil && g.Watcher != nil &&
		g.VideoAssets != nil && g.MaxVideoBytes > 0
}

// submit resolves the live model and wait, then hands the submission to the shared path, which
// charges exactly once. The job it returns is already recorded, so only the inline wait is left.
func (g *VideoGenerate) submit(ctx context.Context, owner string, args videoGenerateArgs) ToolResult {
	model, err := g.Settings.Model(ctx, mediagen.KindVideo)
	if err != nil {
		return mediaErrorResult(err)
	}
	inlineWait, err := g.Settings.VideoInlineWait(ctx)
	if err != nil {
		return mediaErrorResult(err)
	}
	tc, _ := toolCallCtx(ctx)
	job, err := g.Submitter.Submit(ctx, mediagen.VideoSubmission{
		Owner: owner, ConversationID: tc.sessionID, ToolCallID: tc.toolCallID,
		Model: model, Surface: mediagen.SurfaceChat,
		Input: mediagen.VideoInput{
			Prompt: args.Prompt, Duration: args.Duration, Resolution: args.Resolution,
			AspectRatio: args.AspectRatio, FirstFrameAssetID: args.FirstFrameAssetID,
			LastFrameAssetID: args.LastFrameAssetID, ReferenceAssetIDs: args.ReferenceAssetIDs,
			Audio: args.Audio,
		},
	})
	if err != nil {
		return mediaErrorResult(err)
	}
	return g.awaitInline(ctx, owner, job, inlineWait)
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
