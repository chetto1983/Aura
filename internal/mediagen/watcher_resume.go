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
// again until the ceiling counted from its stored creation time, and a completed job never
// delivered notifies its conversation once. Nothing is submitted again.
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

type outcome int

const (
	keepPolling outcome = iota
	jobFinished
	jobVanished
)

// supervision is a supervisor's state between attempts. A fresh submission and a restart start
// it the same way, from the persisted row.
type supervision struct {
	job Job
	// origin is empty when the row records none: no base URL matches it, so the job is never
	// polled and expires at its ceiling.
	origin   string
	deadline time.Time
	// cost is the latest cost the provider reported, kept across polls that report none.
	cost *float64
	// remoteCompleted is kept while the clip download is retried, so completion is not polled
	// again; assetID is kept while Complete is retried, so the clip is not ingested again.
	remoteCompleted bool
	assetID         string
}

func newSupervision(job Job, maxAge time.Duration) supervision {
	audit, _ := job.Audit()
	return supervision{job: job, origin: audit.Origin, deadline: job.CreatedAt.Add(maxAge), cost: job.CostUSD}
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
			return
		case <-timer.C:
		}
		result, err := w.attempt(&run)
		switch {
		case result == jobFinished:
			w.publish(entry, run.job)
			return
		case result == jobVanished:
			w.forget(entry)
			return
		case err != nil && w.ctx.Err() == nil:
			slog.Warn("mediagen: video job not advanced; retrying",
				"job", run.job.ID, "owner", run.job.IdentityID, "err", redact.String(err.Error()))
		}
		timer.Reset(w.opts.PollInterval)
	}
}

// attempt advances the job by one step. Every call it makes is bounded by the watcher's
// context and the job's remaining lifetime; once that lifetime is spent only the expiry is
// written, and no provider is called.
func (w *Watcher) attempt(run *supervision) (outcome, error) {
	remaining := run.deadline.Sub(w.opts.Now())
	if remaining <= 0 {
		row, err := w.store.Progress(w.ctx, run.job.IdentityID, run.job.ID, StatusExpired, run.cost,
			&Error{Code: "job_expired", Message: "Video generation did not finish in time."})
		return w.record(w.ctx, run, row, err)
	}
	ctx, cancel := context.WithTimeout(w.ctx, remaining)
	defer cancel()
	baseURL, apiKey, err := w.endpoint(ctx, run)
	if err != nil {
		return keepPolling, err
	}
	if !run.remoteCompleted {
		remote, err := w.client.GetVideo(ctx, baseURL, apiKey, run.job.ProviderJobID)
		if err != nil {
			return keepPolling, err
		}
		if remote.CostUSD != nil {
			run.cost = remote.CostUSD
		}
		switch remote.Status {
		case StatusPending, StatusInProgress:
			if !run.unrecorded(remote.Status) {
				return keepPolling, nil
			}
			return w.progress(ctx, run, remote.Status, nil)
		case StatusFailed, StatusExpired, StatusCancelled:
			return w.progress(ctx, run, remote.Status, remote.Error)
		case StatusCompleted:
			run.remoteCompleted = true
		default:
			return keepPolling, fmt.Errorf("mediagen: the provider reported an unknown video status %q", remote.Status)
		}
	}
	return w.collect(ctx, run, baseURL, apiKey)
}

// endpoint resolves the owner's credential afresh and returns it only for the origin the job
// was submitted to.
func (w *Watcher) endpoint(ctx context.Context, run *supervision) (string, string, error) {
	baseURL, apiKey, err := w.credentials.For(ctx, run.job.IdentityID)
	if err != nil {
		return "", "", err
	}
	if origin, err := SubmissionOrigin(baseURL); err != nil || origin != run.origin {
		return "", "", errOriginChanged
	}
	return baseURL, apiKey, nil
}

// collect turns a remote completion into an Aura asset and completes the job with it. A clip
// no retry can store fails the job with its cost; any other failure is retried on the same
// provider job.
func (w *Watcher) collect(ctx context.Context, run *supervision, baseURL, apiKey string) (outcome, error) {
	if run.assetID == "" {
		assetID, err := w.ingest(ctx, run, baseURL, apiKey)
		if failure, ok := errors.AsType[*Error](err); ok && (failure.Code == "too_large" || failure.Code == "unsupported") {
			return w.progress(ctx, run, StatusFailed, failure)
		}
		if err != nil {
			return keepPolling, err
		}
		run.assetID = assetID
	}
	row, err := w.store.Complete(ctx, run.job.IdentityID, run.job.ID, run.assetID, run.cost)
	return w.record(ctx, run, row, err)
}

func (w *Watcher) ingest(ctx context.Context, run *supervision, baseURL, apiKey string) (string, error) {
	data, err := w.client.DownloadVideo(ctx, baseURL, apiKey, run.job.ProviderJobID, w.opts.MaxVideoBytes)
	if err != nil {
		return "", err
	}
	return w.assets.IngestVideo(ctx, run.job, data)
}

func (w *Watcher) progress(ctx context.Context, run *supervision, status Status, failure *Error) (outcome, error) {
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
