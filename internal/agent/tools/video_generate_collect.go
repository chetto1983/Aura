package tools

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/mediagen"
)

// videoGenerateUsed is the submission's options as the provider received them.
type videoGenerateUsed struct {
	Duration          int      `json:"duration,omitempty"`
	Resolution        string   `json:"resolution,omitempty"`
	AspectRatio       string   `json:"aspect_ratio,omitempty"`
	FirstFrameAssetID string   `json:"first_frame_asset_id,omitempty"`
	ReferenceAssetIDs []string `json:"reference_asset_ids,omitempty"`
	Audio             *bool    `json:"audio,omitempty"`
}

// videoJobStatus answers for a job this call does not deliver. CostUSD stays null until the
// provider reports a cost: unknown is not free.
type videoJobStatus struct {
	Status      mediagen.Status   `json:"status"`
	JobID       string            `json:"job_id"`
	Message     string            `json:"message"`
	Model       string            `json:"model"`
	CostUSD     *float64          `json:"cost_usd"`
	Used        videoGenerateUsed `json:"used"`
	Adjustments []string          `json:"adjustments"`
}

const videoStillRunning = "The video is still being generated. It will be announced in this conversation when it is ready; then call video_generate once with this job_id to deliver it. Do not submit it again."

// collect delivers or reports an existing job from the store alone. It never tracks the job:
// the watcher that submitted or resumed it already supervises it, and a job no watcher tracks is
// finished. No credential, live model or provider request is needed to hand over a paid clip.
func (g *VideoGenerate) collect(ctx context.Context, owner, jobID string) ToolResult {
	tc, _ := toolCallCtx(ctx)
	job, err := g.Jobs.Get(ctx, owner, jobID)
	switch {
	case errors.Is(err, pgx.ErrNoRows), err == nil && job.ConversationID != tc.sessionID:
		return videoJobNotFound()
	case err != nil:
		return mediaErrorResult(err)
	case job.DeliveredAt == nil && (job.Status == mediagen.StatusPending || job.Status == mediagen.StatusInProgress):
		return videoInProgressResult(job, job.Status)
	case job.DeliveredAt != nil || job.Status != mediagen.StatusCompleted:
		return videoOutcomeResult(job)
	}
	result, _ := g.handOver(ctx, owner, job)
	return result
}

// handOver delivers a completed job's clip to this call. The clip is staged before the claim,
// so a staging failure leaves the job collectible; the claim then binds the clip to this call,
// and the artifact names the same asset every delivery path uses. settled is false when no
// claim was made and the job may still be delivered; a lost claim is settled elsewhere.
func (g *VideoGenerate) handOver(ctx context.Context, owner string, job mediagen.Job) (ToolResult, bool) {
	prompt, used, adjustments, err := videoSubmission(job)
	if err != nil {
		return mediaErrorResult(err), false
	}
	path, filename, mimeType, size, err := stageExistingVideo(ctx, g.VideoAssets, owner, job.AssetID, g.MaxVideoBytes)
	if err != nil {
		return mediaErrorResult(err), false
	}
	tc, _ := toolCallCtx(ctx)
	_, claimed, err := g.Jobs.ClaimDelivery(ctx, owner, job.ID, tc.sessionID, tc.toolCallID)
	if err != nil || !claimed {
		discardStagedMedia(path)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return videoJobNotFound(), false
	case err != nil:
		return mediaErrorResult(err), false
	case !claimed:
		return videoAlreadyDelivered(), true
	}
	preview := mediaResult{
		AssetID: job.AssetID, MIMEType: mimeType, Model: job.Model, CostUSD: job.CostUSD,
		Used: used, Adjustments: adjustments,
	}
	return mediaArtifactResult(ctx, path, filename, mimeType, job.AssetID, prompt, size, preview), true
}

// videoSubmission reads what the job recorded when it was submitted, so every answer about a
// job, after a restart included, repeats the prompt, options and adjustments of the first.
func videoSubmission(job mediagen.Job) (prompt string, used videoGenerateUsed, adjustments []string, err error) {
	req, audit, err := job.Submission()
	if err != nil {
		return "", videoGenerateUsed{}, nil, err
	}
	used = videoGenerateUsed{
		Duration: req.Duration, Resolution: req.Resolution, AspectRatio: req.AspectRatio,
		FirstFrameAssetID: audit.FirstFrameAssetID, ReferenceAssetIDs: audit.ReferenceAssetIDs, Audio: req.GenerateAudio,
	}
	adjustments = audit.Adjustments
	if adjustments == nil {
		adjustments = []string{}
	}
	return req.Prompt, used, adjustments, nil
}

func videoInProgressResult(job mediagen.Job, status mediagen.Status) ToolResult {
	_, used, adjustments, err := videoSubmission(job)
	if err != nil {
		return mediaErrorResult(err)
	}
	result, _ := mediaPreviewResult(videoJobStatus{
		Status: status, JobID: job.ID, Message: videoStillRunning, Model: job.Model, CostUSD: job.CostUSD,
		Used: used, Adjustments: adjustments,
	})
	return result
}

// videoOutcomeResult reports a job that will not be delivered by this call: one delivered
// already, or one that ended without a clip, with its stored cause when it has one.
func videoOutcomeResult(job mediagen.Job) ToolResult {
	switch {
	case job.DeliveredAt != nil:
		return videoAlreadyDelivered()
	case job.Status == mediagen.StatusCancelled:
		return errorResult("job_failed", "The video generation job was cancelled.")
	case job.Status == mediagen.StatusExpired:
		return errorResult("job_expired", storedMessage(job, "Video generation did not finish in time."))
	case job.Error != nil && job.Error.Code != "":
		return errorResult(job.Error.Code, storedMessage(job, "Video generation failed."))
	}
	return errorResult("job_failed", "Video generation failed.")
}

func storedMessage(job mediagen.Job, fallback string) string {
	if job.Error != nil && job.Error.Message != "" {
		return job.Error.Message
	}
	return fallback
}

func videoJobNotFound() ToolResult {
	return errorResult("asset_not_found", "No video job with this job_id exists in this conversation.")
}

func videoAlreadyDelivered() ToolResult {
	return errorResult("already_delivered", "This video was already delivered in this conversation; it is not delivered again.")
}
