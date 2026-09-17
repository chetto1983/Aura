package mediagen

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/redact"
)

// videoJobRecordTimeout bounds the Insert that makes an accepted submission durable. The Insert
// is detached from the turn: a turn cancelled right after the provider accepted the job must
// still leave the row a watcher and a restart can resume.
const videoJobRecordTimeout = 10 * time.Second

// VideoSubmission is one video to submit: whose it is, which surface asked, and what was asked
// for. A chat submission names the conversation and the tool call that made it; a Studio
// submission belongs to neither.
type VideoSubmission struct {
	Owner          string
	ConversationID string
	ToolCallID     string
	Model          string
	Surface        Surface
	Input          VideoInput
}

// VideoSubmitter is the one path that pays for a video: the video_generate tool and the cockpit
// Studio both submit through it, so a clamp or an audit field fixed here is fixed for both.
type VideoSubmitter struct {
	Credentials MediaCredentials
	Catalog     *Catalog
	Client      *Client
	References  ReferenceReader
	Jobs        JobStore
	// MaxImageBytes bounds each frame and reference image read into the request.
	MaxImageBytes int64
}

// Configured reports whether every dependency is present; a caller refuses before any paid
// request otherwise.
func (s *VideoSubmitter) Configured() bool {
	return s != nil && s.Credentials != nil && s.Catalog != nil && s.Client != nil &&
		s.References != nil && s.Jobs != nil && s.MaxImageBytes > 0
}

// Submit charges exactly once. Credentials, the catalog entry, the clamp, the frames and the
// persisted request are settled before the one POST, which is never repeated; the accepted
// job is then recorded.
func (s *VideoSubmitter) Submit(ctx context.Context, sub VideoSubmission) (Job, error) {
	baseURL, apiKey, err := s.Credentials.For(ctx, sub.Owner)
	if err != nil {
		return Job{}, err
	}
	entry, adjustments, err := s.Catalog.Entry(ctx, baseURL, KindVideo, sub.Model)
	if err != nil {
		return Job{}, err
	}
	input, notes, err := ClampVideo(sub.Input, entry)
	if err != nil {
		return Job{}, err
	}
	adjustments = append(adjustments, notes...)
	req, err := s.request(ctx, sub.Owner, sub.Model, input)
	if err != nil {
		return Job{}, err
	}
	persisted, err := JobRequest(req, JobAudit{
		Origin: baseURL, FirstFrameAssetID: input.FirstFrameAssetID, LastFrameAssetID: input.LastFrameAssetID,
		ReferenceAssetIDs: input.ReferenceAssetIDs, Adjustments: adjustments,
	})
	if err != nil {
		return Job{}, err
	}
	remote, err := s.Client.SubmitVideo(ctx, baseURL, apiKey, req)
	if err != nil {
		return Job{}, err
	}
	return s.record(ctx, sub, persisted, remote)
}

// request reads the kept frames and references and builds the provider body.
func (s *VideoSubmitter) request(ctx context.Context, owner, model string, input VideoInput) (VideoRequest, error) {
	req := VideoRequest{
		Model: model, Prompt: input.Prompt, Duration: input.Duration, Resolution: input.Resolution,
		AspectRatio: input.AspectRatio, GenerateAudio: input.Audio, Seed: input.Seed,
	}
	for _, frame := range []struct{ assetID, frameType string }{
		{input.FirstFrameAssetID, "first_frame"},
		{input.LastFrameAssetID, "last_frame"},
	} {
		if frame.assetID == "" {
			continue
		}
		image, err := LoadReferences(ctx, s.References, owner, []string{frame.assetID}, s.MaxImageBytes)
		if err != nil {
			return VideoRequest{}, err
		}
		req.FrameImages = append(req.FrameImages, FrameReference{Type: image[0].Type, ImageURL: image[0].ImageURL, FrameType: frame.frameType})
	}
	references, err := LoadReferences(ctx, s.References, owner, input.ReferenceAssetIDs, s.MaxImageBytes)
	if err != nil {
		return VideoRequest{}, err
	}
	req.InputReferences = references
	return req, nil
}

// record persists the accepted job. Only pending and in_progress rows exist before the
// watcher has looked, so any answer but pending is stored in_progress. A lost Insert is the
// excluded submission interval: logged for reconciliation, never submitted again.
func (s *VideoSubmitter) record(ctx context.Context, sub VideoSubmission, request json.RawMessage, remote RemoteVideo) (Job, error) {
	status := StatusInProgress
	if remote.Status == StatusPending {
		status = StatusPending
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), videoJobRecordTimeout)
	defer cancel()
	job, err := s.Jobs.Insert(recordCtx, Job{
		IdentityID: sub.Owner, Surface: sub.Surface, Kind: KindVideo, ConversationID: sub.ConversationID,
		ToolCallID: sub.ToolCallID, ProviderJobID: remote.ID, Model: sub.Model, Request: request,
		Status: status, CostUSD: remote.CostUSD,
	})
	if err != nil {
		slog.Error("mediagen: the provider accepted a video job that could not be recorded; it is not submitted again",
			"owner", sub.Owner, "surface", sub.Surface, "provider_job_id", remote.ID, "err", redact.String(err.Error()))
		return Job{}, &Error{Code: "job_failed", Message: "The video was submitted but could not be recorded, so it cannot be delivered. It was not submitted again.", cause: err}
	}
	return job, nil
}
