package adaptive

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const (
	SchemaVersion1 = "1.0"
	SchemaVersion2 = "2.0"
	StaticActionID = "static"
	ActionNoneID   = "none"
)

type Domain string

const (
	DomainReasoning     Domain = "reasoning"
	DomainToolDiscovery Domain = "tool_discovery"
	DomainSkillRouting  Domain = "skill_routing"
	DomainKnowledge     Domain = "knowledge_retrieval"
	DomainMemoryRecall  Domain = "memory_recall"
)

type DecisionPoint string

const (
	PointReasoning     DecisionPoint = "reasoning"
	PointToolDiscovery DecisionPoint = "tool_discovery"
	PointSkillRouting  DecisionPoint = "skill_routing"
	PointKnowledge     DecisionPoint = "knowledge_retrieval"
	PointMemoryRecall  DecisionPoint = "memory_recall"
)

type SelectionReason string

const (
	SelectionShadowStatic     SelectionReason = "shadow_static"
	SelectionCanaryAssignment SelectionReason = "canary_assignment"
	SelectionCanaryDiagnostic SelectionReason = "canary_diagnostic"
	SelectionOperatorOverride SelectionReason = "operator_override"
	SelectionActivePolicy     SelectionReason = "active_policy"
	SelectionPolicyOff        SelectionReason = "policy_off"
	SelectionPolicyRollback   SelectionReason = "policy_rollback"
	SelectionStateMissing     SelectionReason = "state_missing"
	SelectionStateInvalid     SelectionReason = "state_invalid"
	SelectionStateStale       SelectionReason = "state_stale"
	SelectionOwnerMismatch    SelectionReason = "owner_mismatch"
	SelectionModelMismatch    SelectionReason = "model_mismatch"
	SelectionProviderMismatch SelectionReason = "provider_mismatch"
	SelectionUnsupported      SelectionReason = "unsupported"
	SelectionChecksumMismatch SelectionReason = "checksum_mismatch"
)

type DeliveryStatus string

const (
	DeliverySuccess  DeliveryStatus = "success"
	DeliveryFallback DeliveryStatus = "fallback"
)

type FallbackReason string

const (
	FallbackCandidateUnavailable FallbackReason = "candidate_unavailable"
	FallbackStrategyFailed       FallbackReason = "strategy_failed"
	FallbackStateInvalid         FallbackReason = "state_invalid"
	FallbackStateStale           FallbackReason = "state_stale"
	FallbackOwnerMismatch        FallbackReason = "owner_mismatch"
	FallbackModelMismatch        FallbackReason = "model_mismatch"
	FallbackProviderMismatch     FallbackReason = "provider_mismatch"
	FallbackUnsupported          FallbackReason = "unsupported"
	FallbackChecksumMismatch     FallbackReason = "checksum_mismatch"
	FallbackContextBudget        FallbackReason = "context_budget"
	FallbackResultPersistFailed  FallbackReason = "result_persist_failed"
)

type ResultKind string

const (
	ResultArtifact             ResultKind = "artifact"
	ResultNode                 ResultKind = "node"
	ResultTool                 ResultKind = "tool"
	ResultSkill                ResultKind = "skill"
	ResultMemoryEntity         ResultKind = "memory_entity"
	ResultMemoryPreference     ResultKind = "memory_preference"
	ResultMemoryMessage        ResultKind = "memory_message"
	ResultMemoryReasoningTrace ResultKind = "memory_reasoning_trace"
)

type ResultID struct {
	Kind ResultKind `json:"kind"`
	ID   string     `json:"id"`
}

type ActionProbability struct {
	ActionID    string  `json:"action_id"`
	Probability float64 `json:"probability"`
}

type Assignment struct {
	SchemaVersion       string                `json:"schema_version"`
	AssignmentID        uuid.UUID             `json:"assignment_id"`
	OwnerID             uuid.UUID             `json:"owner_id"`
	RequestID           uuid.UUID             `json:"request_id"`
	Domain              Domain                `json:"domain"`
	Point               DecisionPoint         `json:"point"`
	PointOrdinal        uint32                `json:"point_ordinal"`
	PolicyEpoch         uint64                `json:"policy_epoch"`
	PolicyVersion       string                `json:"policy_version"`
	PolicyMode          PolicyMode            `json:"policy_mode"`
	SnapshotID          uuid.UUID             `json:"snapshot_id"`
	SnapshotSHA256      string                `json:"snapshot_sha256"`
	Environment         EvaluationEnvironment `json:"environment"`
	ProviderID          string                `json:"provider_id"`
	ModelID             string                `json:"model_id"`
	CohortID            *uuid.UUID            `json:"cohort_id"`
	EligibleActions     []string              `json:"eligible_actions"`
	EligibilitySHA256   string                `json:"eligibility_sha256"`
	CatalogSHA256       string                `json:"catalog_sha256"`
	ChampionActionID    string                `json:"champion_action_id"`
	RecommendedActionID string                `json:"recommended_action_id"`
	IntendedActionID    string                `json:"intended_action_id"`
	ExperimentID        string                `json:"experiment_id"`
	ArmID               string                `json:"arm_id"`
	ArmProbability      *float64              `json:"arm_probability"`
	ActionProbabilities []ActionProbability   `json:"action_probabilities"`
	SelectionReason     SelectionReason       `json:"selection_reason"`
	Override            bool                  `json:"override"`
	Features            map[string]float64    `json:"features"`
}

type Delivery struct {
	SchemaVersion       string            `json:"schema_version"`
	AssignmentID        uuid.UUID         `json:"assignment_id"`
	IntendedActionID    string            `json:"intended_action_id"`
	ActualActionID      string            `json:"actual_action_id"`
	Status              DeliveryStatus    `json:"status"`
	ExposureKnown       bool              `json:"exposure_known"`
	ExposureProbability *float64          `json:"exposure_probability"`
	FallbackReason      FallbackReason    `json:"fallback_reason"`
	ResultCount         int               `json:"result_count"`
	ResultIDs           []ResultID        `json:"result_ids"`
	Revisions           map[string]string `json:"revisions"`
	EffectiveLimits     map[string]int    `json:"effective_limits"`
}

func NewAssignmentEvent(assignment Assignment) (Event, error) {
	if err := validateAssignment(assignment); err != nil {
		return Event{}, err
	}
	payload, err := json.Marshal(assignment)
	if err != nil {
		return Event{}, fmt.Errorf("marshal adaptive assignment: %w", err)
	}
	return newEvent(EventParams{
		ID:          EventIDForSource(assignment.AssignmentID, EventDecision, "assignment"),
		OwnerID:     assignment.OwnerID,
		AggregateID: assignment.RequestID.String(),
		DecisionID:  assignment.AssignmentID,
		Kind:        EventDecision,
		Payload:     payload,
	})
}

func NewDeliveryEvent(ownerID, requestID uuid.UUID, delivery Delivery) (Event, error) {
	if ownerID == uuid.Nil {
		return Event{}, errors.New("adaptive delivery owner_id is required")
	}
	if requestID == uuid.Nil {
		return Event{}, errors.New("adaptive delivery request_id is required")
	}
	if err := validateDelivery(delivery); err != nil {
		return Event{}, err
	}
	payload, err := json.Marshal(delivery)
	if err != nil {
		return Event{}, fmt.Errorf("marshal adaptive delivery: %w", err)
	}
	return newEvent(EventParams{
		ID:          EventIDForSource(delivery.AssignmentID, EventDelivery, "assignment"),
		OwnerID:     ownerID,
		AggregateID: requestID.String(),
		DecisionID:  delivery.AssignmentID,
		Kind:        EventDelivery,
		Payload:     payload,
	})
}

func validateAssignment(assignment Assignment) error {
	switch {
	case assignment.SchemaVersion != SchemaVersion2:
		return fmt.Errorf("adaptive assignment schema_version must be %q", SchemaVersion2)
	case assignment.AssignmentID == uuid.Nil:
		return errors.New("adaptive assignment assignment_id is required")
	case assignment.OwnerID == uuid.Nil:
		return errors.New("adaptive assignment owner_id is required")
	case assignment.RequestID == uuid.Nil:
		return errors.New("adaptive assignment request_id is required")
	case !assignment.Domain.valid():
		return fmt.Errorf("adaptive assignment domain %q is invalid", assignment.Domain)
	case !assignment.Point.valid():
		return fmt.Errorf("adaptive assignment point %q is invalid", assignment.Point)
	case assignment.Domain.point() != assignment.Point:
		return fmt.Errorf("adaptive assignment domain %q does not bind point %q", assignment.Domain, assignment.Point)
	case assignment.AssignmentID != AssignmentIDForPoint(
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Point,
		assignment.PointOrdinal,
	):
		return errors.New("adaptive assignment assignment_id does not match owner, request, point, and ordinal")
	case assignment.PolicyEpoch == 0:
		return errors.New("adaptive assignment policy_epoch is required")
	case !validASCIIID(assignment.PolicyVersion, maxPolicyVersionIDLength):
		return errors.New("adaptive assignment policy_version is invalid")
	case !assignment.PolicyMode.valid():
		return fmt.Errorf("adaptive assignment policy_mode %q is invalid", assignment.PolicyMode)
	case assignment.SnapshotID == uuid.Nil:
		return errors.New("adaptive assignment snapshot_id is required")
	case !validSHA256(assignment.SnapshotSHA256):
		return errors.New("adaptive assignment snapshot_sha256 must be a lowercase SHA-256")
	case !assignment.Environment.valid():
		return fmt.Errorf("adaptive assignment environment %q is invalid", assignment.Environment)
	case !validASCIIID(assignment.ProviderID, maxProviderIDLength):
		return errors.New("adaptive assignment provider_id is invalid")
	case !validASCIIID(assignment.ModelID, maxModelIDLength):
		return errors.New("adaptive assignment model_id is invalid")
	case assignment.CohortID != nil && *assignment.CohortID == uuid.Nil:
		return errors.New("adaptive assignment cohort_id cannot be nil UUID")
	case !validSHA256(assignment.EligibilitySHA256):
		return errors.New("adaptive assignment eligibility_sha256 must be a lowercase SHA-256")
	case !validSHA256(assignment.CatalogSHA256):
		return errors.New("adaptive assignment catalog_sha256 must be a lowercase SHA-256")
	case !assignment.SelectionReason.valid():
		return fmt.Errorf("adaptive assignment selection_reason %q is invalid", assignment.SelectionReason)
	case assignment.ExperimentID != "" && !validASCIIID(assignment.ExperimentID, maxExperimentIDLength):
		return errors.New("adaptive assignment experiment_id is invalid")
	case assignment.ArmID != "" && !validASCIIID(assignment.ArmID, maxArmIDLength):
		return errors.New("adaptive assignment arm_id is invalid")
	}
	if err := validateActionCatalog(assignment); err != nil {
		return err
	}
	if err := validateAssignmentMode(assignment); err != nil {
		return err
	}
	return validateNumericFeatures(assignment.Features)
}

func validateActionCatalog(assignment Assignment) error {
	if len(assignment.EligibleActions) == 0 {
		return errors.New("adaptive assignment eligible_actions are required")
	}
	if !slices.IsSorted(assignment.EligibleActions) {
		return errors.New("adaptive assignment eligible_actions must be ordered")
	}
	seen := make(map[string]struct{}, len(assignment.EligibleActions))
	for _, actionID := range assignment.EligibleActions {
		if !validASCIIID(actionID, maxActionIDLength) {
			return fmt.Errorf("adaptive assignment eligible action ID %q is invalid", actionID)
		}
		if _, exists := seen[actionID]; exists {
			return fmt.Errorf("adaptive assignment eligible action %q is duplicated", actionID)
		}
		seen[actionID] = struct{}{}
	}
	if _, exists := seen[StaticActionID]; !exists {
		return fmt.Errorf("adaptive assignment eligible_actions must include %q", StaticActionID)
	}
	for name, actionID := range map[string]string{
		"champion_action_id":    assignment.ChampionActionID,
		"recommended_action_id": assignment.RecommendedActionID,
		"intended_action_id":    assignment.IntendedActionID,
	} {
		if _, exists := seen[actionID]; !exists {
			return fmt.Errorf("adaptive assignment %s %q is not eligible", name, actionID)
		}
	}
	if len(assignment.ActionProbabilities) != len(assignment.EligibleActions) {
		return errors.New("adaptive assignment action probability catalog is incomplete")
	}
	total := 0.0
	for i, action := range assignment.ActionProbabilities {
		if action.ActionID != assignment.EligibleActions[i] {
			return errors.New("adaptive assignment action probabilities must follow eligible action order")
		}
		if !finiteProbability(action.Probability, true) {
			return fmt.Errorf("adaptive assignment action probability for %q is invalid", action.ActionID)
		}
		total += action.Probability
	}
	if math.Abs(total-1) > 1e-9 {
		return fmt.Errorf("adaptive assignment action probabilities total %g, want 1", total)
	}
	return nil
}

func validateAssignmentMode(assignment Assignment) error {
	if assignment.Override {
		if assignment.PolicyMode != PolicyShadow &&
			assignment.PolicyMode != PolicyCanary &&
			assignment.PolicyMode != PolicyActive {
			return errors.New("adaptive assignment override is only valid in a serving mode")
		}
		if assignment.SelectionReason != SelectionOperatorOverride {
			return errors.New("adaptive assignment override requires operator_override selection reason")
		}
		if assignment.ExperimentID != "" || assignment.ArmID != "" || assignment.ArmProbability != nil {
			return errors.New("adaptive assignment override cannot claim a randomized arm")
		}
		if !deterministicIntendedAction(assignment) {
			return errors.New("adaptive assignment override must give its intended action probability 1")
		}
		return nil
	}
	if assignment.SelectionReason == SelectionOperatorOverride {
		return errors.New("adaptive assignment operator_override reason requires explicit override")
	}
	switch assignment.PolicyMode {
	case PolicyShadow:
		if assignment.SelectionReason == SelectionShadowStatic {
			return validateNonRandomizedAction(assignment, assignment.ChampionActionID, false)
		}
		if assignment.SelectionReason.failClosed() {
			return validateNonRandomizedAction(assignment, StaticActionID, false)
		}
		return errors.New("adaptive shadow assignment has invalid selection reason")
	case PolicyCanary:
		switch {
		case assignment.SelectionReason == SelectionCanaryAssignment:
			return validateFocalCanary(assignment)
		case assignment.SelectionReason == SelectionCanaryDiagnostic:
			return validateNonRandomizedAction(assignment, assignment.ChampionActionID, true)
		case assignment.SelectionReason.failClosed():
			return validateNonRandomizedAction(assignment, StaticActionID, false)
		default:
			return errors.New("adaptive canary assignment has invalid selection reason")
		}
	case PolicyOff:
		if assignment.SelectionReason != SelectionPolicyOff {
			return errors.New("adaptive off assignment requires policy_off selection reason")
		}
		return validateNonRandomizedAction(assignment, StaticActionID, false)
	case PolicyRollback:
		if assignment.SelectionReason != SelectionPolicyRollback {
			return errors.New("adaptive rollback assignment requires policy_rollback selection reason")
		}
		return validateNonRandomizedAction(assignment, StaticActionID, false)
	case PolicyActive:
		if assignment.SelectionReason == SelectionActivePolicy {
			return validateNonRandomizedAction(assignment, assignment.IntendedActionID, false)
		}
		if assignment.SelectionReason.failClosed() {
			return validateNonRandomizedAction(assignment, StaticActionID, false)
		}
		return errors.New("adaptive active assignment has invalid selection reason")
	}
	return nil
}

func validateFocalCanary(assignment Assignment) error {
	if assignment.CohortID == nil {
		return errors.New("adaptive canary assignment cohort_id is required")
	}
	if strings.TrimSpace(assignment.ExperimentID) == "" || strings.TrimSpace(assignment.ArmID) == "" {
		return errors.New("adaptive canary assignment experiment_id and arm_id are required")
	}
	if assignment.ArmProbability == nil || !finiteProbability(*assignment.ArmProbability, false) {
		return errors.New("adaptive canary assignment arm_probability is invalid")
	}
	for _, action := range assignment.ActionProbabilities {
		if action.Probability == 0 {
			return fmt.Errorf("adaptive canary action %q has no support", action.ActionID)
		}
	}
	return nil
}

func validateNonRandomizedAction(
	assignment Assignment,
	requiredActionID string,
	rejectCohort bool,
) error {
	if assignment.ExperimentID != "" || assignment.ArmID != "" || assignment.ArmProbability != nil {
		return errors.New("adaptive non-randomized assignment cannot claim experiment or arm")
	}
	if rejectCohort && assignment.CohortID != nil {
		return errors.New("adaptive non-focal assignment cannot claim cohort")
	}
	if assignment.IntendedActionID != requiredActionID || !deterministicIntendedAction(assignment) {
		return fmt.Errorf("adaptive assignment must deterministically intend action %q", requiredActionID)
	}
	return nil
}

func deterministicIntendedAction(assignment Assignment) bool {
	for _, action := range assignment.ActionProbabilities {
		want := 0.0
		if action.ActionID == assignment.IntendedActionID {
			want = 1
		}
		if action.Probability != want {
			return false
		}
	}
	return true
}

func validateDelivery(delivery Delivery) error {
	switch {
	case delivery.SchemaVersion != SchemaVersion2:
		return fmt.Errorf("adaptive delivery schema_version must be %q", SchemaVersion2)
	case delivery.AssignmentID == uuid.Nil:
		return errors.New("adaptive delivery assignment_id is required")
	case !validASCIIID(delivery.IntendedActionID, maxActionIDLength):
		return errors.New("adaptive delivery intended_action_id is invalid")
	case !validASCIIID(delivery.ActualActionID, maxActionIDLength):
		return errors.New("adaptive delivery actual_action_id is invalid")
	case !delivery.Status.valid():
		return fmt.Errorf("adaptive delivery status %q is invalid", delivery.Status)
	case delivery.ExposureKnown && (delivery.ExposureProbability == nil ||
		!finiteProbability(*delivery.ExposureProbability, false)):
		return errors.New("adaptive delivery known exposure_probability is invalid")
	case !delivery.ExposureKnown && delivery.ExposureProbability != nil:
		return errors.New("adaptive delivery unknown exposure cannot include probability")
	case delivery.ResultCount < 0:
		return errors.New("adaptive delivery result_count cannot be negative")
	case len(delivery.ResultIDs) > delivery.ResultCount:
		return errors.New("adaptive delivery result_ids exceed result_count")
	}
	if delivery.Status == DeliverySuccess {
		if delivery.ActualActionID != delivery.IntendedActionID {
			return errors.New("adaptive successful delivery actual action must match intended action")
		}
		if delivery.FallbackReason != "" {
			return errors.New("adaptive successful delivery cannot include fallback_reason")
		}
	} else {
		if delivery.ActualActionID == delivery.IntendedActionID {
			return errors.New("adaptive fallback delivery must change the actual action")
		}
		if !delivery.FallbackReason.valid() {
			return fmt.Errorf("adaptive fallback delivery fallback_reason %q is invalid", delivery.FallbackReason)
		}
	}
	if err := validateResultIDs(delivery.ResultIDs); err != nil {
		return err
	}
	if err := validateDeliveryMaps(delivery.Revisions, delivery.EffectiveLimits); err != nil {
		return err
	}
	return validateMemoryMetadata(delivery)
}

func validSHA256(value string) bool {
	if len(value) != sha256HexLength || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func finiteProbability(value float64, allowZero bool) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value > 1 {
		return false
	}
	if allowZero {
		return value >= 0
	}
	return value > 0
}

const sha256HexLength = 64
