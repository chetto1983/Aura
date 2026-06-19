// Row-map → contract normalization (Phase 27, GRAPH-01/03). knowledge.Client.Read
// returns []map[string]any; each seed/expand row carries a SOURCE node (s_*), an
// optional NEIGHBOR node (n_*), and an optional relationship (r_*). This builds the
// de-duped node/edge sets, decodes labels from the apoc.convert.toJson STRING via
// json.Unmarshal (lists return NULL through the read tool — Pitfall 3), and derives
// GraphNode.Citations from the already-fetched edges (NO extra Cypher, D-09).
package knowledge

import (
	"encoding/json"
	"sort"
)

// citationLabels are the neighbor labels that count as evidence a node connects to
// (GRAPH-03 / SC3). A node adjacent to a neighbor carrying one of these gets that
// neighbor named in its Citations list.
var citationLabels = map[string]bool{"Document": true, "Source": true}

// normalizeRows projects seed/expand rows into the flat contract. op is carried for
// future op-specific shaping; today seed + expand share the s_*/n_*/r_* shape.
func normalizeRows(_ string, rows []map[string]any) GraphResult {
	nodes := map[string]*GraphNode{}
	order := []string{} // stable node insertion order
	edges := map[string]*GraphEdge{}
	edgeOrder := []string{}

	addNode := func(idKey, labelsKey, typeKey, capKey, propsKey string, row map[string]any) {
		id := asString(row[idKey])
		if id == "" {
			return
		}
		if _, ok := nodes[id]; ok {
			return // de-dupe by elementId
		}
		n := &GraphNode{
			ID:         id,
			Caption:    asString(row[capKey]),
			Labels:     decodeLabels(row[labelsKey]),
			EntityType: asString(row[typeKey]),
			Props:      asMap(row[propsKey]),
		}
		nodes[id] = n
		order = append(order, id)
	}

	for _, row := range rows {
		addNode("s_id", "s_labels", "s_entity_type", "s_caption", "s_props", row)
		addNode("n_id", "n_labels", "n_entity_type", "n_caption", "n_props", row)

		rid := asString(row["r_id"])
		src := asString(row["r_src"])
		dst := asString(row["r_dst"])
		if rid == "" || src == "" || dst == "" {
			continue
		}
		if _, ok := edges[rid]; ok {
			continue
		}
		edges[rid] = &GraphEdge{ID: rid, Source: src, Target: dst, RelType: asString(row["r_type"])}
		edgeOrder = append(edgeOrder, rid)
	}

	deriveCitations(nodes, edges)
	deriveDegree(nodes, edges)

	return GraphResult{
		Nodes: collectNodes(nodes, order),
		Edges: collectEdges(edges, edgeOrder),
	}
}

// deriveCitations fills GraphNode.Citations from the already-fetched edges: for
// each node, any neighbor whose decoded labels include Document/Source is named as
// a citation (its caption, falling back to its id). Costs NO extra Cypher (D-09).
func deriveCitations(nodes map[string]*GraphNode, edges map[string]*GraphEdge) {
	isEvidence := func(id string) bool {
		n, ok := nodes[id]
		if !ok {
			return false
		}
		for _, l := range n.Labels {
			if citationLabels[l] {
				return true
			}
		}
		return false
	}
	cite := func(self, neighbor string) {
		n, ok := nodes[self]
		if !ok || !isEvidence(neighbor) {
			return
		}
		nb := nodes[neighbor]
		name := nb.Caption
		if name == "" {
			name = neighbor
		}
		for _, existing := range n.Citations {
			if existing == name {
				return
			}
		}
		n.Citations = append(n.Citations, name)
	}
	for _, e := range edges {
		// an evidence neighbor on either end is a citation for the OTHER end
		cite(e.Source, e.Target)
		cite(e.Target, e.Source)
	}
}

// deriveDegree counts each node's incident edges (in the fetched subgraph).
func deriveDegree(nodes map[string]*GraphNode, edges map[string]*GraphEdge) {
	for _, e := range edges {
		if n, ok := nodes[e.Source]; ok {
			n.Degree++
		}
		if n, ok := nodes[e.Target]; ok {
			n.Degree++
		}
	}
}

func collectNodes(nodes map[string]*GraphNode, order []string) []GraphNode {
	out := make([]GraphNode, 0, len(order))
	for _, id := range order {
		out = append(out, *nodes[id])
	}
	return out
}

func collectEdges(edges map[string]*GraphEdge, order []string) []GraphEdge {
	out := make([]GraphEdge, 0, len(order))
	for _, id := range order {
		out = append(out, *edges[id])
	}
	return out
}

// normalizeSchema decodes the single schema-overview row (D-06): each list field is
// a apoc.convert.toJson STRING that json.Unmarshal turns back into a []string.
func normalizeSchema(rows []map[string]any) GraphSchema {
	if len(rows) == 0 {
		return GraphSchema{Labels: []string{}, RelTypes: []string{}}
	}
	row := rows[0]
	sch := GraphSchema{
		Labels:       decodeStrings(row["labels_json"]),
		RelTypes:     decodeStrings(row["rel_types_json"]),
		PropertyKeys: decodeStrings(row["prop_keys_json"]),
		EntityTypes:  decodeStrings(row["entity_types_json"]),
	}
	sort.Strings(sch.Labels)
	sort.Strings(sch.RelTypes)
	sort.Strings(sch.PropertyKeys)
	sort.Strings(sch.EntityTypes)
	if sch.Labels == nil {
		sch.Labels = []string{}
	}
	if sch.RelTypes == nil {
		sch.RelTypes = []string{}
	}
	return sch
}

// decodeLabels unmarshals a apoc.convert.toJson(labels(n)) string into []string,
// guarding against missing/NULL columns (returns nil, which omitempty drops).
func decodeLabels(v any) []string { return decodeStrings(v) }

// decodeStrings turns a JSON-array STRING (the toJson shape) into a []string. A
// missing/NULL/empty column yields nil. It also tolerates a native []any (in case
// a future driver returns the list directly) for robustness.
func decodeStrings(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" || t == "null" {
			return nil
		}
		var out []string
		if err := json.Unmarshal([]byte(t), &out); err != nil {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
