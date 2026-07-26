package adaptive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"
)

// OPEMemberOrder is the complete frozen closed-member sort key.
type OPEMemberOrder struct {
	AnalysisStratumID     string
	InterferenceClusterID string
	ClaimID               string
	RequestID             string
	AssignmentID          string
}

// OPERow is one closed-ledger member evaluated by selected intention.
type OPERow struct {
	Order               OPEMemberOrder
	TargetActionID      string
	IntendedActionID    string
	Reward              bool
	DeliveryKnown       bool
	ActionProbabilities []ExactActionProbability
}

// EndpointOutcomeModelBinding binds endpoint model content to its prediction vector.
type EndpointOutcomeModelBinding struct {
	EndpointKind           EvidenceEndpointKind
	ArtifactSHA256         string
	PredictionSHA256       string
	RewardDefinitionSHA256 string
	TargetPolicySHA256     string
}

// SelectedIntentionOPEPlan is the immutable policy/model contract for one endpoint.
type SelectedIntentionOPEPlan struct {
	EndpointKind           EvidenceEndpointKind
	ActionSchemaSHA256     string
	RewardDefinitionSHA256 string
	TargetPolicySHA256     string
	BehaviorPolicySHA256   string
	OutcomeModelSHA256     string
	PredictionSHA256       string
	MinimumESS             ExactRational
	MinimumOverlap         ExactRational
}

// SelectedIntentionOPEArtifacts contains the content-addressed plan dependencies.
type SelectedIntentionOPEArtifacts struct {
	ActionSchema     ActionSchemaArtifact
	RewardDefinition BinaryRewardDefinition
	TargetPolicy     TargetPolicyManifest
	BehaviorPolicy   BehaviorPolicyManifest
	Prediction       PredictionVector
	OutcomeModel     EndpointOutcomeModelBinding
}

// SelectedIntentionOPEInput is the complete store-independent endpoint input.
type SelectedIntentionOPEInput struct {
	Plan      SelectedIntentionOPEPlan
	Artifacts SelectedIntentionOPEArtifacts
	Rows      []OPERow
}

// OPEResult contains the frozen diagnostic estimators.
type OPEResult struct {
	DR    float64
	SNIPS float64
	ESS   float64
}

type preparedOPERow struct {
	clusterID      string
	drContribution float64
}

// EvaluateSelectedIntentionOPE computes the frozen deterministic-policy estimators.
func EvaluateSelectedIntentionOPE(input SelectedIntentionOPEInput) (OPEResult, error) {
	result, _, err := prepareSelectedIntentionOPE(input)
	return result, err
}

func prepareSelectedIntentionOPE(input SelectedIntentionOPEInput) (OPEResult, []preparedOPERow, error) {
	if err := validateOPEBindings(input.Plan, input.Artifacts); err != nil {
		return OPEResult{}, nil, err
	}
	rows, err := canonicalOPERows(input.Rows)
	if err != nil {
		return OPEResult{}, nil, err
	}
	qhat, err := predictionMap(input.Artifacts.Prediction)
	if err != nil {
		return OPEResult{}, nil, err
	}
	prepared := make([]preparedOPERow, len(rows))
	drTerms := make([]float64, len(rows))
	weightedRewards := make([]float64, len(rows))
	weights := make([]float64, len(rows))
	squaredWeights := make([]float64, len(rows))
	for index, row := range rows {
		probabilities, err := validateSelectedIntentionRow(
			row, input.Plan, input.Artifacts.ActionSchema.Actions,
		)
		if err != nil {
			return OPEResult{}, nil, fmt.Errorf("OPE row %d: %w", index, err)
		}
		targetQHat, ok := qhat[row.TargetActionID]
		if !ok {
			return OPEResult{}, nil, errors.New(
				"OPE row target action has no endpoint prediction",
			)
		}
		targetQ, err := targetQHat.Float64()
		if err != nil {
			return OPEResult{}, nil, err
		}
		behavior := probabilities[row.IntendedActionID]
		behaviorFloat, err := behavior.Float64()
		if err != nil {
			return OPEResult{}, nil, err
		}
		weight := 0.0
		if row.IntendedActionID == row.TargetActionID {
			weight = 1 / behaviorFloat
		}
		intendedQ, err := qhat[row.IntendedActionID].Float64()
		if err != nil {
			return OPEResult{}, nil, err
		}
		reward := 0.0
		if row.Reward {
			reward = 1
		}
		drTerm := targetQ + weight*(reward-intendedQ)
		prepared[index] = preparedOPERow{
			clusterID: row.Order.InterferenceClusterID, drContribution: drTerm,
		}
		drTerms[index] = drTerm
		weightedRewards[index] = weight * reward
		weights[index] = weight
		squaredWeights[index] = weight * weight
	}
	drSum, err := NeumaierSum(drTerms)
	if err != nil {
		return OPEResult{}, nil, err
	}
	rewardSum, err := NeumaierSum(weightedRewards)
	if err != nil {
		return OPEResult{}, nil, err
	}
	weightSum, err := NeumaierSum(weights)
	if err != nil || weightSum <= 0 {
		return OPEResult{}, nil, errors.New("OPE SNIPS denominator is nonpositive")
	}
	squaredWeightSum, err := NeumaierSum(squaredWeights)
	if err != nil || squaredWeightSum <= 0 {
		return OPEResult{}, nil, errors.New("OPE squared-weight denominator is nonpositive")
	}
	result := OPEResult{
		DR: drSum / float64(len(rows)), SNIPS: rewardSum / weightSum,
		ESS: weightSum * weightSum / squaredWeightSum,
	}
	minimumESS, err := input.Plan.MinimumESS.Float64()
	if err != nil || result.ESS < minimumESS {
		return OPEResult{}, nil, errors.New("OPE effective sample size is below the frozen minimum")
	}
	return result, prepared, nil
}

func validateOPEBindings(plan SelectedIntentionOPEPlan, artifacts SelectedIntentionOPEArtifacts) error {
	if plan.EndpointKind != EvidenceEndpointQuality && plan.EndpointKind != EvidenceEndpointHarm {
		return errors.New("OPE endpoint kind is invalid")
	}
	if plan.MinimumESS.Cmp(MustExactRational(0, 1)) <= 0 ||
		plan.MinimumOverlap.Cmp(MustExactRational(1, 2)) != 0 {
		return errors.New("OPE plan thresholds are invalid")
	}
	actionHash, err := artifacts.ActionSchema.SHA256()
	if err != nil {
		return err
	}
	rewardHash, err := artifacts.RewardDefinition.SHA256()
	if err != nil {
		return err
	}
	targetHash, err := artifacts.TargetPolicy.SHA256()
	if err != nil {
		return err
	}
	behaviorHash, err := artifacts.BehaviorPolicy.SHA256()
	if err != nil {
		return err
	}
	predictionHash, err := artifacts.Prediction.SHA256()
	if err != nil {
		return err
	}
	model := artifacts.OutcomeModel
	if actionHash != plan.ActionSchemaSHA256 ||
		rewardHash != plan.RewardDefinitionSHA256 ||
		targetHash != plan.TargetPolicySHA256 ||
		behaviorHash != plan.BehaviorPolicySHA256 ||
		predictionHash != plan.PredictionSHA256 ||
		model.ArtifactSHA256 != plan.OutcomeModelSHA256 {
		return errors.New("OPE plan content hash does not match its artifact")
	}
	if artifacts.RewardDefinition.EndpointKind != string(plan.EndpointKind) ||
		artifacts.Prediction.EndpointKind != string(plan.EndpointKind) ||
		model.EndpointKind != plan.EndpointKind ||
		artifacts.TargetPolicy.ActionSchemaSHA256 != actionHash ||
		artifacts.BehaviorPolicy.ActionSchemaSHA256 != actionHash ||
		artifacts.Prediction.ActionSchemaSHA256 != actionHash ||
		artifacts.Prediction.RewardDefinitionSHA256 != rewardHash ||
		model.PredictionSHA256 != predictionHash ||
		model.RewardDefinitionSHA256 != rewardHash ||
		model.TargetPolicySHA256 != targetHash ||
		!validSHA256(model.ArtifactSHA256) {
		return errors.New("OPE endpoint artifact binding is inconsistent")
	}
	if !slices.Contains(artifacts.ActionSchema.Actions, "static") {
		return errors.New("OPE static action is outside the frozen catalog")
	}
	return nil
}

func predictionMap(vector PredictionVector) (map[string]ExactRational, error) {
	predictions := make(map[string]ExactRational, len(vector.Predictions))
	for _, prediction := range vector.Predictions {
		predictions[prediction.ActionID] = prediction.QHat
	}
	return predictions, nil
}

func validateSelectedIntentionRow(
	row OPERow,
	plan SelectedIntentionOPEPlan,
	actions []string,
) (map[string]ExactRational, error) {
	if len(row.ActionProbabilities) != len(actions) {
		return nil, errors.New("behavior action catalog is incomplete")
	}
	probabilities := make(map[string]ExactRational, len(actions))
	for index, actionID := range actions {
		probability := row.ActionProbabilities[index]
		if probability.ActionID != actionID ||
			probability.Probability.Cmp(MustExactRational(0, 1)) < 0 ||
			probability.Probability.Cmp(MustExactRational(1, 1)) > 0 {
			return nil, errors.New("behavior action row is noncanonical")
		}
		expected := MustExactRational(0, 1)
		if row.TargetActionID == "static" && actionID == "static" {
			expected = MustExactRational(1, 1)
		} else if row.TargetActionID != "static" &&
			(actionID == "static" || actionID == row.TargetActionID) {
			expected = MustExactRational(1, 2)
		}
		if probability.Probability.Cmp(expected) != 0 {
			return nil, errors.New("behavior row is not the frozen two-arm marginal")
		}
		probabilities[actionID] = probability.Probability
	}
	intended, ok := probabilities[row.IntendedActionID]
	if !ok || intended.Cmp(MustExactRational(0, 1)) <= 0 {
		return nil, errors.New("intended action lacks behavior support")
	}
	targetBehavior, targetExists := probabilities[row.TargetActionID]
	if !targetExists {
		return nil, errors.New("target action is outside the behavior catalog")
	}
	if targetBehavior.Cmp(plan.MinimumOverlap) < 0 {
		return nil, errors.New("target action overlap is below the frozen minimum")
	}
	return probabilities, nil
}

func canonicalOPERows(rows []OPERow) ([]OPERow, error) {
	if len(rows) == 0 {
		return nil, errors.New("OPE row set is empty")
	}
	canonical := make([]OPERow, len(rows))
	for index, row := range rows {
		if !validSHA256(row.Order.AnalysisStratumID) ||
			!validSHA256(row.Order.InterferenceClusterID) {
			return nil, errors.New("OPE member stratum or cluster id is invalid")
		}
		if err := validateCanonicalUUID(row.Order.ClaimID); err != nil {
			return nil, errors.New("OPE claim id is invalid")
		}
		if err := validateCanonicalUUID(row.Order.RequestID); err != nil {
			return nil, errors.New("OPE request id is invalid")
		}
		if err := validateCanonicalUUID(row.Order.AssignmentID); err != nil {
			return nil, errors.New("OPE assignment id is invalid")
		}
		canonical[index] = row
		canonical[index].ActionProbabilities = slices.Clone(row.ActionProbabilities)
	}
	slices.SortFunc(canonical, func(left, right OPERow) int {
		return compareOPEMemberOrder(left.Order, right.Order)
	})
	for index := 1; index < len(canonical); index++ {
		if compareOPEMemberOrder(canonical[index-1].Order, canonical[index].Order) == 0 {
			return nil, errors.New("duplicate OPE member order key")
		}
	}
	return canonical, nil
}

func compareOPEMemberOrder(left, right OPEMemberOrder) int {
	leftFields := [...]string{
		left.AnalysisStratumID, left.InterferenceClusterID,
		left.ClaimID, left.RequestID, left.AssignmentID,
	}
	rightFields := [...]string{
		right.AnalysisStratumID, right.InterferenceClusterID,
		right.ClaimID, right.RequestID, right.AssignmentID,
	}
	for index := range leftFields {
		if leftFields[index] < rightFields[index] {
			return -1
		}
		if leftFields[index] > rightFields[index] {
			return 1
		}
	}
	return 0
}

// SampleClusterIndices exposes the frozen sampler vector without buffering digests.
func SampleClusterIndices(seed [32]byte, clusterCount, sampleCount int) ([]int, error) {
	sampler, err := newSHA256ClusterSampler(seed, clusterCount)
	if err != nil || sampleCount < 0 {
		return nil, errors.New("cluster sampler counts are invalid")
	}
	indices := make([]int, sampleCount)
	for index := range indices {
		indices[index] = sampler.next()
	}
	return indices, nil
}

type sha256ClusterSampler struct {
	seed         [32]byte
	clusterCount uint64
	threshold    uint64
	counter      uint64
	digest       [32]byte
	wordOffset   int
}

func newSHA256ClusterSampler(seed [32]byte, clusterCount int) (*sha256ClusterSampler, error) {
	if clusterCount <= 0 {
		return nil, errors.New("cluster count must be positive")
	}
	count := uint64(clusterCount)
	return &sha256ClusterSampler{
		seed: seed, clusterCount: count, threshold: -count % count, wordOffset: 4,
	}, nil
}

func (sampler *sha256ClusterSampler) next() int {
	for {
		if sampler.wordOffset == 4 {
			var counterBytes [8]byte
			binary.BigEndian.PutUint64(counterBytes[:], sampler.counter)
			preimage := make([]byte, 40)
			copy(preimage, sampler.seed[:])
			copy(preimage[32:], counterBytes[:])
			sampler.digest = sha256.Sum256(preimage)
			sampler.counter++
			sampler.wordOffset = 0
		}
		offset := sampler.wordOffset * 8
		word := binary.BigEndian.Uint64(sampler.digest[offset : offset+8])
		sampler.wordOffset++
		if word >= sampler.threshold {
			return int(word % sampler.clusterCount)
		}
	}
}

// BootstrapInterval is the frozen outward percentile order-statistic pair.
type BootstrapInterval struct {
	Lower float64
	Upper float64
}

// OPEUncertaintyCaps bounds deterministic bootstrap resources.
type OPEUncertaintyCaps struct {
	MaxMembers     int
	MaxClusters    int
	MaxMemoryBytes int64
	MaxWallTime    time.Duration
}

// DefaultOPEUncertaintyCaps returns the frozen production bootstrap limits.
func DefaultOPEUncertaintyCaps() OPEUncertaintyCaps {
	return OPEUncertaintyCaps{
		MaxMembers: 5_000, MaxClusters: 5_000,
		MaxMemoryBytes: 64 << 20, MaxWallTime: 10 * time.Second,
	}
}

type clusterContribution struct {
	id          string
	drSum       float64
	memberCount int
}

// ClusterBootstrapDR streams 10,000 whole-cluster replicates in bounded memory.
func ClusterBootstrapDR(
	ctx context.Context,
	seed [32]byte,
	input SelectedIntentionOPEInput,
	caps OPEUncertaintyCaps,
) (BootstrapInterval, error) {
	if err := ctx.Err(); err != nil {
		return BootstrapInterval{}, fmt.Errorf("cluster bootstrap cancelled: %w", err)
	}
	if len(input.Rows) > caps.MaxMembers || caps.MaxClusters <= 0 ||
		caps.MaxMemoryBytes <= 0 || caps.MaxWallTime <= 0 {
		return BootstrapInterval{}, errors.New("cluster bootstrap caps are invalid or exceeded")
	}
	_, rows, err := prepareSelectedIntentionOPE(input)
	if err != nil {
		return BootstrapInterval{}, err
	}
	clusters, err := aggregateOPEClusters(rows)
	if err != nil {
		return BootstrapInterval{}, err
	}
	if len(clusters) < 2 || len(clusters) > caps.MaxClusters {
		return BootstrapInterval{}, errors.New("cluster bootstrap requires a bounded multi-cluster cohort")
	}
	const replicates = 10_000
	estimatedMemory := int64(len(clusters))*64 + int64(replicates)*16
	if estimatedMemory > caps.MaxMemoryBytes {
		return BootstrapInterval{}, errors.New("cluster bootstrap memory cap exceeded")
	}
	sampler, err := newSHA256ClusterSampler(seed, len(clusters))
	if err != nil {
		return BootstrapInterval{}, err
	}
	startedAt := time.Now()
	values := make([]bootstrapValue, replicates)
	terms := make([]float64, len(clusters))
	for replicate := range replicates {
		if err := ctx.Err(); err != nil {
			return BootstrapInterval{}, fmt.Errorf("cluster bootstrap cancelled: %w", err)
		}
		if time.Since(startedAt) > caps.MaxWallTime {
			return BootstrapInterval{}, errors.New("cluster bootstrap wall-time cap exceeded")
		}
		memberCount := 0
		for draw := range len(clusters) {
			cluster := clusters[sampler.next()]
			terms[draw] = cluster.drSum
			memberCount += cluster.memberCount
		}
		sum, err := NeumaierSum(terms)
		if err != nil || memberCount == 0 {
			return BootstrapInterval{}, errors.New("cluster bootstrap produced an invalid replicate")
		}
		value := sum / float64(memberCount)
		if !finite(value) {
			return BootstrapInterval{}, errors.New("cluster bootstrap produced a nonfinite replicate")
		}
		values[replicate] = bootstrapValue{value: value, index: replicate}
	}
	slices.SortFunc(values, func(left, right bootstrapValue) int {
		if left.value < right.value {
			return -1
		}
		if left.value > right.value {
			return 1
		}
		return left.index - right.index
	})
	return BootstrapInterval{Lower: values[249].value, Upper: values[9750].value}, nil
}

type bootstrapValue struct {
	value float64
	index int
}

func aggregateOPEClusters(rows []preparedOPERow) ([]clusterContribution, error) {
	type clusterAccumulator struct {
		sum         neumaierAccumulator
		memberCount int
	}
	byID := make(map[string]clusterAccumulator)
	for _, row := range rows {
		cluster := byID[row.clusterID]
		if err := cluster.sum.add(row.drContribution); err != nil {
			return nil, err
		}
		cluster.memberCount++
		byID[row.clusterID] = cluster
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	clusters := make([]clusterContribution, len(ids))
	for index, id := range ids {
		cluster := byID[id]
		sum, err := cluster.sum.result()
		if err != nil {
			return nil, err
		}
		clusters[index] = clusterContribution{
			id: id, drSum: sum, memberCount: cluster.memberCount,
		}
	}
	return clusters, nil
}
