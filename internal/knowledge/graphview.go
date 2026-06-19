// Read-only Neo4j graph-explorer normalizer (Phase 27, GRAPH-01/03/04). It
// compiles STRUCTURED query intents (never raw Cypher) into parameterized
// read-Cypher, runs them through the existing knowledge.Client.Read MCP path, and
// projects the resulting row maps into the flat {nodes,edges,paths,schema,query}
// JSON contract the REST layer (plan 02) and the cockpit (plans 03/04) consume.
//
// All injection-safety lives here: values ride the param map (D-05, never
// fmt.Sprintf into the body), a belt-and-suspenders assertReadOnly write-verb
// guard backs the read-only-by-construction compilers, and the MCP-serialization
// quirks (list->NULL, node->property-dict) are absorbed by the explicit-field
// projection + normalizeRows. No native Go Neo4j driver is imported for reads
// (CLAUDE.md ban); the GraphReader seam exposes Read ONLY.
package knowledge

import (
	"context"
	"errors"
	"fmt"
)

// Cap defaults + hard maxes (GRAPH-04 / D-08): dense graphs default to a legible
// filtered subgraph, and the server clamps every cap before binding so a crafted
// intent can never request an unbounded subgraph.
const (
	defaultNodeCap = 75
	maxNodeCap     = 300
	defaultEdgeCap = 200
	maxEdgeCap     = 800
)

// errBadOp is returned for an unknown GraphIntent.Op. Sanitized — it never echoes
// the offending value beyond the (caller-supplied, already-typed) op string.
var errBadOp = errors.New("graphview: unknown op")

// GraphReader is the narrow Cypher seam GraphView needs. *knowledge.Client
// satisfies it. It exposes Read ONLY — the read-only milestone never surfaces a
// Write path through this seam (mirror reasoningstore.GraphClient, minus Write).
type GraphReader interface {
	Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}

// GraphView wraps a GraphReader and serves the two read operations the REST layer
// needs: Schema (live label/rel-type overview) and Query (a structured intent →
// the flat contract). It holds no state beyond the reader.
type GraphView struct {
	r GraphReader
}

// NewGraphView wraps a GraphReader (typically *knowledge.Client).
func NewGraphView(r GraphReader) *GraphView { return &GraphView{r: r} }

// GraphResult is the flat tagged contract a graph read emits (mirror
// display.Payload discipline: a struct, not an interface, with omitempty so
// decode(encode) is identity and the wire stays lean).
type GraphResult struct {
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
	Paths  []GraphPath `json:"paths,omitempty"`
	Schema GraphSchema `json:"schema"`
	Query  string      `json:"query"`
}

// GraphNode is one node in the contract. ID is the elementId (de-dupe + edge
// anchor). Labels arrives via apoc.convert.toJson(labels(n)) → json.Unmarshal.
// EntityType is the POLE+O sub-type carried on Entity.type — a SECOND color
// dimension (D-02). Citations is DERIVED in the normalizer from this node's
// neighbor edges whose target carries a :Document/:Source label (GRAPH-03 / D-09);
// it costs no extra Cypher and is empty when the node has no such neighbor.
type GraphNode struct {
	ID         string         `json:"id"`
	Caption    string         `json:"caption,omitempty"`
	Labels     []string       `json:"labels,omitempty"`
	EntityType string         `json:"entity_type,omitempty"`
	Degree     int            `json:"degree,omitempty"`
	Props      map[string]any `json:"props,omitempty"`
	RefID      string         `json:"ref_id,omitempty"`
	Citations  []string       `json:"citations,omitempty"`
}

// GraphEdge is one relationship: Source/Target are elementId keys matching node
// IDs (attached via elementId(startNode(r))/elementId(endNode(r))).
type GraphEdge struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	RelType string `json:"rel_type,omitempty"`
}

// GraphPath is an ordered run of edges (the path strip, D-10).
type GraphPath struct {
	Steps []GraphEdge `json:"steps"`
}

// GraphSchema is the live introspection result (D-06): the label set + rel-types
// + property keys feed the left-panel filters + color legend + the schema-overview
// fallback. EntityTypes is the POLE+O second color dimension. Counts is optional.
type GraphSchema struct {
	Labels       []string       `json:"labels"`
	RelTypes     []string       `json:"rel_types"`
	PropertyKeys []string       `json:"property_keys,omitempty"`
	EntityTypes  []string       `json:"entity_types,omitempty"`
	Counts       map[string]int `json:"counts,omitempty"`
}

// Schema runs the live schema-overview query (D-06) and returns the label set.
func (v *GraphView) Schema(ctx context.Context) (GraphSchema, error) {
	cypher, params := compileSchema()
	if err := assertReadOnly(cypher); err != nil {
		return GraphSchema{}, err
	}
	rows, err := v.r.Read(ctx, cypher, params)
	if err != nil {
		return GraphSchema{}, err
	}
	return normalizeSchema(rows), nil
}

// Query dispatches a structured intent through the full
// compile → assertReadOnly → Read → normalize path. GraphResult.Query is set to
// the compiled (display) Cypher so the cockpit's read-only "Show Cypher"
// affordance (D-09) shows the exact query. On an empty seed footprint it falls
// back to the schema overview (D-07/D-08) so the default open is never blank.
func (v *GraphView) Query(ctx context.Context, in GraphIntent) (GraphResult, error) {
	switch in.Op {
	case opSeed:
		return v.runSeed(ctx, in)
	case opExpand:
		cypher, params := compileExpand(in)
		return v.runCompiled(ctx, opExpand, cypher, params)
	case opSchemaOverview:
		return v.schemaResult(ctx)
	default:
		return GraphResult{}, fmt.Errorf("%w: %q", errBadOp, in.Op)
	}
}

// runSeed compiles + runs the conversation footprint; on zero rows it returns the
// schema overview instead of an empty canvas (D-08 fallback).
func (v *GraphView) runSeed(ctx context.Context, in GraphIntent) (GraphResult, error) {
	cypher, params := compileSeed(in)
	if err := assertReadOnly(cypher); err != nil {
		return GraphResult{}, err
	}
	rows, err := v.r.Read(ctx, cypher, params)
	if err != nil {
		return GraphResult{}, err
	}
	res := normalizeRows(opSeed, rows)
	res.Query = cypher
	if len(res.Nodes) == 0 {
		return v.schemaResult(ctx)
	}
	return res, nil
}

// runCompiled runs an already-compiled read and attaches the display Cypher.
func (v *GraphView) runCompiled(ctx context.Context, op, cypher string, params map[string]any) (GraphResult, error) {
	if err := assertReadOnly(cypher); err != nil {
		return GraphResult{}, err
	}
	rows, err := v.r.Read(ctx, cypher, params)
	if err != nil {
		return GraphResult{}, err
	}
	res := normalizeRows(op, rows)
	res.Query = cypher
	return res, nil
}

// schemaResult is the schema-overview as a GraphResult (the empty-state + the
// no-footprint fallback). It carries no nodes/edges — just the schema + the
// compiled query for the Cypher preview.
func (v *GraphView) schemaResult(ctx context.Context) (GraphResult, error) {
	cypher, params := compileSchema()
	if err := assertReadOnly(cypher); err != nil {
		return GraphResult{}, err
	}
	rows, err := v.r.Read(ctx, cypher, params)
	if err != nil {
		return GraphResult{}, err
	}
	return GraphResult{Schema: normalizeSchema(rows), Query: cypher}, nil
}
