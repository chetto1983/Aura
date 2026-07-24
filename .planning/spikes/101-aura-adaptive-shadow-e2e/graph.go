package main

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type shadowGraph struct {
	driver neo4j.DriverWithContext
	runID  string
}

type graphEvidence struct {
	Decisions       int64  `json:"decisions"`
	Outcomes        int64  `json:"outcomes"`
	UnchangedServed int64  `json:"unchanged_served"`
	SnapshotStatus  string `json:"snapshot_status"`
	CleanupNodes    int64  `json:"cleanup_nodes"`
}

func openShadowGraph(ctx context.Context, password, runID string) (*shadowGraph, error) {
	if password == "" {
		return nil, fmt.Errorf("NEO4J_PASSWORD is required")
	}
	driver, err := neo4j.NewDriverWithContext("bolt://127.0.0.1:7687", neo4j.BasicAuth("neo4j", password, ""))
	if err != nil {
		return nil, err
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, err
	}
	return &shadowGraph{driver: driver, runID: runID}, nil
}

func (g *shadowGraph) recordDecision(
	ctx context.Context,
	decisionID string,
	sc sourceScenario,
	vec []float64,
	servedAction, shadowAction string,
	scores map[string]float64,
) error {
	scoreRows := make([]map[string]any, 0, len(scores))
	for action, score := range scores {
		scoreRows = append(scoreRows, map[string]any{"action": action, "score": score})
	}
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
MERGE (c:AdaptiveShadowContext {id: $context_id})
SET c.run_id = $run_id, c.domain = $domain, c.prompt = $prompt, c.embedding = $embedding
MERGE (served:AdaptiveShadowAction {id: $served_id})
SET served.run_id = $run_id, served.domain = $domain, served.name = $served_action
MERGE (shadow:AdaptiveShadowAction {id: $shadow_id})
SET shadow.run_id = $run_id, shadow.domain = $domain, shadow.name = $shadow_action
MERGE (d:AdaptiveShadowDecision {id: $decision_id})
SET d.run_id = $run_id, d.served_action_initial = $served_action,
    d.served_action = $served_action, d.shadow_action = $shadow_action,
    d.created_at = datetime()
MERGE (d)-[:FOR_CONTEXT]->(c)
MERGE (d)-[:SERVED]->(served)
MERGE (d)-[:SHADOW_RECOMMENDED]->(shadow)
WITH d
UNWIND $scores AS candidate
MERGE (a:AdaptiveShadowAction {id: $domain + '|' + candidate.action + '|' + $run_id})
SET a.run_id = $run_id, a.domain = $domain, a.name = candidate.action
MERGE (d)-[r:CONSIDERED]->(a)
SET r.score = candidate.score
`, map[string]any{
			"run_id": g.runID, "decision_id": decisionID,
			"context_id": g.runID + "|" + sc.ID, "domain": sc.Domain,
			"prompt": sc.Prompt, "embedding": vec,
			"served_id":     g.runID + "|" + sc.Domain + "|" + servedAction,
			"shadow_id":     g.runID + "|" + sc.Domain + "|" + shadowAction,
			"served_action": servedAction, "shadow_action": shadowAction,
			"scores": scoreRows,
		})
		if err != nil {
			return nil, err
		}
		_, err = result.Consume(ctx)
		return nil, err
	})
	return err
}

func (g *shadowGraph) attachOutcome(ctx context.Context, decisionID string, outcome pendingOutcome) error {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
MATCH (d:AdaptiveShadowDecision {id: $decision_id, run_id: $run_id})
MERGE (o:AdaptiveShadowOutcome {id: $decision_id + '/outcome'})
SET o.run_id = $run_id, o.correct = $correct, o.reward = $reward,
    o.tokens = $tokens, o.latency_ms = $latency_ms, o.error = $error,
    o.observed_at = datetime()
MERGE (d)-[:OBSERVED]->(o)
`, map[string]any{
			"run_id": g.runID, "decision_id": decisionID,
			"correct": outcome.live.Correct, "reward": outcome.reward,
			"tokens": outcome.live.Tokens, "latency_ms": outcome.live.LatencyMS,
			"error": outcome.live.Error,
		})
		if err != nil {
			return nil, err
		}
		_, err = result.Consume(ctx)
		return nil, err
	})
	return err
}

func (g *shadowGraph) saveSnapshot(ctx context.Context, status string, decisions int, meanReward, observeP95 float64) error {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `
MERGE (p:AdaptiveShadowPolicySnapshot {id: $run_id + '/graph-knn-v1'})
SET p.run_id = $run_id, p.kind = 'graph_knn', p.status = $status,
    p.decisions = $decisions, p.mean_reward = $mean_reward,
    p.observe_p95_ms = $observe_p95_ms, p.created_at = datetime(),
    p.evidence_spikes = ['097','098b','099','100','101']
`, map[string]any{
		"run_id": g.runID, "status": status, "decisions": decisions,
		"mean_reward": meanReward, "observe_p95_ms": observeP95,
	})
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (g *shadowGraph) evidence(ctx context.Context) (graphEvidence, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `
MATCH (d:AdaptiveShadowDecision {run_id: $run_id})
OPTIONAL MATCH (d)-[:OBSERVED]->(o:AdaptiveShadowOutcome {run_id: $run_id})
WITH count(DISTINCT d) AS decisions, count(DISTINCT o) AS outcomes,
     count(DISTINCT CASE WHEN d.served_action = d.served_action_initial THEN d END) AS unchanged
OPTIONAL MATCH (p:AdaptiveShadowPolicySnapshot {run_id: $run_id})
RETURN decisions, outcomes, unchanged, coalesce(p.status, '') AS status
`, map[string]any{"run_id": g.runID})
	if err != nil {
		return graphEvidence{}, err
	}
	if !result.Next(ctx) {
		return graphEvidence{}, fmt.Errorf("evidence query returned no row")
	}
	record := result.Record()
	return graphEvidence{
		Decisions: asInt64(record, "decisions"), Outcomes: asInt64(record, "outcomes"),
		UnchangedServed: asInt64(record, "unchanged"), SnapshotStatus: asString(record, "status"),
	}, nil
}

func (g *shadowGraph) cleanup(ctx context.Context) (int64, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `
MATCH (n)
WHERE n.run_id = $run_id AND (
  n:AdaptiveShadowContext OR n:AdaptiveShadowAction OR n:AdaptiveShadowDecision OR
  n:AdaptiveShadowOutcome OR n:AdaptiveShadowPolicySnapshot
)
WITH collect(n) AS doomed, count(n) AS count
FOREACH (n IN doomed | DETACH DELETE n)
RETURN count
`, map[string]any{"run_id": g.runID})
	if err != nil {
		return 0, err
	}
	if !result.Next(ctx) {
		return 0, fmt.Errorf("cleanup returned no row")
	}
	return asInt64(result.Record(), "count"), nil
}

func (g *shadowGraph) close(ctx context.Context) error {
	return g.driver.Close(ctx)
}

func asInt64(record *neo4j.Record, key string) int64 {
	value, _ := record.Get(key)
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}

func asString(record *neo4j.Record, key string) string {
	value, _ := record.Get(key)
	return fmt.Sprint(value)
}
