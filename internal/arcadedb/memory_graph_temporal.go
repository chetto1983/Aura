package arcadedb

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const graphTemporalSemantics = "valid_time_stored_relationships"

func cypherValidAt(alias string) string {
	return alias + ".valid_from <= localdatetime($at) AND (" + alias + ".valid_to IS NULL OR " + alias + ".valid_to > localdatetime($at))"
}

const cypherFactMap = "{rid:elementId(f),fact_key:f.fact_key,statement:f.statement,predicate:f.predicate,sources:f.sources,valid_from:f.valid_from,valid_to:f.valid_to,subject:a.name,subject_kind:a.kind,object:b.name,object_kind:b.kind}"

func cypherMentionAdmissible(mention, fact string) string {
	return "EXISTS { MATCH ()-[" + fact + ":FACT]->() WHERE ((" + mention + ".fact_rid IS NOT NULL AND elementId(" + fact + ")=toString(" + mention + ".fact_rid)) OR (" + mention + ".fact_rid IS NULL AND " + fact + ".fact_key=" + mention + ".fact_key)) AND " + cypherValidAt(fact) + " }"
}

func (c *Client) readMemoryGraphPath(ctx context.Context, request MemoryGraphPathRequest, relations, left, right string, depth int, at time.Time) ([]map[string]any, error) {
	// Only validated type names, punctuation and depth enter the pattern; names
	// and dates remain parameters. Filtering after shortestPath loses alternatives.
	relationship := "[:" + strings.ReplaceAll(relations, ",", "|") + fmt.Sprintf("*..%d", depth)
	projection := " RETURN nodes(p) AS nodes, relationships(p) AS edges"
	params := map[string]any{"source": request.Source, "target": request.Target}
	if !at.IsZero() {
		predicate := "(type(r)='FACT' AND " + cypherValidAt("r") + ") OR (type(r)='MENTIONS' AND " + cypherMentionAdmissible("r", "f") + ")"
		relationship = "[r:" + strings.ReplaceAll(relations, ",", "|") + fmt.Sprintf("*..%d WHERE %s", depth, predicate)
		params["at"] = at.UTC().Format("2006-01-02T15:04:05.999999999")
		projection = " WITH nodes(p) AS nodes, relationships(p) AS edges MATCH (a:Entity)-[f:FACT]->(b:Entity) WHERE elementId(f) IN [r IN edges | elementId(r)] OR elementId(f) IN [r IN edges | toString(r.fact_rid)] OR f.fact_key IN [r IN edges WHERE r.fact_rid IS NULL | r.fact_key] RETURN nodes,edges,collect(" + cypherFactMap + ") AS supports"
	}
	query := "MATCH (s:Entity {name:$source}), (t:Entity {name:$target}), p=shortestPath((s)" + left + relationship + "]" + right + "(t))" + projection
	if at.IsZero() {
		return c.Read(ctx, query, params)
	}
	return c.readRepeatable(ctx, query, params)
}

func temporalPathSupport(row map[string]any, edges []MemoryGraphPathEdge, at time.Time) error {
	values, ok := row["supports"].([]any)
	if !ok {
		return fmt.Errorf("native temporal path returned no evidence projection")
	}
	support := make(map[string]FactHit, len(values))
	for _, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("native temporal path returned malformed evidence")
		}
		fact := factHitFromRow(record)
		if err := validateTemporalFact(&fact, at); err != nil {
			return err
		}
		ref := fact.RID
		if ref == "" {
			ref = fact.FactKey
		}
		if _, exists := support[ref]; exists {
			return fmt.Errorf("native temporal evidence repeats a fact key")
		}
		support[ref] = fact
		if fact.FactKey != "" {
			support[fact.FactKey] = fact
		}
	}
	for i := range edges {
		edge := &edges[i]
		ref := edge.FactRID
		if edge.Type == factEdgeType {
			ref = edge.RID
		}
		if ref == "" {
			ref = edge.FactKey
		}
		fact, ok := support[ref]
		if !ok {
			return fmt.Errorf("native temporal path has an unsupported relationship")
		}
		if edge.Type == factEdgeType {
			if edge.Fact == nil || edge.From != fact.Subject || edge.To != fact.Object {
				return fmt.Errorf("native temporal fact endpoints disagree with evidence")
			}
			if err := validateTemporalFact(edge.Fact, at); err != nil {
				return err
			}
			if edge.Fact.FactKey != fact.FactKey || edge.Fact.Statement != fact.Statement || edge.Fact.Predicate != fact.Predicate || edge.Fact.ValidFrom != fact.ValidFrom || edge.Fact.ValidTo != fact.ValidTo {
				return fmt.Errorf("native temporal fact disagrees with supporting projection")
			}
			edge.Fact = &fact
		} else {
			edge.SupportingFact = &fact
		}
		edge.FactRID, edge.FactKey = fact.RID, fact.FactKey
	}
	return nil
}

func validateTemporalFact(fact *FactHit, at time.Time) error {
	from, err := parseMemoryBatchTime(fact.ValidFrom)
	if err != nil || from.IsZero() || from.After(at) {
		return fmt.Errorf("native temporal evidence has an inadmissible valid_from")
	}
	until, err := parseMemoryBatchTime(fact.ValidTo)
	if err != nil || (!until.IsZero() && !at.Before(until)) {
		return fmt.Errorf("native temporal evidence has an inadmissible valid_to")
	}
	if (fact.FactKey == "" && fact.RID == "") || fact.Subject == "" || fact.Object == "" || fact.Statement == "" || fact.Predicate == "" || len(fact.Sources) == 0 {
		return fmt.Errorf("native temporal evidence is incomplete")
	}
	fact.ValidFrom = from.Format(time.RFC3339Nano)
	if !until.IsZero() {
		fact.ValidTo = until.Format(time.RFC3339Nano)
	}
	return nil
}
