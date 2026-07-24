package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sourceScenario struct {
	ID        string    `json:"id"`
	Domain    string    `json:"domain"`
	Prompt    string    `json:"prompt"`
	Expected  string    `json:"expected"`
	Embedding []float64 `json:"embedding"`
}

type sourceOutcome struct {
	ScenarioID  string             `json:"scenario_id"`
	Domain      string             `json:"domain"`
	Action      string             `json:"action"`
	Correct     bool               `json:"correct"`
	Selected    string             `json:"selected"`
	Content     string             `json:"content"`
	TotalTokens int                `json:"total_tokens"`
	LatencyMS   float64            `json:"latency_ms"`
	Rewards     map[string]float64 `json:"rewards"`
	Error       string             `json:"error"`
}

type sourceSurface struct {
	RunID     string           `json:"run_id"`
	Scenarios []sourceScenario `json:"scenarios"`
	Outcomes  []sourceOutcome  `json:"outcomes"`
}

type measuredOutcome struct {
	Correct   bool
	Tokens    int
	LatencyMS float64
	Rewards   map[string]float64
}

type benchScenario struct {
	ID        string
	Domain    string
	Prompt    string
	Expected  string
	Embedding []float64
	Actions   []string
	Samples   map[string][]measuredOutcome
}

type rewardModel struct {
	Scenarios []benchScenario
	byID      map[string]int
}

func loadRewardModel(root string) (*rewardModel, error) {
	paths := []string{
		filepath.Join(root, ".planning", "spikes", "097-qwen-multidomain-reward-surface", "artifacts", "reward-surface-run1.json"),
		filepath.Join(root, ".planning", "spikes", "097-qwen-multidomain-reward-surface", "artifacts", "reward-surface-run2.json"),
	}
	surfaces := make([]sourceSurface, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var surface sourceSurface
		if err := json.Unmarshal(raw, &surface); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		surfaces = append(surfaces, surface)
	}
	if len(surfaces[0].Scenarios) == 0 {
		return nil, fmt.Errorf("empty reward surface")
	}
	model := &rewardModel{byID: make(map[string]int)}
	for _, sc := range surfaces[len(surfaces)-1].Scenarios {
		model.byID[sc.ID] = len(model.Scenarios)
		model.Scenarios = append(model.Scenarios, benchScenario{
			ID: sc.ID, Domain: sc.Domain, Prompt: sc.Prompt, Expected: sc.Expected,
			Embedding: sc.Embedding, Samples: make(map[string][]measuredOutcome),
		})
	}
	maxTokens, maxLatency := map[string]float64{}, map[string]float64{}
	for _, surface := range surfaces {
		for _, item := range surface.Outcomes {
			maxTokens[item.Domain] = math.Max(maxTokens[item.Domain], float64(item.TotalTokens))
			maxLatency[item.Domain] = math.Max(maxLatency[item.Domain], item.LatencyMS)
		}
	}
	for _, surface := range surfaces {
		for _, item := range surface.Outcomes {
			idx, ok := model.byID[item.ScenarioID]
			if !ok {
				return nil, fmt.Errorf("outcome references unknown scenario %s", item.ScenarioID)
			}
			sc := &model.Scenarios[idx]
			correct := scoreOutcome(*sc, item)
			rewards := scalarRewards(correct, item.Error != "", item.TotalTokens, item.LatencyMS, maxTokens[item.Domain], maxLatency[item.Domain])
			sc.Samples[item.Action] = append(sc.Samples[item.Action], measuredOutcome{
				Correct: correct, Tokens: item.TotalTokens, LatencyMS: item.LatencyMS, Rewards: rewards,
			})
		}
	}
	for i := range model.Scenarios {
		sc := &model.Scenarios[i]
		for action, samples := range sc.Samples {
			if len(samples) != len(surfaces) {
				return nil, fmt.Errorf("%s/%s has %d samples, want %d", sc.ID, action, len(samples), len(surfaces))
			}
			sc.Actions = append(sc.Actions, action)
		}
		sort.Strings(sc.Actions)
		if len(sc.Actions) != 3 {
			return nil, fmt.Errorf("%s has %d actions, want 3", sc.ID, len(sc.Actions))
		}
	}
	return model, nil
}

func scoreOutcome(sc benchScenario, item sourceOutcome) bool {
	if item.Error != "" {
		return false
	}
	switch sc.Domain {
	case "tool", "skill":
		return sameAnswer(item.Selected, sc.Expected)
	default:
		return sameAnswer(item.Content, sc.Expected)
	}
}

func scalarRewards(correct, failed bool, tokens int, latency, maxTokens, maxLatency float64) map[string]float64 {
	quality := 0.0
	if correct {
		quality = 1
	}
	if failed {
		quality = -0.25
	}
	tokenNorm, latencyNorm := 0.0, 0.0
	if maxTokens > 0 {
		tokenNorm = float64(tokens) / maxTokens
	}
	if maxLatency > 0 {
		latencyNorm = latency / maxLatency
	}
	weights := map[string][2]float64{
		"quality_first": {0.02, 0.01},
		"balanced":      {0.08, 0.04},
		"economy":       {0.18, 0.08},
	}
	out := make(map[string]float64, len(weights))
	for profile, w := range weights {
		out[profile] = quality - w[0]*tokenNorm - w[1]*latencyNorm
	}
	return out
}

func (m *rewardModel) expected(sc benchScenario, action, profile string) float64 {
	samples := sc.Samples[action]
	var sum float64
	for _, sample := range samples {
		sum += sample.Rewards[profile]
	}
	return sum / float64(len(samples))
}

func (m *rewardModel) expectedQuality(sc benchScenario, action string) float64 {
	samples := sc.Samples[action]
	var sum float64
	for _, sample := range samples {
		if sample.Correct {
			sum++
		}
	}
	return sum / float64(len(samples))
}

func (m *rewardModel) best(sc benchScenario, profile string) (string, float64) {
	bestAction, bestReward := "", math.Inf(-1)
	for _, action := range sc.Actions {
		reward := m.expected(sc, action, profile)
		if reward > bestReward {
			bestAction, bestReward = action, reward
		}
	}
	return bestAction, bestReward
}

func sameAnswer(got, expected string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.Trim(value, "`'\".,;:!?$€ ")
		value = strings.ReplaceAll(value, "seven", "7")
		value = strings.ReplaceAll(value, "eighteen", "18")
		value = strings.ReplaceAll(value, "ninety", "90")
		value = strings.Join(strings.Fields(value), " ")
		value = strings.ReplaceAll(value, " at ", " ")
		return value
	}
	g, e := normalize(got), normalize(expected)
	return g == e || strings.Contains(g, e)
}
