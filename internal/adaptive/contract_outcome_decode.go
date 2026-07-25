package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DecodeOutcome strictly reconstructs a registered typed outcome payload.
func DecodeOutcome(
	payload []byte,
	assignment Assignment,
	catalog EvaluatorCatalog,
) (OutcomeObservation, error) {
	var observation OutcomeObservation
	if err := decodeStrictOutcomePayload(payload, &observation); err != nil {
		return OutcomeObservation{}, fmt.Errorf("decode adaptive outcome: %w", err)
	}
	observation = canonicalOutcomeObservation(observation)
	if err := validateOutcomeObservation(observation, catalog); err != nil {
		return OutcomeObservation{}, err
	}
	if err := validateOutcomeAssignment(assignment, observation); err != nil {
		return OutcomeObservation{}, err
	}
	return observation, nil
}

// DecodeCorrection strictly reconstructs a registered typed correction payload.
func DecodeCorrection(
	payload []byte,
	assignment Assignment,
	catalog EvaluatorCatalog,
) (CorrectionObservation, error) {
	var correction CorrectionObservation
	if err := decodeStrictOutcomePayload(payload, &correction); err != nil {
		return CorrectionObservation{}, fmt.Errorf("decode adaptive correction: %w", err)
	}
	correction = canonicalCorrectionObservation(correction)
	if err := validateCorrectionObservation(correction, catalog); err != nil {
		return CorrectionObservation{}, err
	}
	if err := validateCorrectionAssignment(assignment, correction); err != nil {
		return CorrectionObservation{}, err
	}
	return correction, nil
}

func decodeStrictOutcomePayload(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("adaptive evidence contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing value: %w", err)
	}
	return nil
}
