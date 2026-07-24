package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

type report struct {
	SchemaVersion string             `json:"schema_version"`
	CreatedAt     time.Time          `json:"created_at"`
	SurfaceRuns   int                `json:"surface_runs"`
	Scenarios     int                `json:"scenarios"`
	StepsPerSeed  int                `json:"steps_per_seed"`
	Seeds         int                `json:"seeds"`
	Aggregates    []aggregate        `json:"aggregates"`
	Paired        []pairedDifference `json:"paired_differences"`
	SeedResults   []seedResult       `json:"seed_results"`
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
	model, err := loadRewardModel(root)
	if err != nil {
		return err
	}
	const (
		seeds = 200
		steps = 600
	)
	conditions := []condition{{Name: "stationary", Delay: 0}, {Name: "delay_8", Delay: 8}}
	profiles := []string{"quality_first", "balanced", "economy"}
	factories := []func(int) policy{
		func(int) policy { return staticPolicy{} },
		func(int) policy { return newKNNPolicy() },
		func(dim int) policy { return newLinUCBPolicy(dim) },
		func(dim int) policy { return newHybridPolicy(dim) },
	}
	type task struct {
		factory   func(int) policy
		condition condition
		profile   string
		seed      int64
	}
	tasks := make(chan task)
	results := make(chan seedResult)
	workers := max(1, runtime.NumCPU()-1)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range tasks {
				results <- runReplay(model, item.factory, item.condition, item.profile, item.seed, steps)
			}
		}()
	}
	go func() {
		for _, cond := range conditions {
			for _, profile := range profiles {
				for _, factory := range factories {
					for seed := 0; seed < seeds; seed++ {
						tasks <- task{factory: factory, condition: cond, profile: profile, seed: int64(980000 + seed)}
					}
				}
			}
		}
		close(tasks)
		wg.Wait()
		close(results)
	}()
	all := make([]seedResult, 0, len(conditions)*len(profiles)*len(factories)*seeds)
	for item := range results {
		all = append(all, item)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Condition != all[j].Condition {
			return all[i].Condition < all[j].Condition
		}
		if all[i].Profile != all[j].Profile {
			return all[i].Profile < all[j].Profile
		}
		if all[i].Policy != all[j].Policy {
			return all[i].Policy < all[j].Policy
		}
		return all[i].Seed < all[j].Seed
	})
	aggregates := aggregateResults(all)
	paired := append(
		pairedRegret(all, "static_centroid", "graph_prior_linucb"),
		pairedRegret(all, "linucb", "graph_prior_linucb")...,
	)
	paired = append(paired, pairedRegret(all, "graph_knn", "graph_prior_linucb")...)
	out := report{
		SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), SurfaceRuns: 2,
		Scenarios: len(model.Scenarios), StepsPerSeed: steps, Seeds: seeds,
		Aggregates: aggregates, Paired: paired, SeedResults: all,
	}
	dir := filepath.Join(root, ".planning", "spikes", "098d-graph-prior-linucb", "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "policy-benchmark.json"), raw, 0o644); err != nil {
		return err
	}
	for _, item := range aggregates {
		fmt.Printf("%s [METRIC] condition=%s profile=%s policy=%s regret=%.3f ci=[%.3f,%.3f] final100=%.3f harm=%.4f oracle_match=%.3f decision_p95_us=%.1f\n",
			time.Now().UTC().Format(time.RFC3339Nano), item.Condition, item.Profile, item.Policy,
			item.MeanRegret, item.RegretCILow, item.RegretCIHigh, item.MeanFinal100Regret,
			item.MeanHarmfulRate, item.MeanOracleMatch, item.P95DecisionNanos/1000)
	}
	fmt.Printf("%s [SUMMARY] COMPLETE artifact=%s runs=%d\n", time.Now().UTC().Format(time.RFC3339Nano),
		filepath.Join(dir, "policy-benchmark.json"), len(all))
	return nil
}
