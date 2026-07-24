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
	ScenarioID  string  `json:"scenario_id"`
	Domain      string  `json:"domain"`
	Action      string  `json:"action"`
	Selected    string  `json:"selected"`
	Content     string  `json:"content"`
	TotalTokens int     `json:"total_tokens"`
	LatencyMS   float64 `json:"latency_ms"`
	Error       string  `json:"error"`
}

type sourceSurface struct {
	Scenarios []sourceScenario `json:"scenarios"`
	Outcomes  []sourceOutcome  `json:"outcomes"`
}

type sample struct {
	reward  float64
	correct bool
}

type scenario struct {
	id, domain, prompt, expected string
	embedding                    []float64
	actions                      []string
	samples                      map[string][]sample
}

type rewardSurface struct {
	scenarios []scenario
}

func loadSurface(root string) (*rewardSurface, error) {
	paths := []string{
		filepath.Join(root, ".planning", "spikes", "097-qwen-multidomain-reward-surface", "artifacts", "reward-surface-run1.json"),
		filepath.Join(root, ".planning", "spikes", "097-qwen-multidomain-reward-surface", "artifacts", "reward-surface-run2.json"),
	}
	var surfaces []sourceSurface
	maxTokens, maxLatency := map[string]float64{}, map[string]float64{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var parsed sourceSurface
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		surfaces = append(surfaces, parsed)
		for _, out := range parsed.Outcomes {
			maxTokens[out.Domain] = math.Max(maxTokens[out.Domain], float64(out.TotalTokens))
			maxLatency[out.Domain] = math.Max(maxLatency[out.Domain], out.LatencyMS)
		}
	}
	latest := surfaces[len(surfaces)-1]
	byID := make(map[string]int)
	result := &rewardSurface{}
	for _, sc := range latest.Scenarios {
		byID[sc.ID] = len(result.scenarios)
		result.scenarios = append(result.scenarios, scenario{
			id: sc.ID, domain: sc.Domain, prompt: sc.Prompt, expected: sc.Expected,
			embedding: sc.Embedding, samples: make(map[string][]sample),
		})
	}
	for _, surface := range surfaces {
		for _, out := range surface.Outcomes {
			index, ok := byID[out.ScenarioID]
			if !ok {
				return nil, fmt.Errorf("unknown scenario %s", out.ScenarioID)
			}
			sc := &result.scenarios[index]
			correct := score(*sc, out)
			reward := 0.0
			if correct {
				reward = 1
			}
			if out.Error != "" {
				reward = -0.25
			}
			reward -= 0.08 * float64(out.TotalTokens) / maxTokens[out.Domain]
			reward -= 0.04 * out.LatencyMS / maxLatency[out.Domain]
			sc.samples[out.Action] = append(sc.samples[out.Action], sample{reward: reward, correct: correct})
		}
	}
	for i := range result.scenarios {
		for action, samples := range result.scenarios[i].samples {
			if len(samples) != 2 {
				return nil, fmt.Errorf("%s/%s samples=%d", result.scenarios[i].id, action, len(samples))
			}
			result.scenarios[i].actions = append(result.scenarios[i].actions, action)
		}
		sort.Strings(result.scenarios[i].actions)
	}
	return result, nil
}

func score(sc scenario, out sourceOutcome) bool {
	if out.Error != "" {
		return false
	}
	if sc.domain == "tool" || sc.domain == "skill" {
		return same(out.Selected, sc.expected)
	}
	return same(out.Content, sc.expected)
}

func same(got, expected string) bool {
	norm := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.Trim(value, "`'\".,;:!?$€ ")
		value = strings.ReplaceAll(value, "seven", "7")
		value = strings.ReplaceAll(value, "eighteen", "18")
		value = strings.ReplaceAll(value, "ninety", "90")
		value = strings.Join(strings.Fields(value), " ")
		value = strings.ReplaceAll(value, " at ", " ")
		return value
	}
	g, e := norm(got), norm(expected)
	return g == e || strings.Contains(g, e)
}

func expected(sc scenario, action string, step int, drift bool) float64 {
	samples := sc.samples[action]
	value := (samples[0].reward + samples[1].reward) / 2
	if drift && step >= 400 && degraded(sc.domain, action) {
		return -0.15
	}
	return value
}

func expectedQuality(sc scenario, action string, step int, drift bool) float64 {
	if drift && step >= 400 && degraded(sc.domain, action) {
		return 0
	}
	samples := sc.samples[action]
	var value float64
	for _, item := range samples {
		if item.correct {
			value++
		}
	}
	return value / float64(len(samples))
}

func degraded(domain, action string) bool {
	switch domain {
	case "reasoning":
		return action == "high_768"
	case "tool":
		return action == "qwen_top3"
	case "skill":
		return action == "semantic_top1"
	case "knowledge":
		return action == "graph_expand"
	default:
		return false
	}
}
