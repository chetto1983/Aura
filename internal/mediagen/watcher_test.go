package mediagen

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// mp4Clip starts with the ftyp box http.DetectContentType recognizes as video/mp4.
var mp4Clip = []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41\x00\x00\x00\x08free")

// fakeJobStore is JobStore in memory under Store's contract: a job of another identity reads
// as pgx.ErrNoRows, a terminal job refuses every update with ErrJobNotActive, Complete accepts
// only a video ingested for the job's owner and conversation, and one delivery claim wins.
// Rows are copied in and out; calls are counted and can be failed one at a time.
type fakeJobStore struct {
	mu     sync.Mutex
	now    func() time.Time
	jobs   map[string]Job
	videos map[string]Job
	fail   map[string][]error
	calls  map[string]int
}

func newFakeJobStore(now func() time.Time) *fakeJobStore {
	return &fakeJobStore{now: now, jobs: map[string]Job{}, videos: map[string]Job{}, fail: map[string][]error{}, calls: map[string]int{}}
}

var _ JobStore = (*fakeJobStore)(nil)

func cloneJob(job Job) Job {
	job.Request = slices.Clone(job.Request)
	job.CostUSD = clonePointer(job.CostUSD)
	job.Error = clonePointer(job.Error)
	job.CompletedAt = clonePointer(job.CompletedAt)
	job.DeliveredAt = clonePointer(job.DeliveredAt)
	return job
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// called counts method and returns the next failure queued for it. The caller holds mu.
func (s *fakeJobStore) called(method string) error {
	s.calls[method]++
	queue := s.fail[method]
	if len(queue) == 0 {
		return nil
	}
	s.fail[method] = queue[1:]
	return queue[0]
}

func (s *fakeJobStore) failNext(method string, errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[method] = append(s.fail[method], errs...)
}

func (s *fakeJobStore) count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[method]
}

func (s *fakeJobStore) seed(job Job) Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	s.jobs[job.ID] = cloneJob(job)
	return cloneJob(job)
}

func (s *fakeJobStore) remove(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, jobID)
}

func (s *fakeJobStore) acceptVideo(assetID string, job Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.videos[assetID] = job
}

func (s *fakeJobStore) Insert(_ context.Context, job Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.called("Insert"); err != nil {
		return Job{}, err
	}
	if err := validateNewJob(job); err != nil {
		return Job{}, err
	}
	for _, existing := range s.jobs {
		if existing.ProviderJobID == job.ProviderJobID {
			return Job{}, errors.New("duplicate provider job id")
		}
	}
	job.ID, job.CreatedAt, job.UpdatedAt = uuid.NewString(), s.now(), s.now()
	job.Error, job.AssetID, job.CompletedAt, job.DeliveredAt = nil, "", nil, nil
	s.jobs[job.ID] = cloneJob(job)
	return cloneJob(job), nil
}

func (s *fakeJobStore) owned(ownerID, jobID string) (Job, error) {
	job, ok := s.jobs[jobID]
	if !ok || job.IdentityID != ownerID {
		return Job{}, pgx.ErrNoRows
	}
	return job, nil
}

func (s *fakeJobStore) Get(_ context.Context, ownerID, jobID string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.called("Get"); err != nil {
		return Job{}, err
	}
	job, err := s.owned(ownerID, jobID)
	return cloneJob(job), err
}

func (s *fakeJobStore) Recoverable(_ context.Context, ownerID string) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.called("Recoverable"); err != nil {
		return nil, err
	}
	var jobs []Job
	for _, job := range s.jobs {
		undelivered := job.Status == StatusCompleted && job.DeliveredAt == nil
		if job.IdentityID == ownerID && (job.Status.active() || undelivered) {
			jobs = append(jobs, cloneJob(job))
		}
	}
	slices.SortFunc(jobs, func(a, b Job) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return jobs, nil
}

// update applies change to an owned active job, the guard every store update shares.
func (s *fakeJobStore) update(method, ownerID, jobID string, change func(*Job) error) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.called(method); err != nil {
		return Job{}, err
	}
	job, err := s.owned(ownerID, jobID)
	if err != nil {
		return Job{}, err
	}
	if !job.Status.active() {
		return Job{}, ErrJobNotActive
	}
	if err := change(&job); err != nil {
		return Job{}, err
	}
	job.UpdatedAt = s.now()
	s.jobs[job.ID] = cloneJob(job)
	return cloneJob(job), nil
}

func (s *fakeJobStore) Progress(_ context.Context, ownerID, jobID string, status Status, cost *float64, failure *Error) (Job, error) {
	if status == StatusCompleted {
		return Job{}, errors.New("a job completes through Complete")
	}
	return s.update("Progress", ownerID, jobID, func(job *Job) error {
		job.Status, job.Error = status, clonePointer(failure)
		if cost != nil {
			job.CostUSD = clonePointer(cost)
		}
		if !status.active() {
			job.CompletedAt = new(s.now())
		}
		return nil
	})
}

func (s *fakeJobStore) Complete(_ context.Context, ownerID, jobID, assetID string, cost *float64) (Job, error) {
	return s.update("Complete", ownerID, jobID, func(job *Job) error {
		ingested, ok := s.videos[assetID]
		if !ok || ingested.IdentityID != job.IdentityID || ingested.ConversationID != job.ConversationID {
			return videoAssetNotFound()
		}
		job.Status, job.AssetID, job.Error = StatusCompleted, assetID, nil
		if cost != nil {
			job.CostUSD = clonePointer(cost)
		}
		job.CompletedAt = new(s.now())
		return nil
	})
}

func (s *fakeJobStore) ClaimDelivery(_ context.Context, ownerID, jobID, conversationID, deliveryCallID string) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.called("ClaimDelivery"); err != nil {
		return Job{}, false, err
	}
	if conversationID == "" || deliveryCallID == "" {
		return Job{}, false, errors.New("a delivery claim needs its conversation and tool call")
	}
	job, err := s.owned(ownerID, jobID)
	if err != nil || job.ConversationID != conversationID {
		return Job{}, false, pgx.ErrNoRows
	}
	if job.Status != StatusCompleted || job.AssetID == "" || job.DeliveredAt != nil {
		return cloneJob(job), false, nil
	}
	job.DeliveredAt = new(s.now())
	s.jobs[job.ID] = cloneJob(job)
	return cloneJob(job), true, nil
}

// fakeVideoAssets keeps one asset per job, as the adapter's stable source reference does, and
// registers it as the video Complete accepts. A queued failure is returned after the asset is
// stored: the answer of an ingest that did happen was lost.
type fakeVideoAssets struct {
	store    *fakeJobStore
	mu       sync.Mutex
	bySource map[string]string
	ingested []string
	fail     []error
}

func (a *fakeVideoAssets) IngestVideo(_ context.Context, job Job, data []byte) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ingested = append(a.ingested, job.ID)
	source := "media-job:" + job.ID
	assetID, ok := a.bySource[source]
	if !ok {
		assetID = uuid.NewString()
		a.bySource[source] = assetID
		a.store.acceptVideo(assetID, job)
	}
	if len(a.fail) > 0 {
		err := a.fail[0]
		a.fail = a.fail[1:]
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("empty clip")
	}
	return assetID, nil
}

func (a *fakeVideoAssets) ingests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.ingested)
}

// credentialAnswer is one answer of fakeCredentials. When set, entered is signalled as the call
// starts and hold blocks the answer until it is closed, whatever the caller's context.
type credentialAnswer struct {
	baseURL string
	err     error
	entered chan struct{}
	hold    chan struct{}
}

// fakeCredentials answers in order and repeats its last answer. The key names its identity, so
// a request shows whose credential it carried.
type fakeCredentials struct {
	mu      sync.Mutex
	answers []credentialAnswer
	calls   int
}

func (c *fakeCredentials) For(_ context.Context, identityID string) (string, string, error) {
	c.mu.Lock()
	c.calls++
	answer := c.answers[0]
	if len(c.answers) > 1 {
		c.answers = c.answers[1:]
	}
	c.mu.Unlock()
	if answer.entered != nil {
		select {
		case answer.entered <- struct{}{}:
		default:
		}
	}
	if answer.hold != nil {
		<-answer.hold
	}
	if answer.err != nil {
		return "", "", answer.err
	}
	return answer.baseURL, "key-" + identityID, nil
}

// fakeProvider is OpenRouter's video API. poll answers the nth status request and download the
// nth content request; every request is counted and its path and Authorization recorded.
type fakeProvider struct {
	*httptest.Server
	posts, polls, downloads atomic.Int32
	mu                      sync.Mutex
	requests                []string
}

type providerHandler func(n int32, w http.ResponseWriter, r *http.Request)

func newFakeProvider(t *testing.T, poll, download providerHandler) *fakeProvider {
	t.Helper()
	p := &fakeProvider{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests = append(p.requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		p.mu.Unlock()
		switch {
		case r.Method != http.MethodGet:
			p.posts.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
		case strings.HasSuffix(r.URL.Path, "/content"):
			download(p.downloads.Add(1), w, r)
		default:
			poll(p.polls.Add(1), w, r)
		}
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *fakeProvider) recorded() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.requests)
}

func respondJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// statuses answers the nth poll with the nth body and repeats the last one.
func statuses(bodies ...string) providerHandler {
	return func(n int32, w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, http.StatusOK, bodies[min(int(n), len(bodies))-1])
	}
}

func serveClip(_ int32, w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "video/mp4")
	_, _ = w.Write(mp4Clip)
}

func refuseRequest(t *testing.T) providerHandler {
	return func(n int32, w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected provider request %d: %s %s", n, r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}
}

type watcherHarness struct {
	provider    *fakeProvider
	clock       *fakeClock
	store       *fakeJobStore
	assets      *fakeVideoAssets
	credentials *fakeCredentials
	notices     chan Completion
	watcher     *Watcher
}

// newWatcherHarness builds a watcher over the fakes with a one-millisecond poll interval, the
// production ceiling on a fake clock and a one-MiB clip ceiling; adjust changes the options.
func newWatcherHarness(t *testing.T, parent context.Context, provider *fakeProvider, adjust func(*WatcherOptions)) *watcherHarness {
	t.Helper()
	clock := &fakeClock{at: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}
	store := newFakeJobStore(clock.now)
	h := &watcherHarness{
		provider: provider, clock: clock, store: store,
		assets:      &fakeVideoAssets{store: store, bySource: map[string]string{}},
		credentials: &fakeCredentials{answers: []credentialAnswer{{baseURL: provider.URL}}},
		notices:     make(chan Completion, 16),
	}
	opts := WatcherOptions{PollInterval: time.Millisecond, MaxAge: VideoJobMaxAge, MaxVideoBytes: 1 << 20, Now: clock.now}
	if adjust != nil {
		adjust(&opts)
	}
	h.watcher = NewWatcher(parent, store, NewClient(provider.Client(), 1<<20), h.credentials, h.assets,
		func(completion Completion) { h.notices <- completion }, opts)
	t.Cleanup(func() { h.stop(t) })
	return h
}

func (h *watcherHarness) insertJob(t *testing.T, providerID string) Job {
	t.Helper()
	request, err := JobRequest(VideoRequest{Model: DefaultVideoModel, Prompt: "the sea at dawn", Duration: 6},
		JobAudit{Origin: h.provider.URL})
	if err != nil {
		t.Fatal(err)
	}
	job, err := h.store.Insert(context.Background(), Job{
		IdentityID: uuid.NewString(), ConversationID: "thread-" + providerID, ToolCallID: "call-submit",
		ProviderJobID: providerID, Model: DefaultVideoModel, Request: request, Status: StatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func (h *watcherHarness) stop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.watcher.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func (h *watcherHarness) awaitNotice(t *testing.T) Completion {
	t.Helper()
	select {
	case notice := <-h.notices:
		return notice
	case <-time.After(10 * time.Second):
		t.Fatal("no completion was notified")
		return Completion{}
	}
}

// drainNotices returns every notification sent so far; call it after stop, once no
// supervisor can send another.
func (h *watcherHarness) drainNotices() []Completion {
	var notices []Completion
	for {
		select {
		case notice := <-h.notices:
			notices = append(notices, notice)
		default:
			return notices
		}
	}
}

// awaitTerminal tracks job without an inline waiter and waits for its terminal row.
func (h *watcherHarness) awaitTerminal(t *testing.T, job Job) Job {
	t.Helper()
	finished, owns := h.watcher.Track(job, false).Wait(context.Background(), 10*time.Second)
	if owns || finished.Status.active() {
		t.Fatalf("job did not finish without an owner: %+v owns=%v", finished, owns)
	}
	return finished
}

// awaitSupervisorsExit waits for every supervisor to return on its own, without Stop.
func awaitSupervisorsExit(t *testing.T, w *Watcher) {
	t.Helper()
	exited := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("a supervisor kept running")
	}
}

func trackedJobs(w *Watcher) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.jobs)
}

func completionOf(job Job) Completion {
	return Completion{IdentityID: job.IdentityID, ConversationID: job.ConversationID, JobID: job.ID, Status: job.Status}
}

func TestWatcherStopsWithTheDaemonAndLeavesTheJobRecoverable(t *testing.T) {
	polled := make(chan struct{}, 1)
	provider := newFakeProvider(t, func(_ int32, _ http.ResponseWriter, r *http.Request) {
		select {
		case polled <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}, refuseRequest(t))
	daemon, stopDaemon := context.WithCancel(context.Background())
	h := newWatcherHarness(t, daemon, provider, nil)
	job := h.insertJob(t, "vid_daemon")
	waiter := h.watcher.Track(job, true)
	select {
	case <-polled:
	case <-time.After(10 * time.Second):
		t.Fatal("the job was never polled")
	}

	stopDaemon()
	if _, owns := waiter.Wait(context.Background(), time.Minute); owns {
		t.Fatal("a stopped watcher handed out a job")
	}
	awaitSupervisorsExit(t, h.watcher)
	recoverable, err := h.store.Recoverable(context.Background(), job.IdentityID)
	if err != nil || len(recoverable) != 1 || recoverable[0].ID != job.ID || recoverable[0].Status != StatusPending {
		t.Fatalf("Recoverable = %+v, %v; want the job still pending for the next boot", recoverable, err)
	}
	if notices := h.drainNotices(); len(notices) != 0 || provider.polls.Load() != 1 {
		t.Fatalf("notices=%+v polls=%d; want no notice and the one cancelled poll", notices, provider.polls.Load())
	}
}

func TestWatcherStoppedTracksNothing(t *testing.T) {
	provider := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	active := h.insertJob(t, "vid_after_stop")
	finished := h.insertJob(t, "vid_finished_after_stop")
	finished.Status, finished.AssetID = StatusCompleted, uuid.NewString()
	h.stop(t)

	if _, owns := h.watcher.Track(active, true).Wait(context.Background(), time.Minute); owns {
		t.Fatal("a stopped watcher handed out a job")
	}
	h.watcher.Track(finished, false)
	if notices := h.drainNotices(); len(notices) != 0 {
		t.Fatalf("a stopped watcher notified %+v", notices)
	}
	if err := h.watcher.Stop(context.Background()); err != nil {
		t.Fatalf("a second Stop = %v", err)
	}
}

func TestWatcherStopGivesUpWhenASupervisorOutlivesItsContext(t *testing.T) {
	provider := newFakeProvider(t, refuseRequest(t), refuseRequest(t))
	h := newWatcherHarness(t, context.Background(), provider, nil)
	entered, hold := make(chan struct{}, 1), make(chan struct{})
	h.credentials.answers = []credentialAnswer{{err: &Error{Code: "no_key", Message: "held"}, entered: entered, hold: hold}}
	h.watcher.Track(h.insertJob(t, "vid_held"), false)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor never asked for a credential")
	}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.watcher.Stop(expired); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v, want the context's error while a supervisor is still running", err)
	}
	close(hold)
}
