package mediagen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

func (p *fakeProvider) requestsTo(path string) int {
	count := 0
	for _, request := range p.recorded() {
		if strings.Fields(request)[1] == path {
			count++
		}
	}
	return count
}

// captureLogs sends the default logger to a buffer for the test; read it once no supervisor runs.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
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
				w.Header().Set("Content-Length", "1048576")
				_, _ = w.Write(mp4Clip[:8])
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
		t.Fatalf("polls=%d downloads=%d POSTs=%d; want the completion kept across 3 watcher downloads and no submission",
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

// spendLifetime wraps a provider handler so that answering it moves the clock to the ceiling.
func spendLifetime(h **watcherHarness, handler providerHandler) providerHandler {
	return func(n int32, w http.ResponseWriter, r *http.Request) {
		(*h).clock.advance(VideoJobMaxAge)
		handler(n, w, r)
	}
}

func TestWatcherExpiresAfterOneFinalAttemptWithTheLastKnownCost(t *testing.T) {
	cases := map[string]struct {
		poll, download   func(h **watcherHarness) providerHandler
		polls, downloads int32
		progressWrites   int
	}{
		"while the job is still running": {
			poll: func(h **watcherHarness) providerHandler {
				return spendLifetime(h, statuses(`{"id":"vid_slow","status":"in_progress","usage":{"cost":0.2}}`))
			},
			// The in_progress write, a failed expiry, and the expiry retried without a provider call.
			polls: 2, progressWrites: 3,
		},
		"while the clip download is retried": {
			poll: func(**watcherHarness) providerHandler {
				return statuses(`{"id":"vid_slow","status":"completed","usage":{"cost":0.2}}`)
			},
			download: func(h **watcherHarness) providerHandler {
				return spendLifetime(h, func(_ int32, w http.ResponseWriter, _ *http.Request) {
					respondJSON(w, http.StatusBadGateway, `{"error":{"code":502,"message":"Bad gateway"}}`)
				})
			},
			polls: 1, downloads: 2, progressWrites: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var h *watcherHarness
			download := refuseRequest(t)
			if tc.download != nil {
				download = tc.download(&h)
			}
			provider := newFakeProvider(t, tc.poll(&h), download)
			h = newWatcherHarness(t, context.Background(), provider, nil)
			if tc.download == nil {
				h.store.failNext("Progress", nil, errors.New("connection reset by peer"))
			}
			job := h.insertJob(t, "vid_slow")

			finished := h.awaitTerminal(t, job)
			requireRow(t, h, job, StatusExpired, "job_expired", 0.2)
			if provider.polls.Load() != tc.polls || provider.downloads.Load() != tc.downloads || h.store.count("Progress") != tc.progressWrites {
				t.Fatalf("polls=%d downloads=%d Progress=%d; want %d, %d and %d: one final provider call, then only the expiry",
					provider.polls.Load(), provider.downloads.Load(), h.store.count("Progress"), tc.polls, tc.downloads, tc.progressWrites)
			}
			requireOneNotice(t, h, completionOf(finished))
		})
	}
}

func TestWatcherCollectsTheClipOnItsFinalAttempt(t *testing.T) {
	cases := map[string]struct {
		download          func(h **watcherHarness) providerHandler
		failComplete      bool
		downloads         int32
		completes, grants int
	}{
		"a completion whose download failed at the ceiling": {
			download: func(h **watcherHarness) providerHandler {
				return func(n int32, w http.ResponseWriter, r *http.Request) {
					if n == 1 {
						spendLifetime(h, func(_ int32, w http.ResponseWriter, _ *http.Request) {
							respondJSON(w, http.StatusBadGateway, `{"error":{"code":502,"message":"Bad gateway"}}`)
						})(n, w, r)
						return
					}
					serveClip(n, w, r)
				}
			},
			downloads: 2, completes: 1, grants: 2,
		},
		"an ingested clip whose Complete failed at the ceiling": {
			download:     func(h **watcherHarness) providerHandler { return spendLifetime(h, serveClip) },
			failComplete: true,
			downloads:    1, completes: 2, grants: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var h *watcherHarness
			provider := newFakeProvider(t, statuses(`{"id":"vid_edge","status":"completed","usage":{"cost":0.45}}`), tc.download(&h))
			h = newWatcherHarness(t, context.Background(), provider, nil)
			if tc.failComplete {
				h.store.failNext("Complete", errors.New("connection reset by peer"))
			}
			job := h.insertJob(t, "vid_edge")

			finished := h.awaitTerminal(t, job)
			requireRow(t, h, job, StatusCompleted, "", 0.45)
			h.credentials.mu.Lock()
			grants := h.credentials.calls
			h.credentials.mu.Unlock()
			if provider.polls.Load() != 1 || provider.downloads.Load() != tc.downloads || h.store.count("Complete") != tc.completes || grants != tc.grants {
				t.Fatalf("polls=%d downloads=%d completes=%d credentials=%d; want 1, %d, %d and %d",
					provider.polls.Load(), provider.downloads.Load(), h.store.count("Complete"), grants, tc.downloads, tc.completes, tc.grants)
			}
			if d := h.store.deadline("Complete"); d.IsZero() || time.Until(d) > videoFinalAttemptGrace {
				t.Fatalf("the final Complete had deadline %v, want one within the final attempt's grace", d)
			}
			requireOneNotice(t, h, completionOf(finished))
		})
	}
}

// TestWatcherRetriesAKnownFinalOutcome fails the one store write of a final attempt that already
// knows the outcome: that write is retried as it is, never asked of the provider again and
// never replaced by an expiry.
func TestWatcherRetriesAKnownFinalOutcome(t *testing.T) {
	oversized := func(_ int32, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2097152")
		w.WriteHeader(http.StatusOK)
		http.NewResponseController(w).Flush()
		<-r.Context().Done()
	}
	cases := map[string]struct {
		remote, write, code string
		download            providerHandler
		want                Status
		downloads           int32
	}{
		"a clip the final attempt ingested": {"completed", "Complete", "", serveClip, StatusCompleted, 1},
		"a failure the provider reported":   {"failed", "Progress", "job_failed", nil, StatusFailed, 0},
		"a clip above the byte ceiling":     {"completed", "Progress", "too_large", oversized, StatusFailed, 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			download := tc.download
			if download == nil {
				download = refuseRequest(t)
			}
			provider := newFakeProvider(t, statuses(`{"id":"vid_known","status":"`+tc.remote+`","usage":{"cost":0.55}}`), download)
			h := newWatcherHarness(t, context.Background(), provider, nil)
			job := h.insertJob(t, "vid_known")
			job.CreatedAt = h.clock.now().Add(-VideoJobMaxAge - time.Hour)
			h.store.seed(job)
			h.store.failNext(tc.write, errors.New("connection reset by peer"))

			finished := h.awaitTerminal(t, job)
			requireRow(t, h, job, tc.want, tc.code, 0.55)
			if provider.polls.Load() != 1 || provider.downloads.Load() != tc.downloads || h.store.count(tc.write) != 2 {
				t.Fatalf("polls=%d downloads=%d %s=%d; want 1, %d and 2: the known write retried without the provider",
					provider.polls.Load(), provider.downloads.Load(), tc.write, h.store.count(tc.write), tc.downloads)
			}
			if tc.write == "Complete" && h.store.count("Progress") != 0 {
				t.Fatal("an expiry was written for a clip already ingested")
			}
			requireOneNotice(t, h, completionOf(finished))
		})
	}
}

func TestWatcherStopAbortsTheFinalAttempt(t *testing.T) {
	polled := make(chan struct{}, 1)
	provider := newFakeProvider(t, func(_ int32, _ http.ResponseWriter, r *http.Request) {
		select {
		case polled <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}, refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	job := h.insertJob(t, "vid_stopped")
	job.CreatedAt = h.clock.now().Add(-VideoJobMaxAge - time.Hour)
	h.store.seed(job)

	h.watcher.Track(job, false)
	select {
	case <-polled:
	case <-time.After(10 * time.Second):
		t.Fatal("the final attempt never read the job")
	}
	h.stop(t)
	if row, err := h.store.Get(context.Background(), job.IdentityID, job.ID); err != nil || row.Status != StatusPending {
		t.Fatalf("row = %+v, %v; a stopped final attempt must leave the job for the next boot", row, err)
	}
	if notices := h.drainNotices(); len(notices) != 0 || h.store.count("Progress") != 0 {
		t.Fatalf("notices=%+v Progress=%d; want nothing recorded", notices, h.store.count("Progress"))
	}
}

func TestWatcherReportsAPersistentCauseOnce(t *testing.T) {
	logs := captureLogs(t)
	attempts := make(chan struct{}, 1)
	provider := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	var clock func() time.Time
	counted := 0
	h := newWatcherHarness(t, context.Background(), provider, func(opts *WatcherOptions) {
		clock = opts.Now
		opts.Now = func() time.Time {
			if counted++; counted == 5 {
				attempts <- struct{}{}
			}
			return clock()
		}
	})
	job := h.insertJob(t, "vid_no_origin")
	job.Request = json.RawMessage(`{"model":"minimax/hailuo-3-max"}`)
	h.store.seed(job)

	h.watcher.Track(job, false)
	select {
	case <-attempts:
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor did not retry")
	}
	h.stop(t)
	lines := strings.Count(logs.String(), "job="+job.ID)
	if lines != 1 || !strings.Contains(logs.String(), "records no submission origin") || strings.Contains(logs.String(), errOriginChanged.Error()) {
		t.Fatalf("logged %d lines:\n%s\nwant the unreadable origin reported once, as itself", lines, logs.String())
	}
	if h.credentials.calls != 0 || len(provider.recorded()) != 0 {
		t.Fatal("a job with no readable origin reached the credential or the provider")
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
			if len(h.assets.ingests()) != tc.ingests || h.assets.assets() != 0 || provider.downloads.Load() != 1 {
				t.Fatalf("ingests=%d assets=%d downloads=%d; a clip no retry can store is fetched once and stored nowhere",
					len(h.assets.ingests()), h.assets.assets(), provider.downloads.Load())
			}
			requireOneNotice(t, h, completionOf(finished))
		})
	}
}

func TestWatcherNeverSendsTheJobToAnotherOrigin(t *testing.T) {
	logs := captureLogs(t)
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
	if lines := strings.Count(logs.String(), "job="+job.ID); lines != 2 {
		t.Fatalf("logged %d lines:\n%s\nwant the missing key and the changed origin, once each", lines, logs.String())
	}
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

func TestResumeGivesAJobPastItsCeilingOneFinalAttempt(t *testing.T) {
	provider := newFakeProvider(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/videos/vid_charged":
			respondJSON(w, http.StatusOK, `{"id":"vid_charged","status":"completed","usage":{"cost":0.7}}`)
		case "/videos/vid_running":
			respondJSON(w, http.StatusOK, `{"id":"vid_running","status":"in_progress","usage":{"cost":0.4}}`)
		default:
			respondJSON(w, http.StatusOK, `{"id":"vid_young","status":"in_progress"}`)
		}
	}, serveClip)
	h := newWatcherHarness(t, context.Background(), provider, nil)
	charged, running, young := h.insertJob(t, "vid_charged"), h.insertJob(t, "vid_running"), h.insertJob(t, "vid_young")
	owner := charged.IdentityID
	running.IdentityID, young.IdentityID = owner, owner
	charged.CreatedAt = h.clock.now().Add(-VideoJobMaxAge - time.Minute)
	running.CreatedAt = charged.CreatedAt
	young.CreatedAt = h.clock.now().Add(-VideoJobMaxAge + time.Minute)
	for _, job := range []Job{charged, running, young} {
		h.store.seed(job)
	}

	if err := h.watcher.Resume(context.Background(), owner); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	notices := []Completion{h.awaitNotice(t), h.awaitNotice(t)}
	charged.Status, running.Status = StatusCompleted, StatusExpired
	want := []Completion{completionOf(charged), completionOf(running)}
	byJob := func(a, b Completion) int { return strings.Compare(a.JobID, b.JobID) }
	slices.SortFunc(notices, byJob)
	slices.SortFunc(want, byJob)
	if !slices.Equal(notices, want) {
		t.Fatalf("notices = %+v, want %+v", notices, want)
	}
	h.stop(t)
	requireRow(t, h, charged, StatusCompleted, "", 0.7)
	requireRow(t, h, running, StatusExpired, "job_expired", 0.4)
	if row, err := h.store.Get(context.Background(), owner, young.ID); err != nil || !row.Status.active() {
		t.Fatalf("young row = %+v, %v; a job inside its ceiling keeps running", row, err)
	}
	reads := []int{provider.requestsTo("/videos/vid_charged"), provider.requestsTo("/videos/vid_charged/content"), provider.requestsTo("/videos/vid_running")}
	if !slices.Equal(reads, []int{1, 1, 1}) || provider.posts.Load() != 0 {
		t.Fatalf("charged reads, charged downloads, running reads = %v, POSTs = %d; want exactly one each and no submission",
			reads, provider.posts.Load())
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
