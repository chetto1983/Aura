package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/mediagen"
)

// The brief's schema required prompt or job_id through a root anyOf. Anthropic rejects a tool
// whose input_schema has anyOf, oneOf or allOf at the top level, and a promoted deferred tool's
// schema rides every later request, so one loaded video_generate would fail every following turn
// on a Claude route. The requirement is checked in Execute instead, as task and skill do (D-10).
func TestVideoGenerateSchemaAllowsCollectWithoutPrompt(t *testing.T) {
	spec := (&VideoGenerate{}).Spec()
	if spec.Name != "video_generate" || !spec.Deferred || !spec.Mutating {
		t.Fatal("missing deferred mutation metadata")
	}
	if spec.OperationScope != OperationScopeAgent || spec.OperationNormalizer != OperationNormalizerCanonical ||
		spec.ReplayPolicy != ReplayToolResult || spec.Destructive || spec.Multiplexed {
		t.Fatalf("operation metadata = %+v, want the standard agent mutation, not multiplexed", spec)
	}
	if spec.Summary != "Generate a video or animate an image; collect a completed video job." {
		t.Fatalf("Summary = %q", spec.Summary)
	}
	for _, phrase := range []string{`"job_id"`, "in_progress", "notif", "once"} {
		if !strings.Contains(spec.Description, phrase) {
			t.Errorf("Description lacks %q: it must show submit and collect and the asynchronous contract", phrase)
		}
	}
	var schema map[string]any
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	for _, keyword := range []string{"anyOf", "oneOf", "allOf", "enum", "required"} {
		if _, exists := schema[keyword]; exists {
			t.Fatalf("root %q: providers reject root composition, and prompt is optional in collect mode", keyword)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	if _, exists := properties["model"]; exists {
		t.Fatal("model cannot be an agent argument")
	}
	want := []string{"aspect_ratio", "audio", "duration", "first_frame_asset_id", "job_id", "prompt", "reference_asset_ids", "resolution"}
	if got := slices.Sorted(maps.Keys(properties)); !slices.Equal(got, want) || schema["additionalProperties"] != false {
		t.Fatalf("properties = %v additionalProperties = %v, want %v and false", got, schema["additionalProperties"], want)
	}
	enums := map[string][]string{
		"resolution":   {"480p", "720p", "768p", "1080p", "1K", "2K", "4K"},
		"aspect_ratio": {"16:9", "9:16", "1:1", "4:3", "3:4", "3:2", "2:3", "21:9", "9:21"},
	}
	for name, values := range enums {
		var got []string
		for _, v := range properties[name].(map[string]any)["enum"].([]any) {
			got = append(got, v.(string))
		}
		if !slices.Equal(got, values) {
			t.Errorf("%s enum = %v, want %v", name, got, values)
		}
	}
	if properties["duration"].(map[string]any)["minimum"] != float64(1) {
		t.Error("duration must declare minimum 1")
	}
	for _, args := range []string{`{"prompt":"waves"}`, `{"job_id":"0b8f1c2e-8f7a-4a51-9d0f-3c3f9d1a2b4c"}`} {
		if _, err := OperationFingerprint(spec, json.RawMessage(args)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVideoGenerateDeliversAnEarlyCompletionInline(t *testing.T) {
	f := newVideoFixture(t)
	res := f.execute(t, f.callCtx("call-video"), `{"prompt":"`+videoPrompt+`","duration":6}`)

	if f.provider.count("POST /videos") != 1 {
		t.Fatalf("requests = %v, want exactly one submit", f.provider.seen())
	}
	if events := f.events.snapshot(); len(events) < 2 || events[0] != "insert" || events[1] != "poll" {
		t.Fatalf("events = %v, want the job row persisted before the watcher's first poll", events)
	}
	job := f.jobs.inserted[0]
	assetID := f.library.assetOf(job.ID)
	descriptor := artifactMap(t, res)
	path, _ := descriptor["path"].(string)
	want := map[string]any{
		"path": path, "filename": "generated.mp4", "mime_type": "video/mp4", "asset_id": assetID,
		"caption": videoPrompt, "tool_call_id": "call-video", "size_bytes": int64(len(generatedClip)),
	}
	if assetID == "" || len(descriptor) != len(want) {
		t.Fatalf("descriptor = %#v, want exactly %#v", descriptor, want)
	}
	for key, value := range want {
		if descriptor[key] != value {
			t.Fatalf("descriptor[%q] = %#v, want %#v", key, descriptor[key], value)
		}
	}
	if staged, err := os.ReadFile(path); err != nil || !bytes.Equal(staged, generatedClip) {
		t.Fatalf("staged %q = %d bytes (%v), want the ingested clip", path, len(staged), err)
	}
	preview := decodeVideoPreview(t, res)
	if preview.AssetID != assetID || preview.MIMEType != "video/mp4" || preview.Model != mediagen.DefaultVideoModel ||
		preview.CostUSD == nil || *preview.CostUSD != 0.4 || preview.Used.Duration != 6 ||
		preview.Adjustments == nil || len(preview.Adjustments) != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	if calls := f.jobs.deliveryCalls(); !slices.Equal(calls, []string{"call-video"}) || f.jobs.job(job.ID).DeliveredAt == nil {
		t.Fatalf("delivery claims = %v, want one by the submitting call", calls)
	}
	if notices := f.remainingNotices(t); len(notices) != 0 {
		t.Fatalf("an inline delivery also woke the conversation: %+v", notices)
	}
	if job.IdentityID != videoOwner || job.ConversationID != videoThread || job.ToolCallID != "call-video" ||
		job.ProviderJobID != "vid_1" || job.Status != mediagen.StatusPending {
		t.Fatalf("persisted job = %+v", job)
	}
}

// With a wait the provider cannot meet, the call detaches, the watcher wakes the conversation
// once, a fresh collect delivers the same clip, and a second collect delivers nothing.
func TestVideoGenerateDetachesThenDeliversOnceThroughTheWake(t *testing.T) {
	for name, wait := range map[string]time.Duration{"zero wait": 0, "short wait": 25 * time.Millisecond} {
		t.Run(name, func(t *testing.T) {
			gate := make(chan struct{})
			f := newVideoFixture(t, func(p *fakeVideoProvider) { p.pollGate = gate })
			f.settings.wait = wait
			status := videoStatus(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"`+videoPrompt+`"}`))
			job := f.jobs.inserted[0]
			if status.Status != "in_progress" || status.JobID != job.ID || status.Model != mediagen.DefaultVideoModel ||
				!strings.Contains(status.Message, "announced") || status.Adjustments == nil {
				t.Fatalf("status = %+v, want the static in_progress result for job %s", status, job.ID)
			}
			if f.jobs.count("ClaimDelivery") != 0 {
				t.Fatal("a detached call claimed the delivery")
			}

			close(gate)
			if notice := f.awaitNotice(t); notice != (mediagen.Completion{
				IdentityID: videoOwner, ConversationID: videoThread, JobID: job.ID, Status: mediagen.StatusCompleted,
			}) {
				t.Fatalf("wake = %+v", notice)
			}
			collected := f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`)
			if descriptor := artifactMap(t, collected); descriptor["asset_id"] != f.library.assetOf(job.ID) ||
				descriptor["tool_call_id"] != "call-collect" {
				t.Fatalf("collected descriptor = %#v, want the ingested asset on the collecting call", descriptor)
			}
			requests := f.provider.seen()
			again := f.execute(t, f.callCtx("call-again"), `{"job_id":"`+job.ID+`"}`)
			if code, _ := toolError(t, again); code != "already_delivered" {
				t.Fatalf("second collect = %q, want already_delivered", code)
			}
			if !slices.Equal(f.provider.seen(), requests) || f.provider.count("POST /videos") != 1 {
				t.Fatalf("collects reached the provider: %v", f.provider.seen())
			}
			if notices := f.remainingNotices(t); len(notices) != 0 || !slices.Equal(f.jobs.deliveryCalls(), []string{"call-collect"}) {
				t.Fatalf("extra wakes %+v, claims %v; want one wake and one claim", notices, f.jobs.deliveryCalls())
			}
		})
	}
}

func TestVideoGenerateClampsLoadsTheFrameAndPersistsTheSubmission(t *testing.T) {
	gate := make(chan struct{})
	f := newVideoFixture(t, func(p *fakeVideoProvider) { p.pollGate = gate })
	f.settings.wait = 0
	args := `{"prompt":"make it move https://example.com/cb?token=sk-or-v1-secret","duration":4,"resolution":"1080p",` +
		`"aspect_ratio":"21:9","first_frame_asset_id":"frame-1","reference_asset_ids":["ref-1"],"audio":true}`
	status := videoStatus(t, f.execute(t, f.callCtx("call-video"), args))

	wantNotes := []string{
		"duration 4s is not offered; used 5s",
		"resolution 1080p is not offered; used 768p",
		"audio true is not supported by this model; omitted",
	}
	if !slices.Equal(status.Adjustments, wantNotes) {
		t.Fatalf("adjustments = %q, want %q", status.Adjustments, wantNotes)
	}
	used := status.Used
	if used.Duration != 5 || used.Resolution != "768p" || used.AspectRatio != "21:9" || used.FirstFrameAssetID != "frame-1" ||
		!slices.Equal(used.ReferenceAssetIDs, []string{"ref-1"}) || used.Audio != nil {
		t.Fatalf("used = %+v, want the clamped request", used)
	}
	body, auth := f.provider.lastSubmit()
	frames, _ := body["frame_images"].([]any)
	refs, _ := body["input_references"].([]any)
	if auth != "Bearer identity-key" || body["model"] != mediagen.DefaultVideoModel || body["duration"] != float64(5) ||
		body["resolution"] != "768p" || len(frames) != 1 || len(refs) != 1 || body["generate_audio"] != nil {
		t.Fatalf("submit = %#v (auth %q)", body, auth)
	}
	frame := frames[0].(map[string]any)
	frameURL, _ := frame["image_url"].(map[string]any)["url"].(string)
	if frame["frame_type"] != "first_frame" || !strings.HasPrefix(frameURL, "data:image/png;base64,") {
		t.Fatalf("frame = %#v, want the owned image as a first_frame data URL", frame)
	}

	job := f.jobs.inserted[0]
	if bytes.Contains(job.Request, []byte("base64")) || bytes.Contains(job.Request, []byte("sk-or-v1-secret")) {
		t.Fatalf("persisted request %s carries image data or a credential", job.Request)
	}
	req, audit, err := job.Submission()
	wantOrigin, _ := mediagen.SubmissionOrigin(f.provider.server.URL)
	if err != nil || audit.Origin != wantOrigin || audit.FirstFrameAssetID != "frame-1" ||
		!slices.Equal(audit.ReferenceAssetIDs, []string{"ref-1"}) || !slices.Equal(audit.Adjustments, wantNotes) || req.Duration != 5 {
		t.Fatalf("persisted submission = %+v %+v (%v)", req, audit, err)
	}
	if !slices.Equal(f.references.opened, []string{"frame-1", "ref-1"}) {
		t.Fatalf("opened = %v, want the frame then the reference", f.references.opened)
	}
	close(gate)
}

func TestVideoGenerateProceedsUncheckedWhenTheVideoCatalogIsUnavailable(t *testing.T) {
	gate := make(chan struct{})
	f := newVideoFixture(t, func(p *fakeVideoProvider) { p.pollGate, p.catalogStatus = gate, 503 })
	f.settings.wait = 0
	status := videoStatus(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"waves","duration":4,"resolution":"1080p"}`))
	if len(status.Adjustments) != 1 || !strings.Contains(status.Adjustments[0], "not checked") {
		t.Fatalf("adjustments = %q, want the single unchecked-options note", status.Adjustments)
	}
	if body, _ := f.provider.lastSubmit(); body["duration"] != float64(4) || body["resolution"] != "1080p" {
		t.Fatalf("submit = %#v, want the unclamped request", body)
	}
	close(gate)
}

// Only pending and in_progress rows can be inserted, and a provider saying completed has not
// produced an Aura asset yet: every other answer is stored in_progress for the watcher's poll.
func TestVideoGenerateStoresTheSubmitStatusTheWatcherCanResume(t *testing.T) {
	for remote, want := range map[string]mediagen.Status{
		"pending": mediagen.StatusPending, "in_progress": mediagen.StatusInProgress,
		"completed": mediagen.StatusInProgress, "failed": mediagen.StatusInProgress,
	} {
		t.Run(remote, func(t *testing.T) {
			gate := make(chan struct{})
			f := newVideoFixture(t, func(p *fakeVideoProvider) {
				p.pollGate, p.submitBody = gate, `{"id":"vid_1","status":"`+remote+`","usage":{"cost":0.25}}`
			})
			f.settings.wait = 0
			status := videoStatus(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"waves"}`))
			job := f.jobs.inserted[0]
			if job.Status != want || job.CostUSD == nil || *job.CostUSD != 0.25 || status.Status != "in_progress" ||
				status.CostUSD == nil || *status.CostUSD != 0.25 {
				t.Fatalf("stored %q with cost %v, answered %+v; want %q and the reported cost", job.Status, job.CostUSD, status, want)
			}
			close(gate)
		})
	}
}

// A turn cancelled right after the provider accepted the job must still leave a durable row: the
// watcher then finishes the job and wakes the conversation instead of the lost call.
func TestVideoGenerateRecordsTheJobWhenTheTurnIsCancelledAtHandoff(t *testing.T) {
	f := newVideoFixture(t)
	ctx, cancel := context.WithCancel(f.callCtx("call-video"))
	defer cancel()
	f.jobs.beforeInsert = cancel
	status := videoStatus(t, f.execute(t, ctx, `{"prompt":"waves"}`))

	if f.jobs.insertCtxErr != nil || f.jobs.insertDeadline.IsZero() || time.Until(f.jobs.insertDeadline) > time.Minute {
		t.Fatalf("Insert ran with err %v and deadline %v, want a live context of its own with a bounded deadline",
			f.jobs.insertCtxErr, f.jobs.insertDeadline)
	}
	job := f.jobs.inserted[0]
	if status.Status != "in_progress" || status.JobID != job.ID {
		t.Fatalf("status = %+v", status)
	}
	if notice := f.awaitNotice(t); notice.JobID != job.ID || notice.Status != mediagen.StatusCompleted {
		t.Fatalf("wake = %+v", notice)
	}
	if f.jobs.count("ClaimDelivery") != 0 {
		t.Fatal("a cancelled turn claimed the delivery")
	}
	descriptor := artifactMap(t, f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`))
	if descriptor["asset_id"] != f.library.assetOf(job.ID) {
		t.Fatalf("collected %#v, want the watcher's asset", descriptor)
	}
	if notices := f.remainingNotices(t); len(notices) != 0 {
		t.Fatalf("extra wakes %+v", notices)
	}
}

// A clip that finished inside the wait but could not be staged is not lost and not failed: the
// claim goes back to the wake path, and a collect delivers it once staging works again.
func TestVideoGenerateHandsAFailedInlineDeliveryToTheWake(t *testing.T) {
	f := newVideoFixture(t)
	blocked := filepath.Join(f.runDir, "tmp")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := videoStatus(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"waves"}`))
	job := f.jobs.inserted[0]
	if status.Status != "in_progress" || f.jobs.count("ClaimDelivery") != 0 {
		t.Fatalf("status = %+v after %d claims, want in_progress and no claim", status, f.jobs.count("ClaimDelivery"))
	}
	if notice := f.awaitNotice(t); notice.JobID != job.ID {
		t.Fatalf("wake = %+v", notice)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	artifactMap(t, f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`))
	if notices := f.remainingNotices(t); len(notices) != 0 || f.provider.count("POST /videos") != 1 {
		t.Fatalf("extra wakes %+v, requests %v", notices, f.provider.seen())
	}
}

func TestVideoGenerateReportsATerminalFailureInline(t *testing.T) {
	f := newVideoFixture(t, func(p *fakeVideoProvider) {
		p.submitBody = `{"id":"vid_1","status":"failed","error":"the model refused this prompt"}`
		p.pollBody = p.submitBody
	})
	res := f.execute(t, f.callCtx("call-video"), `{"prompt":"waves"}`)
	if code, message := toolError(t, res); code != "job_failed" || message != "the model refused this prompt" {
		t.Fatalf("got %q %q, want job_failed with the provider's stored message", code, message)
	}
	if notices := f.remainingNotices(t); len(notices) != 0 || f.jobs.count("ClaimDelivery") != 0 {
		t.Fatalf("wakes %+v, claims %d; a failure reported inline is acknowledged", notices, f.jobs.count("ClaimDelivery"))
	}
}

// A clip that finished inside the wait can still fail its hand-over at the claim. A claim that
// never committed goes back to the wake path, once, and a collect delivers the same asset. A
// claim that committed but lost its answer, or that a rival already won, is delivered: nobody is
// woken, the call answers in_progress or already_delivered, and a collect answers
// already_delivered.
func TestVideoGenerateInlineHandOverWhenTheClaimFails(t *testing.T) {
	for name, tc := range map[string]struct {
		arrange       func(f *videoFixture)
		inline        string
		wakes         int
		collectedCode string
	}{
		"claim refused before it commits": {
			arrange: func(f *videoFixture) { f.jobs.claimErr = errors.New("connection reset") },
			inline:  "in_progress", wakes: 1,
		},
		"claim committed, answer lost": {
			arrange: func(f *videoFixture) { f.jobs.lostAnswer = errors.New("connection reset after commit") },
			inline:  "in_progress", collectedCode: "already_delivered",
		},
		"claim lost to a rival": {
			arrange: func(f *videoFixture) {
				f.jobs.beforeClaim = func(jobID string) {
					f.jobs.beforeClaim = nil
					if _, won, err := f.jobs.ClaimDelivery(context.Background(), videoOwner, jobID, videoThread, "call-rival"); !won || err != nil {
						t.Errorf("rival claim = %v, %v", won, err)
					}
				}
			},
			inline: "already_delivered", collectedCode: "already_delivered",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			tc.arrange(f)
			res := f.execute(t, f.callCtx("call-video"), `{"prompt":"waves"}`)
			job := f.jobs.inserted[0]
			if tc.inline == "in_progress" {
				if status := videoStatus(t, res); status.Status != "in_progress" || status.JobID != job.ID {
					t.Fatalf("inline status = %+v, want in_progress for job %s", status, job.ID)
				}
			} else if code, _ := toolError(t, res); code != tc.inline {
				t.Fatalf("inline code = %q, want %q", code, tc.inline)
			}
			if tc.wakes == 1 {
				if notice := f.awaitNotice(t); notice.JobID != job.ID || notice.Status != mediagen.StatusCompleted {
					t.Fatalf("wake = %+v", notice)
				}
			}
			if notices := f.remainingNotices(t); len(notices) != 0 {
				t.Fatalf("wakes beyond %d: %+v", tc.wakes, notices)
			}
			if dirs := stagedMediaDirs(t, f.runDir); len(dirs) != 0 {
				t.Fatalf("an undelivered inline clip stayed staged in %v", dirs)
			}
			f.jobs.claimErr, f.jobs.lostAnswer = nil, nil
			collected := f.execute(t, f.callCtx("call-collect"), `{"job_id":"`+job.ID+`"}`)
			if tc.collectedCode != "" {
				if code, _ := toolError(t, collected); code != tc.collectedCode {
					t.Fatalf("collect code = %q, want %q", code, tc.collectedCode)
				}
				return
			}
			if descriptor := artifactMap(t, collected); descriptor["asset_id"] != f.library.assetOf(job.ID) {
				t.Fatalf("collected %#v, want the watcher's asset %s", descriptor, f.library.assetOf(job.ID))
			}
		})
	}
}
