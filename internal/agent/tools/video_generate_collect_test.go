package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/mediagen"
)

// assertCollectUntouched proves a collect neither reached the provider, nor supervised the job
// (a tracked job is polled or, when finished, woken), nor claimed or staged anything.
func assertCollectUntouched(t *testing.T, f *videoFixture) {
	t.Helper()
	if notices := f.remainingNotices(t); len(notices) != 0 {
		t.Fatalf("a collect handed the job to the watcher: %+v", notices)
	}
	if seen := f.provider.seen(); len(seen) != 0 {
		t.Fatalf("a collect reached the provider: %v", seen)
	}
	if f.jobs.count("ClaimDelivery") != 0 || f.library.opens() != 0 || len(stagedMediaDirs(t, f.runDir)) != 0 {
		t.Fatalf("claims=%d opens=%d staged=%v, want none", f.jobs.count("ClaimDelivery"), f.library.opens(), stagedMediaDirs(t, f.runDir))
	}
}

// After a restart nothing tracks the job, the identity may have lost its credit, the operator
// may have picked another model and moved the base URL: a finished, paid clip is delivered from
// the stored row and the stored asset alone, exactly as it was submitted.
func TestVideoGenerateCollectsAJobFinishedBeforeARestart(t *testing.T) {
	f := newVideoFixture(t)
	job := f.seedJob(t, mediagen.StatusCompleted, videoThread)
	f.credentials.err = &mediagen.Error{Code: "no_credit", Message: "the key has no credit left"}
	f.credentials.baseURL = "https://moved.example/api/v1"
	f.settings.model = "google/veo-3.1"

	res := f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`","prompt":"ignored in collect mode"}`)
	descriptor := artifactMap(t, res)
	if descriptor["asset_id"] != job.AssetID || descriptor["tool_call_id"] != "call-collect" || descriptor["caption"] != videoPrompt ||
		descriptor["mime_type"] != "video/mp4" || descriptor["filename"] != "generated.mp4" {
		t.Fatalf("descriptor = %#v, want the stored asset delivered to the collecting call", descriptor)
	}
	preview := decodeVideoPreview(t, res)
	if preview.AssetID != job.AssetID || preview.Model != mediagen.DefaultVideoModel || preview.CostUSD == nil || *preview.CostUSD != 0.4 ||
		preview.Used.Duration != 5 || preview.Used.Resolution != "768p" || preview.Used.FirstFrameAssetID != "frame-1" ||
		!slices.Equal(preview.Adjustments, []string{"duration 4s is not offered; used 5s"}) || preview.Delivered != mediaDeliveredNote {
		t.Fatalf("preview = %+v, want the submission's model, cost, options, adjustments and delivery note", preview)
	}
	if f.credentials.calls != 0 || f.settings.calls != 0 {
		t.Fatalf("collect read credentials %d and settings %d times, want neither", f.credentials.calls, f.settings.calls)
	}
	if !slices.Equal(f.jobs.deliveryCalls(), []string{"call-collect"}) || f.jobs.job(job.ID).DeliveredAt == nil {
		t.Fatalf("claims = %v, want one by the collecting call", f.jobs.deliveryCalls())
	}
	if notices := f.remainingNotices(t); len(notices) != 0 || len(f.provider.seen()) != 0 {
		t.Fatalf("collect supervised the job (wakes %+v) or reached the provider (%v)", notices, f.provider.seen())
	}
}

func TestVideoGenerateCollectFindsOnlyJobsOfThisConversation(t *testing.T) {
	f := newVideoFixture(t)
	foreign := f.seedJob(t, mediagen.StatusCompleted, videoThread)
	foreign.IdentityID = "owner-2"
	f.jobs.put(foreign)
	elsewhere := f.seedJob(t, mediagen.StatusCompleted, "another-thread")
	for name, jobID := range map[string]string{
		"missing job":          uuid.NewString(),
		"not a job id":         "../../etc/passwd",
		"another identity":     foreign.ID,
		"another conversation": elsewhere.ID,
	} {
		code, message := toolError(t, f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+jobID+`"}`))
		if code != "asset_not_found" || strings.Contains(message, jobID) {
			t.Fatalf("%s -> %q %q, want asset_not_found that does not echo the id", name, code, message)
		}
	}
	assertCollectUntouched(t, f)
}

func TestVideoGenerateCollectReportsJobsNotReadyToDeliver(t *testing.T) {
	delivered := func(job *mediagen.Job) { job.DeliveredAt = new(job.CreatedAt) }
	failure := func(code, message string) func(*mediagen.Job) {
		return func(job *mediagen.Job) { job.Error = &mediagen.Error{Code: code, Message: message} }
	}
	cases := map[string]struct {
		status      mediagen.Status
		mutate      func(*mediagen.Job)
		code, match string
	}{
		"still pending":       {status: mediagen.StatusPending},
		"still running":       {status: mediagen.StatusInProgress},
		"already delivered":   {status: mediagen.StatusCompleted, mutate: delivered, code: "already_delivered", match: "already delivered"},
		"failed with a cause": {status: mediagen.StatusFailed, mutate: failure("too_large", "Media exceeds the configured byte limit."), code: "too_large", match: "byte limit"},
		"failed, no cause":    {status: mediagen.StatusFailed, code: "job_failed", match: "failed"},
		"expired":             {status: mediagen.StatusExpired, mutate: failure("job_expired", "Video generation did not finish in time."), code: "job_expired", match: "in time"},
		"expired, no cause":   {status: mediagen.StatusExpired, code: "job_expired", match: "in time"},
		"cancelled":           {status: mediagen.StatusCancelled, mutate: failure("job_failed", "Video generation failed."), code: "job_failed", match: "cancelled"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			job := f.seedJob(t, tc.status, videoThread)
			if tc.mutate != nil {
				tc.mutate(&job)
				f.jobs.put(job)
			}
			res := f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`)
			if tc.code == "" {
				status := videoStatus(t, res)
				if status.Status != string(tc.status) || status.JobID != job.ID || status.Model != mediagen.DefaultVideoModel ||
					status.CostUSD == nil || *status.CostUSD != 0.4 || status.Used.Duration != 5 || len(status.Adjustments) != 1 {
					t.Fatalf("status = %+v, want the job's own status and submission", status)
				}
			} else if code, message := toolError(t, res); code != tc.code || !strings.Contains(message, tc.match) {
				t.Fatalf("got %q %q, want %q mentioning %q", code, message, tc.code, tc.match)
			}
			assertCollectUntouched(t, f)
		})
	}
}

// Staging comes before the claim: a clip that cannot be staged stays collectible, and the next
// collect delivers it.
func TestVideoGenerateCollectLeavesTheJobCollectibleWhenStagingFails(t *testing.T) {
	f := newVideoFixture(t)
	job := f.seedJob(t, mediagen.StatusCompleted, videoThread)
	blocked := filepath.Join(f.runDir, "tmp")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, message := toolError(t, f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`))
	if code != "job_failed" || message != videoRetryLater {
		t.Fatalf("got %q %q, want job_failed saying the same job_id can be collected again", code, message)
	}
	if f.jobs.count("ClaimDelivery") != 0 || f.jobs.job(job.ID).DeliveredAt != nil {
		t.Fatal("a failed staging still claimed the delivery")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	artifactMap(t, f.execute(t, f.callCtx("call-retry"), `{"job_id":"`+job.ID+`"}`))
	if !slices.Equal(f.jobs.deliveryCalls(), []string{"call-retry"}) {
		t.Fatalf("claims = %v, want the retry's", f.jobs.deliveryCalls())
	}
}

func TestVideoGenerateCollectReportsAClipThatIsGone(t *testing.T) {
	f := newVideoFixture(t)
	job := f.seedJob(t, mediagen.StatusCompleted, videoThread)
	f.library.remove(job.AssetID)
	if code, _ := toolError(t, f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`)); code != "asset_not_found" {
		t.Fatalf("code = %q, want asset_not_found", code)
	}
	if f.jobs.count("ClaimDelivery") != 0 || len(stagedMediaDirs(t, f.runDir)) != 0 {
		t.Fatal("a missing clip was claimed or staged")
	}
}

func TestVideoGenerateCollectDiscardsTheStagedClipWhenTheClaimFails(t *testing.T) {
	for name, tc := range map[string]struct {
		err           error
		code, message string
	}{
		"asset no longer bindable": {&mediagen.Error{Code: "asset_not_found", Message: "The generated video is not available for this job."}, "asset_not_found", "The generated video is not available for this job."},
		"store unavailable":        {errors.New("dial tcp 10.0.0.7:5432: connection refused"), "job_failed", videoRetryLater},
		"job vanished":             {pgx.ErrNoRows, "asset_not_found", "No video job with this job_id exists in this conversation."},
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			job := f.seedJob(t, mediagen.StatusCompleted, videoThread)
			f.jobs.claimErr = tc.err
			if code, message := toolError(t, f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`)); code != tc.code || message != tc.message {
				t.Fatalf("got %q %q, want %q %q", code, message, tc.code, tc.message)
			}
			if dirs := stagedMediaDirs(t, f.runDir); len(dirs) != 0 {
				t.Fatalf("an undelivered clip stayed staged in %v", dirs)
			}
		})
	}
}

// Two collects racing for one clip, as a wake and an operator's own request can: one delivers,
// the other answers already_delivered and leaves nothing staged.
func TestVideoGenerateConcurrentCollectsDeliverOnce(t *testing.T) {
	f := newVideoFixture(t)
	job := f.seedJob(t, mediagen.StatusCompleted, videoThread)
	results := make([]ToolResult, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			res, err := f.tool.Execute(f.callCtx("call-"+string(rune('a'+i))), []byte(`{"job_id":"`+job.ID+`"}`))
			if err != nil {
				t.Error(err)
			}
			results[i] = res
		})
	}
	wg.Wait()
	artifacts, refusals := 0, 0
	for _, res := range results {
		if res.Meta != nil {
			artifacts++
			continue
		}
		if code, _ := toolError(t, res); code == "already_delivered" {
			refusals++
		}
	}
	if artifacts != 1 || refusals != 1 || len(f.jobs.deliveryCalls()) != 1 {
		t.Fatalf("artifacts=%d already_delivered=%d claims=%v, want exactly one delivery", artifacts, refusals, f.jobs.deliveryCalls())
	}
	if dirs := stagedMediaDirs(t, f.runDir); len(dirs) != 1 {
		t.Fatalf("staged %v, want only the delivered clip", dirs)
	}
}

func TestVideoGenerateCollectDeliversNothingForACancelledTurn(t *testing.T) {
	f := newVideoFixture(t)
	job := f.seedJob(t, mediagen.StatusCompleted, videoThread)
	ctx, cancel := context.WithCancel(f.callCtx("call-collect"))
	cancel()
	if code, _ := toolError(t, f.execute(t, ctx, `{"job_id":"`+job.ID+`"}`)); code != "job_failed" {
		t.Fatalf("code = %q, want job_failed for an abandoned collect", code)
	}
	if f.jobs.count("ClaimDelivery") != 0 || f.jobs.job(job.ID).DeliveredAt != nil {
		t.Fatal("an abandoned collect claimed the delivery")
	}
}

// TestVideoInProgressSurvivesAnUnreadableSubmissionRecord: the job was submitted and is running,
// so a record that cannot be read back must not turn into a failure the model might retry.
func TestVideoInProgressSurvivesAnUnreadableSubmissionRecord(t *testing.T) {
	logs := captureLogs(t)
	res := videoInProgressResult(mediagen.Job{ID: "job-7", Model: "m", Request: json.RawMessage(`{`)}, mediagen.StatusInProgress)
	preview := decodeVideoPreview(t, res)
	if preview.Status != string(mediagen.StatusInProgress) || preview.JobID != "job-7" || preview.Message != videoStillRunning {
		t.Fatalf("preview = %+v, want the running job", preview)
	}
	if !strings.Contains(logs.String(), "job-7") {
		t.Fatalf("log %q, want the unreadable record reported", logs.String())
	}
}
