package main

import (
	"context"
	"errors"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func (driver *adaptiveBenchmarkAuraDriver) recoverTurnFailure(
	ctx context.Context,
	requestID uuid.UUID,
	domain adaptive.Domain,
	reason string,
	elapsed time.Duration,
	assignmentBaseline map[uuid.UUID]struct{},
) (
	auraeval.AdaptiveBenchmarkDriverResult,
	adaptiveBenchmarkPendingEvaluation,
	error,
) {
	if requestID == uuid.Nil {
		requestID = driver.capturedFailureRequestID(
			domain,
			assignmentBaseline,
		)
	}
	if requestID == uuid.Nil ||
		(reason != auraeval.AdaptiveBenchmarkReasonModelTransportFailure &&
			reason != auraeval.AdaptiveBenchmarkReasonModelParserFailure) {
		result, err := driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{},
			reason,
		)
		return result, adaptiveBenchmarkPendingEvaluation{}, err
	}
	assignmentID, err := driver.recorder.TerminalEffectiveAssignmentID(
		driver.ownerID,
		requestID,
		domain,
	)
	var facts adaptiveBenchmarkPersistedFacts
	if err == nil {
		facts, err = driver.recorder.PersistedFactsForAssignment(
			ctx,
			driver.ownerID,
			requestID,
			domain,
			assignmentID,
		)
	}
	if err != nil {
		if errors.Is(err, errAdaptiveBenchmarkAssignmentFactMissing) ||
			errors.Is(err, errAdaptiveBenchmarkDeliveryFactMissing) {
			result, failureErr := driver.failure(
				auraeval.AdaptiveBenchmarkDriverResult{},
				reason,
			)
			return result, adaptiveBenchmarkPendingEvaluation{}, failureErr
		}
		result, failureErr := driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{
				RequestID: requestID.String(),
			},
			auraeval.AdaptiveBenchmarkReasonFactLinkageInvalid,
		)
		return result, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	result := adaptiveBenchmarkDriverResultFromLinkage(facts)
	if facts.Assignment.ProviderID != driver.providerID ||
		facts.Assignment.ModelID != driver.modelID {
		failed, failureErr := driver.failure(
			result,
			auraeval.AdaptiveBenchmarkReasonProvenanceMissing,
		)
		return failed, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	if elapsed <= 0 {
		failed, failureErr := driver.failure(
			result,
			auraeval.AdaptiveBenchmarkReasonProvenanceMissing,
		)
		return failed, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	result.LatencyNS = elapsed.Nanoseconds()
	failed, failureErr := driver.failure(result, reason)
	return failed, adaptiveBenchmarkPendingEvaluation{}, failureErr
}

func (driver *adaptiveBenchmarkAuraDriver) capturedAssignmentBaseline(
	domain adaptive.Domain,
) map[uuid.UUID]struct{} {
	baseline := make(map[uuid.UUID]struct{})
	driver.recorder.mu.Lock()
	defer driver.recorder.mu.Unlock()
	for assignmentID, assignment := range driver.recorder.assignments {
		if assignment.OwnerID == driver.ownerID &&
			assignment.Domain == domain {
			baseline[assignmentID] = struct{}{}
		}
	}
	return baseline
}

func (driver *adaptiveBenchmarkAuraDriver) capturedFailureRequestID(
	domain adaptive.Domain,
	baseline map[uuid.UUID]struct{},
) uuid.UUID {
	driver.recorder.mu.Lock()
	defer driver.recorder.mu.Unlock()
	requestIDs := make(map[uuid.UUID]struct{})
	for assignmentID, assignment := range driver.recorder.assignments {
		if _, existed := baseline[assignmentID]; existed ||
			assignment.OwnerID != driver.ownerID ||
			assignment.Domain != domain {
			continue
		}
		delivery, ok := driver.recorder.deliveries[assignmentID]
		if !ok ||
			delivery.ownerID != assignment.OwnerID ||
			delivery.requestID != assignment.RequestID {
			continue
		}
		requestIDs[assignment.RequestID] = struct{}{}
	}
	if len(requestIDs) != 1 {
		return uuid.Nil
	}
	for requestID := range requestIDs {
		return requestID
	}
	return uuid.Nil
}
