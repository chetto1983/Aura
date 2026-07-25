package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DecodeAssignment reconstructs an owned Assignment from its persisted payload.
func DecodeAssignment(payload []byte) (Assignment, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var assignment Assignment
	if err := decoder.Decode(&assignment); err != nil {
		return Assignment{}, fmt.Errorf("decode adaptive assignment: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Assignment{}, errors.New("adaptive assignment contains multiple JSON values")
		}
		return Assignment{}, fmt.Errorf("decode adaptive assignment trailing value: %w", err)
	}
	assignment = canonicalAssignment(assignment)
	if err := validateAssignment(assignment); err != nil {
		return Assignment{}, fmt.Errorf("validate persisted adaptive assignment: %w", err)
	}
	return assignment, nil
}
