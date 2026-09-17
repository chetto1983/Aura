package mediagen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeSubmitProvider is OpenRouter's video catalog and submission endpoint. It counts the POSTs
// so a test can prove a paid call happened exactly once, or not at all.
type fakeSubmitProvider struct {
	*httptest.Server
	catalog  string
	accepted string
	posts    int
	bodies   []map[string]any
}

func newFakeSubmitProvider(t *testing.T, p *fakeSubmitProvider) *fakeSubmitProvider {
	t.Helper()
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			respondJSON(w, http.StatusOK, p.catalog)
			return
		}
		p.posts++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode submit body: %v", err)
		}
		p.bodies = append(p.bodies, body)
		respondJSON(w, http.StatusOK, p.accepted)
	}))
	t.Cleanup(p.Close)
	return p
}

const seededVideoCatalog = `{"data":[{"id":"google/veo-3.1-lite","seed":true,` +
	`"supported_frame_images":["first_frame","last_frame"],"supported_durations":[6]}]}`

// newSubmitFixture wires a submitter over the fakes with both frames owned by "owner".
func newSubmitFixture(t *testing.T, provider *fakeSubmitProvider) (*VideoSubmitter, *fakeJobStore, *fakeReferenceReader) {
	t.Helper()
	png, jpg := tinyPNG(t), tinyJPEG(t)
	references := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/start": {data: png, meta: ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: int64(len(png))}},
		"owner/end":   {data: jpg, meta: ReferenceMeta{MIMEType: "image/jpeg", Modality: "image", SizeBytes: int64(len(jpg))}},
	}}
	jobs := newFakeJobStore(time.Now)
	return &VideoSubmitter{
		Credentials:   &fakeCredentials{answers: []credentialAnswer{{baseURL: provider.URL}}},
		Catalog:       NewCatalog(provider.Client()),
		Client:        NewClient(provider.Client(), 1<<20),
		References:    references,
		Jobs:          jobs,
		MaxImageBytes: 1 << 20,
	}, jobs, references
}

func studioSubmission() VideoSubmission {
	seed := 7
	return VideoSubmission{
		Owner: "owner", Surface: SurfaceStudio, Model: "google/veo-3.1-lite",
		Input: VideoInput{
			Prompt: "a lighthouse at dusk", Duration: 6,
			FirstFrameAssetID: "start", LastFrameAssetID: "end", Seed: &seed,
		},
	}
}

// TestVideoSubmitterRecordsAStudioSubmission pins what the Studio needs the one submit path to
// do that the chat tool never did: a job with no conversation, both frames in the body in the
// order the provider reads them, the seed, and an audit naming the end frame so a delivery after
// a restart still reports it. The fake store runs the real validateNewJob, so a surface or kind
// this path forgets is a failed Insert, not a silently mislabelled row.
func TestVideoSubmitterRecordsAStudioSubmission(t *testing.T) {
	provider := newFakeSubmitProvider(t, &fakeSubmitProvider{
		catalog:  seededVideoCatalog,
		accepted: `{"id":"prov-1","status":"pending","usage":{"cost":0.42}}`,
	})
	submitter, jobs, references := newSubmitFixture(t, provider)

	job, err := submitter.Submit(context.Background(), studioSubmission())
	if err != nil {
		t.Fatal(err)
	}
	if job.Surface != SurfaceStudio || job.Kind != KindVideo || job.ConversationID != "" || job.ToolCallID != "" {
		t.Fatalf("job = %+v, want a Studio video job bound to no conversation", job)
	}
	if job.ProviderJobID != "prov-1" || job.Status != StatusPending || job.CostUSD == nil || *job.CostUSD != 0.42 {
		t.Fatalf("job = %+v, want the accepted provider job", job)
	}

	if provider.posts != 1 {
		t.Fatalf("posts = %d, want exactly one paid call", provider.posts)
	}
	body := provider.bodies[0]
	if body["seed"] != float64(7) {
		t.Fatalf("submit body seed = %v, want 7", body["seed"])
	}
	frames, _ := body["frame_images"].([]any)
	if len(frames) != 2 {
		t.Fatalf("frame_images = %v, want the start and the end frame", body["frame_images"])
	}
	wantURLs := []string{
		"data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG(t)),
		"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(tinyJPEG(t)),
	}
	for i, wantType := range []string{"first_frame", "last_frame"} {
		frame, _ := frames[i].(map[string]any)
		url, _ := frame["image_url"].(map[string]any)["url"].(string)
		if frame["frame_type"] != wantType || url != wantURLs[i] {
			t.Fatalf("frame_images[%d] = %#v, want the owned %s image", i, frame, wantType)
		}
	}
	if len(references.opened) != 2 || references.opened[0] != "owner/start" || references.opened[1] != "owner/end" {
		t.Fatalf("opened = %v, want the start frame then the end frame", references.opened)
	}

	stored, err := jobs.Get(context.Background(), "owner", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	req, audit, err := stored.Submission()
	if err != nil {
		t.Fatal(err)
	}
	wantOrigin, _ := SubmissionOrigin(provider.URL)
	if audit.Origin != wantOrigin || audit.FirstFrameAssetID != "start" || audit.LastFrameAssetID != "end" {
		t.Fatalf("audit = %+v, want both frames and the submission origin", audit)
	}
	if req.Seed == nil || *req.Seed != 7 || len(req.FrameImages) != 0 {
		t.Fatalf("persisted request = %+v, want the seed and no image data", req)
	}
}

// TestVideoSubmitterRefusesBeforeSpending pins that every local refusal lands before the paid
// POST: the clamp's verdict on an end frame the model cannot take, and an unreadable reference.
func TestVideoSubmitterRefusesBeforeSpending(t *testing.T) {
	for _, tc := range []struct {
		name    string
		catalog string
		mutate  func(*VideoSubmission)
		code    string
	}{
		{
			name:    "an end frame the model cannot take",
			catalog: `{"data":[{"id":"google/veo-3.1-lite","supported_frame_images":["first_frame"]}]}`,
			code:    "unsupported",
		},
		{
			name:    "a reference that is not owned",
			catalog: seededVideoCatalog,
			mutate:  func(s *VideoSubmission) { s.Input.ReferenceAssetIDs = []string{"someone-elses"} },
			code:    "asset_not_found",
		},
		// The frames are read in request(), ahead of the ordinary references; a rewrite that
		// moved only the frame reads past the POST would still pass the reference case above.
		{
			name:    "a start frame that is not owned",
			catalog: seededVideoCatalog,
			mutate:  func(s *VideoSubmission) { s.Input.FirstFrameAssetID = "someone-elses" },
			code:    "asset_not_found",
		},
		{
			name:    "an end frame that is not owned",
			catalog: seededVideoCatalog,
			mutate:  func(s *VideoSubmission) { s.Input.LastFrameAssetID = "someone-elses" },
			code:    "asset_not_found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := newFakeSubmitProvider(t, &fakeSubmitProvider{catalog: tc.catalog, accepted: `{}`})
			submitter, jobs, _ := newSubmitFixture(t, provider)
			submission := studioSubmission()
			if tc.mutate != nil {
				tc.mutate(&submission)
			}
			if _, err := submitter.Submit(context.Background(), submission); ErrorCode(err) != tc.code {
				t.Fatalf("err = %v (code %q), want %s", err, ErrorCode(err), tc.code)
			}
			if provider.posts != 0 {
				t.Fatalf("posts = %d, want none: nothing may be billed", provider.posts)
			}
			if jobs.count("Insert") != 0 {
				t.Fatalf("inserts = %d, want none", jobs.count("Insert"))
			}
		})
	}
}

// TestVideoSubmitterNeverResubmitsALostRecord pins the excluded submission interval: the provider
// has been paid, so a failed Insert is reported, never retried.
func TestVideoSubmitterNeverResubmitsALostRecord(t *testing.T) {
	provider := newFakeSubmitProvider(t, &fakeSubmitProvider{
		catalog:  seededVideoCatalog,
		accepted: `{"id":"prov-1","status":"in_progress"}`,
	})
	submitter, jobs, _ := newSubmitFixture(t, provider)
	jobs.failNext("Insert", errors.New("connection reset by peer"))

	_, err := submitter.Submit(context.Background(), studioSubmission())
	if ErrorCode(err) != "job_failed" {
		t.Fatalf("err = %v (code %q), want job_failed", err, ErrorCode(err))
	}
	if cause := errors.Unwrap(err); cause == nil || cause.Error() != "connection reset by peer" {
		t.Fatalf("err %v carries cause %v, want the store failure for the log", err, cause)
	}
	if provider.posts != 1 {
		t.Fatalf("posts = %d, want exactly one: a lost record is never submitted again", provider.posts)
	}
	if jobs.count("Insert") != 1 {
		t.Fatalf("inserts = %d, want exactly one attempt", jobs.count("Insert"))
	}
}

// TestVideoSubmitterRecordsOnADetachedContext pins that an accepted job is still written when the
// turn that asked for it is already over. The caller's deadline here is far shorter than the
// record's own, so an Insert that inherited the caller's context would show that shorter deadline
// and, on a turn already cancelled, would lose a job the provider had been paid for.
func TestVideoSubmitterRecordsOnADetachedContext(t *testing.T) {
	provider := newFakeSubmitProvider(t, &fakeSubmitProvider{
		catalog:  seededVideoCatalog,
		accepted: `{"id":"prov-1","status":"in_progress"}`,
	})
	submitter, jobs, _ := newSubmitFixture(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := submitter.Submit(ctx, studioSubmission()); err != nil {
		t.Fatal(err)
	}
	deadline := jobs.deadline("Insert")
	if remaining := time.Until(deadline); remaining <= 2*time.Second || remaining > videoJobRecordTimeout {
		t.Fatalf("the record Insert had %v left, want close to %v: it must not inherit the turn's deadline",
			remaining, videoJobRecordTimeout)
	}
}

// TestVideoSubmitterStoresAnAcceptedJobInProgress pins that only pending stays pending: a
// provider already claiming completed has produced no Aura asset, so the watcher decides it.
func TestVideoSubmitterStoresAnAcceptedJobInProgress(t *testing.T) {
	provider := newFakeSubmitProvider(t, &fakeSubmitProvider{
		catalog:  seededVideoCatalog,
		accepted: `{"id":"prov-1","status":"completed"}`,
	})
	submitter, _, _ := newSubmitFixture(t, provider)
	job, err := submitter.Submit(context.Background(), studioSubmission())
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusInProgress {
		t.Fatalf("status = %q, want in_progress until the watcher has looked", job.Status)
	}
}

func TestVideoSubmitterConfigured(t *testing.T) {
	var nilSubmitter *VideoSubmitter
	if nilSubmitter.Configured() || (&VideoSubmitter{}).Configured() {
		t.Fatal("a nil or zero submitter must never report itself configured")
	}
	provider := newFakeSubmitProvider(t, &fakeSubmitProvider{catalog: seededVideoCatalog, accepted: `{}`})
	full, _, _ := newSubmitFixture(t, provider)
	if !full.Configured() {
		t.Fatal("a fully wired submitter must report itself configured")
	}
	for name, breakOne := range map[string]func(*VideoSubmitter){
		"credentials":   func(s *VideoSubmitter) { s.Credentials = nil },
		"catalog":       func(s *VideoSubmitter) { s.Catalog = nil },
		"client":        func(s *VideoSubmitter) { s.Client = nil },
		"references":    func(s *VideoSubmitter) { s.References = nil },
		"jobs":          func(s *VideoSubmitter) { s.Jobs = nil },
		"image ceiling": func(s *VideoSubmitter) { s.MaxImageBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			broken, _, _ := newSubmitFixture(t, provider)
			breakOne(broken)
			if broken.Configured() {
				t.Fatalf("a submitter missing its %s must not report itself configured", name)
			}
		})
	}
}
