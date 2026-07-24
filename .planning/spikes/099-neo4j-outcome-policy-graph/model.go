package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type sourceScenario struct {
	ID        string    `json:"id"`
	Domain    string    `json:"domain"`
	Prompt    string    `json:"prompt"`
	Embedding []float64 `json:"embedding"`
}

type sourceOutcome struct {
	ScenarioID  string             `json:"scenario_id"`
	Domain      string             `json:"domain"`
	Action      string             `json:"action"`
	Correct     bool               `json:"correct"`
	TotalTokens int                `json:"total_tokens"`
	LatencyMS   float64            `json:"latency_ms"`
	Rewards     map[string]float64 `json:"rewards"`
}

type sourceSurface struct {
	Scenarios []sourceScenario `json:"scenarios"`
	Outcomes  []sourceOutcome  `json:"outcomes"`
}

type decisionRow struct {
	DecisionID  string          `json:"decision_id"`
	ContextHash string          `json:"context_hash"`
	ScenarioID  string          `json:"scenario_id"`
	Domain      string          `json:"domain"`
	Prompt      string          `json:"prompt"`
	Embedding   []float64       `json:"embedding"`
	Action      string          `json:"action"`
	Propensity  float64         `json:"propensity"`
	PolicyID    string          `json:"policy_id"`
	Delayed     bool            `json:"delayed"`
	Outcome     outcomeRow      `json:"outcome"`
	Considered  []consideredRow `json:"considered"`
}

type outcomeRow struct {
	ID        string  `json:"id"`
	Correct   bool    `json:"correct"`
	Reward    float64 `json:"reward"`
	Tokens    int     `json:"tokens"`
	LatencyMS float64 `json:"latency_ms"`
	Source    string  `json:"source"`
}

type consideredRow struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Score float64 `json:"score"`
	Rank  int     `json:"rank"`
}

func loadDecisionRows(root, runID, policyID string) ([]decisionRow, error) {
	path := filepath.Join(root, ".planning", "spikes", "097-qwen-multidomain-reward-surface", "artifacts", "reward-surface.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var surface sourceSurface
	if err := json.Unmarshal(raw, &surface); err != nil {
		return nil, err
	}
	byScenario := make(map[string]sourceScenario, len(surface.Scenarios))
	actions := make(map[string][]sourceOutcome)
	for _, sc := range surface.Scenarios {
		byScenario[sc.ID] = sc
	}
	for _, item := range surface.Outcomes {
		actions[item.ScenarioID] = append(actions[item.ScenarioID], item)
	}
	rows := make([]decisionRow, 0, len(surface.Outcomes))
	for i, item := range surface.Outcomes {
		sc, ok := byScenario[item.ScenarioID]
		if !ok {
			return nil, fmt.Errorf("unknown scenario %s", item.ScenarioID)
		}
		alternatives := append([]sourceOutcome(nil), actions[item.ScenarioID]...)
		sort.Slice(alternatives, func(i, j int) bool {
			return alternatives[i].Rewards["balanced"] > alternatives[j].Rewards["balanced"]
		})
		considered := make([]consideredRow, 0, len(alternatives))
		for rank, alt := range alternatives {
			considered = append(considered, consideredRow{
				Key: sc.Domain + "|" + alt.Action, Name: alt.Action,
				Score: alt.Rewards["balanced"], Rank: rank + 1,
			})
		}
		decisionID := fmt.Sprintf("%s/%s/%s", runID, sc.ID, item.Action)
		rows = append(rows, decisionRow{
			DecisionID: decisionID, ContextHash: hash(runID + "/" + sc.ID),
			ScenarioID: sc.ID, Domain: sc.Domain, Prompt: sc.Prompt, Embedding: sc.Embedding,
			Action: item.Action, Propensity: 0.96, PolicyID: policyID, Delayed: i%7 == 0,
			Outcome: outcomeRow{
				ID: decisionID + "/outcome", Correct: item.Correct,
				Reward: item.Rewards["balanced"], Tokens: item.TotalTokens,
				LatencyMS: item.LatencyMS, Source: "qwen-live-surface",
			},
			Considered: considered,
		})
	}
	return rows, nil
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
