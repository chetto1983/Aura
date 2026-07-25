package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/google/uuid"
)

type deliveryWire struct {
	SchemaVersion       string            `json:"schema_version"`
	AssignmentID        uuid.UUID         `json:"assignment_id"`
	IntendedActionID    string            `json:"intended_action_id"`
	ActualActionID      string            `json:"actual_action_id"`
	Status              DeliveryStatus    `json:"status"`
	ExposureKnown       bool              `json:"exposure_known"`
	ExposureProbability *float64          `json:"exposure_probability"`
	FallbackReason      FallbackReason    `json:"fallback_reason"`
	ResultCount         int               `json:"result_count"`
	ResultIDs           []resultIDWire    `json:"result_ids"`
	Revisions           map[string]string `json:"revisions"`
	EffectiveLimits     map[string]int    `json:"effective_limits"`
}

type resultIDWire struct {
	Kind ResultKind `json:"kind"`
	ID   string     `json:"id"`
}

// DecodeDelivery rebuilds a persisted delivery, re-validating it against the assignment it
// belongs to and the frozen catalogs. The catalogs are parameters rather than globals
// because tool, skill and revision identifiers cannot be checked structurally — decoding
// without them would mean trusting the payload to name only things that exist, which is
// exactly what ResultID.UnmarshalJSON refuses to do on its own.
func DecodeDelivery(
	payload []byte,
	assignment Assignment,
	results ResultCatalog,
	revisions RevisionCatalog,
) (Delivery, error) {
	wire, err := decodeDeliveryWire(payload)
	if err != nil {
		return Delivery{}, err
	}
	resultIDs := make([]ResultID, len(wire.ResultIDs))
	for i, encoded := range wire.ResultIDs {
		resultIDs[i], err = decodeResultID(encoded, results)
		if err != nil {
			return Delivery{}, err
		}
	}
	revisionSet, err := decodeRevisionSet(wire.Revisions, revisions)
	if err != nil {
		return Delivery{}, err
	}
	delivery := Delivery{
		SchemaVersion:       wire.SchemaVersion,
		AssignmentID:        wire.AssignmentID,
		IntendedActionID:    wire.IntendedActionID,
		ActualActionID:      wire.ActualActionID,
		Status:              wire.Status,
		ExposureKnown:       wire.ExposureKnown,
		ExposureProbability: wire.ExposureProbability,
		FallbackReason:      wire.FallbackReason,
		ResultCount:         wire.ResultCount,
		ResultIDs:           resultIDs,
		Revisions:           revisionSet,
		EffectiveLimits:     wire.EffectiveLimits,
	}
	_, delivery, err = canonicalAssignmentBoundDelivery(assignment, delivery)
	if err != nil {
		return Delivery{}, err
	}
	return delivery, nil
}

func decodeDeliveryWire(payload []byte) (deliveryWire, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var wire deliveryWire
	if err := decoder.Decode(&wire); err != nil {
		return deliveryWire{}, fmt.Errorf("decode adaptive delivery: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return deliveryWire{}, errors.New("adaptive delivery contains multiple JSON values")
		}
		return deliveryWire{}, fmt.Errorf("decode adaptive delivery trailing value: %w", err)
	}
	return wire, nil
}

func decodeResultID(encoded resultIDWire, catalog ResultCatalog) (ResultID, error) {
	switch encoded.Kind {
	case ResultArtifact:
		return NewArtifactResultID(encoded.ID)
	case ResultNode:
		return NewNodeResultID(encoded.ID)
	case ResultTool:
		return NewToolResultID(encoded.ID, catalog)
	case ResultSkill:
		return NewSkillResultID(encoded.ID, catalog)
	case ResultMemoryEntity:
		return NewMemoryEntityResultID(encoded.ID)
	case ResultMemoryPreference:
		return NewMemoryPreferenceResultID(encoded.ID)
	case ResultMemoryMessage:
		return NewMemoryMessageResultID(encoded.ID)
	case ResultMemoryReasoningTrace:
		return NewMemoryReasoningTraceResultID(encoded.ID)
	default:
		return ResultID{}, fmt.Errorf("adaptive result kind %q is invalid", encoded.Kind)
	}
}

func decodeRevisionSet(
	encoded map[string]string,
	catalog RevisionCatalog,
) (RevisionSet, error) {
	revisions := make([]Revision, 0, len(encoded))
	for key, value := range encoded {
		kind := RevisionKind(key)
		if kind == RevisionCorpus {
			epoch, err := strconv.ParseUint(value, 10, 64)
			if err != nil || epoch == 0 || strconv.FormatUint(epoch, 10) != value {
				return RevisionSet{}, errors.New("adaptive corpus revision must be a positive canonical epoch")
			}
			revision, err := NewCorpusRevision(epoch)
			if err != nil {
				return RevisionSet{}, err
			}
			revisions = append(revisions, revision)
			continue
		}
		if !kind.fixed() {
			return RevisionSet{}, fmt.Errorf("adaptive revision kind %q is invalid", kind)
		}
		revision, err := NewRegisteredRevision(kind, value, catalog)
		if err != nil {
			return RevisionSet{}, err
		}
		revisions = append(revisions, revision)
	}
	return NewRevisionSet(revisions...)
}
