package main

import (
	"math"
	"math/rand"
	"time"
)

type condition struct {
	Name  string
	Delay int
}

type seedResult struct {
	Policy            string  `json:"policy"`
	Condition         string  `json:"condition"`
	Profile           string  `json:"profile"`
	Seed              int64   `json:"seed"`
	Reward            float64 `json:"reward"`
	Regret            float64 `json:"regret"`
	Final100Regret    float64 `json:"final_100_regret"`
	HarmfulRate       float64 `json:"harmful_rate"`
	OracleMatchRate   float64 `json:"oracle_match_rate"`
	MeanDecisionNanos float64 `json:"mean_decision_nanos"`
	P95DecisionNanos  float64 `json:"p95_decision_nanos"`
}

type pendingFeedback struct {
	at       int
	action   string
	features []float64
	reward   float64
}

func runReplay(model *rewardModel, makePolicy func(int) policy, cond condition, profile string, seed int64, steps int) seedResult {
	rng := rand.New(rand.NewSource(seed))
	dim := len(contextFeatures(model.Scenarios[0], profile))
	p := makePolicy(dim)
	var totalReward, totalRegret, finalRegret float64
	var harmful, oracleMatches int
	var pending []pendingFeedback
	decisionTimes := make([]float64, 0, steps)
	for step := 0; step < steps; step++ {
		pending = deliverDue(p, pending, step)
		sc := model.Scenarios[rng.Intn(len(model.Scenarios))]
		features := contextFeatures(sc, profile)
		start := time.Now()
		chosen := p.Choose(sc, profile, features, rng)
		decisionTimes = append(decisionTimes, float64(time.Since(start).Nanoseconds()))
		bestAction, bestReward := model.best(sc, profile)
		chosenExpected := model.expected(sc, chosen.Action, profile)
		regret := math.Max(0, bestReward-chosenExpected)
		totalRegret += regret
		if step >= steps-100 {
			finalRegret += regret
		}
		if chosen.Action == bestAction {
			oracleMatches++
		}
		bestQuality := model.expectedQuality(sc, bestAction)
		chosenQuality := model.expectedQuality(sc, chosen.Action)
		if bestQuality-chosenQuality >= 0.5 {
			harmful++
		}
		samples := sc.Samples[chosen.Action]
		sample := samples[rng.Intn(len(samples))]
		reward := sample.Rewards[profile]
		totalReward += reward
		pending = append(pending, pendingFeedback{
			at: step + cond.Delay, action: chosen.Action, features: features, reward: reward,
		})
	}
	deliverDue(p, pending, math.MaxInt)
	sortFloat64s(decisionTimes)
	return seedResult{
		Policy: p.Name(), Condition: cond.Name, Profile: profile, Seed: seed,
		Reward: totalReward / float64(steps), Regret: totalRegret,
		Final100Regret: finalRegret, HarmfulRate: float64(harmful) / float64(steps),
		OracleMatchRate:   float64(oracleMatches) / float64(steps),
		MeanDecisionNanos: mean(decisionTimes), P95DecisionNanos: percentile(decisionTimes, 0.95),
	}
}

func deliverDue(p policy, pending []pendingFeedback, step int) []pendingFeedback {
	kept := pending[:0]
	for _, item := range pending {
		if item.at <= step {
			p.Update(item.action, item.features, item.reward)
		} else {
			kept = append(kept, item)
		}
	}
	return kept
}
