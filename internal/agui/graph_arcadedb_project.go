package agui

import (
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// mergeStudioGraph mirrors ArcadeDB Studio's cumulative graph behavior: records
// already drawn keep their place and a repeated RID is ignored.
func mergeStudioGraph(dst *arcadedb.StudioGraph, src arcadedb.StudioGraph) {
	vertices := make(map[string]struct{}, len(dst.Vertices)+len(src.Vertices))
	for _, vertex := range dst.Vertices {
		vertices[vertex.RID] = struct{}{}
	}
	for _, vertex := range src.Vertices {
		if vertex.RID == "" {
			continue
		}
		if _, exists := vertices[vertex.RID]; exists {
			continue
		}
		vertices[vertex.RID] = struct{}{}
		dst.Vertices = append(dst.Vertices, vertex)
	}
	edges := make(map[string]struct{}, len(dst.Edges)+len(src.Edges))
	for _, edge := range dst.Edges {
		edges[edge.RID] = struct{}{}
	}
	for _, edge := range src.Edges {
		if edge.RID == "" {
			continue
		}
		if _, exists := edges[edge.RID]; exists {
			continue
		}
		edges[edge.RID] = struct{}{}
		dst.Edges = append(dst.Edges, edge)
	}
}

// projectStudioGraph maps the compact Studio keys onto Aura's stable graph
// contract. ArcadeDB documents the studio serializer as the graph-aware query
// representation; Studio uses o as source and i as target:
// https://docs.arcadedb.com/arcadedb/reference/http-api/http
func projectStudioGraph(
	raw arcadedb.StudioGraph,
	in GraphIntent,
	schema GraphSchema,
	nodeCap int,
	edgeCap int,
) GraphResult {
	allowedLabels := stringSet(in.Labels)
	allowedRels := stringSet(in.RelTypes)
	nodes := make([]GraphNode, 0, min(len(raw.Vertices), nodeCap))
	included := make(map[string]struct{}, min(len(raw.Vertices), nodeCap))
	truncated := false
	for _, vertex := range raw.Vertices {
		if vertex.RID == "" || vertex.Type == "" || !setAllows(allowedLabels, vertex.Type) {
			continue
		}
		if _, duplicate := included[vertex.RID]; duplicate {
			continue
		}
		if len(nodes) == nodeCap {
			truncated = true
			continue
		}
		props := displayGraphProperties(vertex.Properties)
		nodes = append(nodes, GraphNode{
			ID:         vertex.RID,
			Caption:    graphCaption(props, vertex.Type),
			Labels:     []string{vertex.Type},
			EntityType: propertyString(props, "kind"),
			Degree:     vertex.Degree(),
			Props:      props,
		})
		included[vertex.RID] = struct{}{}
	}
	edges := make([]GraphEdge, 0, min(len(raw.Edges), edgeCap))
	seenEdges := make(map[string]struct{}, min(len(raw.Edges), edgeCap))
	for _, edge := range raw.Edges {
		if edge.RID == "" || !setAllows(allowedRels, edge.Type) {
			continue
		}
		if _, duplicate := seenEdges[edge.RID]; duplicate {
			continue
		}
		if _, ok := included[edge.Out]; !ok {
			continue
		}
		if _, ok := included[edge.In]; !ok {
			continue
		}
		if len(edges) == edgeCap {
			truncated = true
			continue
		}
		edges = append(edges, GraphEdge{
			ID: edge.RID, Source: edge.Out, Target: edge.In,
			RelType: edge.Type, Caption: propertyString(edge.Properties, "predicate"),
		})
		seenEdges[edge.RID] = struct{}{}
	}
	return GraphResult{Nodes: nodes, Edges: edges, Schema: schema, Truncated: truncated}
}

func displayGraphProperties(properties map[string]any) map[string]any {
	if len(properties) == 0 {
		return nil
	}
	out := make(map[string]any, len(properties))
	for key, value := range properties {
		if strings.EqualFold(key, "embedding") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func graphCaption(properties map[string]any, fallback string) string {
	for _, key := range []string{"name", "title"} {
		if caption := propertyString(properties, key); caption != "" {
			return caption
		}
	}
	return fallback
}

func propertyString(properties map[string]any, key string) string {
	value, _ := properties[key].(string)
	return strings.TrimSpace(value)
}

func stringSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func setAllows(set map[string]struct{}, value string) bool {
	if len(set) == 0 {
		return true
	}
	_, ok := set[value]
	return ok
}
