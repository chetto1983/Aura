package arcadedb

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemoryGraphPathRequest requires exact endpoints and a bounded native traversal.
type MemoryGraphPathRequest struct {
	MemoryGraphRequest
	Source    string `json:"source" jsonschema:"exact starting entity name"`
	Target    string `json:"target" jsonschema:"exact destination entity name"`
	Direction string `json:"direction,omitempty" jsonschema:"BOTH (default), OUT, or IN"`
	MaxDepth  int    `json:"max_depth,omitempty" jsonschema:"maximum hops, 1 to 6; default 3"`
}

// MemoryGraphPathEdge preserves original orientation even on a reverse traversal.
type MemoryGraphPathEdge struct {
	RID            string   `json:"rid"`
	Type           string   `json:"type"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	FactKey        string   `json:"fact_key,omitempty"`
	FactRID        string   `json:"fact_rid,omitempty"`
	Fact           *FactHit `json:"fact,omitempty"`
	SupportingFact *FactHit `json:"supporting_fact,omitempty"`
	SupportMissing bool     `json:"support_missing,omitempty"`
}

// MemoryGraphPath distinguishes stored topology from valid-time evidence.
type MemoryGraphPath struct {
	Semantics   string                `json:"semantics"`
	AsOf        string                `json:"as_of,omitempty"`
	Consistency string                `json:"consistency,omitempty"`
	Relations   string                `json:"relations"`
	Direction   string                `json:"direction"`
	MaxDepth    int                   `json:"max_depth"`
	Found       bool                  `json:"found"`
	Reason      string                `json:"reason,omitempty"`
	Nodes       []string              `json:"nodes"`
	Edges       []MemoryGraphPathEdge `json:"edges"`
}

// MemoryGraphPath reads a bounded native shortest path and its original evidence.
func (c *Client) MemoryGraphPath(ctx context.Context, request MemoryGraphPathRequest) (MemoryGraphPath, error) {
	out := MemoryGraphPath{Semantics: graphTopologySemantics, Nodes: []string{}, Edges: []MemoryGraphPathEdge{}}
	selection := request.MemoryGraphRequest
	selection.AsOf = ""
	relations, err := memoryGraphRelations(selection)
	if err != nil {
		return out, err
	}
	var asOf time.Time
	if request.AsOf != "" {
		asOf, err = time.Parse(time.RFC3339Nano, request.AsOf)
		if err != nil {
			return out, fmt.Errorf("graph as_of must be an RFC3339 instant: %w", err)
		}
		if asOf.IsZero() {
			return out, fmt.Errorf("graph as_of cannot be the zero instant")
		}
		asOf = asOf.UTC()
		out.AsOf, out.Semantics, out.Consistency = asOf.Format(time.RFC3339Nano), graphTemporalSemantics, "repeatable_read"
	}
	if request.Source == "" || request.Target == "" {
		return out, fmt.Errorf("graph path requires source and target entity names")
	}
	for _, name := range []string{request.Source, request.Target} {
		if err := validateRuneLimit("entity", name, c.memoryLimits().EntityRunes); err != nil {
			return out, err
		}
	}
	depth := request.MaxDepth
	if depth == 0 {
		depth = 3
	}
	if depth < 1 || depth > 6 {
		return out, fmt.Errorf("graph max_depth must be between 1 and 6")
	}
	direction := strings.ToUpper(request.Direction)
	if direction == "" {
		direction = "BOTH"
	}
	left, right := "-", "-"
	switch direction {
	case "OUT":
		right = "->"
	case "IN":
		left = "<-"
	case "BOTH":
	default:
		return out, fmt.Errorf("graph direction must be BOTH, OUT, or IN")
	}
	out.Relations, out.Direction, out.MaxDepth = relations, direction, depth
	nodes, err := c.memoryGraphNodes(ctx)
	if err != nil {
		return out, err
	}
	byName, byRID := map[string]MemoryGraphNode{}, map[string]MemoryGraphNode{}
	for _, node := range nodes {
		byName[node.Name], byRID[node.RID] = node, node
	}
	_, sourceFound := byName[request.Source]
	_, targetFound := byName[request.Target]
	if !sourceFound || !targetFound {
		out.Reason = "entity_not_found"
		return out, nil
	}
	if request.Source == request.Target {
		out.Found, out.Nodes = true, []string{request.Source}
		return out, nil
	}
	rows, err := c.readMemoryGraphPath(ctx, request, relations, left, right, depth, asOf)
	if err != nil {
		return out, err
	}
	if len(rows) == 0 {
		out.Reason = "no_path_within_depth"
		return out, nil
	}
	pathNodes, nodesOK := rows[0]["nodes"].([]any)
	pathEdges, edgesOK := rows[0]["edges"].([]any)
	if !nodesOK || !edgesOK || len(pathEdges) > depth || len(pathNodes) != len(pathEdges)+1 {
		return out, fmt.Errorf("native path returned an invalid or over-depth result")
	}
	for _, raw := range pathNodes {
		row, ok := raw.(map[string]any)
		node, found := byRID[rowString(row, "@rid")]
		if !ok || !found {
			return out, fmt.Errorf("native path left the entity snapshot")
		}
		if !asOf.IsZero() {
			node.Name, node.Kind = rowString(row, "name"), rowString(row, "kind")
			if node.Name == "" {
				return out, fmt.Errorf("native temporal path returned an unnamed entity")
			}
			byRID[node.RID] = node
		}
		out.Nodes = append(out.Nodes, node.Name)
	}
	for _, raw := range pathEdges {
		row, ok := raw.(map[string]any)
		from, fromOK := byRID[rowString(row, "@out")]
		to, toOK := byRID[rowString(row, "@in")]
		if !ok || !fromOK || !toOK {
			return out, fmt.Errorf("native path returned an unresolved relationship")
		}
		edge := MemoryGraphPathEdge{RID: rowString(row, "@rid"), Type: rowString(row, "@type"),
			From: from.Name, To: to.Name, FactKey: rowString(row, "fact_key"), FactRID: rowString(row, "fact_rid")}
		if edge.Type == factEdgeType {
			fact := factHitFromRow(row)
			fact.Subject, fact.Object, fact.SubjectKind, fact.ObjectKind = from.Name, to.Name, from.Kind, to.Kind
			edge.Fact = &fact
		} else if edge.Type != mentionsEdgeType {
			return out, fmt.Errorf("native path returned a disallowed relationship type")
		}
		out.Edges = append(out.Edges, edge)
	}
	if !asOf.IsZero() {
		if err := temporalPathSupport(rows[0], out.Edges, asOf); err != nil {
			return out, err
		}
	} else {
		if err := c.memoryGraphPathSupport(ctx, out.Edges); err != nil {
			return out, err
		}
	}
	out.Found = true
	return out, nil
}

func (c *Client) memoryGraphPathSupport(ctx context.Context, edges []MemoryGraphPathEdge) error {
	keys := make([]string, 0, len(edges))
	rids := make([]string, 0, len(edges))
	for _, edge := range edges {
		if edge.Type != mentionsEdgeType {
			continue
		}
		if edge.FactRID != "" {
			rids = append(rids, edge.FactRID)
		} else if edge.FactKey != "" {
			keys = append(keys, edge.FactKey)
		}
	}
	support := make(map[string]FactHit)
	if len(keys)+len(rids) > 0 {
		rows, err := c.Query(ctx, factsAboutProjection+" FROM FACT WHERE @rid IN :rids OR fact_key IN :keys LIMIT :cap",
			map[string]any{"keys": keys, "rids": rids, "cap": len(keys) + len(rids)})
		if err != nil {
			return err
		}
		for _, row := range rows {
			fact := factHitFromRow(row)
			if fact.FactKey != "" {
				support[fact.FactKey] = fact
			}
			if fact.RID != "" {
				support[fact.RID] = fact
			}
		}
	}
	for i := range edges {
		if edges[i].Type != mentionsEdgeType {
			continue
		}
		ref := edges[i].FactRID
		if ref == "" {
			ref = edges[i].FactKey
		}
		if fact, found := support[ref]; found {
			edges[i].SupportingFact = &fact
			edges[i].FactKey, edges[i].FactRID = fact.FactKey, fact.RID
		} else {
			edges[i].SupportMissing = true
		}
	}
	return nil
}
