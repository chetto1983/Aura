package main

import (
	"math"
	"math/rand"
	"sort"
)

type aggregate struct {
	Policy             string  `json:"policy"`
	Condition          string  `json:"condition"`
	Profile            string  `json:"profile"`
	Seeds              int     `json:"seeds"`
	MeanReward         float64 `json:"mean_reward"`
	MeanRegret         float64 `json:"mean_regret"`
	RegretCILow        float64 `json:"regret_ci_low"`
	RegretCIHigh       float64 `json:"regret_ci_high"`
	MeanFinal100Regret float64 `json:"mean_final_100_regret"`
	MeanHarmfulRate    float64 `json:"mean_harmful_rate"`
	MeanOracleMatch    float64 `json:"mean_oracle_match"`
	MeanDecisionNanos  float64 `json:"mean_decision_nanos"`
	P95DecisionNanos   float64 `json:"p95_decision_nanos"`
}

type pairedDifference struct {
	Condition string  `json:"condition"`
	Profile   string  `json:"profile"`
	PolicyA   string  `json:"policy_a"`
	PolicyB   string  `json:"policy_b"`
	Metric    string  `json:"metric"`
	Mean      float64 `json:"mean"`
	CILow     float64 `json:"ci_low"`
	CIHigh    float64 `json:"ci_high"`
}

func aggregateResults(results []seedResult) []aggregate {
	grouped := make(map[string][]seedResult)
	for _, item := range results {
		key := item.Condition + "|" + item.Profile + "|" + item.Policy
		grouped[key] = append(grouped[key], item)
	}
	out := make([]aggregate, 0, len(grouped))
	for _, rows := range grouped {
		regrets := field(rows, func(item seedResult) float64 { return item.Regret })
		low, high := bootstrapMeanCI(regrets, 5000, 9801)
		decisionP95s := field(rows, func(item seedResult) float64 { return item.P95DecisionNanos })
		out = append(out, aggregate{
			Policy: rows[0].Policy, Condition: rows[0].Condition, Profile: rows[0].Profile, Seeds: len(rows),
			MeanReward: mean(field(rows, func(item seedResult) float64 { return item.Reward })),
			MeanRegret: mean(regrets), RegretCILow: low, RegretCIHigh: high,
			MeanFinal100Regret: mean(field(rows, func(item seedResult) float64 { return item.Final100Regret })),
			MeanHarmfulRate:    mean(field(rows, func(item seedResult) float64 { return item.HarmfulRate })),
			MeanOracleMatch:    mean(field(rows, func(item seedResult) float64 { return item.OracleMatchRate })),
			MeanDecisionNanos:  mean(field(rows, func(item seedResult) float64 { return item.MeanDecisionNanos })),
			P95DecisionNanos:   percentile(decisionP95s, 0.95),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Condition != out[j].Condition {
			return out[i].Condition < out[j].Condition
		}
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		return out[i].MeanRegret < out[j].MeanRegret
	})
	return out
}

func pairedRegret(results []seedResult, policyA, policyB string) []pairedDifference {
	type pair struct {
		a, b *seedResult
	}
	groups := make(map[string]map[int64]*pair)
	for i := range results {
		item := &results[i]
		if item.Policy != policyA && item.Policy != policyB {
			continue
		}
		key := item.Condition + "|" + item.Profile
		if groups[key] == nil {
			groups[key] = make(map[int64]*pair)
		}
		if groups[key][item.Seed] == nil {
			groups[key][item.Seed] = &pair{}
		}
		if item.Policy == policyA {
			groups[key][item.Seed].a = item
		} else {
			groups[key][item.Seed].b = item
		}
	}
	out := make([]pairedDifference, 0, len(groups))
	for _, pairs := range groups {
		diffs := make([]float64, 0, len(pairs))
		var example *pair
		for _, item := range pairs {
			if item.a == nil || item.b == nil {
				continue
			}
			example = item
			diffs = append(diffs, item.a.Regret-item.b.Regret)
		}
		if example == nil {
			continue
		}
		low, high := bootstrapMeanCI(diffs, 5000, 9811)
		out = append(out, pairedDifference{
			Condition: example.a.Condition, Profile: example.a.Profile,
			PolicyA: policyA, PolicyB: policyB, Metric: "cumulative_regret_a_minus_b",
			Mean: mean(diffs), CILow: low, CIHigh: high,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Condition != out[j].Condition {
			return out[i].Condition < out[j].Condition
		}
		return out[i].Profile < out[j].Profile
	})
	return out
}

func bootstrapMeanCI(values []float64, iterations int, seed int64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, iterations)
	for i := range means {
		var sum float64
		for range values {
			sum += values[rng.Intn(len(values))]
		}
		means[i] = sum / float64(len(values))
	}
	sort.Float64s(means)
	return percentile(means, 0.025), percentile(means, 0.975)
}

func field(rows []seedResult, fn func(seedResult) float64) []float64 {
	out := make([]float64, len(rows))
	for i, item := range rows {
		out[i] = fn(item)
	}
	return out
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(p*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func sortFloat64s(values []float64) { sort.Float64s(values) }
