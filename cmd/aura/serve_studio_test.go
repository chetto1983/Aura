package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
)

const studioOwner = "0192f6d4-6a3c-7c1e-9b2a-3f4e5d6c7b8c"

type stubStudioCatalog struct {
	models []mediagen.Model
	err    error
}

func (s *stubStudioCatalog) List(_ context.Context, _ mediagen.Kind, _ bool) ([]mediagen.Model, error) {
	return s.models, s.err
}

type stubStudioSettings struct{ model string }

func (s stubStudioSettings) Model(context.Context, mediagen.Kind) (string, error) {
	return s.model, nil
}

func (s stubStudioSettings) VideoInlineWait(context.Context) (time.Duration, error) { return 0, nil }

// stubStudioCredentials counts its reads and answers a DIFFERENT route after the first, so a
// caller that resolves the origin separately from the paid call records the wrong one.
type stubStudioCredentials struct {
	baseURL string
	calls   *int
}

func (s stubStudioCredentials) For(context.Context, string) (string, string, error) {
	if s.calls == nil {
		return s.baseURL, "sk-test", nil
	}
	*s.calls++
	if *s.calls > 1 {
		return "https://moved.example/v1", "sk-test", nil
	}
	return s.baseURL, "sk-test", nil
}

type stubStudioAssets struct {
	ingested  assets.AgentIngestRequest
	ingestErr error
	body      []byte

	listedOwner string
	listedLimit int
	recent      []assets.Asset

	finalizedID       string
	finalizedModality assets.Modality
}

func (s *stubStudioAssets) IngestAgentFile(_ context.Context, req assets.AgentIngestRequest) (assets.Asset, error) {
	s.ingested = req
	if req.Reader != nil {
		body, err := io.ReadAll(req.Reader)
		if err != nil {
			return assets.Asset{}, err
		}
		s.body = body
	}
	if s.ingestErr != nil {
		return assets.Asset{}, s.ingestErr
	}
	return assets.Asset{ID: "asset-studio-1", Modality: assets.ModalityImage}, nil
}

func (s *stubStudioAssets) ListRecentImages(_ context.Context, identityID string, limit int) ([]assets.Asset, error) {
	s.listedOwner, s.listedLimit = identityID, limit
	return s.recent, nil
}

func (s *stubStudioAssets) FinalizeUnprocessed(_ context.Context, _, assetID string, modality assets.Modality) (assets.Asset, error) {
	s.finalizedID, s.finalizedModality = assetID, modality
	return assets.Asset{ID: assetID, Modality: modality}, nil
}

type stubStudioJobs struct {
	inserted mediagen.Job
	inserts  int

	listedOwner  string
	listedBefore string
	listedKind   mediagen.Kind
	listedLimit  int
}

func (s *stubStudioJobs) InsertImage(_ context.Context, job mediagen.Job) (mediagen.Job, error) {
	s.inserted, s.inserts = job, s.inserts+1
	job.ID = "job-image-1"
	return job, nil
}

func (s *stubStudioJobs) ListStudio(_ context.Context, ownerID, beforeID string, kind mediagen.Kind, limit int) ([]mediagen.Job, error) {
	s.listedOwner, s.listedBefore, s.listedKind, s.listedLimit = ownerID, beforeID, kind, limit
	return nil, nil
}

// studioFixture is a backend whose every paid leg is a recorder: no provider is reached, so a
// test that expects a refusal can prove nothing was generated rather than only that an error
// came back.
type studioFixture struct {
	backend     studioBackend
	catalog     *stubStudioCatalog
	assets      *stubStudioAssets
	jobs        *stubStudioJobs
	submissions []mediagen.VideoSubmission
	generations []mediagen.ImageGeneration
	tracked     []mediagen.Job
	generated   mediagen.GeneratedImage
	// credentialReads counts the backend's OWN credential resolutions, which the image path
	// must not need: the generator already reports the origin it paid.
	credentialReads int
}

func newStudioFixture(t *testing.T, models ...mediagen.Model) *studioFixture {
	t.Helper()
	cost := 0.04
	fixture := &studioFixture{
		catalog: &stubStudioCatalog{models: models},
		assets:  &stubStudioAssets{},
		jobs:    &stubStudioJobs{},
		generated: mediagen.GeneratedImage{
			Result: mediagen.ImageResult{Bytes: []byte("PNG-BYTES"), MIMEType: "image/png", CostUSD: &cost},
			Prompt: "a cat in a hat",
			// The generator reports the route it actually paid; the record must use this one.
			Origin:      "https://openrouter.ai/api/v1",
			Used:        mediagen.ImageInput{Prompt: "a cat in a hat", AspectRatio: "1:1"},
			Adjustments: []string{"aspect ratio narrowed to 1:1"},
		},
	}
	fixture.backend = studioBackend{
		catalog:     fixture.catalog,
		settings:    stubStudioSettings{model: "default/model"},
		credentials: stubStudioCredentials{baseURL: "https://openrouter.ai/api/v1", calls: &fixture.credentialReads},
		submit: func(_ context.Context, submission mediagen.VideoSubmission) (mediagen.Job, error) {
			fixture.submissions = append(fixture.submissions, submission)
			return mediagen.Job{ID: "job-video-1", IdentityID: submission.Owner, Surface: submission.Surface,
				Kind: mediagen.KindVideo, Status: mediagen.StatusPending}, nil
		},
		generate: func(_ context.Context, generation mediagen.ImageGeneration) (mediagen.GeneratedImage, error) {
			fixture.generations = append(fixture.generations, generation)
			return fixture.generated, nil
		},
		track:  func(job mediagen.Job) { fixture.tracked = append(fixture.tracked, job) },
		assets: fixture.assets,
		jobs:   fixture.jobs,
	}
	return fixture
}

func (f *studioFixture) assertNothingGenerated(t *testing.T) {
	t.Helper()
	if len(f.submissions) != 0 || len(f.generations) != 0 {
		t.Fatalf("a refusal still reached a paid path: %d submissions, %d generations", len(f.submissions), len(f.generations))
	}
	if f.jobs.inserts != 0 || f.assets.ingested.IdentityID != "" {
		t.Fatalf("a refusal still wrote a row or an asset")
	}
}

func TestStudioRefusesAModelTheCatalogDoesNotList(t *testing.T) {
	fixture := newStudioFixture(t, mediagen.Model{ID: "listed/model", Kind: mediagen.KindVideo})

	_, videoErr := fixture.backend.SubmitVideo(context.Background(), studioOwner, agui.StudioVideoRequest{
		Model: "forged/model", Prompt: "a cat",
	})
	_, imageErr := fixture.backend.GenerateImage(context.Background(), studioOwner, agui.StudioImageRequest{
		Model: "forged/model", Prompt: "a cat",
	})

	for _, err := range []error{videoErr, imageErr} {
		if mediagen.ErrorCode(err) != "unsupported" {
			t.Fatalf("error = %v (code %q), want unsupported", err, mediagen.ErrorCode(err))
		}
	}
	fixture.assertNothingGenerated(t)
}

func TestStudioReportsAnUnavailableCatalogAsNothingGenerated(t *testing.T) {
	fixture := newStudioFixture(t)
	fixture.catalog.err = errors.New("GET videos/models: 503 from https://openrouter.ai with key sk-secret")

	_, err := fixture.backend.SubmitVideo(context.Background(), studioOwner, agui.StudioVideoRequest{
		Model: "listed/model", Prompt: "a cat",
	})

	if mediagen.ErrorCode(err) != "job_failed" {
		t.Fatalf("error = %v (code %q), want job_failed", err, mediagen.ErrorCode(err))
	}
	if strings.Contains(err.Error(), "sk-secret") || strings.Contains(err.Error(), "openrouter.ai") {
		t.Fatalf("the refusal echoed the catalog's cause: %q", err.Error())
	}
	fixture.assertNothingGenerated(t)
}

// A local route is not a failed generation: it is a routing choice the operator can undo, and
// the cockpit prints its own sentence for it, so the sentinel has to survive the wrapping.
func TestStudioKeepsTheLocalRouteRefusal(t *testing.T) {
	fixture := newStudioFixture(t)
	fixture.catalog.err = agui.ErrCatalogLocalRoute

	_, err := fixture.backend.SubmitVideo(context.Background(), studioOwner, agui.StudioVideoRequest{Model: "m"})

	if !errors.Is(err, agui.ErrCatalogLocalRoute) {
		t.Fatalf("error = %v, want ErrCatalogLocalRoute", err)
	}
	fixture.assertNothingGenerated(t)
}

// The Studio's form leaves every option optional, and an option the body omits is not free:
// the provider fills an absent field with its own default, which on a per-second SKU is the
// long, high, audible clip. What leaves for the provider must be the cheapest the model offers.
func TestStudioSubmitsTheCheapestOptionsWhenTheRequestOmitsThem(t *testing.T) {
	fixture := newStudioFixture(t, mediagen.Model{
		ID: "listed/model", Kind: mediagen.KindVideo,
		Durations: []int{12, 4, 8}, Resolutions: []string{"1080p", "480p", "720p"}, GenerateAudio: true,
	})

	if _, err := fixture.backend.SubmitVideo(context.Background(), studioOwner,
		agui.StudioVideoRequest{Model: "listed/model", Prompt: "a cat"}); err != nil {
		t.Fatalf("SubmitVideo() error = %v", err)
	}

	if len(fixture.submissions) != 1 {
		t.Fatalf("submissions = %d, want 1", len(fixture.submissions))
	}
	input := fixture.submissions[0].Input
	if input.Duration != 4 {
		t.Fatalf("duration = %d, want the shortest the model declares (4)", input.Duration)
	}
	if input.Resolution != "480p" {
		t.Fatalf("resolution = %q, want the lowest the model declares (480p)", input.Resolution)
	}
	if input.Audio == nil || *input.Audio {
		t.Fatalf("audio = %v, want an explicit false rather than the provider's choice", input.Audio)
	}
	if input.Seed != nil {
		t.Fatalf("seed = %d, want none: no seed is already the cheapest", *input.Seed)
	}
}

func TestStudioSubmitsOnTheStudioSurfaceAndTracksWithoutAnInlineWaiter(t *testing.T) {
	fixture := newStudioFixture(t, mediagen.Model{ID: "listed/model", Kind: mediagen.KindVideo})
	audio, seed := true, 11

	job, err := fixture.backend.SubmitVideo(context.Background(), studioOwner, agui.StudioVideoRequest{
		Model: "listed/model", Prompt: "a cat", Duration: 8, Resolution: "720p", AspectRatio: "16:9",
		Audio: &audio, Seed: &seed, FirstFrameAssetID: "asset-first", LastFrameAssetID: "asset-last",
	})
	if err != nil {
		t.Fatalf("SubmitVideo() error = %v", err)
	}

	if len(fixture.submissions) != 1 {
		t.Fatalf("submissions = %d, want 1", len(fixture.submissions))
	}
	submission := fixture.submissions[0]
	if submission.Surface != mediagen.SurfaceStudio {
		t.Fatalf("surface = %q, want studio", submission.Surface)
	}
	if submission.ConversationID != "" || submission.ToolCallID != "" {
		t.Fatalf("a Studio submission named a conversation or a call: %#v", submission)
	}
	if submission.Owner != studioOwner || submission.Model != "listed/model" {
		t.Fatalf("submission = %#v", submission)
	}
	input := submission.Input
	if input.Duration != 8 || input.Resolution != "720p" || input.AspectRatio != "16:9" ||
		input.FirstFrameAssetID != "asset-first" || input.LastFrameAssetID != "asset-last" {
		t.Fatalf("submitted input = %#v", input)
	}
	if input.Audio == nil || !*input.Audio || input.Seed == nil || *input.Seed != 11 {
		t.Fatalf("submitted audio/seed = %#v, %#v", input.Audio, input.Seed)
	}
	if len(fixture.tracked) != 1 || fixture.tracked[0].ID != job.ID {
		t.Fatalf("tracked = %#v, want the accepted job", fixture.tracked)
	}
}

func TestStudioStoresTheImageOutsideEveryConversation(t *testing.T) {
	fixture := newStudioFixture(t, mediagen.Model{ID: "listed/image", Kind: mediagen.KindImage})

	job, err := fixture.backend.GenerateImage(context.Background(), studioOwner, agui.StudioImageRequest{
		Model: "listed/image", Prompt: "a cat in a hat", AspectRatio: "1:1", ReferenceAssetIDs: []string{"ref-1"},
	})
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}

	ingested := fixture.assets.ingested
	if ingested.IdentityID != studioOwner || ingested.ThreadID != "" || ingested.ToolCallID != "" {
		t.Fatalf("ingest = %#v, want the owner's own file outside every thread", ingested)
	}
	if ingested.Modality != assets.ModalityImage || ingested.MIMEType != "image/png" ||
		!strings.HasSuffix(ingested.FileName, ".png") || ingested.SizeBytes != int64(len("PNG-BYTES")) {
		t.Fatalf("ingest = %#v", ingested)
	}
	if string(fixture.assets.body) != "PNG-BYTES" {
		t.Fatalf("ingested bytes = %q", fixture.assets.body)
	}

	row := fixture.jobs.inserted
	if row.Surface != mediagen.SurfaceStudio || row.Kind != mediagen.KindImage {
		t.Fatalf("row = %#v, want a Studio image row", row)
	}
	if row.ConversationID != "" || row.ToolCallID != "" {
		t.Fatalf("the image row named a conversation or a call: %#v", row)
	}
	if row.IdentityID != studioOwner || row.Model != "listed/image" || row.AssetID != "asset-studio-1" {
		t.Fatalf("row = %#v", row)
	}
	if row.CostUSD == nil || *row.CostUSD != 0.04 {
		t.Fatalf("row cost = %#v, want the paid cost", row.CostUSD)
	}
	if !strings.HasPrefix(row.ProviderJobID, "image-") || len(row.ProviderJobID) <= len("image-") {
		t.Fatalf("provider job id = %q, want a minted image-<uuid>", row.ProviderJobID)
	}
	if ingested.SourceRef != "studio:"+row.ProviderJobID {
		t.Fatalf("source ref = %q, want it to name the same minted id", ingested.SourceRef)
	}
	if job.ID != "job-image-1" {
		t.Fatalf("returned job = %#v, want the recorded row", job)
	}

	request, audit, err := row.Submission()
	if err != nil {
		t.Fatalf("the recorded image request is unreadable: %v", err)
	}
	if request.Prompt != "a cat in a hat" || request.AspectRatio != "1:1" {
		t.Fatalf("recorded request = %#v", request)
	}
	if audit.Origin != "https://openrouter.ai/api/v1" || len(audit.Adjustments) != 1 {
		t.Fatalf("recorded audit = %#v", audit)
	}
	// The recorded origin is the generator's own. A backend that resolved credentials again
	// would have recorded https://moved.example/v1 — the endpoint the image did NOT come from.
	if fixture.credentialReads != 0 {
		t.Fatalf("the image path resolved credentials %d time(s) of its own; the origin must come back with the result",
			fixture.credentialReads)
	}
}

// A stored image whose row fails is reported as stored, never regenerated: the operator has
// already paid and the bytes are theirs.
func TestStudioReportsAStoredImageThatCouldNotBeSaved(t *testing.T) {
	fixture := newStudioFixture(t, mediagen.Model{ID: "listed/image", Kind: mediagen.KindImage})
	fixture.assets.ingestErr = errors.New("put object: bucket aura-assets unreachable")

	_, err := fixture.backend.GenerateImage(context.Background(), studioOwner, agui.StudioImageRequest{
		Model: "listed/image", Prompt: "a cat",
	})

	if mediagen.ErrorCode(err) != "job_failed" {
		t.Fatalf("error = %v (code %q), want job_failed", err, mediagen.ErrorCode(err))
	}
	if strings.Contains(err.Error(), "aura-assets") {
		t.Fatalf("the refusal echoed the store's cause: %q", err.Error())
	}
	if fixture.jobs.inserts != 0 {
		t.Fatalf("an unstored image still wrote a row")
	}
}

func TestStudioReadsHistoryAndLibraryForTheOwner(t *testing.T) {
	fixture := newStudioFixture(t)
	fixture.assets.recent = []assets.Asset{{ID: "asset-recent", Modality: assets.ModalityImage}}

	if _, err := fixture.backend.History(context.Background(), studioOwner, "job-9", mediagen.KindImage, 7); err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if fixture.jobs.listedOwner != studioOwner || fixture.jobs.listedBefore != "job-9" ||
		fixture.jobs.listedKind != mediagen.KindImage || fixture.jobs.listedLimit != 7 {
		t.Fatalf("history read = %q, %q, %q, %d", fixture.jobs.listedOwner, fixture.jobs.listedBefore,
			fixture.jobs.listedKind, fixture.jobs.listedLimit)
	}

	library, err := fixture.backend.Library(context.Background(), studioOwner, 5)
	if err != nil {
		t.Fatalf("Library() error = %v", err)
	}
	if len(library) != 1 || library[0].ID != "asset-recent" {
		t.Fatalf("library = %#v, want the identity's recent images", library)
	}
	if fixture.assets.listedOwner != studioOwner || fixture.assets.listedLimit != 5 {
		t.Fatalf("library read = %q, %d", fixture.assets.listedOwner, fixture.assets.listedLimit)
	}

	// The Studio finalizes a reference the operator uploaded, and only as an image: a document
	// finalized here would become a reference no generation can read.
	if _, err := fixture.backend.FinalizeUpload(context.Background(), studioOwner, "asset-7"); err != nil {
		t.Fatalf("FinalizeUpload() error = %v", err)
	}
	if fixture.assets.finalizedID != "asset-7" || fixture.assets.finalizedModality != assets.ModalityImage {
		t.Fatalf("finalize = %q as %q", fixture.assets.finalizedID, fixture.assets.finalizedModality)
	}
}

func TestStudioModelsCarryTheLiveDefault(t *testing.T) {
	fixture := newStudioFixture(t, mediagen.Model{ID: "listed/model", Kind: mediagen.KindVideo})

	defaultModel, models, err := fixture.backend.Models(context.Background(), mediagen.KindVideo)
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if defaultModel != "default/model" {
		t.Fatalf("default = %q, want the stored settings row", defaultModel)
	}
	if len(models) != 1 || models[0].ID != "listed/model" {
		t.Fatalf("models = %#v", models)
	}
}

// A Studio completion belongs to no conversation, so there is no turn to wake: the dispatcher
// must drop it rather than mint an operation for an empty conversation.
func TestStudioCompletionWakesNoConversation(t *testing.T) {
	run := &fakeBackgroundCompletionRunner{started: make(chan struct{}, 2), release: make(chan struct{}, 2)}
	dispatcher := newBackgroundCompletionDispatcher(context.Background(), run, acceptingSteerPusher{})

	dispatcher.NotifyMedia(mediagen.Completion{
		IdentityID: studioOwner, ConversationID: "", JobID: "job-video-1", Status: mediagen.StatusCompleted,
	})
	// Stop drains every route the enqueue could have started, so the count below is final
	// rather than a read that happened to precede a wake goroutine.
	stopDispatcher(t, dispatcher)

	if wakes := run.recorded(); len(wakes) != 0 {
		t.Fatalf("a Studio completion enqueued %d wakes, want none", len(wakes))
	}
}
