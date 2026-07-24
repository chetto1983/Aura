package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

type stressCondition struct {
	name      string
	delay     int
	noise     float64
	drift     bool
	storeDown bool
}

type pending struct {
	at       int
	action   string
	features []float64
	reward   float64
}

type seedResult struct {
	Policy         string  `json:"policy"`
	Condition      string  `json:"condition"`
	Seed           int64   `json:"seed"`
	Regret         float64 `json:"regret"`
	Final100Regret float64 `json:"final_100_regret"`
	HarmfulRate    float64 `json:"harmful_rate"`
	FallbackRate   float64 `json:"fallback_rate"`
	AdaptationLag  int     `json:"adaptation_lag"`
	Rollbacks      int     `json:"rollbacks"`
}

type aggregate struct {
	Policy         string  `json:"policy"`
	Condition      string  `json:"condition"`
	MeanRegret     float64 `json:"mean_regret"`
	CILow          float64 `json:"ci_low"`
	CIHigh         float64 `json:"ci_high"`
	Final100Regret float64 `json:"final_100_regret"`
	HarmfulRate    float64 `json:"harmful_rate"`
	FallbackRate   float64 `json:"fallback_rate"`
	AdaptationLag  float64 `json:"adaptation_lag"`
	MeanRollbacks  float64 `json:"mean_rollbacks"`
}

type report struct {
	SchemaVersion string       `json:"schema_version"`
	CreatedAt     time.Time    `json:"created_at"`
	Seeds         int          `json:"seeds"`
	Steps         int          `json:"steps"`
	Aggregates    []aggregate  `json:"aggregates"`
	Comparisons   []comparison `json:"paired_comparisons"`
	SeedResults   []seedResult `json:"seed_results"`
}

type comparison struct {
	Condition          string  `json:"condition"`
	RegretDelta        float64 `json:"regret_delta_governed_minus_raw"`
	RegretDeltaCILow   float64 `json:"regret_delta_ci_low"`
	RegretDeltaCIHigh  float64 `json:"regret_delta_ci_high"`
	Final100Delta      float64 `json:"final_100_regret_delta"`
	HarmfulRateDelta   float64 `json:"harmful_rate_delta"`
	AdaptationLagDelta float64 `json:"adaptation_lag_delta"`
}

type replayJob struct {
	factory   func() learner
	condition stressCondition
	seed      int64
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "[SUMMARY] INVALIDATED error=%q\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	surface, err := loadSurface(root)
	if err != nil {
		return err
	}
	conditions := []stressCondition{
		{name: "nominal"},
		{name: "delay_32", delay: 32},
		{name: "reward_noise_10", noise: 0.10},
		{name: "abrupt_drift", drift: true},
		{name: "graph_outage", storeDown: true},
		{name: "combined", delay: 32, noise: 0.10, drift: true, storeDown: true},
	}
	factories := []func() learner{
		func() learner { return newRawKNN() },
		func() learner { return newGovernedKNN() },
	}
	const (
		seeds = 200
		steps = 800
	)
	jobs := make(chan replayJob)
	completed := make(chan seedResult)
	var workers sync.WaitGroup
	for range max(1, runtime.GOMAXPROCS(0)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				completed <- replay(surface, job.factory(), job.condition, job.seed, steps)
			}
		}()
	}
	go func() {
		workers.Wait()
		close(completed)
	}()
	go func() {
		defer close(jobs)
		for _, condition := range conditions {
			for _, factory := range factories {
				for seed := 0; seed < seeds; seed++ {
					jobs <- replayJob{factory: factory, condition: condition, seed: int64(1000000 + seed)}
				}
			}
		}
	}()
	results := make([]seedResult, 0, len(conditions)*len(factories)*seeds)
	for result := range completed {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Condition != results[j].Condition {
			return results[i].Condition < results[j].Condition
		}
		if results[i].Policy != results[j].Policy {
			return results[i].Policy < results[j].Policy
		}
		return results[i].Seed < results[j].Seed
	})
	aggregates := aggregateResults(results)
	comparisons := comparePaired(results)
	out := report{
		SchemaVersion: "3.1", CreatedAt: time.Now().UTC(), Seeds: seeds, Steps: steps,
		Aggregates: aggregates, Comparisons: comparisons, SeedResults: results,
	}
	dir := filepath.Join(root, ".planning", "spikes", "100-adaptive-policy-governance-stress", "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "governance-stress.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	for _, item := range aggregates {
		fmt.Printf("%s [METRIC] condition=%s policy=%s regret=%.2f ci=[%.2f,%.2f] final100=%.2f harm=%.3f fallback=%.3f lag=%.1f rollbacks=%.2f\n",
			time.Now().UTC().Format(time.RFC3339Nano), item.Condition, item.Policy,
			item.MeanRegret, item.CILow, item.CIHigh, item.Final100Regret,
			item.HarmfulRate, item.FallbackRate, item.AdaptationLag, item.MeanRollbacks)
	}
	for _, item := range comparisons {
		fmt.Printf("%s [PAIRED] condition=%s regret_delta=%.2f ci=[%.2f,%.2f] final100_delta=%.2f harm_delta=%.3f lag_delta=%.1f\n",
			time.Now().UTC().Format(time.RFC3339Nano), item.Condition,
			item.RegretDelta, item.RegretDeltaCILow, item.RegretDeltaCIHigh,
			item.Final100Delta, item.HarmfulRateDelta, item.AdaptationLagDelta)
	}
	fmt.Printf("%s [SUMMARY] COMPLETE artifact=%s runs=%d\n", time.Now().UTC().Format(time.RFC3339Nano), path, len(results))
	return nil
}

func replay(surface *rewardSurface, policy learner, condition stressCondition, seed int64, steps int) seedResult {
	rng := rand.New(rand.NewSource(seed))
	var queue []pending
	var regret, finalRegret float64
	var harmful, fallback int
	stepRegrets := make([]float64, steps)
	for step := 0; step < steps; step++ {
		kept := queue[:0]
		for _, item := range queue {
			if item.at <= step {
				policy.update(item.action, item.features, item.reward)
			} else {
				kept = append(kept, item)
			}
		}
		queue = kept
		sc := surface.scenarios[rng.Intn(len(surface.scenarios))]
		x := features(sc)
		storeUp := !(condition.storeDown && step >= 300 && step < 400)
		chosen := policy.choose(sc, x, storeUp, rng)
		if chosen.fallback {
			fallback++
		}
		bestAction, bestReward := best(sc, step, condition.drift)
		chosenExpected := expected(sc, chosen.action, step, condition.drift)
		stepRegret := math.Max(0, bestReward-chosenExpected)
		stepRegrets[step] = stepRegret
		regret += stepRegret
		if step >= steps-100 {
			finalRegret += stepRegret
		}
		if expectedQuality(sc, bestAction, step, condition.drift)-expectedQuality(sc, chosen.action, step, condition.drift) >= 0.5 {
			harmful++
		}
		samples := sc.samples[chosen.action]
		observed := samples[rng.Intn(len(samples))].reward
		if condition.drift && step >= 400 && degraded(sc.domain, chosen.action) {
			observed = -0.15
		}
		if rng.Float64() < condition.noise {
			if observed > 0.5 {
				observed = -0.10
			} else {
				observed = 0.90
			}
		}
		queue = append(queue, pending{at: step + condition.delay, action: chosen.action, features: x, reward: observed})
	}
	return seedResult{
		Policy: policy.name(), Condition: condition.name, Seed: seed, Regret: regret,
		Final100Regret: finalRegret, HarmfulRate: float64(harmful) / float64(steps),
		FallbackRate: float64(fallback) / float64(steps), AdaptationLag: adaptationLag(stepRegrets, condition.drift),
		Rollbacks: policy.rollbacks(),
	}
}

func best(sc scenario, step int, drift bool) (string, float64) {
	action, value := sc.actions[0], math.Inf(-1)
	for _, candidate := range sc.actions {
		reward := expected(sc, candidate, step, drift)
		if reward > value {
			action, value = candidate, reward
		}
	}
	return action, value
}

func adaptationLag(regrets []float64, drift bool) int {
	if !drift {
		return 0
	}
	for step := 449; step < len(regrets); step++ {
		if average(regrets[step-49:step+1]) < 0.10 {
			return step - 400
		}
	}
	return len(regrets) - 400
}

func aggregateResults(results []seedResult) []aggregate {
	grouped := make(map[string][]seedResult)
	for _, item := range results {
		grouped[item.Condition+"|"+item.Policy] = append(grouped[item.Condition+"|"+item.Policy], item)
	}
	out := make([]aggregate, 0, len(grouped))
	for _, rows := range grouped {
		regrets := values(rows, func(item seedResult) float64 { return item.Regret })
		low, high := bootstrap(regrets, 3000)
		out = append(out, aggregate{
			Policy: rows[0].Policy, Condition: rows[0].Condition,
			MeanRegret: average(regrets), CILow: low, CIHigh: high,
			Final100Regret: average(values(rows, func(item seedResult) float64 { return item.Final100Regret })),
			HarmfulRate:    average(values(rows, func(item seedResult) float64 { return item.HarmfulRate })),
			FallbackRate:   average(values(rows, func(item seedResult) float64 { return item.FallbackRate })),
			AdaptationLag:  average(values(rows, func(item seedResult) float64 { return float64(item.AdaptationLag) })),
			MeanRollbacks:  average(values(rows, func(item seedResult) float64 { return float64(item.Rollbacks) })),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Condition != out[j].Condition {
			return out[i].Condition < out[j].Condition
		}
		return out[i].Policy < out[j].Policy
	})
	return out
}

func comparePaired(results []seedResult) []comparison {
	grouped := make(map[string]map[int64]map[string]seedResult)
	for _, item := range results {
		if grouped[item.Condition] == nil {
			grouped[item.Condition] = make(map[int64]map[string]seedResult)
		}
		if grouped[item.Condition][item.Seed] == nil {
			grouped[item.Condition][item.Seed] = make(map[string]seedResult)
		}
		grouped[item.Condition][item.Seed][item.Policy] = item
	}
	out := make([]comparison, 0, len(grouped))
	for condition, seeds := range grouped {
		var regret, final100, harm, lag []float64
		for _, policies := range seeds {
			raw, rawOK := policies["raw_graph_knn"]
			governed, governedOK := policies["governed_graph_knn"]
			if !rawOK || !governedOK {
				continue
			}
			regret = append(regret, governed.Regret-raw.Regret)
			final100 = append(final100, governed.Final100Regret-raw.Final100Regret)
			harm = append(harm, governed.HarmfulRate-raw.HarmfulRate)
			lag = append(lag, float64(governed.AdaptationLag-raw.AdaptationLag))
		}
		low, high := bootstrap(regret, 3000)
		out = append(out, comparison{
			Condition: condition, RegretDelta: average(regret),
			RegretDeltaCILow: low, RegretDeltaCIHigh: high,
			Final100Delta: average(final100), HarmfulRateDelta: average(harm),
			AdaptationLagDelta: average(lag),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Condition < out[j].Condition })
	return out
}

func bootstrap(values []float64, iterations int) (float64, float64) {
	rng := rand.New(rand.NewSource(10001))
	means := make([]float64, iterations)
	for i := range means {
		var sum float64
		for range values {
			sum += values[rng.Intn(len(values))]
		}
		means[i] = sum / float64(len(values))
	}
	sort.Float64s(means)
	return means[int(0.025*float64(iterations))], means[int(0.975*float64(iterations))]
}

func values(rows []seedResult, fn func(seedResult) float64) []float64 {
	out := make([]float64, len(rows))
	for i, item := range rows {
		out[i] = fn(item)
	}
	return out
}
