package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

type loggedBanditRow struct {
	contextID  string
	action     string
	reward     float64
	propensity float64
	target     string
	qChosen    float64
	qTarget    float64
}

func runOPE(
	holdout []scenario,
	live []outcome,
	staticChoices, targetChoices map[string]string,
	predictions map[string]map[string]float64,
) ([]opeMetric, bool, error) {
	byCell := make(map[string][]outcome)
	for _, result := range live {
		byCell[result.ScenarioID+"\x00"+result.Action] = append(byCell[result.ScenarioID+"\x00"+result.Action], result)
	}
	groundTruth := 0.0
	for _, sc := range holdout {
		rows := byCell[sc.ID+"\x00"+targetChoices[sc.ID]]
		value, _ := meanCell(rows)
		groundTruth += value
	}
	groundTruth /= float64(len(holdout))

	const (
		nRows   = 40_000
		explore = 0.30
	)
	rng := rand.New(rand.NewSource(10202))
	logged := make([]loggedBanditRow, 0, nRows)
	for i := 0; i < nRows; i++ {
		sc := holdout[rng.Intn(len(holdout))]
		available := actions(sc.Domain)
		baseline := staticChoices[sc.ID]
		probs := make(map[string]float64, len(available))
		for _, action := range available {
			probs[action] = explore / float64(len(available))
		}
		probs[baseline] += 1 - explore
		draw := rng.Float64()
		chosen := available[len(available)-1]
		cumulative := 0.0
		for _, action := range available {
			cumulative += probs[action]
			if draw <= cumulative {
				chosen = action
				break
			}
		}
		cell := byCell[sc.ID+"\x00"+chosen]
		if len(cell) == 0 {
			return nil, false, fmt.Errorf("OPE: missing live cell %s/%s", sc.ID, chosen)
		}
		reward := cell[rng.Intn(len(cell))].Rewards[proofProfile]
		logged = append(logged, loggedBanditRow{
			contextID:  sc.ID,
			action:     chosen,
			reward:     reward,
			propensity: probs[chosen],
			target:     targetChoices[sc.ID],
			qChosen:    predictions[sc.ID][chosen],
			qTarget:    predictions[sc.ID][targetChoices[sc.ID]],
		})
	}
	if err := validateOverlap(logged); err != nil {
		return nil, false, err
	}

	metrics := make([]opeMetric, 0, 3)
	for _, estimator := range []string{"ips", "snips", "doubly_robust"} {
		estimate, ess, maxWeight, minProp := estimateOPE(logged, estimator)
		lo, hi := bootstrapOPE(logged, estimator, 1_000, 200, 10203)
		metrics = append(metrics, opeMetric{
			Estimator:     estimator,
			Estimate:      estimate,
			GroundTruth:   groundTruth,
			AbsoluteError: math.Abs(estimate - groundTruth),
			EffectiveN:    ess,
			MaxWeight:     maxWeight,
			OverlapMin:    minProp,
			Supported:     true,
			CILower:       lo,
			CIUpper:       hi,
			CoversTruth:   lo <= groundTruth && groundTruth <= hi,
		})
	}

	// Negative control: deterministic production logging has probability zero for
	// target-only alternatives. The validator must reject it instead of producing a
	// misleading promotion estimate.
	unsupported := make([]loggedBanditRow, 0, len(holdout))
	for _, sc := range holdout {
		unsupported = append(unsupported, loggedBanditRow{
			contextID:  sc.ID,
			action:     staticChoices[sc.ID],
			reward:     0,
			propensity: 1,
			target:     targetChoices[sc.ID],
		})
	}
	deterministicRejected := validateOverlap(unsupported) != nil
	return metrics, deterministicRejected, nil
}

func validateOverlap(rows []loggedBanditRow) error {
	if len(rows) == 0 {
		return fmt.Errorf("OPE: empty log")
	}
	seenContext := map[string]map[string]bool{}
	for _, row := range rows {
		if row.propensity <= 0 || row.propensity > 1 || math.IsNaN(row.propensity) {
			return fmt.Errorf("OPE: invalid propensity for %s/%s: %v", row.contextID, row.action, row.propensity)
		}
		if seenContext[row.contextID] == nil {
			seenContext[row.contextID] = map[string]bool{}
		}
		if row.action == row.target {
			seenContext[row.contextID][row.target] = true
		}
	}
	for _, row := range rows {
		if !seenContext[row.contextID][row.target] {
			return fmt.Errorf("OPE: target action %s has no logging support for context %s", row.target, row.contextID)
		}
	}
	return nil
}

func estimateOPE(rows []loggedBanditRow, estimator string) (estimate, ess, maxWeight, minProp float64) {
	minProp = 1
	var weightedReward, weights, weightsSquared float64
	for _, row := range rows {
		if row.propensity < minProp {
			minProp = row.propensity
		}
		w := 0.0
		if row.action == row.target {
			w = 1 / row.propensity
		}
		maxWeight = math.Max(maxWeight, w)
		weights += w
		weightsSquared += w * w
		switch estimator {
		case "ips":
			weightedReward += w * row.reward
		case "snips":
			weightedReward += w * row.reward
		case "doubly_robust":
			weightedReward += row.qTarget + w*(row.reward-row.qChosen)
		}
	}
	switch estimator {
	case "snips":
		if weights > 0 {
			estimate = weightedReward / weights
		}
	default:
		estimate = weightedReward / float64(len(rows))
	}
	if weightsSquared > 0 {
		ess = weights * weights / weightsSquared
	}
	return estimate, ess, maxWeight, minProp
}

func bootstrapOPE(rows []loggedBanditRow, estimator string, iterations, blocks int, seed int64) (float64, float64) {
	if blocks > len(rows) {
		blocks = len(rows)
	}
	blockSize := len(rows) / blocks
	grouped := make([][]loggedBanditRow, 0, blocks)
	for i := 0; i < blocks; i++ {
		start := i * blockSize
		end := start + blockSize
		if i == blocks-1 {
			end = len(rows)
		}
		grouped = append(grouped, rows[start:end])
	}
	rng := rand.New(rand.NewSource(seed))
	samples := make([]float64, iterations)
	resampled := make([]loggedBanditRow, 0, len(rows))
	for i := range samples {
		resampled = resampled[:0]
		for range grouped {
			resampled = append(resampled, grouped[rng.Intn(len(grouped))]...)
		}
		samples[i], _, _, _ = estimateOPE(resampled, estimator)
	}
	sort.Float64s(samples)
	return quantile(samples, 0.025), quantile(samples, 0.975)
}
