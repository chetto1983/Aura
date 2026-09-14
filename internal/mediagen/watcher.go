package mediagen

import (
	"context"
	"sync"
	"time"
)

// The production supervision values of spec §3: a job is polled every five seconds and
// expires thirty minutes after its row was created.
const (
	VideoPollInterval = 5 * time.Second
	VideoJobMaxAge    = 30 * time.Minute
)

// VideoAssets stores a finished job's clip as the owner's accepted video asset in the job's
// conversation, the asset Complete accepts. Ingesting the same job again returns the asset
// already stored, so a retry after a lost answer never creates a second one. A clip that
// cannot be stored as video is refused with an *Error coded unsupported, which fails the job
// instead of being retried.
type VideoAssets interface {
	IngestVideo(ctx context.Context, job Job, data []byte) (assetID string, err error)
}

// Completion tells the wake path that a job no inline waiter owns reached a terminal status.
type Completion struct {
	IdentityID, ConversationID, JobID string
	Status                            Status
}

// WatcherOptions are the watcher's clock and ceilings: the daemon passes VideoPollInterval,
// VideoJobMaxAge and the asset video byte ceiling. A nil Now reads the wall clock.
type WatcherOptions struct {
	PollInterval, MaxAge time.Duration
	MaxVideoBytes        int64
	Now                  func() time.Time
}

// Watcher supervises persisted video jobs until they reach a terminal status. It lives as
// long as the daemon context it was built from, never as long as the turn that submitted a
// job, and it only polls and downloads: nothing in it submits a generation.
type Watcher struct {
	ctx         context.Context
	cancel      context.CancelFunc
	store       JobStore
	client      *Client
	credentials MediaCredentials
	assets      VideoAssets
	notify      func(Completion)
	opts        WatcherOptions

	mu       sync.Mutex
	closed   bool
	jobs     map[string]*trackedJob
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopDone chan struct{}
}

// trackedJob is one job's entry. job and handoff are guarded by the watcher's mutex; done is
// closed once, when the job reaches its terminal row.
type trackedJob struct {
	job     Job
	handoff jobHandoff
	done    chan struct{}
}

// NewWatcher returns a watcher whose supervisors stop when parent is cancelled or Stop is
// called. notify receives each completion the wake path owns, after the watcher's lock is
// released; it runs on the goroutine that finished or released the job, Resume's included, so
// it must not block. A missing dependency or a nonpositive option is a wiring error and panics:
// a zero byte limit would fail every paid clip, a zero interval would poll without pause and a
// zero ceiling would leave every job a single attempt.
func NewWatcher(parent context.Context, store JobStore, client *Client, credentials MediaCredentials, assets VideoAssets, notify func(Completion), opts WatcherOptions) *Watcher {
	if store == nil || client == nil || credentials == nil || assets == nil || notify == nil {
		panic("mediagen: NewWatcher needs a job store, a client, credentials, video assets and a notify function")
	}
	if opts.PollInterval <= 0 || opts.MaxAge <= 0 || ValidByteLimit(opts.MaxVideoBytes) != nil {
		panic("mediagen: NewWatcher needs a positive poll interval, job ceiling and video byte limit")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	ctx, cancel := context.WithCancel(parent)
	return &Watcher{
		ctx: ctx, cancel: cancel, store: store, client: client, credentials: credentials,
		assets: assets, notify: notify, opts: opts,
		jobs: make(map[string]*trackedJob), stopDone: make(chan struct{}),
	}
}

// Track supervises a persisted job and returns a waiter for it. With inlineWaiter the waiter
// is registered as the job's owner, under the same lock, before supervision starts: whenever
// the job finishes, either that waiter claims it or the wake path is notified, exactly once,
// provided the owner calls Wait or Release. Until one of them runs, a finished job stays with
// its waiter and nobody is notified. A job already tracked keeps its one supervisor and its
// owner, and the waiter returned owns nothing. A job tracked in a terminal status is not
// polled: it is claimed by its inline waiter or notified at once.
func (w *Watcher) Track(job Job, inlineWaiter bool) *Waiter {
	waiter, fresh := w.register(job, inlineWaiter)
	switch {
	case !fresh:
	case job.Status.active():
		go w.supervise(waiter.entry, job)
	default:
		w.publish(waiter.entry, job)
	}
	return waiter
}

// register installs job's entry, and counts its supervisor, before anything can observe it.
// fresh is false for a job already tracked and on a stopped watcher.
func (w *Watcher) register(job Job, inline bool) (*Waiter, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if tracked, ok := w.jobs[job.ID]; ok {
		return &Waiter{watcher: w, entry: tracked}, false
	}
	entry := &trackedJob{job: job, handoff: jobHandoff{waiting: inline}, done: make(chan struct{})}
	waiter := &Waiter{watcher: w, entry: entry, owner: inline}
	if w.closed {
		return waiter, false
	}
	w.jobs[job.ID] = entry
	if job.Status.active() {
		w.wg.Add(1)
	}
	return waiter, true
}

// publish records the job's terminal row and closes its done channel.
func (w *Watcher) publish(entry *trackedJob, job Job) {
	w.transition(entry, func(state *jobHandoff) bool {
		entry.job = job
		close(entry.done)
		return state.finish()
	})
}

// forget drops the entry of a job whose row no longer exists, so tracking it again starts over.
func (w *Watcher) forget(entry *trackedJob) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dropLocked(entry)
}

// dropLocked removes entry from the tracked jobs unless the job was tracked again since. The
// caller holds mu.
func (w *Watcher) dropLocked(entry *trackedJob) {
	if w.jobs[entry.job.ID] == entry {
		delete(w.jobs, entry.job.ID)
	}
}

// transition runs one handoff step under the lock, drops an entry left with nothing to decide
// and notifies after unlocking. It returns the entry's job and whether a waiter holds its claim.
func (w *Watcher) transition(entry *trackedJob, step func(*jobHandoff) bool) (Job, bool) {
	w.mu.Lock()
	notify := step(&entry.handoff)
	job, claimed := entry.job, entry.handoff.claimed
	if entry.handoff.settled() {
		w.dropLocked(entry)
	}
	w.mu.Unlock()
	if notify {
		w.notify(Completion{IdentityID: job.IdentityID, ConversationID: job.ConversationID, JobID: job.ID, Status: job.Status})
	}
	return job, claimed
}

// Stop cancels every supervisor and waits for them to return, or for ctx. Rows still active
// stay active, so the next Resume picks them up.
func (w *Watcher) Stop(ctx context.Context) error {
	w.stopOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()
		w.cancel()
		go func() {
			w.wg.Wait()
			close(w.stopDone)
		}()
	})
	select {
	case <-w.stopDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Waiter is one caller's hold on a tracked job. Only the waiter Track registered as the inline
// owner can claim the job or hand it to the wake path, and it must call Wait or Release.
type Waiter struct {
	watcher  *Watcher
	entry    *trackedJob
	owner    bool
	released sync.Once
}

// Wait blocks until the job finishes, duration passes, ctx is cancelled or the watcher stops,
// then settles ownership under the watcher's lock. It returns the job's terminal row once the
// job has finished, and otherwise the row Track was given: progress is not copied back. True
// means this waiter owns the finished job's inline handling; false means the wake path owns it
// and has been notified if the job already finished. A cancelled ctx never claims: its turn can
// no longer deliver.
func (w *Waiter) Wait(ctx context.Context, duration time.Duration) (Job, bool) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-w.entry.done:
	case <-timer.C:
	case <-ctx.Done():
	case <-w.watcher.ctx.Done():
	}
	canClaim := ctx.Err() == nil
	job, claimed := w.watcher.transition(w.entry, func(state *jobHandoff) bool {
		if !w.owner || (canClaim && state.claim()) {
			return false
		}
		return state.detach()
	})
	return job, w.owner && claimed
}

// Release gives up the owner's hold; it is idempotent and a no-op for any other waiter. A job
// still running is left to the wake path. A claimed completion goes back to the wake path, once,
// unless its delivery claim committed in the store. On a stopped watcher it is not woken: the row
// stays completed and undelivered, and the next Resume wakes it. A claimed failure was already
// reported by the inline turn and stays acknowledged.
func (w *Waiter) Release() {
	if w.owner {
		w.released.Do(func() { w.watcher.release(w.entry) })
	}
}

func (w *Watcher) release(entry *trackedJob) {
	job, claimed := w.transition(entry, func(*jobHandoff) bool { return false })
	if claimed && (job.Status != StatusCompleted || w.deliveryClaimed(job) || w.ctx.Err() != nil) {
		return
	}
	w.transition(entry, (*jobHandoff).release)
}

// deliveryCheckTimeout bounds the store read a Release makes on the releasing turn's goroutine.
const deliveryCheckTimeout = 10 * time.Second

// deliveryClaimed reports whether the job's delivery claim committed. An unreadable row counts
// as unclaimed: a wake that finds the job delivered answers already_delivered, while a wake
// never sent would leave the clip uncollected until the next restart.
func (w *Watcher) deliveryClaimed(job Job) bool {
	ctx, cancel := context.WithTimeout(w.ctx, deliveryCheckTimeout)
	defer cancel()
	row, err := w.store.Get(ctx, job.IdentityID, job.ID)
	return err == nil && row.DeliveredAt != nil
}
