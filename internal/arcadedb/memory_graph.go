package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const graphTopologySemantics = "stored_topology_all_validity_windows"

// MemoryGraphRequest selects relationships; paths also accept a validity instant.
type MemoryGraphRequest struct {
	Relations string `json:"relations,omitempty" jsonschema:"facts (default), mentions, or combined"`
	AsOf      string `json:"as_of,omitempty" jsonschema:"RFC3339 validity instant for graph_path; graph_diagnostics does not support temporal projection"`
}

// MemoryGraphNode carries native structural metrics, not semantic importance.
type MemoryGraphNode struct {
	RID        string `json:"rid"`
	Name       string `json:"name"`
	Kind       string `json:"kind,omitempty"`
	Type       string `json:"type"`
	InDegree   int64  `json:"in_degree"`
	OutDegree  int64  `json:"out_degree"`
	Degree     int64  `json:"degree"`
	CoreNumber int64  `json:"core_number"`
	Component  int64  `json:"component"`
}

func memoryGraphRelations(request MemoryGraphRequest) (string, error) {
	if request.AsOf != "" {
		return "", fmt.Errorf("memory graph is stored topology, not an as_of projection")
	}
	switch request.Relations {
	case "", "facts":
		return "FACT", nil
	case "mentions":
		return "MENTIONS", nil
	case "combined":
		return "FACT,MENTIONS", nil
	default:
		return "", fmt.Errorf("graph relations must be facts, mentions, or combined")
	}
}

// Native procedures load every vertex before returning a bounded result. Cap
// that input, not only the returned rows. The engine's own working-memory guard
// remains authoritative if the database grows concurrently after this check.
func (c *Client) memoryGraphPreflight(ctx context.Context) error {
	schema, err := c.Schema(ctx)
	if err != nil {
		return err
	}
	remaining := int64(c.memoryLimits().GraphMaxRecords)
	for _, group := range [][]SchemaType{schema.Vertices, schema.Edges, schema.Documents} {
		for _, entry := range group {
			if entry.Records < 0 || entry.Records > remaining {
				return fmt.Errorf("graph exceeds AURA_MEMORY_GRAPH_MAX_RECORDS budget")
			}
			remaining -= entry.Records
		}
	}
	return nil
}

func (c *Client) memoryGraphNodes(ctx context.Context) ([]MemoryGraphNode, error) {
	if err := c.memoryGraphPreflight(ctx); err != nil {
		return nil, err
	}
	rows, err := c.Query(ctx, "SELECT @rid AS rid, @type AS type, name, kind FROM Entity ORDER BY name LIMIT :cap",
		map[string]any{"cap": int64(c.memoryLimits().GraphMaxRecords)})
	if err != nil {
		return nil, err
	}
	if len(rows) > c.memoryLimits().GraphMaxRecords {
		return nil, fmt.Errorf("entity graph grew beyond the record budget")
	}
	nodes := make([]MemoryGraphNode, 0, len(rows))
	for _, row := range rows {
		node := MemoryGraphNode{RID: rowString(row, "rid"), Type: rowString(row, "type"),
			Name: rowString(row, "name"), Kind: rowString(row, "kind")}
		if node.RID == "" || node.Type == "" || node.Name == "" {
			return nil, fmt.Errorf("entity graph contains an incomplete node")
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func memoryGraphLabels(nodes []MemoryGraphNode) string {
	types := make(map[string]struct{})
	for _, node := range nodes {
		types[node.Type] = struct{}{}
	}
	labels := make([]string, 0, len(types))
	for name := range types {
		labels = append(labels, name)
	}
	sort.Strings(labels)
	return strings.Join(labels, ",")
}
