package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
)

const studioIdentityID = "00000000-0000-0000-0000-0000000000a1"

type fakeStudioBackend struct {
	defaultModel string
	models       []mediagen.Model
	modelsKind   mediagen.Kind
	modelsErr    error

	videoOwner string
	videoReq   StudioVideoRequest
	videoJob   mediagen.Job
	videoErr   error

	imageOwner string
	imageReq   StudioImageRequest
	imageJob   mediagen.Job
	imageErr   error

	historyOwner  string
	historyBefore string
	historyKind   mediagen.Kind
	historyLimit  int
	historyJobs   []mediagen.Job
	historyErr    error

	libraryOwner  string
	libraryLimit  int
	libraryAssets []assets.Asset
	libraryErr    error

	finalizeOwner string
	finalizeAsset string
	finalizeOut   assets.Asset
	finalizeErr   error
}

func (f *fakeStudioBackend) Models(_ context.Context, kind mediagen.Kind) (string, []mediagen.Model, error) {
	f.modelsKind = kind
	return f.defaultModel, f.models, f.modelsErr
}

func (f *fakeStudioBackend) SubmitVideo(_ context.Context, owner string, req StudioVideoRequest) (mediagen.Job, error) {
	f.videoOwner, f.videoReq = owner, req
	return f.videoJob, f.videoErr
}

func (f *fakeStudioBackend) GenerateImage(_ context.Context, owner string, req StudioImageRequest) (mediagen.Job, error) {
	f.imageOwner, f.imageReq = owner, req
	return f.imageJob, f.imageErr
}

func (f *fakeStudioBackend) History(_ context.Context, owner, beforeID string, kind mediagen.Kind, limit int) ([]mediagen.Job, error) {
	f.historyOwner, f.historyBefore, f.historyKind, f.historyLimit = owner, beforeID, kind, limit
	return f.historyJobs, f.historyErr
}

func (f *fakeStudioBackend) Library(_ context.Context, owner string, limit int) ([]assets.Asset, error) {
	f.libraryOwner, f.libraryLimit = owner, limit
	return f.libraryAssets, f.libraryErr
}

func (f *fakeStudioBackend) FinalizeUpload(_ context.Context, owner, assetID string) (assets.Asset, error) {
	f.finalizeOwner, f.finalizeAsset = owner, assetID
	return f.finalizeOut, f.finalizeErr
}

func studioServer(t *testing.T, backend StudioBackend) *Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	if backend != nil {
		s.SetStudio(backend)
	}
	return s
}

func serveStudio(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = withPrincipal(req, studioIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

// studioVideoJobFixture is a persisted Studio video row built through the same JobRequest the
// submitter writes, so the record DTO is read back over the real encoding rather than a
// hand-written body that could drift from it.
func studioVideoJobFixture(t *testing.T) mediagen.Job {
	t.Helper()
	audio, seed := true, 7
	request, err := mediagen.JobRequest(
		mediagen.VideoRequest{
			Model: "google/veo-3.1-lite", Prompt: "a cat in a hat", Duration: 8, Resolution: "720p",
			AspectRatio: "16:9", GenerateAudio: &audio, Seed: &seed,
		},
		mediagen.JobAudit{
			Origin: "https://openrouter.ai/api/v1", FirstFrameAssetID: "asset-first",
			LastFrameAssetID: "asset-last", Adjustments: []string{"duration clamped to 8s"},
		},
	)
	if err != nil {
		t.Fatalf("JobRequest() error = %v", err)
	}
	cost := 0.24
	return mediagen.Job{
		ID: "job-1", IdentityID: studioIdentityID, Surface: mediagen.SurfaceStudio,
		Kind: mediagen.KindVideo, ProviderJobID: "remote-1", Model: "google/veo-3.1-lite",
		Request: request, Status: mediagen.StatusPending, CostUSD: &cost,
		CreatedAt: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC),
	}
}

type studioRecordWire struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Used   struct {
		Duration          int    `json:"duration"`
		Resolution        string `json:"resolution"`
		AspectRatio       string `json:"aspect_ratio"`
		Audio             *bool  `json:"audio"`
		Seed              *int   `json:"seed"`
		FirstFrameAssetID string `json:"first_frame_asset_id"`
		LastFrameAssetID  string `json:"last_frame_asset_id"`
	} `json:"used"`
	Adjustments []string `json:"adjustments"`
	CostUSD     *float64 `json:"cost_usd"`
	AssetID     string   `json:"asset_id"`
	CreatedAt   string   `json:"created_at"`
}

type studioModelsWire struct {
	Default string `json:"default"`
	Models  []struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		Durations    []int    `json:"durations"`
		Resolutions  []string `json:"resolutions"`
		AspectRatios []string `json:"aspect_ratios"`
		FrameImages  []string `json:"frame_images"`
		Audio        bool     `json:"audio"`
		Seed         bool     `json:"seed"`
		Prices       []struct {
			Resolution   string  `json:"resolution"`
			Audio        bool    `json:"audio"`
			USDPerSecond float64 `json:"usd_per_second"`
		} `json:"prices"`
		ReferenceMax *int     `json:"reference_max"`
		ImageMinUSD  *float64 `json:"image_min_usd"`
		ImageMaxUSD  *float64 `json:"image_max_usd"`
	} `json:"models"`
}

func decodeStudio[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestStudioModelsCarryNamesAndPrices(t *testing.T) {
	referenceMax := 4
	backend := &fakeStudioBackend{
		defaultModel: "google/veo-3.1-lite",
		models: []mediagen.Model{{
			ID: "google/veo-3.1-lite", Kind: mediagen.KindVideo, Name: "Google: Veo 3.1 Lite",
			Description: "a video model", Durations: []int{4, 8}, Resolutions: []string{"720p", "1080p"},
			AspectRatios: []string{"16:9", "9:16"}, FrameImages: []string{"first_frame", "last_frame"},
			GenerateAudio: true, Seed: true,
			PricingSKUs: map[string]string{
				"duration_seconds_with_audio":         "0.08",
				"duration_seconds_without_audio":      "0.05",
				"duration_seconds_with_audio_720p":    "0.05",
				"duration_seconds_without_audio_720p": "0.03",
			},
		}},
	}
	s := studioServer(t, backend)

	rec := serveStudio(t, s, http.MethodGet, "/api/studio/models?kind=video", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if backend.modelsKind != mediagen.KindVideo {
		t.Fatalf("backend kind = %q, want video", backend.modelsKind)
	}
	out := decodeStudio[studioModelsWire](t, rec)
	if out.Default != "google/veo-3.1-lite" || len(out.Models) != 1 {
		t.Fatalf("models response = %#v", out)
	}
	row := out.Models[0]
	if row.Name != "Google: Veo 3.1 Lite" || row.Description != "a video model" || !row.Seed || !row.Audio {
		t.Fatalf("video row = %#v", row)
	}
	if len(row.Durations) != 2 || len(row.AspectRatios) != 2 || len(row.FrameImages) != 2 {
		t.Fatalf("video capabilities = %#v", row)
	}
	// Four rows: each declared resolution against each audio choice the model allows. A matrix
	// built from one of the two axes alone, or from the plain rate, has a different length.
	if len(row.Prices) != 4 {
		t.Fatalf("price rows = %#v, want 4", row.Prices)
	}
	want := map[string]float64{"720p/false": 0.03, "720p/true": 0.05, "1080p/false": 0.05, "1080p/true": 0.08}
	for _, price := range row.Prices {
		key := fmt.Sprintf("%s/%t", price.Resolution, price.Audio)
		expected, declared := want[key]
		if !declared {
			t.Fatalf("unexpected price row %q = %v", key, price.USDPerSecond)
		}
		if price.USDPerSecond != expected {
			t.Fatalf("price %q = %v, want %v", key, price.USDPerSecond, expected)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing price rows: %v", want)
	}

	backend.models = []mediagen.Model{{
		ID: "google/gemini-2.5-flash-image", Kind: mediagen.KindImage, Name: "Nano Banana",
		AspectRatios: []string{"1:1", "16:9"},
		Parameters:   map[string]mediagen.Parameter{"input_references": {Max: &referenceMax}},
		ImagePricing: []mediagen.PriceLine{
			{Billable: "output_image", Unit: "image", CostUSD: 0.02},
			{Billable: "output_image", Unit: "image", CostUSD: 0.06},
		},
	}}
	backend.defaultModel = "google/gemini-2.5-flash-image"

	rec = serveStudio(t, s, http.MethodGet, "/api/studio/models?kind=image", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("image status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if backend.modelsKind != mediagen.KindImage {
		t.Fatalf("backend kind = %q, want image", backend.modelsKind)
	}
	out = decodeStudio[studioModelsWire](t, rec)
	if len(out.Models) != 1 {
		t.Fatalf("image models = %#v", out)
	}
	image := out.Models[0]
	if len(image.AspectRatios) != 2 || image.ReferenceMax == nil || *image.ReferenceMax != 4 {
		t.Fatalf("image row = %#v", image)
	}
	if image.ImageMinUSD == nil || *image.ImageMinUSD != 0.02 || image.ImageMaxUSD == nil || *image.ImageMaxUSD != 0.06 {
		t.Fatalf("image price = %#v", image)
	}
	if len(image.Prices) != 0 {
		t.Fatalf("image row carries a per-second price matrix: %#v", image.Prices)
	}

	for _, target := range []string{"/api/studio/models", "/api/studio/models?kind=audio"} {
		rec := serveStudio(t, s, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400", target, rec.Code)
		}
	}
}

func TestStudioVideoCreateAnswersTheRecord(t *testing.T) {
	backend := &fakeStudioBackend{videoJob: studioVideoJobFixture(t)}
	s := studioServer(t, backend)

	body := `{"model":"google/veo-3.1-lite","prompt":"a cat in a hat","duration":8,"resolution":"720p",` +
		`"aspect_ratio":"16:9","audio":true,"seed":7,"first_frame_asset_id":"asset-first",` +
		`"last_frame_asset_id":"asset-last"}`
	rec := serveStudio(t, s, http.MethodPost, "/api/studio/videos", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	if backend.videoOwner != studioIdentityID {
		t.Fatalf("submitted owner = %q, want the authenticated principal", backend.videoOwner)
	}
	got := backend.videoReq
	if got.Model != "google/veo-3.1-lite" || got.Prompt != "a cat in a hat" || got.Duration != 8 ||
		got.Resolution != "720p" || got.AspectRatio != "16:9" {
		t.Fatalf("submitted request = %#v", got)
	}
	if got.Audio == nil || !*got.Audio || got.Seed == nil || *got.Seed != 7 {
		t.Fatalf("submitted audio/seed = %#v, %#v", got.Audio, got.Seed)
	}
	if got.FirstFrameAssetID != "asset-first" || got.LastFrameAssetID != "asset-last" {
		t.Fatalf("submitted frames = %q, %q", got.FirstFrameAssetID, got.LastFrameAssetID)
	}

	out := decodeStudio[studioRecordWire](t, rec)
	if out.ID != "job-1" || out.Kind != "video" || out.Status != "pending" || out.Model != "google/veo-3.1-lite" {
		t.Fatalf("record = %#v", out)
	}
	if out.Prompt != "a cat in a hat" {
		t.Fatalf("record prompt = %q, want the persisted request's prompt", out.Prompt)
	}
	if out.Used.Duration != 8 || out.Used.Resolution != "720p" || out.Used.AspectRatio != "16:9" {
		t.Fatalf("record used = %#v", out.Used)
	}
	if out.Used.Audio == nil || !*out.Used.Audio || out.Used.Seed == nil || *out.Used.Seed != 7 {
		t.Fatalf("record used audio/seed = %#v, %#v", out.Used.Audio, out.Used.Seed)
	}
	if out.Used.FirstFrameAssetID != "asset-first" || out.Used.LastFrameAssetID != "asset-last" {
		t.Fatalf("record used frames = %#v", out.Used)
	}
	if len(out.Adjustments) != 1 || out.Adjustments[0] != "duration clamped to 8s" {
		t.Fatalf("record adjustments = %#v", out.Adjustments)
	}
	if out.CostUSD == nil || *out.CostUSD != 0.24 {
		t.Fatalf("record cost = %#v", out.CostUSD)
	}
	if !strings.HasPrefix(out.CreatedAt, "2026-09-17T10:00:00") {
		t.Fatalf("record created_at = %q", out.CreatedAt)
	}
}

func TestStudioImageCreateAnswersTheRecord(t *testing.T) {
	request, err := mediagen.JobRequest(
		mediagen.VideoRequest{Prompt: "a hat on a cat", AspectRatio: "1:1"},
		mediagen.JobAudit{Origin: "https://openrouter.ai/api/v1", ReferenceAssetIDs: []string{"ref-1"}},
	)
	if err != nil {
		t.Fatalf("JobRequest() error = %v", err)
	}
	completed := time.Date(2026, 9, 17, 11, 0, 0, 0, time.UTC)
	backend := &fakeStudioBackend{imageJob: mediagen.Job{
		ID: "job-2", IdentityID: studioIdentityID, Surface: mediagen.SurfaceStudio,
		Kind: mediagen.KindImage, Model: "google/gemini-2.5-flash-image", Request: request,
		Status: mediagen.StatusCompleted, AssetID: "asset-9", CreatedAt: completed, CompletedAt: &completed,
	}}
	s := studioServer(t, backend)

	body := `{"model":"google/gemini-2.5-flash-image","prompt":"a hat on a cat","aspect_ratio":"1:1",` +
		`"reference_asset_ids":["ref-1"]}`
	rec := serveStudio(t, s, http.MethodPost, "/api/studio/images", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	if backend.imageOwner != studioIdentityID {
		t.Fatalf("generated owner = %q, want the authenticated principal", backend.imageOwner)
	}
	if backend.imageReq.AspectRatio != "1:1" || len(backend.imageReq.ReferenceAssetIDs) != 1 ||
		backend.imageReq.ReferenceAssetIDs[0] != "ref-1" {
		t.Fatalf("generated request = %#v", backend.imageReq)
	}
	out := decodeStudio[studioRecordWire](t, rec)
	if out.Kind != "image" || out.Status != "completed" || out.AssetID != "asset-9" {
		t.Fatalf("record = %#v", out)
	}
	if out.Prompt != "a hat on a cat" || out.Used.AspectRatio != "1:1" {
		t.Fatalf("record read nothing back from the persisted request: %#v", out)
	}
}

func TestStudioRejectsUnknownFields(t *testing.T) {
	for _, tc := range []struct{ target, body string }{
		{"/api/studio/videos", `{"model":"m","prompt":"p","negative_prompt":"none"}`},
		{"/api/studio/images", `{"model":"m","prompt":"p","steps":30}`},
	} {
		backend := &fakeStudioBackend{}
		rec := serveStudio(t, studioServer(t, backend), http.MethodPost, tc.target, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST %s status = %d (%s), want 400", tc.target, rec.Code, rec.Body.String())
		}
		if backend.videoOwner != "" || backend.imageOwner != "" {
			t.Fatalf("POST %s reached the backend despite an unknown field", tc.target)
		}
	}
}

func TestStudioHistoryPassesCursorKindAndLimit(t *testing.T) {
	backend := &fakeStudioBackend{historyJobs: []mediagen.Job{studioVideoJobFixture(t)}}
	s := studioServer(t, backend)

	rec := serveStudio(t, s, http.MethodGet, "/api/studio/history?before=j1&kind=image&limit=5", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if backend.historyOwner != studioIdentityID || backend.historyBefore != "j1" ||
		backend.historyKind != mediagen.KindImage || backend.historyLimit != 5 {
		t.Fatalf("history call = %q, %q, %q, %d", backend.historyOwner, backend.historyBefore, backend.historyKind, backend.historyLimit)
	}
	out := decodeStudio[struct {
		Records []studioRecordWire `json:"records"`
	}](t, rec)
	if len(out.Records) != 1 || out.Records[0].ID != "job-1" || out.Records[0].Prompt != "a cat in a hat" {
		t.Fatalf("history records = %#v", out.Records)
	}

	if rec := serveStudio(t, s, http.MethodGet, "/api/studio/history", ""); rec.Code != http.StatusOK {
		t.Fatalf("bare history status = %d, want 200", rec.Code)
	}
	if backend.historyLimit != 24 {
		t.Fatalf("default limit = %d, want 24", backend.historyLimit)
	}
	if backend.historyKind != "" {
		t.Fatalf("bare history kind = %q, want no filter", backend.historyKind)
	}

	if rec := serveStudio(t, s, http.MethodGet, "/api/studio/history?limit=500", ""); rec.Code != http.StatusOK {
		t.Fatalf("over-limit history status = %d, want 200", rec.Code)
	}
	if backend.historyLimit != mediagen.StudioPageMax {
		t.Fatalf("clamped limit = %d, want %d", backend.historyLimit, mediagen.StudioPageMax)
	}

	backend.historyLimit = 0
	if rec := serveStudio(t, s, http.MethodGet, "/api/studio/history?limit=x", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric limit status = %d, want 400", rec.Code)
	}
	if backend.historyLimit != 0 {
		t.Fatalf("a non-numeric limit still reached the backend (limit = %d)", backend.historyLimit)
	}

	if rec := serveStudio(t, s, http.MethodGet, "/api/studio/history?kind=audio", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown history kind status = %d, want 400", rec.Code)
	}
}

func TestStudioLibraryAndUploadFinalize(t *testing.T) {
	backend := &fakeStudioBackend{libraryAssets: []assets.Asset{{
		ID: "asset-1", IdentityID: studioIdentityID, Modality: assets.ModalityImage,
		FileName: "cat.png", MIMEType: "image/png", SizeBytes: 1024,
		ObjectBucket: "aura-assets", ObjectKey: "identities/alice/secret-object-path.png",
		CreatedAt: time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC),
	}}}
	s := studioServer(t, backend)

	rec := serveStudio(t, s, http.MethodGet, "/api/studio/library?limit=6", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if backend.libraryOwner != studioIdentityID || backend.libraryLimit != 6 {
		t.Fatalf("library call = %q, %d", backend.libraryOwner, backend.libraryLimit)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"object_key", "object_bucket", "secret-object-path", "aura-assets"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("library body exposed %q: %s", leaked, body)
		}
	}
	out := decodeStudio[struct {
		Assets []struct {
			ID       string `json:"id"`
			FileName string `json:"file_name"`
			MIMEType string `json:"mime_type"`
		} `json:"assets"`
	}](t, rec)
	if len(out.Assets) != 1 || out.Assets[0].ID != "asset-1" || out.Assets[0].FileName != "cat.png" ||
		out.Assets[0].MIMEType != "image/png" {
		t.Fatalf("library assets = %#v", out.Assets)
	}

	backend.finalizeErr = assets.ErrWrongModality
	rec = serveStudio(t, s, http.MethodPost, "/api/studio/uploads/asset-2/finalize", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("document finalize status = %d (%s), want 422", rec.Code, rec.Body.String())
	}
	if backend.finalizeOwner != studioIdentityID || backend.finalizeAsset != "asset-2" {
		t.Fatalf("finalize call = %q, %q", backend.finalizeOwner, backend.finalizeAsset)
	}

	backend.finalizeErr = nil
	backend.finalizeOut = assets.Asset{ID: "asset-3", Modality: assets.ModalityImage, FileName: "dog.png", ObjectKey: "identities/alice/dog.png"}
	rec = serveStudio(t, s, http.MethodPost, "/api/studio/uploads/asset-3/finalize", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("image finalize status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "object_key") {
		t.Fatalf("finalize body exposed the object key: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "asset-3") {
		t.Fatalf("finalize body did not name the asset: %s", rec.Body.String())
	}
}

func TestStudioNeedsABackendAndAPrincipal(t *testing.T) {
	routes := []struct {
		method, target, body string
	}{
		{http.MethodGet, "/api/studio/models?kind=video", ""},
		{http.MethodPost, "/api/studio/videos", `{"model":"m","prompt":"p"}`},
		{http.MethodPost, "/api/studio/images", `{"model":"m","prompt":"p"}`},
		{http.MethodGet, "/api/studio/history", ""},
		{http.MethodGet, "/api/studio/library", ""},
		{http.MethodPost, "/api/studio/uploads/asset-1/finalize", ""},
	}
	unwired := studioServer(t, nil)
	wired := studioServer(t, &fakeStudioBackend{})
	for _, route := range routes {
		rec := serveStudio(t, unwired, route.method, route.target, route.body)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s without a backend = %d, want 503", route.method, route.target, rec.Code)
		}

		req := httptest.NewRequest(route.method, route.target, strings.NewReader(route.body))
		if route.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		anonymous := httptest.NewRecorder()
		wired.Mux().ServeHTTP(anonymous, req)
		if anonymous.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a principal = %d, want 401", route.method, route.target, anonymous.Code)
		}
	}
}
