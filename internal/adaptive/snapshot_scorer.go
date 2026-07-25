package adaptive

import (
	"errors"
	"math"
	"slices"

	"github.com/google/uuid"
)

const maxSnapshotNeighbors = 5

var errInvalidSnapshotVector = errors.New("adaptive snapshot vector is invalid")

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

// Score chooses from frozen support or returns the explicit static champion.
func (snapshot *PolicySnapshot) Score(query SnapshotQuery) SnapshotDecision {
	if snapshot == nil ||
		!snapshot.verified ||
		snapshot.id == uuid.Nil ||
		!validSHA256(snapshot.sha256) ||
		snapshot.championActionID != StaticActionID {
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

	scores := make([]SnapshotActionScore, len(snapshot.actions))
	selectedIndex := -1
	for actionIndex, action := range snapshot.actions {
		score := scoreSnapshotAction(action, queryVector, snapshot.featureSchema)
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
	action SnapshotAction,
	queryVector []float64,
	schema []SnapshotFeatureDefinition,
) SnapshotActionScore {
	score := SnapshotActionScore{ActionID: action.ActionID}
	neighbors := make([]snapshotNeighbor, 0, len(action.Examples))
	for _, example := range action.Examples {
		vector, err := normalizedSnapshotVector(example.Features, schema)
		if err != nil {
			continue
		}
		similarity, valid := snapshotCosine(queryVector, vector)
		if !valid || similarity <= 0 {
			continue
		}
		neighbors = append(neighbors, snapshotNeighbor{
			outcomeID:  example.OutcomeID,
			similarity: similarity,
			utility:    example.Utility,
		})
	}
	slices.SortFunc(neighbors, func(left, right snapshotNeighbor) int {
		switch {
		case left.similarity > right.similarity:
			return -1
		case left.similarity < right.similarity:
			return 1
		default:
			return slices.Compare(left.outcomeID[:], right.outcomeID[:])
		}
	})
	if len(neighbors) > maxSnapshotNeighbors {
		neighbors = neighbors[:maxSnapshotNeighbors]
	}

	weightedUtility := 0.0
	totalSimilarity := 0.0
	for _, neighbor := range neighbors {
		if !finiteInRange(neighbor.utility, 0, 1) ||
			!finiteInRange(neighbor.similarity, math.SmallestNonzeroFloat64, 1) {
			continue
		}
		weightedUtility += neighbor.similarity * neighbor.utility
		totalSimilarity += neighbor.similarity
		score.Neighbors++
	}
	if totalSimilarity <= 0 {
		return score
	}
	score.Utility = canonicalZero(weightedUtility / totalSimilarity)
	if math.IsNaN(score.Utility) || math.IsInf(score.Utility, 0) {
		score.Utility = 0
		score.Neighbors = 0
		return score
	}
	score.Supported = true
	return score
}

func normalizedSnapshotVector(
	features []SnapshotFeatureValue,
	schema []SnapshotFeatureDefinition,
) ([]float64, error) {
	canonical, err := canonicalSnapshotFeatureValues(features, schema)
	if err != nil {
		return nil, err
	}
	vector := make([]float64, len(schema))
	for index, definition := range schema {
		value := (canonical[index].Value - definition.Center) / definition.Scale
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errInvalidSnapshotVector
		}
		vector[index] = canonicalZero(value)
	}
	return vector, nil
}

func snapshotCosine(left, right []float64) (float64, bool) {
	if len(left) == 0 || len(left) != len(right) {
		return 0, false
	}
	dot := 0.0
	leftNorm := 0.0
	rightNorm := 0.0
	for index := range left {
		if math.IsNaN(left[index]) || math.IsInf(left[index], 0) ||
			math.IsNaN(right[index]) || math.IsInf(right[index], 0) {
			return 0, false
		}
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0, false
	}
	similarity := dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
	if math.IsNaN(similarity) || math.IsInf(similarity, 0) {
		return 0, false
	}
	if similarity > 1 {
		similarity = 1
	}
	return canonicalZero(max(0, similarity)), true
}
