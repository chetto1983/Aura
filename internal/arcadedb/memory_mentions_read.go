package arcadedb

import (
	"context"
	"fmt"
	"time"
)

func (c *Client) factsAboutNeighborhood(ctx context.Context, entity, predicate string, limit int, at time.Time) ([]FactHit, error) {
	if err := c.memoryGraphPreflight(ctx); err != nil {
		return nil, err
	}
	// A shared mention costs two hops. Every bridge must retain admissible
	// support; filtering only the final facts admitted orphan/expired bridges.
	query := "MATCH (s:Entity {name:$entity}), p=(s)-[m:MENTIONS*0..2 WHERE " + cypherMentionAdmissible("m", "support") + "]-(near:Entity) WITH s,collect(DISTINCT near) AS nearby MATCH (a:Entity)-[f:FACT]->(b:Entity) WHERE (a IN nearby OR b IN nearby) AND " + cypherValidAt("f")
	params := map[string]any{"entity": entity, "at": at.UTC().Format("2006-01-02T15:04:05.999999999"), "limit": limit}
	if predicate != "" {
		query += " AND f.predicate=$predicate"
		params["predicate"] = predicate
	}
	query += " RETURN " + cypherFactMap + " AS fact ORDER BY CASE WHEN a=s OR b=s THEN 0 ELSE 1 END, f.created_at DESC,f.fact_key ASC LIMIT $limit"
	rows, err := c.readRepeatable(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: temporal neighborhood for %q: %w", entity, err)
	}
	if len(rows) > limit {
		return nil, fmt.Errorf("native neighborhood exceeded the fact limit")
	}
	hits := make([]FactHit, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		record, ok := row["fact"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("native neighborhood returned malformed evidence")
		}
		fact := factHitFromRow(record)
		if err := validateTemporalFact(&fact, at); err != nil {
			return nil, err
		}
		ref := fact.RID
		if ref == "" {
			ref = fact.FactKey
		}
		if seen[ref] {
			return nil, fmt.Errorf("native neighborhood repeated a fact")
		}
		seen[ref] = true
		hits = append(hits, fact)
	}
	return hits, nil
}
