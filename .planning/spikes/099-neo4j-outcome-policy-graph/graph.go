package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	contextIndex = "adaptive_bench_context_embedding"
)

var constraints = []struct {
	name  string
	label string
	prop  string
}{
	{"adaptive_bench_decision_id", "AdaptiveDecision", "id"},
	{"adaptive_bench_context_hash", "AdaptiveContext", "hash"},
	{"adaptive_bench_action_key", "AdaptiveAction", "key"},
	{"adaptive_bench_outcome_id", "AdaptiveOutcome", "id"},
	{"adaptive_bench_policy_id", "AdaptivePolicySnapshot", "id"},
	{"adaptive_bench_head_scope", "AdaptivePolicyHead", "scope"},
}

type outcomeGraph struct {
	driver neo4j.DriverWithContext
	runID  string
}

type graphCounts struct {
	Decisions  int64 `json:"decisions"`
	Contexts   int64 `json:"contexts"`
	Actions    int64 `json:"actions"`
	Outcomes   int64 `json:"outcomes"`
	Considered int64 `json:"considered"`
}

func openGraph(ctx context.Context, password, runID string) (*outcomeGraph, error) {
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
	return &outcomeGraph{driver: driver, runID: runID}, nil
}

func (g *outcomeGraph) createSchema(ctx context.Context, dim int) error {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	for _, item := range constraints {
		query := fmt.Sprintf("CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.%s IS UNIQUE", item.name, item.label, item.prop)
		if result, err := session.Run(ctx, query, nil); err != nil {
			return err
		} else if _, err := result.Consume(ctx); err != nil {
			return err
		}
	}
	query := fmt.Sprintf(
		"CREATE VECTOR INDEX %s IF NOT EXISTS FOR (n:AdaptiveContext) ON (n.embedding) OPTIONS {indexConfig: {`vector.dimensions`: %d, `vector.similarity_function`: 'cosine'}}",
		contextIndex, dim,
	)
	if result, err := session.Run(ctx, query, nil); err != nil {
		return err
	} else if _, err := result.Consume(ctx); err != nil {
		return err
	}
	result, err := session.Run(ctx, "CALL db.awaitIndex($name, 120)", map[string]any{"name": contextIndex})
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (g *outcomeGraph) seedSnapshots(ctx context.Context, baselineID, candidateID string) error {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
MERGE (baseline:AdaptivePolicySnapshot {id: $baseline_id})
ON CREATE SET baseline.run_id = $run_id, baseline.status = 'active', baseline.kind = 'static_centroid'
MERGE (candidate:AdaptivePolicySnapshot {id: $candidate_id})
ON CREATE SET candidate.run_id = $run_id, candidate.status = 'shadow', candidate.kind = 'graph_knn'
MERGE (head:AdaptivePolicyHead {scope: $scope})
ON CREATE SET head.run_id = $run_id
MERGE (head)-[:ACTIVE]->(baseline)
`, map[string]any{
			"baseline_id": baselineID, "candidate_id": candidateID,
			"run_id": g.runID, "scope": g.runID + "/global",
		})
		if err != nil {
			return nil, err
		}
		_, err = result.Consume(ctx)
		return nil, err
	})
	return err
}

func (g *outcomeGraph) writeRows(ctx context.Context, rows []decisionRow) (time.Duration, error) {
	payload := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		considered := make([]map[string]any, 0, len(row.Considered))
		for _, alternative := range row.Considered {
			considered = append(considered, map[string]any{
				"key": alternative.Key, "name": alternative.Name,
				"score": alternative.Score, "rank": alternative.Rank,
			})
		}
		payload = append(payload, map[string]any{
			"decision_id": row.DecisionID, "context_hash": row.ContextHash,
			"scenario_id": row.ScenarioID, "domain": row.Domain, "prompt": row.Prompt,
			"embedding": row.Embedding, "action": row.Action, "action_key": row.Domain + "|" + row.Action,
			"propensity": row.Propensity, "policy_id": row.PolicyID, "delayed": row.Delayed,
			"outcome": map[string]any{
				"id": row.Outcome.ID, "correct": row.Outcome.Correct, "reward": row.Outcome.Reward,
				"tokens": row.Outcome.Tokens, "latency_ms": row.Outcome.LatencyMS, "source": row.Outcome.Source,
			},
			"considered": considered,
		})
	}
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	start := time.Now()
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
UNWIND $rows AS row
MERGE (c:AdaptiveContext {hash: row.context_hash})
ON CREATE SET c.run_id = $run_id, c.scenario_id = row.scenario_id,
              c.domain = row.domain, c.prompt = row.prompt, c.embedding = row.embedding
MERGE (a:AdaptiveAction {key: row.action_key})
ON CREATE SET a.run_id = $run_id, a.domain = row.domain, a.name = row.action
WITH row, c, a
MATCH (p:AdaptivePolicySnapshot {id: row.policy_id})
MERGE (d:AdaptiveDecision {id: row.decision_id})
ON CREATE SET d.run_id = $run_id, d.created_at = datetime(),
              d.domain = row.domain, d.propensity = row.propensity
MERGE (d)-[:FOR_CONTEXT]->(c)
MERGE (d)-[:CHOSEN]->(a)
MERGE (d)-[:FROM_POLICY]->(p)
WITH row, d
UNWIND row.considered AS alt
MERGE (aa:AdaptiveAction {key: alt.key})
ON CREATE SET aa.run_id = $run_id, aa.domain = row.domain, aa.name = alt.name
MERGE (d)-[considered:CONSIDERED]->(aa)
SET considered.score = alt.score, considered.rank = alt.rank
WITH DISTINCT row, d
FOREACH (_ IN CASE WHEN row.delayed THEN [] ELSE [1] END |
  MERGE (o:AdaptiveOutcome {id: row.outcome.id})
  ON CREATE SET o.run_id = $run_id, o.observed_at = datetime()
  SET o.correct = row.outcome.correct, o.reward = row.outcome.reward,
      o.tokens = row.outcome.tokens, o.latency_ms = row.outcome.latency_ms,
      o.source = row.outcome.source
  MERGE (d)-[:OBSERVED]->(o)
)
`, map[string]any{"rows": payload, "run_id": g.runID})
		if err != nil {
			return nil, err
		}
		_, err = result.Consume(ctx)
		return nil, err
	})
	return time.Since(start), err
}

func (g *outcomeGraph) attachDelayed(ctx context.Context, rows []decisionRow) error {
	payload := make([]map[string]any, 0)
	for _, row := range rows {
		if !row.Delayed {
			continue
		}
		payload = append(payload, map[string]any{
			"decision_id": row.DecisionID,
			"outcome": map[string]any{
				"id": row.Outcome.ID, "correct": row.Outcome.Correct, "reward": row.Outcome.Reward,
				"tokens": row.Outcome.Tokens, "latency_ms": row.Outcome.LatencyMS, "source": row.Outcome.Source,
			},
		})
	}
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
UNWIND $rows AS row
MATCH (d:AdaptiveDecision {id: row.decision_id, run_id: $run_id})
MERGE (o:AdaptiveOutcome {id: row.outcome.id})
ON CREATE SET o.run_id = $run_id, o.observed_at = datetime()
SET o.correct = row.outcome.correct, o.reward = row.outcome.reward,
    o.tokens = row.outcome.tokens, o.latency_ms = row.outcome.latency_ms,
    o.source = row.outcome.source
MERGE (d)-[:OBSERVED]->(o)
`, map[string]any{"rows": payload, "run_id": g.runID})
		if err != nil {
			return nil, err
		}
		_, err = result.Consume(ctx)
		return nil, err
	})
	return err
}

func (g *outcomeGraph) counts(ctx context.Context) (graphCounts, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `
MATCH (d:AdaptiveDecision {run_id: $run_id})
OPTIONAL MATCH (d)-[:FOR_CONTEXT]->(c:AdaptiveContext {run_id: $run_id})
OPTIONAL MATCH (d)-[:CHOSEN]->(a:AdaptiveAction {run_id: $run_id})
OPTIONAL MATCH (d)-[:OBSERVED]->(o:AdaptiveOutcome {run_id: $run_id})
OPTIONAL MATCH (d)-[r:CONSIDERED]->(:AdaptiveAction)
RETURN count(DISTINCT d) AS decisions, count(DISTINCT c) AS contexts,
       count(DISTINCT a) AS actions, count(DISTINCT o) AS outcomes,
       count(DISTINCT r) AS considered
`, map[string]any{"run_id": g.runID})
	if err != nil {
		return graphCounts{}, err
	}
	if !result.Next(ctx) {
		return graphCounts{}, fmt.Errorf("count query returned no row")
	}
	rec := result.Record()
	return graphCounts{
		Decisions: asInt64(rec, "decisions"), Contexts: asInt64(rec, "contexts"),
		Actions: asInt64(rec, "actions"), Outcomes: asInt64(rec, "outcomes"),
		Considered: asInt64(rec, "considered"),
	}, nil
}

func (g *outcomeGraph) neighborLatencies(ctx context.Context, embedding []float64, iterations int) ([]float64, error) {
	latencies := make([]float64, 0, iterations)
	for range iterations {
		session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
		start := time.Now()
		result, err := session.Run(ctx, `
CALL db.index.vector.queryNodes($index, 8, $embedding)
YIELD node, score
WHERE node:AdaptiveContext AND node.run_id = $run_id
MATCH (d:AdaptiveDecision)-[:FOR_CONTEXT]->(node)
MATCH (d)-[:CHOSEN]->(a:AdaptiveAction)
MATCH (d)-[:OBSERVED]->(o:AdaptiveOutcome)
RETURN node.hash AS context_hash, a.name AS action, avg(o.reward) AS reward, max(score) AS similarity
ORDER BY similarity DESC, reward DESC
LIMIT 5
`, map[string]any{"index": contextIndex, "embedding": embedding, "run_id": g.runID})
		if err != nil {
			_ = session.Close(ctx)
			return nil, err
		}
		if _, err := result.Collect(ctx); err != nil {
			_ = session.Close(ctx)
			return nil, err
		}
		latencies = append(latencies, float64(time.Since(start).Microseconds())/1000)
		_ = session.Close(ctx)
	}
	sort.Float64s(latencies)
	return latencies, nil
}

func (g *outcomeGraph) promoteAndRollback(ctx context.Context, baselineID, candidateID string) (string, string, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	scope := g.runID + "/global"
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
MATCH (head:AdaptivePolicyHead {scope: $scope})-[rel:ACTIVE]->(old:AdaptivePolicySnapshot)
MATCH (candidate:AdaptivePolicySnapshot {id: $candidate_id})
DELETE rel
SET old.status = 'retired', candidate.status = 'active',
    candidate.previous_id = old.id, candidate.promoted_at = datetime()
MERGE (head)-[:ACTIVE]->(candidate)
`, map[string]any{"scope": scope, "candidate_id": candidateID})
		if err != nil {
			return nil, err
		}
		_, err = result.Consume(ctx)
		return nil, err
	})
	if err != nil {
		return "", "", err
	}
	activeAfterPromote, err := g.activePolicy(ctx, scope)
	if err != nil {
		return "", "", err
	}
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
MATCH (head:AdaptivePolicyHead {scope: $scope})-[rel:ACTIVE]->(current:AdaptivePolicySnapshot {id: $candidate_id})
MATCH (previous:AdaptivePolicySnapshot {id: current.previous_id})
DELETE rel
SET current.status = 'rolled_back', previous.status = 'active',
    current.rolled_back_at = datetime()
MERGE (head)-[:ACTIVE]->(previous)
`, map[string]any{"scope": scope, "candidate_id": candidateID})
		if err != nil {
			return nil, err
		}
		_, err = result.Consume(ctx)
		return nil, err
	})
	if err != nil {
		return "", "", err
	}
	activeAfterRollback, err := g.activePolicy(ctx, scope)
	if err != nil {
		return "", "", err
	}
	if activeAfterRollback != baselineID {
		return "", "", fmt.Errorf("rollback active=%s, want %s", activeAfterRollback, baselineID)
	}
	return activeAfterPromote, activeAfterRollback, nil
}

func (g *outcomeGraph) activePolicy(ctx context.Context, scope string) (string, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `
MATCH (:AdaptivePolicyHead {scope: $scope})-[:ACTIVE]->(p:AdaptivePolicySnapshot)
RETURN p.id AS id
`, map[string]any{"scope": scope})
	if err != nil {
		return "", err
	}
	if !result.Next(ctx) {
		return "", fmt.Errorf("no active policy")
	}
	value, _ := result.Record().Get("id")
	return fmt.Sprint(value), nil
}

func (g *outcomeGraph) cleanup(ctx context.Context) error {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `
MATCH (n)
WHERE (n:AdaptiveDecision OR n:AdaptiveContext OR n:AdaptiveAction OR
       n:AdaptiveOutcome OR n:AdaptivePolicySnapshot OR n:AdaptivePolicyHead)
  AND n.run_id = $run_id
DETACH DELETE n
`, map[string]any{"run_id": g.runID})
	if err != nil {
		return err
	}
	if _, err := result.Consume(ctx); err != nil {
		return err
	}
	if result, err = session.Run(ctx, "DROP INDEX "+contextIndex+" IF EXISTS", nil); err == nil {
		_, err = result.Consume(ctx)
	}
	if err != nil {
		return err
	}
	for _, item := range constraints {
		result, err = session.Run(ctx, "DROP CONSTRAINT "+item.name+" IF EXISTS", nil)
		if err != nil {
			return err
		}
		if _, err := result.Consume(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (g *outcomeGraph) close(ctx context.Context) error {
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
