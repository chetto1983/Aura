package agui

import (
	"context"
	"net/http"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/google/uuid"
)

type workerControlJobStore interface {
	FindDelegationJob(context.Context, string, string, string) (documents.DelegationJobRow, bool, error)
	RequestQueuedWorkerCancellation(context.Context, documents.QueuedWorkerCancellationRequest) error
}

type queuedWorkerTarget struct {
	JobID        string `json:"job_id"`
	AttemptCount int    `json:"attempt_count"`
}

// SetWorkerControlJobs exposes durable pre-start controls when no RunSession exists yet.
func (s *Server) SetWorkerControlJobs(store workerControlJobStore) { s.workerJobs = store }

func (s *Server) workerControlJob(r *http.Request) (documents.DelegationJobRow, bool, error) {
	if s.workerJobs == nil {
		return documents.DelegationJobRow{}, false, nil
	}
	return s.workerJobs.FindDelegationJob(r.Context(), scopedIdentityID(r.Context()), r.PathValue("conv"), r.PathValue("child"))
}

func (s *Server) cancelQueuedWorker(w http.ResponseWriter, r *http.Request, req workerControlRequest) {
	if _, err := uuid.Parse(req.JobID); err != nil || req.RunID != "" || req.AttemptCount == nil || *req.AttemptCount < 0 || int64(*req.AttemptCount) > 1<<31-1 {
		http.Error(w, "invalid queued target", http.StatusBadRequest)
		return
	}
	job, found, err := s.workerControlJob(r)
	if err != nil {
		http.Error(w, "worker controls unavailable", http.StatusServiceUnavailable)
		return
	}
	if !found || job.ID != req.JobID {
		workerControlNotFound(w)
		return
	}
	err = s.workerJobs.RequestQueuedWorkerCancellation(r.Context(), documents.QueuedWorkerCancellationRequest{
		IdentityID: scopedIdentityID(r.Context()), ConversationID: r.PathValue("conv"),
		ChildID: r.PathValue("child"), JobID: req.JobID, AttemptCount: *req.AttemptCount,
	})
	if err != nil {
		writeWorkerControlError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
}
