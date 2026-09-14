package mediagen

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func requireRow(t *testing.T, h *watcherHarness, job Job, status Status, code string, cost float64) Job {
	t.Helper()
	row, err := h.store.Get(context.Background(), job.IdentityID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotCode := ""
	if row.Error != nil {
		gotCode = row.Error.Code
	}
	if row.Status != status || gotCode != code || row.CostUSD == nil || *row.CostUSD != cost {
		t.Fatalf("row = status %q, error %q, cost %v; want %q, %q, %v", row.Status, gotCode, row.CostUSD, status, code, cost)
	}
	return row
}

func requireOneNotice(t *testing.T, h *watcherHarness, want Completion) {
	t.Helper()
	h.stop(t)
	if notices := h.drainNotices(); len(notices) != 1 || notices[0] != want {
		t.Fatalf("notices = %+v, want exactly %+v", notices, want)
	}
}

func TestWatcherRetriesTheDownloadWithoutPollingOrSubmittingAgain(t *testing.T) {
	provider := newFakeProvider(t, statuses(`{"id":"vid_dl","status":"completed","usage":{"cost":0.5}}`),
		func(n int32, w http.ResponseWriter, r *http.Request) {
			switch n {
			case 1:
				respondJSON(w, http.StatusTooManyRequests, `{"error":{"code":429,"message":"Rate limited"}}`)
			case 2:
				conn, _, err := http.NewResponseController(w).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			default:
				serveClip(n, w, r)
			}
		})
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_dl")

	finished := h.awaitTerminal(t, job)
	row := requireRow(t, h, job, StatusCompleted, "", 0.5)
	if row.AssetID == "" || finished.AssetID != row.AssetID {
		t.Fatalf("finished asset %q, row asset %q; want the ingested clip", finished.AssetID, row.AssetID)
	}
	if provider.polls.Load() != 1 || provider.downloads.Load() != 3 || provider.posts.Load() != 0 {
		t.Fatalf("polls=%d downloads=%d POSTs=%d; want the completion kept across 3 downloads and no submission",
			provider.polls.Load(), provider.downloads.Load(), provider.posts.Load())
	}
	requireOneNotice(t, h, completionOf(finished))
}

func TestWatcherReusesTheIngestedAssetAcrossFailures(t *testing.T) {
	provider := newFakeProvider(t, statuses(`{"id":"vid_ingest","status":"completed","usage":{"cost":0.35}}`), serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	h.assets.fail = []error{errors.New("object store: connection reset")}
	h.store.failNext("Complete", errors.New("conn closed"))
	job := h.insertJob(t, "vid_ingest")

	finished := h.awaitTerminal(t, job)
	requireRow(t, h, job, StatusCompleted, "", 0.35)
	if ingests := h.assets.ingests(); !slices.Equal(ingests, []string{job.ID, job.ID}) {
		t.Fatalf("ingests = %q, want the same job ingested again only after the lost ingest answer", ingests)
	}
	if want := h.assets.bySource["media-job:"+job.ID]; finished.AssetID != want || len(h.assets.bySource) != 1 {
		t.Fatalf("asset = %q, want the one asset %q stored for the job", finished.AssetID, want)
	}
	if h.store.count("Complete") != 2 || provider.downloads.Load() != 2 {
		t.Fatalf("completes=%d downloads=%d; a Complete retry must not fetch or ingest the clip again",
			h.store.count("Complete"), provider.downloads.Load())
	}
	requireOneNotice(t, h, completionOf(finished))
}

func TestWatcherRecordsARemoteFailureWithItsCost(t *testing.T) {
	cases := []struct {
		status Status
		code   string
	}{
		{StatusFailed, "job_failed"},
		{StatusExpired, "job_expired"},
		{StatusCancelled, "job_failed"},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			provider := newFakeProvider(t, statuses(
				`{"id":"vid_fail","status":"in_progress"}`,
				`{"id":"vid_fail","status":"`+string(tc.status)+`","error":"provider gave up","usage":{"cost":0.3}}`,
			), refuseRequest(t))
			h := newWatcherHarness(t, context.Background(), provider, nil)
			job := h.insertJob(t, "vid_fail")

			finished := h.awaitTerminal(t, job)
			row := requireRow(t, h, job, tc.status, tc.code, 0.3)
			if row.CompletedAt == nil || row.Error.Message != "provider gave up" {
				t.Fatalf("row = %+v; want its completion time and the provider's message", row)
			}
			requireOneNotice(t, h, completionOf(finished))
		})
	}
}

func TestWatcherExpiresAtTheCeilingWithTheLastKnownCost(t *testing.T) {
	cases := map[string]struct {
		poll, download   providerHandler
		spendOnDownload  bool
		polls, downloads int32
	}{
		"while the job is still running": {
			poll: func(_ int32, w http.ResponseWriter, _ *http.Request) {
				respondJSON(w, http.StatusOK, `{"id":"vid_slow","status":"in_progress","usage":{"cost":0.2}}`)
			},
			polls: 1,
		},
		"while the clip download is retried": {
			poll: statuses(`{"id":"vid_slow","status":"completed","usage":{"cost":0.2}}`),
			download: func(_ int32, w http.ResponseWriter, _ *http.Request) {
				respondJSON(w, http.StatusBadGateway, `{"error":{"code":502,"message":"Bad gateway"}}`)
			},
			spendOnDownload: true,
			polls:           1,
			downloads:       1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var h *watcherHarness
			spendLifetime := func(handler providerHandler) providerHandler {
				return func(n int32, w http.ResponseWriter, r *http.Request) {
					h.clock.advance(VideoJobMaxAge)
					handler(n, w, r)
				}
			}
			poll, download := spendLifetime(tc.poll), refuseRequest(t)
			if tc.spendOnDownload {
				poll, download = tc.poll, spendLifetime(tc.download)
			}
			provider := newFakeProvider(t, poll, download)
			h = newWatcherHarness(t, context.Background(), provider, nil)
			job := h.insertJob(t, "vid_slow")

			finished := h.awaitTerminal(t, job)
			requireRow(t, h, job, StatusExpired, "job_expired", 0.2)
			if provider.polls.Load() != tc.polls || provider.downloads.Load() != tc.downloads {
				t.Fatalf("polls=%d downloads=%d; want %d and %d, and no provider call past the ceiling",
					provider.polls.Load(), provider.downloads.Load(), tc.polls, tc.downloads)
			}
			requireOneNotice(t, h, completionOf(finished))
		})
	}
}

func TestWatcherFailsAClipItCannotStoreWithItsCost(t *testing.T) {
	cases := map[string]struct {
		download providerHandler
		refuse   error
		code     string
		ingests  int
	}{
		"declared above the byte ceiling": {
			download: func(_ int32, w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("Content-Length", "2097152")
				w.WriteHeader(http.StatusOK)
				http.NewResponseController(w).Flush()
				<-r.Context().Done()
			},
			code: "too_large",
		},
		"not a storable video": {
			download: serveClip,
			refuse:   &Error{Code: "unsupported", Message: "The generated video is neither MP4 nor WebM."},
			code:     "unsupported",
			ingests:  1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider := newFakeProvider(t, statuses(`{"id":"vid_big","status":"completed","usage":{"cost":0.6}}`), tc.download)
			h := newWatcherHarness(t, context.Background(), provider, nil)
			if tc.refuse != nil {
				h.assets.fail = []error{tc.refuse}
			}
			job := h.insertJob(t, "vid_big")

			finished := h.awaitTerminal(t, job)
			requireRow(t, h, job, StatusFailed, tc.code, 0.6)
			if len(h.assets.ingests()) != tc.ingests || provider.downloads.Load() != 1 {
				t.Fatalf("ingests=%d downloads=%d; a clip no retry can store is fetched once", len(h.assets.ingests()), provider.downloads.Load())
			}
			requireOneNotice(t, h, completionOf(finished))
		})
	}
}

func TestWatcherNeverSendsTheJobToAnotherOrigin(t *testing.T) {
	provider := newFakeProvider(t, statuses(`{"id":"vid_origin","status":"completed","usage":{"cost":0.1}}`), serveClip)
	other := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	h.credentials.answers = []credentialAnswer{
		{err: &Error{Code: "no_key", Message: "Connect this identity to OpenRouter."}},
		{baseURL: other.URL},
		{baseURL: provider.URL + "/v2"},
		{baseURL: "not a url"},
		{baseURL: provider.URL + "/"},
	}
	job := h.insertJob(t, "vid_origin")

	finished := h.awaitTerminal(t, job)
	requireRow(t, h, job, StatusCompleted, "", 0.1)
	h.credentials.mu.Lock()
	reads := h.credentials.calls
	h.credentials.mu.Unlock()
	if len(other.recorded()) != 0 || reads < 5 {
		t.Fatalf("other origin saw %q after %d credential reads", other.recorded(), reads)
	}
	for _, request := range provider.recorded() {
		if !strings.HasPrefix(request, "GET /videos/vid_origin") {
			t.Fatalf("request %q reached a path of another origin", request)
		}
	}
	requireOneNotice(t, h, completionOf(finished))
}

func TestWatcherWritesOnlyUnrecordedProgress(t *testing.T) {
	provider := newFakeProvider(t, statuses(
		`{"id":"vid_steps","status":"pending"}`,
		`{"id":"vid_steps","status":"queued"}`,
		`{"id":"vid_steps","status":"in_progress","usage":{"cost":0.1}}`,
		`{"id":"vid_steps","status":"in_progress","usage":{"cost":0.1}}`,
		`{"id":"vid_steps","status":"in_progress"}`,
		`{"id":"vid_steps","status":"in_progress","usage":{"cost":0.15}}`,
		`{"id":"vid_steps","status":"completed"}`,
	), serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_steps")

	finished := h.awaitTerminal(t, job)
	requireRow(t, h, job, StatusCompleted, "", 0.15)
	if h.store.count("Progress") != 2 || provider.polls.Load() != 7 {
		t.Fatalf("Progress=%d polls=%d; want writes only for the new status and the new cost",
			h.store.count("Progress"), provider.polls.Load())
	}
	requireOneNotice(t, h, completionOf(finished))
}

func TestWatcherPublishesARowFinishedElsewhere(t *testing.T) {
	var h *watcherHarness
	var job Job
	provider := newFakeProvider(t, func(_ int32, w http.ResponseWriter, _ *http.Request) {
		cancelled := job
		cancelled.Status = StatusCancelled
		h.store.seed(cancelled)
		respondJSON(w, http.StatusOK, `{"id":"vid_elsewhere","status":"in_progress"}`)
	}, refuseRequest(t))
	h = newWatcherHarness(t, context.Background(), provider, nil)
	job = h.insertJob(t, "vid_elsewhere")

	if finished := h.awaitTerminal(t, job); finished.Status != StatusCancelled {
		t.Fatalf("finished = %+v, want the row the store already holds", finished)
	}
	job.Status = StatusCancelled
	requireOneNotice(t, h, completionOf(job))
}

func TestWatcherEndsSupervisionWhenTheRowVanishes(t *testing.T) {
	provider := newFakeProvider(t, statuses(`{"id":"vid_gone","status":"in_progress"}`), refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_gone")
	h.store.remove(job.ID)

	h.watcher.Track(job, false)
	awaitSupervisorsExit(t, h.watcher)
	if tracked := trackedJobs(h.watcher); tracked != 0 || provider.polls.Load() != 1 {
		t.Fatalf("tracked=%d polls=%d; a job whose row is gone is dropped after one poll", tracked, provider.polls.Load())
	}
	h.stop(t)
	if notices := h.drainNotices(); len(notices) != 0 {
		t.Fatalf("a vanished job was notified: %+v", notices)
	}
}

func TestResumeWakesUndeliveredCompletionsOnceAndRejoinsActiveJobs(t *testing.T) {
	provider := newFakeProvider(t, func(n int32, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/videos/vid_active" {
			t.Errorf("resume polled %s", r.URL.Path)
		}
		respondJSON(w, http.StatusOK, `{"id":"vid_active","status":"completed","usage":{"cost":0.9}}`)
	}, serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	undelivered, delivered, failed, active := h.insertJob(t, "vid_undelivered"), h.insertJob(t, "vid_delivered"),
		h.insertJob(t, "vid_failed"), h.insertJob(t, "vid_active")
	owner := undelivered.IdentityID
	for _, job := range []*Job{&undelivered, &delivered, &failed, &active} {
		job.IdentityID = owner
	}
	undelivered.Status, undelivered.AssetID = StatusCompleted, uuid.NewString()
	delivered.Status, delivered.AssetID, delivered.DeliveredAt = StatusCompleted, uuid.NewString(), new(h.clock.now())
	failed.Status = StatusFailed
	for _, job := range []Job{undelivered, delivered, failed, active} {
		h.store.seed(job)
	}
	h.insertJob(t, "vid_foreign")

	if err := h.watcher.Resume(context.Background(), owner); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	notices := []Completion{h.awaitNotice(t), h.awaitNotice(t)}
	active.Status = StatusCompleted
	want := []Completion{completionOf(undelivered), completionOf(active)}
	byJob := func(a, b Completion) int { return strings.Compare(a.JobID, b.JobID) }
	slices.SortFunc(notices, byJob)
	slices.SortFunc(want, byJob)
	if !slices.Equal(notices, want) {
		t.Fatalf("notices = %+v, want %+v", notices, want)
	}
	requireRow(t, h, active, StatusCompleted, "", 0.9)
	h.stop(t)
	if extra := h.drainNotices(); len(extra) != 0 || provider.polls.Load() != 1 || provider.posts.Load() != 0 {
		t.Fatalf("extra=%+v polls=%d POSTs=%d; want one poll of the active job and nothing resubmitted",
			extra, provider.polls.Load(), provider.posts.Load())
	}
}

func TestResumeCountsTheCeilingFromTheStoredCreationTime(t *testing.T) {
	polled := make(chan struct{}, 1)
	provider := newFakeProvider(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/videos/vid_young" {
			t.Errorf("resume polled %s", r.URL.Path)
		}
		select {
		case polled <- struct{}{}:
		default:
		}
		respondJSON(w, http.StatusOK, `{"id":"vid_young","status":"in_progress"}`)
	}, refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, func(opts *WatcherOptions) {
		*opts = WatcherOptions{PollInterval: VideoPollInterval, MaxAge: VideoJobMaxAge, MaxVideoBytes: DefaultAssetMaxVideoBytes}
	})
	old, young := h.insertJob(t, "vid_old"), h.insertJob(t, "vid_young")
	young.IdentityID = old.IdentityID
	old.CreatedAt, old.CostUSD = time.Now().Add(-VideoJobMaxAge-time.Minute), new(0.7)
	young.CreatedAt = time.Now().Add(-VideoJobMaxAge + time.Minute)
	h.store.seed(old)
	h.store.seed(young)

	if err := h.watcher.Resume(context.Background(), old.IdentityID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	old.Status = StatusExpired
	if notice := h.awaitNotice(t); notice != completionOf(old) {
		t.Fatalf("notice = %+v, want the expiry of the job created past the ceiling", notice)
	}
	requireRow(t, h, old, StatusExpired, "job_expired", 0.7)
	select {
	case <-polled:
	case <-time.After(10 * time.Second):
		t.Fatal("the job still inside its ceiling was not polled")
	}
	h.stop(t)
	if provider.polls.Load() != 1 {
		t.Fatalf("polls = %d, want only the job still inside its ceiling", provider.polls.Load())
	}
}

func TestResumeReportsTheStoreError(t *testing.T) {
	provider := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	unavailable := errors.New("connection refused")
	h.store.failNext("Recoverable", unavailable)
	if err := h.watcher.Resume(context.Background(), uuid.NewString()); !errors.Is(err, unavailable) {
		t.Fatalf("Resume = %v, want the store's error", err)
	}
}
