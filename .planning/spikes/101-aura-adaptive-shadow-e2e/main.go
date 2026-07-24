package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/activelearn"
	"github.com/chetto1983/aura/internal/documents"
)

const (
	qwenURL    = "http://127.0.0.1:18080"
	graniteURL = "http://127.0.0.1:8081"
	embedDim   = 768
)

type pendingOutcome struct {
	domain string
	action string
	vec    []float64
	live   liveOutcome
	reward float64
}

type savedResult struct {
	decisionID string
	err        error
}

type outcomeOracle struct {
	graph   *shadowGraph
	policy  *shadowPolicy
	pending sync.Map
	saved   chan savedResult
}

func (o *outcomeOracle) LabelAndSave(ctx context.Context, decisionID string, _ []float64) bool {
	value, ok := o.pending.LoadAndDelete(decisionID)
	if !ok {
		o.saved <- savedResult{decisionID: decisionID, err: fmt.Errorf("missing pending outcome")}
		return false
	}
	outcome := value.(pendingOutcome)
	if err := o.graph.attachOutcome(ctx, decisionID, outcome); err != nil {
		o.saved <- savedResult{decisionID: decisionID, err: err}
		return false
	}
	o.policy.add(outcome.domain, outcome.action, outcome.vec, outcome.reward)
	o.saved <- savedResult{decisionID: decisionID}
	return true
}

type turnEvidence struct {
	DecisionID            string             `json:"decision_id"`
	ScenarioID            string             `json:"scenario_id"`
	Domain                string             `json:"domain"`
	ServedAction          string             `json:"served_action"`
	ShadowAction          string             `json:"shadow_action"`
	ShadowScores          map[string]float64 `json:"shadow_scores"`
	ServedActionUnchanged bool               `json:"served_action_unchanged"`
	Outcome               liveOutcome        `json:"outcome"`
	Reward                float64            `json:"reward"`
	ObserveLatencyMS      float64            `json:"observe_latency_ms"`
}

type learningCounts struct {
	Accepted int `json:"accepted"`
	Success  int `json:"success"`
	Errors   int `json:"errors"`
}

type shadowReport struct {
	SchemaVersion        string         `json:"schema_version"`
	RunID                string         `json:"run_id"`
	CreatedAt            time.Time      `json:"created_at"`
	Model                string         `json:"model"`
	ModelURL             string         `json:"model_url"`
	EmbedURL             string         `json:"embed_url"`
	Policy               string         `json:"policy"`
	QwenHealthy          bool           `json:"qwen_healthy"`
	AgentMemoryBefore    bool           `json:"agent_memory_before"`
	AgentMemoryAfter     bool           `json:"agent_memory_after"`
	Turns                []turnEvidence `json:"turns"`
	Learning             learningCounts `json:"learning"`
	ObserveP95MS         float64        `json:"observe_p95_ms"`
	MeanReward           float64        `json:"mean_reward"`
	ShadowDisagreement   float64        `json:"shadow_disagreement_rate"`
	Graph                graphEvidence  `json:"graph"`
	PromotionEligibility string         `json:"promotion_eligibility"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[SUMMARY] INVALIDATED error=%q\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	runID := "adaptive-shadow-101-" + time.Now().UTC().Format("20060102T150405Z")
	qwenHealthy := httpHealthy(ctx, qwenURL+"/health")
	if !qwenHealthy {
		return fmt.Errorf("Qwen llama.cpp health check failed")
	}
	agentMemoryBefore := tcpHealthy("127.0.0.1:8091")
	embedder := &documents.EmbeddingClient{
		BaseURL: graniteURL, Model: "granite-embedding-311m-multilingual-r2",
		Dimensions: embedDim, Client: &http.Client{Timeout: 90 * time.Second},
	}
	scenarios, policy, err := loadScenariosAndPolicy(root, embedder)
	if err != nil {
		return fmt.Errorf("load policy: %w", err)
	}
	selectedIDs := []string{"r03", "t01", "s01", "k01", "r04", "t02", "s02", "k05"}
	selected := make([]sourceScenario, 0, len(selectedIDs))
	prompts := make([]string, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		sc, ok := scenarios[id]
		if !ok {
			return fmt.Errorf("missing scenario %s", id)
		}
		selected = append(selected, sc)
		prompts = append(prompts, sc.Prompt)
	}
	vectors, err := embedder.Embed(ctx, prompts)
	if err != nil {
		return fmt.Errorf("live Granite embedding: %w", err)
	}
	graph, err := openShadowGraph(ctx, os.Getenv("NEO4J_PASSWORD"), runID)
	if err != nil {
		return fmt.Errorf("open Neo4j: %w", err)
	}
	defer func() {
		_, _ = graph.cleanup(context.Background())
		_ = graph.close(context.Background())
	}()

	oracle := &outcomeOracle{graph: graph, policy: policy, saved: make(chan savedResult, len(selected))}
	var metricMu sync.Mutex
	learning := learningCounts{}
	refreshes := 0
	learner := activelearn.New(activelearn.Config{
		Oracle: oracle, MarginFloor: 0.05, Queue: 16,
		Refresh: func() {
			metricMu.Lock()
			refreshes++
			metricMu.Unlock()
		},
		Metrics: func(metric activelearn.LearningMetric) {
			metricMu.Lock()
			defer metricMu.Unlock()
			switch metric.Outcome {
			case "accepted":
				learning.Accepted++
			case "success":
				learning.Success++
			case "error":
				learning.Errors++
			}
		},
	})
	defer learner.Close()

	client := &qwenClient{baseURL: qwenURL, client: &http.Client{Timeout: 90 * time.Second}}
	turns := make([]turnEvidence, 0, len(selected))
	var observeLatencies []float64
	var rewardSum float64
	disagreements := 0
	for i, sc := range selected {
		shadowAction, scores, err := policy.predict(sc.Domain, vectors[i])
		if err != nil {
			return err
		}
		servedAction := staticServedAction(sc)
		decisionID := fmt.Sprintf("%s/%02d/%s", runID, i+1, sc.ID)
		if err := graph.recordDecision(ctx, decisionID, sc, vectors[i], servedAction, shadowAction, scores); err != nil {
			return fmt.Errorf("record decision %s: %w", sc.ID, err)
		}
		live := executeLive(ctx, client, sc, servedAction)
		reward := liveReward(live.Correct, live.Tokens, live.LatencyMS, live.Error)
		oracle.pending.Store(decisionID, pendingOutcome{
			domain: sc.Domain, action: servedAction, vec: vectors[i], live: live, reward: reward,
		})
		observeStart := time.Now()
		learner.Observe(decisionID, vectors[i], 0)
		observeMS := float64(time.Since(observeStart).Nanoseconds()) / 1e6
		observeLatencies = append(observeLatencies, observeMS)
		select {
		case saved := <-oracle.saved:
			if saved.err != nil {
				return fmt.Errorf("async outcome save %s: %w", saved.decisionID, saved.err)
			}
		case <-time.After(15 * time.Second):
			return fmt.Errorf("timed out waiting for async outcome %s", decisionID)
		case <-ctx.Done():
			return ctx.Err()
		}
		if shadowAction != servedAction {
			disagreements++
		}
		rewardSum += reward
		turns = append(turns, turnEvidence{
			DecisionID: decisionID, ScenarioID: sc.ID, Domain: sc.Domain,
			ServedAction: servedAction, ShadowAction: shadowAction, ShadowScores: scores,
			ServedActionUnchanged: true, Outcome: live, Reward: reward, ObserveLatencyMS: observeMS,
		})
		fmt.Printf("%s [TURN] id=%s domain=%s served=%s shadow=%s correct=%t tokens=%d latency_ms=%.1f observe_ms=%.4f\n",
			time.Now().UTC().Format(time.RFC3339Nano), sc.ID, sc.Domain, servedAction, shadowAction,
			live.Correct, live.Tokens, live.LatencyMS, observeMS)
	}
	metricMu.Lock()
	learningSnapshot := learning
	refreshSnapshot := refreshes
	metricMu.Unlock()
	observeP95 := percentile(observeLatencies, 0.95)
	meanReward := rewardSum / float64(len(turns))
	status := "promotion_eligible"
	if learningSnapshot.Success != len(turns) || refreshSnapshot != len(turns) || hasExecutionError(turns) {
		status = "not_eligible"
	}
	if err := graph.saveSnapshot(ctx, status, len(turns), meanReward, observeP95); err != nil {
		return err
	}
	evidence, err := graph.evidence(ctx)
	if err != nil {
		return err
	}
	if evidence.Decisions != int64(len(turns)) ||
		evidence.Outcomes != int64(len(turns)) ||
		evidence.UnchangedServed != int64(len(turns)) ||
		evidence.SnapshotStatus != status {
		return fmt.Errorf("graph evidence mismatch: %+v", evidence)
	}
	agentMemoryAfter := tcpHealthy("127.0.0.1:8091")
	cleanupNodes, err := graph.cleanup(ctx)
	if err != nil {
		return err
	}
	evidence.CleanupNodes = cleanupNodes
	report := shadowReport{
		SchemaVersion: "1.0", RunID: runID, CreatedAt: time.Now().UTC(),
		Model:    "unsloth/Qwen3.5-2B-GGUF:Qwen3.5-2B-Q4_K_M.gguf",
		ModelURL: qwenURL, EmbedURL: graniteURL, Policy: "graph_knn_shadow_v1",
		QwenHealthy: qwenHealthy, AgentMemoryBefore: agentMemoryBefore, AgentMemoryAfter: agentMemoryAfter,
		Turns: turns, Learning: learningSnapshot, ObserveP95MS: observeP95,
		MeanReward: meanReward, ShadowDisagreement: float64(disagreements) / float64(len(turns)),
		Graph: evidence, PromotionEligibility: status,
	}
	artifactDir := filepath.Join(root, ".planning", "spikes", "101-aura-adaptive-shadow-e2e", "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(artifactDir, "shadow-e2e.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s [SUMMARY] VALIDATED artifact=%s turns=%d outcomes=%d unchanged=%d observe_p95_ms=%.4f status=%s cleaned=%d\n",
		time.Now().UTC().Format(time.RFC3339Nano), path, len(turns), evidence.Outcomes,
		evidence.UnchangedServed, observeP95, status, cleanupNodes)
	return nil
}

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(float64(len(sorted)-1) * quantile)
	return sorted[index]
}

func hasExecutionError(turns []turnEvidence) bool {
	for _, turn := range turns {
		if turn.Outcome.Error != "" {
			return true
		}
	}
	return false
}

func httpHealthy(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode/100 == 2
}

func tcpHealthy(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
