package swarm

import (
	"context"

	"github.com/chetto1983/aura/internal/documents"
)

type workerCancellationStore interface {
	RequestWorkerCancellation(context.Context, documents.WorkerCancellationRequest) error
}

func (l *DelegationClaimLoop) deliverCanceled(ctx context.Context, job documents.IngestionJob, payload DelegationPayload) error {
	payload.OperatorCancelled = true
	report := ChildReport{
		ChildID: payload.ChildID, GoalIndex: payload.GoalIndex, Goal: payload.Goal, Attempts: job.AttemptCount,
		Status: StatusCanceled, Summary: "Stopped by the operator. Do not retry without a new operator request.",
	}
	pending, err := l.stageTerminalDelivery(ctx, job, payload, report, "canceled", "", "", "swarm_delegation.canceled", "delegation stopped by operator")
	if err != nil {
		return err
	}
	return l.deliverPending(ctx, job, payload, pending)
}
