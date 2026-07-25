package adaptive

import (
	"errors"
	"math"

	"github.com/google/uuid"
)

const maxSnapshotNeighbors = 5

var (
	errInvalidSnapshotVector = errors.New("adaptive snapshot vector is invalid")
	errZeroSnapshotVector    = errors.New("adaptive snapshot vector is zero")
)

// SnapshotDecisionReason identifies why local scoring selected or fell back.
type SnapshotDecisionReason string

// Snapshot decision reasons distinguish learned selection from fail-closed fallback.
const (
	SnapshotScored              SnapshotDecisionReason = "scored"
	SnapshotStaticNoSupport     SnapshotDecisionReason = "static_no_support"
	SnapshotStaticInvalid       SnapshotDecisionReason = "static_snapshot_invalid"
	SnapshotStaticScopeMismatch SnapshotDecisionReason = "static_scope_mismatch"
	SnapshotStaticQueryInvalid  SnapshotDecisionReason = "static_query_invalid"
)

// SnapshotQuery contains one serving scope and its bounded registered features.
type SnapshotQuery struct {
	OwnerID       uuid.UUID
	Domain        Domain
	Point         DecisionPoint
	ProviderID    string
	ModelID       string
	PolicyVersion string
	Features      []SnapshotFeatureValue
}

// SnapshotActionScore reports bounded support and weighted utility for one action.
type SnapshotActionScore struct {
	ActionID  string
	Utility   float64
	Neighbors int
	Supported bool
}

// SnapshotDecision contains the selected action and deterministic score diagnostics.
type SnapshotDecision struct {
	ActionID string
	Reason   SnapshotDecisionReason
	Scores   []SnapshotActionScore
}

type snapshotNeighbor struct {
	outcomeID  uuid.UUID
	similarity float64
	utility    float64
}

type snapshotScoringAction struct {
	actionID string
	examples []snapshotScoringExample
}

type snapshotScoringExample struct {
	outcomeID uuid.UUID
	utility   float64
	unit      []float64
}

// Score chooses from frozen support or returns the explicit static champion.
func (snapshot *PolicySnapshot) Score(query SnapshotQuery) SnapshotDecision {
	if snapshot == nil ||
		!snapshot.verified ||
		snapshot.id == uuid.Nil ||
		!validSHA256(snapshot.sha256) ||
		snapshot.championActionID != StaticActionID ||
		len(snapshot.scoringActions) != len(snapshot.actions) {
		return staticSnapshotDecision(SnapshotStaticInvalid, nil)
	}
	if !snapshot.scopeMatches(query) {
		return staticSnapshotDecision(
			SnapshotStaticScopeMismatch,
			snapshot.emptyActionScores(),
		)
	}
	queryVector, err := normalizedSnapshotVector(
		query.Features,
		snapshot.featureSchema,
	)
	if err != nil {
		return staticSnapshotDecision(
			SnapshotStaticQueryInvalid,
			snapshot.emptyActionScores(),
		)
	}
	queryUnit, err := snapshotUnitVector(queryVector)
	if errors.Is(err, errZeroSnapshotVector) {
		return staticSnapshotDecision(
			SnapshotStaticNoSupport,
			snapshot.emptyActionScores(),
		)
	}
	if err != nil {
		return staticSnapshotDecision(
			SnapshotStaticQueryInvalid,
			snapshot.emptyActionScores(),
		)
	}

	scores := make([]SnapshotActionScore, len(snapshot.actions))
	selectedIndex := -1
	for actionIndex, action := range snapshot.scoringActions {
		if action.actionID != snapshot.actions[actionIndex].ActionID ||
			len(action.examples) != len(snapshot.actions[actionIndex].Examples) {
			return staticSnapshotDecision(
				SnapshotStaticInvalid,
				snapshot.emptyActionScores(),
			)
		}
		score, valid := scoreSnapshotAction(action, queryUnit)
		if !valid {
			return staticSnapshotDecision(
				SnapshotStaticInvalid,
				snapshot.emptyActionScores(),
			)
		}
		scores[actionIndex] = score
		if !score.Supported {
			continue
		}
		if selectedIndex == -1 || score.Utility > scores[selectedIndex].Utility {
			selectedIndex = actionIndex
		}
	}
	if selectedIndex == -1 {
		return staticSnapshotDecision(SnapshotStaticNoSupport, scores)
	}
	return SnapshotDecision{
		ActionID: scores[selectedIndex].ActionID,
		Reason:   SnapshotScored,
		Scores:   scores,
	}
}

func (snapshot *PolicySnapshot) scopeMatches(query SnapshotQuery) bool {
	return query.OwnerID != uuid.Nil &&
		query.OwnerID == snapshot.scope.OwnerID &&
		query.Domain == snapshot.scope.Domain &&
		query.Point == snapshot.scope.Point &&
		query.ProviderID == snapshot.scope.ProviderID &&
		query.ModelID == snapshot.scope.ModelID &&
		query.PolicyVersion == snapshot.scope.PolicyVersion
}

func (snapshot *PolicySnapshot) emptyActionScores() []SnapshotActionScore {
	scores := make([]SnapshotActionScore, len(snapshot.actions))
	for index, action := range snapshot.actions {
		scores[index].ActionID = action.ActionID
	}
	return scores
}

func staticSnapshotDecision(
	reason SnapshotDecisionReason,
	scores []SnapshotActionScore,
) SnapshotDecision {
	return SnapshotDecision{
		ActionID: StaticActionID,
		Reason:   reason,
		Scores:   scores,
	}
}

func scoreSnapshotAction(
	action snapshotScoringAction,
	queryUnit []float64,
) (SnapshotActionScore, bool) {
	score := SnapshotActionScore{ActionID: action.actionID}
	var neighbors [maxSnapshotNeighbors]snapshotNeighbor
	neighborCount := 0
	for _, example := range action.examples {
		similarity, valid := snapshotUnitCosine(queryUnit, example.unit)
		if !valid || !finiteInRange(example.utility, 0, 1) {
			return SnapshotActionScore{}, false
		}
		if similarity <= 0 {
			continue
		}
		neighborCount = insertSnapshotNeighbor(
			&neighbors,
			neighborCount,
			snapshotNeighbor{
				outcomeID:  example.outcomeID,
				similarity: similarity,
				utility:    example.utility,
			},
		)
	}

	weightedUtility := 0.0
	totalSimilarity := 0.0
	for _, neighbor := range neighbors[:neighborCount] {
		weightedUtility += neighbor.similarity * neighbor.utility
		totalSimilarity += neighbor.similarity
		score.Neighbors++
	}
	if totalSimilarity <= 0 {
		return score, true
	}
	score.Utility = canonicalZero(weightedUtility / totalSimilarity)
	if math.IsNaN(score.Utility) || math.IsInf(score.Utility, 0) {
		return SnapshotActionScore{}, false
	}
	score.Supported = true
	return score, true
}

func normalizedSnapshotVector(
	features []SnapshotFeatureValue,
	schema []SnapshotFeatureDefinition,
) ([]float64, error) {
	canonical, err := canonicalSnapshotFeatureValues(features, schema)
	if err != nil {
		return nil, err
	}
	return denseCanonicalSnapshotVector(canonical, schema)
}

func buildSnapshotScoringActions(
	actions []SnapshotAction,
	schema []SnapshotFeatureDefinition,
) ([]snapshotScoringAction, error) {
	scoring := make([]snapshotScoringAction, len(actions))
	for actionIndex, action := range actions {
		scoring[actionIndex].actionID = action.ActionID
		scoring[actionIndex].examples = make(
			[]snapshotScoringExample,
			len(action.Examples),
		)
		for exampleIndex, example := range action.Examples {
			vector, err := denseCanonicalSnapshotVector(example.Features, schema)
			if err != nil {
				return nil, err
			}
			unit, err := snapshotUnitVector(vector)
			if err != nil {
				return nil, errors.New(
					"adaptive snapshot frozen example vector is invalid",
				)
			}
			scoring[actionIndex].examples[exampleIndex] = snapshotScoringExample{
				outcomeID: example.OutcomeID,
				utility:   example.Utility,
				unit:      unit,
			}
		}
	}
	return scoring, nil
}

func denseCanonicalSnapshotVector(
	features []SnapshotFeatureValue,
	schema []SnapshotFeatureDefinition,
) ([]float64, error) {
	if len(features) != len(schema) {
		return nil, errInvalidSnapshotVector
	}
	vector := make([]float64, len(schema))
	for index, definition := range schema {
		if features[index].Key != definition.Key {
			return nil, errInvalidSnapshotVector
		}
		value := (features[index].Value - definition.Center) / definition.Scale
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errInvalidSnapshotVector
		}
		vector[index] = canonicalZero(value)
	}
	return vector, nil
}

func snapshotUnitVector(vector []float64) ([]float64, error) {
	maximum := 0.0
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errInvalidSnapshotVector
		}
		maximum = max(maximum, math.Abs(value))
	}
	if maximum == 0 {
		return nil, errZeroSnapshotVector
	}
	scaledSquares := 0.0
	for _, value := range vector {
		scaled := value / maximum
		scaledSquares += scaled * scaled
	}
	norm := math.Sqrt(scaledSquares)
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, errInvalidSnapshotVector
	}
	for index, value := range vector {
		vector[index] = (value / maximum) / norm
	}
	return vector, nil
}

func snapshotUnitCosine(left, right []float64) (float64, bool) {
	if len(left) == 0 || len(left) != len(right) {
		return 0, false
	}
	dot := 0.0
	for index := range left {
		if math.IsNaN(left[index]) || math.IsInf(left[index], 0) ||
			math.IsNaN(right[index]) || math.IsInf(right[index], 0) {
			return 0, false
		}
		dot += left[index] * right[index]
	}
	if math.IsNaN(dot) || math.IsInf(dot, 0) {
		return 0, false
	}
	if dot > 1 {
		dot = 1
	}
	return canonicalZero(max(0, dot)), true
}

func insertSnapshotNeighbor(
	neighbors *[maxSnapshotNeighbors]snapshotNeighbor,
	count int,
	candidate snapshotNeighbor,
) int {
	position := count
	for index := range count {
		if snapshotNeighborBefore(candidate, neighbors[index]) {
			position = index
			break
		}
	}
	if position >= maxSnapshotNeighbors {
		return count
	}
	if count < maxSnapshotNeighbors {
		count++
	}
	for index := count - 1; index > position; index-- {
		neighbors[index] = neighbors[index-1]
	}
	neighbors[position] = candidate
	return count
}

func snapshotNeighborBefore(left, right snapshotNeighbor) bool {
	if left.similarity != right.similarity {
		return left.similarity > right.similarity
	}
	for index := range len(left.outcomeID) {
		if left.outcomeID[index] != right.outcomeID[index] {
			return left.outcomeID[index] < right.outcomeID[index]
		}
	}
	return false
}
