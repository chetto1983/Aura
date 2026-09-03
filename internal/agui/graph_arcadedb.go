package agui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

const (
	defaultGraphNodeCap = 75
	defaultGraphEdgeCap = 200
	maxGraphNodeCap     = 75
	maxGraphEdgeCap     = arcadedb.MaxStudioGraphLimit
)

var (
	errInvalidArcadeRID = errors.New("graphview: invalid ArcadeDB RID")
	arcadeRIDPattern    = regexp.MustCompile(`^#[0-9]+:[0-9]+$`)
	sqlTypePattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ArcadeGraphView answers the cockpit's graph routes from one identity's
// ArcadeDB type catalogue and read-only Studio graph queries.
type ArcadeGraphView struct {
	store ArcadeGraphReader
}

// ArcadeGraphReader keeps the identity explicit on every read. Aura stores one
// ArcadeDB database per identity, so an implicit/default database would be a
// cross-tenant failure mode.
type ArcadeGraphReader interface {
	Schema(ctx context.Context, identityID string) (arcadedb.Schema, error)
	QueryGraph(
		ctx context.Context,
		identityID string,
		statement string,
		params map[string]any,
		limit int,
	) (arcadedb.StudioGraph, error)
}

// NewArcadeGraphView wraps the tenant-scoped store as the GraphView consumed by
// the authenticated routes.
func NewArcadeGraphView(store ArcadeGraphReader) *ArcadeGraphView {
	return &ArcadeGraphView{store: store}
}

// Schema projects the identity's ArcadeDB type catalogue onto the wire contract.
func (v *ArcadeGraphView) Schema(ctx context.Context, identityID string) (GraphSchema, error) {
	catalogue, err := v.store.Schema(ctx, identityID)
	if err != nil {
		return GraphSchema{}, err
	}
	return projectArcadeSchema(catalogue), nil
}

// Query compiles only the two structured, bounded graph operations. Neither
// operation accepts conversation scope because one ArcadeDB database already
// represents all memory owned by the authenticated identity.
func (v *ArcadeGraphView) Query(ctx context.Context, in GraphIntent) (GraphResult, error) {
	switch in.Op {
	case OpOverview:
		if in.NodeID != "" {
			return GraphResult{}, errors.New("graphview: node_id is only valid for expand")
		}
	case OpExpand:
		if !arcadeRIDPattern.MatchString(in.NodeID) {
			return GraphResult{}, errInvalidArcadeRID
		}
	default:
		return GraphResult{}, errors.New("graphview: unknown op")
	}
	catalogue, err := v.store.Schema(ctx, in.UserID)
	if err != nil {
		return GraphResult{}, err
	}
	schema := projectArcadeSchema(catalogue)
	nodeCap := boundedGraphCap(in.NodeCap, defaultGraphNodeCap, maxGraphNodeCap)
	edgeCap := boundedGraphCap(in.EdgeCap, defaultGraphEdgeCap, maxGraphEdgeCap)

	if in.Op == OpOverview {
		return v.overview(ctx, in, catalogue, schema, nodeCap, edgeCap)
	}
	return v.expand(ctx, in, catalogue, schema, nodeCap, edgeCap)
}

func (v *ArcadeGraphView) overview(
	ctx context.Context,
	in GraphIntent,
	catalogue arcadedb.Schema,
	schema GraphSchema,
	nodeCap int,
	edgeCap int,
) (GraphResult, error) {
	edgeTypes, err := selectedSchemaTypes(catalogue.Edges, in.RelTypes)
	if err != nil {
		return GraphResult{}, err
	}
	vertexTypes, err := selectedSchemaTypes(catalogue.Vertices, in.Labels)
	if err != nil {
		return GraphResult{}, err
	}

	raw := arcadedb.StudioGraph{Vertices: []arcadedb.StudioVertex{}, Edges: []arcadedb.StudioEdge{}}
	statements := make([]string, 0, len(edgeTypes)+len(vertexTypes))
	for _, edgeType := range edgeTypes {
		remaining := edgeCap - len(raw.Edges)
		if remaining <= 0 {
			break
		}
		statement, err := selectTypeStatement(edgeType.Name, remaining)
		if err != nil {
			return GraphResult{}, err
		}
		graph, err := v.store.QueryGraph(ctx, in.UserID, statement, nil, remaining)
		if err != nil {
			return GraphResult{}, err
		}
		mergeStudioGraph(&raw, graph)
		statements = append(statements, statement)
	}
	// The budget counts the vertices the CALLER ASKED FOR, not everything accumulated so
	// far. The edge queries above run first and drag their endpoints in with them — turns,
	// entities, whatever the relationship touched — and those are types the caller may not
	// have selected at all. Counting them here let a label filter come back EMPTY: with
	// every relationship type still selected and one small label chosen, the endpoints
	// filled nodeCap before this loop ran, it broke immediately, the chosen type was never
	// queried, and projectStudioGraph then discarded every accumulated vertex for having
	// the wrong label. Measured 2026-09-03 in the cockpit: filtering to ReasoningToolCall
	// returned 0 nodes and 0 connections while the database held 7 of them.
	//
	// With no label selected the set allows everything, so this is the previous count and
	// the previous bound.
	requested := stringSet(in.Labels)
	for _, vertexType := range vertexTypes {
		if countAllowedVertices(raw.Vertices, requested) >= nodeCap {
			break
		}
		// Edge queries already return their endpoints. Read a full node-cap page
		// so duplicate endpoints do not consume the small "remaining" window and
		// hide isolated vertices later in the type.
		statement, err := selectTypeStatement(vertexType.Name, nodeCap)
		if err != nil {
			return GraphResult{}, err
		}
		graph, err := v.store.QueryGraph(ctx, in.UserID, statement, nil, nodeCap)
		if err != nil {
			return GraphResult{}, err
		}
		mergeStudioGraph(&raw, graph)
		statements = append(statements, statement)
	}

	result := projectStudioGraph(raw, in, schema, nodeCap, edgeCap)
	result.Query = strings.Join(statements, "\n")
	result.Truncated = result.Truncated || catalogueExceedsCaps(edgeTypes, vertexTypes, edgeCap, nodeCap)
	return result, nil
}

func (v *ArcadeGraphView) expand(
	ctx context.Context,
	in GraphIntent,
	catalogue arcadedb.Schema,
	schema GraphSchema,
	nodeCap int,
	edgeCap int,
) (GraphResult, error) {
	edgeTypes, err := selectedSchemaTypes(catalogue.Edges, in.RelTypes)
	if err != nil {
		return GraphResult{}, err
	}
	statement, err := expandStatement(in.NodeID, edgeTypes, edgeCap)
	if err != nil {
		return GraphResult{}, err
	}
	graph, err := v.store.QueryGraph(ctx, in.UserID, statement, nil, edgeCap)
	if err != nil {
		return GraphResult{}, err
	}
	result := projectStudioGraph(graph, in, schema, nodeCap, edgeCap)
	result.Query = statement
	result.Truncated = result.Truncated || len(graph.Edges) >= edgeCap || len(graph.Vertices) >= nodeCap
	return result, nil
}

func boundedGraphCap(requested, fallback, maximum int) int {
	if requested <= 0 {
		return fallback
	}
	if requested > maximum {
		return maximum
	}
	return requested
}

func selectedSchemaTypes(available []arcadedb.SchemaType, selected []string) ([]arcadedb.SchemaType, error) {
	if len(selected) == 0 {
		return available, nil
	}
	wanted := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		wanted[name] = struct{}{}
	}
	out := make([]arcadedb.SchemaType, 0, len(selected))
	for _, entry := range available {
		if _, ok := wanted[entry.Name]; ok {
			out = append(out, entry)
			delete(wanted, entry.Name)
		}
	}
	if len(wanted) != 0 {
		return nil, errors.New("graphview: filter is absent from ArcadeDB schema")
	}
	return out, nil
}

func selectTypeStatement(typeName string, limit int) (string, error) {
	if !sqlTypePattern.MatchString(typeName) {
		return "", errors.New("graphview: unsafe ArcadeDB type name")
	}
	return "SELECT FROM `" + typeName + "` LIMIT " + strconv.Itoa(limit), nil
}

func expandStatement(rid string, edgeTypes []arcadedb.SchemaType, limit int) (string, error) {
	if !arcadeRIDPattern.MatchString(rid) {
		return "", errInvalidArcadeRID
	}
	args := make([]string, 0, len(edgeTypes))
	for _, edgeType := range edgeTypes {
		if !sqlTypePattern.MatchString(edgeType.Name) {
			return "", errors.New("graphview: unsafe ArcadeDB type name")
		}
		args = append(args, "'"+edgeType.Name+"'")
	}
	return fmt.Sprintf("SELECT expand(bothE(%s)) FROM %s LIMIT %d", strings.Join(args, ","), rid, limit), nil
}

func catalogueExceedsCaps(edges, vertices []arcadedb.SchemaType, edgeCap, nodeCap int) bool {
	var edgeCount, vertexCount int64
	for _, entry := range edges {
		edgeCount += entry.Records
	}
	for _, entry := range vertices {
		vertexCount += entry.Records
	}
	return edgeCount > int64(edgeCap) || vertexCount > int64(nodeCap)
}

// projectArcadeSchema maps graph-capable catalogue types onto the wire schema.
// Document types are counted but are not labels because they cannot be an edge
// endpoint. EntityTypes remains data-derived and is intentionally omitted.
func projectArcadeSchema(in arcadedb.Schema) GraphSchema {
	out := GraphSchema{
		Labels:   make([]string, 0, len(in.Vertices)),
		RelTypes: make([]string, 0, len(in.Edges)),
	}
	counts := make(map[string]int, len(in.Vertices)+len(in.Edges)+len(in.Documents))
	keys := make(map[string]struct{})
	for _, group := range [][]arcadedb.SchemaType{in.Vertices, in.Edges, in.Documents} {
		for _, entry := range group {
			counts[entry.Name] = int(entry.Records)
			for _, property := range entry.Properties {
				keys[property] = struct{}{}
			}
		}
	}
	for _, vertex := range in.Vertices {
		out.Labels = append(out.Labels, vertex.Name)
	}
	for _, edge := range in.Edges {
		out.RelTypes = append(out.RelTypes, edge.Name)
	}
	out.PropertyKeys = sortedSet(keys)
	if len(counts) > 0 {
		out.Counts = counts
	}
	return out
}

func sortedSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for member := range set {
		out = append(out, member)
	}
	sort.Strings(out)
	return out
}
