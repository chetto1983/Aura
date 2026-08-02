package agui

import (
	"context"
	"sort"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// ArcadeGraphView answers the cockpit's two graph routes from ArcadeDB's
// `schema:types` catalogue, and from nothing else.
//
// THIS IS A CAPABILITY REDUCTION, recorded here rather than discovered later: the
// Graph Explorer used to be drawable. It seeded from the active conversation,
// expanded a clicked node's neighbours and sampled an ownership-scoped
// relationship overview. All three were parameterized Cypher built out of
// elementId(), apoc.convert.toJson(), apoc.map.removeKey() and
// startNode()/endNode() — Neo4j primitives with no ArcadeDB equivalent — so they
// did not survive the move. Porting them is a rewrite, not a translation, and it
// is deliberately not attempted here.
//
// What remains is the type catalogue: which vertex types exist, which edge types,
// what properties they carry and how many records are in each. That is enough for
// the left-panel filters, the colour legend and an honest "here is the shape of
// your memory" empty state. It is not enough to draw a graph, and Query says so
// in the one field a caller reads either way.
type ArcadeGraphView struct {
	types ArcadeSchemaReader
}

// ArcadeSchemaReader hands back one identity's type catalogue. The identity is a
// PARAMETER, not something read out of the context, because memory is one
// ArcadeDB database per identity: naming the wrong one is the whole failure mode
// this design exists to prevent, and an implicit lookup is exactly how it would
// happen quietly.
type ArcadeSchemaReader interface {
	Schema(ctx context.Context, identityID string) (arcadedb.Schema, error)
}

// NewArcadeGraphView wraps a schema reader as the GraphView the routes consume.
func NewArcadeGraphView(types ArcadeSchemaReader) *ArcadeGraphView {
	return &ArcadeGraphView{types: types}
}

// schemaOnlyQuery is what GraphResult.Query carries now that no compiler stands
// behind it. It is prose where Cypher used to be, deliberately: `query` is the
// field the "Show Cypher" affordance displays and the only place a response can
// tell its reader that an empty node list means "not implemented" rather than
// "your graph is empty". The two are very different facts and the wire must not
// blur them.
const schemaOnlyQuery = "schema-only: this view reports the live ArcadeDB type " +
	"catalogue; graph traversal (seed/expand/overview) is not implemented against " +
	"ArcadeDB, so no nodes or edges are returned"

// Schema projects the identity's ArcadeDB type catalogue onto the wire contract.
func (v *ArcadeGraphView) Schema(ctx context.Context, identityID string) (GraphSchema, error) {
	catalogue, err := v.types.Schema(ctx, identityID)
	if err != nil {
		return GraphSchema{}, err
	}
	return projectArcadeSchema(catalogue), nil
}

// Query answers every op with the same schema-only result: the live schema, an
// empty canvas, and a Query string naming the reason. It is a 200 rather than a
// 501 so the panel still renders its schema-driven empty state instead of an
// error card — but the emptiness is explained in the body, never implied.
func (v *ArcadeGraphView) Query(ctx context.Context, in GraphIntent) (GraphResult, error) {
	schema, err := v.Schema(ctx, in.UserID)
	if err != nil {
		return GraphResult{}, err
	}
	return GraphResult{
		Nodes:  []GraphNode{},
		Edges:  []GraphEdge{},
		Schema: schema,
		Query:  schemaOnlyQuery,
	}, nil
}

// projectArcadeSchema maps the catalogue onto the contract:
//
//	vertex types            -> Labels    (what a node could be)
//	edge types              -> RelTypes  (what a connection could be)
//	union of all properties -> PropertyKeys
//	records per type        -> Counts
//
// Document types are counted but are NOT labels: an ArcadeDB document is a record
// with no place on either side of an arrow, and offering it as a canvas filter
// would advertise a node type that can never be drawn.
//
// EntityTypes stays EMPTY. It was the POLE+O `Entity.type` value set — a second
// colour dimension read from the DATA, not from the schema — and `schema:types`
// has no equivalent: recovering it means a DISTINCT over every Entity record,
// which is a data scan this catalogue read is not. The frontend already treats
// the field as optional.
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

// sortedSet returns the set's members in a stable order, or nil when empty so the
// omitempty tag drops the field rather than shipping `[]`.
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
