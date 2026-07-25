package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
)

const (
	// SnapshotSchemaVersion identifies the first immutable policy snapshot contract.
	SnapshotSchemaVersion        = "1.0"
	maxSnapshotActions           = 128
	maxSnapshotFeatures          = 128
	maxSnapshotExamplesPerAction = 256
	maxSnapshotFeatureScale      = 1_000_000_000
)

var snapshotIDNamespace = uuid.MustParse("079f0700-7db9-4d37-9d2a-1890d2da90f8")

// SnapshotScope binds a frozen policy artifact to one serving boundary.
type SnapshotScope struct {
	OwnerID        uuid.UUID     `json:"owner_id"`
	Domain         Domain        `json:"domain"`
	Point          DecisionPoint `json:"point"`
	ProviderID     string        `json:"provider_id"`
	ModelID        string        `json:"model_id"`
	PolicyVersion  string        `json:"policy_version"`
	TrainingCutoff time.Time     `json:"training_cutoff"`
}

// SnapshotFeatureDefinition freezes one registered feature's normalization.
type SnapshotFeatureDefinition struct {
	Key    FeatureKey `json:"key"`
	Center float64    `json:"center"`
	Scale  float64    `json:"scale"`
}

// SnapshotFeatureValue is one bounded raw value from a registered feature.
type SnapshotFeatureValue struct {
	Key   FeatureKey `json:"key"`
	Value float64    `json:"value"`
}

// SnapshotExample is one eligible historical outcome frozen before the cutoff.
type SnapshotExample struct {
	OutcomeID  uuid.UUID              `json:"outcome_id"`
	OwnerID    uuid.UUID              `json:"owner_id"`
	Domain     Domain                 `json:"domain"`
	Point      DecisionPoint          `json:"point"`
	ProviderID string                 `json:"provider_id"`
	ModelID    string                 `json:"model_id"`
	ObservedAt time.Time              `json:"observed_at"`
	Eligible   bool                   `json:"eligible"`
	Utility    float64                `json:"utility"`
	Features   []SnapshotFeatureValue `json:"features"`
}

// SnapshotAction groups the bounded historical support for one eligible action.
type SnapshotAction struct {
	ActionID string            `json:"action_id"`
	Examples []SnapshotExample `json:"examples"`
}

// SnapshotSpec describes the complete typed content of an immutable snapshot.
type SnapshotSpec struct {
	Scope            SnapshotScope               `json:"scope"`
	ChampionActionID string                      `json:"champion_action_id"`
	FeatureSchema    []SnapshotFeatureDefinition `json:"feature_schema"`
	Actions          []SnapshotAction            `json:"actions"`
}

// PolicySnapshot is a verified immutable local scoring artifact.
type PolicySnapshot struct {
	id               uuid.UUID
	sha256           string
	scope            SnapshotScope
	championActionID string
	featureSchema    []SnapshotFeatureDefinition
	actions          []SnapshotAction
	scoringActions   []snapshotScoringAction
	verified         bool
}

type canonicalSnapshot struct {
	SchemaVersion    string                      `json:"schema_version"`
	Scope            SnapshotScope               `json:"scope"`
	ChampionActionID string                      `json:"champion_action_id"`
	FeatureSchema    []SnapshotFeatureDefinition `json:"feature_schema"`
	Actions          []SnapshotAction            `json:"actions"`
}

// NewPolicySnapshot validates, canonicalizes, hashes, and freezes a snapshot.
func NewPolicySnapshot(spec SnapshotSpec) (*PolicySnapshot, error) {
	canonical, err := canonicalizeSnapshot(spec)
	if err != nil {
		return nil, err
	}
	scoringActions, err := buildSnapshotScoringActions(
		canonical.Actions,
		canonical.FeatureSchema,
	)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(canonicalSnapshot{
		SchemaVersion:    SnapshotSchemaVersion,
		Scope:            canonical.Scope,
		ChampionActionID: canonical.ChampionActionID,
		FeatureSchema:    canonical.FeatureSchema,
		Actions:          canonical.Actions,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal adaptive policy snapshot: %w", err)
	}
	sum := sha256.Sum256(payload)
	return &PolicySnapshot{
		id: uuid.NewHash(
			sha256.New(),
			snapshotIDNamespace,
			sum[:],
			5,
		),
		sha256:           hex.EncodeToString(sum[:]),
		scope:            canonical.Scope,
		championActionID: canonical.ChampionActionID,
		featureSchema:    canonical.FeatureSchema,
		actions:          canonical.Actions,
		scoringActions:   scoringActions,
		verified:         true,
	}, nil
}

// ID returns the deterministic content-derived snapshot ID.
func (snapshot *PolicySnapshot) ID() uuid.UUID {
	if snapshot == nil {
		return uuid.Nil
	}
	return snapshot.id
}

// SHA256 returns the canonical typed-content digest.
func (snapshot *PolicySnapshot) SHA256() string {
	if snapshot == nil {
		return ""
	}
	return snapshot.sha256
}

// Scope returns the immutable serving scope.
func (snapshot *PolicySnapshot) Scope() SnapshotScope {
	if snapshot == nil {
		return SnapshotScope{}
	}
	return snapshot.scope
}

// ChampionActionID returns the explicit static fallback action.
func (snapshot *PolicySnapshot) ChampionActionID() string {
	if snapshot == nil {
		return StaticActionID
	}
	return snapshot.championActionID
}

// FeatureSchema returns a defensive copy of the frozen feature schema.
func (snapshot *PolicySnapshot) FeatureSchema() []SnapshotFeatureDefinition {
	if snapshot == nil {
		return nil
	}
	return append([]SnapshotFeatureDefinition(nil), snapshot.featureSchema...)
}

// Actions returns a defensive deep copy of the frozen action catalog.
func (snapshot *PolicySnapshot) Actions() []SnapshotAction {
	if snapshot == nil {
		return nil
	}
	return cloneSnapshotActions(snapshot.actions)
}

func canonicalizeSnapshot(spec SnapshotSpec) (SnapshotSpec, error) {
	spec.Scope.TrainingCutoff = spec.Scope.TrainingCutoff.UTC()
	spec.FeatureSchema = append([]SnapshotFeatureDefinition(nil), spec.FeatureSchema...)
	spec.Actions = cloneSnapshotActions(spec.Actions)
	if err := validateSnapshotScope(spec.Scope); err != nil {
		return SnapshotSpec{}, err
	}
	if err := validateSnapshotFeatureSchema(spec.FeatureSchema); err != nil {
		return SnapshotSpec{}, err
	}
	if err := validateSnapshotActionCatalog(spec); err != nil {
		return SnapshotSpec{}, err
	}

	outcomeIDs := make(map[uuid.UUID]struct{})
	for actionIndex := range spec.Actions {
		action := &spec.Actions[actionIndex]
		for exampleIndex := range action.Examples {
			example := &action.Examples[exampleIndex]
			example.ObservedAt = example.ObservedAt.UTC()
			features, err := canonicalSnapshotFeatureValues(
				example.Features,
				spec.FeatureSchema,
			)
			if err != nil {
				return SnapshotSpec{}, fmt.Errorf(
					"adaptive snapshot action %q example %s: %w",
					action.ActionID,
					example.OutcomeID,
					err,
				)
			}
			example.Features = features
			example.Utility = canonicalZero(example.Utility)
			if err := validateSnapshotExample(spec.Scope, *example); err != nil {
				return SnapshotSpec{}, fmt.Errorf(
					"adaptive snapshot action %q: %w",
					action.ActionID,
					err,
				)
			}
			if _, duplicate := outcomeIDs[example.OutcomeID]; duplicate {
				return SnapshotSpec{}, fmt.Errorf(
					"adaptive snapshot outcome %s is duplicated",
					example.OutcomeID,
				)
			}
			outcomeIDs[example.OutcomeID] = struct{}{}
		}
		slices.SortFunc(action.Examples, func(left, right SnapshotExample) int {
			return slices.Compare(left.OutcomeID[:], right.OutcomeID[:])
		})
	}
	return spec, nil
}

func validateSnapshotScope(scope SnapshotScope) error {
	switch {
	case scope.OwnerID == uuid.Nil:
		return errors.New("adaptive snapshot owner_id is required")
	case !scope.Domain.valid():
		return fmt.Errorf("adaptive snapshot domain %q is invalid", scope.Domain)
	case !scope.Point.valid():
		return fmt.Errorf("adaptive snapshot point %q is invalid", scope.Point)
	case scope.Domain.point() != scope.Point:
		return fmt.Errorf(
			"adaptive snapshot domain %q does not bind point %q",
			scope.Domain,
			scope.Point,
		)
	case !validASCIIID(scope.ProviderID, maxProviderIDLength):
		return errors.New("adaptive snapshot provider_id is invalid")
	case !validASCIIID(scope.ModelID, maxModelIDLength):
		return errors.New("adaptive snapshot model_id is invalid")
	case !validASCIIID(scope.PolicyVersion, maxPolicyVersionIDLength):
		return errors.New("adaptive snapshot policy_version is invalid")
	case scope.TrainingCutoff.IsZero():
		return errors.New("adaptive snapshot training_cutoff is required")
	}
	return nil
}

func validateSnapshotFeatureSchema(schema []SnapshotFeatureDefinition) error {
	if len(schema) == 0 || len(schema) > maxSnapshotFeatures {
		return fmt.Errorf(
			"adaptive snapshot feature schema must contain 1 to %d entries",
			maxSnapshotFeatures,
		)
	}
	if !slices.IsSortedFunc(schema, func(left, right SnapshotFeatureDefinition) int {
		return stringCompare(string(left.Key), string(right.Key))
	}) {
		return errors.New("adaptive snapshot feature schema must be ordered")
	}
	seen := make(map[FeatureKey]struct{}, len(schema))
	for index := range schema {
		definition := &schema[index]
		definition.Center = canonicalZero(definition.Center)
		definition.Scale = canonicalZero(definition.Scale)
		if privatePayloadAlias(string(definition.Key)) {
			return fmt.Errorf("adaptive snapshot feature %q is private", definition.Key)
		}
		registered, exists := featureSchema[definition.Key]
		if !exists {
			return fmt.Errorf("adaptive snapshot feature %q is not registered", definition.Key)
		}
		if _, duplicate := seen[definition.Key]; duplicate {
			return fmt.Errorf("adaptive snapshot feature %q is duplicated", definition.Key)
		}
		seen[definition.Key] = struct{}{}
		if !finiteInRange(definition.Center, registered.minimum, registered.maximum) {
			return fmt.Errorf("adaptive snapshot feature %q center is invalid", definition.Key)
		}
		if !finiteInRange(definition.Scale, math.SmallestNonzeroFloat64, maxSnapshotFeatureScale) {
			return fmt.Errorf("adaptive snapshot feature %q scale is invalid", definition.Key)
		}
		if !snapshotNormalizationRangeIsFinite(*definition, registered) {
			return fmt.Errorf(
				"adaptive snapshot feature %q normalization range is invalid",
				definition.Key,
			)
		}
	}
	return nil
}

func snapshotNormalizationRangeIsFinite(
	definition SnapshotFeatureDefinition,
	registered featureSpec,
) bool {
	minimum := (registered.minimum - definition.Center) / definition.Scale
	maximum := (registered.maximum - definition.Center) / definition.Scale
	return !math.IsNaN(minimum) && !math.IsInf(minimum, 0) &&
		!math.IsNaN(maximum) && !math.IsInf(maximum, 0)
}

func validateSnapshotActionCatalog(spec SnapshotSpec) error {
	if spec.ChampionActionID != StaticActionID {
		return fmt.Errorf("adaptive snapshot champion must be %q", StaticActionID)
	}
	if len(spec.Actions) == 0 || len(spec.Actions) > maxSnapshotActions {
		return fmt.Errorf(
			"adaptive snapshot action catalog must contain 1 to %d entries",
			maxSnapshotActions,
		)
	}
	if !slices.IsSortedFunc(spec.Actions, func(left, right SnapshotAction) int {
		return stringCompare(left.ActionID, right.ActionID)
	}) {
		return errors.New("adaptive snapshot action catalog must be ordered")
	}
	seen := make(map[string]struct{}, len(spec.Actions))
	for _, action := range spec.Actions {
		if !validSafeSlug(action.ActionID, maxActionIDLength) ||
			action.ActionID == ActionNoneID ||
			privatePayloadAlias(action.ActionID) ||
			sensitiveCatalogID(action.ActionID) {
			return fmt.Errorf("adaptive snapshot action ID %q is invalid", action.ActionID)
		}
		if _, duplicate := seen[action.ActionID]; duplicate {
			return fmt.Errorf("adaptive snapshot action %q is duplicated", action.ActionID)
		}
		seen[action.ActionID] = struct{}{}
		if len(action.Examples) > maxSnapshotExamplesPerAction {
			return fmt.Errorf(
				"adaptive snapshot action %q exceeds %d examples",
				action.ActionID,
				maxSnapshotExamplesPerAction,
			)
		}
	}
	if _, exists := seen[StaticActionID]; !exists {
		return fmt.Errorf("adaptive snapshot action catalog must include %q", StaticActionID)
	}
	return nil
}

func validateSnapshotExample(scope SnapshotScope, example SnapshotExample) error {
	switch {
	case example.OutcomeID == uuid.Nil:
		return errors.New("adaptive snapshot outcome_id is required")
	case example.OwnerID != scope.OwnerID:
		return errors.New("adaptive snapshot example owner does not match")
	case example.Domain != scope.Domain:
		return errors.New("adaptive snapshot example domain does not match")
	case example.Point != scope.Point:
		return errors.New("adaptive snapshot example point does not match")
	case example.ProviderID != scope.ProviderID:
		return errors.New("adaptive snapshot example provider does not match")
	case example.ModelID != scope.ModelID:
		return errors.New("adaptive snapshot example model does not match")
	case example.ObservedAt.IsZero():
		return errors.New("adaptive snapshot example observed_at is required")
	case example.ObservedAt.After(scope.TrainingCutoff):
		return errors.New("adaptive snapshot example is after training cutoff")
	case !example.Eligible:
		return errors.New("adaptive snapshot example is not eligible")
	case !finiteInRange(example.Utility, 0, 1):
		return errors.New("adaptive snapshot example utility is invalid")
	}
	return nil
}

func canonicalSnapshotFeatureValues(
	values []SnapshotFeatureValue,
	schema []SnapshotFeatureDefinition,
) ([]SnapshotFeatureValue, error) {
	if len(values) != len(schema) {
		return nil, errors.New("feature dimensions do not match schema")
	}
	byKey := make(map[FeatureKey]float64, len(values))
	for _, feature := range values {
		if privatePayloadAlias(string(feature.Key)) {
			return nil, fmt.Errorf("feature %q is private", feature.Key)
		}
		registered, exists := featureSchema[feature.Key]
		if !exists {
			return nil, fmt.Errorf("feature %q is not registered", feature.Key)
		}
		if _, duplicate := byKey[feature.Key]; duplicate {
			return nil, fmt.Errorf("feature %q is duplicated", feature.Key)
		}
		if !finiteInRange(feature.Value, registered.minimum, registered.maximum) ||
			(registered.integer && math.Trunc(feature.Value) != feature.Value) {
			return nil, fmt.Errorf("feature %q value is invalid", feature.Key)
		}
		byKey[feature.Key] = canonicalZero(feature.Value)
	}
	canonical := make([]SnapshotFeatureValue, len(schema))
	for index, definition := range schema {
		value, exists := byKey[definition.Key]
		if !exists {
			return nil, fmt.Errorf("feature %q is missing", definition.Key)
		}
		canonical[index] = SnapshotFeatureValue{Key: definition.Key, Value: value}
	}
	return canonical, nil
}

func cloneSnapshotActions(actions []SnapshotAction) []SnapshotAction {
	cloned := make([]SnapshotAction, len(actions))
	for actionIndex, action := range actions {
		cloned[actionIndex].ActionID = action.ActionID
		cloned[actionIndex].Examples = make([]SnapshotExample, len(action.Examples))
		for exampleIndex, example := range action.Examples {
			example.Features = append([]SnapshotFeatureValue(nil), example.Features...)
			cloned[actionIndex].Examples[exampleIndex] = example
		}
	}
	return cloned
}

func finiteInRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		value >= minimum && value <= maximum
}

func stringCompare(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
