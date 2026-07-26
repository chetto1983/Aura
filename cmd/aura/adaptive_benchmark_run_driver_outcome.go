package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

const adaptiveBenchmarkEvaluatorContract = "" +
	"aura/adaptive-deterministic-checker\x001\x00" +
	"normalized-exact-answer\x00exact-tool-id\x00exact-skill-id\x00" +
	"exact-ranked-result-ids"

func adaptiveBenchmarkDeterministicEvaluatorRegistration() adaptive.EvaluatorRegistration {
	sum := sha256.Sum256(
		[]byte(adaptiveBenchmarkEvaluatorContract),
	)
	return adaptive.EvaluatorRegistration{
		Kind:             adaptive.EvaluatorDeterministic,
		ID:               auraeval.AdaptiveBenchmarkEvaluatorID,
		Version:          "1",
		RubricID:         "benchmark-quality",
		CalibrationID:    "none",
		ProvenanceSHA256: hex.EncodeToString(sum[:]),
	}
}

func (driver *adaptiveBenchmarkAuraDriver) RecordEvaluation(
	ctx context.Context,
	record auraeval.AdaptiveBenchmarkEvaluationRecord,
) (auraeval.AdaptiveBenchmarkRecordedEvaluation, error) {
	if driver == nil {
		return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
			adaptiveBenchmarkOutcomeFailure()
	}
	requestID, requestErr := parseCanonicalAdaptiveBenchmarkUUID(
		record.RequestID,
	)
	assignmentID, assignmentErr := parseCanonicalAdaptiveBenchmarkUUID(
		record.AssignmentID,
	)
	deliveryID, deliveryErr := parseCanonicalAdaptiveBenchmarkUUID(
		record.DeliveryEventID,
	)
	if record.ScenarioID == "" ||
		requestErr != nil ||
		assignmentErr != nil ||
		deliveryErr != nil {
		return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
			adaptiveBenchmarkOutcomeFailure()
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	pending, ok := driver.pending[record.ScenarioID]
	if !ok ||
		pending.assignment.RequestID != requestID ||
		pending.assignment.AssignmentID != assignmentID ||
		pending.deliveryEvent.ID != deliveryID {
		return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
			adaptiveBenchmarkOutcomeFailure()
	}
	observedAt := pending.outcomeObservedAt
	if observedAt.IsZero() {
		observedAt = driver.now().Round(0).UTC()
		if observedAt.IsZero() {
			return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
				adaptiveBenchmarkOutcomeFailure()
		}
		pending.outcomeObservedAt = observedAt
		driver.pending[record.ScenarioID] = pending
	}
	quality := float64(0)
	if record.Evaluation.Passed {
		quality = 1
	}
	harm := float64(0)
	observation := adaptive.OutcomeObservation{
		SchemaVersion: adaptive.SchemaVersion2,
		AssignmentID:  pending.assignment.AssignmentID,
		OwnerID:       pending.assignment.OwnerID,
		Domain:        pending.assignment.Domain,
		ProviderID:    pending.assignment.ProviderID,
		ModelID:       pending.assignment.ModelID,
		Evaluator: adaptiveBenchmarkDeterministicEvaluatorRegistration().
			Provenance(),
		SourceObservationID: uuid.NewSHA1(
			driver.runID,
			[]byte("quality\x00"+record.ScenarioID),
		),
		Status: adaptive.OutcomeObserved,
		Measures: adaptive.OutcomeMeasures{
			Quality: &quality,
			Harm:    &harm,
		},
		ObservedAt: observedAt,
	}
	expected, err := adaptive.NewOutcomeEvent(
		pending.assignment,
		observation,
		driver.catalog,
	)
	if err != nil {
		return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
			adaptiveBenchmarkOutcomeFailure()
	}
	if _, err := driver.outcomes.RecordOutcome(
		ctx,
		observation,
	); err != nil {
		if cancellation := adaptiveBenchmarkCancellation(ctx, err); cancellation != nil {
			return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
				cancellation
		}
		return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
			adaptiveBenchmarkOutcomeFailure()
	}
	records, err := driver.ledger.ListAggregate(
		ctx,
		driver.ownerID,
		requestID.String(),
	)
	if err != nil {
		return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
			adaptiveBenchmarkOutcomeFailure()
	}
	sequence, err := adaptiveBenchmarkPersistedSequence(
		records,
		expected,
	)
	if err != nil || sequence <= pending.deliverySequence {
		return auraeval.AdaptiveBenchmarkRecordedEvaluation{},
			adaptiveBenchmarkOutcomeFailure()
	}
	return auraeval.AdaptiveBenchmarkRecordedEvaluation{
		QualityOutcomeID: expected.ID.String(),
		Quality: &auraeval.AdaptiveBenchmarkObservation{
			EvaluatorID:       auraeval.AdaptiveBenchmarkEvaluatorID,
			EvaluatorRevision: auraeval.AdaptiveBenchmarkEvaluatorRevision,
			Value:             &quality,
			Eligible:          true,
			ReasonCodes: slices.Clone(
				record.Evaluation.ReasonCodes,
			),
		},
	}, nil
}

func parseCanonicalAdaptiveBenchmarkUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil ||
		parsed == uuid.Nil ||
		parsed.String() != value {
		return uuid.Nil, errors.New(
			"adaptive benchmark UUID is not canonical",
		)
	}
	return parsed, nil
}

func adaptiveBenchmarkOutcomeFailure() error {
	return fmt.Errorf(
		"%w: %s",
		errAdaptiveBenchmarkScenarioFailure,
		auraeval.AdaptiveBenchmarkReasonOutcomeMissing,
	)
}
