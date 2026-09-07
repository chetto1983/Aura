package arcadedb

import (
	"context"
	"fmt"
	"sort"
)

// MemoryGraphDiagnosticsRequest limits returned details, independently of input size.
type MemoryGraphDiagnosticsRequest struct {
	MemoryGraphRequest
	Limit int `json:"limit,omitempty" jsonschema:"maximum node details returned, 1 to 100; default 20"`
}

// MemoryGraphDiagnostics distinguishes native zero-out-degree counts from isolates.
type MemoryGraphDiagnostics struct {
	Semantics      string            `json:"semantics"`
	Relations      string            `json:"relations"`
	NodeCount      int64             `json:"node_count"`
	EdgeCount      int64             `json:"edge_count"`
	IsolatedNodes  int64             `json:"isolated_nodes"`
	ZeroOutDegree  int64             `json:"zero_out_degree"`
	ComponentSizes []int64           `json:"component_sizes"`
	CoreHistogram  map[string]int64  `json:"core_histogram"`
	Nodes          []MemoryGraphNode `json:"nodes"`
	Truncated      bool              `json:"truncated"`
}

// MemoryGraphDiagnostics measures the stored entity topology using native procedures.
func (c *Client) MemoryGraphDiagnostics(ctx context.Context, request MemoryGraphDiagnosticsRequest) (MemoryGraphDiagnostics, error) {
	out := MemoryGraphDiagnostics{Semantics: graphTopologySemantics, Nodes: []MemoryGraphNode{},
		ComponentSizes: []int64{}, CoreHistogram: map[string]int64{}}
	relations, err := memoryGraphRelations(request.MemoryGraphRequest)
	if err != nil {
		return out, err
	}
	out.Relations = relations
	if request.Limit < 0 || request.Limit > c.memoryLimits().Results {
		return out, fmt.Errorf("graph detail limit must be between 1 and %d", c.memoryLimits().Results)
	}
	limit := boundedLimit(request.Limit, 20, c.memoryLimits().Results)
	nodes, err := c.memoryGraphNodes(ctx)
	if err != nil || len(nodes) == 0 {
		return out, err
	}
	params := map[string]any{"relations": relations, "labels": memoryGraphLabels(nodes)}
	rows, err := c.Read(ctx, "CALL algo.graphSummary($relations,$labels) YIELD nodeCount,edgeCount,isolatedNodes RETURN nodeCount,edgeCount,isolatedNodes", params)
	if err != nil {
		return out, err
	}
	if len(rows) != 1 || intField(rows[0]["nodeCount"]) != int64(len(nodes)) {
		return out, fmt.Errorf("native graph summary does not cover the entity snapshot")
	}
	out.NodeCount, out.EdgeCount = intField(rows[0]["nodeCount"]), intField(rows[0]["edgeCount"])
	// Verified on the installed engine: this native field counts zero OUT-degree,
	// so a connected sink qualifies. Actual isolates come from degree(BOTH).
	out.ZeroOutDegree = intField(rows[0]["isolatedNodes"])
	index := make(map[string]int, len(nodes))
	for i, node := range nodes {
		index[node.RID] = i
	}
	for _, query := range []string{
		"CALL algo.degree($relations,'BOTH') YIELD node,inDegree,outDegree,degree RETURN node,inDegree,outDegree,degree",
		"CALL algo.wcc($relations) YIELD node,componentId RETURN node,componentId",
		"CALL algo.kcore($relations) YIELD node,coreNumber RETURN node,coreNumber",
	} {
		rows, err = c.Read(ctx, query, params)
		if err != nil {
			return out, err
		}
		seen := make(map[string]struct{})
		for _, row := range rows {
			rid := rowString(row, "node")
			i, ok := index[rid]
			if !ok {
				continue
			}
			seen[rid] = struct{}{}
			if value, ok := row["degree"]; ok {
				nodes[i].Degree = intField(value)
				nodes[i].InDegree, nodes[i].OutDegree = intField(row["inDegree"]), intField(row["outDegree"])
			}
			if value, ok := row["componentId"]; ok {
				nodes[i].Component = intField(value)
			}
			if value, ok := row["coreNumber"]; ok {
				nodes[i].CoreNumber = intField(value)
			}
		}
		if len(seen) != len(nodes) {
			return out, fmt.Errorf("native graph procedure returned an incomplete entity snapshot")
		}
	}
	components := make(map[int64]int64)
	var degreeSum int64
	for _, node := range nodes {
		degreeSum += node.Degree
		components[node.Component]++
		out.CoreHistogram[fmt.Sprint(node.CoreNumber)]++
		if node.Degree == 0 {
			out.IsolatedNodes++
		}
	}
	if degreeSum != 2*out.EdgeCount {
		return out, fmt.Errorf("selected relationships leave the entity snapshot or changed during analysis")
	}
	for _, size := range components {
		out.ComponentSizes = append(out.ComponentSizes, size)
	}
	sort.Slice(out.ComponentSizes, func(i, j int) bool { return out.ComponentSizes[i] > out.ComponentSizes[j] })
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].CoreNumber != nodes[j].CoreNumber {
			return nodes[i].CoreNumber > nodes[j].CoreNumber
		}
		if nodes[i].Degree != nodes[j].Degree {
			return nodes[i].Degree > nodes[j].Degree
		}
		return nodes[i].Name < nodes[j].Name
	})
	out.Truncated = len(nodes) > limit
	out.Nodes = nodes[:min(len(nodes), limit)]
	return out, nil
}
