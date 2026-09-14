package tools

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/mediagen"
)

// fakeVideoJobs is mediagen.JobStore in memory under Store's contract: a job of another owner
// reads as pgx.ErrNoRows, only an active job moves, Complete accepts only a clip ingested for
// the job's owner and conversation, and one delivery claim wins, in the job's conversation only.
// Reads and claims answer a cancelled context as pgx does.
type fakeVideoJobs struct {
	events *timeline

	mu             sync.Mutex
	jobs           map[string]mediagen.Job
	accepted       map[string]mediagen.Job
	calls          map[string]int
	inserted       []mediagen.Job
	insertErr      error
	beforeInsert   func()
	insertCtxErr   error
	insertDeadline time.Time
	claimErr       error
	claims         []string
}

var _ mediagen.JobStore = (*fakeVideoJobs)(nil)

func newFakeVideoJobs(events *timeline) *fakeVideoJobs {
	return &fakeVideoJobs{events: events, jobs: map[string]mediagen.Job{}, accepted: map[string]mediagen.Job{}, calls: map[string]int{}}
}

func (s *fakeVideoJobs) count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[method]
}

func (s *fakeVideoJobs) job(id string) mediagen.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (s *fakeVideoJobs) seed(job mediagen.Job) mediagen.Job {
	job.ID = uuid.NewString()
	s.put(job)
	return job
}

func (s *fakeVideoJobs) put(job mediagen.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

func (s *fakeVideoJobs) Insert(ctx context.Context, job mediagen.Job) (mediagen.Job, error) {
	if s.beforeInsert != nil {
		s.beforeInsert()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls["Insert"]++
	s.insertCtxErr = ctx.Err()
	s.insertDeadline, _ = ctx.Deadline()
	switch {
	case ctx.Err() != nil:
		return mediagen.Job{}, ctx.Err()
	case s.insertErr != nil:
		return mediagen.Job{}, s.insertErr
	case job.Status != mediagen.StatusPending && job.Status != mediagen.StatusInProgress:
		return mediagen.Job{}, errors.New("a new job must be active")
	}
	if _, err := job.Audit(); err != nil {
		return mediagen.Job{}, err
	}
	job.ID, job.CreatedAt, job.UpdatedAt = uuid.NewString(), time.Now(), time.Now()
	s.jobs[job.ID] = job
	s.inserted = append(s.inserted, job)
	s.events.add("insert")
	return job, nil
}

func (s *fakeVideoJobs) owned(ownerID, jobID string) (mediagen.Job, error) {
	job, ok := s.jobs[jobID]
	if !ok || job.IdentityID != ownerID {
		return mediagen.Job{}, pgx.ErrNoRows
	}
	return job, nil
}

func (s *fakeVideoJobs) Get(ctx context.Context, ownerID, jobID string) (mediagen.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls["Get"]++
	if err := ctx.Err(); err != nil {
		return mediagen.Job{}, err
	}
	return s.owned(ownerID, jobID)
}

func (s *fakeVideoJobs) Recoverable(context.Context, string) ([]mediagen.Job, error) {
	return nil, errors.New("video_generate never lists recoverable jobs")
}

func (s *fakeVideoJobs) update(ownerID, jobID string, change func(*mediagen.Job) error) (mediagen.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.owned(ownerID, jobID)
	if err != nil {
		return mediagen.Job{}, err
	}
	if job.Status != mediagen.StatusPending && job.Status != mediagen.StatusInProgress {
		return mediagen.Job{}, mediagen.ErrJobNotActive
	}
	if err := change(&job); err != nil {
		return mediagen.Job{}, err
	}
	s.jobs[job.ID] = job
	return job, nil
}

func (s *fakeVideoJobs) Progress(_ context.Context, ownerID, jobID string, status mediagen.Status, cost *float64, failure *mediagen.Error) (mediagen.Job, error) {
	return s.update(ownerID, jobID, func(job *mediagen.Job) error {
		job.Status, job.Error = status, failure
		if cost != nil {
			job.CostUSD = cost
		}
		return nil
	})
}

func (s *fakeVideoJobs) Complete(_ context.Context, ownerID, jobID, assetID string, cost *float64) (mediagen.Job, error) {
	return s.update(ownerID, jobID, func(job *mediagen.Job) error {
		ingested, ok := s.accepted[assetID]
		if !ok || ingested.IdentityID != job.IdentityID || ingested.ConversationID != job.ConversationID {
			return &mediagen.Error{Code: "asset_not_found", Message: "The generated video is not available for this job."}
		}
		job.Status, job.AssetID = mediagen.StatusCompleted, assetID
		if cost != nil {
			job.CostUSD = cost
		}
		return nil
	})
}

func (s *fakeVideoJobs) ClaimDelivery(ctx context.Context, ownerID, jobID, conversationID, deliveryCallID string) (mediagen.Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls["ClaimDelivery"]++
	if err := errors.Join(ctx.Err(), s.claimErr); err != nil {
		return mediagen.Job{}, false, err
	}
	job, err := s.owned(ownerID, jobID)
	if err != nil || job.ConversationID != conversationID {
		return mediagen.Job{}, false, pgx.ErrNoRows
	}
	if job.Status != mediagen.StatusCompleted || job.AssetID == "" || job.DeliveredAt != nil {
		return job, false, nil
	}
	job.DeliveredAt = new(time.Now())
	s.jobs[job.ID] = job
	s.claims = append(s.claims, deliveryCallID)
	return job, true, nil
}

func (s *fakeVideoJobs) accept(assetID string, job mediagen.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accepted[assetID] = job
}

func (s *fakeVideoJobs) deliveryCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.claims)
}

// fakeVideoLibrary is the asset adapter both sides of a job use: the watcher ingests a clip
// once per job, and the tool opens it again to deliver it.
type fakeVideoLibrary struct {
	jobs *fakeVideoJobs

	mu       sync.Mutex
	clips    map[string]storedClip
	bySource map[string]string
	opened   []string
}

type storedClip struct {
	owner string
	data  []byte
}

func (l *fakeVideoLibrary) IngestVideo(_ context.Context, job mediagen.Job, data []byte) (string, error) {
	l.mu.Lock()
	source := "media-job:" + job.ID
	assetID, ok := l.bySource[source]
	if !ok {
		assetID = uuid.NewString()
		l.bySource[source] = assetID
		l.clips[assetID] = storedClip{owner: job.IdentityID, data: bytes.Clone(data)}
	}
	l.mu.Unlock()
	l.jobs.accept(assetID, job)
	return assetID, nil
}

func (l *fakeVideoLibrary) Open(_ context.Context, owner, assetID string) (io.ReadCloser, mediagen.ReferenceMeta, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, assetID)
	clip, ok := l.clips[assetID]
	if !ok || clip.owner != owner {
		return nil, mediagen.ReferenceMeta{}, errors.New("asset not visible to this identity")
	}
	meta := mediagen.ReferenceMeta{MIMEType: "video/mp4", Modality: "video", SizeBytes: int64(len(clip.data))}
	return io.NopCloser(bytes.NewReader(clip.data)), meta, nil
}

func (l *fakeVideoLibrary) store(owner string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	assetID := uuid.NewString()
	l.clips[assetID] = storedClip{owner: owner, data: bytes.Clone(generatedClip)}
	return assetID
}

func (l *fakeVideoLibrary) remove(assetID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.clips, assetID)
}

func (l *fakeVideoLibrary) assetOf(jobID string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bySource["media-job:"+jobID]
}

func (l *fakeVideoLibrary) opens() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.opened)
}
