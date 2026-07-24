package adaptive

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

const (
	maxPolicyVersionIDLength = 128
	maxProviderIDLength      = 64
	maxModelIDLength         = 256
	maxExperimentIDLength    = 128
	maxArmIDLength           = 128
	maxActionIDLength        = 128
	maxResultIDLength        = 256
	maxRevisionIDLength      = 128
)

var (
	assignmentIDNamespace = uuid.MustParse("c5370396-c73f-4a44-ae7b-112f070523ae")
	eventIDNamespace      = uuid.MustParse("fb3f7ce9-d343-41fb-a26f-35155b229189")
)

var registeredFeatureKeys = map[string]struct{}{
	"available_skill_count":  {},
	"available_tool_count":   {},
	"candidate_count":        {},
	"context_length":         {},
	"context_token_count":    {},
	"deferred_tool_count":    {},
	"document_count":         {},
	"input_token_count":      {},
	"matched_skill_count":    {},
	"memory_count":           {},
	"message_count":          {},
	"prompt_length":          {},
	"query_length":           {},
	"recall_limit":           {},
	"request_length":         {},
	"result_count":           {},
	"retrieval_limit":        {},
	"retrieved_result_count": {},
	"retry_count":            {},
	"skill_candidate_count":  {},
	"tool_argument_count":    {},
	"tool_result_count":      {},
}

var registeredRevisionKeys = map[string]struct{}{
	"corpus": {}, "index": {}, "retriever": {},
}

var registeredEffectiveLimitKeys = map[string]struct{}{
	"candidate_limit": {}, "max_results": {}, "max_rounds": {}, "max_tokens": {},
	"recall_limit": {}, "skill_limit": {}, "timeout_ms": {}, "tool_limit": {}, "top_k": {},
}

func AssignmentIDForPoint(
	ownerID uuid.UUID,
	requestID uuid.UUID,
	point DecisionPoint,
	ordinal uint32,
) uuid.UUID {
	identity := make([]byte, 0, 16+16+1+len(point)+1+4)
	identity = append(identity, ownerID[:]...)
	identity = append(identity, requestID[:]...)
	identity = append(identity, 0)
	identity = append(identity, strings.TrimSpace(string(point))...)
	identity = append(identity, 0)
	identity = binary.BigEndian.AppendUint32(identity, ordinal)
	return uuid.NewHash(sha256.New(), assignmentIDNamespace, identity, 5)
}

func EventIDForSource(
	assignmentID uuid.UUID,
	kind EventKind,
	sourceIdentity string,
) uuid.UUID {
	sourceIdentity = strings.TrimSpace(sourceIdentity)
	identity := make([]byte, 0, len(SchemaVersion2)+1+16+1+len(kind)+1+len(sourceIdentity))
	identity = append(identity, SchemaVersion2...)
	identity = append(identity, 0)
	identity = append(identity, assignmentID[:]...)
	identity = append(identity, 0)
	identity = append(identity, kind...)
	identity = append(identity, 0)
	identity = append(identity, sourceIdentity...)
	return uuid.NewHash(sha256.New(), eventIDNamespace, identity, 5)
}

func validateNumericFeatures(features map[string]float64) error {
	if len(features) > 128 {
		return errors.New("adaptive assignment features exceed 128 entries")
	}
	for key, value := range features {
		if err := validateRegisteredKey("feature", key, registeredFeatureKeys); err != nil {
			return err
		}
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			value < 0 || value > 1_000_000_000 || math.Trunc(value) != value {
			return fmt.Errorf("adaptive assignment feature %q is out of range", key)
		}
	}
	return nil
}

func validateResultIDs(resultIDs []ResultID) error {
	seen := make(map[ResultID]struct{}, len(resultIDs))
	for _, resultID := range resultIDs {
		if !resultID.Kind.valid() || !validASCIIID(resultID.ID, maxResultIDLength) {
			return fmt.Errorf("adaptive delivery result ID %#v is invalid", resultID)
		}
		if _, exists := seen[resultID]; exists {
			return fmt.Errorf("adaptive delivery result ID %#v is duplicated", resultID)
		}
		seen[resultID] = struct{}{}
	}
	return nil
}

func validateDeliveryMaps(revisions map[string]string, limits map[string]int) error {
	for key, revision := range revisions {
		if err := validateRegisteredKey("revision", key, registeredRevisionKeys); err != nil {
			return err
		}
		if !validASCIIID(revision, maxRevisionIDLength) {
			return fmt.Errorf("adaptive delivery revision %q is invalid", key)
		}
	}
	for key, limit := range limits {
		if err := validateRegisteredKey("effective limit", key, registeredEffectiveLimitKeys); err != nil {
			return err
		}
		if limit < 0 || limit > 1_000_000_000 {
			return fmt.Errorf("adaptive delivery effective limit %q is out of range", key)
		}
	}
	return nil
}

func validateRegisteredKey(kind, key string, registry map[string]struct{}) error {
	if privatePayloadAlias(key) {
		return fmt.Errorf("adaptive %s key %q is private", kind, key)
	}
	if _, registered := registry[key]; !registered {
		return fmt.Errorf("adaptive %s key %q is not registered", kind, key)
	}
	return nil
}

func privatePayloadAlias(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range []string{"fingerprint", "digest", "checksum", "_hash", "_sha"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func validASCIIID(value string, maximumLength int) bool {
	if value == "" || len(value) > maximumLength {
		return false
	}
	for i := range len(value) {
		character := value[i]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("-._:/@+", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func (domain Domain) valid() bool {
	switch domain {
	case DomainReasoning, DomainToolDiscovery, DomainSkillRouting, DomainKnowledge, DomainMemoryRecall:
		return true
	default:
		return false
	}
}

func (domain Domain) point() DecisionPoint {
	switch domain {
	case DomainReasoning:
		return PointReasoning
	case DomainToolDiscovery:
		return PointToolDiscovery
	case DomainSkillRouting:
		return PointSkillRouting
	case DomainKnowledge:
		return PointKnowledge
	case DomainMemoryRecall:
		return PointMemoryRecall
	default:
		return ""
	}
}

func (point DecisionPoint) valid() bool {
	switch point {
	case PointReasoning, PointToolDiscovery, PointSkillRouting, PointKnowledge, PointMemoryRecall:
		return true
	default:
		return false
	}
}

func (reason SelectionReason) valid() bool {
	switch reason {
	case SelectionShadowStatic, SelectionCanaryAssignment, SelectionOperatorOverride,
		SelectionActivePolicy, SelectionPolicyOff, SelectionPolicyRollback,
		SelectionStateMissing, SelectionStateInvalid, SelectionStateStale,
		SelectionOwnerMismatch, SelectionModelMismatch, SelectionProviderMismatch,
		SelectionUnsupported, SelectionChecksumMismatch:
		return true
	default:
		return false
	}
}

func (reason FallbackReason) valid() bool {
	switch reason {
	case FallbackCandidateUnavailable, FallbackStrategyFailed, FallbackStateInvalid,
		FallbackStateStale, FallbackOwnerMismatch, FallbackModelMismatch,
		FallbackProviderMismatch, FallbackUnsupported, FallbackChecksumMismatch,
		FallbackResultPersistFailed:
		return true
	default:
		return false
	}
}

func (mode PolicyMode) valid() bool {
	switch mode {
	case PolicyOff, PolicyShadow, PolicyCanary, PolicyActive, PolicyRollback:
		return true
	default:
		return false
	}
}

func (environment EvaluationEnvironment) valid() bool {
	switch environment {
	case EvaluationSpike, EvaluationOffline, EvaluationProductionCanary:
		return true
	default:
		return false
	}
}

func (status DeliveryStatus) valid() bool {
	return status == DeliverySuccess || status == DeliveryFallback
}

func (kind ResultKind) valid() bool {
	switch kind {
	case ResultArtifact, ResultNode, ResultTool, ResultSkill:
		return true
	default:
		return false
	}
}
