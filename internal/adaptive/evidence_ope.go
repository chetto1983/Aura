package adaptive

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
)

// OPEAction is one ordered action row for selected-intention evaluation.
type OPEAction struct {
	ActionID string
	Behavior float64
	Target   float64
	QHat     float64
}

// OPERow is one canonically ordered closed-ledger member.
type OPERow struct {
	MemberID         string
	ClusterID        string
	IntendedActionID string
	Reward           float64
	Actions          []OPEAction
}

// OPEResult contains the frozen diagnostic estimators.
type OPEResult struct {
	DR    float64
	SNIPS float64
	ESS   float64
}

// EvaluateSelectedIntentionOPE computes DR, SNIPS, and importance ESS.
func EvaluateSelectedIntentionOPE(rows []OPERow) (OPEResult, error) {
	if len(rows) == 0 {
		return OPEResult{}, errors.New("OPE row set is empty")
	}
	memberTerms := make([]float64, len(rows))
	weightedRewards := make([]float64, len(rows))
	weights := make([]float64, len(rows))
	squaredWeights := make([]float64, len(rows))
	previousMemberID := ""
	for rowIndex, row := range rows {
		if row.MemberID == "" || row.ClusterID == "" || row.MemberID <= previousMemberID {
			return OPEResult{}, errors.New("OPE rows are not in unique canonical member order")
		}
		previousMemberID = row.MemberID
		if !finite(row.Reward) || row.Reward < 0 || row.Reward > 1 || len(row.Actions) == 0 {
			return OPEResult{}, errors.New("OPE row reward or action set is invalid")
		}
		qTerms := make([]float64, len(row.Actions))
		previousActionID := ""
		intendedIndex := -1
		for actionIndex, action := range row.Actions {
			if action.ActionID == "" || action.ActionID <= previousActionID {
				return OPEResult{}, errors.New("OPE actions are not in unique canonical order")
			}
			previousActionID = action.ActionID
			if !finite(action.Behavior) || !finite(action.Target) || !finite(action.QHat) ||
				action.Behavior < 0 || action.Behavior > 1 ||
				action.Target < 0 || action.Target > 1 ||
				action.QHat < 0 || action.QHat > 1 {
				return OPEResult{}, errors.New("OPE action value is invalid")
			}
			if action.Target > 0 && action.Behavior <= 0 {
				return OPEResult{}, errors.New("OPE target action lacks behavior overlap")
			}
			if action.ActionID == row.IntendedActionID {
				intendedIndex = actionIndex
			}
			qTerms[actionIndex] = action.Target * action.QHat
		}
		if intendedIndex < 0 || row.Actions[intendedIndex].Behavior <= 0 {
			return OPEResult{}, errors.New("OPE intended action lacks positive behavior probability")
		}
		targetSum, err := NeumaierSum(targetProbabilities(row.Actions))
		if err != nil || math.Abs(targetSum-1) > 1e-12 {
			return OPEResult{}, errors.New("OPE target probabilities do not sum to one")
		}
		qTarget, err := NeumaierSum(qTerms)
		if err != nil {
			return OPEResult{}, err
		}
		intended := row.Actions[intendedIndex]
		weight := intended.Target / intended.Behavior
		memberTerms[rowIndex] = qTarget + weight*(row.Reward-intended.QHat)
		weightedRewards[rowIndex] = weight * row.Reward
		weights[rowIndex] = weight
		squaredWeights[rowIndex] = weight * weight
	}
	drSum, err := NeumaierSum(memberTerms)
	if err != nil {
		return OPEResult{}, err
	}
	rewardSum, err := NeumaierSum(weightedRewards)
	if err != nil {
		return OPEResult{}, err
	}
	weightSum, err := NeumaierSum(weights)
	if err != nil || weightSum <= 0 {
		return OPEResult{}, errors.New("OPE SNIPS denominator is nonpositive")
	}
	squaredWeightSum, err := NeumaierSum(squaredWeights)
	if err != nil || squaredWeightSum <= 0 {
		return OPEResult{}, errors.New("OPE squared-weight denominator is nonpositive")
	}
	return OPEResult{
		DR:    drSum / float64(len(rows)),
		SNIPS: rewardSum / weightSum,
		ESS:   weightSum * weightSum / squaredWeightSum,
	}, nil
}

func targetProbabilities(actions []OPEAction) []float64 {
	values := make([]float64, len(actions))
	for index, action := range actions {
		values[index] = action.Target
	}
	return values
}

// SampleClusterIndices implements the SHA-256 counter/rejection sampler.
func SampleClusterIndices(seed [32]byte, clusterCount, sampleCount int) ([]int, error) {
	if clusterCount <= 0 || sampleCount < 0 {
		return nil, errors.New("cluster sampler counts are invalid")
	}
	if uint64(clusterCount) > math.MaxUint64 {
		return nil, errors.New("cluster sampler count exceeds uint64")
	}
	threshold := -uint64(clusterCount) % uint64(clusterCount)
	indices := make([]int, 0, sampleCount)
	var counter uint64
	var counterBytes [8]byte
	for len(indices) < sampleCount {
		binary.BigEndian.PutUint64(counterBytes[:], counter)
		preimage := make([]byte, 0, len(seed)+len(counterBytes))
		preimage = append(preimage, seed[:]...)
		preimage = append(preimage, counterBytes[:]...)
		digest := sha256.Sum256(preimage)
		counter++
		for offset := 0; offset < len(digest) && len(indices) < sampleCount; offset += 8 {
			word := binary.BigEndian.Uint64(digest[offset : offset+8])
			if word < threshold {
				continue
			}
			indices = append(indices, int(word%uint64(clusterCount)))
		}
	}
	return indices, nil
}

// BootstrapInterval is the frozen outward percentile order-statistic pair.
type BootstrapInterval struct {
	Lower float64
	Upper float64
}

// ClusterBootstrapDR resamples whole canonical clusters and returns the DR interval.
func ClusterBootstrapDR(seed [32]byte, rows []OPERow, replicates int) (BootstrapInterval, error) {
	if replicates != 10_000 {
		return BootstrapInterval{}, errors.New("cluster bootstrap requires exactly 10000 replicates")
	}
	clusters, err := partitionOPEClusters(rows)
	if err != nil {
		return BootstrapInterval{}, err
	}
	if len(clusters) < 2 {
		return BootstrapInterval{}, errors.New("cluster bootstrap requires at least two clusters")
	}
	indices, err := SampleClusterIndices(seed, len(clusters), replicates*len(clusters))
	if err != nil {
		return BootstrapInterval{}, err
	}
	values := make([]bootstrapValue, replicates)
	for replicate := range replicates {
		sampled := make([]OPERow, 0, len(rows))
		for draw := range len(clusters) {
			sampled = append(sampled, clusters[indices[replicate*len(clusters)+draw]]...)
		}
		for index := range sampled {
			sampled[index].MemberID = fmt.Sprintf("%08d/%08d", index, replicate)
		}
		result, err := EvaluateSelectedIntentionOPE(sampled)
		if err != nil || !finite(result.DR) {
			return BootstrapInterval{}, errors.New("cluster bootstrap produced an invalid replicate")
		}
		values[replicate] = bootstrapValue{value: result.DR, index: replicate}
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

func partitionOPEClusters(rows []OPERow) ([][]OPERow, error) {
	if len(rows) == 0 {
		return nil, errors.New("cluster bootstrap row set is empty")
	}
	clusters := make([][]OPERow, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		if len(clusters) == 0 || clusters[len(clusters)-1][0].ClusterID != row.ClusterID {
			if _, duplicate := seen[row.ClusterID]; duplicate {
				return nil, errors.New("cluster rows are not contiguous in canonical order")
			}
			seen[row.ClusterID] = struct{}{}
			clusters = append(clusters, nil)
		}
		clusters[len(clusters)-1] = append(clusters[len(clusters)-1], row)
	}
	return clusters, nil
}
