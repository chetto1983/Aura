// Unit tier (no build tag): the persisted-request encoding, row mapping and the guards that
// fire before any query. Row-level security, CHECKs and claims need a live Postgres and live
// in store_integration_test.go (db_integration).
package mediagen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/redact"
)

const onePixelPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aXWQAAAAASUVORK5CYII="

func referencedVideoRequest() VideoRequest {
	audio := false
	return VideoRequest{
		Model: "minimax/hailuo-3-max", Prompt: "the sea at dawn", Duration: 6,
		Resolution: "768p", AspectRatio: "16:9",
		FrameImages: []FrameReference{
			{Type: "image_url", ImageURL: ImageURL{URL: onePixelPNG}, FrameType: "first_frame"},
		},
		InputReferences: []ImageReference{{Type: "image_url", ImageURL: ImageURL{URL: onePixelPNG}}},
		GenerateAudio:   &audio,
	}
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return object
}

func TestJobRequestRemovesReferenceDataAndRecordsAudit(t *testing.T) {
	firstFrame, reference := uuid.NewString(), uuid.NewString()
	raw, err := JobRequest(referencedVideoRequest(), JobAudit{
		Origin:            "https://OpenRouter.ai/api/v1/",
		FirstFrameAssetID: firstFrame,
		ReferenceAssetIDs: []string{reference},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("base64")) {
		t.Fatalf("persisted request carries image data: %s", raw)
	}
	got := decodeObject(t, raw)
	for _, dropped := range []string{"frame_images", "input_references"} {
		if _, found := got[dropped]; found {
			t.Errorf("persisted request keeps %s", dropped)
		}
	}
	want := map[string]any{
		"model": "minimax/hailuo-3-max", "prompt": "the sea at dawn", "duration": float64(6),
		"resolution": "768p", "aspect_ratio": "16:9", "generate_audio": false,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %#v, want %#v", key, got[key], value)
		}
	}
	audit, ok := got["_aura"].(map[string]any)
	if !ok {
		t.Fatalf("_aura = %#v, want the audit object", got["_aura"])
	}
	if audit["origin"] != "https://openrouter.ai/api/v1" || audit["first_frame_asset_id"] != firstFrame {
		t.Errorf("_aura = %#v", audit)
	}
	if refs, _ := audit["reference_asset_ids"].([]any); len(refs) != 1 || refs[0] != reference {
		t.Errorf("reference_asset_ids = %#v, want [%s]", audit["reference_asset_ids"], reference)
	}
}

func TestJobRequestLeavesTheSubmitBodyUnchanged(t *testing.T) {
	req := referencedVideoRequest()
	before, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := JobRequest(req, JobAudit{Origin: "https://openrouter.ai/api/v1"}); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("JobRequest changed the submit body:\n%s\n%s", before, after)
	}
	if bytes.Contains(after, []byte("_aura")) || !bytes.Contains(after, []byte("base64")) {
		t.Fatalf("the SDK body must keep its references and never carry _aura: %s", after)
	}
}

func TestJobRequestRedactsCredentialsAndDataURLsInThePrompt(t *testing.T) {
	cases := map[string]struct{ prompt, want string }{
		"inline data url": {
			"animate\n" + onePixelPNG + " gently",
			"animate\n" + redact.Placeholder + " gently",
		},
		"wrapped data url": {"(" + onePixelPNG + ")", redact.Placeholder},
		"url userinfo":     {"see https://ada:hunter2@example.com/cat.png", "see " + redact.Placeholder},
		"signed query":     {"like https://cdn.example.com/cat.png?X-Amz-Signature=abc.", "like " + redact.Placeholder},
		"fragment token":   {"https://example.com/cb#access_token=abc", redact.Placeholder},
		"bearer":           {"use Bearer abcdefghijklmnop please", "use Bearer " + redact.Placeholder + " please"},
		"plain prose": {
			"a cat at https://example.com/cat.png, data: none\n\tsecond line",
			"a cat at https://example.com/cat.png, data: none\n\tsecond line",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := VideoRequest{Model: "m", Prompt: tc.prompt}
			raw, err := JobRequest(req, JobAudit{Origin: "https://openrouter.ai/api/v1"})
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeObject(t, raw)["prompt"]; got != tc.want {
				t.Fatalf("prompt = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubmissionOriginDropsUserDataAndRejectsNonHTTP(t *testing.T) {
	got, err := SubmissionOrigin(" http://ada:sk-or-v1-secret@LOCALHOST:8080/api/v1/?key=sk-or-v1-secret#x ")
	if err != nil || got != "http://localhost:8080/api/v1" {
		t.Fatalf("SubmissionOrigin = %q, %v", got, err)
	}
	for _, bad := range []string{"", "openrouter.ai/api/v1", "ftp://openrouter.ai", "https://", "https://%zz?key=sk-or-v1-secret"} {
		_, err := SubmissionOrigin(bad)
		if err == nil {
			t.Errorf("SubmissionOrigin(%q) accepted a non-http(s) origin", bad)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("error echoes its input: %v", err)
		}
	}
	if _, err := JobRequest(VideoRequest{Model: "m", Prompt: "p"}, JobAudit{Origin: "openrouter.ai"}); err == nil {
		t.Fatal("JobRequest recorded an origin a resume could not compare")
	}
}

func TestJobAuditReadsTheRecordedSubmission(t *testing.T) {
	audit := JobAudit{Origin: "https://openrouter.ai/api/v1", FirstFrameAssetID: uuid.NewString()}
	raw, err := JobRequest(VideoRequest{Model: "m", Prompt: "p"}, audit)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Job{Request: raw}.Audit()
	if err != nil || !reflect.DeepEqual(got, audit) {
		t.Fatalf("Audit = %#v, %v; want %#v", got, err, audit)
	}
	for name, request := range map[string]string{
		"no audit object": `{"model":"m","prompt":"p"}`,
		"no origin":       `{"model":"m","_aura":{"origin":""}}`,
		"malformed":       `{"model":`,
	} {
		if _, err := (Job{ID: "job-1", Request: json.RawMessage(request)}).Audit(); err == nil {
			t.Errorf("%s: Audit accepted a request a resume cannot check", name)
		}
	}
}

// A collect after a restart renders the clip from the stored row alone, so the submitted
// options and the plain-language adjustments must read back exactly, without the image data.
func TestJobSubmissionReadsBackTheClampedRequestAndItsAdjustments(t *testing.T) {
	req := referencedVideoRequest()
	audit := JobAudit{
		Origin: "https://openrouter.ai/api/v1", FirstFrameAssetID: uuid.NewString(),
		Adjustments: []string{"duration 4s is not offered; used 6s"},
	}
	raw, err := JobRequest(req, audit)
	if err != nil {
		t.Fatal(err)
	}
	gotReq, gotAudit, err := Job{Request: raw}.Submission()
	wantReq := req
	wantReq.FrameImages, wantReq.InputReferences = nil, nil
	if err != nil || !reflect.DeepEqual(gotReq, wantReq) || !reflect.DeepEqual(gotAudit, audit) {
		t.Fatalf("Submission = %#v, %#v, %v; want %#v, %#v", gotReq, gotAudit, err, wantReq, audit)
	}
	if _, _, err := (Job{ID: "job-1", Request: json.RawMessage(`{"model":"m","prompt":"p"}`)}).Submission(); err == nil {
		t.Fatal("Submission accepted a request with no recorded origin")
	}
}

func TestJobFromRowMapsNullableColumns(t *testing.T) {
	created := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	id, owner, asset := uuid.New(), uuid.New(), uuid.New()
	row := sqlc.AuraMediaJob{
		ID: pgtype.UUID{Bytes: id, Valid: true}, IdentityID: pgtype.UUID{Bytes: owner, Valid: true},
		ConversationID: "thread-a", ToolCallID: "call-submit", ProviderJobID: "vid_1",
		Model: "minimax/hailuo-3-max", Request: []byte(`{"model":"minimax/hailuo-3-max"}`),
		Status:    string(StatusPending),
		CreatedAt: pgtype.Timestamptz{Time: created, Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: created, Valid: true},
	}
	pending, err := jobFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if pending.ID != id.String() || pending.IdentityID != owner.String() || pending.ProviderJobID != "vid_1" ||
		pending.Status != StatusPending || !pending.CreatedAt.Equal(created) {
		t.Fatalf("pending = %#v", pending)
	}
	if pending.Error != nil || pending.AssetID != "" || pending.CostUSD != nil ||
		pending.CompletedAt != nil || pending.DeliveredAt != nil {
		t.Fatalf("NULL columns must map to absent values: %#v", pending)
	}

	finished := created.Add(time.Minute)
	row.Status = string(StatusCompleted)
	row.AssetID = pgtype.UUID{Bytes: asset, Valid: true}
	row.CostUsd = pgtype.Numeric{Int: big.NewInt(0), Valid: true}
	row.Error = []byte(`{"code":"job_failed","message":"Video generation failed."}`)
	row.CompletedAt = pgtype.Timestamptz{Time: finished, Valid: true}
	row.DeliveredAt = pgtype.Timestamptz{Time: finished, Valid: true}
	done, err := jobFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if done.AssetID != asset.String() || done.CostUSD == nil || *done.CostUSD != 0 {
		t.Fatalf("an explicit zero cost must stay a value: %#v", done)
	}
	if done.Error == nil || *done.Error != (Error{Code: "job_failed", Message: "Video generation failed."}) {
		t.Fatalf("Error = %#v", done.Error)
	}
	if done.CompletedAt == nil || !done.CompletedAt.Equal(finished) || done.DeliveredAt == nil {
		t.Fatalf("terminal times = %v %v", done.CompletedAt, done.DeliveredAt)
	}
}

func TestJobFromRowRefusesMalformedFailure(t *testing.T) {
	if _, err := jobFromRow(sqlc.AuraMediaJob{Error: []byte(`{"code":`)}); err == nil {
		t.Fatal("a failure that does not decode must not read as no failure")
	}
}

func TestFailureJSONKeepsNullAndEncodesCodeAndMessage(t *testing.T) {
	absent, err := failureJSON(nil)
	if err != nil || absent != nil {
		t.Fatalf("failureJSON(nil) = %q, %v; want SQL NULL", absent, err)
	}
	encoded, err := failureJSON(&Error{Code: "job_expired", Message: "Video generation job expired."})
	if err != nil || string(encoded) != `{"code":"job_expired","message":"Video generation job expired."}` {
		t.Fatalf("failureJSON = %s, %v", encoded, err)
	}
}

// unreachablePool parses but never dials: pgxpool.New is lazy, so every guard that fires
// before a query runs without Postgres, and a missing guard fails with a dial error instead.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/nowhere?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestStoreRefusesMalformedInputBeforeAnyQuery(t *testing.T) {
	store := NewStore(unreachablePool(t))
	ctx := context.Background()
	owner, job := uuid.NewString(), uuid.NewString()
	valid := Job{
		IdentityID: owner, Surface: SurfaceChat, Kind: KindVideo,
		ConversationID: "thread-a", ToolCallID: "call-1", ProviderJobID: "vid_1",
		Model:   "minimax/hailuo-3-max",
		Request: json.RawMessage(`{"model":"minimax/hailuo-3-max","_aura":{"origin":"https://openrouter.ai/api/v1"}}`),
		Status:  StatusInProgress,
	}
	if err := validateNewJob(valid); err != nil {
		t.Fatalf("the baseline job must be valid, or every refusal below proves nothing: %v", err)
	}
	invalidJobs := map[string]func(*Job){
		"owner not a uuid":       func(j *Job) { j.IdentityID = "local" },
		"terminal status":        func(j *Job) { j.Status = StatusFailed },
		"completed status":       func(j *Job) { j.Status = StatusCompleted },
		"no conversation":        func(j *Job) { j.ConversationID = "" },
		"no tool call":           func(j *Job) { j.ToolCallID = "" },
		"no model":               func(j *Job) { j.Model = "" },
		"unsafe provider job id": func(j *Job) { j.ProviderJobID = "../videos" },
		"request not json":       func(j *Job) { j.Request = json.RawMessage(`{"model":`) },
		"request without _aura":  func(j *Job) { j.Request = json.RawMessage(`{"model":"minimax/hailuo-3-max"}`) },
		"request with no origin": func(j *Job) { j.Request = json.RawMessage(`{"model":"m","_aura":{"origin":""}}`) },
	}
	for name, mutate := range invalidJobs {
		job := valid
		mutate(&job)
		if _, err := store.Insert(ctx, job); err == nil || isDialError(err) {
			t.Errorf("Insert with %s: err = %v, want a refusal before any query", name, err)
		}
	}

	refusals := map[string]error{}
	_, refusals["Get owner"] = store.Get(ctx, "local", job)
	_, refusals["Recoverable owner"] = store.Recoverable(ctx, "local")
	_, refusals["Progress owner"] = store.Progress(ctx, "local", job, StatusInProgress, nil, nil)
	_, refusals["Progress completed"] = store.Progress(ctx, owner, job, StatusCompleted, nil, nil)
	nan := func() *float64 { v := 0.0; v /= v; return &v }()
	_, refusals["Progress NaN cost"] = store.Progress(ctx, owner, job, StatusInProgress, nan, nil)
	_, refusals["Complete owner"] = store.Complete(ctx, "local", job, uuid.NewString(), nil)
	_, refusals["Complete asset"] = store.Complete(ctx, owner, job, "asset-1", nil)
	_, refusals["Complete NaN cost"] = store.Complete(ctx, owner, job, uuid.NewString(), nan)
	_, _, refusals["Claim owner"] = store.ClaimDelivery(ctx, "local", job, "thread-a", "call-2")
	_, _, refusals["Claim conversation"] = store.ClaimDelivery(ctx, owner, job, "", "call-2")
	_, _, refusals["Claim call"] = store.ClaimDelivery(ctx, owner, job, "thread-a", "")
	for name, err := range refusals {
		if err == nil || isDialError(err) || errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("%s: err = %v, want a refusal before any query", name, err)
		}
	}

	missing := map[string]error{}
	_, missing["Get"] = store.Get(ctx, owner, "job-1")
	_, missing["Progress"] = store.Progress(ctx, owner, "job-1", StatusInProgress, nil, nil)
	_, missing["Complete"] = store.Complete(ctx, owner, "job-1", uuid.NewString(), nil)
	_, claimed, claimErr := store.ClaimDelivery(ctx, owner, "job-1", "thread-a", "call-2")
	missing["ClaimDelivery"] = claimErr
	for name, err := range missing {
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("%s with a malformed job id: err = %v, want pgx.ErrNoRows (no such job)", name, err)
		}
	}
	if claimed {
		t.Error("a malformed job id won a delivery claim")
	}
}

func isDialError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "connect"))
}

func TestValidateNewJobSurfaceScope(t *testing.T) {
	request, err := JobRequest(VideoRequest{Model: "m", Prompt: "p"}, JobAudit{Origin: "https://openrouter.ai/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	base := Job{Model: "m", Request: request, Status: StatusPending, ProviderJobID: "gen-1", Kind: KindVideo}
	cases := []struct {
		name    string
		mutate  func(*Job)
		wantErr bool
	}{
		{"chat with conversation and call", func(j *Job) { j.Surface, j.ConversationID, j.ToolCallID = SurfaceChat, "c", "t" }, false},
		{"chat without conversation", func(j *Job) { j.Surface, j.ToolCallID = SurfaceChat, "t" }, true},
		{"studio without conversation", func(j *Job) { j.Surface = SurfaceStudio }, false},
		{"studio with conversation", func(j *Job) { j.Surface, j.ConversationID = SurfaceStudio, "c" }, true},
		{"unknown surface", func(j *Job) { j.Surface, j.ConversationID, j.ToolCallID = "api", "c", "t" }, true},
		{"image is not submitted as a job", func(j *Job) { j.Surface, j.Kind = SurfaceStudio, KindImage }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := base
			tc.mutate(&job)
			if err := validateNewJob(job); (err != nil) != tc.wantErr {
				t.Fatalf("validateNewJob err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
