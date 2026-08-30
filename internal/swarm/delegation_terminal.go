package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
)

const pendingDeliveryPayloadKey = "pending_delivery"

type delegationPendingDelivery struct {
	DeliveryKey      string      `json:"delivery_key"`
	Report           ChildReport `json:"report"`
	ElapsedSeconds   int64       `json:"elapsed_seconds"`
	TargetStatus     string      `json:"target_status"`
	ErrorCode        string      `json:"error_code,omitempty"`
	ErrorMessage     string      `json:"error_message,omitempty"`
	EventType        string      `json:"event_type"`
	EventMessage     string      `json:"event_message"`
	ArchiveAttempted bool        `json:"archive_attempted,omitempty"`
	ArtifactName     string      `json:"artifact_name,omitempty"`
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
		return l.retryPendingDelivery(ctx, job, fmt.Errorf("delegation delivery is not configured"))
	}
	if !pending.ArchiveAttempted {
		pending.ArchiveAttempted = true
		if err := l.persistPendingDelivery(ctx, job, payload, pending); err != nil {
			return err
		}
		pending.ArtifactName = archiveReport(ctx, l.Delivery.Archiver, identityctx.IdentityID(ctx),
			payload.ConversationID, pending.Report.ChildID,
			DelegationReportMarkdown(pending.Report, time.Duration(pending.ElapsedSeconds)*time.Second))
		if pending.ArtifactName != "" {
			if err := l.persistPendingDelivery(ctx, job, payload, pending); err != nil {
				return err
			}
		}
	}

	recorded, err := l.Delivery.DeliverPreparedReport(ctx, payload, pending.Report,
		time.Duration(pending.ElapsedSeconds)*time.Second, pending.ArtifactName, pending.DeliveryKey)
	if err != nil || !recorded {
		if err == nil {
			err = fmt.Errorf("delegation report was not recorded to its origin conversation %s", payload.ConversationID)
		}
		return l.retryPendingDelivery(ctx, job, err)
	}
	if _, err := l.Store.UpdateStatus(ctx, documents.TransitionIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
		LeaseGeneration: job.LeaseGeneration, Status: pending.TargetStatus,
		ErrorCode: pending.ErrorCode, ErrorMessage: pending.ErrorMessage,
		EventType: pending.EventType, EventMessage: pending.EventMessage,
	}); err != nil {
		return l.retryPendingDelivery(ctx, job, fmt.Errorf("delegation terminal transition: %w", err))
	}
	return nil
}

func (l *DelegationClaimLoop) retryPendingDelivery(ctx context.Context, job documents.IngestionJob, cause error) error {
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
