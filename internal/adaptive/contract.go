package adaptive

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	SchemaVersion2 = "2.0"
	StaticActionID = "static"
	ActionNoneID   = "none"
)

type DecisionPoint string

const (
	PointReasoning     DecisionPoint = "reasoning"
	PointToolDiscovery DecisionPoint = "tool_discovery"
	PointSkillRouting  DecisionPoint = "skill_routing"
	PointKnowledge     DecisionPoint = "knowledge_retrieval"
	PointMemoryRecall  DecisionPoint = "memory_recall"
)

type DeliveryStatus string

const (
	DeliverySuccess  DeliveryStatus = "success"
	DeliveryFallback DeliveryStatus = "fallback"
)

type ResultKind string

const (
	ResultArtifact ResultKind = "artifact"
	ResultNode     ResultKind = "node"
	ResultTool     ResultKind = "tool"
	ResultSkill    ResultKind = "skill"
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
	SelectionReason     string                `json:"selection_reason"`
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
	FallbackReason      string            `json:"fallback_reason"`
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
	case !assignment.Point.valid():
		return fmt.Errorf("adaptive assignment point %q is invalid", assignment.Point)
	case assignment.AssignmentID != AssignmentIDForPoint(
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Point,
		assignment.PointOrdinal,
	):
		return errors.New("adaptive assignment assignment_id does not match owner, request, point, and ordinal")
	case assignment.PolicyEpoch == 0:
		return errors.New("adaptive assignment policy_epoch is required")
	case strings.TrimSpace(assignment.PolicyVersion) == "":
		return errors.New("adaptive assignment policy_version is required")
	case !assignment.PolicyMode.valid():
		return fmt.Errorf("adaptive assignment policy_mode %q is invalid", assignment.PolicyMode)
	case assignment.SnapshotID == uuid.Nil:
		return errors.New("adaptive assignment snapshot_id is required")
	case !validSHA256(assignment.SnapshotSHA256):
		return errors.New("adaptive assignment snapshot_sha256 must be a lowercase SHA-256")
	case !assignment.Environment.valid():
		return fmt.Errorf("adaptive assignment environment %q is invalid", assignment.Environment)
	case strings.TrimSpace(assignment.ProviderID) == "":
		return errors.New("adaptive assignment provider_id is required")
	case strings.TrimSpace(assignment.ModelID) == "":
		return errors.New("adaptive assignment model_id is required")
	case assignment.CohortID != nil && *assignment.CohortID == uuid.Nil:
		return errors.New("adaptive assignment cohort_id cannot be nil UUID")
	case !validSHA256(assignment.EligibilitySHA256):
		return errors.New("adaptive assignment eligibility_sha256 must be a lowercase SHA-256")
	case !validSHA256(assignment.CatalogSHA256):
		return errors.New("adaptive assignment catalog_sha256 must be a lowercase SHA-256")
	case strings.TrimSpace(assignment.SelectionReason) == "":
		return errors.New("adaptive assignment selection_reason is required")
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
		if strings.TrimSpace(actionID) == "" {
			return errors.New("adaptive assignment eligible action ID is required")
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
	if assignment.ChampionActionID != StaticActionID {
		return fmt.Errorf("adaptive assignment champion_action_id must be %q", StaticActionID)
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
	if assignment.Override &&
		(assignment.ExperimentID != "" || assignment.ArmID != "" || assignment.ArmProbability != nil) {
		return errors.New("adaptive assignment override cannot claim a randomized arm")
	}
	switch assignment.PolicyMode {
	case PolicyShadow:
		if assignment.IntendedActionID != assignment.ChampionActionID {
			return errors.New("adaptive shadow assignment must intend the static champion")
		}
		if assignment.ExperimentID != "" || assignment.ArmID != "" || assignment.ArmProbability != nil {
			return errors.New("adaptive shadow assignment cannot claim a randomized arm")
		}
		for _, action := range assignment.ActionProbabilities {
			want := 0.0
			if action.ActionID == assignment.ChampionActionID {
				want = 1
			}
			if action.Probability != want {
				return errors.New("adaptive shadow assignment must give the static champion probability 1")
			}
		}
	case PolicyCanary:
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
	default:
		if assignment.ArmProbability != nil && !finiteProbability(*assignment.ArmProbability, false) {
			return errors.New("adaptive assignment arm_probability is invalid")
		}
	}
	return nil
}

func validateNumericFeatures(features map[string]float64) error {
	if len(features) > 128 {
		return errors.New("adaptive assignment features exceed 128 entries")
	}
	for key, value := range features {
		if err := validateSafeKey("feature", key); err != nil {
			return err
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e12 {
			return fmt.Errorf("adaptive assignment feature %q is out of range", key)
		}
	}
	return nil
}

func validateDelivery(delivery Delivery) error {
	switch {
	case delivery.SchemaVersion != SchemaVersion2:
		return fmt.Errorf("adaptive delivery schema_version must be %q", SchemaVersion2)
	case delivery.AssignmentID == uuid.Nil:
		return errors.New("adaptive delivery assignment_id is required")
	case strings.TrimSpace(delivery.IntendedActionID) == "":
		return errors.New("adaptive delivery intended_action_id is required")
	case strings.TrimSpace(delivery.ActualActionID) == "":
		return errors.New("adaptive delivery actual_action_id is required")
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
		if strings.TrimSpace(delivery.FallbackReason) == "" {
			return errors.New("adaptive fallback delivery fallback_reason is required")
		}
	}
	if err := validateResultIDs(delivery.ResultIDs); err != nil {
		return err
	}
	return validateDeliveryMaps(delivery.Revisions, delivery.EffectiveLimits)
}

func validateResultIDs(resultIDs []ResultID) error {
	seen := make(map[ResultID]struct{}, len(resultIDs))
	for _, resultID := range resultIDs {
		if !resultID.Kind.valid() || strings.TrimSpace(resultID.ID) == "" {
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
	allowedRevisions := map[string]struct{}{
		"corpus": {}, "index": {}, "retriever": {},
	}
	for key, revision := range revisions {
		if err := validateSafeKey("revision", key); err != nil {
			return err
		}
		if _, allowed := allowedRevisions[key]; !allowed {
			return fmt.Errorf("adaptive delivery revision key %q is not registered", key)
		}
		if strings.TrimSpace(revision) == "" {
			return fmt.Errorf("adaptive delivery revision %q is empty", key)
		}
	}
	for key, limit := range limits {
		if err := validateSafeKey("effective limit", key); err != nil {
			return err
		}
		if limit < 0 || limit > 1_000_000_000 {
			return fmt.Errorf("adaptive delivery effective limit %q is out of range", key)
		}
	}
	return nil
}

func validateSafeKey(kind, key string) error {
	if key == "" || len(key) > 64 {
		return fmt.Errorf("adaptive %s key %q is invalid", kind, key)
	}
	for _, r := range key {
		if r != '_' && !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return fmt.Errorf("adaptive %s key %q is invalid", kind, key)
		}
	}
	privateKeys := map[string]struct{}{
		"api_key": {}, "chain_of_thought": {}, "content_fingerprint": {},
		"content_hash": {}, "credentials": {}, "document_content": {},
		"document_hash": {}, "memory_content": {}, "memory_hash": {},
		"password": {}, "prompt": {}, "prompt_text": {}, "query": {},
		"query_hash": {}, "query_text": {}, "raw_prompt": {}, "raw_query": {},
		"result_hash": {}, "secret": {}, "tool_args": {}, "tool_arguments": {},
	}
	if _, private := privateKeys[key]; private {
		return fmt.Errorf("adaptive %s key %q is private", kind, key)
	}
	return nil
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

func (point DecisionPoint) valid() bool {
	switch point {
	case PointReasoning, PointToolDiscovery, PointSkillRouting, PointKnowledge, PointMemoryRecall:
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

const sha256HexLength = 64
