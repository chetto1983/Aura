package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
)

const pendingDeliveryPayloadKey = "pending_delivery"

// deliveryRetryAllowance is how many attempts past the worker cap a staged delivery may
// spend before it is abandoned.
//
// A delivery retry is deliberately claimable beyond max_attempts (delegation_queue.go says
// why: losing a completed worker's report to one transient blip would be the worse answer).
// Until 2026-09-06 that meant NO ceiling at all, and a PERMANENT delivery failure spun
// forever. Measured live that day on a first operator whose object-store leg had been
// skipped: both workers finished ok, archiving their report failed with "resolve <uuid>: no
// rows in result set" — an error no amount of waiting fixes — and the rows sat at
// attempt_count 58 against max_attempts 3, still `queued`, re-claimed every 15 seconds with
// no end. swarm_status faithfully reported that state, so the model was told two finished
// workers were still queued.
//
// Twelve attempts at the 15s backoff is about three minutes of retrying — long enough to
// outlast a restart of the store or the object store, short enough to be a bounded cost.
const deliveryRetryAllowance = 12

type delegationPendingDelivery struct {
	DeliveryKey    string      `json:"delivery_key"`
	Report         ChildReport `json:"report"`
	ElapsedSeconds int64       `json:"elapsed_seconds"`
	TargetStatus   string      `json:"target_status"`
	ErrorCode      string      `json:"error_code,omitempty"`
	ErrorMessage   string      `json:"error_message,omitempty"`
	EventType      string      `json:"event_type"`
	EventMessage   string      `json:"event_message"`
	ArtifactName   string      `json:"artifact_name,omitempty"`
}

func pendingDeliveryFromJob(job documents.IngestionJob) (*delegationPendingDelivery, error) {
	raw, ok := job.Payload[pendingDeliveryPayloadKey]
	if !ok || raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("delegation pending delivery encode: %w", err)
	}
	var pending delegationPendingDelivery
	if err := json.Unmarshal(b, &pending); err != nil {
		return nil, fmt.Errorf("delegation pending delivery decode: %w", err)
	}
	if pending.DeliveryKey == "" || pending.Report.ChildID == "" {
		return nil, fmt.Errorf("delegation pending delivery is incomplete")
	}
	if pending.DeliveryKey != job.ID+":terminal" {
		return nil, fmt.Errorf("delegation pending delivery key does not match job %s", job.ID)
	}
	if pending.TargetStatus != "succeeded" && pending.TargetStatus != "dead_letter" {
		return nil, fmt.Errorf("delegation pending delivery has invalid target %q", pending.TargetStatus)
	}
	return &pending, nil
}

func (l *DelegationClaimLoop) stageTerminalDelivery(ctx context.Context, job documents.IngestionJob, payload DelegationPayload, report ChildReport, targetStatus, errorCode, errorMessage, eventType, eventMessage string) (*delegationPendingDelivery, error) {
	pending := &delegationPendingDelivery{
		DeliveryKey: job.ID + ":terminal", Report: report,
		ElapsedSeconds: int64(time.Since(job.CreatedAt) / time.Second),
		TargetStatus:   targetStatus, ErrorCode: errorCode, ErrorMessage: errorMessage,
		EventType: eventType, EventMessage: eventMessage,
	}
	if err := l.persistPendingDelivery(ctx, job, payload, pending); err != nil {
		return nil, err
	}
	return pending, nil
}

func (l *DelegationClaimLoop) persistPendingDelivery(ctx context.Context, job documents.IngestionJob, payload DelegationPayload, pending *delegationPendingDelivery) error {
	payloadMap, err := delegationPayloadMap(payload)
	if err != nil {
		return fmt.Errorf("delegation terminal payload: %w", err)
	}
	payloadMap[pendingDeliveryPayloadKey] = pending
	_, err = l.Store.StageDelegationDelivery(ctx, documents.StageDelegationDeliveryRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
		LeaseGeneration: job.LeaseGeneration, Payload: payloadMap,
	})
	if err != nil {
		return fmt.Errorf("stage delegation terminal delivery: %w", err)
	}
	return nil
}

func (l *DelegationClaimLoop) deliverPending(ctx context.Context, job documents.IngestionJob, payload DelegationPayload, pending *delegationPendingDelivery) error {
	if l.Delivery == nil {
		return l.retryPendingDelivery(ctx, job, pending, fmt.Errorf("delegation delivery is not configured"))
	}
	if pending.ArtifactName == "" {
		artifactName, err := archivePreparedReport(ctx, l.Delivery.Archiver, identityctx.IdentityID(ctx),
			payload.ConversationID, pending.DeliveryKey, pending.Report.ChildID,
			DelegationReportMarkdown(pending.Report, time.Duration(pending.ElapsedSeconds)*time.Second))
		if err != nil {
			return l.retryPendingDelivery(ctx, job, pending, err)
		}
		pending.ArtifactName = artifactName
		if err := l.persistPendingDelivery(ctx, job, payload, pending); err != nil {
			return l.retryPendingDelivery(ctx, job, pending, fmt.Errorf("checkpoint delegation report archive: %w", err))
		}
	}

	recorded, err := l.Delivery.DeliverPreparedReport(ctx, payload, pending.Report,
		time.Duration(pending.ElapsedSeconds)*time.Second, pending.ArtifactName, pending.DeliveryKey)
	if err != nil || !recorded {
		if err == nil {
			err = fmt.Errorf("delegation report was not recorded to its origin conversation %s", payload.ConversationID)
		}
		return l.retryPendingDelivery(ctx, job, pending, err)
	}
	if _, err := l.Store.UpdateStatus(ctx, documents.TransitionIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
		LeaseGeneration: job.LeaseGeneration, Status: pending.TargetStatus,
		ErrorCode: pending.ErrorCode, ErrorMessage: pending.ErrorMessage,
		EventType: pending.EventType, EventMessage: pending.EventMessage,
	}); err != nil {
		return l.retryPendingDelivery(ctx, job, pending, fmt.Errorf("delegation terminal transition: %w", err))
	}
	return nil
}

// retryPendingDelivery schedules another delivery attempt, or abandons the delivery once
// the allowance is spent.
//
// Abandoning transitions the row to the status the delivery was FOR — a worker that
// succeeded is recorded as succeeded — carrying the delivery error, because "the worker
// failed" and "the worker succeeded and its report could not be delivered" are different
// facts and only the second one is true here. The transition is direct: routing it back
// through deliverPending would re-enter the failure that caused the abandonment.
func (l *DelegationClaimLoop) retryPendingDelivery(ctx context.Context, job documents.IngestionJob, pending *delegationPendingDelivery, cause error) error {
	if l.deliveryAllowanceSpent(job) {
		return l.abandonPendingDelivery(ctx, job, pending, cause)
	}
	if _, err := l.Store.Retry(ctx, documents.RetryIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
		LeaseGeneration: job.LeaseGeneration, ErrorCode: "delivery_failed",
		ErrorMessage: cause.Error(), EventMessage: cause.Error(),
		NextAttemptAt: time.Now().UTC().Add(l.retryBackoff()),
	}); err != nil {
		return fmt.Errorf("schedule delegation delivery retry: %w", err)
	}
	return nil
}

// deliveryAllowanceSpent reports whether this row has used its delivery retries. A job with
// no cap at all (MaxAttempts <= 0) keeps the unbounded behavior it asked for.
func (l *DelegationClaimLoop) deliveryAllowanceSpent(job documents.IngestionJob) bool {
	if job.MaxAttempts <= 0 {
		return false
	}
	return job.AttemptCount >= job.MaxAttempts+deliveryRetryAllowance
}

// abandonPendingDelivery ends a delivery that cannot be completed, loudly.
func (l *DelegationClaimLoop) abandonPendingDelivery(ctx context.Context, job documents.IngestionJob, pending *delegationPendingDelivery, cause error) error {
	status, message := "dead_letter", cause.Error()
	if pending != nil {
		status = pending.TargetStatus
		message = fmt.Sprintf("delegation report could not be delivered after %d attempts: %s",
			job.AttemptCount, cause)
	}
	slog.Error("swarm.delegation.delivery_abandoned",
		"job_id", job.ID, "attempts", job.AttemptCount, "status", status, "err", cause)
	if _, err := l.Store.UpdateStatus(ctx, documents.TransitionIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
		LeaseGeneration: job.LeaseGeneration, Status: status,
		ErrorCode: "delivery_failed", ErrorMessage: message,
		EventType: "swarm_delegation.delivery_abandoned", EventMessage: message,
	}); err != nil {
		return fmt.Errorf("abandon delegation delivery: %w", err)
	}
	return nil
}
