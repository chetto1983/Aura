package mediagen

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/redact"
)

// Resume rejoins the owner's recoverable jobs after a restart: an active job is supervised
// again until the ceiling counted from its stored creation time, then given its final attempt,
// and a completed job never delivered notifies its conversation once. Nothing is submitted
// again. Call it once per owner, before any tool can Track that owner's jobs.
func (w *Watcher) Resume(ctx context.Context, ownerID string) error {
	jobs, err := w.store.Recoverable(ctx, ownerID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		w.Track(job, false)
	}
	return nil
}

// errOriginChanged keeps a job ID away from a base URL it was not submitted to.
var errOriginChanged = errors.New("mediagen: the configured base URL is not the origin this video job was submitted to")

// videoFinalAttemptGrace bounds each call of the one attempt a job gets once its ceiling has
// passed: enough to fetch or store a 50 MiB clip at under half a MiB per second. It is not
// counted from the job's age, so a clip the provider finished and charged is still collected
// after a restart that outlasted the ceiling.
const videoFinalAttemptGrace = 2 * time.Minute

type outcome int

const (
	keepPolling outcome = iota
	jobFinished
	jobVanished
)

// callBound gives one provider or store call its context.
type callBound func() (context.Context, context.CancelFunc)

// supervision is a supervisor's state between attempts. A fresh submission and a restart start
// it the same way, from the persisted row.
type supervision struct {
	job Job
	// origin is the recorded submission origin; originErr says why the row has none readable,
	// and such a job is never polled.
	origin    string
	originErr error
	deadline  time.Time
	// cost is the latest cost the provider reported, kept across polls that report none.
	cost *float64
	// remoteCompleted is kept while the clip download is retried, so completion is not polled
	// again; assetID is kept while Complete is retried, so the clip is not fetched again.
	remoteCompleted bool
	assetID         string
	// verdict is a terminal status already decided by the provider or by the clip, kept while
	// its write is retried so the decision is never asked for again.
	verdict *verdict
	// final is set when the ceiling has passed and the job's one final attempt starts.
	final bool
	// cause is the retry cause last logged, so a failure that persists is reported once.
	cause string
}

type verdict struct {
	status  Status
	failure *Error
}

// decided reports an outcome only a store write is missing for: an ingested clip to complete
// the job with, or a terminal verdict.
func (r *supervision) decided() bool {
	return r.assetID != "" || r.verdict != nil
}

func newSupervision(job Job, maxAge time.Duration) supervision {
	audit, err := job.Audit()
	return supervision{
		job: job, origin: audit.Origin, originErr: err, deadline: job.CreatedAt.Add(maxAge), cost: job.CostUSD,
	}
}

// supervise polls immediately and then every PollInterval until the job's row is terminal,
// the row is gone or the watcher stops.
func (w *Watcher) supervise(entry *trackedJob, job Job) {
	defer w.wg.Done()
	run := newSupervision(job, w.opts.MaxAge)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-w.ctx.Done():
		case <-timer.C:
		}
		if w.ctx.Err() != nil {
			return
		}
		result, err := w.attempt(&run)
		switch result {
		case jobFinished:
			w.publish(entry, run.job)
			return
		case jobVanished:
			w.forget(entry)
			return
		}
		w.report(&run, err)
		timer.Reset(w.opts.PollInterval)
	}
}

// attempt advances the job by one step. Before the ceiling every call shares the job's
// remaining lifetime. The first attempt past the ceiling is the job's last contact with the
// provider, each call bounded by videoFinalAttemptGrace: it can still complete an ingested clip,
// record a terminal status or collect a completed clip. An outcome it decided is written, and
// that write alone is retried, without the provider; a job it leaves undecided expires with the
// freshest known cost, and only that expiry is retried.
func (w *Watcher) attempt(run *supervision) (outcome, error) {
	if remaining := run.deadline.Sub(w.opts.Now()); remaining > 0 {
		deadline := time.Now().Add(remaining)
		return w.advance(run, func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(w.ctx, deadline)
		})
	}
	if !run.final || run.decided() {
		run.final = true
		result, err := w.advance(run, w.graceBound)
		if result != keepPolling || w.ctx.Err() != nil || run.decided() {
			return result, err
		}
		w.report(run, err)
	}
	return w.progress(run, w.graceBound, StatusExpired,
		&Error{Code: "job_expired", Message: "Video generation did not finish in time."})
}

func (w *Watcher) graceBound() (context.Context, context.CancelFunc) {
	return context.WithTimeout(w.ctx, videoFinalAttemptGrace)
}

// advance writes an outcome already decided, or reads the provider's status and acts on it.
func (w *Watcher) advance(run *supervision, bound callBound) (outcome, error) {
	switch {
	case run.assetID != "":
		return w.complete(run, bound)
	case run.verdict != nil:
		return w.progress(run, bound, run.verdict.status, run.verdict.failure)
	}
	baseURL, apiKey, err := w.endpoint(run, bound)
	if err != nil {
		return keepPolling, err
	}
	if !run.remoteCompleted {
		remote, err := w.poll(run, bound, baseURL, apiKey)
		if err != nil {
			return keepPolling, err
		}
		switch remote.Status {
		case StatusPending, StatusInProgress:
			if run.final || !run.unrecorded(remote.Status) {
				return keepPolling, nil
			}
			return w.progress(run, bound, remote.Status, nil)
		case StatusFailed, StatusExpired, StatusCancelled:
			return w.conclude(run, bound, remote.Status, remote.Error)
		case StatusCompleted:
			run.remoteCompleted = true
		default:
			return keepPolling, fmt.Errorf("mediagen: the provider reported an unknown video status %q", remote.Status)
		}
	}
	return w.collect(run, bound, baseURL, apiKey)
}

// endpoint resolves the owner's credential afresh and returns it only for the origin the job
// was submitted to.
func (w *Watcher) endpoint(run *supervision, bound callBound) (string, string, error) {
	if run.originErr != nil {
		return "", "", run.originErr
	}
	ctx, cancel := bound()
	defer cancel()
	baseURL, apiKey, err := w.credentials.For(ctx, run.job.IdentityID)
	if err != nil {
		return "", "", err
	}
	if origin, err := SubmissionOrigin(baseURL); err != nil || origin != run.origin {
		return "", "", errOriginChanged
	}
	return baseURL, apiKey, nil
}

func (w *Watcher) poll(run *supervision, bound callBound, baseURL, apiKey string) (RemoteVideo, error) {
	ctx, cancel := bound()
	defer cancel()
	remote, err := w.client.GetVideo(ctx, baseURL, apiKey, run.job.ProviderJobID)
	if err == nil && remote.CostUSD != nil {
		run.cost = remote.CostUSD
	}
	return remote, err
}

// collect turns a remote completion into an Aura asset and completes the job with it. A clip
// no retry can store fails the job with its cost; any other failure is retried on the same
// provider job.
func (w *Watcher) collect(run *supervision, bound callBound, baseURL, apiKey string) (outcome, error) {
	assetID, err := w.ingest(run, bound, baseURL, apiKey)
	if failure, ok := errors.AsType[*Error](err); ok && (failure.Code == "too_large" || failure.Code == "unsupported") {
		return w.conclude(run, bound, StatusFailed, failure)
	}
	if err != nil {
		return keepPolling, err
	}
	run.assetID = assetID
	return w.complete(run, bound)
}

// conclude keeps a terminal verdict, so a failed write of it is retried as it is, and writes it.
func (w *Watcher) conclude(run *supervision, bound callBound, status Status, failure *Error) (outcome, error) {
	run.verdict = &verdict{status: status, failure: failure}
	return w.progress(run, bound, status, failure)
}

func (w *Watcher) ingest(run *supervision, bound callBound, baseURL, apiKey string) (string, error) {
	downloadCtx, cancelDownload := bound()
	defer cancelDownload()
	data, err := w.client.DownloadVideo(downloadCtx, baseURL, apiKey, run.job.ProviderJobID, w.opts.MaxVideoBytes)
	if err != nil {
		return "", err
	}
	ingestCtx, cancelIngest := bound()
	defer cancelIngest()
	return w.assets.IngestVideo(ingestCtx, run.job, data)
}

func (w *Watcher) complete(run *supervision, bound callBound) (outcome, error) {
	ctx, cancel := bound()
	defer cancel()
	row, err := w.store.Complete(ctx, run.job.IdentityID, run.job.ID, run.assetID, run.cost)
	return w.record(ctx, run, row, err)
}

func (w *Watcher) progress(run *supervision, bound callBound, status Status, failure *Error) (outcome, error) {
	ctx, cancel := bound()
	defer cancel()
	row, err := w.store.Progress(ctx, run.job.IdentityID, run.job.ID, status, run.cost, failure)
	return w.record(ctx, run, row, err)
}

// record reads a store answer: the row written, the terminal row of a job the store refused to
// move, or a job that no longer exists.
func (w *Watcher) record(ctx context.Context, run *supervision, row Job, err error) (outcome, error) {
	if errors.Is(err, ErrJobNotActive) {
		row, err = w.store.Get(ctx, run.job.IdentityID, run.job.ID)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return jobVanished, nil
	case err != nil:
		return keepPolling, err
	}
	run.job = row
	if row.Status.active() {
		return keepPolling, nil
	}
	return jobFinished, nil
}

// unrecorded reports an active status or a cost the row does not hold yet.
func (r *supervision) unrecorded(status Status) bool {
	if status != r.job.Status {
		return true
	}
	return r.cost != nil && (r.job.CostUSD == nil || *r.cost != *r.job.CostUSD)
}

// report logs why an attempt left the job unfinished, once per cause: a failure that persists
// is not repeated every interval, and one that clears and returns is reported again.
func (w *Watcher) report(run *supervision, err error) {
	cause := ""
	if err != nil && w.ctx.Err() == nil {
		cause = redact.String(err.Error())
	}
	if cause != "" && cause != run.cause {
		slog.Warn("mediagen: video job not advanced; retrying",
			"job", run.job.ID, "owner", run.job.IdentityID, "err", cause)
	}
	run.cause = cause
}
