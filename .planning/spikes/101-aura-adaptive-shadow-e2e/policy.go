package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/chetto1983/aura/internal/semindex"
)

type sourceScenario struct {
	ID         string              `json:"id"`
	Domain     string              `json:"domain"`
	Prompt     string              `json:"prompt"`
	Expected   string              `json:"expected"`
	Embedding  []float64           `json:"embedding"`
	Candidates map[string][]string `json:"candidates"`
	Contexts   map[string]string   `json:"contexts"`
}

type sourceOutcome struct {
	ScenarioID  string             `json:"scenario_id"`
	Domain      string             `json:"domain"`
	Action      string             `json:"action"`
	Correct     bool               `json:"correct"`
	TotalTokens int                `json:"total_tokens"`
	LatencyMS   float64            `json:"latency_ms"`
	Error       string             `json:"error"`
	Rewards     map[string]float64 `json:"rewards"`
}

type sourceSurface struct {
	Scenarios []sourceScenario `json:"scenarios"`
	Outcomes  []sourceOutcome  `json:"outcomes"`
}

type indexedOutcome struct {
	action string
	reward float64
}

type shadowPolicy struct {
	embedder semindex.Embedder
	mu       sync.RWMutex
	banks    map[string]map[string]*semindex.Ranker
	outcomes map[string]indexedOutcome
	nextID   int
}

func loadScenariosAndPolicy(root string, embedder semindex.Embedder) (map[string]sourceScenario, *shadowPolicy, error) {
	paths := []string{
		filepath.Join(root, ".planning", "spikes", "097-qwen-multidomain-reward-surface", "artifacts", "reward-surface-run1.json"),
		filepath.Join(root, ".planning", "spikes", "097-qwen-multidomain-reward-surface", "artifacts", "reward-surface-run2.json"),
	}
	scenarios := make(map[string]sourceScenario)
	policy := &shadowPolicy{
		embedder: embedder,
		banks:    make(map[string]map[string]*semindex.Ranker),
		outcomes: make(map[string]indexedOutcome),
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var surface sourceSurface
		if err := json.Unmarshal(raw, &surface); err != nil {
			return nil, nil, err
		}
		for _, sc := range surface.Scenarios {
			scenarios[sc.ID] = sc
		}
		for _, outcome := range surface.Outcomes {
			sc, ok := scenarios[outcome.ScenarioID]
			if !ok {
				return nil, nil, fmt.Errorf("outcome references unknown scenario %s", outcome.ScenarioID)
			}
			reward, ok := outcome.Rewards["balanced"]
			if !ok {
				reward = liveReward(outcome.Correct, outcome.TotalTokens, outcome.LatencyMS, outcome.Error)
			}
			policy.add(sc.Domain, outcome.Action, sc.Embedding, reward)
		}
	}
	return scenarios, policy, nil
}

func (p *shadowPolicy) add(domain, action string, vec []float64, reward float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.banks[domain] == nil {
		p.banks[domain] = make(map[string]*semindex.Ranker)
	}
	if p.banks[domain][action] == nil {
		p.banks[domain][action] = semindex.NewRanker(p.embedder)
	}
	label := fmt.Sprintf("%s/%s/%06d", domain, action, p.nextID)
	p.nextID++
	p.outcomes[label] = indexedOutcome{action: action, reward: reward}
	p.banks[domain][action].AddVecs(semindex.Item{Label: label, Vec: vec})
}

func (p *shadowPolicy) predict(domain string, vec []float64) (string, map[string]float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	banks := p.banks[domain]
	if len(banks) == 0 {
		return "", nil, fmt.Errorf("no shadow bank for domain %s", domain)
	}
	actions := make([]string, 0, len(banks))
	for action := range banks {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	scores := make(map[string]float64, len(actions))
	best := ""
	for _, action := range actions {
		neighbors := banks[action].RankVecs(vec, 5)
		if len(neighbors) == 0 {
			continue
		}
		var sum float64
		for _, neighbor := range neighbors {
			sum += p.outcomes[neighbor.Label].reward
		}
		scores[action] = sum / float64(len(neighbors))
		if best == "" || scores[action] > scores[best] {
			best = action
		}
	}
	if best == "" {
		return "", scores, fmt.Errorf("no scored shadow action for domain %s", domain)
	}
	return best, scores, nil
}

func staticServedAction(sc sourceScenario) string {
	switch sc.Domain {
	case "reasoning":
		if sc.ID == "r04" {
			return "off"
		}
		return "low_256"
	case "tool", "skill":
		return "qwen_full"
	case "knowledge":
		return "graph_expand"
	default:
		return ""
	}
}

func liveReward(correct bool, tokens int, latencyMS float64, executionError string) float64 {
	quality := 0.0
	if correct {
		quality = 1
	}
	if executionError != "" {
		quality = -0.25
	}
	tokenCost := min(1, float64(tokens)/1000)
	latencyCost := min(1, latencyMS/10000)
	return quality - 0.08*tokenCost - 0.04*latencyCost
}
