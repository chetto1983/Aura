package adaptive

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

func validateCohortDeliveryFact(assignment Assignment, event Event, wire deliveryWire) error {
	if err := validateCohortEventIdentity(event, assignment, EventDelivery, eventIDForSource(assignment.AssignmentID, EventDelivery, "assignment")); err != nil {
		return err
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("%w: delivery payload", ErrInvalidCohortLedger)
	}
	expectedPayload, expectedPayloadErr := canonicalPayload(payload)
	actualPayload, actualPayloadErr := canonicalPayload(event.Payload)
	if expectedPayloadErr != nil || actualPayloadErr != nil || !bytes.Equal(expectedPayload, actualPayload) {
		return fmt.Errorf("%w: delivery payload", ErrInvalidCohortLedger)
	}
	if !bytes.Equal(payloadSHA256(expectedPayload), event.PayloadHash) {
		return fmt.Errorf("%w: delivery payload hash", ErrInvalidCohortLedger)
	}
	if !validCohortDeliveryWire(assignment, wire) {
		return fmt.Errorf("%w: delivery envelope", ErrInvalidCohortLedger)
	}
	return nil
}

func validCohortDeliveryWire(assignment Assignment, wire deliveryWire) bool {
	if wire.SchemaVersion != SchemaVersion2 || wire.AssignmentID != assignment.AssignmentID ||
		wire.IntendedActionID != assignment.IntendedActionID || !validASCIIID(wire.ActualActionID, maxActionIDLength) ||
		!wire.Status.valid() || wire.ResultCount < 0 || len(wire.ResultIDs) > wire.ResultCount {
		return false
	}
	if wire.ActualActionID != ActionNoneID {
		eligible := false
		for _, actionID := range assignment.EligibleActions {
			if actionID == wire.ActualActionID {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
		}
	}
	if wire.ExposureKnown != (wire.ExposureProbability != nil) ||
		(wire.ExposureProbability != nil && !finiteProbability(*wire.ExposureProbability, false)) {
		return false
	}
	if wire.Status == DeliverySuccess {
		if wire.ActualActionID != assignment.IntendedActionID || wire.FallbackReason != "" {
			return false
		}
		if wire.ExposureKnown {
			for _, action := range assignment.ActionProbabilities {
				if action.ActionID == assignment.IntendedActionID {
					return *wire.ExposureProbability == action.Probability
				}
			}
			return false
		}
		return true
	}
	return wire.ActualActionID != assignment.IntendedActionID && wire.FallbackReason.valid() &&
		(wire.ActualActionID != ActionNoneID || (!wire.ExposureKnown && len(wire.ResultIDs) == 0 && wire.ResultCount == 0))
}

func validateCohortOutcomeFact(assignment Assignment, event Event, outcome OutcomeObservation, catalog EvaluatorCatalog) error {
	expected, err := NewOutcomeEvent(assignment, outcome, catalog)
	if err != nil {
		return fmt.Errorf("%w: outcome envelope", ErrInvalidCohortLedger)
	}
	return validateCohortExpectedEvent(expected, event, "outcome")
}

func validateCohortCorrectionFact(assignment Assignment, event Event, correction CorrectionObservation, catalog EvaluatorCatalog) error {
	expected, err := NewCorrectionEvent(assignment, correction, catalog)
	if err != nil {
		return fmt.Errorf("%w: correction envelope", ErrInvalidCohortLedger)
	}
	return validateCohortExpectedEvent(expected, event, "correction")
}

func validateCohortExpectedEvent(expected, actual Event, label string) error {
	expectedPayload, expectedPayloadErr := canonicalPayload(expected.Payload)
	actualPayload, actualPayloadErr := canonicalPayload(actual.Payload)
	if expectedPayloadErr != nil || actualPayloadErr != nil || expected.ID != actual.ID ||
		expected.OwnerID != actual.OwnerID || expected.AggregateID != actual.AggregateID ||
		expected.DecisionID != actual.DecisionID || expected.Kind != actual.Kind ||
		!bytes.Equal(expectedPayload, actualPayload) || !bytes.Equal(expected.PayloadHash, actual.PayloadHash) {
		return fmt.Errorf("%w: %s envelope", ErrInvalidCohortLedger, label)
	}
	return nil
}

func validateCohortEventIdentity(event Event, assignment Assignment, kind EventKind, eventID uuid.UUID) error {
	if event.ID != eventID || event.OwnerID != assignment.OwnerID ||
		event.AggregateID != assignment.RequestID.String() || event.DecisionID != assignment.AssignmentID || event.Kind != kind {
		return fmt.Errorf("%w: event identity", ErrInvalidCohortLedger)
	}
	return nil
}
