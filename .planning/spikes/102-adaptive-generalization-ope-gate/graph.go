package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type factGraph struct {
	driver neo4j.DriverWithContext
	runID  string
	index  string
	dim    int
}

func openFactGraph(ctx context.Context, password, runID string, dim int) (*factGraph, error) {
	if strings.TrimSpace(password) == "" {
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
	return &factGraph{driver: driver, runID: runID, index: "adaptive_proof_fact_embedding", dim: dim}, nil
}

func (g *factGraph) seed(ctx context.Context, facts []knowledgeFact, vectors [][]float64) error {
	if len(facts) != len(vectors) {
		return fmt.Errorf("facts/vectors mismatch: %d/%d", len(facts), len(vectors))
	}
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if _, err := tx.Run(ctx, "MATCH (n:AdaptiveProofFact {run_id: $run_id}) DETACH DELETE n", map[string]any{"run_id": g.runID}); err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(facts))
		for i, fact := range facts {
			rows = append(rows, map[string]any{
				"id": fact.ID, "text": fact.Text, "related_id": fact.RelatedID, "embedding": vectors[i],
			})
		}
		if _, err := tx.Run(ctx, `
UNWIND $rows AS row
CREATE (n:AdaptiveProofFact {id: row.id, text: row.text, run_id: $run_id, embedding: row.embedding})
WITH row, n
OPTIONAL MATCH (target:AdaptiveProofFact {id: row.related_id, run_id: $run_id})
FOREACH (_ IN CASE WHEN target IS NULL THEN [] ELSE [1] END | CREATE (n)-[:RELATED_TO]->(target))
`, map[string]any{"rows": rows, "run_id": g.runID}); err != nil {
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return err
	}
	_, err = session.Run(ctx, fmt.Sprintf(
		"CREATE VECTOR INDEX %s IF NOT EXISTS FOR (n:AdaptiveProofFact) ON (n.embedding) OPTIONS {indexConfig: {`vector.dimensions`: %d, `vector.similarity_function`: 'cosine'}}",
		g.index, g.dim,
	), nil)
	if err != nil {
		return err
	}
	_, err = session.Run(ctx, "CALL db.awaitIndex($name, 120)", map[string]any{"name": g.index})
	return err
}

func (g *factGraph) contexts(ctx context.Context, embedding []float64) (string, string, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer func() { _ = session.Close(ctx) }()
	res, err := session.Run(ctx, `
CALL db.index.vector.queryNodes($index, 3, $embedding)
YIELD node, score
WHERE node:AdaptiveProofFact AND node.run_id = $run_id
RETURN node.text AS text
ORDER BY score DESC
LIMIT 1
`, map[string]any{"index": g.index, "embedding": embedding, "run_id": g.runID})
	if err != nil {
		return "", "", err
	}
	if !res.Next(ctx) {
		return "", "", fmt.Errorf("vector retrieval returned no fact")
	}
	anchor, _ := res.Record().Get("text")
	vectorContext, _ := anchor.(string)
	res, err = session.Run(ctx, `
CALL db.index.vector.queryNodes($index, 3, $embedding)
YIELD node, score
WHERE node:AdaptiveProofFact AND node.run_id = $run_id
WITH node ORDER BY score DESC LIMIT 1
OPTIONAL MATCH (node)-[:RELATED_TO]->(related:AdaptiveProofFact {run_id: $run_id})
RETURN node.text AS anchor, collect(related.text) AS related
`, map[string]any{"index": g.index, "embedding": embedding, "run_id": g.runID})
	if err != nil {
		return "", "", err
	}
	if !res.Next(ctx) {
		return "", "", fmt.Errorf("graph retrieval returned no fact")
	}
	rec := res.Record()
	anchor, _ = rec.Get("anchor")
	related, _ := rec.Get("related")
	parts := []string{fmt.Sprint(anchor)}
	if list, ok := related.([]any); ok {
		for _, item := range list {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				parts = append(parts, text)
			}
		}
	}
	return vectorContext, strings.Join(parts, "\n"), nil
}

func (g *factGraph) close(ctx context.Context) {
	if g == nil {
		return
	}
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	_, _ = session.Run(ctx, "MATCH (n:AdaptiveProofFact {run_id: $run_id}) DETACH DELETE n", map[string]any{"run_id": g.runID})
	_ = session.Close(ctx)
	_ = g.driver.Close(ctx)
}
